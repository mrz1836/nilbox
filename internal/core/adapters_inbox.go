package core

import (
	"database/sql"

	"github.com/mrz1836/nilbox/internal/inbox"
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
