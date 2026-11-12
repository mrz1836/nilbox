package worktrees

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/progress"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

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

	// ErrInUse is returned for a worktree a running process works in, which is never
	// removed (see InUseError).
	ErrInUse = errors.New("a session is running in the worktree")

	// ErrSessionsUnknown is returned when the automatic cleanup or a force removal cannot
	// tell whether a session runs in a worktree, so it leaves it.
	ErrSessionsUnknown = errors.New("can't tell whether a session is running in the worktree")

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

	// ErrBranchProtected is returned for a branch the user protects, or a remote's
	// default branch, which is never deleted.
	ErrBranchProtected = errors.New("the branch is protected")

	// ErrBranchGone is returned for a branch to delete that no longer exists.
	ErrBranchGone = errors.New("the branch is already gone")

	// ErrBranchMoved is returned for a branch to delete that moved since it was checked.
	ErrBranchMoved = errors.New("the branch moved since it was checked; check again")

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

	// ErrOutsideCloneRoot is returned for a repository that isn't a clone the repository
	// locations hold: directly inside the clone root or a folder location, or a clone
	// location itself. A location removed in Settings leaves its clones outside until it is
	// added again.
	ErrOutsideCloneRoot = errors.New("not a clone in your repository locations; check Settings → Repositories")

	// ErrRemovalRefused is matched by every RefusedError.
	ErrRemovalRefused = errors.New("git refused to remove the worktree")

	// errGit marks an error that carries git's own message.
	errGit = errors.New("git")
)

// InUseError is returned for a worktree running processes work in. It matches ErrInUse.
type InUseError struct {
	// Sessions are the processes, by PID.
	Sessions []Session
}

// Error names the processes, as in "a session is running in the worktree: codex (pid
// 4242)".
func (e *InUseError) Error() string {
	names := make([]string, 0, len(e.Sessions))
	for _, s := range e.Sessions {
		names = append(names, fmt.Sprintf("%s (pid %d)", s.name(), s.PID))
	}

	return ErrInUse.Error() + ": " + strings.Join(names, ", ")
}

// Is makes an InUseError match ErrInUse.
func (e *InUseError) Is(target error) bool {
	return target == ErrInUse
}

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

// SweepSettings is the part of the user's preferences the worktree service reads. Every
// field is a plain value.
type SweepSettings struct {
	// CloneRoot is the directory whose children are the clones, as entered, e.g.
	// "~/projects".
	CloneRoot string

	// CloneLocations are the user's other repository locations, as stored.
	CloneLocations []localrepos.Location

	// AutoCleanup lets the automatic sweep clean up a finished worktree once its pull
	// request merges or its issue closes.
	AutoCleanup bool

	// Branches are the user's rules for deleting branches: the automatic sweep follows
	// them, and nothing ever deletes a branch they protect.
	Branches branchrules.Rules
}

// SettingsReader reads the sweep settings as they are now, so every listing and sweep
// sees the user's latest choices. The app adapts the settings service to it.
type SettingsReader interface {
	Get(ctx context.Context) (SweepSettings, error)
}

// ItemState is what GitHub says about a pull request or issue as of now.
type ItemState struct {
	// PullRequest reports a pull request; false is an issue.
	PullRequest bool

	Title string
	URL   string

	// State is ItemOpen, ItemMerged, or ItemClosed; any other value reads as ItemOpen.
	State ItemStatus

	// ClosedAt is when the item was merged or closed, or zero while it is open.
	ClosedAt time.Time

	// HeadOID is a pull request's head commit SHA; "" for an issue.
	HeadOID string

	// HeadRef is a pull request's head branch; "" for an issue.
	HeadRef string
}

// GitHub reads the current state of pull requests and issues. The app adapts the GitHub
// client to it, with the GitHub CLI's token.
type GitHub interface {
	// ItemStates returns the state of each item in refs that GitHub knows, keyed by the
	// ref's Key.
	ItemStates(ctx context.Context, refs []item.Ref) (map[string]ItemState, error)

	// MergedPullRequests returns the merged pull requests whose head branch had each
	// name asked about, newest first, keyed by BranchKey. A name none had is left out.
	MergedPullRequests(ctx context.Context, queries []BranchQuery) (map[BranchKey][]MergedPR, error)
}

// BranchQuery asks which of a repository's branch names were the head of a merged pull
// request.
type BranchQuery struct {
	Repo     repo.Name
	Branches []string
}

// BranchKey names a repository's branch in the answer to MergedPullRequests: the
// repository's Key and the branch.
type BranchKey struct {
	Repo   string
	Branch string
}

// MergedPR is a merged pull request a branch was the head of.
type MergedPR struct {
	Number   int
	Title    string
	URL      string
	MergedAt time.Time

	// HeadOID is the commit its head branch was at, which GitHub keeps.
	HeadOID string
}

// Records reads, writes, and prunes the record of the worktrees the app made or found for
// pull requests and issues. *worklog.Store implements it.
type Records interface {
	Worktrees(ctx context.Context) ([]worklog.Record, error)
	Record(ctx context.Context, r worklog.Record) error
	Forget(ctx context.Context, clonePath, worktree string) error
}

// History records the worktrees the app cleaned up. *worklog.Store implements it.
type History interface {
	RecordCleanup(ctx context.Context, c worklog.Cleanup) error
	RecentCleanups(ctx context.Context, limit int) ([]worklog.Cleanup, error)
	ClearCleanups(ctx context.Context) error
}

// Config holds a Service's dependencies. Settings and Git are required; the rest are
// optional.
type Config struct {
	// Settings supplies the repository locations and whether the sweep cleans up on its own.
	Settings SettingsReader

	// Git runs git argv; it also reads remotes during the clone-root scan.
	Git execpath.Runner

	// Finder resolves the git binary up front, so a missing git fails with a clear
	// error. Without it, Git resolves "git" itself.
	Finder execpath.Finder

	// Guard works out what the commands worktree creation runs that reach a remote (its
	// fetches and its ls-remote) or write a worktree's files (git worktree add) run with:
	// gitread's guards, which refuse a clone whose own git config holds a setting outside
	// their allowlist, so none runs a program the clone's own config names. They run the
	// same git as every other command here, with the guard's environment, which holds their
	// settings, through GitWith. Core gives every Service one; without it, as in tests that
	// script git, they run as every other command here does.
	Guard Guard

	// GitWith runs git argv with the whole environment env, as a guarded command needs: the
	// same runner as Git but for the environment. It defaults to an execpath.Exec with env.
	GitWith func(env []string) execpath.Runner

	// Home is the user's home directory; it expands a ~ clone root and shortens paths.
	Home string

	// Clock stamps snapshots. It defaults to the system clock.
	Clock clock.Clock

	// GitHub reads the state of the items worktrees are linked to. Without it no
	// worktree is linked, so none is ever finished.
	GitHub GitHub

	// Records holds the worktrees the app made or found for pull requests and issues.
	Records Records

	// Procs lists the running processes, to tell the worktrees a session works in.
	// Without it no worktree counts as in use by a process; a lock Claude Code holds
	// still counts.
	Procs procs.Lister

	// History records each cleanup, for the view's list of recent ones.
	History History

	// Agents names the processes of the agent sessions the app follows, so the view says
	// "Claude" rather than the name of Claude Code's program. Optional.
	Agents AgentProcesses

	// Events hears an event.WorktreesUpdated when a sweep removed a worktree or changed
	// how many need the user's attention, and an event.WorktreesProgress as the window's
	// listing goes.
	Events event.Sink

	// Clones is the app's one set of clone locks, which every job that changes a clone
	// takes, so a worktree is never made or removed while a run is using its clone. It
	// defaults to a set of the Service's own.
	Clones *clonelock.Set

	// Logger receives sweep failures and, at Debug, why GitHub could not say which pull
	// requests and issues the worktrees belong to. Without it they are not logged.
	Logger *slog.Logger
}

// Guard works out what a git command that reaches the clone's remote, or writes a
// worktree's files, runs with, one in a clone or a worktree that could run one of the
// clone's programs, as a status or a removal, and the environment a read runs with:
// gitread's RemoteSetupFor, CheckoutSetup, LocalSetup, and ReadEnv, *gitread.Git.
type Guard interface {
	RemoteSetupFor(ctx context.Context, dir, remote string) (gitread.Setup, error)
	CheckoutSetup(ctx context.Context, dir string) (gitread.Setup, error)
	LocalSetup(ctx context.Context, dir string) (gitread.Local, error)
	ReadEnv() []string
}

// Service lists and removes worktrees of the clones in the repository locations.
type Service struct {
	settings SettingsReader
	run      execpath.Runner
	finder   execpath.Finder
	guard    Guard
	gitWith  func(env []string) execpath.Runner
	home     string
	realHome string
	clock    clock.Clock
	github   GitHub
	launches Records
	lister   procs.Lister
	history  History
	agents   AgentProcesses
	events   event.Sink
	logger   *slog.Logger
	procs    processTable

	// progressEvery is how often, at most, the window's listing tells how far it has got.
	progressEvery time.Duration

	// zone is the machine's time zone, which an agent lock's start may be written in.
	zone *time.Location

	// act serializes every removal, so a sweep and a click never work on the same
	// worktree at once. A Maker holds it shared while it makes or resumes a worktree, so
	// neither runs then either.
	act sync.RWMutex

	// clones holds a lock per clone (see holdClone): a Maker holds a clone's while it
	// finds, makes, and records one of its worktrees, a removal while it removes one or
	// deletes a branch, and a listing to forget a record, so it never forgets one a launch
	// is writing. Whoever needs act takes it before a clone's lock.
	clones *clonelock.Set

	// mu guards last.
	mu   sync.Mutex
	last SweepReport
}

// NewService returns a worktrees Service over cfg.
func NewService(cfg Config) *Service {
	realHome := cfg.Home
	if cfg.Home != "" {
		realHome = paths.Resolved(cfg.Home)
	}

	clones := cfg.Clones
	if clones == nil {
		clones = clonelock.New()
	}

	return &Service{
		settings: cfg.Settings,
		run:      cfg.Git,
		finder:   cfg.Finder,
		guard:    cfg.Guard,
		gitWith:  cfg.GitWith,
		home:     cfg.Home,
		realHome: realHome,
		clock:    clock.Or(cfg.Clock),
		github:   cfg.GitHub,
		launches: cfg.Records,
		lister:   cfg.Procs,
		history:  cfg.History,
		agents:   cfg.Agents,
		events:   event.Or(cfg.Events),
		logger:   logging.Or(cfg.Logger),
		clones:   clones,
		procs:    osProcesses{},
		zone:     time.Local, //nolint:gosmopolitan // the agent writes a session's start in the machine's zone or in UTC
		last:     emptyReport(),

		progressEvery: progress.Every,
	}
}

// Remove removes one linked worktree without --force, and with it the branches named,
// of those it offers (see Worktree.Branches): each goes only when nothing on it would be
// lost and nothing protects it, and only while it still points at the commit assessed.
// The repository must be a clone in the repository locations and path one of its linked
// worktrees, which is assessed again first. The main worktree, a locked one, and one a
// session works in are refused before git runs, and git itself refuses one with
// uncommitted changes, which comes back as a RefusedError. The result names the branches
// deleted, and says why any other one named was kept.
func (s *Service) Remove(ctx context.Context, repoPath, path string, branches []string) (RemoveResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	if err := s.checkNoRun(repoPath); err != nil {
		return RemoveResult{}, err
	}

	g, wt, err := s.assessOne(ctx, repoPath, path)
	if err != nil {
		return RemoveResult{}, err
	}

	return s.removeRow(ctx, g, repoPath, wt, false, branches)
}

// ForceRemove removes one unlocked linked worktree that has uncommitted changes,
// discarding them, with a single --force, and the branches named as Remove does. Locked
// worktrees, and ones a running process works in, are refused even here, so a running
// session's worktree is never pulled out from under it; so is every worktree when the
// running processes cannot be listed, since nothing else would stop it then.
func (s *Service) ForceRemove(ctx context.Context, repoPath, path string, branches []string) (RemoveResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	if err := s.checkNoRun(repoPath); err != nil {
		return RemoveResult{}, err
	}

	g, wt, err := s.assessOne(ctx, repoPath, path)
	if err != nil {
		return RemoveResult{}, err
	}

	res, err := s.removeRow(ctx, g, repoPath, wt, true, branches)
	if errors.Is(err, ErrSessionsUnknown) {
		return res, uierr.New("This system can't list the running processes, so it can't tell whether a session is "+
			"working in the worktree, and won't discard its changes.", err)
	}

	return res, err
}

// RemoveAllClean removes the clean, unlocked, linked worktrees picked, one at a time, each
// with the branches picked for it as Remove does, and reports each outcome. Each is
// checked against a fresh listing first: one that is no longer clean and removable is
// left, with why, and git refuses any worktree that changed in the meantime.
func (s *Service) RemoveAllClean(ctx context.Context, picks []Pick) ([]RemoveResult, error) {
	s.act.Lock()
	defer s.act.Unlock()

	snap, err := s.list(ctx, nil)
	if err != nil {
		return nil, err
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
		case !wt.CanRemove:
			results = append(results, pickRefused(p, wt.DisplayPath, ErrChanged))
		default:
			res, removeErr := s.removeRow(ctx, g, p.RepoPath, wt, false, p.Branches)
			if removeErr != nil {
				res.Reason = reasonOf(removeErr)
			}

			results = append(results, res)
		}
	}

	return results, nil
}

// pickRefused is the result for a picked worktree left as it was, with why.
func pickRefused(p Pick, display string, err error) RemoveResult {
	return RemoveResult{
		RepoPath: p.RepoPath, Path: p.Path, DisplayPath: display, DeletedBranches: []string{}, Reason: reasonOf(err),
	}
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

	unlock, err := s.holdClone(ctx, repoPath)
	if err != nil {
		return err
	}

	defer unlock()

	if stderr, pruneErr := g.prune(ctx, repoPath); pruneErr != nil {
		return fmt.Errorf("prune %s: %w", s.display(repoPath), gitError(pruneErr, stderr))
	}

	return nil
}

// assessOne assesses the clone at repoPath and returns the row of path, one of its
// linked worktrees.
func (s *Service) assessOne(ctx context.Context, repoPath, path string) (git, Worktree, error) {
	g, snap, err := s.assessClone(ctx, repoPath)
	if err != nil {
		return git{}, Worktree{}, err
	}

	wt, ok := findWorktree(snap, path)

	switch {
	case !ok:
		return git{}, Worktree{}, fmt.Errorf("%w: %s", ErrNotAWorktree, s.display(path))
	case wt.Main:
		return git{}, Worktree{}, ErrMainWorktree
	default:
		return g, wt, nil
	}
}

// removeRow removes an assessed linked worktree, with --force when force is set, which
// discards its uncommitted changes: a force removal needs some to discard, and the
// running processes listed. The worktree must still have the commit assessed checked out.
// The branches named then go with it, each only when it may (see deleteChosen). It holds
// the clone's lock throughout, and a run using the clone refuses it (see holdClone).
func (s *Service) removeRow(ctx context.Context, g git, repoPath string, wt Worktree, force bool,
	branches []string,
) (RemoveResult, error) {
	res := RemoveResult{
		RepoPath: repoPath, Path: wt.Path, DisplayPath: wt.DisplayPath, Item: itemName(wt.Item),
		DeletedBranches: []string{},
	}

	unlock, err := s.holdClone(ctx, repoPath)
	if err != nil {
		return res, err
	}

	defer unlock()

	e, err := s.removable(ctx, repoPath, wt.Path, wt.Head, force)
	if err != nil {
		return res, err
	}

	if force {
		if err = s.discardable(ctx, g, e); err != nil {
			return res, err
		}
	}

	if err = s.removeEntry(ctx, g, repoPath, e, force); err != nil {
		return res, err
	}

	res.Removed = true

	s.forgetRecord(ctx, wt.Path)

	deleted, kept := s.deleteChosen(ctx, g, repoPath, wt, branches)
	res.DeletedBranches, res.Reason = deleted, strings.Join(kept, "; ")

	return res, nil
}

// discardable refuses a force removal of a worktree with nothing to discard, or whose
// directory is gone.
func (s *Service) discardable(ctx context.Context, g git, e Entry) error {
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

	return nil
}

// unpushedText counts the commits a branch has that are not on GitHub, or says "commits"
// when the count could not be read.
func unpushedText(n int) string {
	switch {
	case n == 1:
		return "1 commit"
	case n > 1:
		return fmt.Sprintf("%d commits", n)
	default:
		return "commits"
	}
}

// records reads the worktree records. Without them worktrees are still found by their
// branch and linked by their name, so a failure is only logged.
func (s *Service) records(ctx context.Context) []worklog.Record {
	if s.launches == nil {
		return nil
	}

	records, err := s.launches.Worktrees(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the worktree records", "error", err)
	}

	return records
}

// prepare resolves git, the preferences, and the repository locations.
func (s *Service) prepare(ctx context.Context) (git, localrepos.Roots, SweepSettings, error) {
	g, err := s.git()
	if err != nil {
		return git{}, localrepos.Roots{}, SweepSettings{}, err
	}

	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return git{}, localrepos.Roots{}, SweepSettings{}, err
	}

	roots, err := localrepos.NewRoots(prefs.CloneRoot, prefs.CloneLocations, s.home)
	if err != nil {
		return git{}, localrepos.Roots{}, SweepSettings{}, err
	}

	return g, roots, prefs, nil
}

// git returns the allowlisted git runner, resolving the binary when a Finder is set, for
// one operation: the Local of each folder it runs a command in is worked out once.
func (s *Service) git() (git, error) {
	locals := &localCache{byID: map[string]localResult{}}

	if s.finder == nil {
		return git{run: s.run, bin: gitBin, guard: s.guard, with: s.gitWith, locals: locals}, nil
	}

	bin, err := s.finder.Find(gitBin)
	if err != nil {
		return git{}, fmt.Errorf("find git: %w", err)
	}

	return git{run: s.run, bin: bin, guard: s.guard, with: s.gitWith, locals: locals}, nil
}

// checkedGit resolves git and checks that repoPath is a clone the repository locations
// hold, as List reports them.
func (s *Service) checkedGit(ctx context.Context, repoPath string) (git, error) {
	g, roots, _, err := s.prepare(ctx)
	if err != nil {
		return git{}, err
	}

	if !roots.Holds(repoPath) {
		return git{}, fmt.Errorf("%w: %s", ErrOutsideCloneRoot, s.display(repoPath))
	}

	return g, nil
}

// entryOf re-lists the repository and returns path's entry when it is a linked worktree
// of it.
func (s *Service) entryOf(ctx context.Context, g git, repoPath, path string) (Entry, error) {
	entries, stderr, err := g.list(ctx, repoPath)
	if err != nil {
		return Entry{}, fmt.Errorf("list the worktrees of %s: %w", s.display(repoPath), gitError(err, stderr))
	}

	for _, e := range entries {
		if !paths.Same(e.Path, path) {
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
// unlocked worktree of it that no running process works in. When head is not "", the
// worktree must still have that commit checked out. strict also refuses it when the
// processes cannot be listed.
func (s *Service) removable(ctx context.Context, repoPath, path, head string, strict bool) (Entry, error) {
	g, err := s.checkedGit(ctx, repoPath)
	if err != nil {
		return Entry{}, err
	}

	e, err := s.entryOf(ctx, g, repoPath, path)

	switch {
	case err != nil:
		return Entry{}, err
	case e.Locked:
		return Entry{}, ErrLocked
	case head != "" && e.HEAD != head:
		return Entry{}, ErrChanged
	}

	if !e.Prunable {
		if err = s.checkSessions(ctx, e.Path, strict); err != nil {
			return Entry{}, err
		}
	}

	return e, nil
}

// removeEntry runs `git worktree remove` for a checked entry and turns a refusal into a
// RefusedError.
func (s *Service) removeEntry(ctx context.Context, g git, repoPath string, e Entry, force bool) error {
	stderr, err := g.remove(ctx, repoPath, e.Path, force)
	if err == nil {
		return nil
	}

	if !gitRefused(ctx, err) {
		return fmt.Errorf("remove %s: %w", s.display(e.Path), err)
	}

	return &RefusedError{DisplayPath: s.display(e.Path), Reason: refusalReason(stderr)}
}

// display shows path relative to home with ~, also matching home's resolved form (git
// reports resolved paths, and home may sit behind a symlink).
func (s *Service) display(path string) string {
	if shown := paths.Tilde(path, s.home); shown != path || s.realHome == s.home {
		return shown
	}

	return paths.Tilde(path, s.realHome)
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

// gitError turns a git failure into an error carrying git's own message when it gave one.
func gitError(err error, stderr []byte) error {
	if line := execpath.GitMessage(stderr); line != "" {
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

	if line := execpath.GitMessage(stderr); line != "" {
		return line
	}

	return "git exited with an error"
}

// reasonOf is the reason shown for a failed removal in a bulk result.
func reasonOf(err error) string {
	if refused, ok := errors.AsType[*RefusedError](err); ok {
		return refused.Reason
	}

	return err.Error()
}
