package ghthreads

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// checkStates checks the state of the items st's account's threads are about that are due
// (dueRefs), and those asked, in one call that batches them, and stores each answer: the
// state, when the item was merged or closed, and when it was checked; an item GitHub
// couldn't find is stamped checked, its state kept. With onlyAsked, as for a nudged run
// that didn't list, only those asked are checked. A call that fails stores nothing but the
// status's last error, and leaves the listing stored. stored reports whether any state was
// stored, which the threads read before it no longer show.
func (s *Service) checkStates(
	ctx context.Context, st syncState, asked []item.Ref, onlyAsked bool, at time.Time,
) (stored bool, err error) {
	refs, err := s.refsToCheck(ctx, st.Account, asked, onlyAsked, at)
	if err != nil {
		return false, s.failed(ctx, st, err)
	}

	if len(refs) == 0 {
		return false, nil
	}

	states, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (map[string]github.ItemState, error) {
		return s.client.FetchItemStates(ctx, tok, refs)
	})
	if err != nil {
		return false, s.failed(ctx, st, fmt.Errorf("check the state of the GitHub notifications' items: %w", err))
	}

	err = store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, ref := range refs {
			if storeErr := storeState(ctx, q, st.Account, ref, states, at); storeErr != nil {
				return storeErr
			}
		}

		return nil
	})
	if err != nil {
		return false, s.failed(ctx, st, fmt.Errorf("store the state of the GitHub notifications' items: %w", err))
	}

	return true, nil
}

// refsToCheck returns the items of account's threads whose state is checked as of at: those
// due and those asked (dueRefs), or, with onlyAsked, those asked alone.
func (s *Service) refsToCheck(
	ctx context.Context, account string, asked []item.Ref, onlyAsked bool, at time.Time,
) ([]item.Ref, error) {
	if onlyAsked && len(asked) == 0 {
		return nil, nil
	}

	rows, err := s.q.ListItemThreads(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("read the GitHub notifications' items: %w", err)
	}

	refs := dueRefs(rows, asked, at)
	if onlyAsked {
		return onlyRefs(refs, asked), nil
	}

	return refs, nil
}

// storeState stores what GitHub said of ref, from states, on account's threads about it,
// checked at: its state and when it was merged or closed, or, when GitHub found no such
// item or gave a state the app doesn't know, only that it was checked.
func storeState(
	ctx context.Context, q *queries.Queries, account string, ref item.Ref, states map[string]github.ItemState, at time.Time,
) error {
	checked := at.UTC()
	answer, ok := states[ref.Key()]
	state := subjectState(answer.State)

	if !ok || state == threadrules.StateUnknown {
		err := q.StampStateChecked(ctx, queries.StampStateCheckedParams{
			StateCheckedAt: &checked, AccountLogin: account, RepoOwner: ref.Repo.Owner, RepoName: ref.Repo.Repo,
			Number: int64(ref.Number),
		})
		if err != nil {
			return fmt.Errorf("store that %s was checked: %w", ref, err)
		}

		return nil
	}

	var closedAt *time.Time

	if state != threadrules.StateOpen && !answer.ClosedAt.IsZero() {
		at := answer.ClosedAt.UTC()
		closedAt = &at
	}

	err := q.SetSubjectState(ctx, queries.SetSubjectStateParams{
		SubjectState: string(state), SubjectClosedAt: closedAt, StateCheckedAt: &checked, AccountLogin: account,
		RepoOwner: ref.Repo.Owner, RepoName: ref.Repo.Repo, Number: int64(ref.Number),
	})
	if err != nil {
		return fmt.Errorf("store the state of %s: %w", ref, err)
	}

	return nil
}

// subjectState reads GitHub's state of an item as a threadrules.State, StateUnknown for
// one the app doesn't know.
func subjectState(s string) threadrules.State {
	switch s {
	case github.StateOpen:
		return threadrules.StateOpen
	case github.StateClosed:
		return threadrules.StateClosed
	case github.StateMerged:
		return threadrules.StateMerged
	default:
		return threadrules.StateUnknown
	}
}

// dueRefs returns, each once, in the order of rows, the items whose state a pass checks as
// of at: those of rows due (stateDue), and those of asked that a row is about.
func dueRefs(rows []queries.ListItemThreadsRow, asked []item.Ref, at time.Time) []item.Ref {
	wanted := make(map[string]bool, len(asked))
	for _, ref := range asked {
		wanted[ref.Key()] = true
	}

	seen := map[string]bool{}

	var out []item.Ref

	for i := range rows {
		ref, ok := rowRef(&rows[i])
		if !ok || seen[ref.Key()] {
			continue
		}

		if wanted[ref.Key()] || stateDue(&rows[i], at) {
			seen[ref.Key()] = true
			out = append(out, ref)
		}
	}

	return out
}

// onlyRefs returns those of refs that asked names, in their order.
func onlyRefs(refs, asked []item.Ref) []item.Ref {
	wanted := make(map[string]bool, len(asked))
	for _, ref := range asked {
		wanted[ref.Key()] = true
	}

	out := make([]item.Ref, 0, len(refs))

	for _, ref := range refs {
		if wanted[ref.Key()] {
			out = append(out, ref)
		}
	}

	return out
}

// stateDue reports whether the state of a thread's item is checked as of at: never
// checked, its thread updated since the last check, or the item not known to be merged or
// closed (open, or one GitHub couldn't find) and last checked StateRecheck ago or more. A
// merged or closed item whose thread didn't change is never checked again.
func stateDue(r *queries.ListItemThreadsRow, at time.Time) bool {
	if r.StateCheckedAt == nil {
		return true
	}

	checked := *r.StateCheckedAt
	if r.UpdatedAt.After(checked) {
		return true
	}

	if state := threadrules.State(r.SubjectState); state == threadrules.StateMerged || state == threadrules.StateClosed {
		return false
	}

	return at.Sub(checked) >= StateRecheck
}

// rowRef is the item a row is about; ok is false for a row that names none the app could
// ask about.
func rowRef(r *queries.ListItemThreadsRow) (item.Ref, bool) {
	kind := item.Kind(r.ItemKind)
	name := repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}

	if !kind.Valid() || r.Number <= 0 || !name.Valid() {
		return item.Ref{}, false
	}

	return item.Ref{Repo: name, Number: int(r.Number), Kind: kind}, true
}
