package worktrees

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
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
		names = append(names, fmt.Sprintf("%s (pid %d)", s.Command, s.PID))
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

	// AutoCleanup lets the automatic sweep clean up a finished worktree once its pull
	// request merges or its issue closes.
	AutoCleanup bool
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
}

// GitHub reads the current state of pull requests and issues. The app adapts the GitHub
// client to it, with the GitHub CLI's token.
type GitHub interface {
	// ItemStates returns the state of each item in refs that GitHub knows, keyed by the
	// ref's Key.
	ItemStates(ctx context.Context, refs []item.Ref) (map[string]ItemState, error)
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
	// Settings supplies the clone root and whether the sweep cleans up on its own.
	Settings SettingsReader

	// Git runs git argv; it also reads remotes during the clone-root scan.
	Git execpath.Runner

	// Finder resolves the git binary up front, so a missing git fails with a clear
	// error. Without it, Git resolves "git" itself.
	Finder execpath.Finder

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

	// Events hears an event.WorktreesUpdated when a sweep removed a worktree or changed
	// how many need the user's attention.
	Events event.Sink

	// Logger receives sweep failures and, at Debug, why GitHub could not say which pull
	// requests and issues the worktrees belong to. Without it they are not logged.
	Logger *slog.Logger
}

// Service lists and removes worktrees of the clones under the clone root.
type Service struct {
	settings SettingsReader
	run      execpath.Runner
	finder   execpath.Finder
	home     string
	realHome string
	clock    clock.Clock
	github   GitHub
	launches Records
	lister   procs.Lister
	history  History
	events   event.Sink
	logger   *slog.Logger
	procs    processTable

	// zone is the machine's time zone, which an agent lock's start may be written in.
	zone *time.Location

	// act serializes every removal, so a sweep and a click never work on the same
	// worktree at once. A Maker holds it shared while it makes or resumes a worktree, so
	// neither runs then either.
	act sync.RWMutex

	// clonesMu guards clones, which holds a lock per clone: a Maker holds a clone's while
	// it finds, makes, and records one of its worktrees, and a listing holds it to forget
	// a record, so it never forgets one a launch is writing. Whoever needs act takes it
	// before a clone's lock.
	clonesMu sync.Mutex
	clones   map[string]*sync.Mutex

	// mu guards last.
	mu   sync.Mutex
	last SweepReport
}

// NewService returns a worktrees Service over cfg.
func NewService(cfg Config) *Service {
	realHome := cfg.Home
	if cfg.Home != "" {
		realHome = resolvedPath(cfg.Home)
	}

	return &Service{
		settings: cfg.Settings,
		run:      cfg.Git,
		finder:   cfg.Finder,
		home:     cfg.Home,
		realHome: realHome,
		clock:    clock.Or(cfg.Clock),
		github:   cfg.GitHub,
		launches: cfg.Records,
		lister:   cfg.Procs,
		history:  cfg.History,
		events:   event.Or(cfg.Events),
		logger:   logging.Or(cfg.Logger),
		procs:    osProcesses{},
		zone:     time.Local, //nolint:gosmopolitan // the agent writes a session's start in the machine's zone or in UTC
		last:     emptyReport(),
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
// discarding them, with a single --force. Locked worktrees, and ones a running process
// works in, are refused even here, so a running session's worktree is never pulled out
// from under it; so is every worktree when the running processes cannot be listed, since
// nothing else would stop it then.
func (s *Service) ForceRemove(ctx context.Context, repoPath, path string) error {
	s.act.Lock()
	defer s.act.Unlock()

	g, e, err := s.removable(ctx, repoPath, path, "", true)
	if errors.Is(err, ErrSessionsUnknown) {
		return uierr.New("This system can't list the running processes, so it can't tell whether a session is "+
			"working in the worktree, and won't discard its changes.", err)
	}

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
	g, e, err := s.removable(ctx, repoPath, path, "", false)
	if err != nil {
		return err
	}

	return s.removeEntry(ctx, g, repoPath, e, false)
}

// assessClones lists the given clones' worktrees, checks the records against them, links
// them to their items, finds the sessions working in them, and works out each one's
// stage. The clones are listed a few at a time, then every worktree's state is read under
// the same limit, whichever clone it is in.
//
// The records and the running processes are read before git lists anything: a worktree
// made after the listing then has no record yet rather than a record that seems to name a
// worktree gone, and the git commands the listing runs are not mistaken for sessions.
func (s *Service) assessClones(ctx context.Context, g git, root string, clones []localrepos.LocalRepo) (Snapshot, error) {
	records := s.records(ctx)
	running, known := s.processes(ctx)

	listings := make([]cloneListing, len(clones))
	remotes := make(map[string]map[string]repo.Name, len(clones))

	eg, ectx := errgroup.WithContext(ctx)
	eg.SetLimit(listConcurrency)

	for i, clone := range clones {
		remotes[clone.Path] = clone.Remotes

		eg.Go(func() error {
			var listErr error

			listings[i], listErr = s.listRepo(ectx, g, clone.Path)

			return listErr
		})
	}

	if err := eg.Wait(); err != nil {
		return Snapshot{}, err
	}

	if err := s.readRows(ctx, g, listings); err != nil {
		return Snapshot{}, err
	}

	recorded := s.reconcile(ctx, listings, records)

	listed := make([]listedRepo, 0, len(listings))
	for _, l := range listings {
		listed = append(listed, l.withRows())
	}

	snap := s.snapshot(root, listed)
	snap.LinkError = s.link(ctx, snap.Repos, remotes, recorded)
	s.adopt(ctx, snap.Repos, recorded)
	markSessions(snap.Repos, running)
	snap.SessionsUnknown = !known
	s.tally(&snap)

	return snap, nil
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

// processes lists the running processes other than this app and the commands it runs,
// and reports whether they could be listed. Without a lister none is listed, and that is
// known.
func (s *Service) processes(ctx context.Context) ([]procs.Process, bool) {
	if s.lister == nil {
		return nil, true
	}

	list, err := s.lister.Processes(ctx)
	if err != nil {
		s.logger.DebugContext(ctx, "the running processes could not be listed", "error", err)

		return nil, false
	}

	return procs.Others(list, os.Getpid()), true
}

// sessionsIn returns the sessions working in dir, and whether the processes could be
// listed.
func (s *Service) sessionsIn(ctx context.Context, dir string) ([]Session, bool) {
	running, known := s.processes(ctx)

	return sessionsOf(procs.Within(running, dir)), known
}

// checkSessions refuses a worktree a running process works in. strict refuses it as well
// when the processes cannot be listed, as the automatic cleanup and a force removal do; a
// plain removal the user asks for goes ahead then, since git refuses a worktree with
// changes, and the locks still guard it.
func (s *Service) checkSessions(ctx context.Context, dir string, strict bool) error {
	sessions, known := s.sessionsIn(ctx, dir)

	switch {
	case len(sessions) > 0:
		return &InUseError{Sessions: sessions}
	case !known && strict:
		return ErrSessionsUnknown
	default:
		return nil
	}
}

// markSessions fills in the sessions working in each linked worktree.
func markSessions(repos []Repo, running []procs.Process) {
	for _, wt := range rows(repos) {
		if !wt.Main && !wt.Prunable {
			wt.InUse = sessionsOf(procs.Within(running, wt.Path))
		}
	}
}

// sessionsOf returns processes as sessions, never nil.
func sessionsOf(list []procs.Process) []Session {
	out := make([]Session, 0, len(list))
	for _, p := range list {
		out = append(out, Session{PID: p.PID, Command: p.Command})
	}

	return out
}

// tally works out each worktree's stage, counts what the bulk actions would remove, and
// counts the linked worktrees.
func (s *Service) tally(snap *Snapshot) {
	snap.CleanCount, snap.FinishedCount, snap.ReviewCount, snap.WorktreeCount = 0, 0, 0, 0

	for _, wt := range rows(snap.Repos) {
		assess(wt, !snap.SessionsUnknown)

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
func (s *Service) prepare(ctx context.Context) (git, string, SweepSettings, error) {
	g, err := s.git()
	if err != nil {
		return git{}, "", SweepSettings{}, err
	}

	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return git{}, "", SweepSettings{}, err
	}

	root, err := paths.ExpandHome(prefs.CloneRoot, s.home)
	if err != nil {
		return git{}, "", SweepSettings{}, fmt.Errorf("clone root: %w", err)
	}

	return g, root, prefs, nil
}

// git returns the allowlisted git runner, resolving the binary when a Finder is set.
// lockClone takes the clone's lock (see Service.clones), and returns its release.
func (s *Service) lockClone(clone string) func() {
	key := resolvedPath(clone)

	s.clonesMu.Lock()

	if s.clones == nil {
		s.clones = map[string]*sync.Mutex{}
	}

	mu, ok := s.clones[key]
	if !ok {
		mu = &sync.Mutex{}
		s.clones[key] = mu
	}

	s.clonesMu.Unlock()

	mu.Lock()

	return mu.Unlock
}

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
// unlocked worktree of it that no running process works in. When head is not "", the
// worktree must still have that commit checked out. strict also refuses it when the
// processes cannot be listed.
func (s *Service) removable(ctx context.Context, repoPath, path, head string, strict bool) (git, Entry, error) {
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
	}

	if !e.Prunable {
		if err = s.checkSessions(ctx, e.Path, strict); err != nil {
			return git{}, Entry{}, err
		}
	}

	return g, e, nil
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

// cloneListing is one clone's worktrees as git lists them, then their rows.
type cloneListing struct {
	repo listedRepo

	// entries are the worktrees listed, the bare repository left out; managedDir is where
	// the app makes this clone's worktrees, and legacyDir where Claude Code made them.
	entries    []Entry
	managedDir string
	legacyDir  string

	// times holds the commit time of each listed HEAD, or is nil when git could not read
	// them together, so each worktree's is read on its own.
	times map[string]int64

	// rows are the entries' rows, in order, once read.
	rows []Worktree
}

// withRows returns the clone's repository with its rows added.
func (l cloneListing) withRows() listedRepo {
	listed := l.repo

	for _, wt := range l.rows {
		listed.add(wt)
	}

	return listed
}

// listRepo lists one clone's worktrees and reads the commit time of each one's HEAD, in
// one git run each. A git refusal (for example a broken clone) is recorded on the
// repository; failing to run git at all is an error.
func (s *Service) listRepo(ctx context.Context, g git, dir string) (cloneListing, error) {
	listed := listedRepo{
		repo: Repo{Path: dir, DisplayPath: s.display(dir), Name: filepath.Base(dir), Worktrees: []Worktree{}},
		key:  resolvedPath(dir),
	}

	entries, stderr, err := g.list(ctx, dir)

	switch {
	case err == nil:
	case gitRefused(ctx, err):
		listed.repo.Error = uierr.Message(gitError(err, stderr))

		return cloneListing{repo: listed}, nil
	default:
		return cloneListing{}, fmt.Errorf("list the worktrees of %s: %w", listed.repo.DisplayPath, err)
	}

	if len(entries) > 0 && entries[0].Main {
		listed.key = resolvedPath(entries[0].Path)
	}

	entries = slices.DeleteFunc(entries, func(e Entry) bool { return e.Bare })

	times, err := s.commitTimes(ctx, g, dir, entries)
	if err != nil {
		return cloneListing{}, err
	}

	return cloneListing{
		repo: listed, entries: entries, managedDir: resolvedPath(ManagedRoot(listed.key)),
		legacyDir: resolvedPath(legacyRoot(listed.key)), times: times,
	}, nil
}

// commitTimes reads the commit time of each listed worktree's HEAD in the clone at dir,
// in one git run. A worktree whose directory is gone, or whose HEAD has no commit yet,
// is left out. A git refusal, such as for a HEAD whose commit is missing, returns nil, and
// each worktree's time is then read on its own; failing to run git at all is an error.
func (s *Service) commitTimes(ctx context.Context, g git, dir string, entries []Entry) (map[string]int64, error) {
	var oids []string

	seen := make(map[string]bool, len(entries))

	for _, e := range entries {
		if !e.Prunable && isCommitID(e.HEAD) && !seen[e.HEAD] {
			seen[e.HEAD] = true
			oids = append(oids, e.HEAD)
		}
	}

	if len(oids) == 0 {
		return map[string]int64{}, nil
	}

	times, err := g.commitTimes(ctx, dir, oids)

	switch {
	case err == nil:
		return times, nil
	case gitRefused(ctx, err) || errors.Is(err, errParseCommitTime):
		return nil, nil //nolint:nilnil // no times is the answer: each worktree's is then read on its own
	default:
		return nil, fmt.Errorf("read the last commits in %s: %w", s.display(dir), err)
	}
}

// readRows builds the row of every listed worktree, a few at a time across all clones.
func (s *Service) readRows(ctx context.Context, g git, listings []cloneListing) error {
	eg, ectx := errgroup.WithContext(ctx)
	eg.SetLimit(listConcurrency)

	for i := range listings {
		l := &listings[i]
		l.rows = make([]Worktree, len(l.entries))

		for j, e := range l.entries {
			eg.Go(func() error {
				var rowErr error

				l.rows[j], rowErr = s.row(ectx, g, e, l.managedDir, l.legacyDir, l.times)

				return rowErr
			})
		}
	}

	return eg.Wait()
}

// row builds a worktree's view row, reading its status, unless its directory is gone.
// managedDir and legacyDir are where its clone's worktrees are made, and times holds its
// clone's commit times (see cloneListing).
func (s *Service) row(ctx context.Context, g git, e Entry, managedDir, legacyDir string, times map[string]int64) (Worktree, error) {
	path := resolvedPath(e.Path)

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
		Legacy:         within(legacyDir, path),
		Head:           e.HEAD,
		Unpushed:       -1,
		InUse:          []Session{},
	}

	wt.Managed = wt.Legacy || within(managedDir, path)

	s.judgeLock(&wt)
	wt.OwnBranch = wt.Managed && !e.Detached && e.Branch == ownBranchPrefix+filepath.Base(e.Path)

	if !e.Prunable {
		if err := s.readState(ctx, g, &wt, times); err != nil {
			return Worktree{}, err
		}
	}

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

// readState fills in a worktree's uncommitted-file count, last commit time, and unpushed
// commits. A git refusal leaves the field unknown (and records why, for the status);
// failing to run git at all is an error.
func (s *Service) readState(ctx context.Context, g git, wt *Worktree, times map[string]int64) error {
	count, stderr, err := g.dirtyCount(ctx, wt.Path)

	switch {
	case err == nil:
		wt.Dirty, wt.DirtyCount = count > 0, count
	case gitRefused(ctx, err):
		wt.Error = uierr.Message(fmt.Errorf("could not read the status: %w", gitError(err, stderr)))
	default:
		return fmt.Errorf("read the status of %s: %w", wt.DisplayPath, err)
	}

	unix, err := headTime(ctx, g, wt, times)
	if err != nil {
		return err
	}

	wt.LastCommitUnix = unix

	if wt.Main {
		return nil
	}

	unpushed, _, err := g.unpushed(ctx, wt.Path)

	switch {
	case err == nil:
		wt.Unpushed = unpushed
	case gitRefused(ctx, err) || errors.Is(err, errParseCount):
		// Unknown, which never counts as on GitHub.
	default:
		return fmt.Errorf("count the unpushed commits of %s: %w", wt.DisplayPath, err)
	}

	return nil
}

// headTime returns the commit time of a worktree's HEAD: from times, read with the rest
// of its clone's, or read on its own when times is nil. A HEAD with no commit, as in a
// repository without commits, has none to show and returns 0.
func headTime(ctx context.Context, g git, wt *Worktree, times map[string]int64) (int64, error) {
	if times != nil {
		return times[wt.Head], nil
	}

	unix, err := g.lastCommit(ctx, wt.Path)

	switch {
	case err == nil:
		return unix, nil
	case gitRefused(ctx, err) || errors.Is(err, errParseCommitTime):
		return 0, nil
	default:
		return 0, fmt.Errorf("read the last commit of %s: %w", wt.DisplayPath, err)
	}
}

// snapshot folds clones that share a main worktree and keeps the repositories worth
// showing.
func (s *Service) snapshot(root string, listed []listedRepo) Snapshot {
	snap := Snapshot{
		Root:     s.display(root),
		Repos:    []Repo{},
		ListedAt: clock.Format(s.clock.Now()),
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
	if shown := paths.Tilde(path, s.home); shown != path || s.realHome == s.home {
		return shown
	}

	return paths.Tilde(path, s.realHome)
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
