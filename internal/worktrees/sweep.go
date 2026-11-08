package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/periodic"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// DefaultSweepInterval is how often a Sweeper sweeps. A sweep runs git only in clones
// that have agent worktrees, and asks GitHub once, for one point, only when there are
// any.
const DefaultSweepInterval = 5 * time.Minute

// Sweep checks the worktrees the agent made for pull requests and issues and, when
// automatic cleanup is on, cleans up each finished one whose pull request merged or whose
// issue closed (see autoCleanable for which count). It never lists every clone: it looks
// only in clones with an agent worktree directory or a recorded launch, so it is cheap to
// run on a timer. It also drops the launch records of worktrees that are gone, and counts
// the linked worktrees of every clone from their git directories, again without running
// git. The report is kept for LastSweep, and an event.WorktreesUpdated is emitted when a
// worktree was removed or either count changed.
func (s *Service) Sweep(ctx context.Context) SweepReport {
	report, _ := s.sweepAndKeep(ctx) // the report says what failed, as the user is told

	return report
}

// LastSweep returns the latest sweep's report; its Seq is 0 before the first sweep.
func (s *Service) LastSweep() SweepReport {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.last
}

// sweepAndKeep runs Sweep and also returns the error that stopped the sweep early, if
// any, which the report's Error words for the user.
func (s *Service) sweepAndKeep(ctx context.Context) (SweepReport, error) {
	report, err := s.sweepLocked(ctx)
	report.At = clock.Format(s.clock.Now())

	s.mu.Lock()
	changed := len(report.Cleaned) > 0 || report.Leftover != s.last.Leftover ||
		report.WorktreeCount != s.last.WorktreeCount || report.Error != s.last.Error
	report.Seq = s.last.Seq + 1
	s.last = report
	s.mu.Unlock()

	if changed {
		s.events.Emit(event.WorktreesUpdated{})
	}

	return report, err
}

// emptyReport is a report of a sweep that found nothing, with empty lists rather than nil
// ones, so the window always receives arrays.
func emptyReport() SweepReport {
	return SweepReport{Cleaned: []RemoveResult{}, Failed: []RemoveResult{}}
}

// sweepJob names the automatic sweeps in log entries.
const sweepJob = "worktree sweep"

// sweepError is a report's error that has no Go error behind it: GitHub could not be
// read, which the sweep goes on without.
type sweepError string

// Error returns the report's error.
func (e sweepError) Error() string { return string(e) }

// sweepOnce runs one sweep for a Sweeper, failing with the error that stopped it, or else
// with the report's.
func (s *Service) sweepOnce(ctx context.Context) error {
	report, err := s.sweepAndKeep(ctx)
	if err != nil {
		return err
	}

	if report.Error != "" {
		return sweepError(report.Error)
	}

	return nil
}

// sweepLocked runs sweep while holding act, which a panicking sweep releases too.
func (s *Service) sweepLocked(ctx context.Context) (SweepReport, error) {
	s.act.Lock()
	defer s.act.Unlock()

	return s.sweep(ctx)
}

// sweep does the work of Sweep without the bookkeeping. It returns the error that stopped
// it early, if any, which the report's Error words for the user.
func (s *Service) sweep(ctx context.Context) (SweepReport, error) {
	report := emptyReport()

	g, root, prefs, err := s.prepare(ctx)
	if err != nil {
		report.Error = uierr.Message(err)

		return report, err
	}

	report.Enabled = prefs.AutoCleanup
	err = s.sweepAgentWork(ctx, g, root, &report)

	// Counted last, so the worktrees the sweep removed are not.
	report.WorktreeCount = countWorktrees(root)

	return report, err
}

// sweepAgentWork checks the clones with agent worktrees or recorded launches, cleans up
// what it may, and forgets the launches whose worktree is gone. It returns the error that
// stopped it before it could clean up anything, which it also words in the report.
func (s *Service) sweepAgentWork(ctx context.Context, g git, root string, report *SweepReport) error {
	launches := s.launchRecords(ctx)

	clones := s.sweepClones(ctx, root, launches)
	if len(clones) == 0 {
		s.pruneLaunches(ctx, root, launches, Snapshot{})

		return nil
	}

	snap, err := s.assessClones(ctx, g, root, clones)
	if err != nil {
		report.Error = uierr.Message(err)

		return err
	}

	s.pruneLaunches(ctx, root, launches, snap)
	report.Error = snap.LinkError

	for repo, wt := range rows(snap.Repos) {
		s.sweepRow(ctx, g, repo.Path, *wt, report)
	}

	return nil
}

// sweepRow cleans up one worktree when the sweep may, and counts it when it is left for
// the user.
func (s *Service) sweepRow(ctx context.Context, g git, repoPath string, wt Worktree, report *SweepReport) {
	if report.Enabled && autoCleanable(wt) && ctx.Err() == nil {
		res := s.cleanUpRow(ctx, g, repoPath, wt, true)
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
func (s *Service) sweepClones(ctx context.Context, root string, launches []worklog.Launch) []localrepos.LocalRepo {
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

// agentCloneDirs lists the clones directly under root (see localrepos.Clones) that hold
// an agent worktree directory. A clone reached through a symlink is left out, so a
// clone the root also holds itself is never swept twice; its recorded launches still
// bring it in.
func agentCloneDirs(root string) []string {
	clones, err := localrepos.Clones(root)
	if err != nil {
		return nil
	}

	var dirs []string

	for dir := range clones {
		if info, statErr := os.Lstat(dir); statErr == nil && info.IsDir() && hasAgentWorktrees(dir) {
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

// countWorktrees counts the linked worktrees of the clones directly under root, not
// hidden, as a listing would, without running git. Git registers each linked worktree in
// its repository's common git directory, under worktrees/, and lists every entry there
// that holds a gitdir file, whether or not the worktree's own directory is still there;
// this counts the same entries. Clones of one repository (one a linked worktree of
// another, or one clone reached through a symlink) count its worktrees once.
func countWorktrees(root string) int {
	clones, err := localrepos.Clones(root)
	if err != nil {
		return 0
	}

	seen := map[string]bool{}
	count := 0

	for dir := range clones {
		common := commonDir(dir)
		if common == "" || seen[common] {
			continue
		}

		seen[common] = true
		count += registeredWorktrees(common)
	}

	return count
}

// registeredWorktrees counts the linked worktrees registered in a common git directory:
// the entries of its worktrees directory that hold a non-empty gitdir file.
func registeredWorktrees(common string) int {
	dir := filepath.Join(common, "worktrees")

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	count := 0

	for _, e := range entries {
		info, statErr := os.Stat(filepath.Join(dir, e.Name(), "gitdir"))
		if statErr == nil && info.Mode().IsRegular() && info.Size() > 0 {
			count++
		}
	}

	return count
}

// commonDir returns the resolved common git directory of the clone at dir, or "" when
// dir has no readable .git. A .git directory is that directory. A .git file (a linked
// worktree, or a clone whose git directory lives elsewhere) points at the git directory,
// and a commondir file there points on to the common one.
func commonDir(dir string) string {
	dotGit := filepath.Join(dir, ".git")

	info, err := os.Stat(dotGit)
	if err != nil {
		return ""
	}

	if info.IsDir() {
		return resolvedPath(dotGit)
	}

	gitDir, ok := readGitPointer(dotGit, dir, "gitdir: ")
	if !ok {
		return ""
	}

	if common, found := readGitPointer(filepath.Join(gitDir, "commondir"), gitDir, ""); found {
		return resolvedPath(common)
	}

	return resolvedPath(gitDir)
}

// readGitPointer reads the path a one-line git file holds after prefix. A relative path
// is taken from base, as git takes it.
func readGitPointer(file, base, prefix string) (string, bool) {
	data, err := os.ReadFile(file) //nolint:gosec // a git file of a clone under the clone root
	if err != nil {
		return "", false
	}

	path, ok := strings.CutPrefix(strings.TrimRight(string(data), "\r\n"), prefix)
	if !ok || path == "" {
		return "", false
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}

	return path, true
}

// pruneLaunches forgets the launch records whose worktree is gone: its clone is no
// longer a directory, or was listed without the worktree. A clone that could not be
// listed keeps its records.
func (s *Service) pruneLaunches(ctx context.Context, root string, launches []worklog.Launch, snap Snapshot) {
	if s.launches == nil {
		return
	}

	present, listed := listedPaths(snap)

	for _, l := range launches {
		if present[launchKey(l.ClonePath, l.Worktree)] || !s.launchGone(l, root, listed) {
			continue
		}

		if err := s.launches.ForgetLaunch(ctx, l.ClonePath, l.Worktree); err != nil {
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
func (s *Service) launchGone(l worklog.Launch, root string, listed map[string]bool) bool {
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
	svc      *Service
	interval time.Duration
}

// NewSweeper returns a Sweeper over svc that sweeps every interval.
func NewSweeper(svc *Service, interval time.Duration) Sweeper {
	return Sweeper{svc: svc, interval: interval}
}

// Run sweeps now and then every interval until ctx is canceled, and returns once the
// sweep in flight has. A failed sweep is logged, and a panicking one is recovered and
// logged; the next one still runs.
func (sw Sweeper) Run(ctx context.Context) {
	periodic.Run(ctx, sw.interval, sweepJob, sw.svc.sweepOnce, sw.svc.logger)
}
