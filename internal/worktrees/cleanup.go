package worktrees

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/paths"
)

// Unlock releases a lock the coding agent wrote for a session that has certainly ended,
// so the worktree can be removed. A lock set by hand, or one whose session may still be
// running, is refused, and the lock is read again just before it is released.
func (s *Service) Unlock(ctx context.Context, repoPath, path string) error {
	s.act.Lock()
	defer s.act.Unlock()

	g, err := s.checkedGit(ctx, repoPath)
	if err != nil {
		return err
	}

	release, err := s.holdClone(ctx, repoPath)
	if err != nil {
		return err
	}

	defer release()

	return s.unlockEntry(ctx, g, repoPath, path, "")
}

// CleanUp removes one finished worktree: its pull request merged or its issue closed, it
// has no uncommitted changes, every commit it has is on GitHub, and no session runs in it,
// by its lock or as a running process.
// The worktree is assessed again first, GitHub included. It releases the lock of an agent
// session that has ended, removes the worktree without --force, and deletes the branches
// named, of those it offers, as Remove does.
func (s *Service) CleanUp(ctx context.Context, repoPath, path string, branches []string) (RemoveResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	if err := s.checkNoRun(repoPath); err != nil {
		return RemoveResult{}, err
	}

	g, snap, err := s.assessClone(ctx, repoPath)
	if err != nil {
		return RemoveResult{}, err
	}

	wt, ok := findWorktree(snap, path)
	if err = s.cleanUpRefusal(wt, ok, path, snap.LinkError); err != nil {
		return RemoveResult{}, err
	}

	res, err := s.cleanUpRow(ctx, g, repoPath, wt, false, branches)

	switch {
	case errors.Is(err, clonelock.ErrBusy):
		return res, err
	case !res.Removed:
		return res, fmt.Errorf("%w: %s: %s", ErrCleanUpFailed, wt.DisplayPath, res.Reason)
	}

	return res, nil
}

// CleanUpFinished cleans up the finished worktrees picked, one at a time, each with the
// branches picked for it, and reports each outcome. Each is checked against a fresh
// listing, GitHub included, and again just before it is removed: one no longer finished
// is left, with why.
func (s *Service) CleanUpFinished(ctx context.Context, picks []Pick) ([]RemoveResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	snap, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	if snap.LinkError != "" {
		return nil, fmt.Errorf("%w: %s", ErrGitHubUnavailable, snap.LinkError)
	}

	g, err := s.git()
	if err != nil {
		return nil, err
	}

	results := make([]RemoveResult, 0, len(picks))

	for _, p := range picks {
		if err = ctx.Err(); err != nil {
			return results, err
		}

		wt, ok := findWorktree(snap, p.Path)

		switch {
		case !ok:
			results = append(results, pickRefused(p, s.display(p.Path), ErrNotAWorktree))
		case !wt.CanCleanUp:
			results = append(results, pickRefused(p, wt.DisplayPath, fmt.Errorf("%w: %s", ErrNotFinished,
				notFinishedReason(wt))))
		default:
			res, _ := s.cleanUpRow(ctx, g, p.RepoPath, wt, false, p.Branches) // its Reason says why
			results = append(results, res)
		}
	}

	return results, nil
}

// unlockEntry re-reads the worktree's lock and releases it when it is an agent lock whose
// session has ended. When want is not "", the lock must still be the one assessed, so a
// session that took the worktree over since then keeps its lock.
func (s *Service) unlockEntry(ctx context.Context, g git, repoPath, path, want string) error {
	e, err := s.entryOf(ctx, g, repoPath, path)
	if err != nil {
		return err
	}

	_, agent := parseAgentLock(e.LockReason)

	switch {
	case !e.Locked:
		return ErrNotLocked
	case !agent:
		return ErrHandLock
	case want != "" && e.LockReason != want:
		return ErrChanged
	case !endedAgentLock(e.LockReason, s.procs, s.zone):
		return ErrSessionRunning
	}

	if stderr, unlockErr := g.unlock(ctx, repoPath, e.Path); unlockErr != nil {
		return fmt.Errorf("unlock %s: %w", s.display(e.Path), gitError(unlockErr, stderr))
	}

	return nil
}

// cleanUpRefusal says why CleanUp refuses the row found for path, or is nil when it may
// go ahead.
func (s *Service) cleanUpRefusal(wt Worktree, found bool, path, linkError string) error {
	switch {
	case !found:
		return fmt.Errorf("%w: %s", ErrNotAWorktree, s.display(path))
	case wt.Main:
		return ErrMainWorktree
	case wt.CanCleanUp:
		return nil
	case linkError != "":
		return fmt.Errorf("%w: %s", ErrGitHubUnavailable, linkError)
	default:
		return fmt.Errorf("%w: %s", ErrNotFinished, notFinishedReason(wt))
	}
}

// cleanUpRow removes one worktree assessed as finished, with the branches named, records
// it in the history, and reports the outcome; automatic tells a sweep from the user. Each
// step checks git again: the lock must be the one assessed and its session still ended,
// the worktree must still have the commit assessed checked out, git refuses one with
// uncommitted changes, and a branch is deleted only when it may (see deleteChosen). It
// holds the clone's lock throughout; the error, when it removed nothing, is why, which
// the Reason words, and a run using the clone is a clonelock.ErrBusy (see holdClone).
func (s *Service) cleanUpRow(ctx context.Context, g git, repoPath string, wt Worktree, automatic bool,
	branches []string,
) (RemoveResult, error) {
	res := RemoveResult{
		RepoPath: repoPath, Path: wt.Path, DisplayPath: wt.DisplayPath, Item: itemName(wt.Item),
		DeletedBranches: []string{},
	}

	unlock, err := s.holdClone(ctx, repoPath)
	if err == nil {
		defer unlock()

		err = s.removeFinished(ctx, g, repoPath, wt, automatic)
	}

	if err != nil {
		res.Reason = reasonOf(err)

		return res, err
	}

	res.Removed = true

	s.forgetRecord(ctx, wt.Path)

	deleted, kept := s.deleteChosen(ctx, g, repoPath, wt, branches)
	res.DeletedBranches, res.Reason = deleted, strings.Join(kept, "; ")

	s.recordCleanup(ctx, repoPath, wt, strings.Join(deleted, ", "), automatic)

	return res, nil
}

// removeFinished releases a finished worktree's stale lock, if it has one, and removes it
// without --force while it still has the commit assessed checked out and no running
// process works in it. automatic also leaves it when the processes cannot be listed.
func (s *Service) removeFinished(ctx context.Context, g git, repoPath string, wt Worktree, automatic bool) error {
	if wt.Locked {
		// Checked before the lock is released, too, so a worktree in use keeps its lock.
		if err := s.checkSessions(ctx, wt.Path, automatic); err != nil {
			return err
		}

		if err := s.unlockEntry(ctx, g, repoPath, wt.Path, wt.LockReason); err != nil {
			return err
		}
	}

	e, err := s.removable(ctx, repoPath, wt.Path, wt.Head, automatic)
	if err != nil {
		return err
	}

	return s.removeEntry(ctx, g, repoPath, e, false)
}

// forgetRecord removes the record of a removed worktree. A record left behind is dropped
// by the next listing or sweep, so a failure is only logged.
func (s *Service) forgetRecord(ctx context.Context, path string) {
	for _, r := range s.records(ctx) {
		if paths.Same(r.Dir(), path) {
			s.forget(ctx, r)
		}
	}
}

// findWorktree returns the snapshot's row for path.
func findWorktree(snap Snapshot, path string) (Worktree, bool) {
	for _, wt := range rows(snap.Repos) {
		if paths.Same(wt.Path, path) {
			return *wt, true
		}
	}

	return Worktree{}, false
}

// notFinishedReason says why CleanUp refuses a worktree that is not finished.
func notFinishedReason(wt Worktree) string {
	if len(wt.InUse) > 0 {
		return (&InUseError{Sessions: wt.InUse}).Error()
	}

	if wt.Locked && !wt.StaleLock {
		return lockedReason(wt)
	}

	switch {
	case wt.Prunable:
		return "its directory is gone; prune the repository instead"
	case wt.Item == nil:
		return "it is not linked to a pull request or issue"
	case !wt.Item.done():
		return "its pull request or issue is still open"
	case wt.Dirty:
		return "it has uncommitted changes"
	case wt.Error != "":
		return wt.Error
	default:
		return "it has commits that are not on GitHub"
	}
}

// lockedReason says why a worktree under a live or hand-set lock is refused.
func lockedReason(wt Worktree) string {
	if wt.AgentLock {
		return "a session is still running in it"
	}

	return "it is locked"
}
