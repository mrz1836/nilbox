package sim_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// A profile out of usage rests until its limit resets, and the step starts again on the
// next profile of its chain; with every one resting, the start waits, asking GitHub
// nothing, until the soonest rest ends.

// withFallbacks is the flagship's shape, falling back from its profile to the work
// profile, then to Codex.
func withFallbacks() spec.Spec {
	sp := sim.FixFailing()
	sp.Steps[0].Agent.Fallbacks = []spec.ProfileRef{{Agent: "claude", Profile: "work"}, {Agent: "codex", Profile: "default"}}

	return sp
}

func TestAProfileOutOfUsageRestsAndTheStepStartsAgainOnItsFallback(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, withFallbacks(), autopilotprefs.ModeAuto)
		s.Runner.LaunchAs("claude", "default")
		s.World.Fail(1)
		s.Refresh()

		first := only(t, s)
		require.Equal(t, autopilot.StateRunning, first.State, first.Words)
		assert.Empty(t, s.Runner.Started()[0].Resting, "no profile rests yet")

		// Its runner falls back to the work profile, as the chain says.
		s.Runner.LaunchAs("claude", "work")

		reset := s.Clock.Now().Add(3 * time.Hour).Truncate(time.Second)
		s.Runner.Finish(first.Runs[0].ID, autopilot.RunEnd{
			Outcome: autopilot.RunOutOfUsage, Error: "Out of usage: its plan's five-hour limit is reached.",
			Limit: &autopilot.UsageLimit{Type: "five_hour", ResetsAt: reset},
		})
		s.Advance(2*time.Minute, false)

		m := only(t, s)
		assert.Equal(t, autopilot.StateRunning, m.State, "an out-of-usage run closes nothing: %s", m.Words)
		assert.Equal(t, 1, m.Attempt, "and is no try: the run after it is the first")
		assert.Equal(t, string(autopilot.RunOutOfUsage), m.Runs[0].Outcome)

		require.Len(t, s.Runner.Started(), 2, "the step starts again")
		assert.Equal(t, []autopilot.Resting{{Agent: "claude", Profile: "default", Until: reset.UTC()}},
			s.Runner.Started()[1].Resting, "telling its runner the default profile rests, so it takes the next")

		// The work profile runs out too, and the runner finds nothing left: every profile
		// rests, so the start waits for the soonest to reset.
		second := m.Runs[1]
		sooner := s.Clock.Now().Add(time.Hour).Truncate(time.Second)

		s.Runner.AnswerWith(autopilot.Result{
			Wait: autopilot.BlockOutOfUsage, WaitWords: "Queued: the work profile is out of usage until 10:00.",
			RetryAt: sooner,
		})
		s.Runner.Finish(second.ID, autopilot.RunEnd{
			Outcome: autopilot.RunOutOfUsage, Limit: &autopilot.UsageLimit{ResetsAt: sooner},
		})
		s.Advance(2*time.Minute, false)

		require.Len(t, s.Runner.Held(), 1)
		assert.Len(t, s.Runner.Held()[0].Resting, 2, "both of its profiles rest")

		m = only(t, s)
		assert.Equal(t, autopilot.StateQueued, m.State)
		assert.Equal(t, autopilot.BlockOutOfUsage, m.BlockedBy)
		assert.Zero(t, m.Attempt)

		reads := s.World.Reads(1)

		s.Advance(40*time.Minute, false)
		assert.Len(t, s.Runner.Held(), 1, "nothing tries it again before the rest ends")
		assert.Equal(t, reads, s.World.Reads(1), "nor reads it from GitHub")

		// A restart remembers which profiles rest: the runner, asked once more, finds both
		// resting still, and the start waits again for the soonest.
		s.Runner.AnswerWith(autopilot.Result{
			Wait: autopilot.BlockOutOfUsage, WaitWords: "Queued: the work profile is out of usage until 10:00.",
			RetryAt: sooner,
		})
		s.Restart()
		s.Refresh()

		require.Len(t, s.Runner.Held(), 2)
		assert.Len(t, s.Runner.Held()[1].Resting, 2, "the rests outlive the restart")

		s.Advance(25*time.Minute, false)

		started := s.Runner.Started()
		require.Len(t, started, 3, "once the work profile's rest ends, the step starts again")
		assert.Equal(t, []autopilot.Resting{{Agent: "claude", Profile: "default", Until: reset.UTC()}},
			started[2].Resting, "with only the default profile still resting")
		assert.Equal(t, 1, only(t, s).Attempt, "the run that starts is its first try")
	})
}
