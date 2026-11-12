package worktrees

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/progress"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// listConcurrency bounds how many repositories List reads at once.
const listConcurrency = 8

// List scans the repository locations and returns every repository that has linked worktrees,
// with the status of each worktree and the pull request or issue it was made for. A
// repository that git cannot read is listed with an error rather than hiding the rest. A
// clone that is itself a linked worktree of another clone is folded into that repository.
// It tells the window how far it has got as it goes (event.WorktreesProgress).
//
// git runs only in the clones worth listing (see worthListing): those whose git folder
// registers a linked worktree, can't be found, or can't be read, and those a record names.
// A clone with none of these runs no git at all, so one with no worktrees whose git folder
// is there but which git can't read shows no error.
func (s *Service) List(ctx context.Context) (Snapshot, error) {
	tracker := s.newListProgress()
	defer tracker.End()

	return s.list(ctx, tracker)
}

// listProgress tells the window how far its listing has got (event.WorktreesProgress); a
// nil one, as every listing but the window's passes, tells nothing.
type listProgress = progress.Tracker[event.WorktreesPhase]

// newListProgress returns the progress of the window's listing.
func (s *Service) newListProgress() *listProgress {
	return progress.New(s.events, s.progressEvery, func(phase event.WorktreesPhase, done, total int) event.Event {
		return event.WorktreesProgress{Phase: phase, Done: done, Total: total}
	})
}

// list is List, telling tracker how far it has got, or nothing for a nil one, as a
// listing another job makes passes.
func (s *Service) list(ctx context.Context, tracker *listProgress) (Snapshot, error) {
	g, roots, prefs, err := s.prepare(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	tracker.Start(event.WorktreesFindingClones, 0)

	records := s.records(ctx)

	found, clones, err := s.scanSome(ctx, roots, func(dirs []string) []string { return worthListing(dirs, records) })
	if err != nil {
		return Snapshot{}, err
	}

	snap, err := s.assessClones(ctx, g, roots, clones, records, prefs.Branches, tracker)
	if err != nil {
		return Snapshot{}, err
	}

	snap.Unread = unreadOf(found, s.home)

	return snap, nil
}

// scan lists the clones in roots and reads their remotes.
func (s *Service) scan(ctx context.Context, roots localrepos.Roots) (localrepos.Found, []localrepos.LocalRepo, error) {
	return s.scanSome(ctx, roots, nil)
}

// scanSome lists the clones in roots and reads the remotes of those keep keeps, or of
// every one for a nil keep.
func (s *Service) scanSome(ctx context.Context, roots localrepos.Roots, keep func([]string) []string,
) (localrepos.Found, []localrepos.LocalRepo, error) {
	found, err := roots.Clones(ctx)
	if err != nil {
		return localrepos.Found{}, nil, err
	}

	dirs := found.Dirs
	if keep != nil {
		dirs = keep(dirs)
	}

	clones, err := localrepos.Scan(ctx, dirs, s.run)
	if err != nil {
		return localrepos.Found{}, nil, err
	}

	return found, clones, nil
}

// worthListing keeps the clones of dirs a listing runs git in, without running any: one
// whose common git folder registers a linked worktree; one whose common git folder can't
// be found or read, so git says why on its row; and one a record names, so a record whose
// worktree is gone is still checked and forgotten. Any other clone has no linked worktree
// to list.
func worthListing(dirs []string, records []worklog.Record) []string {
	named := make(map[string]bool, 2*len(records))

	for _, r := range records {
		named[filepath.Clean(r.ClonePath)] = true
		named[paths.Resolved(r.ClonePath)] = true
	}

	kept := make([]string, 0, len(dirs))

	for _, dir := range dirs {
		_, linked, known := linkedWorktrees(dir)

		switch {
		case !known, linked > 0, named[filepath.Clean(dir)]:
		case len(named) > 0 && named[paths.Resolved(dir)]:
		default:
			continue
		}

		kept = append(kept, dir)
	}

	return kept
}

// unreadOf names the locations a listing couldn't read, with ~; never nil.
func unreadOf(found localrepos.Found, home string) []string {
	out := make([]string, 0, len(found.Skipped))
	for _, u := range found.Skipped {
		out = append(out, paths.Tilde(u.Dir, home))
	}

	return out
}

// assessClone assesses the clone at repoPath alone, GitHub included, as a listing would:
// the repository locations must hold the clone.
func (s *Service) assessClone(ctx context.Context, repoPath string) (git, Snapshot, error) {
	g, roots, prefs, err := s.prepare(ctx)
	if err != nil {
		return git{}, Snapshot{}, err
	}

	if !roots.Holds(repoPath) {
		return git{}, Snapshot{}, fmt.Errorf("%w: %s", ErrOutsideCloneRoot, s.display(repoPath))
	}

	clone, err := localrepos.Inspect(ctx, repoPath, s.run)
	if err != nil {
		return git{}, Snapshot{}, err
	}

	snap, err := s.assessClones(ctx, g, roots, []localrepos.LocalRepo{clone}, s.records(ctx), prefs.Branches, nil)
	if err != nil {
		return git{}, Snapshot{}, err
	}

	return g, snap, nil
}

// assessClones lists the given clones' worktrees, checks records, the worktree records,
// against them, links them to their items, finds the sessions working in them, and works
// out each one's stage. The clones are listed a few at a time, then every worktree's
// state is read under the same limit, whichever clone it is in.
//
// The records and the running processes are read before git lists anything: a worktree
// made after the listing then has no record yet rather than a record that seems to name a
// worktree gone, and the git commands the listing runs are not mistaken for sessions.
func (s *Service) assessClones(ctx context.Context, g git, roots localrepos.Roots, clones []localrepos.LocalRepo,
	records []worklog.Record, rules branchrules.Rules, tracker *listProgress,
) (Snapshot, error) {
	tracker.Start(event.WorktreesListing, len(clones))

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

			tracker.Add(1)

			return listErr
		})
	}

	if err := eg.Wait(); err != nil {
		return Snapshot{}, err
	}

	if err := s.readRows(ctx, g, listings, tracker); err != nil {
		return Snapshot{}, err
	}

	recorded := s.reconcile(ctx, listings, records)

	listed := make([]listedRepo, 0, len(listings))
	for _, l := range listings {
		listed = append(listed, l.withRows())
	}

	snap := s.snapshot(roots, listed)
	snap.LinkError = s.link(ctx, snap.Repos, remotes, recorded, tracker)
	s.adopt(ctx, snap.Repos, recorded)
	markSessions(snap.Repos, running, s.agentProcesses(ctx))
	snap.SessionsUnknown = !known
	s.tally(&snap)
	s.markBranches(ctx, g, snap.Repos, rules)

	return snap, nil
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
		key:  paths.Resolved(dir),
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
		listed.key = paths.Resolved(entries[0].Path)
	}

	entries = slices.DeleteFunc(entries, func(e Entry) bool { return e.Bare })

	times, err := s.commitTimes(ctx, g, dir, entries)
	if err != nil {
		return cloneListing{}, err
	}

	return cloneListing{
		repo: listed, entries: entries, managedDir: paths.Resolved(ManagedRoot(listed.key)),
		legacyDir: paths.Resolved(legacyRoot(listed.key)), times: times,
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
		if !e.Prunable && gitread.IsCommitID(e.HEAD) && !seen[e.HEAD] {
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
func (s *Service) readRows(ctx context.Context, g git, listings []cloneListing, tracker *listProgress) error {
	eg, ectx := errgroup.WithContext(ctx)
	eg.SetLimit(listConcurrency)

	rows := 0
	for _, l := range listings {
		rows += len(l.entries)
	}

	tracker.Start(event.WorktreesChecking, rows)

	for i := range listings {
		l := &listings[i]
		l.rows = make([]Worktree, len(l.entries))

		for j, e := range l.entries {
			eg.Go(func() error {
				var rowErr error

				l.rows[j], rowErr = s.row(ectx, g, e, l.managedDir, l.legacyDir, l.times)

				tracker.Add(1)

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
	path := paths.Resolved(e.Path)

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
		Branches:       []Branch{},
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
	case guarded(err):
		// The clone's own config refuses a status: the worktree says why, and nothing that
		// needs its status, as a sweep's removal, touches it.
		wt.Error = uierr.Message(err)
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
func (s *Service) snapshot(roots localrepos.Roots, listed []listedRepo) Snapshot {
	snap := Snapshot{
		Roots:    roots.Shown(s.home),
		Unread:   []string{},
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

// within reports whether path is strictly inside dir.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)

	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
