package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// wtDir is the worktree the processes work in. claudeCmd is the program name Claude Code's
// process has, its version, and codexCmd Codex's.
const (
	wtDir     = "/srv/wt/pr-12"
	claudeCmd = "2.1.319"
	codexCmd  = "codex"
)

func TestSessionsFoldHelpersAndNameFollowedAgents(t *testing.T) {
	t.Parallel()

	list := []procs.Process{
		{PID: 63296, PPID: 600, Command: claudeCmd, Dir: wtDir},
		{PID: 74056, PPID: 63296, Command: "caffeinate", Dir: wtDir},
		{PID: 700, PPID: 600, Command: "zsh", Dir: wtDir},
		{PID: 701, PPID: 701, Command: "odd", Dir: wtDir},
	}

	got := sessionsOf(list, map[int]agent.ID{63296: agent.Claude})

	assert.Equal(t, []Session{
		{PID: 63296, Command: claudeCmd, Agent: agent.Claude},
		{PID: 700, Command: "zsh"},
		{PID: 701, Command: "odd"},
	}, got, "the helper the session started is folded into it; a shell of the user's own stays")

	err := &InUseError{Sessions: got}
	assert.Equal(t, "a session is running in the worktree: Claude (pid 63296), zsh (pid 700), odd (pid 701)", err.Error())

	assert.NotNil(t, sessionsOf(nil, nil), "never nil, so the window gets []")
}

// namedAgents is an AgentProcesses that answers from a map, or fails with err.
type namedAgents struct {
	pids map[int]agent.ID
	err  error
}

func (n namedAgents) AgentProcesses(context.Context) (map[int]agent.ID, error) {
	return n.pids, n.err
}

func TestAgentProcessesAreOptional(t *testing.T) {
	t.Parallel()

	assert.Nil(t, NewService(Config{}).agentProcesses(t.Context()))

	named := NewService(Config{Agents: namedAgents{pids: map[int]agent.ID{7: agent.Codex}}})
	require.Equal(t, map[int]agent.ID{7: agent.Codex}, named.agentProcesses(t.Context()))

	failing := NewService(Config{Agents: namedAgents{err: assert.AnError}})
	assert.Nil(t, failing.agentProcesses(t.Context()), "failing to name them only costs the names")
}

func TestAFoldedAgentStandsForTheProcessItIsFoldedInto(t *testing.T) {
	t.Parallel()

	list := []procs.Process{
		// The user resumed a Claude session from a shell of their own in the worktree.
		{PID: 4100, PPID: 4000, Command: "zsh", Dir: wtDir},
		{PID: 4242, PPID: 4100, Command: claudeCmd, Dir: wtDir},
		{PID: 4343, PPID: 4242, Command: "caffeinate", Dir: wtDir},
		// A shell that runs a script that runs Codex: found two levels down.
		{PID: 5100, PPID: 5000, Command: "zsh", Dir: wtDir},
		{PID: 5200, PPID: 5100, Command: "bash", Dir: wtDir},
		{PID: 5300, PPID: 5200, Command: codexCmd, Dir: wtDir},
		// A followed agent that started another stands for itself.
		{PID: 6100, PPID: 6000, Command: codexCmd, Dir: wtDir},
		{PID: 6200, PPID: 6100, Command: claudeCmd, Dir: wtDir},
		// A shell of the user's own with no followed agent in it stays as it is.
		{PID: 7100, PPID: 7000, Command: "zsh", Dir: wtDir},
		{PID: 7200, PPID: 7100, Command: "vim", Dir: wtDir},
	}
	agents := map[int]agent.ID{4242: agent.Claude, 5300: agent.Codex, 6100: agent.Codex, 6200: agent.Claude}

	got := sessionsOf(list, agents)

	assert.Equal(t, []Session{
		{PID: 4242, Command: claudeCmd, Agent: agent.Claude},
		{PID: 5300, Command: codexCmd, Agent: agent.Codex},
		{PID: 6100, Command: codexCmd, Agent: agent.Codex},
		{PID: 7100, Command: "zsh"},
	}, got, "the agent's own PID, the process the user would look up or stop")

	err := &InUseError{Sessions: got}
	assert.Equal(t, "a session is running in the worktree: Claude (pid 4242), Codex (pid 5300), Codex (pid 6100), zsh (pid 7100)",
		err.Error())
}

func TestAParentLoopIsListedOnce(t *testing.T) {
	t.Parallel()

	// A reused PID can make a process its own grandparent.
	list := []procs.Process{
		{PID: 11, PPID: 10, Command: "zsh", Dir: wtDir},
		{PID: 10, PPID: 11, Command: claudeCmd, Dir: wtDir},
		{PID: 12, PPID: 11, Command: "caffeinate", Dir: wtDir},
	}

	assert.Equal(t, []Session{{PID: 10, Command: claudeCmd, Agent: agent.Claude}},
		sessionsOf(list, map[int]agent.ID{10: agent.Claude}), "still in use, under the loop's lowest PID")
}

// countedAgents is an AgentProcesses that answers from a map and counts the calls.
type countedAgents struct {
	pids  map[int]agent.ID
	calls atomic.Int32
}

func (c *countedAgents) AgentProcesses(context.Context) (map[int]agent.ID, error) {
	c.calls.Add(1)

	return c.pids, nil
}

func TestARefusalNamesTheFollowedAgent(t *testing.T) {
	t.Parallel()

	wt := t.TempDir()
	running := &testutil.Lister[procs.Process]{List: []procs.Process{
		{PID: 4000, PPID: 1, Command: "iTerm2", Dir: "/"},
		{PID: 4100, PPID: 4000, Command: "zsh", Dir: wt},
		{PID: 4242, PPID: 4100, Command: claudeCmd, Dir: wt},
	}}
	agents := &countedAgents{pids: map[int]agent.ID{4242: agent.Claude}}
	svc := NewService(Config{Procs: running, Agents: agents})

	err := svc.checkSessions(t.Context(), wt, true)
	require.ErrorIs(t, err, ErrInUse)
	require.EqualError(t, err, "a session is running in the worktree: Claude (pid 4242)")
	assert.Equal(t, int32(1), agents.calls.Load())

	require.NoError(t, svc.checkSessions(t.Context(), t.TempDir(), true))
	assert.Equal(t, int32(1), agents.calls.Load(), "with no process in the folder, the followed sessions are not read")
}

func TestListMatchesEachWorktreeToTheSessionsInIt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newRepo(t, "example-repo")
	worktrees := filepath.Join(repo, ".claude", "worktrees")
	first := addWorktree(t, repo, filepath.Join(worktrees, "wt-1"), ownBranchPrefix+"wt-1")
	second := addWorktree(t, repo, filepath.Join(worktrees, "wt-2"), ownBranchPrefix+"wt-2")
	idle := addWorktree(t, repo, filepath.Join(worktrees, "wt-3"), ownBranchPrefix+"wt-3")
	require.NoError(t, os.MkdirAll(filepath.Join(second, "src"), 0o700))

	link := filepath.Join(e.home, "second")
	require.NoError(t, os.Symlink(second, link))

	running := &testutil.Lister[procs.Process]{List: []procs.Process{
		{PID: 4000, PPID: 1, Command: "iTerm2", Dir: "/"},
		{PID: 4100, PPID: 4000, Command: "fish", Dir: first},
		{PID: 4200, PPID: 4000, Command: "vim", Dir: filepath.Join(link, "src")},
		{PID: 4242, PPID: 4100, Command: claudeCmd, Dir: filepath.Join(first, "missing")},
		{PID: 4300, PPID: 4000, Command: "node", Dir: first + "-other"},
		{PID: 4400, PPID: 4000, Command: "make", Dir: repo},
		{PID: 4500, PPID: 4000, Command: "none", Dir: ""},
		{PID: 4600, PPID: 4000, Command: "go", Dir: second},
	}}
	svc := e.linked(linkDeps{running: running})
	svc.agents = namedAgents{pids: map[int]agent.ID{4242: agent.Claude}}

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.False(t, snap.SessionsUnknown)

	assert.Equal(t, []Session{{PID: 4242, Command: claudeCmd, Agent: agent.Claude}}, mustRow(t, snap, first).InUse,
		"the followed agent stands for the shell it runs under, though its folder is gone")
	assert.Equal(t, []Session{{PID: 4200, Command: "vim"}, {PID: 4600, Command: "go"}}, mustRow(t, snap, second).InUse,
		"by PID, one of them through a link")
	assert.Empty(t, mustRow(t, snap, idle).InUse, "a sibling's folder, the clone's, or none is no session here")
	assert.NotNil(t, mustRow(t, snap, idle).InUse)
	assert.Empty(t, mustRow(t, snap, repo).InUse, "the clone's own checkout is never marked")
}

// describer is a Config.Describe that answers from a map, counting the reads.
type describer struct {
	details map[int]procs.Detail
	calls   atomic.Int32
}

func (d *describer) describe(pid int) (procs.Detail, bool) {
	d.calls.Add(1)
	got, ok := d.details[pid]

	return got, ok
}

func TestListDescribesWhatWorksInAWorktree(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newRepo(t, "example-repo")
	worktrees := filepath.Join(repo, ".claude", "worktrees")
	left := addWorktree(t, repo, filepath.Join(worktrees, "wt-1"), ownBranchPrefix+"wt-1")
	shell := addWorktree(t, repo, filepath.Join(worktrees, "wt-2"), ownBranchPrefix+"wt-2")
	service := addWorktree(t, repo, filepath.Join(worktrees, "wt-3"), ownBranchPrefix+"wt-3")
	unread := addWorktree(t, repo, filepath.Join(worktrees, "wt-4"), ownBranchPrefix+"wt-4")

	loop := "/bin/sh -c while true; do sleep 0.05; done"
	running := &testutil.Lister[procs.Process]{List: []procs.Process{
		// A test's child its test binary left behind: launchd adopted it, and its sleeps.
		{PID: 75083, PPID: 1, Command: "bash", Dir: left},
		{PID: 75090, PPID: 75083, Command: "sleep", Dir: left},
		// A shell in a terminal.
		{PID: 4000, PPID: 1, Command: "iTerm2", Dir: "/"},
		{PID: 4100, PPID: 4000, Command: "fish", Dir: shell},
		// A daemon launchd runs with a terminal of its own, which isn't detached.
		{PID: 950, PPID: 1, Command: "gradle", Dir: service},
		// One whose detail can't be read.
		{PID: 960, PPID: 1, Command: "node", Dir: unread},
	}}
	described := &describer{details: map[int]procs.Detail{
		75083: {Args: loop},
		4100:  {Args: "-fish", Terminal: true},
		950:   {Args: "java -jar gradle-daemon.jar", Terminal: true},
	}}

	svc := e.linked(linkDeps{running: running})
	svc.describe = described.describe

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	assert.Equal(t, []Session{{PID: 75083, Command: "bash", Args: loop, Detached: true}}, mustRow(t, snap, left).InUse,
		"no terminal, and what started it has exited")
	assert.Equal(t, []Session{{PID: 4100, Command: "fish", Args: "-fish"}}, mustRow(t, snap, shell).InUse)
	assert.Equal(t, []Session{{PID: 950, Command: "gradle", Args: "java -jar gradle-daemon.jar"}},
		mustRow(t, snap, service).InUse, "adopted, but a terminal holds it")
	assert.Equal(t, []Session{{PID: 960, Command: "node"}}, mustRow(t, snap, unread).InUse,
		"by its program alone, and never called detached, when it can't be read")
	assert.Equal(t, int32(4), described.calls.Load(), "each session's process once, and none of the folded helpers")
}

func TestDescribeSessionsReadsEachProcessOnceAndNoRun(t *testing.T) {
	t.Parallel()

	nested := Worktree{InUse: []Session{{PID: 7, Command: "node"}, {PID: 9, By: "⚡ Fix checks"}}}
	outer := Worktree{InUse: []Session{{PID: 7, Command: "node"}, {PID: 8, Command: "gone"}}}
	repos := []Repo{{Worktrees: []Worktree{nested, outer}}}
	running := []procs.Process{{PID: 7, PPID: 1, Command: "node", Dir: "/srv"}, {PID: 9, PPID: 1, Command: "claude", Dir: "/srv"}}
	described := &describer{details: map[int]procs.Detail{7: {Args: "node server.js"}, 9: {Args: "claude -p"}}}

	describeSessions(repos, running, described.describe)

	assert.Equal(t, []Session{{PID: 7, Command: "node", Args: "node server.js", Detached: true}, {PID: 9, By: "⚡ Fix checks"}},
		repos[0].Worktrees[0].InUse, "the app's own run is never read")
	assert.Equal(t, []Session{{PID: 7, Command: "node", Args: "node server.js", Detached: true}, {PID: 8, Command: "gone"}},
		repos[0].Worktrees[1].InUse, "a process the listing doesn't hold is left as it is")
	assert.Equal(t, int32(1), described.calls.Load(), "a process in two worktrees is read once")

	describeSessions(repos, running, nil) // without Describe, nothing is read, and nothing fails
}
