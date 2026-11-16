package autopilot

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// updateProblems works out the routines' problems now (a routine that won't load, a search
// GitHub fails), keeps when each began, forgets the ones that ended, and tells the user of
// each new one, once. A problem's start is saved before it is told, since its notice is
// keyed by it: a crash after the telling tells it again with the same key, which the
// notifications refuse, never with a later start.
func (e *Engine) updateProblems(ctx context.Context, p *pass) {
	found := p.problems()
	kept := make([]Problem, 0, len(found))

	for _, f := range found {
		if i := slices.IndexFunc(e.state.Problems, func(p Problem) bool { return p.Key == f.Key }); i >= 0 {
			f.Since, f.Told = e.state.Problems[i].Since, e.state.Problems[i].Told
		}

		kept = append(kept, f)
	}

	e.state.Problems = kept

	if e.problems != nil && slices.ContainsFunc(kept, func(p Problem) bool { return !p.Told }) {
		if err := writeEngineState(ctx, e.prefs, e.state); err != nil {
			e.logger.WarnContext(ctx, "could not save Autopilot's problems; telling them on a later cycle", "error", err)

			return
		}
	}

	e.tellProblems(ctx, p)
}

// problems returns the routines' problems now: each enabled routine of the account that
// won't load, and each search of a routine that GitHub fails.
func (p *pass) problems() []Problem {
	return append(p.brokenProblems(), p.searchProblems()...)
}

// brokenProblems returns a problem for each enabled routine of the account that won't
// load.
func (p *pass) brokenProblems() []Problem {
	found := []Problem{}

	for _, row := range p.others {
		if !row.Enabled || otherAccount(row.AccountLogin, p.inbox.Viewer) {
			continue
		}

		if l := load(row.Spec); l.broken != nil {
			found = append(found, Problem{
				Key: problemKey(row.ID, ProblemWontLoad, ""), RoutineID: row.ID, Kind: ProblemWontLoad,
				Detail: brokenWords(l.broken), Since: p.now.UTC(),
			})
		}
	}

	return found
}

// searchProblems returns a problem for each search of a routine that GitHub fails.
func (p *pass) searchProblems() []Problem {
	found := []Problem{}

	for _, r := range p.routines {
		for _, key := range r.keys {
			if st := p.inbox.Searches[key]; st.Error != "" {
				found = append(found, Problem{
					Key: problemKey(r.id(), ProblemSearchFailed, key), RoutineID: r.id(), Kind: ProblemSearchFailed,
					Detail: "GitHub couldn't answer its search: " + st.Error, Since: p.now.UTC(),
				})
			}
		}
	}

	return found
}

// problemKey names a problem by its routine, its kind, and its search.
func problemKey(routineID int64, kind ProblemKind, search string) string {
	key := strconv.FormatInt(routineID, 10) + ":" + string(kind)
	if search != "" {
		key += ":" + search
	}

	return key
}

// tellProblems tells the user of each problem not told yet, once: with the routine's name
// and its own banner switch for problems, and gathered when there are more than one, as
// on a first pass. One that couldn't be told is told on a later cycle.
func (e *Engine) tellProblems(ctx context.Context, p *pass) {
	if e.problems == nil {
		return
	}

	untold := 0

	for _, pr := range e.state.Problems {
		if !pr.Told {
			untold++
		}
	}

	for i := range e.state.Problems {
		pr := &e.state.Problems[i]
		if pr.Told {
			continue
		}

		if err := e.problems.Problem(ctx, p.problemNotice(*pr, untold > 1 || e.state.CatchingUp[pr.RoutineID])); err != nil {
			e.logger.InfoContext(ctx, "Autopilot could not tell of a problem", "routine", pr.RoutineID, "error", err)

			continue
		}

		pr.Told = true
	}
}

// problemNotice is pr as the user is told of it: with its routine's name and when it was
// made, and the routine's own banner switch for its problems, when it loads.
func (p *pass) problemNotice(pr Problem, burst bool) ProblemNotice {
	notice := ProblemNotice{Problem: pr, Burst: burst}

	if r, ok := p.byID[pr.RoutineID]; ok {
		notice.Routine, notice.RoutineCreated = r.row.Name, r.row.CreatedAt

		if r.spec.Notify != nil {
			notice.Banner = r.spec.Notify.Problem
		}
	} else if row, ok := p.others[pr.RoutineID]; ok {
		notice.Routine, notice.RoutineCreated = row.Name, row.CreatedAt
	}

	return notice
}

// Cleared is what Clear history removed.
type Cleared struct {
	// Removed is how many matches it removed, with their runs, and Kept how many it kept,
	// without their traces and summaries, since they still block their occurrence.
	Removed int `json:"removed"`
	Kept    int `json:"kept"`
}

// prune runs the retention, at most every pruneEvery: closed matches older than the
// history kept, once their item is known to be done with.
func (e *Engine) prune(ctx context.Context, now time.Time, cfg autopilotprefs.Config) {
	if now.Sub(e.state.LastPrune) < pruneEvery {
		return
	}

	e.state.LastPrune = now

	cutoff := now.Add(-time.Duration(cfg.HistoryDays) * 24 * time.Hour)

	cleared, err := e.clear(ctx, cutoff, cutoff, false)
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not prune its history", "error", err)

		return
	}

	if cleared.Removed > 0 {
		e.logger.InfoContext(ctx, "Autopilot pruned its history", "removed", cleared.Removed)
	}
}

// clear removes the closed matches closed before closedBefore whose item is done with:
// gone, merged or closed, or out of every list and watch since before goneBefore, which
// is always the history's days ago, since the app reads no more of an item once it left
// them. Deleting the record of an item still open would let it fire again, so a match
// that may still block its occurrence stays; with scrub, it stays without its trace and
// its summaries. It also forgets what deleted routines kept of their items, and removes
// for good a deleted routine nothing refers to.
func (e *Engine) clear(ctx context.Context, closedBefore, goneBefore time.Time, scrub bool) (Cleared, error) {
	remove, keep, err := e.prunable(ctx, closedBefore, goneBefore)
	if err != nil {
		return Cleared{}, err
	}

	out := Cleared{Removed: len(remove)}
	if scrub {
		out.Kept = len(keep)
	}

	err = e.ledger.inTx(ctx, func(q *queries.Queries) error {
		if deleteErr := deleteMatches(ctx, q, remove); deleteErr != nil {
			return deleteErr
		}

		if scrub {
			if clearErr := scrubMatches(ctx, q, keep); clearErr != nil {
				return clearErr
			}
		}

		return e.forgetDeleted(ctx, q)
	})
	if err != nil {
		return Cleared{}, fmt.Errorf("clear the history: %w", err)
	}

	return out, nil
}

// prunable returns the closed matches closed before closedBefore that may go, their item
// done with (doneWith, by goneBefore), and those that stay, since they may still block
// their occurrence.
func (e *Engine) prunable(ctx context.Context, closedBefore, goneBefore time.Time) (remove, keep []int64, err error) {
	rows, err := e.ledger.q.ListPrunableMatches(ctx, new(closedBefore))
	if err != nil {
		return nil, nil, fmt.Errorf("read the closed matches: %w", err)
	}

	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.ItemID != nil {
			ids = append(ids, *r.ItemID)
		}
	}

	states, err := e.items.ItemStates(ctx, union(ids, nil))
	if err != nil {
		return nil, nil, err
	}

	for _, r := range rows {
		if doneWith(r.ItemID, states, goneBefore) {
			remove = append(remove, r.ID)
		} else {
			keep = append(keep, r.ID)
		}
	}

	return remove, keep, nil
}

// doneWith reports an item the ledger may forget: gone, merged or closed, or out of every
// list and watch since before cutoff.
func doneWith(id *int64, states map[int64]ItemState, cutoff time.Time) bool {
	if id == nil {
		return true
	}

	st, ok := states[*id]

	return !ok || st.Closed() || !st.Listed && !st.Watched && st.FetchedAt.Before(cutoff)
}

// deleteMatches deletes the matches ids, with their runs.
func deleteMatches(ctx context.Context, q *queries.Queries, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	_, err := q.DeleteMatches(ctx, ids)

	return err
}

// scrubMatches clears the traces and the summaries of the matches ids.
func scrubMatches(ctx context.Context, q *queries.Queries, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	if err := q.ClearMatchTraces(ctx, ids); err != nil {
		return err
	}

	return q.ClearRunSummaries(ctx, ids)
}

// forgetDeleted forgets what deleted routines kept of their items, and removes a deleted
// routine nothing of the history refers to.
func (e *Engine) forgetDeleted(ctx context.Context, q *queries.Queries) error {
	for _, del := range []func(context.Context) error{
		q.DeleteDeletedRoutinesState, q.DeleteDeletedRoutinesTries, q.DeleteDeletedRoutinesHandsOff,
	} {
		if err := del(ctx); err != nil {
			return err
		}
	}

	_, err := q.DeleteGoneRoutines(ctx, new(e.clock.Now()))

	return err
}
