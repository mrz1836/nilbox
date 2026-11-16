package launcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// A trusted pull request's files aren't checked on a click, but when an agent run's commits
// wrote agent settings on it, the app's file check carries that finding (the inbox's
// check with fence.MergeChanged), and the next click at that head waits for the user, as
// an outside contributor's pull request would.
func TestARunThatWroteAgentFilesHoldsTheNextClick(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	changed := fence.MergeChanged(nil,
		fence.CheckChanged(holdHeadA, []string{".claude/settings.json", "go.sum"}, sensitive.DefaultConfig()))
	h.cfg.Files = &fakeFiles{checks: map[int64]*sensitive.Check{h.trustedPR: &changed}}

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	require.NotNil(t, res.Hold, "the click is held")
	assert.Equal(t, HoldAgentFiles, res.Hold.Reason)
	require.Len(t, res.Hold.Files, 1)
	assert.Equal(t, ".claude/settings.json", res.Hold.Files[0].Path)
	assert.Empty(t, h.iterm.opened, "nothing is started")

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Confirmed: res.Hold.Key})
	require.NoError(t, err)
	assert.Nil(t, res.Hold, "the user's OK at that head launches it")
}
