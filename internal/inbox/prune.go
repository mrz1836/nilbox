package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilotprefs"
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
// keeps, read before a whole refresh's transaction; none without a Keep seam. ok is false
// when they couldn't be read, which is logged: the refresh then prunes nothing, keeping
// everything, and goes on.
func (s *Service) keptItems(ctx context.Context) (ids []int64, ok bool) {
	if s.keep == nil {
		return nil, true
	}

	ids, err := s.keep.KeptItems(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the items kept, so this refresh prunes no old item", "error", err)

		return nil, false
	}

	return ids, true
}

// saveLists stores a whole refresh's lists: the repositories of the items left out, the
// items with their reason flags, the state of the items its searches returned merged or
// closed, and then the prune of the old items, unless what else refers to an item
// couldn't be read.
func (w *writer) saveLists(ctx context.Context, got refreshed) error {
	if err := w.saveRepos(ctx, got.archived); err != nil {
		return err
	}

	if err := w.saveItems(ctx, got.inbox); err != nil {
		return err
	}

	// The refresh applies again, after this, every item write stored while it fetched (see
	// order.go), so its closed results' state needs no guard of its own.
	if err := w.saveClosedStates(ctx, got.closed); err != nil {
		return err
	}

	if err := w.pruneRestoreMarks(ctx); err != nil {
		return err
	}

	if !got.keptRead {
		return nil
	}

	return w.pruneOld(ctx, got.kept)
}

// pruneRestoreMarks drops the marks routines' Restores left on items they brought back,
// once they are older than Autopilot keeps its history (autopilot.historyDays, which
// settings writes): only an Undo of that Restore reads one, and an item with nothing else
// in its local state is then pruned as any other. The user's own triage of the item clears
// its mark sooner.
func (w *writer) pruneRestoreMarks(ctx context.Context) error {
	stored, err := w.prefs.GetMany(ctx, autopilotprefs.KeyHistoryDays)
	if err != nil {
		return fmt.Errorf("read how long Autopilot keeps its history: %w", err)
	}

	days := autopilotprefs.Decode(stored).HistoryDays

	if err = w.q.PruneRestoreMarks(ctx, w.now.AddDate(0, 0, -days)); err != nil {
		return fmt.Errorf("prune the routines' old restore marks: %w", err)
	}

	return nil
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

// saveClosedStates records the state of the items a whole refresh's searches returned
// merged or closed, which the index trails, as their own reads would: the searches read
// no more of them. A merged item keeps its state.
func (w *writer) saveClosedStates(ctx context.Context, closed []github.Item) error {
	for _, it := range closed {
		err := w.q.SetRefState(ctx, queries.SetRefStateParams{
			State: it.State, ClosedAt: timeOrNil(it.ClosedAt), FetchedAt: w.now, Number: int64(it.Number),
			Owner: it.RepoOwner, Name: it.RepoName,
		})
		if err != nil {
			return fmt.Errorf("save the state of %s: %w", it.Ref(), err)
		}
	}

	return nil
}

// noteClosedStates records the state of the items a pass's or a test's searches, which
// went out at readBefore, returned merged or closed, as saveClosedStates does, and
// returns the states it wrote, for the write order: an item stored at or after
// readBefore keeps what that newer read stored.
func (w *writer) noteClosedStates(ctx context.Context, closed []github.Item, readBefore time.Time) ([]stateWrite, error) {
	var out []stateWrite

	for _, it := range closed {
		wrote, err := setRefStateBefore(ctx, w.q, it.Ref(), it.State, it.ClosedAt, w.now, readBefore)
		if err != nil {
			return nil, err
		}

		out = append(out, wrote...)
	}

	return out, nil
}

// setRefStateBefore records the state of every stored item of ref's repository and number
// stored before readBefore, as a read that went out then said, stamping fetched_at with
// stamp, and returns the states it wrote (SetRefStateBefore).
func setRefStateBefore(
	ctx context.Context, q *queries.Queries, ref item.Ref, state string, closedAt, stamp, readBefore time.Time,
) ([]stateWrite, error) {
	ids, err := q.SetRefStateBefore(ctx, queries.SetRefStateBeforeParams{
		State: state, ClosedAt: timeOrNil(closedAt), FetchedAt: stamp.UTC(), Number: int64(ref.Number),
		Owner: ref.Repo.Owner, Name: ref.Repo.Repo, ReadBefore: readBefore.UTC(),
	})
	if err != nil {
		return nil, fmt.Errorf("save the state of %s: %w", ref, err)
	}

	out := make([]stateWrite, 0, len(ids))
	for _, id := range ids {
		out = append(out, stateWrite{id: id, state: state, closedAt: closedAt})
	}

	return out, nil
}

// stateIDs returns the ids of the items states names.
func stateIDs(states []stateWrite) []int64 {
	out := make([]int64, 0, len(states))
	for _, st := range states {
		out = append(out, st.id)
	}

	return out
}
