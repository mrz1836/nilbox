package autopilot

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestRunNowKeepsToTheKindsOfItemTheRoutineActsOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		r.start()

		// Checks fail fires only for pull requests, so the routine acts on no issue: a click
		// runs it whatever its trigger says, never on a kind of item it doesn't act on, with
		// Run anyway or without.
		for _, anyway := range []bool{false, true} {
			_, err := r.svc.RunNow(t.Context(), id, cmdIssue, anyway)
			require.ErrorIs(t, err, ErrNoMatch, "anyway: %v", anyway)
			assert.Equal(t, "It acts only on pull requests.", uierr.Message(err))
		}

		page, err := r.svc.Activity(t.Context(), 0, 0)
		require.NoError(t, err)
		assert.Empty(t, page.Days, "nothing recorded on the issue")

		m, err := r.svc.RunNow(t.Context(), id, cmdPR, false)
		require.NoError(t, err)
		assert.True(t, m.Manual, "a pull request it acts on runs")
	})
}
