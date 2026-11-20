package describeit

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestTheAgentIsInheritedUntilSettingsChoosesOne(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.agents.profiles = []Profile{{Name: defaultProfile, Default: true}, {Name: otherProfile}}

	got, err := r.svc.Agent(t.Context())
	require.NoError(t, err)
	assert.Equal(t, AgentChoice{}, got, "each part inherits")

	stored, err := r.svc.SetAgent(t.Context(), AgentChoice{Profile: " " + otherProfile + " ", Model: "opus", Effort: "high"})
	require.NoError(t, err)
	assert.Equal(t, AgentChoice{Profile: otherProfile, Model: "opus", Effort: "high"}, stored, "as stored")

	got, err = r.svc.Agent(t.Context())
	require.NoError(t, err)
	assert.Equal(t, stored, got)

	info, err := os.Stat(r.path("agent.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	conv, err := r.svc.NewConversation(t.Context(), AgentChoice{})
	require.NoError(t, err)
	assert.Equal(t, stored, conv.Agent, "a new conversation starts with it")

	stored, err = r.svc.SetAgent(t.Context(), AgentChoice{})
	require.NoError(t, err)
	assert.Equal(t, AgentChoice{}, stored, "and inherits again once cleared")
}

func TestSetAgentRefusesWhatClaudeCodeWouldnt(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.agents.profiles = []Profile{{Name: defaultProfile, Default: true}}

	for name, tt := range map[string]struct {
		choice AgentChoice
		want   string
	}{
		"a model that reads as a flag": {AgentChoice{Model: "--dangerous"}, `"--dangerous" isn't a model name ` + agentWords + " takes."},
		"an effort there isn't":        {AgentChoice{Effort: "turbo"}, `"turbo" isn't an effort ` + agentWords + " takes: choose low, medium, high, xhigh, or max."},
		"a profile there isn't":        {AgentChoice{Profile: "home"}, agentWords + ` has no profile named "home".`},
	} {
		_, err := r.svc.SetAgent(t.Context(), tt.choice)
		require.Error(t, err, name)
		assert.Equal(t, tt.want, uierr.Message(err), name)

		_, err = r.svc.NewConversation(t.Context(), tt.choice)
		require.Error(t, err, "%s: a new conversation refuses it too", name)
	}

	r.agents.profErr = errFake

	_, err := r.svc.SetAgent(t.Context(), AgentChoice{Profile: defaultProfile})
	require.Error(t, err)
	assert.Equal(t, "Describe it couldn't read "+agentWords+"'s profiles.", uierr.Message(err))

	got, err := r.svc.Agent(t.Context())
	require.NoError(t, err)
	assert.Equal(t, AgentChoice{}, got, "nothing refused is stored")
}

func TestAnAgentFileThatCantBeReadInherits(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"not JSON":         "{",
		"another version":  `{"version":2,"model":"opus"}`,
		"a model refused":  `{"version":1,"model":"-x"}`,
		"a profile of two": `{"version":1,"profile":"a\nb"}`,
	} {
		r := newRig(t)
		require.NoError(t, os.WriteFile(r.path("agent.json"), []byte(content), 0o600))

		got, err := r.svc.Agent(t.Context())
		require.NoError(t, err, name)
		assert.Equal(t, AgentChoice{}, got, name)
	}
}

func TestStatusSaysWhetherATurnCanRunAndWithWhichAgent(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.agents.set(Start{Ready: true, Program: "/bin/true", Version: readyVersion(), Model: "sonnet", Effort: "medium"})

	st, err := r.svc.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Status{
		Ready: true, Version: readyVersion(), Profile: defaultProfile, Model: "sonnet", Effort: "medium",
	}, st, "inherit comes to the profile's own")

	r.agents.profiles = []Profile{{Name: defaultProfile, Default: true}, {Name: otherProfile}}
	_, err = r.svc.SetAgent(t.Context(), AgentChoice{Profile: otherProfile, Model: "opus"})
	require.NoError(t, err)

	st, err = r.svc.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, AgentChoice{Profile: otherProfile, Model: "opus"}, st.Agent)
	assert.Equal(t, otherProfile, st.Profile)
	assert.Equal(t, "opus", st.Model, "a choice is its own")
	assert.Equal(t, "medium", st.Effort)

	r.writeProposal(Outside, testNow, 0, pinProposal())

	st, err = r.svc.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, st.Pending)

	for name, tt := range map[string]struct {
		start        Start
		err          error
		problem, fix string
	}{
		"signed out": {
			start:   Start{Program: "/bin/true", Version: readyVersion(), Problem: agentWords + " is signed out.", Fix: "Sign in."},
			problem: agentWords + " is signed out.", fix: "Sign in.",
		},
		"not ready, saying nothing": {start: Start{}, problem: agentWords + " isn't ready for Describe it."},
		"too old": {
			start:   Start{Ready: true, Program: "/bin/true", Version: "2.1.0"},
			problem: agentWords + " 2.1.0 is older than Describe it needs (" + readyVersion() + ").",
			fix:     "Update " + agentWords + ", then try again.",
		},
		"unreadable": {err: errFake, problem: "Describe it couldn't find " + agentWords + "."},
	} {
		r.agents.set(tt.start)
		r.agents.err = tt.err

		st, err = r.svc.Status(t.Context())
		require.NoError(t, err, name)
		assert.False(t, st.Ready, name)
		assert.Equal(t, tt.problem, st.Problem, name)
		assert.Equal(t, tt.fix, st.Fix, name)
	}

	bare := New(Config{Dir: t.TempDir()})

	st, err = bare.Status(t.Context())
	require.NoError(t, err)
	assert.False(t, st.Ready)
	assert.Equal(t, "Describe it can't reach "+agentWords+" in this build.", st.Problem)
}
