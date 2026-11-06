package worktrees

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/localrepos"
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

	return s.unlockEntry(ctx, g, repoPath, path, "")
}

// CleanUp removes one finished worktree: its pull request merged or its issue closed, it
// has no uncommitted changes, every commit it has is on GitHub, and no session runs in it.
// The worktree is assessed again first, GitHub included. It releases the lock of an agent
// session that has ended, removes the worktree without --force, and deletes its own
// worktree-<name> branch while that still points at the commit checked.
func (s *Service) CleanUp(ctx context.Context, repoPath, path string) (RemoveResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	g, root, _, err := s.prepare(ctx)
	if err != nil {
		return RemoveResult{}, err
	}

	if !s.underRoot(repoPath, root) {
		return RemoveResult{}, fmt.Errorf("%w: %s", ErrOutsideCloneRoot, s.display(repoPath))
	}

	clone, err := localrepos.Inspect(ctx, repoPath, s.run)
	if err != nil {
		return RemoveResult{}, err
	}

	snap, err := s.assessClones(ctx, g, root, []localrepos.LocalRepo{clone})
	if err != nil {
		return RemoveResult{}, err
	}

	wt, ok := findWorktree(snap, path)
	if err = s.cleanUpRefusal(wt, ok, path, snap.LinkError); err != nil {
		return RemoveResult{}, err
	}

	res := s.cleanUpRow(ctx, g, repoPath, wt, false)
	if !res.Removed {
		return res, fmt.Errorf("%w: %s: %s", ErrCleanUpFailed, wt.DisplayPath, res.Reason)
	}

	return res, nil
}

// CleanUpFinished cleans up every finished worktree under the clone root, one at a time,
// and reports each outcome. Each is checked again just before it is removed.
func (s *Service) CleanUpFinished(ctx context.Context) ([]RemoveResult, error) {
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

	results := make([]RemoveResult, 0, snap.FinishedCount)

	for repo, wt := range rows(snap.Repos) {
		if !wt.CanCleanUp {
			continue
		}

		if err = ctx.Err(); err != nil {
			return results, err
		}

		results = append(results, s.cleanUpRow(ctx, g, repo.Path, *wt, false))
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

// cleanUpRow removes one worktree assessed as finished, records it in the history, and
// reports the outcome; automatic tells a sweep from the user. Each step checks git again:
// the lock must be the one assessed and its session still ended, the worktree must still
// have the commit assessed checked out, git refuses one with uncommitted changes, and the
// branch is deleted only while it still points at that commit and no worktree has it
// checked out.
func (s *Service) cleanUpRow(ctx context.Context, g git, repoPath string, wt Worktree, automatic bool) RemoveResult {
	res := RemoveResult{RepoPath: repoPath, Path: wt.Path, DisplayPath: wt.DisplayPath, Item: itemName(wt.Item)}

	if err := s.removeFinished(ctx, g, repoPath, wt); err != nil {
		res.Reason = reasonOf(err)

		return res
	}

	res.Removed = true

	s.forgetLaunch(ctx, wt.Path)

	if wt.OwnBranch {
		if err := s.deleteOwnBranch(ctx, g, repoPath, wt.Branch, wt.Head); err != nil {
			res.Reason = "kept the branch " + wt.Branch + ": " + reasonOf(err)
		} else {
			res.DeletedBranch = wt.Branch
		}
	}

	s.recordCleanup(ctx, repoPath, wt, res.DeletedBranch, automatic)

	return res
}

// removeFinished releases a finished worktree's stale lock, if it has one, and removes it
// without --force while it still has the commit assessed checked out.
func (s *Service) removeFinished(ctx context.Context, g git, repoPath string, wt Worktree) error {
	if wt.Locked {
		if err := s.unlockEntry(ctx, g, repoPath, wt.Path, wt.LockReason); err != nil {
			return err
		}
	}

	_, e, err := s.removable(ctx, repoPath, wt.Path, wt.Head)
	if err != nil {
		return err
	}

	return s.removeEntry(ctx, g, repoPath, e, false)
}

// deleteOwnBranch deletes the agent's branch for a removed worktree, refusing while any
// worktree has it checked out. git deletes it only while it still points at head.
func (s *Service) deleteOwnBranch(ctx context.Context, g git, repoPath, branch, head string) error {
	if !isCommitID(head) {
		return fmt.Errorf("%w: no commit to compare the branch with", ErrChanged)
	}

	entries, stderr, err := g.list(ctx, repoPath)
	if err != nil {
		return fmt.Errorf("list the worktrees of %s: %w", s.display(repoPath), gitError(err, stderr))
	}

	for _, e := range entries {
		if e.Branch == branch {
			return ErrBranchInUse
		}
	}

	if stderr, err = g.deleteBranch(ctx, repoPath, branch, head); err != nil {
		return gitError(err, stderr)
	}

	return nil
}

// forgetLaunch removes the launch record of a removed worktree. A record left behind is
// pruned by the next sweep, so a failure is only logged.
func (s *Service) forgetLaunch(ctx context.Context, path string) {
	for _, l := range s.launchRecords(ctx) {
		if launchKey(l.ClonePath, l.Worktree) != resolvedPath(path) {
			continue
		}

		if err := s.launches.Forget(ctx, l.ClonePath, l.Worktree); err != nil {
			s.logger.WarnContext(ctx, "could not forget a worktree launch", "error", err)
		}
	}
}

// findWorktree returns the snapshot's row for path.
func findWorktree(snap Snapshot, path string) (Worktree, bool) {
	for _, wt := range rows(snap.Repos) {
		if samePath(wt.Path, path) {
			return *wt, true
		}
	}

	return Worktree{}, false
}

// notFinishedReason says why CleanUp refuses a worktree that is not finished.
func notFinishedReason(wt Worktree) string {
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
