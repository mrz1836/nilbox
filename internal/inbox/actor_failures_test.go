package inbox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

func TestAnActionGitHubTookButTheInboxCouldNotRecordIsReported(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		acted Acted
		event string
		want  string
	}{
		{
			name: "a review", acted: Acted{Reviewed: true, ReviewEvent: github.ReviewApprove},
			event: "BEFORE UPDATE OF review_left_at ON items", want: "record the review",
		},
		{
			name: "a review requested again",
			acted: Acted{Rerequested: true, ReviewRequests: github.ReviewRequests{
				Users: []string{octoUser}, Teams: []string{},
			}},
			event: "BEFORE UPDATE OF review_users ON item_details", want: "record the review request",
		},
		{
			name: "a reviewer edit",
			acted: Acted{RequestsEdited: true, ReviewRequests: github.ReviewRequests{
				Users: []string{"octo-reviewer"}, Teams: []string{"web"},
			}},
			event: "BEFORE UPDATE OF review_users ON item_details", want: "record the review requests",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h, id := itemHarness(t)
			refuse(t, h, tt.event, "")

			runs := 0

			_, err := NewActor(h.svc).Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
				runs++

				return tt.acted, nil
			})
			require.ErrorContains(t, err, tt.want)
			assert.Equal(t, 1, runs, "GitHub was asked once")
			assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1), "nothing was stored")
		})
	}
}
