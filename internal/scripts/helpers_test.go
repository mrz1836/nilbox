package scripts

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// runWait bounds how long a test waits for a run to end, and runPoll how often it looks.
const (
	runWait = 30 * time.Second
	runPoll = 10 * time.Millisecond
)

// zshPath is a login shell of the POSIX family, and magexTest the command most tests run.
const (
	zshPath   = "/bin/zsh"
	magexTest = "magex test"
)

// rig is Repo scripts end to end: a clone root under a home of its own, a fake login shell
// and a fake magex, magex's recorded built-ins, the real runner over a fresh database, and
// the service, with its runs' Rerunner registered.
type rig struct {
	t       *testing.T
	home    string
	root    string
	scratch string
	db      *sql.DB

	clones   *fakeClones
	git      *fakeGit
	tools    *fakeTools
	settings *fakeSettings
	help     *fakeHelp
	locks    *clonelock.Set
	engine   *scriptrun.Engine
	runs     *scriptrun.Service
	svc      *Service

	// shell is the fake login shell, and env the environment every run gets, which
	// names each fake's script.
	shell string
	mu    sync.Mutex
	env   []string
}

// newRig builds a rig whose login shell and magex succeed.
func newRig(t *testing.T) *rig {
	t.Helper()

	home := testutil.RealTempDir(t)
	r := &rig{
		t: t, home: home, root: filepath.Join(home, "projects"), scratch: t.TempDir(), db: testutil.DB(t),
		clones: &fakeClones{origins: map[string]repo.Name{}}, git: &fakeGit{}, tools: &fakeTools{},
		settings: &fakeSettings{cfg: integrationprefs.Decode(nil)}, help: &fakeHelp{out: fixture(t, "magex-1.30.2-help-commands.txt")},
		locks: clonelock.New(), env: []string{"PATH=/usr/bin:/bin", "HOME=" + home},
	}
	require.NoError(t, os.MkdirAll(r.root, 0o700))
	r.clones.root = r.root

	r.engine = scriptrun.NewEngine(scriptrun.Config{
		DB: r.db, Locks: r.locks, LogDir: filepath.Join(t.TempDir(), "script-runs"), Home: home,
	})
	t.Cleanup(r.engine.StopAll)
	r.runs = scriptrun.NewService(r.engine)

	r.shell = r.fake("zsh", testutil.FakeRun{Stdout: "ran\n"})
	r.magex("1.30.2", testutil.FakeRun{Stdout: "magex ran\n"})

	r.svc = r.service(r.engine)
	r.engine.RegisterRerunner(scriptrun.KindScript, NewRerunner(r.svc))

	return r
}

// service builds the Service over runs.
func (r *rig) service(runs Runs) *Service {
	return NewService(Config{
		Clones: r.clones, Git: r.git, Branches: fakeBranches{"acme-org/sdk": "main"}, Tools: r.tools, Settings: r.settings,
		Runs: runs, History: r.runs, DB: r.db, Env: r.environ, Shell: func() string { return r.shell },
		Runner: r.help.runner, Scratch: r.scratch, Home: r.home,
	})
}

// fake makes a fake tool called name that does what run says, and returns its path.
func (r *rig) fake(name string, run testutil.FakeRun) string {
	r.t.Helper()

	path, entry := testutil.FakeTool(r.t, name, run)

	r.mu.Lock()
	r.env = append(r.env, entry)
	r.mu.Unlock()

	return path
}

// magex makes the copy of magex in use a fake at version that does what run says.
func (r *rig) magex(version string, run testutil.FakeRun) {
	path := r.fake("magex", run)
	r.tools.set(ToolFacts{CopyFacts: integrations.CopyFacts{Path: path, Version: version, State: integrations.StateFound}})
}

// environ is the login shell's environment.
func (r *rig) environ() ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.env), true
}

// clone makes a clone called name under the clone root with files, its origin on GitHub
// at origin when it isn't "", and returns its folder.
func (r *rig) clone(name, origin string, files map[string]string) string {
	r.t.Helper()

	dir := filepath.Join(r.root, name)
	require.NoError(r.t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))

	for rel, text := range files {
		writeText(r.t, filepath.Join(dir, filepath.FromSlash(rel)), text)
	}

	if origin != "" {
		n, err := repo.Parse(origin)
		require.NoError(r.t, err)
		r.clones.setOrigin(dir, n)
	}

	return dir
}

// shown is the folder as the page lists it, with ~.
func (r *rig) shown(dir string) string {
	rel, err := filepath.Rel(r.home, dir)
	require.NoError(r.t, err)

	return "~/" + filepath.ToSlash(rel)
}

// list returns the page for the clone at dir.
func (r *rig) list(dir string) Page {
	r.t.Helper()

	p, err := r.svc.List(r.t.Context(), r.shown(dir), false)
	require.NoError(r.t, err)

	return p
}

// entry returns the page's entry ref names.
func (r *rig) entry(dir, ref string) Entry {
	r.t.Helper()

	for _, sec := range r.list(dir).Sections {
		for _, e := range sec.Entries {
			if e.Ref == ref {
				return e
			}
		}
	}

	r.t.Fatalf("no entry %s", ref)

	return Entry{}
}

// ended waits for the run to end and let go of its clone, and returns it.
func (r *rig) ended(id int64) scriptrun.Run {
	r.t.Helper()

	var run scriptrun.Run

	require.Eventually(r.t, func() bool {
		got, err := r.runs.Run(r.t.Context(), id)
		if err != nil || got.Status.Live() {
			return false
		}

		run = got
		_, busy := r.locks.Holder(filepath.Join(r.home, strings.TrimPrefix(got.Dir, "~/")))

		return !busy
	}, runWait, runPoll, "run %d never ended", id)

	return run
}

// recorded returns how many runs the history holds.
func (r *rig) recorded() int {
	r.t.Helper()

	got, err := r.runs.Runs(r.t.Context(), scriptrun.Filter{})
	require.NoError(r.t, err)

	return len(got)
}

// fakeClones is the clone root: its folder, the clones made under it, and their origins.
type fakeClones struct {
	mu      sync.Mutex
	root    string
	origins map[string]repo.Name
}

func (f *fakeClones) Root(context.Context) (string, error) {
	return f.root, nil
}

// Dirs lists the folders under root that hold a .git, as localrepos.Clones does.
func (f *fakeClones) Dirs(_ context.Context, root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var out []string

	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil {
			out = append(out, dir)
		}
	}

	return out, nil
}

func (f *fakeClones) Origin(_ context.Context, dir string) (repo.Name, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	name, ok := f.origins[dir]

	return name, ok, nil
}

func (f *fakeClones) setOrigin(dir string, name repo.Name) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.origins[dir] = name
}

// fakeGit says every clone is on branch, with origin's HEAD naming head ("" for none), or
// failing to read it with headErr.
type fakeGit struct {
	mu      sync.Mutex
	branch  string
	head    string
	headErr error
}

func (f *fakeGit) CurrentBranch(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.branch, nil
}

func (f *fakeGit) OriginHead(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.head, f.headErr
}

func (f *fakeGit) set(branch string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.branch = branch
}

// setHead makes origin's HEAD name head, or fail to be read with err.
func (f *fakeGit) setHead(head string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.head, f.headErr = head, err
}

// fakeBranches are the default branches the app stored, by "owner/name".
type fakeBranches map[string]string

func (f fakeBranches) DefaultBranch(_ context.Context, name repo.Name) (string, error) {
	return f[name.String()], nil
}

// fakeTools is what the app knows of magex, and whether mage-x and go-pre-commit are on.
type fakeTools struct {
	mu    sync.Mutex
	tools Tools
}

func (f *fakeTools) Tools(context.Context) Tools {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tools
}

// magex returns the copy of magex in use.
func (f *fakeTools) magex() ToolFacts {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tools.Magex
}

// set makes facts the copy of magex in use, and mage-x on when it can be used.
func (f *fakeTools) set(facts ToolFacts) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tools.Magex, f.tools.MageXOn = facts, facts.Usable()
}

// on sets whether mage-x and go-pre-commit are on.
func (f *fakeTools) on(mageX, goPreCommit bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tools.MageXOn, f.tools.GoPreCommitOn = mageX, goPreCommit
}

// fakeSettings are the integrations' settings.
type fakeSettings struct {
	mu  sync.Mutex
	cfg integrationprefs.Config
}

func (f *fakeSettings) Integrations(context.Context) (integrationprefs.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.cfg, nil
}

func (f *fakeSettings) sources(magexTargets, repoFiles bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cfg.ScriptSources = integrationprefs.ScriptSources{MagexTargets: magexTargets, RepoFiles: repoFiles}
}

// scripts sets the Scripts integration's switch.
func (f *fakeSettings) scripts(sw integrationprefs.Switch) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.cfg.Switches = map[integrationprefs.ID]integrationprefs.Switch{integrationprefs.Scripts: sw}
}

// fakeHelp is magex's help:commands, answering with out, or failing with err, recording
// the folder and argv of each call.
type fakeHelp struct {
	mu    sync.Mutex
	out   string
	err   error
	dirs  []string
	argvs [][]string
}

func (f *fakeHelp) runner(_ []string, dir string) execpath.Runner {
	return &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.dirs = append(f.dirs, dir)
		f.argvs = append(f.argvs, argv)

		return []byte(f.out), nil, f.err
	}}
}

// answer makes help:commands answer with out, or fail with err.
func (f *fakeHelp) answer(out string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.out, f.err = out, err
}

func (f *fakeHelp) calls() (dirs []string, argvs [][]string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.dirs), slices.Clone(f.argvs)
}

// changeFirst is a Runs that changes a file just before the run starts: after the run was
// worked out, before its Before reads the source again under the clone's lock.
type changeFirst struct {
	runs   Runs
	change func()
}

func (c changeFirst) Start(ctx context.Context, spec scriptrun.Spec) (int64, error) {
	c.change()

	return c.runs.Start(ctx, spec)
}
