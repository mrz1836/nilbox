package worktrees

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/cleanuplog"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/launchlog"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/settings"
)

// listConcurrency bounds how many repositories List reads at once.
const listConcurrency = 8

// Short reasons for git's removal refusals.
const (
	reasonDirty  = "has uncommitted changes"
	reasonLocked = "is locked"
)

var (
	// ErrMainWorktree is returned for the main worktree, which is never removed.
	ErrMainWorktree = errors.New("the main worktree cannot be removed")

	// ErrLocked is returned for a locked worktree, which is never removed: a running
	// session usually holds the lock.
	ErrLocked = errors.New("the worktree is locked, usually by a running session; unlock it with git worktree unlock first")

	// ErrNotLocked is returned when unlocking a worktree that has no lock.
	ErrNotLocked = errors.New("the worktree is not locked")

	// ErrHandLock is returned for a lock that no agent session wrote; only the user who
	// set it should release it.
	ErrHandLock = errors.New("the worktree was locked by hand, not by an agent session; unlock it yourself with git worktree unlock")

	// ErrSessionRunning is returned for an agent lock whose session may still be running.
	ErrSessionRunning = errors.New("the agent session that locked the worktree may still be running")

	// ErrChanged is returned when a worktree's commit or lock changed since it was checked.
	ErrChanged = errors.New("the worktree changed since it was checked; refresh and try again")

	// ErrNotFinished is returned by CleanUp for a worktree that is not finished.
	ErrNotFinished = errors.New("the worktree is not finished")

	// ErrCleanUpFailed is returned by CleanUp when a step of the removal was refused.
	ErrCleanUpFailed = errors.New("the worktree could not be cleaned up")

	// ErrBranchInUse is returned when a branch to delete is checked out in a worktree.
	ErrBranchInUse = errors.New("the branch is checked out in a worktree")

	// ErrGitHubUnavailable is returned when GitHub could not say which worktrees are
	// finished.
	ErrGitHubUnavailable = errors.New("GitHub could not be asked which worktrees are finished")

	// ErrNotAWorktree is returned for a path that is not a linked worktree of the
	// repository.
	ErrNotAWorktree = errors.New("not a linked worktree of this repository")

	// ErrNotDirty is returned when force removal is asked for a worktree without
	// uncommitted changes.
	ErrNotDirty = errors.New("the worktree has no uncommitted changes; use Remove instead")

	// ErrPrunable is returned when force removal is asked for a worktree whose directory
	// is gone.
	ErrPrunable = errors.New("the worktree's directory is gone; prune the repository instead")

	// ErrOutsideCloneRoot is returned for a repository that is not directly under the
	// clone root.
	ErrOutsideCloneRoot = errors.New("not a repository under the clone root")

	// ErrRemovalRefused is matched by every RefusedError.
	ErrRemovalRefused = errors.New("git refused to remove the worktree")

	// errGit marks an error that carries git's own message.
	errGit = errors.New("git")
)

// RefusedError is git's refusal to remove a worktree, with a short reason such as "has
// uncommitted changes". It matches ErrRemovalRefused.
type RefusedError struct {
	// DisplayPath is the worktree's path relative to home with ~.
	DisplayPath string

	// Reason is why git refused.
	Reason string
}

// Error describes the refusal.
func (e *RefusedError) Error() string {
	return fmt.Sprintf("git refused to remove %s: %s", e.DisplayPath, e.Reason)
}

// Is reports whether target is ErrRemovalRefused.
func (e *RefusedError) Is(target error) bool {
	return target == ErrRemovalRefused
}

// SettingsReader reads the user's preferences; the clone root comes from them.
// *settings.Service implements it.
type SettingsReader interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// Finder resolves a program name to an executable's absolute path. execpath.Finder
// implements it.
type Finder interface {
	Find(name string) (string, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// GitHub reads the current state of pull requests and issues. A GitHub CLI token and a
// github.Client implement it in the app.
type GitHub interface {
	ItemStates(ctx context.Context, refs []github.ItemRef) (map[github.ItemRef]github.ItemState, error)
}

// Launches reads and prunes the record of the worktrees the app launched sessions in.
// *launchlog.Store implements it.
type Launches interface {
	List(ctx context.Context) ([]launchlog.Launch, error)
	Forget(ctx context.Context, clonePath, worktree string) error
}

// History records the worktrees the app cleaned up. *cleanuplog.Store implements it.
type History interface {
	Record(ctx context.Context, c cleanuplog.Cleanup) error
	Recent(ctx context.Context, limit int) ([]cleanuplog.Cleanup, error)
	Clear(ctx context.Context) error
}

// Notifier is told when an automatic sweep removed a worktree or changed how many need
// the user's attention.
type Notifier interface {
	WorktreesUpdated()
}

// Config holds a Service's dependencies. Settings and Git are required; the rest are
// optional.
type Config struct {
	// Settings supplies the clone root.
	Settings SettingsReader

	// Git runs git argv; it also reads remotes during the clone-root scan.
	Git Runner

	// Finder resolves the git binary up front, so a missing git fails with a clear
	// error. Without it, Git resolves "git" itself.
	Finder Finder

	// Home is the user's home directory; it expands a ~ clone root and shortens paths.
	Home string

	// Clock stamps snapshots. It defaults to the system clock.
	Clock Clock

	// GitHub reads the state of the items worktrees are linked to. Without it no
	// worktree is linked, so none is ever finished.
	GitHub GitHub

	// Launches holds the worktrees the app launched sessions in.
	Launches Launches

	// History records each cleanup, for the view's list of recent ones.
	History History

	// Notifier is told when a sweep changed something.
	Notifier Notifier

	// Logger receives sweep failures. It defaults to slog.Default().
	Logger *slog.Logger
}

// Service lists and removes worktrees of the clones under the clone root.
type Service struct {
	settings SettingsReader
	run      Runner
	finder   Finder
	home     string
	realHome string
	clock    Clock
	github   GitHub
	launches Launches
	history  History
	notifier Notifier
	logger   *slog.Logger
	procs    processTable

	// zone is the machine's time zone, which an agent lock's start may be written in.
	zone *time.Location

	// act serializes every change, so a sweep and a click never work on the same
	// worktree at once.
	act sync.Mutex

	// mu guards last.
	mu   sync.Mutex
	last SweepReport
}

// NewService returns a worktrees Service over cfg.
func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}

	realHome := cfg.Home
	if cfg.Home != "" {
		realHome = resolvedPath(cfg.Home)
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		settings: cfg.Settings,
		run:      cfg.Git,
		finder:   cfg.Finder,
		home:     cfg.Home,
		realHome: realHome,
		clock:    clock,
		github:   cfg.GitHub,
		launches: cfg.Launches,
		history:  cfg.History,
		notifier: cfg.Notifier,
		logger:   logger,
		procs:    osProcesses{},
		zone:     time.Local, //nolint:gosmopolitan // the agent writes a session's start in the machine's zone or in UTC
	}
}

// List scans the clone root and returns every repository that has linked worktrees,
// with the status of each worktree and the pull request or issue it was made for. A
// repository that git cannot read is listed with an error rather than hiding the rest. A
// clone that is itself a linked worktree of another clone is folded into that repository.
func (s *Service) List(ctx context.Context) (Snapshot, error) {
	g, root, _, err := s.prepare(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	clones, err := localrepos.Scan(ctx, root, s.run)
	if err != nil {
		return Snapshot{}, err
	}

	return s.assessClones(ctx, g, root, clones)
}

// Remove removes one linked worktree without --force. The repository must be a clone
// under the clone root and path one of its linked worktrees; the main worktree and
// locked worktrees are refused before git runs, and git itself refuses a worktree with
// uncommitted changes, which comes back as a RefusedError.
func (s *Service) Remove(ctx context.Context, repoPath, path string) error {
	s.act.Lock()
	defer s.act.Unlock()

	return s.remove(ctx, repoPath, path)
}

// ForceRemove removes one unlocked linked worktree that has uncommitted changes,
// discarding them, with a single --force. Locked worktrees are refused even here, so a
// running session's worktree is never pulled out from under it.
func (s *Service) ForceRemove(ctx context.Context, repoPath, path string) error {
	s.act.Lock()
	defer s.act.Unlock()

	g, e, err := s.removable(ctx, repoPath, path, "")
	if err != nil {
		return err
	}

	if e.Prunable {
		return ErrPrunable
	}

	count, stderr, err := g.dirtyCount(ctx, e.Path)
	if err != nil {
		return fmt.Errorf("read the status of %s: %w", s.display(e.Path), gitError(err, stderr))
	}

	if count == 0 {
		return ErrNotDirty
	}

	return s.removeEntry(ctx, g, repoPath, e, true)
}

// RemoveAllClean removes every clean, unlocked, linked worktree whose directory still
// exists, one at a time, and reports each outcome. Each removal is re-checked against
// the repository first, and git refuses any worktree that changed in the meantime.
func (s *Service) RemoveAllClean(ctx context.Context) ([]RemoveResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	snap, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	targets := cleanTargets(snap)
	results := make([]RemoveResult, 0, len(targets))

	for _, target := range targets {
		if err = ctx.Err(); err != nil {
			return results, err
		}

		if err = s.remove(ctx, target.RepoPath, target.Path); err != nil {
			target.Reason = reasonOf(err)
		} else {
			target.Removed = true
		}

		results = append(results, target)
	}

	return results, nil
}

// Prune clears the repository's entries for worktrees whose directories are gone. It
// never touches a worktree that still exists or any branch.
func (s *Service) Prune(ctx context.Context, repoPath string) error {
	s.act.Lock()
	defer s.act.Unlock()

	g, err := s.checkedGit(ctx, repoPath)
	if err != nil {
		return err
	}

	if stderr, pruneErr := g.prune(ctx, repoPath); pruneErr != nil {
		return fmt.Errorf("prune %s: %w", s.display(repoPath), gitError(pruneErr, stderr))
	}

	return nil
}

// remove is Remove without taking the action lock.
func (s *Service) remove(ctx context.Context, repoPath, path string) error {
	g, e, err := s.removable(ctx, repoPath, path, "")
	if err != nil {
		return err
	}

	return s.removeEntry(ctx, g, repoPath, e, false)
}

// assessClones lists the given clones' worktrees, links them to their items, and works
// out each one's stage.
func (s *Service) assessClones(ctx context.Context, g git, root string, clones []localrepos.LocalRepo) (Snapshot, error) {
	listed := make([]listedRepo, len(clones))
	remotes := make(map[string]map[string]string, len(clones))

	eg, ectx := errgroup.WithContext(ctx)
	eg.SetLimit(listConcurrency)

	for i, clone := range clones {
		remotes[clone.Path] = clone.Remotes

		eg.Go(func() error {
			repo, listErr := s.listRepo(ectx, g, clone.Path)
			listed[i] = repo

			return listErr
		})
	}

	if err := eg.Wait(); err != nil {
		return Snapshot{}, err
	}

	snap := s.snapshot(root, listed)
	snap.LinkError = s.link(ctx, snap.Repos, remotes, s.launchRecords(ctx))
	s.tally(&snap)

	return snap, nil
}

// launchRecords reads the launch records. Without them worktrees are still linked by
// name, so a failure is only logged.
func (s *Service) launchRecords(ctx context.Context) []launchlog.Launch {
	if s.launches == nil {
		return nil
	}

	records, err := s.launches.List(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the worktree launches", "error", err)
	}

	return records
}

// tally works out each worktree's stage, counts what the bulk actions would remove, and
// counts the linked worktrees.
func (s *Service) tally(snap *Snapshot) {
	snap.CleanCount, snap.FinishedCount, snap.ReviewCount, snap.WorktreeCount = 0, 0, 0, 0

	for _, wt := range rows(snap.Repos) {
		assess(wt)

		if !wt.Main {
			snap.WorktreeCount++
		}

		if wt.CanRemove {
			snap.CleanCount++
		}

		if wt.CanCleanUp {
			snap.FinishedCount++
		}

		if wt.Stage == StageNeedsReview {
			snap.ReviewCount++
		}
	}
}

// prepare resolves git, the preferences, and the clone root.
func (s *Service) prepare(ctx context.Context) (git, string, settings.Settings, error) {
	g, err := s.git()
	if err != nil {
		return git{}, "", settings.Settings{}, err
	}

	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return git{}, "", settings.Settings{}, err
	}

	root, err := localrepos.ExpandRoot(prefs.CloneRoot, s.home)
	if err != nil {
		return git{}, "", settings.Settings{}, err
	}

	return g, root, prefs, nil
}

// git returns the allowlisted git runner, resolving the binary when a Finder is set.
func (s *Service) git() (git, error) {
	if s.finder == nil {
		return git{run: s.run, bin: gitBin}, nil
	}

	bin, err := s.finder.Find(gitBin)
	if err != nil {
		return git{}, fmt.Errorf("find git: %w", err)
	}

	return git{run: s.run, bin: bin}, nil
}

// checkedGit resolves git and checks that repoPath is a clone directly under the clone
// root, as List reports them.
func (s *Service) checkedGit(ctx context.Context, repoPath string) (git, error) {
	g, root, _, err := s.prepare(ctx)
	if err != nil {
		return git{}, err
	}

	if !s.underRoot(repoPath, root) {
		return git{}, fmt.Errorf("%w: %s", ErrOutsideCloneRoot, s.display(repoPath))
	}

	return g, nil
}

// underRoot reports an absolute, non-hidden directory directly under root.
func (s *Service) underRoot(repoPath, root string) bool {
	clean := filepath.Clean(repoPath)

	return filepath.IsAbs(clean) && !strings.HasPrefix(filepath.Base(clean), ".") &&
		resolvedPath(filepath.Dir(clean)) == resolvedPath(root)
}

// entryOf re-lists the repository and returns path's entry when it is a linked worktree
// of it.
func (s *Service) entryOf(ctx context.Context, g git, repoPath, path string) (Entry, error) {
	entries, stderr, err := g.list(ctx, repoPath)
	if err != nil {
		return Entry{}, fmt.Errorf("list the worktrees of %s: %w", s.display(repoPath), gitError(err, stderr))
	}

	for _, e := range entries {
		if !samePath(e.Path, path) {
			continue
		}

		if e.Main || e.Bare {
			return Entry{}, ErrMainWorktree
		}

		return e, nil
	}

	return Entry{}, fmt.Errorf("%w: %s", ErrNotAWorktree, s.display(path))
}

// removable re-lists the repository and returns path's entry when it is a linked,
// unlocked worktree of it. When head is not "", the worktree must still have that commit
// checked out.
func (s *Service) removable(ctx context.Context, repoPath, path, head string) (git, Entry, error) {
	g, err := s.checkedGit(ctx, repoPath)
	if err != nil {
		return git{}, Entry{}, err
	}

	e, err := s.entryOf(ctx, g, repoPath, path)

	switch {
	case err != nil:
		return git{}, Entry{}, err
	case e.Locked:
		return git{}, Entry{}, ErrLocked
	case head != "" && e.HEAD != head:
		return git{}, Entry{}, ErrChanged
	default:
		return g, e, nil
	}
}

// removeEntry runs `git worktree remove` for a checked entry and turns a refusal into a
// RefusedError.
func (s *Service) removeEntry(ctx context.Context, g git, repoPath string, e Entry, force bool) error {
	stderr, err := g.remove(ctx, repoPath, e.Path, force)
	if err == nil {
		return nil
	}

	if !exitCode(ctx, err) {
		return fmt.Errorf("remove %s: %w", s.display(e.Path), err)
	}

	return &RefusedError{DisplayPath: s.display(e.Path), Reason: refusalReason(stderr)}
}

// listRepo lists one clone's worktrees. A git refusal (for example a broken clone) is
// recorded on the repository; failing to run git at all is an error.
func (s *Service) listRepo(ctx context.Context, g git, dir string) (listedRepo, error) {
	listed := listedRepo{
		repo: Repo{Path: dir, DisplayPath: s.display(dir), Name: filepath.Base(dir), Worktrees: []Worktree{}},
		key:  resolvedPath(dir),
	}

	entries, stderr, err := g.list(ctx, dir)

	switch {
	case err == nil:
	case exitCode(ctx, err):
		listed.repo.Error = gitError(err, stderr).Error()

		return listed, nil
	default:
		return listedRepo{}, fmt.Errorf("list the worktrees of %s: %w", listed.repo.DisplayPath, err)
	}

	if len(entries) > 0 && entries[0].Main {
		listed.key = resolvedPath(entries[0].Path)
	}

	claudeDir := filepath.Join(listed.key, ".claude", "worktrees")

	for _, e := range entries {
		if e.Bare {
			continue
		}

		wt, rowErr := s.row(ctx, g, e, claudeDir)
		if rowErr != nil {
			return listedRepo{}, rowErr
		}

		listed.add(wt)
	}

	return listed, nil
}

// row builds a worktree's view row, reading its status and last commit unless its
// directory is gone.
func (s *Service) row(ctx context.Context, g git, e Entry, claudeDir string) (Worktree, error) {
	wt := Worktree{
		Path:           e.Path,
		DisplayPath:    s.display(e.Path),
		Branch:         e.Branch,
		Detached:       e.Detached,
		Main:           e.Main,
		Locked:         e.Locked,
		LockReason:     e.LockReason,
		Prunable:       e.Prunable,
		PrunableReason: e.PrunableReason,
		ClaudeCreated:  within(claudeDir, resolvedPath(e.Path)),
		Head:           e.HEAD,
		Unpushed:       -1,
	}

	s.judgeLock(&wt)
	wt.OwnBranch = wt.ClaudeCreated && !e.Detached && e.Branch == ownBranchPrefix+filepath.Base(e.Path)

	if !e.Prunable {
		if err := s.readState(ctx, g, &wt); err != nil {
			return Worktree{}, err
		}
	}

	linked := !e.Main && !e.Locked && !e.Prunable
	wt.CanRemove = linked && !wt.Dirty
	wt.CanForceRemove = linked && wt.Dirty

	return wt, nil
}

// judgeLock fills in who holds a locked worktree and whether that session has ended.
func (s *Service) judgeLock(wt *Worktree) {
	if !wt.Locked {
		return
	}

	_, wt.AgentLock = parseAgentLock(wt.LockReason)
	wt.StaleLock = endedAgentLock(wt.LockReason, s.procs, s.zone)
	wt.CanUnlock = wt.StaleLock && !wt.Main
}

// readState fills in a worktree's uncommitted-file count and last commit time. A git
// refusal leaves the field unknown (and records why, for the status); failing to run git
// at all is an error.
func (s *Service) readState(ctx context.Context, g git, wt *Worktree) error {
	count, stderr, err := g.dirtyCount(ctx, wt.Path)

	switch {
	case err == nil:
		wt.Dirty, wt.DirtyCount = count > 0, count
	case exitCode(ctx, err):
		wt.Error = "could not read the status: " + gitError(err, stderr).Error()
	default:
		return fmt.Errorf("read the status of %s: %w", wt.DisplayPath, err)
	}

	unix, err := g.lastCommit(ctx, wt.Path)

	switch {
	case err == nil:
		wt.LastCommitUnix = unix
	case exitCode(ctx, err) || errors.Is(err, errParseCommitTime):
		// A repository without commits has no commit time to show.
	default:
		return fmt.Errorf("read the last commit of %s: %w", wt.DisplayPath, err)
	}

	if wt.Main {
		return nil
	}

	unpushed, _, err := g.unpushed(ctx, wt.Path)

	switch {
	case err == nil:
		wt.Unpushed = unpushed
	case exitCode(ctx, err) || errors.Is(err, errParseCount):
		// Unknown, which never counts as on GitHub.
	default:
		return fmt.Errorf("count the unpushed commits of %s: %w", wt.DisplayPath, err)
	}

	return nil
}

// snapshot folds clones that share a main worktree and keeps the repositories worth
// showing.
func (s *Service) snapshot(root string, listed []listedRepo) Snapshot {
	snap := Snapshot{
		Root:     s.display(root),
		Repos:    []Repo{},
		ListedAt: s.clock.Now().UTC().Format(time.RFC3339),
	}

	seen := make(map[string]bool, len(listed))

	for _, l := range listed {
		if seen[l.key] || (!l.linked && l.repo.Error == "") {
			continue
		}

		seen[l.key] = true
		snap.Repos = append(snap.Repos, l.repo)
	}

	return snap
}

// display shows path relative to home with ~, also matching home's resolved form (git
// reports resolved paths, and home may sit behind a symlink).
func (s *Service) display(path string) string {
	if shown := localrepos.TildePath(path, s.home); shown != path || s.realHome == s.home {
		return shown
	}

	return localrepos.TildePath(path, s.realHome)
}

// listedRepo is one clone's listing before clones that share a main worktree are folded.
type listedRepo struct {
	repo Repo

	// key identifies the repository: its main worktree's resolved path.
	key string

	// linked reports at least one linked worktree.
	linked bool
}

// add appends a worktree row and updates the repository's flags.
func (l *listedRepo) add(wt Worktree) {
	l.repo.Worktrees = append(l.repo.Worktrees, wt)

	if !wt.Main {
		l.linked = true
	}

	if wt.Prunable {
		l.repo.HasStale = true
	}
}

// rows yields every worktree of repos with its repository, in order, for reading or
// changing in place.
func rows(repos []Repo) iter.Seq2[*Repo, *Worktree] {
	return func(yield func(*Repo, *Worktree) bool) {
		for i := range repos {
			for j := range repos[i].Worktrees {
				if !yield(&repos[i], &repos[i].Worktrees[j]) {
					return
				}
			}
		}
	}
}

// cleanTargets lists the worktrees "Remove all clean" removes, in snapshot order.
func cleanTargets(snap Snapshot) []RemoveResult {
	targets := make([]RemoveResult, 0, snap.CleanCount)

	for _, repo := range snap.Repos {
		for _, wt := range repo.Worktrees {
			if wt.CanRemove {
				targets = append(targets, RemoveResult{RepoPath: repo.Path, Path: wt.Path, DisplayPath: wt.DisplayPath})
			}
		}
	}

	return targets
}

// gitError turns a git failure into an error carrying git's own message when it gave one.
func gitError(err error, stderr []byte) error {
	if line := firstLine(stderr); line != "" {
		return fmt.Errorf("%w: %s", errGit, line)
	}

	return err
}

// refusalReason shortens git's refusal to remove a worktree.
func refusalReason(stderr []byte) string {
	msg := string(stderr)

	switch {
	case strings.Contains(msg, "modified or untracked files"):
		return reasonDirty
	case strings.Contains(msg, "locked working tree"):
		return reasonLocked
	}

	if line := firstLine(stderr); line != "" {
		return line
	}

	return "git exited with an error"
}

// reasonOf is the reason shown for a failed removal in a bulk result.
func reasonOf(err error) string {
	var refused *RefusedError
	if errors.As(err, &refused) {
		return refused.Reason
	}

	return err.Error()
}

// resolvedPath returns path with symlinks resolved. A path that no longer exists, such as
// a deleted worktree, is resolved through its nearest existing parent, so it still matches
// the form git reports for it. A path with no existing parent is only cleaned.
func resolvedPath(path string) string {
	clean := filepath.Clean(path)

	for dir := clean; ; dir = filepath.Dir(dir) {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			rest, relErr := filepath.Rel(dir, clean)
			if relErr != nil {
				return clean
			}

			return filepath.Join(resolved, rest)
		}

		if filepath.Dir(dir) == dir {
			return clean
		}
	}
}

// samePath reports whether two paths name the same directory.
func samePath(a, b string) bool {
	return resolvedPath(a) == resolvedPath(b)
}

// within reports whether path is strictly inside dir.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)

	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// systemClock is the real clock.
type systemClock struct{}

// Now returns the current time.
func (systemClock) Now() time.Time { return time.Now() }
