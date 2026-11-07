package launcher

import (
	"context"
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
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchlog"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
)

// remoteLookupPattern is the only git config pattern the launcher may pass.
const remoteLookupPattern = `^remote\..*\.url$`

var (
	errGitNotAllowed = errors.New("git command not allowed in a launch")
	errFakeNoClaude  = errors.New("claude is not installed")
	errFakeSettings  = errors.New("settings unreadable")
)

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

// failingSettings cannot read the settings.
type failingSettings struct{}

func (failingSettings) Get(context.Context) (settings.Settings, error) {
	return settings.Settings{}, errFakeSettings
}

// launchHarness is a launch Service over a temp database, a temp home with one clone,
// and fakes for git, the agent lookup, and the terminals.
type launchHarness struct {
	db       *gorm.DB
	home     string
	root     string
	clone    string
	paths    appdata.Paths
	git      *fakeGit
	iterm    *fakeDriver
	terminal *fakeDriver
	settings *settings.Service
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
		iterm:    &fakeDriver{id: settings.TerminalITerm2},
		terminal: &fakeDriver{id: settings.TerminalApp},
	}
	h.root = filepath.Join(h.home, "projects")
	h.clone = filepath.Join(h.root, "example-repo")
	h.git = &fakeGit{remotes: map[string]string{
		h.clone: "remote.origin.url https://github.com/acme-org/example-repo.git\n",
	}}

	makeRepoDir(t, h.clone)

	pool, db := testutil.BridgedDBAt(t, h.paths.DB)
	h.db = db
	h.seed(t)

	h.settings = settings.NewService(pool, h.db, h.home)
	h.cfg = Config{
		DB:       h.db,
		Settings: h.settings,
		Git:      h.git,
		Finder:   testutil.Finder{Paths: map[string]string{claudeBin: filepath.Join(tmp, "bin", "claude")}},
		Registry: NewRegistry(h.iterm, h.terminal),
		Paths:    h.paths,
		Home:     h.home,
		Clock:    testutil.NewClock(fixedNow()),
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

	now := fixedNow()
	account := model.Account{Login: "octo-user", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, h.db.Create(&account).Error)

	repo := model.Repo{Owner: "acme-org", Name: "example-repo", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, h.db.Create(&repo).Error)

	orphan := model.Repo{Owner: "acme-org", Name: "missing-repo", CreatedAt: now, UpdatedAt: now}
	require.NoError(t, h.db.Create(&orphan).Error)

	addItem := func(repoID int64, kind item.Kind, number int, assoc string, cross bool) int64 {
		row := model.Item{
			AccountID: account.ID, RepoID: repoID, Kind: kind, Number: number,
			Title: fmt.Sprintf("Change %d", number), URL: fmt.Sprintf("https://github.com/acme-org/x/%d", number),
			AuthorLogin: "octo-user", AuthorAssociation: assoc, IsCrossRepository: cross, HeadRef: "feature-branch",
			ItemCreatedAt: now, ItemUpdatedAt: now, FetchedAt: now,
		}
		require.NoError(t, h.db.Create(&row).Error)

		return row.ID
	}

	h.trustedPR = addItem(repo.ID, item.KindPR, 7, "OWNER", false)
	h.outsidePR = addItem(repo.ID, item.KindPR, 8, "CONTRIBUTOR", true)
	h.issue = addItem(repo.ID, item.KindIssue, 3, "MEMBER", false)
	h.orphanPR = addItem(orphan.ID, item.KindPR, 9, "OWNER", false)

	work := profiles.Profile{
		Name: "work", ConfigDir: filepath.Join(h.home, ".claude_work"), PermissionMode: "acceptEdits",
		AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{},
	}
	full := profiles.Profile{
		Name: profiles.DefaultName, PermissionMode: profiles.PermissionFull,
		AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{},
	}
	require.NoError(t, profiles.Sync(t.Context(), h.db, []profiles.Profile{full, work}))
	require.NoError(t, prompts.Seed(t.Context(), h.db))
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

	var repo model.Repo
	require.NoError(t, h.db.Where("name = ?", name).First(&repo).Error)

	return repo.LocalPath
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

	prompt, err := os.ReadFile(strings.TrimSuffix(scriptPath, ".command") + promptFileSuffix)
	require.NoError(t, err)

	return string(prompt)
}

func boolPtr(b bool) *bool { return &b }

func TestLaunchWritesFilesAndOpensDriver(t *testing.T) {
	h := newLaunchHarness(t)

	res, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	require.Equal(t, []string{res.ScriptPath}, h.iterm.opened, "iTerm2 is the default terminal")
	assert.Empty(t, h.terminal.opened)

	assert.Equal(t, h.paths.Launch, filepath.Dir(res.ScriptPath))
	assert.True(t, strings.HasSuffix(res.ScriptPath, ".command"))
	assert.Equal(t, LaunchResult{
		ScriptPath: res.ScriptPath,
		ClonePath:  "~/projects/example-repo",
		Worktree:   "pr-7",
		Prompt:     "Review",
		Profile:    profiles.DefaultName,
		Permission: "full access",
		Terminal:   settings.TerminalITerm2,
	}, res)

	assert.Contains(t, script, "cd -- "+shQuote(h.clone)+" || exit 1\n")
	assert.Contains(t, script, "unset CLAUDE_CONFIG_DIR\n")
	assert.Contains(t, script, "'--dangerously-skip-permissions' '--name=Review · example-repo#7' '--worktree' '#7'\n")
	assert.Contains(t, promptFor(t, res.ScriptPath), "Review pull request #7 in acme-org/example-repo: Change 7")
	assert.Len(t, h.launchFiles(t), 2, "one script and one prompt file")

	assert.Equal(t, h.clone, h.cachedPath(t, "example-repo"), "the resolved clone is cached")
}

func TestWorktreePrefDefaultAndUntick(t *testing.T) {
	tests := []struct {
		name string
		pref bool
		req  *bool
		want bool
	}{
		{name: "pref on, request unset", pref: true, req: nil, want: true},
		{name: "pref on, request on", pref: true, req: boolPtr(true), want: true},
		{name: "pref on, request unticked", pref: true, req: boolPtr(false), want: false},
		{name: "pref off, request unset", pref: false, req: nil, want: false},
		{name: "pref off, request on", pref: false, req: boolPtr(true), want: true},
		{name: "pref off, request off", pref: false, req: boolPtr(false), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newLaunchHarness(t)
			require.NoError(t, h.settings.SetWorktreeDefault(t.Context(), tt.pref))

			res, script := h.launch(t, LaunchRequest{ItemID: h.issue, Worktree: tt.req})
			prompt := promptFor(t, res.ScriptPath)

			if !tt.want {
				assert.Empty(t, res.Worktree)
				assert.NotContains(t, script, "--worktree")
				assert.NotContains(t, prompt, "fresh worktree")

				return
			}

			assert.Equal(t, "issue-3", res.Worktree)
			assert.Contains(t, script, "'--worktree' 'issue-3'")
			assert.Contains(t, prompt, "fresh worktree")
		})
	}

	// Unticking applies to one launch only: the next launch follows the pref again.
	h := newLaunchHarness(t)

	res, _ := h.launch(t, LaunchRequest{ItemID: h.issue, Worktree: boolPtr(false)})
	assert.Empty(t, res.Worktree)

	res, _ = h.launch(t, LaunchRequest{ItemID: h.issue})
	assert.Equal(t, "issue-3", res.Worktree)
}

func TestLauncherRunsNoGitCommands(t *testing.T) {
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

func TestLaunchTrustsTheCloneForTheAgent(t *testing.T) {
	h := newLaunchHarness(t)
	defaultConfig := filepath.Join(h.home, ".claude.json")
	workDir := filepath.Join(h.home, ".claude_work")

	key, err := filepath.EvalSymlinks(h.clone)
	require.NoError(t, err)

	_, err = h.service().Preview(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	assert.NoFileExists(t, defaultConfig, "a preview trusts nothing")

	h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.True(t, trustedIn(t, defaultConfig, key), "the default profile's config, in home, trusts the clone")

	// A profile the agent has never run with is left for the agent to set up.
	h.launch(t, LaunchRequest{ItemID: h.trustedPR, Profile: profileWork})
	assert.NoDirExists(t, workDir)

	require.NoError(t, os.Mkdir(workDir, 0o700))
	h.launch(t, LaunchRequest{ItemID: h.outsidePR, Profile: profileWork, Worktree: boolPtr(false)})
	assert.True(t, trustedIn(t, filepath.Join(workDir, ".claude.json"), key),
		"the profile's own config trusts the clone, for every item and with or without a worktree")
}

// recordingLaunches keeps the launches the launcher records, or fails.
type recordingLaunches struct {
	launches []launchlog.Launch
	err      error
}

func (r *recordingLaunches) Record(_ context.Context, l launchlog.Launch) error {
	if r.err != nil {
		return r.err
	}

	r.launches = append(r.launches, l)

	return nil
}

func TestLaunchRecordsTheWorktree(t *testing.T) {
	h := newLaunchHarness(t)
	rec := &recordingLaunches{}
	h.cfg.Launches = rec

	h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	h.launch(t, LaunchRequest{ItemID: h.issue})
	h.launch(t, LaunchRequest{ItemID: h.trustedPR, Worktree: boolPtr(false)})

	assert.Equal(t, []launchlog.Launch{
		{
			ClonePath: h.clone, Worktree: "pr-7", Owner: "acme-org", Name: "example-repo", Kind: "pr", Number: 7,
			LaunchedAt: fixedNow(),
		},
		{
			ClonePath: h.clone, Worktree: "issue-3", Owner: "acme-org", Name: "example-repo", Kind: "issue", Number: 3,
			LaunchedAt: fixedNow(),
		},
	}, rec.launches, "the worktree's directory name, for launches in a worktree only")

	h.cfg.Launches = &recordingLaunches{err: errFakeSettings}
	h.launch(t, LaunchRequest{ItemID: h.trustedPR})
}

func TestLaunchUsesTerminalPref(t *testing.T) {
	h := newLaunchHarness(t)
	require.NoError(t, h.settings.SetTerminal(t.Context(), settings.TerminalApp))

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	assert.Equal(t, []string{res.ScriptPath}, h.terminal.opened)
	assert.Empty(t, h.iterm.opened)
	assert.Equal(t, settings.TerminalApp, res.Terminal)
}

func TestLaunchOutsideContributorIsPlanMode(t *testing.T) {
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
	h := newLaunchHarness(t)
	workDir := filepath.Join(h.home, ".claude_work")

	res, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR, Profile: "work"})
	assert.Equal(t, "work", res.Profile)
	assert.Contains(t, script, "export CLAUDE_CONFIG_DIR="+shQuote(workDir)+"\n")

	require.NoError(t, h.settings.SetDefaultProfile(t.Context(), "work"))

	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "work", res.Profile, "an empty profile uses the default profile setting")

	res, script = h.launch(t, LaunchRequest{ItemID: h.trustedPR, Profile: profiles.DefaultName})
	assert.Equal(t, profiles.DefaultName, res.Profile, "a picked profile applies to this launch")
	assert.Contains(t, script, "unset CLAUDE_CONFIG_DIR\n")

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Profile: "nope"})
	require.ErrorIs(t, err, profiles.ErrProfileNotFound)
}

func TestLaunchRevalidatesTheCachedClone(t *testing.T) {
	h := newLaunchHarness(t)

	stale := filepath.Join(h.root, "gone")
	require.NoError(t, h.db.Exec("UPDATE repos SET local_path = ? WHERE name = ?", stale, "example-repo").Error)

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "~/projects/example-repo", res.ClonePath, "a vanished cached clone is found again")
	assert.Equal(t, h.clone, h.cachedPath(t, "example-repo"))

	// A cached clone whose remotes no longer point at the repository is replaced too.
	other := filepath.Join(h.root, "other-repo")
	makeRepoDir(t, other)
	h.git.remotes[other] = "remote.origin.url git@github.com:acme-org/other-repo.git\n"
	require.NoError(t, h.db.Exec("UPDATE repos SET local_path = ? WHERE name = ?", other, "example-repo").Error)

	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "~/projects/example-repo", res.ClonePath)
	assert.Equal(t, h.clone, h.cachedPath(t, "example-repo"))

	// A valid cached clone outside the clone root is used without a scan.
	elsewhere := filepath.Join(h.home, "elsewhere", "example-repo")
	makeRepoDir(t, elsewhere)
	h.git.remotes[elsewhere] = "remote.origin.url https://github.com/acme-org/example-repo\n"
	require.NoError(t, h.db.Exec("UPDATE repos SET local_path = ? WHERE name = ?", elsewhere, "example-repo").Error)

	before := len(h.git.recorded())
	res, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Equal(t, "~/elsewhere/example-repo", res.ClonePath)
	assert.Len(t, h.git.recorded()[before:], 1, "only the cached clone's remotes are read")
}

func TestLaunchForkOriginUsesNamedPRWorktree(t *testing.T) {
	h := newLaunchHarness(t)
	h.git.remotes[h.clone] = "remote.origin.url git@github.com:octo-user/example-repo.git\n" +
		"remote.upstream.url https://github.com/acme-org/example-repo.git\n"

	res, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	assert.Equal(t, "pr-7", res.Worktree)
	assert.Contains(t, script, "'--worktree' 'pr-7'")
	assert.Contains(t, promptFor(t, res.ScriptPath), "First run `gh pr checkout 7 --repo acme-org/example-repo` inside this worktree")
}

func TestLaunchFailures(t *testing.T) {
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
			name:    "no terminal support on this OS",
			mutate:  func(_ *testing.T, h *launchHarness) { h.cfg.Registry = NewRegistry() },
			wantErr: ErrUnsupportedOS,
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
				require.NoError(t, h.db.Exec("UPDATE claude_profiles SET effort = 'turbo' WHERE name = 'default'").Error)
			},
			wantErr: agent.ErrInvalidEffort,
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
	h := newLaunchHarness(t)
	require.NoError(t, h.db.Exec("UPDATE repos SET local_path = ? WHERE name = ?", h.clone, "example-repo").Error)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := h.service().Launch(ctx, LaunchRequest{ItemID: h.trustedPR})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, h.iterm.opened)
}

func TestPlanWorktreeDir(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "pr-7", Plan{Kind: item.KindPR, Number: 7, Worktree: "#7"}.worktreeDir())
	assert.Equal(t, "pr-7", Plan{Kind: item.KindPR, Number: 7, Worktree: "pr-7"}.worktreeDir())
	assert.Equal(t, "issue-3", Plan{Kind: item.KindIssue, Number: 3, Worktree: "issue-3"}.worktreeDir())
	assert.Empty(t, Plan{Kind: item.KindPR, Number: 7}.worktreeDir())
}

func TestNewServiceDefaultsTheClock(t *testing.T) {
	h := newLaunchHarness(t)
	h.cfg.Clock = nil

	start := time.Now().Add(-time.Second)
	svc := h.service()

	assert.False(t, svc.clock.Now().Before(start))
}
