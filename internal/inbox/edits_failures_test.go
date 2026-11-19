package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestAReviewRequestGitHubTookOffButTheInboxCouldNotRecordSaysSo(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.edited = &github.Edited{ReviewRequests: github.ReviewRequests{Users: []string{"someone"}, Teams: []string{}}}

	refuse(t, h, "BEFORE UPDATE OF review_users ON item_details", "")

	_, err := h.svc.RemoveReviewRequest(t.Context(), ids[1])
	require.ErrorContains(t, err, "save the review requests")
	assert.Equal(t, "GitHub made the change, but the inbox could not record it. The next refresh catches it up.",
		uierr.Message(err))
	assert.Equal(t, []string{"unreview acme-org/example-repo#1 " + octoUser}, h.client.edits, "GitHub was asked once")
	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1), "nothing was stored")
}

func TestAnEditWhoseTokenCantBeCheckedChangesNothing(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.svc = h.service() // a token this Service has not used yet
	h.client.viewerErr = errFakeNetwork

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.ErrorIs(t, err, errFakeNetwork)
	require.ErrorContains(t, err, "check the signed-in account")
	assert.Empty(t, h.client.edits, "nothing is edited on a token whose account isn't known")
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3))
}
