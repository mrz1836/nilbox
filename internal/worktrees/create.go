package worktrees

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// This file is the only one allowed to make worktrees: to fetch a pull request's head or
// a default branch, and to add a worktree. Nothing in the app changes a clone's own
// checkout; only worktree creation fetches and adds worktrees (see the repoguard tests).

const (
	// ensureTimeout bounds one Ensure, fetches included.
	ensureTimeout = 3 * time.Minute

	// remotesRef starts a remote-tracking branch's full ref.
	remotesRef = "refs/remotes/"

	// pullRefPrefix and pullRefSuffix name a pull request's head on GitHub:
	// refs/pull/<n>/head.
	pullRefPrefix = "refs/pull/"
	pullRefSuffix = "/head"
)

// Action is what Ensure does, or would do, to give an item a worktree.
type Action string

// Ensure's actions.
const (
	// ActionResume reuses the item's worktree, where the app made or last found it.
	ActionResume Action = "resume"

	// ActionAdopt takes over a worktree found on the item's branch somewhere else, such
	// as one Claude Code made under the clone's .claude/worktrees, and records it.
	ActionAdopt Action = "adopt"

	// ActionAttach makes a worktree on the item's branch, which is left from an earlier
	// worktree, keeping every commit on it.
	ActionAttach Action = "attach"

	// ActionCreate fetches the pull request's head, or the default branch for an issue,
	// and makes a worktree on a new branch from it.
	ActionCreate Action = "create"
)

var (
	// ErrInvalidRequest is returned for an EnsureRequest that cannot name a worktree.
	ErrInvalidRequest = errors.New("invalid worktree request")

	// ErrPathTaken is returned when something that is not a worktree is where the item's
	// worktree goes.
	ErrPathTaken = errors.New("something else is where the worktree goes")

	// ErrNoDefaultBranch is returned when the default branch of the item's repository
	// cannot be found.
	ErrNoDefaultBranch = errors.New("the repository's default branch could not be found")

	// ErrHeadMoved is returned when a pull request's head fetched for its new worktree is
	// not the commit whose files the launch checked.
	ErrHeadMoved = errors.New("the pull request's head moved after its files were checked")
)

// remotePattern is what a remote's name must look like to be passed to git.
var remotePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// EnsureRequest asks for an item's worktree in a clone.
type EnsureRequest struct {
	// Clone is the clone's absolute directory.
	Clone string

	// Remote is the clone's remote for the item's repository, such as "origin", or
	// "upstream" in a fork's clone; "" is origin.
	Remote string

	// Owner and Name are the item's repository.
	Owner string
	Name  string

	// Kind and Number are the item.
	Kind   item.Kind
	Number int

	// DefaultBranch is the repository's default branch as GitHub reports it, or "". An
	// issue's worktree starts from it.
	DefaultBranch string

	// Head is the pull request's head commit whose files the launch checked, or "" when
	// it checked none. A pull request's worktree made now must start there (see
	// verifyHead).
	Head string
}

// Ensured is an item's worktree, as Ensure found or made it, or Plan says it would.
type Ensured struct {
	// Path is the worktree's directory.
	Path string

	// Branch is the branch it has, or will have, checked out.
	Branch string

	// Action is what was done, or would be.
	Action Action

	// Base names what a created worktree starts from: "the pull request's head" or the
	// remote's default branch, such as "origin/main"; "" for the other actions.
	Base string

	// Head is the commit the worktree has checked out, or "" before it is made.
	Head string

	// Dirty is how many files have uncommitted changes in an existing worktree, or -1
	// when that could not be read; 0 for one not made yet.
	Dirty int

	// Notice says what Ensure could not do as asked but worked around, such as starting
	// from the last fetched default branch when the fetch failed, or "".
	Notice string
}

// Maker finds and makes the worktrees launches start in: one per item per clone, shared
// by every agent, under the clone's managed root (see ManagedRoot), and recorded. It is
// kept apart from Service, every exported method of which the window binds.
type Maker struct {
	s *Service

	// calls runs one Ensure per item at a time, handing its result to every caller that
	// asked while it ran.
	calls flight.Group[Ensured]
}

// NewMaker returns a Maker over svc's git, records, and clock.
func NewMaker(svc *Service) *Maker {
	return &Maker{s: svc}
}

// Plan says what Ensure would do for req, changing nothing and asking no remote.
func (m *Maker) Plan(ctx context.Context, req EnsureRequest) (Ensured, error) {
	if err := req.check(); err != nil {
		return Ensured{}, err
	}

	g, err := m.s.git()
	if err != nil {
		return Ensured{}, err
	}

	f, err := m.find(ctx, g, req, m.record(ctx, req))
	if err != nil {
		return Ensured{}, err
	}

	return f.ensured(), nil
}

// Ensure returns the item's worktree, making it when it has none, in this order:
//
//  1. the worktree the item's record names, while git still has it registered there;
//  2. a worktree with the item's branch (worktree-pr-<n> or worktree-issue-<n>) checked
//     out anywhere, such as one Claude Code made under the clone's .claude/worktrees, which
//     is adopted;
//  3. a worktree added on the item's branch, left from an earlier worktree, keeping every
//     commit on it and never moving it;
//  4. for a pull request, its head fetched from the item's remote into the item's branch,
//     and a worktree added on it;
//  5. for an issue, the repository's default branch fetched, and a worktree added on a
//     new item's branch from it.
//
// Entries whose directories are gone are pruned first. When the worktree's directory is
// taken by something git does not know, git's entries are pruned and the add is tried
// once more; it never deletes anything. The worktree is recorded with its item. Calls for
// one item share one run, and one clone's git work runs one call at a time. While a run (a
// release, checks, a script) is using the clone, Ensure fails at once, naming it.
func (m *Maker) Ensure(ctx context.Context, req EnsureRequest) (Ensured, error) {
	if err := req.check(); err != nil {
		return Ensured{}, err
	}

	key := paths.Resolved(req.Clone) + "\x00" + item.WorktreeName(req.Kind, req.Number)

	return m.calls.Do(ctx, key, func(ctx context.Context) (Ensured, error) {
		ctx, cancel := context.WithTimeout(ctx, ensureTimeout)
		defer cancel()

		// A sweep or a removal never runs while a worktree is being made or resumed, and
		// one clone's git work runs one call at a time. act comes first, as everywhere.
		m.s.act.RLock()
		defer m.s.act.RUnlock()

		unlock, err := m.s.holdClone(ctx, req.Clone)
		if err != nil {
			return Ensured{}, err
		}

		defer unlock()

		return m.ensure(ctx, req)
	})
}

// ensure does Ensure's work under the clone's lock.
func (m *Maker) ensure(ctx context.Context, req EnsureRequest) (Ensured, error) {
	g, err := m.s.git()
	if err != nil {
		return Ensured{}, err
	}

	f, err := m.find(ctx, g, req, m.record(ctx, req))
	if err != nil {
		return Ensured{}, err
	}

	if f.action == ActionAttach || f.action == ActionCreate {
		// One setup serves every guarded command the build runs: its fetches, its
		// ls-remote, and its checkout.
		if err = m.build(ctx, g.session(req.Clone, req.remote()), req, &f); err != nil {
			return Ensured{}, err
		}
	}

	if err = m.s.recordWorktree(ctx, req, f.path, f.branch); err != nil {
		return Ensured{}, err
	}

	return f.ensured(), nil
}

// check reports an EnsureRequest that cannot name a worktree.
func (req EnsureRequest) check() error {
	switch {
	case !filepath.IsAbs(req.Clone):
		return fmt.Errorf("%w: the clone %q is not an absolute path", ErrInvalidRequest, req.Clone)
	case !req.Kind.Valid() || req.Number <= 0:
		return fmt.Errorf("%w: no %s #%d", ErrInvalidRequest, req.Kind, req.Number)
	case req.Owner == "" || req.Name == "":
		return fmt.Errorf("%w: no repository", ErrInvalidRequest)
	case req.remote() != "" && !remotePattern.MatchString(req.remote()):
		return fmt.Errorf("%w: the remote %q", ErrInvalidRequest, req.Remote)
	case !req.validHead():
		return fmt.Errorf("%w: the head %q", ErrInvalidRequest, req.Head)
	default:
		return nil
	}
}

// validHead reports a Head the request may carry: none, or a pull request's commit.
func (req EnsureRequest) validHead() bool {
	return req.Head == "" || (req.Kind == item.KindPR && gitread.IsCommitID(req.Head))
}

// remote returns the remote to fetch from: Remote, or origin.
func (req EnsureRequest) remote() string {
	if req.Remote == "" {
		return originRemote
	}

	return req.Remote
}

// name returns the item's worktree name, pr-<n> or issue-<n>.
func (req EnsureRequest) name() string {
	return item.WorktreeName(req.Kind, req.Number)
}

// record returns the item's worktree record in the clone, or nil. Without it a worktree is
// still found by its branch, so a failure is only logged.
func (m *Maker) record(ctx context.Context, req EnsureRequest) *worklog.Record {
	for _, r := range m.s.records(ctx) {
		if r.Worktree == req.name() && paths.Same(r.ClonePath, req.Clone) {
			return &r
		}
	}

	return nil
}

// found is what find learned: the action, and the worktree it acts on.
type found struct {
	action Action
	path   string
	branch string
	base   string
	head   string
	dirty  int
	notice string

	// prune reports entries whose directories are gone that hold the item's path or
	// branch, which git must forget before the worktree can be added.
	prune bool
}

// ensured reports what was found or made.
func (f found) ensured() Ensured {
	return Ensured{
		Path: f.path, Branch: f.branch, Action: f.action, Base: f.base, Head: f.head, Dirty: f.dirty, Notice: f.notice,
	}
}

// find works out Ensure's first three steps, changing nothing.
func (m *Maker) find(ctx context.Context, g git, req EnsureRequest, rec *worklog.Record) (found, error) {
	entries, stderr, err := g.list(ctx, req.Clone)
	if err != nil {
		return found{}, fmt.Errorf("list the worktrees of %s: %w", m.s.display(req.Clone), gitError(err, stderr))
	}

	target := ManagedPath(req.Clone, req.name())
	branch := ownBranchPrefix + req.name()

	if e, ok := pickEntry(entries, rec, target, branch); ok {
		return m.reuse(ctx, g, e, rec, target), nil
	}

	f := found{action: ActionCreate, path: target, branch: branch, prune: staleEntry(entries, target, branch)}

	oid, err := branchCommit(ctx, g, req.Clone, branch)
	if err != nil {
		return found{}, err
	}

	switch {
	case oid != "":
		f.action, f.head = ActionAttach, oid
	case req.Kind == item.KindPR:
		f.base = "the pull request's head"
	default:
		f.base = req.remote() + "/" + defaultName(req.DefaultBranch)
	}

	return f, nil
}

// reuse is what find learned about a live worktree to reuse: resumed where the app made
// or recorded it, else adopted.
func (m *Maker) reuse(ctx context.Context, g git, e Entry, rec *worklog.Record, target string) found {
	f := found{action: ActionAdopt, path: e.Path, branch: e.Branch, head: e.HEAD, dirty: m.dirty(ctx, g, e.Path)}

	if paths.Same(e.Path, target) || (rec != nil && paths.Same(e.Path, rec.Dir())) {
		f.action = ActionResume
	}

	return f
}

// staleEntry reports an entry whose directory is gone that holds the item's path or
// branch, which git must forget before the worktree can be added.
func staleEntry(entries []Entry, target, branch string) bool {
	return slices.ContainsFunc(entries, func(e Entry) bool {
		return e.Prunable && (e.Branch == branch || paths.Same(e.Path, target))
	})
}

// defaultName names a default branch for people, before it is known for certain.
func defaultName(branch string) string {
	if branch == "" {
		return "HEAD"
	}

	return branch
}

// pickEntry returns the live worktree to reuse: the one the record names, then the one at
// the item's managed path, then one with the item's branch checked out anywhere.
func pickEntry(entries []Entry, rec *worklog.Record, target, branch string) (Entry, bool) {
	matches := []func(Entry) bool{
		func(e Entry) bool { return paths.Same(e.Path, target) },
		func(e Entry) bool { return e.Branch == branch },
	}

	if rec != nil {
		matches = append([]func(Entry) bool{func(e Entry) bool { return paths.Same(e.Path, rec.Dir()) }}, matches...)
	}

	for _, match := range matches {
		for _, e := range entries {
			if !e.Main && !e.Bare && !e.Prunable && match(e) && dirExists(e.Path) {
				return e, true
			}
		}
	}

	return Entry{}, false
}

// dirty counts a worktree's uncommitted files, or -1 when git cannot say.
func (m *Maker) dirty(ctx context.Context, g git, path string) int {
	count, _, err := g.dirtyCount(ctx, path)
	if err != nil {
		return -1
	}

	return count
}

// build adds the worktree find decided on: on the branch left from an earlier worktree,
// or on a branch fetched or made for the item.
func (m *Maker) build(ctx context.Context, g git, req EnsureRequest, f *found) error {
	if f.prune {
		if stderr, err := g.prune(ctx, req.Clone); err != nil {
			return fmt.Errorf("prune the worktrees of %s: %w", m.s.display(req.Clone), gitError(err, stderr))
		}
	}

	if err := os.MkdirAll(filepath.Dir(f.path), worktreeDirPerm); err != nil {
		return fmt.Errorf("make %s: %w", m.s.display(filepath.Dir(f.path)), err)
	}

	add, err := m.addArgs(ctx, g, req, f)
	if err != nil {
		return err
	}

	if err = m.add(ctx, g, req.Clone, f.path, add); err != nil {
		return err
	}

	entries, stderr, err := g.list(ctx, req.Clone)
	if err != nil {
		return fmt.Errorf("list the worktrees of %s: %w", m.s.display(req.Clone), gitError(err, stderr))
	}

	for _, e := range entries {
		if paths.Same(e.Path, f.path) {
			f.path, f.head = e.Path, e.HEAD
		}
	}

	f.dirty = 0

	return nil
}

// addArgs prepares the branch the worktree is added on, fetching what it needs, and
// returns the `git worktree add` arguments that add it at f.path.
func (m *Maker) addArgs(ctx context.Context, g git, req EnsureRequest, f *found) ([]string, error) {
	switch {
	case f.action == ActionAttach:
		return worktreeAddArgs(f.path, f.branch), nil
	case req.Kind == item.KindPR:
		if stderr, err := g.fetchPR(ctx, req.Clone, req.remote(), req.Number); err != nil {
			extra, cause := remoteWhy(req.remote(), err, stderr, g.setAside())

			return nil, uierr.New(fmt.Sprintf("Couldn't fetch pull request #%d from %s.", req.Number, req.remote())+extra,
				fmt.Errorf("fetch pull request #%d from %s: %w", req.Number, req.remote(), cause))
		}

		if err := m.verifyHead(ctx, g, req, f.branch); err != nil {
			return nil, err
		}

		return worktreeAddArgs(f.path, f.branch), nil
	default:
		base, notice, err := m.fetchDefault(ctx, g, req)
		if err != nil {
			return nil, err
		}

		f.base = strings.TrimPrefix(base, remotesRef)
		f.notice = notice

		return worktreeNewBranchArgs(f.path, f.branch, base), nil
	}
}

// verifyHead makes sure the pull request's head just fetched into branch is the commit
// whose files the launch checked, when it checked one. A push between the check and the
// fetch would otherwise put files no one checked, such as agent configuration, in the
// worktree a session then starts in. On a mismatch the branch is deleted again, only
// while it still points where the fetch put it, and the launch is refused, so launching
// again checks the new head.
func (m *Maker) verifyHead(ctx context.Context, g git, req EnsureRequest, branch string) error {
	if req.Head == "" {
		return nil
	}

	got, err := branchCommit(ctx, g, req.Clone, branch)
	if err != nil {
		return err
	}

	if got == req.Head {
		return nil
	}

	if gitread.IsCommitID(got) {
		if stderr, err := g.deleteBranch(ctx, req.Clone, branch, got); err != nil {
			m.s.logger.WarnContext(ctx, "could not delete a pull request head that was not checked",
				"branch", branch, "error", gitError(err, stderr))
		}
	}

	return uierr.New(
		fmt.Sprintf("Pull request #%d changed while it was opening, so its new commits weren't checked. "+
			"Open it again to check them.", req.Number),
		fmt.Errorf("%w: #%d is at %s, its files were checked at %s", ErrHeadMoved, req.Number, got, req.Head),
	)
}

// fetchDefault fetches the default branch of the item's repository from its remote and
// returns its remote-tracking ref. When the fetch fails but an earlier fetch left the ref,
// it starts from that, with a notice saying so.
func (m *Maker) fetchDefault(ctx context.Context, g git, req EnsureRequest) (string, string, error) {
	branch, err := m.defaultBranch(ctx, g, req)
	if err != nil {
		return "", "", err
	}

	remote := req.remote()
	ref := remotesRef + remote + "/" + branch

	stderr, fetchErr := g.fetchBranch(ctx, req.Clone, remote, branch)
	if fetchErr == nil {
		return ref, "", nil
	}

	extra, cause := remoteWhy(remote, fetchErr, stderr, g.setAside())

	if ok, _ := refExists(ctx, g, req.Clone, ref); ok && ctx.Err() == nil {
		return ref, fmt.Sprintf("Couldn't fetch %s/%s, so the worktree starts from the last fetched copy: %s",
			remote, branch, remoteSaid(fetchErr, cause, extra)), nil
	}

	return "", "", uierr.New(fmt.Sprintf("Couldn't fetch %s/%s.", remote, branch)+extra,
		fmt.Errorf("fetch %s/%s: %w", remote, branch, cause))
}

// remoteWhy works out what a guarded command that failed adds to the sentence that says
// so, "" or a space and more words, and its cause: gitread's words, as a sentence, when
// its guards refused it before git ran (a *guardError), naming the clone's own settings
// they refused or the remote, or saying why its setup couldn't be worked out, such as a
// config git can't read; or, when it reached the clone's remote called remote without
// settings of the clone's own, which may be why, which they are and where to set them
// instead (gitread.SetAsideNote), with a gitread.SetAsideError in the cause's chain; or
// else git's own line.
func remoteWhy(remote string, err error, stderr []byte, aside []string) (extra string, cause error) {
	if refused, ok := errors.AsType[*guardError](err); ok {
		return " " + textutil.Sentence(gitread.Message(refused.err)), err
	}

	if len(aside) > 0 {
		return " " + gitread.SetAsideNote(remote, aside),
			&gitread.SetAsideError{Err: gitError(err, stderr), Settings: aside, Remote: remote}
	}

	return "", gitError(err, stderr)
}

// remoteSaid is why a command that reached the clone's remote failed, in a sentence, from
// remoteWhy's cause and extra.
func remoteSaid(err, cause error, extra string) string {
	if _, ok := errors.AsType[*guardError](err); ok {
		return strings.TrimSpace(extra)
	}

	return textutil.EndSentence(uierr.Message(cause)) + extra
}

// defaultBranch finds the default branch of the item's repository: as GitHub reported
// it, else as the clone's remote last said (refs/remotes/<remote>/HEAD), else as the
// remote says now.
func (m *Maker) defaultBranch(ctx context.Context, g git, req EnsureRequest) (string, error) {
	if gitread.ValidBranch(req.DefaultBranch) {
		return req.DefaultBranch, nil
	}

	remote := req.remote()

	if out, _, err := g.do(ctx, req.Clone, symbolicRefArgs(remote)...); err == nil {
		if branch, ok := strings.CutPrefix(strings.TrimSpace(string(out)), remotesRef+remote+"/"); ok && gitread.ValidBranch(branch) {
			return branch, nil
		}
	}

	out, stderr, err := g.reach(ctx, req.Clone, remote, lsRemoteArgs(remote)...)
	if err != nil {
		extra, cause := remoteWhy(remote, err, stderr, g.setAside())

		return "", uierr.New(fmt.Sprintf("Couldn't find the default branch of %s.", remote)+extra,
			fmt.Errorf("%w: %s: %w", ErrNoDefaultBranch, remote, cause))
	}

	if branch, ok := parseSymref(string(out)); ok {
		return branch, nil
	}

	return "", uierr.New(fmt.Sprintf("Couldn't find the default branch of %s.", remote),
		fmt.Errorf("%w: %s", ErrNoDefaultBranch, remote))
}

// parseSymref reads the branch HEAD points at from `git ls-remote --symref <remote>
// HEAD`: "ref: refs/heads/<branch>\tHEAD".
func parseSymref(out string) (string, bool) {
	for line := range strings.Lines(out) {
		target, name, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || name != "HEAD" {
			continue
		}

		branch, ok := strings.CutPrefix(target, "ref: "+headsPrefix)
		if ok && gitread.ValidBranch(branch) {
			return branch, true
		}
	}

	return "", false
}

// add runs `git worktree add`. When it fails because the directory is taken by something
// git does not know, it prunes git's entries and tries once more, then says what is in
// the way; it never deletes anything.
func (m *Maker) add(ctx context.Context, g git, clone, path string, args []string) error {
	stderr, err := g.addWorktree(ctx, clone, args)
	if err == nil {
		return nil
	}

	if !gitRefused(ctx, err) || !dirExists(path) {
		extra, cause := remoteWhy("", err, stderr, nil)

		return uierr.New(fmt.Sprintf("Couldn't make the worktree %s.", m.s.display(path))+extra,
			fmt.Errorf("add the worktree %s: %w", m.s.display(path), cause))
	}

	if _, pruneErr := g.prune(ctx, clone); pruneErr == nil {
		if stderr, err = g.addWorktree(ctx, clone, args); err == nil {
			return nil
		}
	}

	return uierr.New(fmt.Sprintf("%s is in the way of the worktree: it isn't one git knows. Move it or delete it, "+
		"then launch again.", m.s.display(path)),
		fmt.Errorf("%w: %s: %w", ErrPathTaken, m.s.display(path), gitError(err, stderr)))
}

// worktreeDirPerm is the mode of the directories the managed worktrees go in.
const worktreeDirPerm fs.FileMode = 0o755

// dirExists reports whether path is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// branchCommit returns the commit the clone's local branch points at, or "" when it has
// no such branch.
func branchCommit(ctx context.Context, g git, clone, branch string) (string, error) {
	return refCommit(ctx, g, clone, headsPrefix+branch)
}

// refExists reports whether the clone has ref.
func refExists(ctx context.Context, g git, clone, ref string) (bool, error) {
	oid, err := refCommit(ctx, g, clone, ref)

	return oid != "", err
}

// refCommit returns the commit ref points at, or "" when the clone has no such ref.
// git's "no such ref" is "", and failing to run git is an error.
func refCommit(ctx context.Context, g git, clone, ref string) (string, error) {
	stdout, stderr, err := g.do(ctx, clone, revParseArgs(ref)...)

	switch {
	case err == nil:
		return strings.TrimSpace(string(stdout)), nil
	case gitRefused(ctx, err):
		return "", nil
	default:
		return "", fmt.Errorf("look for %s: %w", ref, gitError(err, stderr))
	}
}

// recordWorktree records the item's worktree. A worktree no record names still resumes,
// found by its branch, so this fails only when the database does.
func (s *Service) recordWorktree(ctx context.Context, req EnsureRequest, path, branch string) error {
	if s.launches == nil {
		return nil
	}

	return s.launches.Record(ctx, worklog.Record{
		ClonePath: req.Clone, Worktree: req.name(), Path: path, Branch: branch, Owner: req.Owner, Name: req.Name,
		Kind: req.Kind, Number: req.Number, CreatedAt: s.clock.Now(),
	})
}

// The git commands only this file runs, and the checks the allowlist applies to them.

// fetchPR fetches pull request n's head from remote into the item's branch, which does
// not exist yet: git refuses to move a branch that does, so nothing is ever overwritten.
// It runs under gitread's guards for remote (git.reach).
func (g git) fetchPR(ctx context.Context, dir, remote string, n int) ([]byte, error) {
	_, stderr, err := g.reach(ctx, dir, remote, fetchArgs(remote, pullRefspec(n))...)

	return stderr, err
}

// fetchBranch fetches remote's branch into its remote-tracking ref, as fetchPR does.
func (g git) fetchBranch(ctx context.Context, dir, remote, branch string) ([]byte, error) {
	_, stderr, err := g.reach(ctx, dir, remote, fetchArgs(remote, trackingRefspec(remote, branch))...)

	return stderr, err
}

// fetchArgs is the one fetch worktree creation runs: refspec from remote, which updates
// only the ref refspec names, never one the clone's own remote.<name>.fetch maps
// (--refmap=, as a forced map could name the branch the clone is on), and no tag; asking
// for git's own upload-pack, so a remote.<name>.uploadpack in the clone's config runs
// nothing. The repoguard tests hold its words to exactly these.
func fetchArgs(remote, refspec string) []string {
	return append([]string{"fetch", "--no-tags", "--refmap=", "--upload-pack=git-upload-pack"}, remote, refspec)
}

// addWorktree runs `git worktree add` with args, under gitread's guards for a command that
// writes files (git.write): no hook, and none of the clone's own filter drivers.
func (g git) addWorktree(ctx context.Context, dir string, args []string) ([]byte, error) {
	_, stderr, err := g.write(ctx, dir, args...)

	return stderr, err
}

// worktreeAddArgs adds a worktree at path on an existing branch.
func worktreeAddArgs(path, branch string) []string {
	return []string{"worktree", "add", path, branch}
}

// worktreeNewBranchArgs adds a worktree at path on a new branch from a remote-tracking
// ref, without making the remote's branch its upstream.
func worktreeNewBranchArgs(path, branch, from string) []string {
	return []string{"worktree", "add", "--no-track", "-b", branch, path, from}
}

// pullRefspec fetches pull request n's head into the item's branch.
func pullRefspec(n int) string {
	return pullRefPrefix + strconv.Itoa(n) + pullRefSuffix + ":" + headsPrefix + ownBranchPrefix +
		item.WorktreeName(item.KindPR, n)
}

// trackingRefspec fetches remote's branch into its remote-tracking ref.
func trackingRefspec(remote, branch string) string {
	return "+" + headsPrefix + branch + ":" + remotesRef + remote + "/" + branch
}

// allowedCreate reports one of this file's commands with arguments it may run with:
// fetching a pull request's head into its new item's branch, fetching a default branch
// into its remote-tracking ref, and adding a worktree on an item's branch.
func allowedCreate(args []string) bool {
	switch {
	case hasWords(args, 6, "fetch", "--no-tags", "--refmap=", uploadPack):
		return remotePattern.MatchString(args[4]) && allowedFetchRefspec(args[4], args[5])
	case hasWords(args, 4, "worktree", "add"):
		return filepath.IsAbs(args[2]) && isOwnBranchRef(headsPrefix+args[3])
	case hasWords(args, 7, "worktree", "add", "--no-track", "-b"):
		return isOwnBranchRef(headsPrefix+args[4]) && filepath.IsAbs(args[5]) && isTrackingBranch(args[6])
	default:
		return false
	}
}

// hasWords reports n arguments that start with words.
func hasWords(args []string, n int, words ...string) bool {
	return len(args) == n && joinArgs(args[:len(words)]) == joinArgs(words)
}

// isTrackingBranch reports refs/remotes/<remote>/<branch>, the start of an issue's
// worktree.
func isTrackingBranch(ref string) bool {
	rest, ok := strings.CutPrefix(ref, remotesRef)

	return ok && gitread.ValidBranch(rest)
}

// allowedFetchRefspec reports a refspec this file fetches from remote.
func allowedFetchRefspec(remote, refspec string) bool {
	src, dst, ok := strings.Cut(refspec, ":")
	if !ok {
		return false
	}

	if n, isPR := pullNumber(src); isPR {
		return dst == headsPrefix+ownBranchPrefix+item.WorktreeName(item.KindPR, n)
	}

	branch, ok := strings.CutPrefix(src, "+"+headsPrefix)

	return ok && gitread.ValidBranch(branch) && dst == remotesRef+remote+"/"+branch
}

// pullNumber reads n from refs/pull/<n>/head.
func pullNumber(ref string) (int, bool) {
	rest, ok := strings.CutPrefix(ref, pullRefPrefix)
	if !ok {
		return 0, false
	}

	digits, ok := strings.CutSuffix(rest, pullRefSuffix)
	if !ok {
		return 0, false
	}

	n, err := strconv.Atoi(digits)

	return n, err == nil && n > 0 && strconv.Itoa(n) == digits
}
