package autopilot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestTodaysRunsCountTheAgentsAndTheGitHubActionsSentSinceMidnight(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC)
	r := newStatsRig(t, now)
	agent := r.match(0, step(catalog.StepAgent, ""))

	// Today's agents in the background: one done, one interrupted, and one still going.
	r.run(agent, weekRun{ago: time.Hour, runner: runnerBackground, outcome: RunDone})
	r.run(agent, weekRun{ago: 2 * time.Hour, runner: runnerBackground, outcome: RunInterrupted})
	r.run(agent, weekRun{ago: time.Minute, runner: runnerBackground, going: true})

	// One in the terminal, and one GitHub action sent.
	r.run(agent, weekRun{ago: time.Hour, runner: "terminal", outcome: RunDone})
	r.run(r.match(0, step(catalog.StepGitHub, "")), weekRun{ago: time.Hour, runner: runnerGitHub, outcome: RunDone})

	// What isn't one of today's runs: a start that never happened, a GitHub action that
	// sent nothing, one of the user's scripts (no agent, and no GitHub action), a run of
	// yesterday's, and one going since yesterday.
	r.run(agent, weekRun{ago: time.Hour, runner: runnerBackground, outcome: RunNotStarted})
	r.run(r.match(0, step(catalog.StepGitHub, "")), weekRun{ago: time.Hour, runner: runnerGitHub, outcome: RunNotSent})
	r.run(r.match(0, step(catalog.StepScript, "")), weekRun{ago: time.Hour, runner: runnerScript, outcome: RunDone})
	r.run(agent, weekRun{ago: 10 * time.Hour, runner: runnerBackground, outcome: RunDone})
	r.run(agent, weekRun{ago: 10 * time.Hour, runner: "terminal", going: true})

	got, err := r.svc.RunsToday(t.Context())
	require.NoError(t, err)
	assert.Equal(t, RunsToday{Background: 3, Terminal: 1, Actions: 1}, got)
}

func TestTodaysRunsAreNoneWithoutADatabase(t *testing.T) {
	t.Parallel()

	got, err := NewService(Config{}).RunsToday(t.Context())
	require.NoError(t, err)
	assert.Equal(t, RunsToday{}, got)

	_, err = NewService(Config{Pool: testutil.ClosedDB(t)}).RunsToday(t.Context())
	require.Error(t, err)
	assert.Equal(t, "Autopilot's runs couldn't be read.", uierr.Message(err))
}
