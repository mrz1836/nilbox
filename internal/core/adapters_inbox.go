package core

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/sessions"
)

// itemKeepers names the stored items the tables outside the inbox refer to by id, which
// the inbox's prune of old items keeps (inbox.Config.Keep): an agent session's item, whose
// id has no foreign key, and a notification's, whose link to its item would otherwise be
// cleared, and what Autopilot's ledger refers to, a match's or a run's item, whose record
// of an occurrence must outlive its item's rows, or the item fires again when it comes
// back. A worktree record and a GitHub notification thread name their item by its
// repository and number, so a pruned item comes back for them with the next read, and the
// sheets' caches and the item's own rows go with it.
func itemKeepers(pool *sql.DB, ledger inbox.ItemKeeper) inbox.Keepers {
	return inbox.Keepers{sessions.NewStore(pool), notify.NewStore(pool), ledger}
}

// pullCacheEdits and issueCacheEdits are what the sheets' caches follow of the inbox's own
// edits: review's and issues' InboxEdits.
type (
	pullCacheEdits interface {
		Unassigned(ctx context.Context, id int64, login string) error
	}

	issueCacheEdits interface {
		Unassigned(ctx context.Context, id int64) error
	}
)

// sheetOutcomes tells the sheets' caches what the inbox's own edits changed on GitHub
// (inbox.OutcomeSink): an Unassign me takes the user off the assignees the review sheet
// keeps of a pull request at once, and marks the issue sheet's view of an issue out of
// date, so a sheet opened just after shows them unassigned. Core builds the sheets after the
// inbox, so it sets pulls and issues once it has them; until then it tells nothing.
type sheetOutcomes struct {
	pulls  pullCacheEdits
	issues issueCacheEdits
	logger *slog.Logger
}

// Settle follows each Unassign me of an item the inbox names, in its sheet's cache. A
// cache that can't follow is logged: the sheet reads GitHub once its read is old.
func (s *sheetOutcomes) Settle(ctx context.Context, outcomes []inbox.Outcome) {
	if s == nil || s.pulls == nil || s.issues == nil {
		return
	}

	for _, o := range outcomes {
		if o.Kind != inbox.OutcomeUnassigned || o.ItemID <= 0 {
			continue
		}

		if err := s.unassigned(ctx, o); err != nil {
			logging.Or(s.logger).WarnContext(ctx, "could not follow an unassignment in the sheet's cache",
				"item", o.Ref.String(), "error", err)
		}
	}
}

// unassigned follows one Unassign me in the cache of the sheet its item opens in.
func (s *sheetOutcomes) unassigned(ctx context.Context, o inbox.Outcome) error {
	if o.Ref.Kind == item.KindPR {
		return s.pulls.Unassigned(ctx, o.ItemID, o.Login)
	}

	return s.issues.Unassigned(ctx, o.ItemID)
}
