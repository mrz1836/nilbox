package core

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// A background start works through its step's chain: its own profile as a click resolves
// it, then each fallback in order, passing over a profile resting out of usage and a
// fallback that can't run in the background here.

// The chain's agent and profiles.
const (
	claudeID       = "claude"
	defaultProfile = "default"
	workProfile    = "work"
)

// chainRig is the background agents with a step on Opus whose profile Inherit resolves to
// Claude Code's default, falling back to its work profile, then to Codex, then to its
// spare profile.
func chainRig(t *testing.T) (*agentRig, autopilot.Job) {
	t.Helper()

	r := newAgentRig(t)
	r.planner.set(r.planner.plan(t, testutil.FakeRun{Stdout: agentFixture(t, "success-structured.jsonl")}))
	r.planner.profiles = map[string]plannedProfile{
		"/":                             {agent: agent.Claude, profile: defaultProfile},
		claudeID + "/" + workProfile:    {agent: agent.Claude, profile: workProfile},
		"codex/" + defaultProfile:       {err: fmt.Errorf("%w: Codex isn't installed.", execpath.ErrNotFound)},
		claudeID + "/spare":             {agent: agent.Claude, profile: "spare"},
		claudeID + "/" + defaultProfile: {agent: agent.Claude, profile: defaultProfile},
	}

	job := agentJob(21)
	job.Step.Agent.Model = "opus"
	job.Step.Agent.Fallbacks = []spec.ProfileRef{
		{Agent: claudeID, Profile: workProfile}, {Agent: "codex", Profile: defaultProfile}, {Agent: claudeID, Profile: "spare"},
	}

	return r, job
}

// resting is the agent's profile resting until until.
func resting(agentID, profile string, until time.Time) autopilot.Resting {
	return autopilot.Resting{Agent: agentID, Profile: profile, Until: until}
}

func TestAStartRunsOnItsOwnProfileWhileNoneRests(t *testing.T) {
	t.Parallel()

	r, job := chainRig(t)

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	require.True(t, res.Running)
	assert.Equal(t, defaultProfile, res.Launched.Profile)
	assert.Len(t, r.planner.requests(), 1, "no fallback is planned")

	assert.Equal(t, autopilot.RunDone, r.engine.end(t, 21).Outcome)
}

func TestAStartFallsBackInOrderPastRestingProfilesAndAnAgentThatCantRunHere(t *testing.T) {
	t.Parallel()

	r, job := chainRig(t)
	later := time.Now().Add(time.Hour)
	job.Resting = []autopilot.Resting{resting(claudeID, defaultProfile, later), resting(claudeID, workProfile, later)}

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	require.True(t, res.Running, "the spare profile runs: %s", res.WaitWords)
	assert.Equal(t, "spare", res.Launched.Profile)

	asked := r.planner.requests()
	require.Len(t, asked, 4, "its own profile, then each fallback, in order")
	assert.Equal(t, []string{"/", claudeID + "/" + workProfile, "codex/" + defaultProfile, claudeID + "/spare"}, []string{
		string(asked[0].Agent) + "/" + asked[0].Profile, string(asked[1].Agent) + "/" + asked[1].Profile,
		string(asked[2].Agent) + "/" + asked[2].Profile, string(asked[3].Agent) + "/" + asked[3].Profile,
	})

	require.NotNil(t, asked[1].Model)
	assert.Equal(t, "opus", *asked[1].Model, "a fallback of the same agent keeps the step's model")
	assert.Nil(t, asked[2].Model, "another agent's takes its own launch defaults'")

	assert.Equal(t, autopilot.RunDone, r.engine.end(t, 21).Outcome)
}

func TestAStartWithEveryProfileRestingWaitsForTheSoonestReset(t *testing.T) {
	t.Parallel()

	r, job := chainRig(t)
	job.Step.Agent.Fallbacks = job.Step.Agent.Fallbacks[:1]

	soon, later := time.Now().Add(20*time.Minute), time.Now().Add(3*time.Hour)
	job.Resting = []autopilot.Resting{resting(claudeID, defaultProfile, later), resting("Claude", "WORK", soon)}

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	assert.False(t, res.Running)
	assert.Equal(t, autopilot.BlockOutOfUsage, res.Wait)
	assert.Equal(t, soon, res.RetryAt, "it is tried again once the soonest rest ends")
	assert.True(t, strings.HasPrefix(res.WaitWords, "Queued: the default profile is out of usage until "), res.WaitWords)
	assert.Contains(t, res.WaitWords, "the work profile is out of usage until")
}

func TestAStartWhoseOnlyFallbackCantRunHereWaitsAsNotReady(t *testing.T) {
	t.Parallel()

	r, job := chainRig(t)
	job.Step.Agent.Fallbacks = []spec.ProfileRef{{Agent: "codex", Profile: defaultProfile}}
	soon := time.Now().Add(time.Hour)
	job.Resting = []autopilot.Resting{resting(claudeID, defaultProfile, soon)}

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, res.Wait, "a fallback this Mac can't run waits as not ready")
	assert.Equal(t, soon, res.RetryAt)
	assert.Contains(t, res.WaitWords, "Codex isn't installed.")
	assert.Contains(t, res.WaitWords, "the default profile is out of usage until")
}

func TestAnUnrunnableOwnProfileIsNotReadyWithoutTryingItsFallbacks(t *testing.T) {
	t.Parallel()

	r, job := chainRig(t)
	r.planner.profiles["/"] = plannedProfile{err: agent.ErrPresetNotKept}

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, res.Wait, "a fallback is for a profile out of usage, not a step that can't run")
	assert.Len(t, r.planner.requests(), 1)
}

func TestARunOutOfUsageEndsSoWithItsLimit(t *testing.T) {
	t.Parallel()

	end := runEnd(agentrun.End{Outcome: agentrun.Outcome{
		Status: agentrun.StatusOutOfUsage, Summary: "Out of usage: its plan's five-hour limit is reached.",
		Limit: &agentrun.Limit{Status: "rejected", Type: "five_hour", ResetsAt: 1_763_300_000},
	}})

	assert.Equal(t, autopilot.RunOutOfUsage, end.Outcome)
	assert.Equal(t, "Out of usage: its plan's five-hour limit is reached.", end.Error)
	require.NotNil(t, end.Limit)
	assert.Equal(t, "five_hour", end.Limit.Type)
	assert.Equal(t, time.Unix(1_763_300_000, 0).UTC(), end.Limit.ResetsAt)

	failed := runEnd(agentrun.End{Outcome: agentrun.Outcome{Status: agentrun.StatusOutOfBudget, Summary: "Out of budget."}})
	assert.Equal(t, autopilot.RunFailed, failed.Outcome, "out of budget is a failure, which nothing falls back from")
	assert.Nil(t, failed.Limit)
}
