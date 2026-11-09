package worktrees

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/procs"
)

func TestSessionsFoldHelpersAndNameFollowedAgents(t *testing.T) {
	t.Parallel()

	list := []procs.Process{
		{PID: 63296, PPID: 600, Command: "2.1.319", Dir: "/srv/wt/pr-12"},
		{PID: 74056, PPID: 63296, Command: "caffeinate", Dir: "/srv/wt/pr-12"},
		{PID: 700, PPID: 600, Command: "zsh", Dir: "/srv/wt/pr-12"},
		{PID: 701, PPID: 701, Command: "odd", Dir: "/srv/wt/pr-12"},
	}

	got := sessionsOf(list, map[int]agent.ID{63296: agent.Claude})

	assert.Equal(t, []Session{
		{PID: 63296, Command: "2.1.319", Agent: agent.Claude},
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
