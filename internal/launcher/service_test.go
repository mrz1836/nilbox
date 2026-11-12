package launcher

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
)

// The programs the fake finder knows.
const (
	claudeBin = "claude"
	codexBin  = "codex"
)

// remoteLookupPattern is the only git config pattern the launcher may pass.
const remoteLookupPattern = `^remote\..*\.url$`

var (
	errGitNotAllowed = errors.New("git command not allowed in a launch")
	errFakeNoClaude  = errors.New("claude is not installed")
	errFakeSettings  = errors.New("settings unreadable")
	errFakeProfiles  = errors.New("profiles unreadable")
	errFakePrompts   = errors.New("prompts unreadable")
	errFakeDefaults  = errors.New("launch defaults unreadable")
	errFakeOpen      = errors.New("fake open failed")
)

// fakeDriver is a terminal driver that records the scripts it was asked to open.
type fakeDriver struct {
	id     terminal.ID
	err    error
	opened []string
}

func (d *fakeDriver) ID() terminal.ID { return d.id }

func (d *fakeDriver) Open(_ context.Context, scriptPath string) error {
	d.opened = append(d.opened, scriptPath)

	return d.err
}

// fakeGit answers the read-only remote lookup from a map of clone directory to
// `git config --get-regexp` output, and refuses every other git command.
type fakeGit struct {
	mu      sync.Mutex
	remotes map[string]string
	calls   [][]string
}

// Run records the argv and answers the remote lookup.
func (g *fakeGit) Run(_ context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	argv := append([]string{name}, args...)
	g.calls = append(g.calls, argv)

	if !allowedGitArgv(argv) {
		return nil, nil, fmt.Errorf("%w: %q", errGitNotAllowed, argv)
	}

	out, ok := g.remotes[args[1]]
	if !ok {
		return nil, nil, exitCodeError(1)
	}

	return []byte(out), nil, nil
}

// recorded returns a copy of every argv run so far.
func (g *fakeGit) recorded() [][]string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.calls)
}

// exitCodeError is an error carrying a process exit code, like *exec.ExitError.
type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// ExitCode returns the exit code.
func (e exitCodeError) ExitCode() int { return int(e) }

// allowedGitArgv reports whether argv is the read-only remote lookup
// `git -C <dir> config --get-regexp <remote url pattern>`.
func allowedGitArgv(argv []string) bool {
	return len(argv) == 6 && argv[0] == "git" && argv[1] == "-C" && filepath.IsAbs(argv[2]) &&
		argv[3] == "config" && argv[4] == "--get-regexp" && argv[5] == remoteLookupPattern
}

// failingPrompts cannot read the prompt library.
type failingPrompts struct{}

func (failingPrompts) Library(context.Context) (prompts.Library, error) {
	return prompts.Library{}, errFakePrompts
}

// countingPrompts reads a prompt library and counts the reads.
type countingPrompts struct {
	library PromptReader

	mu    sync.Mutex
	reads int
}

func (c *countingPrompts) Library(ctx context.Context) (prompts.Library, error) {
	c.mu.Lock()
	c.reads++
	c.mu.Unlock()

	return c.library.Library(ctx)
}

// count returns how many times the library was read.
func (c *countingPrompts) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.reads
}

// failingSettings cannot read the settings.
type failingSettings struct{}

func (failingSettings) Get(context.Context) (LaunchSettings, error) {
	return LaunchSettings{}, errFakeSettings
}

// launchSettings reads the launch settings from the settings service, as the app does.
type launchSettings struct {
	prefs *settings.Service
}

func (s launchSettings) Get(ctx context.Context) (LaunchSettings, error) {
	all, err := s.prefs.Get(ctx)
	if err != nil {
		return LaunchSettings{}, err
	}

	return LaunchSettings{
		DefaultAgent: all.DefaultAgent, DefaultProfiles: all.DefaultProfiles, WorktreeDefault: all.WorktreeDefault,
		Terminal: all.Terminal, CloneRoot: all.CloneRoot, TrustedBots: all.TrustedBots,
		EffortOverrides: all.CodexEffort, FastMode: all.CodexFast,
	}, nil
}

// fakeMaker stands in for the worktrees package's Maker: it makes each item's worktree
// directory beside the clone the first time and resumes it after, and records what it
// was asked.
type fakeMaker struct {
	mu      sync.Mutex
	planned []WorktreeRequest
	ensured []WorktreeRequest
	err     error

	// head is the commit an existing worktree has checked out.
	head string

	// notice is what the fake says it worked around, or "".
	notice string
}

// worktreeOf is where the fake makes a request's worktree.
func worktreeOf(req WorktreeRequest) string {
	return filepath.Join(filepath.Dir(req.Clone), "."+brand.Slug, "worktrees", filepath.Base(req.Clone),
		item.WorktreeName(req.Kind, req.Number))
}

func (m *fakeMaker) PlanWorktree(_ context.Context, req WorktreeRequest) (Worktree, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.planned = append(m.planned, req)

	return m.answer(req), nil
}

func (m *fakeMaker) EnsureWorktree(_ context.Context, req WorktreeRequest) (Worktree, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return Worktree{}, m.err
	}

	m.ensured = append(m.ensured, req)
	wt := m.answer(req)

	if err := os.MkdirAll(wt.Path, 0o700); err != nil {
		return Worktree{}, err
	}

	return wt, nil
}

// answer is what the fake finds or would make for req.
func (m *fakeMaker) answer(req WorktreeRequest) Worktree {
	path := worktreeOf(req)
	wt := Worktree{Path: path, Branch: "worktree-" + filepath.Base(path), Action: WorktreeCreate, Dirty: 0, Notice: m.notice}

	if _, err := os.Stat(path); err == nil {
		wt.Action, wt.Head = WorktreeResume, m.head
	} else if req.Kind == item.KindPR {
		wt.Base = "the pull request's head"
	} else {
		wt.Base = req.Remote + "/main"
	}

	return wt
}

// ensures returns a copy of the requests EnsureWorktree was asked.
func (m *fakeMaker) ensures() []WorktreeRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.ensured)
}

// session is one session a launch recorded.
type session struct {
	clone, worktree string
	agent           agent.ID
	at              time.Time
}

// fakeSessions records the sessions launches start, or fails.
type fakeSessions struct {
	mu       sync.Mutex
	sessions []session
	err      error
}

func (f *fakeSessions) Touch(_ context.Context, clonePath, worktree string, id agent.ID, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}

	f.sessions = append(f.sessions, session{clone: clonePath, worktree: worktree, agent: id, at: at})

	return nil
}

// testSessionID is the ID the harness mints for every session.
const testSessionID = "0f8c1e2a-5b4d-4c3e-9a1f-2b3c4d5e6f70"

// fakeLedger records the sessions launches start, and answers Resumable from what it
// holds.
type fakeLedger struct {
	mu        sync.Mutex
	started   []StartedSession
	resumable map[string]ResumableSession
	running   []runningAt
	err       error
	runErr    error
	asked     []string
}

// runningAt is a session the fake ledger says runs in the folder dir.
type runningAt struct {
	dir     string
	session RunningSession
}

func (f *fakeLedger) Running(_ context.Context, dir string) ([]RunningSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, dir)

	out := []RunningSession{}

	for _, r := range f.running {
		if paths.Same(r.dir, dir) {
			out = append(out, r.session)
		}
	}

	return out, f.runErr
}

func (f *fakeLedger) Started(_ context.Context, s StartedSession) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}

	f.started = append(f.started, s)

	return nil
}

func (f *fakeLedger) Resumable(_ context.Context, id string) (ResumableSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.resumable[id]
	if !ok {
		return ResumableSession{}, errUnknownSession
	}

	return s, nil
}

// setRunning replaces the sessions the ledger says run, each in its folder.
func (f *fakeLedger) setRunning(running ...runningAt) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.running = running
}

// all returns the sessions recorded so far.
func (f *fakeLedger) all() []StartedSession {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.started)
}

// errUnknownSession is what fakeLedger fails Resumable with for an ID it does not hold.
var errUnknownSession = errors.New("no such session")

// inboxItems reads the launch items from the inbox's store, as the app does.
type inboxItems struct {
	*inbox.Store
}

func (s inboxItems) LaunchItems(ctx context.Context, ids []int64) ([]StoredItem, error) {
	stored, err := s.Store.LaunchItems(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]StoredItem, 0, len(stored))
	for _, it := range stored {
		items = append(items, StoredItem(it))
	}

	return items, nil
}

// failingProfiles cannot read the stored profiles.
type failingProfiles struct{}

func (failingProfiles) All(context.Context) ([]profiles.Profile, error) {
	return nil, errFakeProfiles
}

// failingDefaults cannot read the launch defaults.
type failingDefaults struct{}

func (failingDefaults) All(context.Context) ([]launchdefaults.Defaults, error) {
	return nil, errFakeDefaults
}

// launchHarness is a launch Service over a temp database, a temp home with one clone,
// and fakes for git, the agent lookup, and the terminals.
type launchHarness struct {
	pool     *sql.DB
	items    *inbox.Store
	profiles *profiles.Store
	prompts  *prompts.Store
	defaults *launchdefaults.Store
	home     string
	root     string
	clone    string
	paths    appdata.Paths
	git      *fakeGit
	iterm    *fakeDriver
	terminal *fakeDriver
	settings *settings.Service
	maker    *fakeMaker
	sessions *fakeSessions
	ledger   *fakeLedger
	cfg      Config

	// Item IDs: a trusted PR, an outside contributor's PR, a trusted issue, and a PR in a
	// repository with no local clone.
	trustedPR, outsidePR, issue, orphanPR int64
}

func newLaunchHarness(t *testing.T) *launchHarness {
	t.Helper()

	tmp := t.TempDir()
	h := &launchHarness{
		home:     filepath.Join(tmp, "home"),
		paths:    appdata.NewPaths(filepath.Join(tmp, "data")),
		iterm:    &fakeDriver{id: terminal.ITerm2},
		terminal: &fakeDriver{id: terminal.TerminalApp},
		maker:    &fakeMaker{},
		sessions: &fakeSessions{},
		ledger:   &fakeLedger{},
	}
	h.root = filepath.Join(h.home, "projects")
	h.clone = filepath.Join(h.root, exampleRepo)
	h.git = &fakeGit{remotes: map[string]string{
		h.clone: "remote.origin.url https://github.com/acme-org/example-repo.git\n",
	}}

	makeRepoDir(t, h.clone)

	pool := testutil.DBAt(t, h.paths.DB)
	h.pool = pool
	h.items = inbox.NewStore(pool, testutil.NewClock(fixedNow()))
	h.profiles = profiles.NewStore(pool, nil)
	h.prompts = prompts.NewStore(pool, nil)
	h.defaults = launchdefaults.NewStore(pool, nil)
	h.seed(t)

	h.settings = settings.NewService(pool, h.profiles, h.home)
	h.cfg = Config{
		Items:    inboxItems{Store: h.items},
		Settings: launchSettings{prefs: h.settings},
		Profiles: h.profiles,
		Prompts:  h.prompts,
		Defaults: h.defaults,
		Git:      h.git,
		Finder: testutil.Finder{Paths: map[string]string{
			claudeBin: filepath.Join(tmp, "bin", "claude"), codexBin: filepath.Join(tmp, "bin", "codex"),
		}},
		Registry:  terminal.NewRegistry(h.iterm, h.terminal),
		Paths:     h.paths,
		Home:      h.home,
		Clock:     testutil.NewClock(fixedNow()),
		Worktrees: h.maker,
		Sessions:  h.sessions,
		Ledger:    h.ledger,
		NewID:     func() string { return testSessionID },
		Trust: map[agent.ID]Truster{
			agent.Claude: workspacetrust.Claude{Home: h.home},
			agent.Codex:  workspacetrust.Codex{Home: h.home},
		},
	}

	return h
}

// makeRepoDir creates a directory that looks like a git working tree.
func makeRepoDir(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
}

// seed stores an account, two repositories, the test items, and two profiles.
func (h *launchHarness) seed(t *testing.T) {
	t.Helper()

	stored := h.store(t,
		storedItem(exampleRepo, item.KindPR, 7, "OWNER", false),
		storedItem(exampleRepo, item.KindPR, 8, "CONTRIBUTOR", true),
		storedItem(exampleRepo, item.KindIssue, 3, "MEMBER", false),
		storedItem("missing-repo", item.KindPR, 9, "OWNER", false),
	)
	h.trustedPR, h.outsidePR, h.issue, h.orphanPR = stored[0], stored[1], stored[2], stored[3]

	work := profiles.Profile{
		Agent: agent.Claude, Name: "work", ConfigDir: filepath.Join(h.home, ".claude_work"), PermissionMode: "acceptEdits",
		AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{},
	}
	full := profiles.Profile{
		Agent: agent.Claude, Name: profiles.DefaultName, PermissionMode: profiles.PermissionFull,
		AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{},
	}
	codexFull, codexWork := full, work
	codexFull.Agent = agent.Codex
	codexWork.Agent, codexWork.ConfigDir, codexWork.PermissionMode = agent.Codex, filepath.Join(h.home, ".codex_work"),
		"workspace-write"
	require.NoError(t, h.profiles.Sync(t.Context(), []profiles.Profile{full, work, codexFull, codexWork}))
	require.NoError(t, prompts.Seed(t.Context(), h.prompts))
}

// storedItem is a pull request or issue by octo-user in an acme-org repository, as the
// inbox stores it.
func storedItem(repoName string, kind item.Kind, number int, assoc string, cross bool) github.Item {
	return github.Item{
		IsPR: kind == item.KindPR, Number: number, Title: fmt.Sprintf("Change %d", number),
		URL: fmt.Sprintf("https://github.com/acme-org/x/%d", number), CreatedAt: fixedNow(), UpdatedAt: fixedNow(),
		HeadRef: "feature-branch", AuthorLogin: "octo-user", AuthorAssociation: assoc, IsCrossRepository: cross,
		RepoOwner: "acme-org", RepoName: repoName,
	}
}

// store stores items for octo-user, in no list, and returns their ids.
func (h *launchHarness) store(t *testing.T, items ...github.Item) []int64 {
	t.Helper()

	fixture := inbox.Fixture{Viewer: "octo-user", At: fixedNow()}
	for _, it := range items {
		fixture.Items = append(fixture.Items, inbox.FixtureItem{Item: it})
	}

	ids, err := inbox.StoreFixture(t.Context(), h.pool, fixture)
	require.NoError(t, err)

	return ids
}

// exec runs a statement on the harness's database.
func (h *launchHarness) exec(t *testing.T, query string, args ...any) {
	t.Helper()

	_, err := h.pool.ExecContext(t.Context(), query, args...)
	require.NoError(t, err)
}

// service returns a launch Service over the harness's current config.
func (h *launchHarness) service() *Service {
	return NewService(h.cfg)
}

// launch runs one launch and requires it to succeed.
func (h *launchHarness) launch(t *testing.T, req LaunchRequest) (LaunchResult, string) {
	t.Helper()

	res, err := h.service().Launch(t.Context(), req)
	require.NoError(t, err)

	script, err := os.ReadFile(res.ScriptPath)
	require.NoError(t, err)

	return res, string(script)
}

// cachedPath returns the clone path stored for a repository.
func (h *launchHarness) cachedPath(t *testing.T, name string) string {
	t.Helper()

	var path string
	require.NoError(t, h.pool.QueryRowContext(t.Context(), "SELECT local_path FROM repos WHERE name = ?", name).
		Scan(&path))

	return path
}

// launchFiles lists the files in the launch directory.
func (h *launchHarness) launchFiles(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(h.paths.Launch)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

// promptFor returns the prompt file a script reads.
func promptFor(t *testing.T, scriptPath string) string {
	t.Helper()

	prompt, err := os.ReadFile(strings.TrimSuffix(scriptPath, ".command") + terminal.PromptSuffix)
	require.NoError(t, err)

	return string(prompt)
}

func TestLaunchWritesFilesAndOpensDriver(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	res, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	require.Equal(t, []string{res.ScriptPath}, h.iterm.opened, "iTerm2 is the default terminal")
	assert.Empty(t, h.terminal.opened)

	assert.Equal(t, h.paths.Launch, filepath.Dir(res.ScriptPath))
	assert.True(t, strings.HasSuffix(res.ScriptPath, ".command"))
	assert.Equal(t, LaunchResult{
		ScriptPath:     res.ScriptPath,
		ClonePath:      "~/projects/example-repo",
		Dir:            "~/projects/." + brand.Slug + "/worktrees/example-repo/pr-7",
		Worktree:       "pr-7",
		WorktreeAction: WorktreeCreate,
		Prompt:         "Review",
		Profile:        profiles.DefaultName,
		Permission:     "full access",
		Terminal:       settings.TerminalITerm2,
		Agent:          agent.Claude,
		SessionID:      testSessionID,
	}, res)

	worktree := filepath.Join(h.root, "."+brand.Slug, "worktrees", exampleRepo, "pr-7")
	assert.Contains(t, script, "cd -- "+shQuote(worktree)+" || exit 1\n", "the session starts in the item's worktree")
	assert.Contains(t, script, "unset CLAUDE_CONFIG_DIR\n")
	assert.Contains(t, script, `exec '`+filepath.Join(filepath.Dir(h.home), "bin", "claude")+`' "$prompt" `+
		"'--dangerously-skip-permissions' '--name=Review · example-repo#7' '--session-id="+testSessionID+"'\n",
		"no agent worktree flag, and the app's own session ID")
	assert.NotContains(t, script, "--worktree")
	assert.Equal(t, []WorktreeRequest{{
		Clone: h.clone, Remote: "origin", Owner: "acme-org", Name: exampleRepo, Kind: item.KindPR, Number: 7,
	}}, h.maker.ensures())
	assert.Contains(t, promptFor(t, res.ScriptPath), "Review pull request #7 in acme-org/example-repo: Change 7")
	assert.Len(t, h.launchFiles(t), 2, "one script and one prompt file")

	assert.Equal(t, h.clone, h.cachedPath(t, exampleRepo), "the resolved clone is cached")
}

func TestWorktreePrefDefaultAndUntick(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pref bool
		req  *bool
		want bool
	}{
		{name: "pref on, request unset", pref: true, req: nil, want: true},
		{name: "pref on, request on", pref: true, req: new(true), want: true},
		{name: "pref on, request unticked", pref: true, req: new(false), want: false},
		{name: "pref off, request unset", pref: false, req: nil, want: false},
		{name: "pref off, request on", pref: false, req: new(true), want: true},
		{name: "pref off, request off", pref: false, req: new(false), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			require.NoError(t, testutil.ErrOf(h.settings.SetWorktreeDefault(t.Context(), tt.pref)))

			res, script := h.launch(t, LaunchRequest{ItemID: h.issue, Worktree: tt.req})
			prompt := promptFor(t, res.ScriptPath)

			if !tt.want {
				assert.Empty(t, res.Worktree)
				assert.Contains(t, script, "cd -- "+shQuote(h.clone)+" || exit 1\n", "the session starts in the clone")
				assert.NotContains(t, prompt, "fresh worktree")
				assert.Empty(t, h.maker.ensures(), "no worktree is made")

				return
			}

			assert.Equal(t, "issue-3", res.Worktree)
			assert.Contains(t, script, "cd -- "+shQuote(filepath.Join(h.root, "."+brand.Slug, "worktrees", exampleRepo, "issue-3")))
			assert.Contains(t, prompt, "fresh worktree")
		})
	}

	// Unticking applies to one launch only: the next launch follows the pref again.
	h := newLaunchHarness(t)

	res, _ := h.launch(t, LaunchRequest{ItemID: h.issue, Worktree: new(false)})
	assert.Empty(t, res.Worktree)

	res, _ = h.launch(t, LaunchRequest{ItemID: h.issue})
	assert.Equal(t, "issue-3", res.Worktree)
}

func TestLauncherRunsNoGitCommands(t *testing.T) {
	t.Parallel()

	forbidden := [][]string{
		{"git", "-C", "/tmp/clone", "checkout", "feature-branch"},
		{"git", "-C", "/tmp/clone", "stash"},
		{"git", "-C", "/tmp/clone", "fetch", "origin"},
		{"git", "-C", "/tmp/clone", "switch", "main"},
		{"git", "-C", "/tmp/clone", "worktree", "add", "/tmp/wt"},
		{"git", "-C", "/tmp/clone", "config", "remote.origin.url", "https://example.com/x.git"},
		{"git", "-C", "/tmp/clone", "config", "--get-regexp", ".*"},
		{"gh", "pr", "checkout", "7"},
		{"git", "config", "--get-regexp", remoteLookupPattern},
	}
	for _, argv := range forbidden {
		assert.False(t, allowedGitArgv(argv), "the fake git refuses %q", argv)
	}

	h := newLaunchHarness(t)
	scriptWords := regexp.MustCompile(`(^|[\s'/])(git|gh)(['\s]|$)|checkout|stash|fetch|switch`)

	// The first launch scans the clone root; the second validates the cached clone.
	for i, id := range []int64{h.trustedPR, h.trustedPR, h.issue} {
		before := len(h.git.recorded())
		res, script := h.launch(t, LaunchRequest{ItemID: id})
		calls := h.git.recorded()[before:]

		require.NotEmpty(t, calls, "launch %d looks up the clone's remotes", i)

		for _, argv := range calls {
			assert.True(t, allowedGitArgv(argv), "launch %d ran a git command other than the remote lookup: %q", i, argv)
		}

		assert.False(t, scriptWords.MatchString(script), "launch %d script runs git or gh:\n%s", i, script)
		assert.Contains(t, script, "exec ")

		if id == h.trustedPR {
			assert.Contains(t, promptFor(t, res.ScriptPath), "gh pr checkout 7",
				"the prompt, not the script, tells the agent to check out the pull request")
		}
	}

	for _, argv := range h.git.recorded() {
		assert.Equal(t, h.clone, argv[2], "git only ever reads the clone's config")
	}
}

func TestLaunchCloneNotFound(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.orphanPR})
	require.ErrorIs(t, err, localrepos.ErrCloneNotFound)
	assert.Contains(t, err.Error(), "acme-org/missing-repo")
	assert.Contains(t, err.Error(), "~/projects")

	assert.Empty(t, h.iterm.opened)
	assert.Empty(t, h.launchFiles(t), "nothing is written for a failed launch")
	assert.Empty(t, h.cachedPath(t, "missing-repo"))
}

// trustedIn reports whether the agent config file at path trusts dir.
func trustedIn(t *testing.T, path, dir string) bool {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // a test file under a temp dir
	require.NoError(t, err)

	var config struct {
		Projects map[string]struct {
			HasTrustDialogAccepted bool `json:"hasTrustDialogAccepted"`
		} `json:"projects"`
	}
	require.NoError(t, json.Unmarshal(data, &config))

	return config.Projects[dir].HasTrustDialogAccepted
}

func TestLaunchTrustsTheFolderForTheAgent(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	defaultConfig := filepath.Join(h.home, ".claude.json")
	workDir := filepath.Join(h.home, ".claude_work")

	clone, err := filepath.EvalSymlinks(h.clone)
	require.NoError(t, err)

	_, err = h.service().Preview(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	assert.NoFileExists(t, defaultConfig, "a preview trusts nothing")

	h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	worktree, err := filepath.EvalSymlinks(filepath.Join(h.root, "."+brand.Slug, "worktrees", exampleRepo, "pr-7"))
	require.NoError(t, err)
	assert.True(t, trustedIn(t, defaultConfig, worktree), "the default profile's config, in home, trusts the worktree")
	assert.True(t, trustedIn(t, defaultConfig, clone), "and the clone it belongs to")

	// A profile the agent has never run with is left for the agent to set up.
	h.launch(t, LaunchRequest{ItemID: h.trustedPR, Profile: profileWork})
	assert.NoDirExists(t, workDir)

	require.NoError(t, os.Mkdir(workDir, 0o700))
	h.launch(t, LaunchRequest{ItemID: h.outsidePR, Profile: profileWork, Worktree: new(false)})
	assert.True(t, trustedIn(t, filepath.Join(workDir, ".claude.json"), clone),
		"the profile's own config trusts the clone, for every item and with or without a worktree")
}

func TestLaunchRecordsTheSession(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	h.launch(t, LaunchRequest{ItemID: h.issue, Agent: agent.Codex})
	h.launch(t, LaunchRequest{ItemID: h.trustedPR, Worktree: new(false)})

	assert.Equal(t, []session{
		{clone: h.clone, worktree: "pr-7", agent: agent.Claude, at: fixedNow()},
		{clone: h.clone, worktree: "issue-3", agent: agent.Codex, at: fixedNow()},
	}, h.sessions.sessions, "each session in a worktree, with its agent; none for a launch in the clone")

	// A session that cannot be recorded has still started.
	h.cfg.Sessions = &fakeSessions{err: errFakeSettings}
	h.launch(t, LaunchRequest{ItemID: h.trustedPR})
}

func TestLaunchMakesTheWorktreeOnlyOnceItIsNotHeldBack(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.maker.err = errFakeOpen

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.ErrorIs(t, err, errFakeOpen, "a worktree that cannot be made stops the launch")
	assert.Empty(t, h.launchFiles(t), "nothing is written")
	assert.Empty(t, h.iterm.opened)
	assert.NoFileExists(t, filepath.Join(h.home, ".claude.json"), "nothing is trusted")
}

func TestLaunchCodex(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	codexHome := filepath.Join(h.home, ".codex_work")
	require.NoError(t, os.MkdirAll(codexHome, 0o700))

	res, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR, Agent: agent.Codex, Profile: profileWork})
	assert.Equal(t, agent.Codex, res.Agent)
	assert.Equal(t, "permission mode workspace-write", res.Permission)

	worktree := filepath.Join(h.root, "."+brand.Slug, "worktrees", exampleRepo, "pr-7")
	assert.Contains(t, script, "cd -- "+shQuote(worktree)+" || exit 1\n")
	assert.Contains(t, script, "export CODEX_HOME="+shQuote(codexHome)+"\n")
	assert.Regexp(t, `exec '[^']*/codex' '--sandbox' 'workspace-write' '--' "\$prompt"\n$`, script,
		"the prompt comes last, after --, and Codex names no session")
	assert.NotContains(t, script, "--name=")

	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml")) //nolint:gosec // the test's own file
	require.NoError(t, err)

	resolved, err := filepath.EvalSymlinks(worktree)
	require.NoError(t, err)
	assert.Contains(t, string(data), "[projects.\""+resolved+"\"]\ntrust_level = \"trusted\"\n",
		"the worktree is trusted in the profile's Codex config")

	// An outside contributor's pull request gets Codex's read-only sandbox.
	res, script = h.launch(t, LaunchRequest{ItemID: h.outsidePR, Agent: agent.Codex})
	assert.Equal(t, "read-only (outside contributor)", res.Permission)
	assert.Contains(t, script, "'--sandbox' 'read-only' '--ask-for-approval' 'on-request' '--' \"$prompt\"")
	assert.Contains(t, script, "unset CODEX_HOME\n")
	assert.NotContains(t, script, "dangerously")
}

func TestLaunchFollowsTheDefaultAgent(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	require.NoError(t, testutil.ErrOf(h.settings.SetDefaultAgent(t.Context(), agent.Codex)))
	require.NoError(t, testutil.ErrOf(h.settings.SetDefaultProfile(t.Context(), agent.Codex, profileWork)))

	res, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, agent.Codex, res.Agent, "the app's default agent")
	assert.Equal(t, profileWork, res.Profile, "with the app's default profile for that agent")
	assert.Contains(t, script, "CODEX_HOME")

	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR, Agent: agent.Claude})
	assert.Equal(t, agent.Claude, res.Agent, "the launch menu's switch picks the agent for one launch")
	assert.Equal(t, profiles.DefaultName, res.Profile, "Claude's own default profile")

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Agent: "example-agent"})
	require.ErrorIs(t, err, agent.ErrUnknownAgent)
}

func TestLaunchSaysWhichAgentIsNotInstalled(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.cfg.Finder = testutil.Finder{Paths: map[string]string{claudeBin: "/opt/bin/claude"}}

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Agent: agent.Codex})
	require.ErrorIs(t, err, testutil.ErrNotFound)
	assert.Equal(t, "Codex isn't installed (codex wasn't found).", uierr.Message(err))
	assert.Empty(t, h.maker.ensures())
}

func TestLaunchUsesTerminalPref(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	require.NoError(t, testutil.ErrOf(h.settings.SetTerminal(t.Context(), settings.TerminalApp)))

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	assert.Equal(t, []string{res.ScriptPath}, h.terminal.opened)
	assert.Empty(t, h.iterm.opened)
	assert.Equal(t, settings.TerminalApp, res.Terminal)
}

func TestLaunchOutsideContributorIsPlanMode(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	res, script := h.launch(t, LaunchRequest{ItemID: h.outsidePR})
	assert.Equal(t, "plan mode (outside contributor)", res.Permission)
	assert.Contains(t, script, "'--permission-mode' 'plan'")
	assert.NotContains(t, script, "dangerously")

	res, script = h.launch(t, LaunchRequest{ItemID: h.outsidePR, FullAccessOverride: true})
	assert.Equal(t, "full access", res.Permission, "the override uses the profile's own permissions")
	assert.Contains(t, script, "'--dangerously-skip-permissions'")

	res, script = h.launch(t, LaunchRequest{ItemID: h.outsidePR, Profile: "work", FullAccessOverride: true})
	assert.Equal(t, "permission mode acceptEdits", res.Permission, "the override keeps a restricted profile restricted")
	assert.Contains(t, script, "'--permission-mode' 'acceptEdits'")
	assert.NotContains(t, script, "dangerously")
}

func TestLaunchProfileSelection(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	workDir := filepath.Join(h.home, ".claude_work")

	res, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR, Profile: "work"})
	assert.Equal(t, "work", res.Profile)
	assert.Contains(t, script, "export CLAUDE_CONFIG_DIR="+shQuote(workDir)+"\n")

	require.NoError(t, testutil.ErrOf(h.settings.SetDefaultProfile(t.Context(), agent.Claude, "work")))

	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "work", res.Profile, "an empty profile uses the default profile setting")

	res, script = h.launch(t, LaunchRequest{ItemID: h.trustedPR, Profile: profiles.DefaultName})
	assert.Equal(t, profiles.DefaultName, res.Profile, "a picked profile applies to this launch")
	assert.Contains(t, script, "unset CLAUDE_CONFIG_DIR\n")

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Profile: "nope"})
	require.ErrorIs(t, err, profiles.ErrProfileNotFound)
}

func TestLaunchRevalidatesTheCachedClone(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	stale := filepath.Join(h.root, "gone")
	h.exec(t, "UPDATE repos SET local_path = ? WHERE name = ?", stale, exampleRepo)

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "~/projects/example-repo", res.ClonePath, "a vanished cached clone is found again")
	assert.Equal(t, h.clone, h.cachedPath(t, exampleRepo))

	// A cached clone whose remotes no longer point at the repository is replaced too.
	other := filepath.Join(h.root, "other-repo")
	makeRepoDir(t, other)
	h.git.remotes[other] = "remote.origin.url git@github.com:acme-org/other-repo.git\n"
	h.exec(t, "UPDATE repos SET local_path = ? WHERE name = ?", other, exampleRepo)

	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "~/projects/example-repo", res.ClonePath)
	assert.Equal(t, h.clone, h.cachedPath(t, exampleRepo))

	// A valid cached clone outside the clone root is used without a scan.
	elsewhere := filepath.Join(h.home, "elsewhere", exampleRepo)
	makeRepoDir(t, elsewhere)
	h.git.remotes[elsewhere] = "remote.origin.url https://github.com/acme-org/example-repo\n"
	h.exec(t, "UPDATE repos SET local_path = ? WHERE name = ?", elsewhere, exampleRepo)

	before := len(h.git.recorded())
	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "~/elsewhere/example-repo", res.ClonePath)
	assert.Len(t, h.git.recorded()[before:], 1, "only the cached clone's remotes are read")
}

func TestLaunchFromAForkFetchesTheItemsRemote(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.git.remotes[h.clone] = "remote.origin.url git@github.com:octo-user/example-repo.git\n" +
		"remote.upstream.url https://github.com/acme-org/example-repo.git\n"

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	assert.Equal(t, "pr-7", res.Worktree)
	require.Len(t, h.maker.ensures(), 1)
	assert.Equal(t, "upstream", h.maker.ensures()[0].Remote, "the pull request's head comes from the item's repository")
	assert.Contains(t, promptFor(t, res.ScriptPath), "This worktree is already at the pull request's head commit")
}

func TestLaunchSaysAWorktreeIsAtThePullRequestsHeadOnlyWhenItIs(t *testing.T) {
	t.Parallel()

	const (
		atHead   = "This worktree is already at the pull request's head commit"
		checkout = "First run `gh pr checkout 7 --repo acme-org/example-repo` inside this worktree"
		head     = "fc1c3975011e66491785ace891c893e4fc716587"
	)

	h := newLaunchHarness(t)

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	require.Equal(t, WorktreeCreate, res.WorktreeAction, "made now")
	assert.Contains(t, promptFor(t, res.ScriptPath), atHead, "a worktree made now starts at the head fetched for it")

	// Resumed, the worktree has whatever commit it was left at.
	h.maker.mu.Lock()
	h.maker.head = "0123456789abcdef0123456789abcdef01234567"
	h.maker.mu.Unlock()

	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	prompt := promptFor(t, res.ScriptPath)
	assert.NotContains(t, prompt, atHead, "a worktree left behind the pull request's head is not at it")
	assert.Contains(t, prompt, checkout)

	// One at the head the last refresh read is.
	h.exec(t, `INSERT INTO item_details (item_id, fetched_at, head_oid) VALUES (?, ?, ?)`, h.trustedPR, fixedNow(), head)
	h.maker.mu.Lock()
	h.maker.head = head
	h.maker.mu.Unlock()

	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Contains(t, promptFor(t, res.ScriptPath), atHead)
}

func TestLaunchFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(t *testing.T, h *launchHarness)
		item    func(h *launchHarness) int64
		wantErr error
		written bool
	}{
		{
			name:    "unknown item",
			item:    func(*launchHarness) int64 { return 9999 },
			wantErr: item.ErrNotFound,
		},
		{
			name:    "unreadable settings",
			mutate:  func(_ *testing.T, h *launchHarness) { h.cfg.Settings = failingSettings{} },
			wantErr: errFakeSettings,
		},
		{
			name:    "agent not installed",
			mutate:  func(_ *testing.T, h *launchHarness) { h.cfg.Finder = testutil.Finder{Err: errFakeNoClaude} },
			wantErr: errFakeNoClaude,
		},
		{
			name:    "no worktrees",
			mutate:  func(_ *testing.T, h *launchHarness) { h.cfg.Worktrees = nil },
			wantErr: ErrNoWorktrees,
		},
		{
			name:    "no terminal support on this OS",
			mutate:  func(_ *testing.T, h *launchHarness) { h.cfg.Registry = terminal.NewRegistry() },
			wantErr: terminal.ErrUnsupportedOS,
		},
		{
			name:    "terminal fails to open",
			mutate:  func(_ *testing.T, h *launchHarness) { h.iterm.err = errFakeOpen },
			wantErr: errFakeOpen,
			written: true,
		},
		{
			name: "invalid profile",
			mutate: func(t *testing.T, h *launchHarness) {
				t.Helper()
				h.exec(t, "UPDATE agent_profiles SET permission_mode = 'yolo' WHERE agent = 'claude' AND name = 'default'")
			},
			wantErr: ErrInvalidPermissionMode,
		},
		{
			name: "agent config unreadable",
			mutate: func(t *testing.T, h *launchHarness) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(h.home, ".claude.json"), []byte("[]"), 0o600))
			},
			wantErr: workspacetrust.ErrNotObject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			if tt.mutate != nil {
				tt.mutate(t, h)
			}

			id := h.trustedPR
			if tt.item != nil {
				id = tt.item(h)
			}

			_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: id})
			require.ErrorIs(t, err, tt.wantErr)

			if !tt.written {
				assert.Empty(t, h.launchFiles(t))
			}
		})
	}
}

func TestLaunchStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.exec(t, "UPDATE repos SET local_path = ? WHERE name = ?", h.clone, exampleRepo)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := h.service().Launch(ctx, LaunchRequest{ItemID: h.trustedPR})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, h.iterm.opened)
}

func TestNewServiceDefaultsTheClock(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.cfg.Clock = nil

	start := time.Now().Add(-time.Second)
	svc := h.service()

	assert.False(t, svc.clock.Now().Before(start))
}
