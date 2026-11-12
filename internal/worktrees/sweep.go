package worktrees

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/periodic"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// DefaultSweepInterval is how often a Sweeper sweeps. A sweep runs git only in clones
// that have worktrees the app or Claude Code made, or records, and asks GitHub once, for
// one point, only when there are any.
const DefaultSweepInterval = 5 * time.Minute

// Sweep checks the worktrees made for pull requests and issues and, when automatic
// cleanup is on, cleans up each finished one whose pull request merged or whose issue
// closed (see autoCleanable for which count), and that no running process works in; when
// the processes cannot be listed, it cleans up none. It never lists every clone: it looks
// only in clones with worktrees the app or Claude Code made, or with records, so it is
// cheap to run on a timer. It also checks the records against git (see reconcile), and
// counts the linked worktrees of every clone from their git directories, again without
// running git. The report is kept for LastSweep, and an event.WorktreesUpdated is emitted when a
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
	err = s.sweepAgentWork(ctx, g, root, prefs.Branches, &report)

	// Counted last, so the worktrees the sweep removed are not.
	report.WorktreeCount = countWorktrees(root)

	return report, err
}

// sweepAgentWork checks the clones with worktrees the app or Claude Code made, or with
// records, cleans up what it may, and drops the records whose worktree is gone. It
// returns the error that stopped it before it could clean up anything, which it also
// words in the report.
func (s *Service) sweepAgentWork(ctx context.Context, g git, root string, rules branchrules.Rules,
	report *SweepReport,
) error {
	records := s.records(ctx)

	clones := s.sweepClones(ctx, root, records)
	if len(clones) == 0 {
		// Nothing to list: only the records whose clone is gone can be dropped.
		s.reconcile(ctx, nil, records)

		return nil
	}

	snap, err := s.assessClones(ctx, g, root, clones, rules, nil)
	if err != nil {
		report.Error = uierr.Message(err)

		return err
	}

	report.Error = snap.LinkError

	// A clone a run is using is still listed, so its worktrees are counted, but nothing in
	// it is removed until a later sweep.
	var skipped map[string]bool
	if report.Enabled && !snap.SessionsUnknown {
		skipped = s.skippedClones(ctx, snap.Repos)
	}

	for repo, wt := range rows(snap.Repos) {
		s.sweepRow(ctx, g, repo.Path, *wt, !snap.SessionsUnknown && !skipped[repo.Path], rules, report)
	}

	return nil
}

// sweepRow cleans up one worktree when the sweep may, and counts it when it is left for
// the user. mayRemove is false when the running processes couldn't be listed or a run is
// using the clone; the sweep removes nothing then. A run that takes the clone meanwhile
// skips the worktree rather than fail it: the next sweep tries again.
func (s *Service) sweepRow(ctx context.Context, g git, repoPath string, wt Worktree, mayRemove bool,
	rules branchrules.Rules, report *SweepReport,
) {
	if report.Enabled && mayRemove && autoCleanable(wt) && ctx.Err() == nil {
		res, err := s.cleanUpRow(ctx, g, repoPath, wt, true, defaultBranches(wt, rules))

		switch busy, isBusy := errors.AsType[*clonelock.BusyError](err); {
		case res.Removed:
			report.Cleaned = append(report.Cleaned, res)

			return
		case isBusy:
			s.logSkipped(ctx, busy.Holder)
		default:
			report.Failed = append(report.Failed, res)
		}
	}

	if wt.CanCleanUp || wt.Stage == StageNeedsReview {
		report.Leftover++
	}
}

// sweepClones lists the clones worth checking: those directly under root with worktrees
// the app or Claude Code made, found without running git, and those with a record. A
// directory that is not a git clone is skipped.
func (s *Service) sweepClones(ctx context.Context, root string, records []worklog.Record) []localrepos.LocalRepo {
	dirs := managedCloneDirs(root)

	for _, r := range records {
		if s.underRoot(r.ClonePath, root) && !slices.ContainsFunc(dirs, func(d string) bool { return paths.Same(d, r.ClonePath) }) {
			dirs = append(dirs, filepath.Clean(r.ClonePath))
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

// managedCloneDirs lists the clones directly under root (see localrepos.Clones) with
// worktrees the app or Claude Code made. A clone reached through a symlink is left out,
// so a clone the root also holds itself is never swept twice; its records still bring it
// in.
func managedCloneDirs(root string) []string {
	clones, err := localrepos.Clones(root)
	if err != nil {
		return nil
	}

	var dirs []string

	for dir := range clones {
		if info, statErr := os.Lstat(dir); statErr == nil && info.IsDir() && managedClone(dir) {
			dirs = append(dirs, dir)
		}
	}

	return dirs
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
		return paths.Resolved(dotGit)
	}

	gitDir, ok := readGitPointer(dotGit, dir, "gitdir: ")
	if !ok {
		return ""
	}

	if common, found := readGitPointer(filepath.Join(gitDir, "commondir"), gitDir, ""); found {
		return paths.Resolved(common)
	}

	return paths.Resolved(gitDir)
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
