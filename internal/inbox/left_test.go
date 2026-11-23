package inbox

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// The user can leave an item on GitHub itself rather than from the inbox, and GitHub's
// searches still return it for a while, as its index trails the edit, with the item's new
// activity. The details, read live, tell the lists the truth. The other way round, the user
// can assign themselves from a sheet, which the inbox records at once.

// The people these tests assign and ask besides the user, and a team of theirs.
const (
	octoAssignee = "octo-assignee"
	octoTeam     = "reviewers"
)

// listening gives h a service that tells the returned sink of arrivals, as arrivalHarness's
// does.
func listening(h *harness) *fakeSink {
	sink := &fakeSink{}
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events, Arrivals: sink,
	})

	return sink
}

// assignLeftAt returns the item's assign_left_at, or "" when there is none.
func (h *harness) assignLeftAt(t *testing.T, id int64) string {
	t.Helper()

	return h.column(t, "SELECT COALESCE(assign_left_at, '') FROM items WHERE id = ?", id)[0]
}

// assignedAt returns when the item arrived in the assigned list, or "" when it isn't in it.
func (h *harness) assignedAt(t *testing.T, id int64) string {
	t.Helper()

	return h.column(t, "SELECT COALESCE(assigned_at, '') FROM items WHERE id = ?", id)[0]
}

func TestARefreshLeavesOutAnIssueTheUserTookThemselvesOffOnGitHub(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)
	left, kept := byOther(issue("example-repo", 3, "MEMBER")), byOther(issue("example-repo", 4, "MEMBER"))
	h.client.details = map[string]github.ItemDetails{
		nodeID(3): {Assignees: []string{octoUser}}, nodeID(4): {Assignees: []string{octoUser}},
	}
	h.seed(t, nil, nil, []github.Item{left, kept})
	id := h.storedItemNumbered(t, 3).ID

	// The user takes themselves off #3 on GitHub, which moves it. The search still returns
	// it, with its new time, and three more past its page.
	h.tick()

	moved := left
	moved.UpdatedAt = h.clock.Now()
	h.client.details[nodeID(3)] = github.ItemDetails{Assignees: []string{octoAssignee}}
	h.client.inbox = fetched(nil, nil, []github.Item{moved, kept})
	h.client.inbox.Assigned.IssueCount = 5

	snap := h.refresh(t)

	assert.Equal(t, []int{4}, numbers(snap.Yours), "it leaves the assigned list")
	assert.Equal(t, 4, snap.Yours.Total, "and its total: the one shown and the three past the page")
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 3))
	assert.Empty(t, h.assignLeftAt(t, id), "nothing is stamped")
	assert.Empty(t, sink.calls, "and nothing arrives")

	// The next refresh, the search still behind, reads its details as an item new to the
	// lists, and leaves it out again.
	h.tick()
	h.client.detailsIDs = nil

	snap = h.seed(t, nil, nil, []github.Item{moved, kept})

	assert.Equal(t, []int{4}, numbers(snap.Yours))
	assert.Contains(t, h.client.detailsIDs, []string{nodeID(3)})
	assert.Empty(t, sink.calls)

	// Assigned again, which moves it: it arrives.
	h.tick()

	again := moved
	again.UpdatedAt = h.clock.Now()
	h.client.details[nodeID(3)] = github.ItemDetails{Assignees: []string{octoAssignee, octoUser}}

	snap = h.seed(t, nil, nil, []github.Item{again, kept})

	assert.Equal(t, []int{3, 4}, numbers(snap.Yours))
	assert.Equal(t, []string{"assigned #3"}, kindsAndNumbers(sink.last()))
}

func TestARefreshLeavesOutAReviewTheUserLeftOnGitHub(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)
	asked, other := byOther(pr(1, "MEMBER", false)), byOther(pr(2, "MEMBER", false))
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): asking([]string{octoUser}, []string{}), nodeID(2): asking([]string{octoUser}, []string{}),
	}
	h.seed(t, []github.Item{asked, other}, nil, []github.Item{asked})

	// The user takes themselves off #1's reviewers on GitHub, which moves it; the search
	// still returns it.
	h.tick()

	moved := asked
	moved.UpdatedAt = h.clock.Now()
	h.client.details[nodeID(1)] = asking([]string{octoAssignee}, []string{})

	snap := h.seed(t, []github.Item{moved, other}, nil, []github.Item{moved})

	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested), "it leaves Review requested")
	assert.Equal(t, 1, snap.ReviewRequested.Total, "and its total")
	assert.Equal(t, []int{1}, numbers(snap.Yours), "and stays in the list GitHub has it in")
	assert.Equal(t, ReviewRequestNone, itemByNumber(t, snap.Yours, 1).ReviewRequest)
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 1))
	assert.Empty(t, h.reviewLeftAt(t, h.storedItemNumbered(t, 1).ID), "nothing is stamped")
	assert.Empty(t, sink.calls, "and nothing arrives")

	// Asked again, which moves it: it arrives.
	h.tick()

	again := moved
	again.UpdatedAt = h.clock.Now()
	h.client.details[nodeID(1)] = asking([]string{octoAssignee, octoUser}, []string{})

	snap = h.seed(t, []github.Item{again, other}, nil, []github.Item{again})

	assert.Equal(t, []int{1, 2}, numbers(snap.ReviewRequested))
	assert.Equal(t, []string{"review_requested #1"}, kindsAndNumbers(sink.last()))
}

func TestRefreshItemTakesAnItemNoLongerAssignedOutOfTheAssignedList(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)
	a, b := byOther(pr(1, "MEMBER", false)), byOther(issue("example-repo", 2, "MEMBER"))
	h.seed(t, []github.Item{a}, nil, []github.Item{a, b})
	id := h.storedItemNumbered(t, 1).ID
	h.clock.Advance(10 * time.Minute)

	// The user took themselves off #1 on GitHub, which moved it; it still asks their review.
	now := a
	now.State, now.UpdatedAt = github.StateOpen, h.clock.Now()
	d := asking([]string{octoUser}, []string{})
	d.Assignees = []string{octoAssignee}
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: now, Details: d, RateLimit: itemRate()}}
	calls := h.events.Count()

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, []int{2}, assignedNumbers(snap), "it is no longer assigned to the user")
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested), "and stays where GitHub still has it")
	assert.Equal(t, [3]bool{true, false, false}, h.reasons(t, 1))
	assert.Equal(t, "2026-01-10 12:10:00+00:00", h.assignLeftAt(t, id), "stamped with GitHub's time as read")
	assert.Empty(t, h.assignedAt(t, id))
	assert.Equal(t, calls+1, h.events.Count(), "the window hears of it")

	// GitHub's search trails the edit: it still lists #1 as assigned, at the time the read
	// saw, and this refresh reads no details to tell it otherwise.
	h.clock.Advance(time.Minute)

	snap = h.seed(t, []github.Item{now}, nil, []github.Item{now, b})

	assert.Equal(t, []int{2}, assignedNumbers(snap), "a lagging search keeps it out")
	assert.Empty(t, sink.calls, "and nothing arrives")
}

func TestRefreshItemStampsNoItemOutOfTheAssignedList(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, nil, []github.Item{pr(1, "OWNER", false)}, nil)
	h.clock.Advance(10 * time.Minute)

	d := prDetails()
	d.Assignees = []string{}
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): open(h, 1, d)}
	id := h.storedItemNumbered(t, 1).ID

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	assert.Empty(t, h.assignLeftAt(t, id), "never assigned, so an assignment made later still arrives")
	assert.Equal(t, [3]bool{false, true, false}, h.reasons(t, 1))
}

func TestReadIssueTakesAnIssueNoLongerAssignedOutOfTheAssignedList(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)

	now := byOther(issue("example-repo", 3, "MEMBER"))
	now.State, now.UpdatedAt = github.StateOpen, h.clock.Now()

	snap, err := NewActor(h.svc).ReadIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueRead, error) {
		return IssueRead{Update: &github.ItemUpdate{
			Found: true, Item: now, Details: github.ItemDetails{Assignees: []string{octoAssignee}}, RateLimit: itemRate(),
		}}, nil
	})
	require.NoError(t, err)

	assert.Equal(t, []int{1}, assignedNumbers(snap), "the sheet's read takes it out of the assigned list")
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 3))
	assert.Equal(t, "2026-01-10 12:01:00+00:00", h.assignLeftAt(t, ids[3]))
}

func TestActRecordsTheUsersOwnAssignment(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := listening(h)

	// #2 asks a team the user is on, and isn't assigned to them; they assign themselves
	// from the review sheet, which GitHub stamps a second before now.
	at := h.clock.Now().Add(-time.Second)

	snap, err := NewActor(h.svc).Act(t.Context(), ids[2], acting(Acted{SelfAssigned: true, SelfAssignedAt: at}))
	require.NoError(t, err)

	assert.True(t, itemByNumber(t, snap.ReviewRequested, 2).Assigned, "assigned at once, with no read")
	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 2))
	assert.Equal(t, "2026-01-10 12:00:59+00:00", h.assignedAt(t, ids[2]), "arrived at GitHub's time of the edit")

	// The next refresh's search lists it as assigned too: listed before, it doesn't arrive.
	h.tick()

	a, c := byOther(pr(1, "MEMBER", false)), byOther(issue("example-repo", 3, "MEMBER"))
	moved := byOther(pr(2, "MEMBER", false))
	moved.UpdatedAt = at

	snap = h.seed(t, []github.Item{a, moved}, nil, []github.Item{a, moved, c})

	assert.True(t, itemByNumber(t, snap.ReviewRequested, 2).Assigned)
	assert.Equal(t, "2026-01-10 12:00:59+00:00", h.assignedAt(t, ids[2]), "it keeps when it arrived")
	assert.Empty(t, sink.calls, "the user's own assignment tells no arrival")
}

func TestActIssueRecordsTheUsersOwnAssignment(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := listening(h)

	// The user took themselves off #3 from the inbox, which a routine watches, then assigns
	// themselves again from its sheet.
	h.client.edited = &github.Edited{UpdatedAt: h.clock.Now()}

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.NoError(t, err)
	require.NotEmpty(t, h.assignLeftAt(t, ids[3]))
	h.exec(t, `INSERT INTO item_watches (watch_key, item_id, seen_at) VALUES (?, ?, ?)`, depQuery, ids[3], h.clock.Now().UTC())

	h.clock.Advance(time.Minute)
	at := h.clock.Now()

	snap, err := NewActor(h.svc).ActIssue(t.Context(), ids[3], func(context.Context, github.Token, IssueTarget) (IssueActed, error) {
		return IssueActed{SelfAssigned: true, SelfAssignedAt: at}, nil
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, []int{1, 3}, assignedNumbers(snap), "assigned at once, with no read")
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3))
	assert.Empty(t, h.assignLeftAt(t, ids[3]), "the stamp of leaving it is cleared, so the next search is believed")

	// The next refresh's search lists it: listed before, it doesn't arrive.
	h.tick()

	a, b := byOther(pr(1, "MEMBER", false)), byOther(pr(2, "MEMBER", false))
	moved := byOther(issue("example-repo", 3, "MEMBER"))
	moved.UpdatedAt = at

	snap = h.seed(t, []github.Item{a, b}, nil, []github.Item{a, moved})

	assert.ElementsMatch(t, []int{1, 3}, assignedNumbers(snap))
	assert.Empty(t, sink.calls, "the user's own assignment tells no arrival")
}

func TestActedSelfAssignedOverTwoTries(t *testing.T) {
	t.Parallel()

	early, late := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC), time.Date(2026, 1, 10, 12, 1, 0, 0, time.UTC)

	got := Acted{SelfAssigned: true, SelfAssignedAt: late}.with(Acted{SelfAssignedAt: early})
	assert.Equal(t, Acted{SelfAssigned: true, SelfAssignedAt: late}, got)
	assert.True(t, got.any(), "an assignment of the user's own is recorded")
	assert.False(t, Acted{}.any())
}
