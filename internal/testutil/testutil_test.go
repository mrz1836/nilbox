package testutil_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errScripted = errors.New("scripted failure")

func TestDBAtReopensTheSameFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")

	_, err := testutil.DBAt(t, path).ExecContext(t.Context(), `INSERT INTO accounts (login, created_at, updated_at)
VALUES ('octo-user', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`)
	require.NoError(t, err)

	var login string
	require.NoError(t, testutil.DBAt(t, path).QueryRowContext(t.Context(), "SELECT login FROM accounts").Scan(&login))
	assert.Equal(t, "octo-user", login)
}

// failureTB records a test failure instead of failing the test it wraps. FailNow ends
// the goroutine it runs on, as testing's does, so call the code under test on one of
// its own.
type failureTB struct {
	testing.TB

	mu     sync.Mutex
	failed bool
}

func (f *failureTB) Errorf(string, ...any) { f.fail() }

func (f *failureTB) FailNow() {
	f.fail()
	runtime.Goexit()
}

func (f *failureTB) Failed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.failed
}

func (f *failureTB) fail() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failed = true
}

// fails reports whether fn fails the test it is handed, run on a goroutine of its own.
func fails(t *testing.T, fn func(tb testing.TB)) bool {
	t.Helper()

	tb := &failureTB{TB: t}
	done := make(chan struct{})

	go func() {
		defer close(done)

		fn(tb)
	}()

	<-done

	return tb.Failed()
}

func TestDBAtFailsWhenTheDatabaseWasSetAside(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	testutil.WriteFile(t, path, "this is not a database file, just text long enough to have a header")

	assert.True(t, fails(t, func(tb testing.TB) { testutil.DBAt(tb, path) }),
		"a test never runs on a fresh database it did not ask for")

	aside, err := filepath.Glob(path + ".broken-*")
	require.NoError(t, err)
	assert.Len(t, aside, 1, "the database opened by setting the file aside, so the failure is the set-aside check")
}

//nolint:paralleltest // the test checks what its subtest left behind once that subtest ended
func TestDatabasesCloseWhenTheTestEnds(t *testing.T) {
	var db, at *sql.DB

	t.Run("open", func(t *testing.T) {
		db = testutil.DB(t)
		at = testutil.DBAt(t, filepath.Join(t.TempDir(), "app.db"))
	})

	require.Error(t, db.PingContext(t.Context()))
	require.Error(t, at.PingContext(t.Context()))
}

func TestClosedDBRefusesQueries(t *testing.T) {
	t.Parallel()

	_, err := testutil.ClosedDB(t).ExecContext(t.Context(), "SELECT 1")
	require.Error(t, err)
}

func TestExecRunsAStatement(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	testutil.Exec(t, db, `INSERT INTO prefs (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)`,
		"example.key", "kept")

	var value string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT value FROM prefs WHERE key = 'example.key'").Scan(&value))
	assert.Equal(t, "kept", value)
}

func TestExecFailsTheTestWhenTheStatementFails(t *testing.T) {
	t.Parallel()

	db := testutil.ClosedDB(t)

	assert.True(t, fails(t, func(tb testing.TB) { testutil.Exec(tb, db, "DELETE FROM prefs") }),
		"a statement that fails fails the test")
}

func TestRequireGitReturnsAnExecutable(t *testing.T) {
	t.Parallel()

	path := testutil.RequireGit(t)

	assert.True(t, filepath.IsAbs(path) || path == "git", path)
}

func TestGitEnvIsolatesGit(t *testing.T) {
	t.Setenv("GIT_DIR", "/nowhere/.git")
	t.Setenv("GIT_INDEX_FILE", "/nowhere/index")
	t.Setenv("HOME", "/nowhere")

	home := t.TempDir()
	env := testutil.GitEnv(home)

	values := map[string][]string{}

	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = append(values[key], value)
	}

	assert.Equal(t, []string{home}, values["HOME"])
	assert.Equal(t, []string{filepath.Join(home, ".config")}, values["XDG_CONFIG_HOME"])
	assert.Equal(t, []string{os.DevNull}, values["GIT_CONFIG_GLOBAL"])
	assert.Equal(t, []string{"1"}, values["GIT_CONFIG_NOSYSTEM"])
	assert.Equal(t, []string{testutil.GitName}, values["GIT_AUTHOR_NAME"])
	assert.Equal(t, []string{testutil.GitEmail}, values["GIT_COMMITTER_EMAIL"])
	assert.NotContains(t, values, "GIT_DIR", "an inherited GIT_ variable is dropped")
	assert.NotContains(t, values, "GIT_INDEX_FILE", "an inherited GIT_ variable is dropped")
	assert.Equal(t, []string{os.Getenv("PATH")}, values["PATH"], "everything else is inherited")
}

func TestRunGitCommitsAsTheTestIdentity(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "example-repo")
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, filepath.Join(dir, "README.md"), "example\n")
	testutil.RunGit(t, dir, "add", "README.md")
	testutil.RunGit(t, dir, "commit", "-q", "-m", "init")

	assert.Equal(t, "main\n", testutil.RunGit(t, dir, "branch", "--show-current"))
	assert.Equal(t, testutil.GitName+" <"+testutil.GitEmail+">\n",
		testutil.RunGit(t, dir, "log", "-1", "--format=%an <%ae>"))
	assert.Empty(t, testutil.RunGit(t, dir, "config", "--global", "--list"), "no global configuration is read")
}

func TestCommitCommitsTheFileUnderItsName(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "example-repo")
	testutil.InitRepo(t, dir)

	first := testutil.Commit(t, dir, "notes/a.txt", "a\n")
	assert.Equal(t, strings.TrimSpace(testutil.RunGit(t, dir, "rev-parse", "HEAD")), first, "the new commit")
	assert.Equal(t, "a\n", testutil.RunGit(t, dir, "show", "HEAD:notes/a.txt"))
	assert.Equal(t, "notes/a.txt\n", testutil.RunGit(t, dir, "log", "-1", "--format=%s"), "the file's name is the message")

	second := testutil.Commit(t, dir, "notes/a.txt", "b\n")
	assert.Equal(t, first+"\n", testutil.RunGit(t, dir, "rev-parse", second+"^"), "a later commit is on top")
	assert.Equal(t, "b\n", testutil.RunGit(t, dir, "show", "HEAD:notes/a.txt"), "the file is written again")
	assert.Empty(t, testutil.RunGit(t, dir, "status", "--porcelain"), "nothing is left uncommitted")
}

func TestGitRunsAnIsolatedCommand(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "git", "config", "--global", "--list")
	cmd.Env = testutil.Git(t)

	out, err := cmd.Output()
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestWriteFileCreatesItsDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "a", "b", "notes.txt")
	testutil.WriteFile(t, path, "hello")

	body, err := os.ReadFile(path) //nolint:gosec // a path the test itself created
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))

	info, err := os.Stat(path)
	require.NoError(t, err)

	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestWriteFileModeLeavesExactlyTheMode(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes")
	}

	path := filepath.Join(t.TempDir(), "a", "b", "notes.txt")
	testutil.WriteFileMode(t, path, "hello", 0o750)

	body, err := os.ReadFile(path) //nolint:gosec // a path the test itself created
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm())

	testutil.WriteFileMode(t, path, "again", 0o604)

	info, err = os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o604), info.Mode().Perm(), "the mode of a file already there is set too")
}

func TestWriteExecutableRuns(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the executable is a POSIX script")
	}

	path := filepath.Join(t.TempDir(), "bin", "example-tool")
	testutil.WriteExecutable(t, path, "#!/bin/sh\necho ran\n")

	out, err := exec.CommandContext(t.Context(), path).Output() //nolint:gosec // the test's own script
	require.NoError(t, err)
	assert.Equal(t, "ran\n", string(out))
}

func TestRealTempDirHasNoSymlinks(t *testing.T) {
	t.Parallel()

	dir := testutil.RealTempDir(t)

	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, resolved)
	assert.DirExists(t, dir)
}

//nolint:paralleltest // the test checks what its subtest did once that subtest ended
func TestSkipAsRoot(t *testing.T) {
	ran := false

	t.Run("skips only as root", func(t *testing.T) {
		testutil.SkipAsRoot(t)

		ran = true
	})

	assert.Equal(t, os.Geteuid() != 0, ran)
}

func TestClaudeHomeMakesProfiles(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude", ".claude_work", ".claude-alt", ".claude-squad/worktrees")

	assert.DirExists(t, filepath.Join(home, ".claude"))
	assert.NoFileExists(t, filepath.Join(home, ".claude", ".claude.json"), "the default config is not a named profile")
	assert.FileExists(t, filepath.Join(home, ".claude_work", ".claude.json"))
	assert.FileExists(t, filepath.Join(home, ".claude-alt", ".claude.json"))
	assert.DirExists(t, filepath.Join(home, ".claude-squad", "worktrees"))
	assert.NoFileExists(t, filepath.Join(home, ".claude-squad", ".claude.json"), "a nested entry makes no profile")
	assert.NoFileExists(t, filepath.Join(home, ".claude-squad", "worktrees", ".claude.json"))
}

func TestAgentHomeMakesEveryAgentsProfiles(t *testing.T) {
	t.Parallel()

	home := testutil.AgentHome(t, ".claude_work", ".codex", ".codex_work", ".codex-alt", ".codexbar")

	assert.FileExists(t, filepath.Join(home, ".claude_work", ".claude.json"))
	assert.NoFileExists(t, filepath.Join(home, ".codex", "auth.json"), "the default config is not a named profile")
	assert.FileExists(t, filepath.Join(home, ".codex_work", "auth.json"))
	assert.FileExists(t, filepath.Join(home, ".codex-alt", "auth.json"))
	assert.DirExists(t, filepath.Join(home, ".codexbar"))
	assert.NoFileExists(t, filepath.Join(home, ".codexbar", "auth.json"), "another tool's folder is no profile")
}

func TestRunnerAnswersAndRecords(t *testing.T) {
	t.Parallel()

	r := &testutil.Runner{Stdout: "out", Stderr: "err", Err: errScripted}

	stdout, stderr, err := r.Run(t.Context(), "git", "-C", "/srv/example-repo", "status")
	require.ErrorIs(t, err, errScripted)
	assert.Equal(t, "out", string(stdout))
	assert.Equal(t, "err", string(stderr))

	calls := r.Calls()
	assert.Equal(t, [][]string{{"git", "-C", "/srv/example-repo", "status"}}, calls)

	calls[0][0] = "changed"

	assert.Equal(t, "git", r.Calls()[0][0], "Calls returns a copy")
}

func TestRunnerScriptAnswers(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	var seen []string

	r := &testutil.Runner{Stdout: "unused", Script: func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		_, ok := ctx.Deadline()
		assert.True(t, ok, "the script gets the call's context")

		seen = argv

		return []byte(strings.Join(argv, " ")), nil, nil
	}}

	stdout, _, err := r.Run(ctx, "gh", "auth", "token")
	require.NoError(t, err)
	assert.Equal(t, "gh auth token", string(stdout))
	assert.Equal(t, []string{"gh", "auth", "token"}, seen)
}

func TestRunnerIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	r := &testutil.Runner{}

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() { _, _, _ = r.Run(t.Context(), "true") })
	}

	wg.Wait()
	assert.Len(t, r.Calls(), 8)
}

func TestFinderResolves(t *testing.T) {
	t.Parallel()

	path, err := testutil.Finder{Path: "/opt/fake/bin/tool"}.Find("anything")
	require.NoError(t, err)
	assert.Equal(t, "/opt/fake/bin/tool", path)

	only := testutil.Finder{Paths: map[string]string{"claude": "/opt/agent/claude"}}

	path, err = only.Find("claude")
	require.NoError(t, err)
	assert.Equal(t, "/opt/agent/claude", path)

	_, err = only.Find("git")
	require.ErrorIs(t, err, testutil.ErrNotFound)

	_, err = testutil.Finder{Path: "/opt/fake/bin/tool", Err: errScripted}.Find("tool")
	require.ErrorIs(t, err, errScripted)
}

func TestClockStandsStillUntilMoved(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := testutil.NewClock(start)

	assert.Equal(t, start, clock.Now())
	assert.Equal(t, start, clock.Now())
	assert.Equal(t, start.Add(time.Hour), clock.Advance(time.Hour))
	assert.Equal(t, start.Add(time.Hour), clock.Now())

	clock.Set(start)
	assert.Equal(t, start, clock.Now())
}

func TestSteppingClockMovesOnEveryRead(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := testutil.NewSteppingClock(start, time.Second)

	assert.Equal(t, start, clock.Now())
	assert.Equal(t, start.Add(time.Second), clock.Now())
	assert.Equal(t, start.Add(2*time.Second), clock.Now())
}

func TestEventsRecordsEveryEventInOrder(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	assert.Zero(t, events.Count())
	assert.Empty(t, events.All())

	events.Emit(event.InboxUpdated{})
	events.Emit(event.NotificationsUpdated{})
	events.Emit(event.WorktreesUpdated{})

	got := events.All()
	assert.Equal(t, []event.Event{event.InboxUpdated{}, event.NotificationsUpdated{}, event.WorktreesUpdated{}}, got)
	assert.Equal(t, 3, events.Count())

	got[0] = event.MenuBarChanged{}

	assert.Equal(t, event.InboxUpdated{}, events.All()[0], "All returns a copy")
}

func TestLogsCollectsRecords(t *testing.T) {
	t.Parallel()

	var logs testutil.Logs

	logger := logs.Logger()
	logger.Debug("debug kept", "n", 1)
	logger.Warn("warning kept")

	assert.Contains(t, logs.String(), `level=DEBUG msg="debug kept" n=1`)
	assert.Contains(t, logs.String(), `level=WARN msg="warning kept"`)
}
