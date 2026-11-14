package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// issueRequest is the body the issue sheet's reads send.
type issueRequest struct {
	Query     string `json:"query"`
	Variables struct {
		ID     string `json:"id"`
		Before string `json:"before"`
		Last   int    `json:"last"`
	} `json:"variables"`
}

// readIssueRequest decodes the request r carries.
func readIssueRequest(t *testing.T, r *http.Request) issueRequest {
	t.Helper()

	raw, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	var req issueRequest
	assert.NoError(t, json.Unmarshal(raw, &req))

	return req
}

// pageSizes records the timeline page sizes requests ask for, as a fake GitHub hears them.
type pageSizes struct {
	mu  sync.Mutex
	got []int
}

func (p *pageSizes) add(last int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.got = append(p.got, last)
}

func (p *pageSizes) all() []int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]int(nil), p.got...)
}

// octoUser is the signed-in account the fixtures scrub logins to.
const octoUser = "octo-user"

// at is a time on the fixtures' days, in UTC.
func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, time.UTC)
}

func TestFetchIssueViewReadsTheSheet(t *testing.T) {
	t.Parallel()

	body := fixture(t, "issue_view_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		assert.Contains(t, req.Query, "node(id: $id) { ...ItemFields ...DetailFields ...IssueViewFields }")
		assert.Contains(t, req.Query, "fragment IssueViewFields on Issue")
		assert.Contains(t, req.Query, "fragment TimelineFields on IssueTimelineItemsConnection")
		assert.Contains(t, req.Query, "timeline: timelineItems(last: $last, itemTypes: "+timelineItemTypes+")")
		assert.Contains(t, req.Query, "lastComment: comments(last: 1)")
		assert.NotContains(t, req.Query, "projectItems(", "projects are read only when asked")
		assert.NotContains(t, req.Query, "enableDuplicate", "GitHub retired the argument")
		assert.Equal(t, "I_fakeNodeId12", req.Variables.ID)
		assert.Equal(t, timelinePage, req.Variables.Last)

		setRateHeaders(w, ResourceGraphQL, 4992, 8)
		_, _ = w.Write(body)
	})

	rates := &recordRates{}
	client.Rates = rates

	view, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", DetailOptions{})
	require.NoError(t, err)

	require.True(t, view.Update.Found)
	assert.False(t, view.Update.Item.IsPR)
	assert.Equal(t, 12, view.Update.Item.Number)
	assert.Equal(t, StateOpen, view.Update.Item.State)
	assert.Equal(t, "octo-other", view.Update.Item.AuthorLogin)
	assert.Equal(t, at(12, 10, 0), view.Update.Item.UpdatedAt)
	assert.Equal(t, 5, view.Update.Details.Comments, "the card reads its comments too")
	assert.Equal(t, SubIssues{Total: 2, Completed: 1}, view.Update.Details.SubIssues)
	assert.Equal(t, 1, view.Update.RateLimit.Cost)
	assert.False(t, view.Update.ProjectsRefused)

	assert.True(t, strings.HasPrefix(view.Body, "Steps to reproduce:"), view.Body)
	assert.Equal(t, at(9, 9, 30), view.LastEditedAt)
	assert.Equal(t, StateReasonReopened, view.StateReason)
	assert.True(t, view.ClosedAt.IsZero())
	assert.False(t, view.Locked)
	assert.Empty(t, view.LockReason)
	assert.True(t, view.ViewerCanClose)
	assert.False(t, view.ViewerCanReopen)
	assert.True(t, view.ViewerCanUpdate)
	assert.False(t, view.ViewerDidAuthor)
	assert.Equal(t, IssueType{Name: "Bug", Description: "An unexpected problem or behavior", Color: "#fa4549"}, view.Type)
	assert.Equal(t, &Milestone{
		Title: "v1.2", URL: "https://github.com/acme-org/example-repo/milestone/3", DueOn: at(20, 0, 0), State: "OPEN",
		Progress: 40,
	}, view.Milestone)
	assert.Nil(t, view.DuplicateOf)
	assert.Equal(t, &ItemRef{
		Repo: "acme-org/example-repo", Number: 3, Title: "Parser epic", State: StateOpen,
		URL: "https://github.com/acme-org/example-repo/issues/3",
	}, view.Parent)
	assert.Equal(t, People{Logins: []string{octoUser, "octo-other"}, Total: 2}, view.Assignees)
	assert.Len(t, view.Participants.Logins, 10)
	assert.Equal(t, 12, view.Participants.Total, "the count runs past the logins read")
	require.Len(t, view.SubIssues, 2)
	assert.Equal(t, StateReasonCompleted, view.SubIssues[0].StateReason)
	assert.Equal(t, []LinkedPull{
		{
			Ref: ItemRef{
				IsPR: true, Repo: "acme-org/example-repo", Number: 14, Title: "Keep the trailing newline",
				URL: "https://github.com/acme-org/example-repo/pull/14", State: StateOpen, IsDraft: true,
			},
			Base: "main", ReviewDecision: "REVIEW_REQUIRED", Checks: "FAILURE",
		},
		{
			Ref: ItemRef{
				IsPR: true, Repo: "acme-org/example-repo", Number: 15, Title: "Fix the parser on release-1.2",
				URL: "https://github.com/acme-org/example-repo/pull/15", State: StateMerged,
			},
			Base: "release-1.2", MergedAt: at(10, 8, 0), ReviewDecision: "APPROVED", Checks: "SUCCESS",
		},
	}, view.Fixes)
	assert.Equal(t, 2, view.FixCount)
	assert.Equal(t, []Reaction{{Content: "THUMBS_UP", Count: 4}, {Content: "HEART", Count: 1}}, view.Reactions,
		"the kinds nobody reacted with are left out")
	assert.Equal(t, &CommentMark{ID: "IC_fakeComment18", CreatedAt: at(12, 10, 0), Author: "octo-carol"}, view.LatestComment)

	assert.True(t, view.Timeline.HasEarlier)
	assert.Equal(t, "Y3Vyc29yOnYyOpfakeCursor12==", view.Timeline.Before)
	assert.Equal(t, 112, view.Timeline.Total)
	assert.Equal(t, view.Update.RateLimit, view.Timeline.RateLimit)
	assert.Len(t, view.Timeline.Items, 18, "the null node is skipped")

	observed := rates.all()
	require.NotEmpty(t, observed)
	assert.Equal(t, 1, observed[len(observed)-1].spent, "the read spends the point it reports")
}

func TestFetchIssueViewReadsEachTimelineItem(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveFixture(t, "issue_view_response.json", ""))

	view, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", DetailOptions{})
	require.NoError(t, err)

	items := view.Timeline.Items
	require.Len(t, items, 18)

	assert.Equal(t, TimelineItem{
		Type: TimelineComment, ID: "IC_fakeComment1", CreatedAt: at(9, 10, 0), Actor: "octo-bob", ActorType: "User",
		Body: "Can repro on 1.4 with a file that ends in `\\n`.", Association: "CONTRIBUTOR", EditedAt: at(9, 10, 5),
		URL: "https://github.com/acme-org/example-repo/issues/12#issuecomment-1",
	}, items[0])

	assert.Equal(t, TimelineCrossReferenced, items[1].Type)
	assert.True(t, items[1].WillClose, "a pull request that closes it once merged")
	assert.Equal(t, 14, items[1].Ref.Number)
	assert.True(t, items[1].Ref.IsPR)
	assert.True(t, items[2].CrossRepo)
	assert.Equal(t, "acme-org/other-repo", items[2].Ref.Repo)

	assert.True(t, items[3].Minimized)
	assert.Equal(t, "SPAM", items[3].MinimizedReason)
	assert.Empty(t, items[3].Actor, "a deleted account names no one")
	assert.Equal(t, "Bot", items[4].ActorType)

	assert.Equal(t, &Label{Name: "bug", Color: "#d73a4a"}, items[5].Label)
	assert.Equal(t, TimelineUnlabeled, items[7].Type)
	assert.Equal(t, &Label{Name: "needs-triage"}, items[7].Label, "a color that is no color is dropped")
	assert.Equal(t, octoUser, items[8].Assignee)
	assert.Equal(t, "octo-dave", items[9].Assignee)

	// A connection's two sides: the one that isn't this issue, whichever side it is on.
	assert.Equal(t, TimelineConnected, items[10].Type)
	assert.Equal(t, 15, items[10].Ref.Number)
	assert.Equal(t, TimelineDisconnected, items[11].Type)
	assert.Equal(t, 16, items[11].Ref.Number)

	assert.Equal(t, TimelineItem{
		Type: TimelineClosed, ID: "CE_fakeEvent13", CreatedAt: at(10, 10, 0), Actor: "octo-other", ActorType: "User",
		StateReason: StateReasonCompleted, Commit: "1111111",
		URL: "https://github.com/acme-org/example-repo/commit/" + strings.Repeat("1", 40),
	}, items[12], "closed by a commit")
	assert.Equal(t, StateReasonReopened, items[13].StateReason)
	assert.Equal(t, "Parser drops newline", items[14].PreviousTitle)
	assert.Equal(t, "Parser drops the trailing newline", items[14].CurrentTitle)
	assert.Equal(t, TimelineMarkedAsDuplicate, items[15].Type)
	assert.Equal(t, 9, items[15].Ref.Number, "a duplicate of this issue, which is the canonical one")

	assert.True(t, items[16].Mine)
	assert.Equal(t, "MEMBER", items[16].Association)
	assert.Equal(t, "IC_fakeComment18", items[17].ID)
}

func TestFetchIssueViewReadsTheProjectsWhenAsked(t *testing.T) {
	t.Parallel()

	body := fixture(t, "issue_view_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		assert.Contains(t, req.Query, "projectItems(")
		assert.Contains(t, req.Query, "fragment ProjectFieldName on ProjectV2FieldConfiguration")
		assert.Contains(t, req.Query, "fragment IssueViewFields on Issue")

		_, _ = w.Write(body)
	})

	view, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", DetailOptions{Projects: true})
	require.NoError(t, err)
	assert.False(t, view.Update.ProjectsRefused)
	assert.Equal(t, 1, view.Update.RateLimit.Cost, "the projects cost nothing more")
}

func TestFetchIssueViewAsksAgainWithoutTheProjectsWhenTheScopeIsMissing(t *testing.T) {
	t.Parallel()

	body := fixture(t, "issue_view_response.json")

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		if strings.Contains(readIssueRequest(t, r).Query, "projectItems(") {
			_, _ = io.WriteString(w, refusedProjects)

			return
		}

		_, _ = w.Write(body)
	})

	view, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", DetailOptions{Projects: true})
	require.NoError(t, err)

	assert.True(t, view.Update.ProjectsRefused)
	assert.True(t, view.Update.Found)
	assert.Len(t, view.Timeline.Items, 18)
	assert.Equal(t, int32(2), requests.Load())
}

func TestFetchIssueViewAsksForASmallerPageWhenTheAnswerIsTooLarge(t *testing.T) {
	t.Parallel()

	body := fixture(t, "issue_view_response.json")
	huge := `{"data":{"node":{"body":"` + strings.Repeat("x", maxResponseBytes) + `"}}}`

	pages := &pageSizes{}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		pages.add(req.Variables.Last)

		if req.Variables.Last == timelinePage {
			_, _ = io.WriteString(w, huge)

			return
		}

		_, _ = w.Write(body)
	})

	view, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", DetailOptions{})
	require.NoError(t, err)
	assert.True(t, view.Update.Found)
	assert.Equal(t, []int{timelinePage, timelineSmallPage}, pages.all())

	// An answer too large even then fails, saying so.
	client = newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, huge)
	})

	_, err = client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", DetailOptions{})
	require.ErrorIs(t, err, ErrResponseTooLarge)
	require.ErrorIs(t, err, ErrMalformedResponse, "a too large answer is still a malformed one")
}

func TestFetchIssueViewOfAPullRequestIsMalformed(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveFixture(t, "item_response.json", ""))

	_, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "PR_fakeNodeId12", DetailOptions{})
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestFetchIssueViewOfAnIssueThatIsGone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"data":{"node":null,"rateLimit":{"cost":1,"remaining":4990}},`+
		`"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a node"}]}`))

	view, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "I_gone", DetailOptions{})
	require.NoError(t, err)
	assert.False(t, view.Update.Found)
	assert.Empty(t, view.Timeline.Items)
	assert.Equal(t, 4990, view.Update.RateLimit.Remaining, "the budget reading still counts")
}

func TestFetchIssueViewFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "errors without data", body: `{"errors":[{"message":"timeout"}]}`, want: ErrGraphQL},
		{name: "a null node for another reason", body: `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"no"}]}`, want: ErrGraphQL},
		{name: "not an item", body: `{"data":{"node":{"__typename":"Repository","id":"R_x"}}}`, want: ErrMalformedResponse},
		{
			name: "a view that isn't one",
			body: `{"data":{"node":{"__typename":"Issue","id":"I_x","number":7,"issueState":"OPEN",` +
				`"repository":{"name":"example-repo","owner":{"login":"acme-org"}},"comments":{"totalCount":1},"timeline":[]}}}`,
			want: ErrMalformedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := newTestClient(t, serveItem(tt.body)).FetchIssueView(t.Context(), NewToken(fakeToken), "I_x", DetailOptions{})
			require.ErrorIs(t, err, tt.want)
		})
	}

	var requests atomic.Int32

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) { requests.Add(1) })

	_, err := client.FetchIssueView(t.Context(), NewToken(fakeToken), "", DetailOptions{})
	require.ErrorIs(t, err, ErrInvalidItemRef)
	assert.Zero(t, requests.Load(), "nothing is sent without an ID")
}

func TestFetchIssueStateReadsTheNewestComments(t *testing.T) {
	t.Parallel()

	body := `{"data":{"node":{"__typename":"Issue","issueState":"OPEN","stateReason":"REOPENED","viewerCanClose":true,
		"recent":{"totalCount":7,"nodes":[
			{"id":"IC_fakeComment6","createdAt":"2026-11-12T09:00:00Z","viewerDidAuthor":true,"author":{"login":"octo-user"}},
			null,
			{"id":"IC_fakeComment7","createdAt":"2026-11-12T10:00:00+02:00","viewerDidAuthor":false,"author":null}]}},
		"rateLimit":{"cost":1,"remaining":4980}}}`

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		assert.Contains(t, req.Query, "recent: comments(last: 10)")
		assert.Contains(t, req.Query, "viewerCanClose")
		assert.Equal(t, "I_fakeNodeId12", req.Variables.ID)

		_, _ = io.WriteString(w, body)
	})

	state, err := client.FetchIssueState(t.Context(), NewToken(fakeToken), "I_fakeNodeId12")
	require.NoError(t, err)
	assert.Equal(t, &IssueState{
		Found: true, State: StateOpen, StateReason: StateReasonReopened, CanClose: true, Comments: 7,
		Recent: []CommentMark{
			{ID: "IC_fakeComment6", CreatedAt: at(12, 9, 0), Author: octoUser, Mine: true},
			{ID: "IC_fakeComment7", CreatedAt: at(12, 8, 0)},
		},
	}, state, "a null comment is skipped, and times are in UTC")
}

func TestFetchIssueStateOfAnIssueThatIsGoneOrNone(t *testing.T) {
	t.Parallel()

	gone := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"NOT_FOUND","message":"gone"}]}`))

	state, err := gone.FetchIssueState(t.Context(), NewToken(fakeToken), "I_gone")
	require.NoError(t, err)
	assert.Equal(t, &IssueState{}, state)

	for name, body := range map[string]string{
		"a pull request":         `{"data":{"node":{"__typename":"PullRequest"}}}`,
		"an issue with no state": `{"data":{"node":{"__typename":"Issue"}}}`,
		"not an object":          `{"data":{"node":"x"}}`,
	} {
		_, err = newTestClient(t, serveItem(body)).FetchIssueState(t.Context(), NewToken(fakeToken), "I_x")
		require.ErrorIs(t, err, ErrMalformedResponse, name)
	}

	failing := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"no"}]}`))

	_, err = failing.FetchIssueState(t.Context(), NewToken(fakeToken), "I_x")
	require.ErrorIs(t, err, ErrGraphQL)

	_, err = failing.FetchIssueState(t.Context(), NewToken(fakeToken), "")
	require.ErrorIs(t, err, ErrInvalidItemRef)
}

func TestFetchTimelineSendsTheCursor(t *testing.T) {
	t.Parallel()

	body := fixture(t, "issue_timeline_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		assert.Contains(t, req.Query, "timelineItems(last: $last, before: $before, itemTypes: "+timelineItemTypes+")")
		assert.Contains(t, req.Query, "number repository { nameWithOwner }")
		assert.Equal(t, "I_fakeNodeId12", req.Variables.ID)
		assert.Equal(t, "Y3Vyc29yOnYyOpfakeCursor12==", req.Variables.Before)
		assert.Equal(t, timelinePage, req.Variables.Last)

		_, _ = w.Write(body)
	})

	page, err := client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindIssue, "I_fakeNodeId12", "Y3Vyc29yOnYyOpfakeCursor12==")
	require.NoError(t, err)

	assert.False(t, page.HasEarlier)
	assert.Empty(t, page.Before, "the first page has no cursor to read further")
	assert.Equal(t, 112, page.Total)
	assert.Equal(t, 1, page.RateLimit.Cost)
	require.Len(t, page.Items, 5, "a type the sheet doesn't read and a null node are skipped")

	assert.Equal(t, "IC_fakeComment90", page.Items[0].ID)
	assert.Equal(t, &ItemRef{
		IsPR: true, Repo: "acme-org/example-repo", Number: 13, Title: "Parse the last line", State: StateMerged,
		URL: "https://github.com/acme-org/example-repo/pull/13",
	}, page.Items[1].Ref, "closed by a pull request")
	assert.Empty(t, page.Items[1].Commit)
	assert.Empty(t, page.Items[2].Actor, "a reopen by a deleted account")
	assert.Equal(t, StateReasonDuplicate, page.Items[3].StateReason)
	assert.Equal(t, 9, page.Items[3].DuplicateOf.Number)
	assert.Nil(t, page.Items[3].Ref, "a close as a duplicate names no closer")
}

func TestFetchTimelineOfAnIssueThatIsGone(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"NOT_FOUND","message":"gone"}]}`))

	_, err := client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindIssue, "I_gone", "cursor")
	require.ErrorIs(t, err, ErrIssueGone)
	assert.Equal(t, "GitHub can't find the issue any more, or your account can no longer see it.", uierr.Message(err))
}

func TestFetchTimelineRefusesABadCursorAndWhatIsNotAnIssue(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, `{"data":{"node":{"__typename":"PullRequest","number":7}}}`)
	})

	for _, before := range []string{"", strings.Repeat("c", maxCursorBytes+1)} {
		_, err := client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindIssue, "I_fakeNodeId12", before)
		require.ErrorIs(t, err, ErrInvalidCursor)
		require.NotErrorIs(t, err, ErrInvalidItemRef, "the issue itself is fine")
	}

	_, err := client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindIssue, "", "cursor")
	require.ErrorIs(t, err, ErrInvalidItemRef)
	assert.Zero(t, requests.Load(), "nothing is sent for a cursor or an ID that can't be one")

	_, err = client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindIssue, "PR_x", "cursor")
	require.ErrorIs(t, err, ErrMalformedResponse)

	for name, body := range map[string]string{
		"errors without data": `{"errors":[{"message":"timeout"}]}`,
		"another null node":   `{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"no"}]}`,
		"not an object":       `{"data":{"node":"x"}}`,
	} {
		_, err = newTestClient(t, serveItem(body)).FetchTimeline(t.Context(), NewToken(fakeToken), item.KindIssue, "I_x", "cursor")
		require.Error(t, err, name)
	}
}

func TestFetchTimelineAsksForASmallerPageWhenTheAnswerIsTooLarge(t *testing.T) {
	t.Parallel()

	body := fixture(t, "issue_timeline_response.json")

	pages := &pageSizes{}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		pages.add(req.Variables.Last)

		if req.Variables.Last == timelinePage {
			_, _ = io.WriteString(w, strings.Repeat(" ", maxResponseBytes+1))

			return
		}

		_, _ = w.Write(body)
	})

	page, err := client.FetchTimeline(t.Context(), NewToken(fakeToken), item.KindIssue, "I_fakeNodeId12", "cursor")
	require.NoError(t, err)
	assert.Len(t, page.Items, 5)
	assert.Equal(t, []int{timelinePage, timelineSmallPage}, pages.all())
}

func TestTimelineItemsOfAnUnknownSideOrKind(t *testing.T) {
	t.Parallel()

	self := ItemRef{Repo: "acme-org/example-repo", Number: 12}
	other := &graphQLRef{TypeName: typeIssue, Number: 12, Repository: &struct {
		NameWithOwner string `json:"nameWithOwner"`
	}{NameWithOwner: "Acme-Org/Example-Repo"}}

	// A connection whose only side is this issue, in another casing, names nothing.
	assert.Nil(t, otherSide(self, other, nil))

	// A closer that is a project, and an event without an ID, are no item to show.
	it, ok := (&graphQLTimelineNode{TypeName: TimelineClosed, ID: "CE_x", Closer: &graphQLRef{TypeName: "ProjectV2"}}).item(self, "")
	require.True(t, ok)
	assert.Nil(t, it.Ref)
	assert.Empty(t, it.Commit)

	_, ok = (&graphQLTimelineNode{TypeName: TimelineComment}).item(self, "")
	assert.False(t, ok)

	var missing *graphQLMilestone
	assert.Nil(t, missing.milestone())
	assert.Equal(t, People{Logins: []string{octoUser}, Total: 1}, graphQLPeople{
		TotalCount: -3, Nodes: []*struct {
			Login string `json:"login"`
		}{{Login: octoUser}, nil, {}},
	}.people(), "a count below the logins read is never shown")
}

func TestShownCountLeavesOutWhatTheTokenCantRead(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                  string
		total, returned, kept int
		want                  int
	}{
		{"every node read", 12, 10, 10, 12},
		{"a null among the first page", 12, 10, 9, 11},
		{"only nulls", 2, 2, 0, 0},
		{"a count below what was kept", 0, 3, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, shownCount(tc.total, tc.returned, tc.kept))
		})
	}
}

func TestATooLargeAnswerStillCountsItsPoint(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		setRateHeaders(w, ResourceGraphQL, 4990, 10)
		_, _ = io.WriteString(w, strings.Repeat(" ", maxResponseBytes+1))
	})

	rates := &recordRates{}
	client.Rates = rates

	_, err := client.FetchIssueState(t.Context(), NewToken(fakeToken), "I_fakeNodeId12")
	require.ErrorIs(t, err, ErrResponseTooLarge)

	spent := 0
	for _, o := range rates.all() {
		spent += o.spent
	}

	assert.Equal(t, minQueryCost, spent, "a query GitHub answered cost a point, however large the answer")
}
