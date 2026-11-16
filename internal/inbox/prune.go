package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
)

// pruneAfter is how long an item nothing refers to is kept after the last read that
// brought it: one in no list, which no routine's search watches, with no triage of the
// user's, and that the Keep seam doesn't name. Watching adds rows the inbox would
// otherwise never store, and this keeps them from piling up.
const pruneAfter = 30 * 24 * time.Hour

// keptItems returns the items something outside the inbox refers to, which the prune
// keeps, read before a whole refresh's transaction; none without a Keep seam.
func (s *Service) keptItems(ctx context.Context) ([]int64, error) {
	if s.keep == nil {
		return nil, nil
	}

	ids, err := s.keep.KeptItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the items kept: %w", err)
	}

	return ids, nil
}

// saveLists stores a whole refresh's lists: the repositories of the items left out, the
// items with their reason flags, the state of the items its searches returned merged or
// closed, and then the prune of the old items.
func (w *writer) saveLists(ctx context.Context, got refreshed) error {
	if err := w.saveRepos(ctx, got.archived); err != nil {
		return err
	}

	if err := w.saveItems(ctx, got.inbox); err != nil {
		return err
	}

	if err := w.saveClosedStates(ctx, got.closed); err != nil {
		return err
	}

	return w.pruneOld(ctx, got.kept)
}

// pruneOld deletes the items nothing refers to that no read has brought for pruneAfter,
// but kept: in no list, watched by no routine's search, and with no triage of the user's
// (PruneItems). Each one's details, file check, and checks go with it. What else refers
// to an item is the Keep seam's, so a later owner of such a reference extends the rule
// there, never here.
func (w *writer) pruneOld(ctx context.Context, kept []int64) error {
	keep := append([]int64{0}, kept...)

	if _, err := w.q.PruneItems(ctx, queries.PruneItemsParams{Cutoff: w.now.Add(-pruneAfter), Keep: keep}); err != nil {
		return fmt.Errorf("prune the old items: %w", err)
	}

	return nil
}

// saveClosedStates records the state of the items a refresh's searches returned merged or
// closed, which the index trails, as their own reads would: the searches read no more of
// them.
func (w *writer) saveClosedStates(ctx context.Context, closed []github.Item) error {
	for _, it := range closed {
		err := w.q.SetRefState(ctx, queries.SetRefStateParams{
			State: it.State, ClosedAt: timeOrNil(it.ClosedAt), Number: int64(it.Number),
			Owner: it.RepoOwner, Name: it.RepoName,
		})
		if err != nil {
			return fmt.Errorf("save the state of %s: %w", it.Ref(), err)
		}
	}

	return nil
}
