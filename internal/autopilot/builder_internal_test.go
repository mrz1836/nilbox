package autopilot

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// TestRecordReasonsAreActivitysWords holds reasonWords to the window's reasonChips, label
// for label, curly apostrophes read as straight ones: the window's record is exhaustive
// over every reason the ledger has, so a reason added in Go that a test's record leaves
// unworded fails here.
func TestRecordReasonsAreActivitysWords(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "autopilot.ts"))
	require.NoError(t, err)

	start := strings.Index(string(source), "export const reasonChips")
	require.GreaterOrEqual(t, start, 0, "frontend/src/lib/autopilot.ts declares reasonChips")

	block, _, found := strings.Cut(string(source[start:]), "\n}\n")
	require.True(t, found)

	window := map[Reason]string{}

	for _, m := range regexp.MustCompile(`(?m)^\s+(\w+): \{ label: '([^']*)'`).FindAllStringSubmatch(block, -1) {
		window[Reason(m[1])] = strings.ReplaceAll(m[2], "’", "'")
	}

	assert.Equal(t, window, reasonWords())
}

func TestAChangeOfOncePerIsAskedAboutAsAChangeOfTheWatch(t *testing.T) {
	t.Parallel()

	before := Routine{Spec: spec.Spec{Trigger: spec.Trigger{Event: catalog.EventChecksFailed}}}
	assert.False(t, watchChanged(before, before))

	after := before
	after.Spec.Limits.OncePer = catalog.OncePerDay
	assert.True(t, watchChanged(before, after), "once per decides each match's occurrence, so every current one is new")

	after = before
	after.Spec.Limits.RunsPerDay = new(4)
	assert.False(t, watchChanged(before, after), "a cap changes no occurrence")
}

func TestARecordSaysWhereItStandsOrWhyItClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state  State
		reason Reason
		want   string
	}{
		{StateDone, ReasonRan, "It already ran on this."},
		{StateWaiting, "", "Waiting for your OK."},
		{StateClosed, ReasonMerged, "Closed: it merged."},
		{StateClosed, ReasonRan, "Closed."},
		{StateClosed, "", "Closed."},
		{StateSkipped, ReasonTries, "Gave up."},
		{StateSkipped, ReasonOutside, "Skipped: an outside contributor's."},
		{StateDismissed, ReasonYouSkipped, "You skipped it."},
		{StateBaseline, ReasonFromNow, "It already matched when you chose only what changes from now on."},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, recordWords(tt.state, tt.reason), "%s, %s", tt.state, tt.reason)
	}
}
