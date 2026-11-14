package issues

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/timeline"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestOpenBeforeAnyReadShowsTheCard(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	is := h.opened(t)

	assert.Equal(t, Issue{
		ID: issueID, Repo: exampleRepo, Number: 12, Title: parserBug, URL: issueURL, State: timeline.StateOpen,
		Author: octoOther, OpenedAt: stamp(openedAt), UpdatedAt: stamp(latestAt), Assigned: true,
		Reactions: []timeline.Reaction{}, Labels: []timeline.Label{{Name: "bug", Color: "#d73a4a"}}, Projects: []Project{},
		Assignees: []Person{}, Participants: []Person{}, SubIssues: []timeline.Ref{}, SubIssuesTotal: 3, SubIssuesDone: 1,
		Fixes: []Fix{}, Mentioned: []timeline.Ref{}, Timeline: timeline.Page{Items: []timeline.Entry{}},
		Seen:     Seen{Comments: 2},
		Standing: Standing{State: StandingAssigned, Sentence: "Assigned to you", At: stamp(openedAt), Opened: true},
		Offers: []Offer{
			{Action: ActionComment, Label: "Comment", Enabled: true},
			{
				Action: ActionCloseCompleted, Label: "Close as completed", Reason: waitForRead, Confirm: true,
				CommentsFirst: true,
			},
			{
				Action: ActionCloseNotPlanned, Label: "Close as not planned", Reason: waitForRead, Confirm: true,
				CommentsFirst: true,
			},
		},
		Primary: ActionComment, Stale: true,
	}, is)

	assert.Zero(t, h.github.called(callView), "opening asks GitHub nothing")
	assert.Zero(t, h.inbox.reads)
}

func TestRefreshReadsTheIssueOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.inbox.projects = true

	is := h.refreshed(t)

	assert.Equal(t, []github.DetailOptions{{Projects: true}}, h.github.viewOpts, "the projects, as the target says")
	require.Len(t, h.inbox.read, 1)
	assert.Equal(t, nodeIssue, h.inbox.read[0].Update.Item.NodeID, "the card is rewritten from the answer")

	assert.Equal(t, "It drops the newline at the end of the file.", is.Body)
	assert.Equal(t, &IssueType{Name: "Bug", Color: "#d73a4a", Description: "Something isn't working"}, is.Type)
	assert.Equal(t, []timeline.Reaction{{Content: "THUMBS_UP", Count: 4}}, is.Reactions)
	assert.Equal(t, []Person{{Login: octoUser, Mine: true}}, is.Assignees)
	assert.Equal(t, []Person{{Login: octoOther}, {Login: octoUser, Mine: true}}, is.Participants)
	assert.Equal(t, 1, is.MoreParticipants)
	assert.Equal(t, Seen{Comments: 2, Latest: "IC_2", LatestAt: stamp(latestAt)}, is.Seen)
	assert.Equal(t, stamp(testNow), is.FetchedAt)
	assert.False(t, is.Stale)
	assert.Empty(t, is.LastSeenAt, "the first visit has nothing to call new")

	require.Len(t, is.Timeline.Items, 3)
	assert.Equal(t, 40, is.Timeline.Total)
	assert.Equal(t, cursor, is.Timeline.Before)
	assert.True(t, is.Timeline.Items[0].Mine)
	assert.Equal(t, timeline.KindMentioned, is.Timeline.Items[1].Kind)
	assert.Equal(t, []timeline.Ref{{
		Kind: item.KindPR, Repo: exampleRepo, Number: 14, Title: "Keep the trailing newline",
		URL: "https://github.com/acme-org/example-repo/pull/14", State: timeline.StateOpen,
	}}, is.Mentioned)

	assert.Equal(t, "Assigned to you", is.Standing.Sentence)
	assert.True(t, offerOf(t, is, ActionCloseCompleted).Enabled, "a close waits only for the read")

	again := h.refreshed(t)
	assert.Equal(t, 1, h.github.called(callView), "a read in the last 20 seconds is not made again")
	assert.Equal(t, is, again)

	h.clock.Advance(refreshAfter)
	h.refreshed(t)
	assert.Equal(t, 2, h.github.called(callView), "a read 20 seconds old is made again")
}

func TestRefreshReadsAgainWhenTheCardShowsActivity(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	h.inbox.issue.Comments = 3
	h.refreshed(t)
	assert.Equal(t, 2, h.github.called(callView), "the card counts a comment the read doesn't")

	h.inbox.issue.Comments = 2
	h.inbox.issue.UpdatedAt = latestAt.Add(time.Minute)
	h.refreshed(t)
	assert.Equal(t, 3, h.github.called(callView), "the card was updated after the read")

	h.inbox.issue.UpdatedAt = latestAt.Add(500 * time.Millisecond)
	h.refreshed(t)
	assert.Equal(t, 3, h.github.called(callView), "GitHub's times are whole seconds")
}

func TestReadAgainReadsGitHubHoweverRecentlyItRead(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	_, err := h.svc.ReadAgain(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, 2, h.github.called(callView))
}

func TestReadsOfOneIssueAreJoined(t *testing.T) {
	t.Parallel()

	// In a synctest bubble, synctest.Wait returns once every goroutine in it is blocked,
	// which is when the second caller has joined the read in flight.
	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		h.github.gate, h.github.arrived = make(chan struct{}), make(chan struct{}, 1)

		type got struct {
			is  Issue
			err error
		}

		first, second := make(chan got, 1), make(chan got, 1)

		go func() {
			is, err := h.svc.ReadAgain(t.Context(), issueID)
			first <- got{is, err}
		}()

		<-h.github.arrived

		go func() {
			is, err := h.svc.Refresh(t.Context(), issueID)
			second <- got{is, err}
		}()

		synctest.Wait()
		close(h.github.gate)

		a, b := <-first, <-second
		require.NoError(t, a.err)
		require.NoError(t, b.err)
		assert.Equal(t, a.is, b.is, "both callers get the one read")
		assert.Equal(t, 1, h.github.called(callView), "GitHub was asked once")

		// A read the user asks for joins only a read of GitHub, never a refresh that served
		// the last read as it was.
		_, err := h.svc.Refresh(t.Context(), issueID)
		require.NoError(t, err)
		assert.Equal(t, 1, h.github.called(callView))

		h.github.gate = nil
		_, err = h.svc.ReadAgain(t.Context(), issueID)
		require.NoError(t, err)
		assert.Equal(t, 2, h.github.called(callView))
	})
}

func TestTheLastVisitMovesOnlyOnOpen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	assert.Empty(t, h.opened(t).LastSeenAt, "a first visit")
	assert.Empty(t, h.refreshed(t).LastSeenAt, "nor does its read move it")

	// The next visit starts from the read the last one showed.
	assert.Equal(t, stamp(latestAt), h.opened(t).LastSeenAt)

	commentedAt := latestAt.Add(30 * time.Minute)
	h.github.views = []*github.IssueView{later(issueView(), commentedAt)}
	h.inbox.issue.UpdatedAt, h.inbox.issue.Comments = commentedAt, 3

	is := h.refreshed(t)
	assert.Equal(t, stamp(latestAt), is.LastSeenAt, "a read keeps the visit's start, so octo-third's comment is new")
	assert.Equal(t, "IC_3", is.Timeline.Items[len(is.Timeline.Items)-1].ID)

	is, err := h.svc.ReadAgain(t.Context(), issueID)
	require.NoError(t, err)
	assert.Equal(t, stamp(latestAt), is.LastSeenAt, "so does a read the user asked for")

	assert.Equal(t, stamp(commentedAt), h.opened(t).LastSeenAt, "the visit after sees it as read")
}

func TestAClosingReadIsShownOnceThenForgotten(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.opened(t)

	closedAt := latestAt.Add(10 * time.Minute)
	closed := issueView()
	closed.Update.Item.State, closed.Update.Item.Title, closed.Update.Item.UpdatedAt = github.StateClosed, "Parser bug", closedAt
	closed.StateReason, closed.ClosedAt = github.StateReasonNotPlanned, closedAt
	closed.Timeline.Items = append(closed.Timeline.Items, github.TimelineItem{
		Type: github.TimelineClosed, ID: "CE_1", CreatedAt: closedAt, Actor: octoOther,
		StateReason: github.StateReasonNotPlanned,
	})
	h.github.views = []*github.IssueView{closed}

	is, err := h.svc.ReadAgain(t.Context(), issueID)
	require.NoError(t, err)

	assert.Equal(t, timeline.StateClosed, is.State)
	assert.Equal(t, timeline.ReasonNotPlanned, is.StateReason)
	assert.Equal(t, "Parser bug", is.Title, "the read's own title: the inbox doesn't rewrite a closed issue's card")
	assert.False(t, is.Assigned, "the issue left the lists")
	assert.Equal(t, Standing{
		State: StandingClosed, Sentence: "Closed as not planned by octo-other", At: stamp(closedAt),
	}, is.Standing)
	assert.Empty(t, is.Offers)
	assert.Equal(t, ActionNone, is.Primary)
	assert.False(t, is.Stale)
	assert.Equal(t, stamp(latestAt), is.LastSeenAt, "the visit's start, as for any read")
	assert.Nil(t, h.cached(t), "the sheet forgets a closed issue")
}

func TestAReadOfAGoneIssue(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.views = []*github.IssueView{{}}

	is, err := h.svc.ReadAgain(t.Context(), issueID)
	require.NoError(t, err)

	assert.Equal(t, StandingGone, is.Standing.State)
	assert.Empty(t, is.Offers)
	assert.False(t, is.Stale, "reading again says the same")
	assert.Equal(t, stamp(testNow), is.FetchedAt)
	assert.Equal(t, "It drops the newline at the end of the file.", is.Body, "what the sheet read before")
	assert.Nil(t, h.cached(t))

	h = newHarness(t)
	h.github.views = []*github.IssueView{{}}
	is = h.refreshed(t)
	assert.Equal(t, StandingGone, is.Standing.State, "one the sheet never read")
	assert.Empty(t, is.Body)
}

func TestRefreshFailures(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.clock.Advance(time.Minute)
	h.github.viewErr = errFakeRead

	_, err := h.svc.Refresh(t.Context(), issueID)
	require.ErrorIs(t, err, errFakeRead)
	assert.Equal(t, stamp(testNow), stamp(h.cached(t).fetchedAt), "a failed read leaves the cache")

	h.github.viewErr = nil
	other := issueView()
	other.Update.Item.NodeID = "I_another"
	h.github.views = []*github.IssueView{other}

	_, err = h.svc.Refresh(t.Context(), issueID)
	require.ErrorIs(t, err, github.ErrMalformedResponse, "an answer about another item")
	assert.Equal(t, stamp(testNow), stamp(h.cached(t).fetchedAt), "is never cached")

	h.inbox.issued = errFakeRead
	_, err = h.svc.Refresh(t.Context(), issueID)
	require.ErrorIs(t, err, errFakeRead)
	_, err = h.svc.Open(t.Context(), issueID)
	require.ErrorIs(t, err, errFakeRead)

	h.inbox.issued = nil
	h.inbox.issue.Kind = item.KindPR
	_, err = h.svc.Open(t.Context(), issueID)
	require.ErrorIs(t, err, ErrNotIssue)

	h.inbox.issue = storedIssue()
	h.inbox.issue.NodeID = ""
	_, err = h.svc.Earlier(t.Context(), issueID, cursor)
	require.ErrorIs(t, err, ErrNotIssue, "an item with no node ID can't be read")
}

func TestATokenGitHubRefusesIsTriedAgainForARead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.viewErr = badCredentials()

	_, err := h.svc.Refresh(t.Context(), issueID)
	require.Error(t, err)
	assert.Equal(t, 2, h.github.called(callView), "the inbox tries a fresh token once")
	assert.Nil(t, h.cached(t))
}

func TestEarlierReadsAPageAndKeepsNothing(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	h.github.timeline = &github.TimelinePage{Items: []github.TimelineItem{
		{Type: github.TimelineComment, ID: "IC_0", CreatedAt: openedAt, Actor: octoUser, Body: "First."},
		{Type: github.TimelineLabeled, ID: "LE_1", CreatedAt: openedAt, Actor: octoOther, Label: &github.Label{Name: "bug"}},
		{Type: "PinnedEvent", ID: "PE_1", CreatedAt: openedAt},
	}, Total: 40}

	page, err := h.svc.Earlier(t.Context(), issueID, cursor)
	require.NoError(t, err)

	assert.Equal(t, timeline.Page{Items: []timeline.Entry{
		{
			Kind: timeline.KindComment, ID: "IC_0", At: stamp(openedAt), Actor: octoUser, Mine: true, Body: "First.",
			LineComments: []timeline.LineComment{},
		},
		{
			Kind: timeline.KindLabeled, ID: "LE_1", At: stamp(openedAt), Actor: octoOther, Label: &timeline.Label{Name: "bug"},
			LineComments: []timeline.LineComment{},
		},
	}, Total: 40}, page, "the user's own marked, a type the sheet doesn't read left out, and no earlier page")
	assert.Equal(t, []string{cursor}, h.github.befores)
	assert.Equal(t, 1, h.inbox.peeks, "through the inbox, which stores nothing")
	assert.Equal(t, 1, h.github.called(callView))
	assert.Equal(t, "IC_1", h.cached(t).view.Timeline[0].ID, "the cache keeps the newest page")

	h.github.timelineErr = github.ErrInvalidCursor
	_, err = h.svc.Earlier(t.Context(), issueID, "")
	require.ErrorIs(t, err, github.ErrInvalidCursor)
	assert.Equal(t, "That part of the conversation can't be read. Reload the issue, then try again.", uierr.Message(err))

	h.github.timelineErr = uierr.New("GitHub can't find the issue any more.", github.ErrIssueGone)
	_, err = h.svc.Earlier(t.Context(), issueID, cursor)
	require.ErrorIs(t, err, github.ErrIssueGone)

	h.inbox.issued = errFakeRead
	_, err = h.svc.Earlier(t.Context(), issueID, cursor)
	require.ErrorIs(t, err, errFakeRead)
}

func TestOpenKeepsTheLastVisitOfAReadThatDoesntSayWhen(t *testing.T) {
	t.Parallel()

	h := readHarness(t)
	assert.Equal(t, stamp(latestAt), h.opened(t).LastSeenAt)

	_, err := h.db.ExecContext(t.Context(), `UPDATE issue_views SET view = json_set(view, '$.updatedAt', '')`)
	require.NoError(t, err)

	assert.Equal(t, stamp(latestAt), h.opened(t).LastSeenAt, "the last visit's, kept")
	assert.Equal(t, stamp(latestAt), h.opened(t).LastSeenAt, "and kept again")
}

func TestOpenStampsTheVisitEvenWhenTheStampFails(t *testing.T) {
	t.Parallel()

	h := readHarness(t)

	_, err := h.db.ExecContext(t.Context(), `CREATE TRIGGER no_open BEFORE UPDATE OF seen_at ON issue_views
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	is := h.opened(t)
	assert.Equal(t, stamp(latestAt), is.LastSeenAt, "the sheet opens all the same")
	assert.Contains(t, h.logs.String(), "could not stamp the cached issue")
}

// offerOf returns the issue's offer of a, which must be there.
func offerOf(t *testing.T, is Issue, a Action) Offer {
	t.Helper()

	for _, o := range is.Offers {
		if o.Action == a {
			return o
		}
	}

	require.Failf(t, "no offer", "%q is not offered", a)

	return Offer{}
}
