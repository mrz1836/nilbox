package worktrees

import (
	"context"
	"errors"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// holdClone takes clone's lock for a worktree change and returns its release. While
// another worktree change holds the clone it waits, until ctx ends: those are short, and
// none waits on act while it holds a clone. Any other job, a run, fails it at once with an
// error that names the run for the user, so nothing waits behind a run that could take
// half an hour, least of all a removal holding act, which every launch then queues behind.
func (s *Service) holdClone(ctx context.Context, clone string) (func(), error) {
	release, err := s.clones.LockWaitingFor(ctx, clone, s.worktreeHolder(clone), isWorktreeChange)

	switch busy, isBusy := errors.AsType[*clonelock.BusyError](err); {
	case isBusy:
		return nil, uierr.New(busy.Holder.Sentence(), err)
	case errors.Is(err, context.DeadlineExceeded):
		return nil, uierr.New("Another worktree change in "+s.display(clone)+" took too long. Try again in a moment.", err)
	}

	return release, err
}

// tryClone takes clone's lock for a worktree change only when it is free, and reports
// whether it did.
func (s *Service) tryClone(clone string) (func(), bool) {
	release, _, ok := s.clones.TryLock(clone, s.worktreeHolder(clone))

	return release, ok
}

// checkNoRun returns the error holdClone would for a run using clone, or nil. A click
// checks it before assessing the clone, which costs git and GitHub requests, so a busy
// clone is refused without them; holdClone checks again before anything changes.
func (s *Service) checkNoRun(clone string) error {
	if held, busy := s.runUsing(clone); busy {
		return uierr.New(held.Sentence(), &clonelock.BusyError{Holder: held})
	}

	return nil
}

// runUsing returns the run using clone, and true, or false when none is.
func (s *Service) runUsing(clone string) (clonelock.Holder, bool) {
	held, busy := s.clones.Holder(clone)

	return held, busy && !isWorktreeChange(held)
}

// skippedClones returns the repositories a run is using, which the sweep leaves as they
// are this time, logging each once.
func (s *Service) skippedClones(ctx context.Context, repos []Repo) map[string]bool {
	skipped := map[string]bool{}

	for _, r := range repos {
		if held, busy := s.runUsing(r.Path); busy {
			skipped[r.Path] = true

			s.logSkipped(ctx, held)
		}
	}

	return skipped
}

// logSkipped logs that the sweep left a clone held by held for the next sweep.
func (s *Service) logSkipped(ctx context.Context, held clonelock.Holder) {
	s.logger.InfoContext(ctx, "the sweep skipped a clone another job is using; the next sweep tries again",
		slog.String("job", string(held.Kind)))
}

// worktreeHolder is a worktree change's hold on clone, which names it as the window does.
func (s *Service) worktreeHolder(clone string) clonelock.Holder {
	return clonelock.Holder{Kind: clonelock.KindWorktree, Repo: s.display(clone)}
}

// isWorktreeChange reports a holder that is a worktree change.
func isWorktreeChange(held clonelock.Holder) bool {
	return held.Kind == clonelock.KindWorktree
}
