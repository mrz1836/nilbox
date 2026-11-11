package hooks

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// recordings is where P0 recorded the real go-pre-commit 1.13.1.
const recordings = "testdata/go-pre-commit-1.13.1"

// The hooks the tests write: go-pre-commit's own, as this copy writes it and as an older
// release did, and husky's, as S3 recorded it.
const (
	currentHook = "#!/bin/bash\n# Go Pre-commit Hook\n# This hook is managed by Go pre-commit system\nexec go-pre-commit run\n"
	oldHook     = currentHook + "# from an older release\n"
	huskyHook   = "#!/usr/bin/env sh\n. \"$(dirname -- \"$0\")/_/husky.sh\"\n\nnpx lint-staged\n"
)

// coreEnv is a modular config's first file, as the synced 00-core.env sets it.
const coreEnv = "ENABLE_GO_PRE_COMMIT=true\nGO_PRE_COMMIT_VERSION=v1.14.0\n"

// runWait bounds how long a test waits for a run to end, and runPoll how often it looks.
const (
	runWait = 30 * time.Second
	runPoll = 10 * time.Millisecond
)

var errFake = errors.New("fake: failed")

// recording is one recorded go-pre-commit run: its arguments, what it wrote, its exit
// status, and its lines in the order they arrived.
type recording struct {
	Name   string   `json:"name"`
	Argv   []string `json:"argv"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Exit   int      `json:"exit"`
	Lines  []struct {
		Stream string `json:"stream"`
		Text   string `json:"text"`
	} `json:"lines"`
}

// loadRecording reads the recording called name.
func loadRecording(t *testing.T, name string) recording {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(recordings, name+".json")) //nolint:gosec // a fixture the test names
	require.NoError(t, err)

	var rec recording
	require.NoError(t, json.Unmarshal(data, &rec))
	require.Equal(t, name, rec.Name)

	return rec
}

// recordingNames lists the recordings whose names start with prefix.
func recordingNames(t *testing.T, prefix string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(recordings, prefix+"*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, strings.TrimSuffix(filepath.Base(p), ".json"))
	}

	return names
}

// log returns the run's output as the app's log holds it: its lines in the order they
// arrived, each ended by a newline.
func (r recording) log() string {
	var b strings.Builder

	for _, l := range r.Lines {
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}

	return b.String()
}

// fakeRun is the recording as a fake go-pre-commit plays it, after files.
func (r recording) fakeRun(files ...testutil.FakeFile) testutil.FakeRun {
	return testutil.FakeRun{Stdout: r.log(), Exit: r.Exit, Files: files}
}

// rig is the Hooks page end to end: a clone root under a home of its own, clones made
// with git isolated from the user's config, a fake go-pre-commit, the real runner over a
// fresh database, and the service, with its runs' Rerunner registered.
type rig struct {
	t      *testing.T
	home   string
	root   string
	env    []string
	git    *gitread.Git
	clones *fakeClones
	tools  *fakeTools
	status *fakeStatus
	locks  *clonelock.Set
	engine *scriptrun.Engine
	runs   *scriptrun.Service
	memory *fakeMemory
	events *testutil.Events
	logs   *testutil.Logs
	svc    *Service

	// scratch is where go-pre-commit's status judges hooks.
	scratch string

	// db and logDir are the runner's history and its logs, which the next start reads.
	db     *sql.DB
	logDir string
}

// newRig builds a rig with go-pre-commit 1.13.1 replaying nothing, or skips the test
// without git.
func newRig(t *testing.T) *rig {
	t.Helper()

	testutil.RequireGit(t)

	home := testutil.RealTempDir(t)
	r := &rig{
		t: t, home: home, root: filepath.Join(home, "projects"), env: testutil.GitEnv(home),
		tools: &fakeTools{}, status: &fakeStatus{}, locks: clonelock.New(),
		memory: &fakeMemory{values: map[string]string{}}, events: &testutil.Events{}, logs: &testutil.Logs{},
		scratch: filepath.Join(t.TempDir(), "scratch"), db: testutil.DB(t), logDir: filepath.Join(t.TempDir(), "script-runs"),
	}
	require.NoError(t, os.MkdirAll(r.root, 0o700))

	r.git = gitread.New(gitread.Config{Env: func() []string { return r.env }})
	r.clones = &fakeClones{root: r.root, origins: map[string]repo.Name{}}

	r.engine = scriptrun.NewEngine(scriptrun.Config{
		DB: r.db, Locks: r.locks, LogDir: r.logDir, Home: home, Logger: r.logs.Logger(),
	})
	t.Cleanup(r.engine.StopAll)

	r.runs = scriptrun.NewService(r.engine)
	r.tool("1.13.1", testutil.FakeRun{})

	r.svc = NewService(Config{
		Clones: r.clones, Git: r.git, Tools: r.tools, Locks: r.locks, Runs: r.engine, History: r.runs,
		Places: fakePlaces{}, Memory: r.memory,
		Env:    func() ([]string, bool) { return append(slices.Clone(r.env), r.tools.envEntry()), true },
		Runner: r.status.runner, Scratch: r.scratch, Home: home, Events: r.events, Logger: r.logs.Logger(),
	})

	r.engine.RegisterRerunner(scriptrun.KindHooks, NewRerunner(r.svc))
	r.engine.RegisterRerunner(scriptrun.KindChecks, NewRerunner(r.svc))

	return r
}

// tool makes the copy of go-pre-commit in use a fake at version that does what run says.
func (r *rig) tool(version string, run testutil.FakeRun) string {
	r.t.Helper()

	path, env := testutil.FakeTool(r.t, "go-pre-commit", run)
	r.tools.set(factsAt(path, version), env)

	return path
}

// factsAt are the facts of a found copy of go-pre-commit at path and version, on the PATH.
func factsAt(path, version string) ToolFacts {
	def := integrations.GoPreCommit()

	return ToolFactsOf(def, integrations.Tool{
		ID: def.ID, On: true, State: integrations.StateFound,
		Copy: integrations.Copy{Path: path, Runs: true, Version: integrations.Version{Raw: version}, OnPath: true},
	}, true)
}

// cloneOpts shape a clone: its config, its hooks, and its origin.
type cloneOpts struct {
	// config is the modular config's 00-core.env, legacy the legacy .env.base; neither
	// leaves the clone without a config.
	config string
	legacy string

	// hook is its pre-commit hook, with mode (0o755 when zero), and hooks its other hooks
	// by name, each 0o755.
	hook  string
	mode  os.FileMode
	hooks map[string]string

	// hooksPath, when set, is core.hooksPath in the clone's own config: absolute, or
	// relative to the clone.
	hooksPath string

	// origin is the GitHub repository its origin is, "owner/name", or "".
	origin string
}

// clone makes a clone called name under the clone root, and returns its folder.
func (r *rig) clone(name string, o cloneOpts) string {
	r.t.Helper()

	dir := filepath.Join(r.root, name)
	testutil.InitRepo(r.t, dir)
	writeFile(r.t, dir, "README.md", "# "+name+"\n", 0o600)
	testutil.RunGit(r.t, dir, "add", "README.md")
	testutil.RunGit(r.t, dir, "commit", "-q", "-m", "first")

	if o.config != "" {
		writeFile(r.t, dir, ".github/env/00-core.env", o.config, 0o600)
	}

	if o.legacy != "" {
		writeFile(r.t, dir, ".github/.env.base", o.legacy, 0o600)
	}

	hooks := filepath.Join(dir, ".git", "hooks")

	if o.hooksPath != "" {
		testutil.RunGit(r.t, dir, "config", "core.hooksPath", o.hooksPath)

		hooks = o.hooksPath
		if !filepath.IsAbs(hooks) {
			hooks = filepath.Join(dir, hooks)
		}
	}

	if o.hook != "" {
		writeFile(r.t, hooks, hookPreCommit, o.hook, cmpMode(o.mode))
	}

	for n, text := range o.hooks {
		writeFile(r.t, hooks, n, text, 0o755)
	}

	if o.origin != "" {
		name, err := repo.Parse(o.origin)
		require.NoError(r.t, err)
		r.clones.setOrigin(dir, name)
	}

	return dir
}

// shown is the folder as the page lists it, with ~.
func (r *rig) shown(dir string) string {
	rel, err := filepath.Rel(r.home, dir)
	require.NoError(r.t, err)

	return "~/" + filepath.ToSlash(rel)
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

// page returns the page, failing the test when it can't be read.
func (r *rig) page(refresh bool) Page {
	r.t.Helper()

	p, err := r.svc.Repos(r.t.Context(), refresh)
	require.NoError(r.t, err)

	return p
}

// row returns the page's row of the clone at dir.
func row(t *testing.T, p Page, dir string) Repo {
	t.Helper()

	i := slices.IndexFunc(p.Repos, func(r Repo) bool { return filepath.Base(r.Dir) == filepath.Base(dir) })
	require.GreaterOrEqual(t, i, 0, "no row for %s", dir)

	return p.Repos[i]
}

// writeFile writes text to name in dir, making the folders it needs, with mode.
func writeFile(t *testing.T, dir, name, text string, mode os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(text), mode))
	require.NoError(t, os.Chmod(path, mode))
}

// readHookText returns the text of the pre-commit hook in the clone at dir.
func readHookText(t *testing.T, dir string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(hookInClone))) //nolint:gosec // the test's own clone
	require.NoError(t, err)

	return string(data)
}

// cmpMode returns mode, or 0o755 for none.
func cmpMode(mode os.FileMode) os.FileMode {
	if mode == 0 {
		return 0o755
	}

	return mode
}

// fakeClones is the clone root: its folder, the clones made under it, and their origins.
type fakeClones struct {
	mu      sync.Mutex
	root    string
	rootErr error
	dirsErr error
	origins map[string]repo.Name
	listed  int

	// rooted and asked count the reads of the root and of an origin; onDirs, when set, is
	// called as the clones are listed, and onOrigin as an origin is read.
	rooted   int
	asked    int
	onDirs   func()
	onOrigin func()
}

func (f *fakeClones) Root(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rooted++

	return f.root, f.rootErr
}

// Dirs lists the folders under root that hold a .git, as localrepos.Clones does.
func (f *fakeClones) Dirs(_ context.Context, root string) ([]string, error) {
	f.mu.Lock()
	f.listed++
	err, hook := f.dirsErr, f.onDirs
	f.mu.Unlock()

	if hook != nil {
		hook()
	}

	if err != nil {
		return nil, err
	}

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
	f.asked++
	name, ok := f.origins[dir]
	hook := f.onOrigin
	f.mu.Unlock()

	if hook != nil {
		hook()
	}

	return name, ok, nil
}

// counts returns how often the root and an origin were read.
func (f *fakeClones) counts() (rooted, asked int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.rooted, f.asked
}

// hook sets what is called as the clones are listed, and as an origin is read.
func (f *fakeClones) hook(onDirs, onOrigin func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.onDirs, f.onOrigin = onDirs, onOrigin
}

func (f *fakeClones) setOrigin(dir string, name repo.Name) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.origins[dir] = name
}

func (f *fakeClones) setDirsErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.dirsErr = err
}

func (f *fakeClones) listings() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.listed
}

// fakeTools is what the app knows of go-pre-commit, and the environment entry its fake
// needs.
type fakeTools struct {
	mu    sync.Mutex
	facts ToolFacts
	env   string
}

func (f *fakeTools) HooksTool(context.Context) ToolFacts {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.facts
}

func (f *fakeTools) set(facts ToolFacts, env string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.facts, f.env = facts, env
}

func (f *fakeTools) envEntry() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.env
}

// statusCall is one run of a fake go-pre-commit status: its argv, the folder it ran in,
// and the hook it found there.
type statusCall struct {
	argv []string
	dir  string
	hook string
}

// fakeStatus is go-pre-commit's status as a copy that writes current judges the hook in
// the folder it runs in: ready when it is current byte for byte, else outdated, as the
// real one said in status-ready and status-outdated. say, when set, names the recording it
// says instead, whatever the hook; withJSON has it answer as status --json does.
type fakeStatus struct {
	mu       sync.Mutex
	current  string
	say      string
	withJSON bool
	calls    []statusCall
}

// sayInstead makes every status say what the recording called name said.
func (f *fakeStatus) sayInstead(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.say = name
}

// answerJSON makes every status answer as status --json does.
func (f *fakeStatus) answerJSON() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.withJSON = true
}

// runs returns every status run so far.
func (f *fakeStatus) runs() []statusCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.calls)
}

// runner is the Service's Runner.
func (f *fakeStatus) runner(_ []string, dir string) execpath.Runner {
	return &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		hook, _ := os.ReadFile(filepath.Join(dir, ".git", "hooks", hookPreCommit)) //nolint:gosec // the scratch repository

		f.mu.Lock()
		defer f.mu.Unlock()

		f.calls = append(f.calls, statusCall{argv: argv, dir: dir, hook: string(hook)})

		current := string(hook) == cmp.Or(f.current, currentHook)

		if f.withJSON {
			return []byte(fmt.Sprintf(`{"version": "1.14.1", "enabled": true, "hooks": [
				{"name": "pre-commit", "state": %q, "executable": true, "outdated": %t}]}`,
				map[bool]string{true: statusReady, false: statusOutdated}[current], !current)), nil, nil
		}

		name := cmp.Or(f.say, map[bool]string{true: "status-ready", false: "status-outdated"}[current])
		rec := readStatusRecording(name)

		if rec.Exit != 0 {
			return []byte(rec.Stdout), []byte(rec.Stderr), errFake
		}

		return []byte(rec.Stdout), []byte(rec.Stderr), nil
	}}
}

// readStatusRecording reads the status recording called name, or returns an empty one.
func readStatusRecording(name string) recording {
	var rec recording

	data, err := os.ReadFile(filepath.Join(recordings, name+".json")) //nolint:gosec // a fixture
	if err == nil {
		_ = json.Unmarshal(data, &rec)
	}

	return rec
}

// fakePlaces says runs go in the app.
type fakePlaces struct {
	where integrationprefs.RunsIn
	err   error
}

func (f fakePlaces) RunsIn(context.Context) (integrationprefs.RunsIn, error) {
	return f.where, f.err
}

// fakeMemory is the hooks.* preferences.
type fakeMemory struct {
	mu     sync.Mutex
	values map[string]string
	err    error
}

func (f *fakeMemory) Get(_ context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	v, ok := f.values[key]

	return v, ok, f.err
}

func (f *fakeMemory) Set(_ context.Context, key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}

	f.values[key] = value

	return nil
}
