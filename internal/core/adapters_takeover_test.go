package core

import (
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTakingOverABackgroundRunStopsItFirstAndWaitsForItsEnd(t *testing.T) {
	t.Parallel()

	r := newAgentRig(t)
	plan := r.planner.plan(t, testutil.FakeRun{
		Stdout: agentLines(t, "sigint.jsonl", 2), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Stdout: agentLastLine(t, "sigint.jsonl")},
	})
	r.planner.set(plan)

	res, err := r.agents.Dispatch(t.Context(), agentJob(21))
	require.NoError(t, err)
	require.NotNil(t, res.Launched)

	id := res.Launched.ScriptRunID

	var pid int

	require.Eventually(t, func() bool {
		if folders := r.agents.RunFolders(t.Context()); len(folders) == 1 {
			pid = folders[0].PID
		}

		return pid > 0
	}, waitAgent, 10*time.Millisecond, "the run works")

	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })

	assert.Equal(t, &launcher.Fence{Preset: fence.ReadOnly, RunDir: plan.Folder}, r.agents.fenceOf(t.Context(), id),
		"its session resumes under its run's own fence")

	require.NoError(t, r.agents.TakeOver(t.Context(), id))

	assert.Equal(t, []int64{21}, r.engine.takenOver(), "Autopilot is told before the run's end")

	run, err := r.history.Run(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, scriptrun.StatusStopped, run.Status, "by the time it returns, the run's end is recorded")
	assert.Equal(t, autopilot.RunStopped, r.engine.end(t, 21).Outcome)
	assert.Empty(t, r.agents.RunFolders(t.Context()), "its worktree is free for the session that carries it on")

	require.NoError(t, r.agents.TakeOver(t.Context(), id), "a run that ended is taken over without a stop")
	assert.Equal(t, []int64{21, 21}, r.engine.takenOver())

	err = r.agents.TakeOver(t.Context(), id+100)
	require.ErrorIs(t, err, launcher.ErrNotBackground, "a run the history doesn't hold")
	assert.Nil(t, r.agents.fenceOf(t.Context(), id+100))
}
