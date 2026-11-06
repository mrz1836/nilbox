package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/launchlog"
	"github.com/mrz1836/nilbox/internal/localrepos"
)

// DefaultSweepInterval is how often a Sweeper sweeps. A sweep runs git only in clones
// that have agent worktrees, and asks GitHub once, for one point, only when there are
// any.
const DefaultSweepInterval = 5 * time.Minute

// Sweep checks the worktrees the agent made for pull requests and issues and, when
// automatic cleanup is on, cleans up each finished one whose pull request merged or whose
// issue closed (see autoCleanable for which count). It never lists every clone: it looks
// only in clones with an agent worktree directory or a recorded launch, so it is cheap to
// run on a timer. It also drops the launch records of worktrees that are gone. The report
// is kept for LastSweep, and the notifier is told when a worktree was removed or the
// number left for the user changed.
func (s *Service) Sweep(ctx context.Context) SweepReport {
	s.act.Lock()
	report := s.sweep(ctx)
	s.act.Unlock()

	report.At = s.clock.Now().UTC().Format(time.RFC3339)

	s.mu.Lock()
	changed := len(report.Cleaned) > 0 || report.Leftover != s.last.Leftover || report.Error != s.last.Error
	report.Seq = s.last.Seq + 1
	s.last = report
	s.mu.Unlock()

	if changed && s.notifier != nil {
		s.notifier.WorktreesUpdated()
	}

	return report
}

// LastSweep returns the latest sweep's report; its Seq is 0 before the first sweep.
func (s *Service) LastSweep() SweepReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.last
}

// safeSweep runs one sweep, logging a failure or a panic.
func (s *Service) safeSweep(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.ErrorContext(ctx, "worktree sweep panicked", "panic", r)
		}
	}()

	if report := s.Sweep(ctx); report.Error != "" && ctx.Err() == nil {
		s.logger.WarnContext(ctx, "worktree sweep failed", "error", report.Error)
	}
}

// sweep does the work of Sweep without the bookkeeping.
func (s *Service) sweep(ctx context.Context) SweepReport {
	report := SweepReport{Cleaned: []RemoveResult{}, Failed: []RemoveResult{}}

	g, root, prefs, err := s.prepare(ctx)
	if err != nil {
		report.Error = err.Error()

		return report
	}

	report.Enabled = prefs.WorktreeAutoCleanup
	launches := s.launchRecords(ctx)

	clones := s.sweepClones(ctx, root, launches)
	if len(clones) == 0 {
		s.pruneLaunches(ctx, root, launches, Snapshot{})

		return report
	}

	snap, err := s.assessClones(ctx, g, root, clones)
	if err != nil {
		report.Error = err.Error()

		return report
	}

	s.pruneLaunches(ctx, root, launches, snap)
	report.Error = snap.LinkError

	for repo, wt := range rows(snap.Repos) {
		s.sweepRow(ctx, g, repo.Path, *wt, &report)
	}

	return report
}

// sweepRow cleans up one worktree when the sweep may, and counts it when it is left for
// the user.
func (s *Service) sweepRow(ctx context.Context, g git, repoPath string, wt Worktree, report *SweepReport) {
	if report.Enabled && autoCleanable(wt) && ctx.Err() == nil {
		res := s.cleanUpRow(ctx, g, repoPath, wt)
		if res.Removed {
			report.Cleaned = append(report.Cleaned, res)

			return
		}

		report.Failed = append(report.Failed, res)
	}

	if wt.CanCleanUp || wt.Stage == StageNeedsReview {
		report.Leftover++
	}
}

// sweepClones lists the clones worth checking: those directly under root that hold an
// agent worktree directory, found without running git, and those with a recorded launch.
// A directory that is not a git clone is skipped.
func (s *Service) sweepClones(ctx context.Context, root string, launches []launchlog.Launch) []localrepos.LocalRepo {
	dirs := agentCloneDirs(root)

	for _, l := range launches {
		if s.underRoot(l.ClonePath, root) && !slices.ContainsFunc(dirs, func(d string) bool { return samePath(d, l.ClonePath) }) {
			dirs = append(dirs, filepath.Clean(l.ClonePath))
		}
	}

	slices.Sort(dirs)

	clones := make([]localrepos.LocalRepo, 0, len(dirs))

	for _, dir := range dirs {
		if clone, err := localrepos.Inspect(ctx, dir, s.run); err == nil {
			clones = append(clones, clone)
		}
	}

	return clones
}

// agentCloneDirs lists the directories directly under root, not hidden, that hold an
// agent worktree directory.
func agentCloneDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var dirs []string

	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && hasAgentWorktrees(dir) {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

// hasAgentWorktrees reports a .claude/worktrees directory with at least one entry.
func hasAgentWorktrees(dir string) bool {
	f, err := os.Open(filepath.Join(dir, ".claude", "worktrees")) //nolint:gosec // a fixed subdirectory of a clone under the clone root
	if err != nil {
		return false
	}

	defer func() { _ = f.Close() }()

	names, err := f.Readdirnames(1)

	return err == nil && len(names) > 0
}

// pruneLaunches forgets the launch records whose worktree is gone: its clone is no
// longer a directory, or was listed without the worktree. A clone that could not be
// listed keeps its records.
func (s *Service) pruneLaunches(ctx context.Context, root string, launches []launchlog.Launch, snap Snapshot) {
	if s.launches == nil {
		return
	}

	present, listed := listedPaths(snap)

	for _, l := range launches {
		if present[launchKey(l.ClonePath, l.Worktree)] || !s.launchGone(l, root, listed) {
			continue
		}

		if err := s.launches.Forget(ctx, l.ClonePath, l.Worktree); err != nil {
			s.logger.WarnContext(ctx, "could not forget a worktree launch", "error", err)
		}
	}
}

// listedPaths returns the resolved paths of a snapshot's worktrees, and of its
// repositories that were listed without an error.
func listedPaths(snap Snapshot) (worktrees, repos map[string]bool) {
	worktrees, repos = make(map[string]bool), make(map[string]bool)

	for _, repo := range snap.Repos {
		if repo.Error == "" {
			repos[resolvedPath(repo.Path)] = true
		}
	}

	for _, wt := range rows(snap.Repos) {
		worktrees[resolvedPath(wt.Path)] = true
	}

	return worktrees, repos
}

// launchGone reports a recorded launch whose worktree is certainly gone.
func (s *Service) launchGone(l launchlog.Launch, root string, listed map[string]bool) bool {
	info, err := os.Stat(l.ClonePath)

	switch {
	case err != nil && os.IsNotExist(err):
		return true
	case err != nil || !info.IsDir() || !s.underRoot(l.ClonePath, root):
		return false
	default:
		_, statErr := os.Stat(filepath.Join(l.ClonePath, ".claude", "worktrees", l.Worktree))

		return listed[resolvedPath(l.ClonePath)] || os.IsNotExist(statErr)
	}
}

// Sweeper runs a Service's sweeps on a timer. It is kept apart from Service, every
// exported method of which the window binds.
type Sweeper struct {
	svc *Service
}

// NewSweeper returns a Sweeper over svc.
func NewSweeper(svc *Service) Sweeper {
	return Sweeper{svc: svc}
}

// Run sweeps now and then every interval until ctx is canceled. A panicking sweep is
// logged and the next one still runs.
func (sw Sweeper) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		sw.svc.safeSweep(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
