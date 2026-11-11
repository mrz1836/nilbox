package release

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The counts Unpushed holds when git can't give a number.
const (
	// UnpushedSome is a branch with commits origin's lacks, which git couldn't count:
	// the clone has no origin/<branch>, and its head isn't an ancestor of origin's.
	UnpushedSome = -1

	// UnpushedUnknown is a branch git can't compare with origin's: the clone has no
	// origin/<branch>, nor the commit origin's branch points at.
	UnpushedUnknown = -2
)

// The branches Release offers besides the default (D10), in the order the menu lists them.
const (
	branchMain   = "main"
	branchMaster = "master"
)

const (
	// goModFile is a Go module's file, at the clone's root, and moduleDirective the
	// directive in it that names the module.
	goModFile       = "go.mod"
	moduleDirective = "module"

	// goModLimit caps how much of go.mod, or any other small file, is read.
	goModLimit = 1 << 20

	// hookPrePush runs when magex pushes the tag, and hookPostCheckout when it switches
	// branches.
	hookPrePush      = "pre-push"
	hookPostCheckout = "post-checkout"
)

// releaseHooks are the hooks a release can run.
func releaseHooks() []string {
	return []string{hookPrePush, hookPostCheckout}
}

// otherBranches are the branches Release offers besides the default when origin has them.
func otherBranches() []string {
	return []string{branchMain, branchMaster}
}

// Facts is everything a release's offer depends on, gathered fresh each time: Decide
// works the offer out from them alone.
type Facts struct {
	Tool   ToolFacts
	Repo   RepoFacts
	Clone  CloneFacts
	Choice Choice
}

// branch is the branch the release tags: the one chosen, or the default on GitHub.
func (f Facts) branch() string {
	return cmp.Or(f.Choice.Branch, f.Repo.DefaultBranch)
}

// remoteHead is the commit origin's branch points at: as origin said just now, or, when
// origin couldn't be reached, as GitHub said for its default branch. It is "" when
// neither knows.
func (f Facts) remoteHead(branch string) string {
	if f.Clone.Reached {
		return f.Clone.RemoteHeads[branch]
	}

	if branch != "" && branch == f.Repo.DefaultBranch {
		return f.Repo.HeadOID
	}

	return ""
}

// ToolFacts is what Release knows of magex: where it is, its version, and what it can do.
type ToolFacts struct {
	// Path is the copy in use, absolute, or "" when there is none; Version is its
	// version as printed, without a leading v.
	Path    string
	Version string

	// State is what the settings say of it, and Off reports the user turned mage-x off:
	// only their switch says so, since automatic is on whenever the copy can be used, and a
	// copy too old to use is too old, not off.
	State integrations.State
	Off   bool

	// Features are the upstream changes this copy has.
	Features []integrations.Feature

	// MinVersion is the oldest version Release drives, and ExpectSince the first release
	// with expect=, or "" while none has it.
	MinVersion  string
	ExpectSince string
}

// ToolFactsOf reads magex's facts from the integrations' view of it: def is mage-x's
// definition and tool what the settings show of it.
func ToolFactsOf(def integrations.Definition, tool integrations.Tool) ToolFacts {
	out := ToolFacts{
		Path: tool.Copy.Path, Version: tool.Copy.Version.Raw, State: tool.State,
		Off:      tool.Switch == integrationprefs.SwitchOff,
		Features: []integrations.Feature{}, MinVersion: def.MinVersion,
		ExpectSince: def.Features[integrations.FeatureExpect],
	}

	for _, f := range slices.Sorted(maps.Keys(def.Features)) {
		if def.Has(f, tool.Copy.Version) {
			out.Features = append(out.Features, f)
		}
	}

	return out
}

// Usable reports a copy Release can run: on, found, and new enough.
func (t ToolFacts) Usable() bool {
	return !t.Off && t.Path != "" && t.State.Usable()
}

// Has reports whether the copy in use has feature f.
func (t ToolFacts) Has(f integrations.Feature) bool {
	return t.Usable() && slices.Contains(t.Features, f)
}

// RepoFacts is what GitHub says of the repository, and which of the branches Release
// offers besides the default origin has.
type RepoFacts struct {
	// Name is the repository.
	Name repo.Name

	// Error says, as a sentence, why GitHub couldn't be read, or is "" when it was;
	// Found reports GitHub shows the repository to the user.
	Error string
	Found bool

	// Archived and Permission are its state and the user's access, as GitHub says.
	Archived   bool
	Permission string

	// DefaultBranch is its default branch, and HeadOID and Checks its newest commit and
	// that commit's checks.
	DefaultBranch string
	HeadOID       string
	Checks        Checks

	// Branches are those of main and master that origin has (D10).
	Branches []string
}

// CloneFacts is what read-only git, and a few files, say of the clone the release runs in.
type CloneFacts struct {
	// Dir is the clone's folder, absolute.
	Dir string

	// Error says why git couldn't read the clone, or is "" when it could.
	Error string

	// OriginMatches reports that the clone's origin is the repository.
	OriginMatches bool

	// InProgress is what git is in the middle of, and Changes the uncommitted files.
	InProgress gitread.Operation
	Changes    []string

	// Branch is the branch checked out, and Detached reports a detached HEAD instead.
	Branch   string
	Detached bool

	// Elsewhere are the clone's other worktrees, with ~, that have the release's branch
	// checked out.
	Elsewhere []string

	// LocalHead is the release's branch in the clone, and TrackingHead origin/<branch>
	// as the clone last learned it; each is "" when the clone has none.
	LocalHead    string
	TrackingHead string

	// Upstream is what the branch is set to follow.
	Upstream gitread.Upstream

	// Unpushed counts the branch's commits origin's lacks, or is UnpushedSome or
	// UnpushedUnknown. Behind reports origin's branch has commits the clone's lacks.
	Unpushed int
	Behind   bool

	// Reached reports origin answered, and ReachError, when it didn't, what git said.
	Reached    bool
	ReachError string

	// RemoteHeads are origin's branches, and RemoteTags its tags, when it answered.
	RemoteHeads map[string]string
	RemoteTags  map[string]gitread.RemoteTag

	// LocalTags are the clone's version tags.
	LocalTags []string

	// Signing is whether, and how, git signs the release's tag here.
	Signing SigningFacts

	// Hooks are the hooks a release can run that the clone has: pre-push, post-checkout.
	Hooks []string

	// ShellRead reports that the login shell's environment was read.
	ShellRead bool

	// Busy reports another job holds the clone, and Holder which.
	Busy   bool
	Holder clonelock.Holder

	// ModulePath is go.mod's module path, or "" when the clone isn't a Go module.
	ModulePath string
}

// Choice is what the user chose on the sheet.
type Choice struct {
	// Bump is the release's bump, patch when empty (D11), and Branch the branch to
	// release, the default on GitHub when empty.
	Bump   Bump
	Branch string

	// Typed is what the user typed to confirm a major, and AllowModulePath reports the box
	// ticked to release a major that go.mod doesn't name.
	Typed           string
	AllowModulePath bool

	// RunsIn is where the run goes, and Terminal the terminal's name for a run there, as
	// in "iTerm2", or "" to say "your terminal".
	RunsIn   integrationprefs.RunsIn
	Terminal string
}

// ToolSource reads what Release knows of magex. Core adapts the integrations service, with
// ToolFactsOf. It never fails: a magex it can't read is missing.
type ToolSource interface {
	ReleaseTool(ctx context.Context) ToolFacts
}

// OriginReader reads which GitHub repository a clone's origin is, and whether it is one.
// Core adapts localrepos.Inspect.
type OriginReader interface {
	Origin(ctx context.Context, dir string) (name repo.Name, onGitHub bool, err error)
}

// LockHolders says which job holds a clone, if any: a *clonelock.Set.
type LockHolders interface {
	Holder(path string) (clonelock.Holder, bool)
}

// GatherConfig holds a Gatherer's dependencies. Git, GitHub, and Origins are required.
type GatherConfig struct {
	Git     *gitread.Git
	GitHub  GitHub
	Origins OriginReader
	Tools   ToolSource
	Locks   LockHolders

	// Env returns the environment ssh-add and ssh-keygen run with, and whether it is the
	// login shell's (shellenv.Reader.Env). It defaults to the app's own, counted as read.
	Env func() ([]string, bool)

	// Runner returns what runs ssh-add and ssh-keygen with env. It defaults to an
	// execpath.Exec that finds them on env's PATH.
	Runner func(env []string) execpath.Runner

	// Home is the user's home folder, for ~ paths.
	Home string
}

// Gatherer gathers a release's facts, reading GitHub, origin, and the clone at once.
type Gatherer struct {
	git     *gitread.Git
	github  GitHub
	origins OriginReader
	tools   ToolSource
	locks   LockHolders
	env     func() ([]string, bool)
	runner  func(env []string) execpath.Runner
	home    string
}

// NewGatherer returns a Gatherer over cfg.
func NewGatherer(cfg GatherConfig) *Gatherer {
	g := &Gatherer{
		git: cfg.Git, github: cfg.GitHub, origins: cfg.Origins, tools: cfg.Tools, locks: cfg.Locks,
		env: cfg.Env, runner: cfg.Runner, home: cfg.Home,
	}

	if g.env == nil {
		g.env = appEnv
	}

	if g.runner == nil {
		g.runner = toolRunner
	}

	return g
}

// GatherRequest names what to gather: the repository, its clone's folder, the user's
// choice, and whether the caller already holds the clone's lock, as Start does, so the
// lock it holds isn't counted as another job's.
type GatherRequest struct {
	Repo   repo.Name
	Dir    string
	Choice Choice
	Locked bool

	// GitHub, when set, is what GitHub said of the repository moments ago, used as it is
	// rather than asked again: Start, under the clone's lock, reads again only what the
	// release's version and readiness hang on, origin and the clone.
	GitHub *RepoFacts
}

// Gather reads every fact a release's offer depends on: magex, the repository on GitHub
// (unless req.GitHub gives it), origin (one ls-remote, which takes longest), and the
// clone, all at once. What it can't read becomes a fact, as an error the offer shows, so it
// fails only when ctx ends.
func (g *Gatherer) Gather(ctx context.Context, req GatherRequest) (Facts, error) {
	env, shellRead := g.env()
	f := Facts{Choice: req.Choice, Repo: RepoFacts{Name: req.Repo, Branches: []string{}}}

	var (
		wg    sync.WaitGroup
		known = make(chan struct{})
		r     cloneReads
	)

	wg.Go(func() { f.Tool = g.tool(ctx) })
	wg.Go(func() {
		defer close(known)

		if req.GitHub != nil {
			f.Repo = *req.GitHub

			return
		}

		f.Repo = g.repo(ctx, req.Repo)
	})
	wg.Go(func() { r.remote, r.remoteErr = g.git.RemoteRefs(ctx, req.Dir) })
	wg.Go(func() { r.branch, r.branchErr = g.git.CurrentBranch(ctx, req.Dir) })
	wg.Go(func() { r.op, r.opErr = g.git.InProgress(ctx, req.Dir) })
	wg.Go(func() { r.changes, r.changesErr = g.git.Changes(ctx, req.Dir) })
	wg.Go(func() { r.tags, r.tagsErr = g.git.LocalVersionTags(ctx, req.Dir) })
	wg.Go(func() { r.onto, r.ontoErr = g.branchReads(ctx, req, known, &f.Repo) })
	wg.Go(func() { r.signing, r.signingErr = g.signing(ctx, req.Dir, env) })
	wg.Go(func() { r.hooks, r.hooksErr = g.hooks(ctx, req.Dir) })
	wg.Go(func() { r.matches, r.originErr = g.originMatches(ctx, req) })
	wg.Go(func() { r.module = modulePath(req.Dir) })
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return Facts{}, err
	}

	f.Clone = r.facts(req.Dir, shellRead)
	f.Repo.Branches = originBranches(f.Clone.RemoteHeads)

	if err := g.settle(ctx, &f); err != nil && f.Clone.Error == "" {
		f.Clone.Error = gitMessage(err)
	}

	if g.locks != nil && !req.Locked {
		f.Clone.Holder, f.Clone.Busy = g.locks.Holder(req.Dir)
	}

	if err := ctx.Err(); err != nil {
		return Facts{}, err
	}

	return f, nil
}

// freshen reads again, into facts served a second time, what is cheap to read and changes
// on its own: magex, the shell's environment, and the clone's lock.
func (g *Gatherer) freshen(ctx context.Context, f *Facts) {
	f.Tool = g.tool(ctx)
	_, f.Clone.ShellRead = g.env()
	f.Clone.Holder, f.Clone.Busy = clonelock.Holder{}, false

	if g.locks != nil {
		f.Clone.Holder, f.Clone.Busy = g.locks.Holder(f.Clone.Dir)
	}
}

// tool reads magex's facts, or none without a source.
func (g *Gatherer) tool(ctx context.Context) ToolFacts {
	if g.tools == nil {
		return ToolFacts{Features: []integrations.Feature{}}
	}

	return g.tools.ReleaseTool(ctx)
}

// repo asks GitHub about the repository: one repository's status, round one.
func (g *Gatherer) repo(ctx context.Context, name repo.Name) RepoFacts {
	out := RepoFacts{Name: name, Branches: []string{}}

	got, err := g.github.ReleaseStatus(ctx, []repo.Name{name}, nil)
	if err != nil {
		out.Error = repoError(name, err)

		return out
	}

	status, found := got[name.Key()]
	out.Found, out.Archived, out.Permission = found, status.Archived, status.Permission
	out.DefaultBranch, out.HeadOID, out.Checks = status.DefaultBranch, status.HeadOID, status.Checks

	return out
}

// branchReads reads what the clone has of the release's branch: the chosen one, or,
// once GitHub has answered (known closes), the default.
func (g *Gatherer) branchReads(ctx context.Context, req GatherRequest, known <-chan struct{}, rf *RepoFacts,
) (branchFacts, error) {
	branch := req.Choice.Branch
	if branch == "" {
		select {
		case <-known:
			branch = rf.DefaultBranch
		case <-ctx.Done():
			return branchFacts{}, ctx.Err()
		}
	}

	if branch == "" {
		return branchFacts{elsewhere: []string{}}, nil
	}

	return g.readBranch(ctx, req.Dir, branch)
}

// readBranch reads branch in the clone at dir: the worktrees that have it, its head here
// and as origin's was last learned, what it follows, and its commits not on origin/<branch>.
func (g *Gatherer) readBranch(ctx context.Context, dir, branch string) (branchFacts, error) {
	var (
		out branchFacts
		err error
	)

	others, err := g.git.CheckedOutElsewhere(ctx, dir, branch)
	if err != nil {
		return branchFacts{}, err
	}

	out.elsewhere = make([]string, 0, len(others))
	for _, path := range others {
		out.elsewhere = append(out.elsewhere, paths.Tilde(path, g.home))
	}

	// origin/<branch> counts without the branch itself: magex makes the branch from it.
	if out.tracking, err = g.git.TrackingBranch(ctx, dir, branch); err != nil {
		return out, err
	}

	if out.local, err = g.git.LocalBranch(ctx, dir, branch); err != nil || out.local == "" {
		return out, err
	}

	if out.upstream, err = g.git.Upstream(ctx, dir, branch); err != nil {
		return out, err
	}

	if out.tracking != "" {
		out.unpushed, err = g.git.Unpushed(ctx, dir, branch)
	}

	return out, err
}

// hooks lists the hooks a release can run that the clone's hooks folder has, as git
// would run them: present and executable.
func (g *Gatherer) hooks(ctx context.Context, dir string) ([]string, error) {
	folder, err := g.git.HooksDir(ctx, dir)
	if err != nil {
		return nil, err
	}

	out := []string{}

	for _, name := range releaseHooks() {
		info, statErr := os.Stat(filepath.Join(folder, name))
		if statErr == nil && execpath.Executable(info.Mode()) {
			out = append(out, name)
		}
	}

	return out, nil
}

// originMatches reports whether the clone's origin is the repository.
func (g *Gatherer) originMatches(ctx context.Context, req GatherRequest) (bool, error) {
	name, onGitHub, err := g.origins.Origin(ctx, req.Dir)
	if err != nil {
		return false, err
	}

	return onGitHub && name.Key() == req.Repo.Key(), nil
}

// settle works out, once origin and the clone are read, how the release's branch stands
// with origin's: whether it is behind, and its commits origin lacks. A count from a stale
// origin/<branch> is set right when origin's newest commit has them all.
func (g *Gatherer) settle(ctx context.Context, f *Facts) error {
	c := &f.Clone
	if c.Error != "" || c.LocalHead == "" {
		return nil
	}

	head := f.remoteHead(f.branch())

	ancestor, known, err := g.within(ctx, c.Dir, c.LocalHead, head)
	if err != nil {
		return err
	}

	c.Behind = head != "" && head != c.LocalHead && (!known || ancestor)
	c.Unpushed = unpushedAfter(c.Unpushed, c.TrackingHead != "", known, ancestor)

	return nil
}

// unpushedAfter settles the count of a branch's commits origin's lacks: counted against
// origin/<branch> when the clone has it (tracked), and none when the branch is within
// origin's newest commit, which the clone has (known). Without either, it can't be told.
func unpushedAfter(counted int, tracked, known, ancestor bool) int {
	switch {
	case known && ancestor:
		return 0
	case tracked:
		return counted
	case !known:
		return UnpushedUnknown
	default:
		return UnpushedSome
	}
}

// within reports whether the commit local is head or an ancestor of it, and whether that
// could be told: the clone has head.
func (g *Gatherer) within(ctx context.Context, dir, local, head string) (ancestor, known bool, err error) {
	if head == "" {
		return false, false, nil
	}

	if known, err = g.git.HasCommit(ctx, dir, head); err != nil || !known {
		return false, false, err
	}

	ancestor, err = g.git.IsAncestor(ctx, dir, local, head)

	return ancestor, err == nil, err
}

// modulePath returns the module path the go.mod at the clone's root names, or "" when
// there is none or it can't be read.
func modulePath(dir string) string {
	data, err := readHead(filepath.Join(dir, goModFile), goModLimit)
	if err != nil {
		return ""
	}

	return parseModulePath(data)
}

// parseModulePath reads the module directive of a go.mod, quoted or not, or "".
func parseModulePath(data []byte) string {
	for line := range strings.Lines(string(data)) {
		line, _, _ = strings.Cut(line, "//")

		rest, ok := strings.CutPrefix(strings.TrimSpace(line), moduleDirective)
		if !ok || rest == "" || !strings.ContainsRune(" \t\"`", rune(rest[0])) {
			continue
		}

		path := strings.TrimSpace(rest)
		if unquoted, err := strconv.Unquote(path); err == nil {
			path = unquoted
		}

		return path
	}

	return ""
}

// readHead reads up to limit bytes of the file at path.
func readHead(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // a file in the clone or the user's own config, only read
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	return data, nil
}

// branchFacts is what the clone has of the release's branch.
type branchFacts struct {
	elsewhere []string
	local     string
	tracking  string
	upstream  gitread.Upstream
	unpushed  int
}

// cloneReads holds what each of Gather's reads of the clone found, and how each failed.
type cloneReads struct {
	remote     gitread.RemoteRefs
	remoteErr  error
	branch     string
	branchErr  error
	op         gitread.Operation
	opErr      error
	changes    []gitread.Change
	changesErr error
	tags       []string
	tagsErr    error
	onto       branchFacts
	ontoErr    error
	signing    SigningFacts
	signingErr error
	hooks      []string
	hooksErr   error
	matches    bool
	originErr  error
	module     string
}

// facts puts the reads together. The first failure, in a fixed order, is the clone's
// Error; origin's is its own, ReachError.
func (r cloneReads) facts(dir string, shellRead bool) CloneFacts {
	out := CloneFacts{
		Dir: dir, OriginMatches: r.matches, InProgress: r.op, Changes: make([]string, 0, len(r.changes)),
		Branch: r.branch, Detached: r.branchErr == nil && r.branch == "", Elsewhere: textutil.NonNil(r.onto.elsewhere),
		LocalHead: r.onto.local, TrackingHead: r.onto.tracking, Upstream: r.onto.upstream, Unpushed: r.onto.unpushed,
		Reached: r.remoteErr == nil, RemoteHeads: map[string]string{}, RemoteTags: map[string]gitread.RemoteTag{},
		LocalTags: textutil.NonNil(r.tags), Signing: r.signing, Hooks: textutil.NonNil(r.hooks), ShellRead: shellRead,
		ModulePath: r.module,
	}

	for _, ch := range r.changes {
		out.Changes = append(out.Changes, ch.Path)
	}

	if out.Reached {
		out.RemoteHeads, out.RemoteTags = textutil.NonNilMap(r.remote.Heads), textutil.NonNilMap(r.remote.Tags)
	} else {
		out.ReachError = gitMessage(r.remoteErr)
	}

	if err := cmp.Or(r.originErr, r.branchErr, r.opErr, r.changesErr, r.ontoErr, r.tagsErr, r.signingErr,
		r.hooksErr); err != nil {
		out.Error = gitMessage(err)
	}

	return out
}

// originBranches are those of the branches Release offers besides the default that
// origin has.
func originBranches(heads map[string]string) []string {
	out := []string{}

	for _, b := range otherBranches() {
		if heads[b] != "" {
			out = append(out, b)
		}
	}

	return out
}

// repoError says why GitHub couldn't be read about name, as a sentence.
func repoError(name repo.Name, err error) string {
	if errors.Is(err, ErrSignedOut) {
		return "Sign in to GitHub first, so Release can read " + name.String() + "."
	}

	if _, worded := errors.AsType[*uierr.Error](err); worded {
		return uierr.Message(err)
	}

	return "Couldn't read " + name.String() + " on GitHub."
}

// gitMessage is the part of a git failure worth showing: what git said, without the
// command and the exit status, or that origin didn't answer in time.
func gitMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "origin didn't answer within " + gitread.RemoteTimeout.String()
	}

	text := err.Error()
	if inner := errors.Unwrap(err); inner != nil {
		text = strings.TrimSuffix(text, ": "+inner.Error())
	}

	if rest, ok := strings.CutPrefix(text, "git "); ok {
		if _, said, found := strings.Cut(rest, ": "); found && said != "" {
			return said
		}
	}

	return text
}

// appEnv is the app's own environment, counted as read.
func appEnv() ([]string, bool) {
	return os.Environ(), true
}

// toolRunner is the default Runner: an execpath.Exec that runs with env and finds a
// program on env's PATH, then in the well-known tool folders under env's HOME.
func toolRunner(env []string) execpath.Runner {
	search := execpath.Search{Path: shellenv.Lookup(env, "PATH"), Home: shellenv.Lookup(env, "HOME")}

	return execpath.Exec{Search: search, Env: env}
}
