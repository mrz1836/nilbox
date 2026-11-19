package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/github/githubtest"
)

func TestEditLabelsSendsEachChangeOnceAtTheHeadSeen(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)

	got, err := h.svc.EditLabels(t.Context(), LabelsRequest{
		ItemID: pullID, Head: headA, Add: []string{"needs-repro", " Needs-Repro ", "p1"}, Remove: []string{"bug", "p1"},
	})
	require.NoError(t, err)

	assert.Equal(t, LabelsResult{Sent: 2}, got)
	assert.Equal(t, []string{"+needs-repro", "-bug"}, h.github.labeled, "a name both added and removed is left alone")
	assert.Equal(t, 1, h.inbox.acts, "through the inbox's Actor")
}

func TestEditLabelsSendsNothingOnAPullRequestPushedToSince(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)

	got, err := h.svc.EditLabels(t.Context(), LabelsRequest{ItemID: pullID, Head: headB, Add: []string{"needs-repro"}})
	require.NoError(t, err)

	assert.True(t, got.PushedSince)
	assert.Zero(t, h.github.called(callLabel))
	assert.Zero(t, h.inbox.acts)
}

func TestEditLabelsNeverSendsAStepTwiceAcrossTheTokenRetry(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.github.unlabelErrs = map[string]error{"bug": githubtest.BadCredentials()}

	got, err := h.svc.EditLabels(t.Context(), LabelsRequest{
		ItemID: pullID, Head: headA, Add: []string{"needs-repro"}, Remove: []string{"bug"},
	})
	require.NoError(t, err)

	assert.Equal(t, LabelsResult{Sent: 2}, got)
	assert.Equal(t, []string{"+needs-repro", "-bug"}, h.github.labeled, "the labels added went once")
	assert.Equal(t, 1, h.github.called(callLabel))
	assert.Equal(t, 2, h.github.called(callUnlabel))
	assert.Equal(t, 2, h.inbox.tries)
}

func TestEditLabelsCountsWhatGitHubMadeBeforeAFailure(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.github.unlabelErrs = map[string]error{"bug": errGateShut}

	got, err := h.svc.EditLabels(t.Context(), LabelsRequest{
		ItemID: pullID, Head: headA, Add: []string{"needs-repro"}, Remove: []string{"bug"},
	})
	require.NoError(t, err, "a step GitHub made stands")

	assert.Equal(t, 1, got.Sent)
	assert.NotEmpty(t, got.Failed)

	h.github.labelErr = errGateShut

	_, err = h.svc.EditLabels(t.Context(), LabelsRequest{ItemID: pullID, Head: headA, Add: []string{"p1"}})
	require.ErrorIs(t, err, errGateShut, "a failure before any step is the error")
}

func TestEditLabelsNeedsTriageAccess(t *testing.T) {
	t.Parallel()

	h := readHarness(t, github.MergeStateBlocked)
	h.inbox.pull.ViewerPermission = "READ"

	_, err := h.svc.EditLabels(t.Context(), LabelsRequest{ItemID: pullID, Head: headA, Add: []string{"needs-repro"}})
	require.ErrorIs(t, err, ErrCannotEditLabels)
	assert.Zero(t, h.github.called(callLabel))
}
