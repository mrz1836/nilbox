package autopilot_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot"
)

func TestEveryStateIsOpenOrClosedTheOpenOnesFirst(t *testing.T) {
	t.Parallel()

	states := autopilot.States()
	assert.Len(t, states, 10)

	for i, s := range states {
		assert.Equal(t, i < 5, s.Open(), s)
	}

	assert.False(t, autopilot.State("teleported").Open(), "a state this build doesn't know isn't one it acts on")
}

func TestEveryVerdictIsNamedOnce(t *testing.T) {
	t.Parallel()

	seen := map[autopilot.Verdict]bool{}

	for _, v := range autopilot.Verdicts() {
		assert.NotEmpty(t, v)
		assert.False(t, seen[v], "%s is listed once", v)

		seen[v] = true
	}

	assert.True(t, seen[autopilot.VerdictFixed])
	assert.True(t, seen[autopilot.VerdictOvertaken])
}
