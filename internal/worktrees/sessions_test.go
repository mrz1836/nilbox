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
	running := fakeRunning{list: []procs.Process{
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

	running := fakeRunning{list: []procs.Process{
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
