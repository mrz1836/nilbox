package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
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

	// The refresh applies again, after this, every item write stored while it fetched (see
	// order.go), so its closed results' state needs no guard of its own.
	if err := w.saveClosedStates(ctx, got.closed, nil); err != nil {
		return err
	}

	return w.pruneOld(ctx, got.kept)
}

// pruneOld deletes the items nothing refers to that no read has brought for pruneAfter,
// but kept: in no list, watched by no routine's search, and with no triage of the user's
// (PruneItems). Each one's details, file check, checks, and the caches of its sheets go
// with it, by their foreign keys. What else refers to an item is the Keep seam's (core
// hands it Keepers over the agent sessions and the notifications), so a later owner of
// such a reference extends the rule there, never here.
func (w *writer) pruneOld(ctx context.Context, kept []int64) error {
	keep := append([]int64{0}, kept...)

	if _, err := w.q.PruneItems(ctx, queries.PruneItemsParams{Cutoff: w.now.Add(-pruneAfter), Keep: keep}); err != nil {
		return fmt.Errorf("prune the old items: %w", err)
	}

	return nil
}

// saveClosedStates records the state of the items a refresh's or a pass's searches
// returned merged or closed, which the index trails, as their own reads would: the
// searches read no more of them. A merged item keeps its state, and with readBefore, when
// the searches went out, an item stored since keeps what that newer read stored.
func (w *writer) saveClosedStates(ctx context.Context, closed []github.Item, readBefore *time.Time) error {
	for _, it := range closed {
		if err := setRefState(ctx, w.q, it.Ref(), it.State, it.ClosedAt, w.now, readBefore); err != nil {
			return err
		}
	}

	return nil
}

// setRefState records the state of every stored item of ref's repository and number, as
// a read said, stamping fetched_at with stamp; with readBefore, an item stored at or after
// it keeps what that newer read stored (SetRefStateBefore).
func setRefState(
	ctx context.Context, q *queries.Queries, ref item.Ref, state string, closedAt, stamp time.Time, readBefore *time.Time,
) error {
	var err error

	if readBefore == nil {
		err = q.SetRefState(ctx, queries.SetRefStateParams{
			State: state, ClosedAt: timeOrNil(closedAt), FetchedAt: stamp.UTC(), Number: int64(ref.Number),
			Owner: ref.Repo.Owner, Name: ref.Repo.Repo,
		})
	} else {
		err = q.SetRefStateBefore(ctx, queries.SetRefStateBeforeParams{
			State: state, ClosedAt: timeOrNil(closedAt), FetchedAt: stamp.UTC(), Number: int64(ref.Number),
			Owner: ref.Repo.Owner, Name: ref.Repo.Repo, ReadBefore: readBefore.UTC(),
		})
	}

	if err != nil {
		return fmt.Errorf("save the state of %s: %w", ref, err)
	}

	return nil
}
