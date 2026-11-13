package issues

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
)

func TestViewOfKeepsWhatTheSheetShows(t *testing.T) {
	t.Parallel()

	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	merged := testNow.Add(-30 * time.Minute)
	pr14 := github.ItemRef{
		IsPR: true, Repo: exampleRepo, Number: 14, Title: "Keep the trailing newline", State: github.StateMerged,
	}

	v := issueView()
	v.LastEditedAt, v.Locked, v.LockReason, v.ViewerDidAuthor = latestAt, true, "RESOLVED", true
	v.Milestone = &github.Milestone{
		Title: "1.5", URL: "https://github.com/acme-org/example-repo/milestone/2", DueOn: due,
		State: "OPEN", Progress: 40,
	}
	v.Parent = &github.ItemRef{Repo: exampleRepo, Number: 2, Title: "Parser", State: github.StateOpen}
	v.DuplicateOf = &github.ItemRef{
		Repo: exampleRepo, Number: 3, State: github.StateClosed, StateReason: github.StateReasonCompleted,
	}
	v.SubIssues = []github.ItemRef{{Repo: exampleRepo, Number: 20, State: github.StateOpen}}
	v.Fixes = []github.LinkedPull{{Ref: pr14, Base: "main", MergedAt: merged, ReviewDecision: "APPROVED", Checks: "SUCCESS"}}
	v.FixCount = 3
	v.LatestComment = &github.CommentMark{ID: "IC_9", CreatedAt: latestAt, Author: "Octo-User"}
	v.Type = github.IssueType{}
	v.Timeline.Items = append(v.Timeline.Items,
		github.TimelineItem{Type: github.TimelineClosed, ID: "CE_1", CreatedAt: latestAt, Actor: octoOther},
		github.TimelineItem{Type: github.TimelineReopened, ID: "RE_1", CreatedAt: latestAt, Actor: octoOther},
		github.TimelineItem{
			Type: github.TimelineClosed, ID: "CE_2", CreatedAt: latestAt, Actor: "github-actions",
			ActorType: "Bot", Ref: &pr14, Commit: "abc123",
		},
		github.TimelineItem{
			Type: github.TimelineRenamed, ID: "RTE_1", CreatedAt: latestAt, Actor: octoOther,
			PreviousTitle: "Parser bug", CurrentTitle: parserBug,
		},
	)

	view := viewOf(v, octoUser)

	assert.Equal(t, stamp(latestAt), view.UpdatedAt)
	assert.Equal(t, stamp(latestAt), view.EditedAt)
	assert.True(t, view.Locked)
	assert.Equal(t, "RESOLVED", view.LockReason)
	assert.True(t, view.Mine)
	assert.Nil(t, view.Type, "no type")
	assert.Equal(t, &Milestone{
		Title: "1.5", URL: "https://github.com/acme-org/example-repo/milestone/2",
		DueOn: "2026-12-01T00:00:00Z", State: "OPEN", Progress: 40,
	}, view.Milestone)
	assert.Equal(t, &Linked{
		Kind: item.KindIssue, Repo: exampleRepo, Number: 2, Title: "Parser",
		State: StateOpen,
	}, view.Parent)
	assert.Equal(t, ReasonCompleted, view.DuplicateOf.StateReason)
	assert.Len(t, view.SubIssues, 1)
	assert.Equal(t, []Fix{{
		PR: Linked{
			Kind: item.KindPR, Repo: exampleRepo, Number: 14, Title: "Keep the trailing newline",
			State: StateMerged,
		},
		Base: "main", MergedAt: stamp(merged), Checks: ChecksPassing, Review: ReviewApproved,
	}}, view.Fixes)
	assert.Equal(t, 3, view.FixCount)
	assert.True(t, view.LatestMine, "the user's comment, by their login in any case")
	assert.Equal(t, "acme-org/example-repo#14", view.ClosedBy, "the newest close names what closed it")

	last := view.Timeline[len(view.Timeline)-1]
	assert.Equal(t, TimelineItem{
		Kind: EventRenamed, ID: "RTE_1", At: stamp(latestAt), Actor: octoOther, From: "Parser bug", To: parserBug,
	}, last)
	assert.True(t, view.Timeline[len(view.Timeline)-2].ActorIsBot)

	v.Timeline.Items = v.Timeline.Items[:len(v.Timeline.Items)-2]
	assert.Equal(t, octoOther, viewOf(v, octoUser).ClosedBy, "or who did")

	v.Timeline.Items = nil
	assert.Empty(t, viewOf(v, octoUser).ClosedBy, "a page with no close names nobody")
}

func TestEveryTimelineTypeTheSheetReads(t *testing.T) {
	t.Parallel()

	for typ, want := range map[string]EventKind{
		github.TimelineComment: EventComment, github.TimelineCrossReferenced: EventMentioned,
		github.TimelineConnected: EventLinked, github.TimelineDisconnected: EventUnlinked,
		github.TimelineClosed: EventClosed, github.TimelineReopened: EventReopened,
		github.TimelineAssigned: EventAssigned, github.TimelineUnassigned: EventUnassigned,
		github.TimelineLabeled: EventLabeled, github.TimelineUnlabeled: EventUnlabeled,
		github.TimelineRenamed: EventRenamed, github.TimelineMarkedAsDuplicate: EventDuplicate,
	} {
		got, ok := eventKind(typ)
		assert.True(t, ok, typ)
		assert.Equal(t, want, got, typ)
	}

	_, ok := eventKind("PinnedEvent")
	assert.False(t, ok)
}

func TestTheWordsOfAPullRequest(t *testing.T) {
	t.Parallel()

	for state, want := range map[string]ChecksWord{
		"SUCCESS": ChecksPassing, "failure": ChecksFailing, "ERROR": ChecksFailing, "PENDING": ChecksRunning,
		"EXPECTED": ChecksRunning, "": ChecksNone, "NEUTRAL": ChecksNone,
	} {
		assert.Equal(t, want, checksWord(state), state)
	}

	for decision, want := range map[string]ReviewWord{
		"APPROVED": ReviewApproved, "CHANGES_REQUESTED": ReviewChangesRequested, "review_required": ReviewRequired,
		"": ReviewNone, "DISMISSED": ReviewNone,
	} {
		assert.Equal(t, want, reviewWord(decision), decision)
	}
}

func TestMentionedListsEachOnceButTheFixes(t *testing.T) {
	t.Parallel()

	pr14 := Linked{Kind: item.KindPR, Repo: exampleRepo, Number: 14}
	issue9 := Linked{Kind: item.KindIssue, Repo: "acme-org/api", Number: 9}

	got := mentioned([]TimelineItem{
		{Kind: EventMentioned, Ref: &pr14},
		{Kind: EventMentioned, Ref: &issue9},
		{Kind: EventLinked, Ref: &Linked{Repo: exampleRepo, Number: 15}},
		{Kind: EventMentioned, Ref: &Linked{Kind: item.KindIssue, Repo: "ACME-org/API", Number: 9}},
		{Kind: EventMentioned},
	}, []Fix{{PR: pr14}})

	assert.Equal(t, []Linked{issue9}, got)
}

func TestStale(t *testing.T) {
	t.Parallel()

	card := storedIssue()
	read := &cached{view: cachedView{UpdatedAt: stamp(latestAt), Comments: 2}, fetchedAt: testNow}

	assert.True(t, stale(card, nil), "never read")
	assert.False(t, stale(card, read))
	assert.True(t, stale(card, &cached{view: read.view}), "out of date since an action")
	assert.True(t, stale(card, &cached{view: cachedView{Comments: 2}, fetchedAt: testNow}), "a read that doesn't say when")

	card.Comments = 3
	assert.True(t, stale(card, read), "a comment the read doesn't have")

	card = storedIssue()
	card.UpdatedAt = latestAt.Add(time.Second)
	assert.True(t, stale(card, read), "activity after the read")
}

func TestABuiltIssueHoldsNoNilList(t *testing.T) {
	t.Parallel()

	card := storedIssue()
	card.Labels, card.Projects = nil, []github.ProjectItem{{
		Title: "Roadmap", Status: "Todo",
		Fields: []github.ProjectField{{Name: "Priority", Value: "P1", Color: "#ff0000"}},
	}}

	is := build(card, &cached{view: cachedView{State: StateOpen}}, "", false)

	assert.NotNil(t, is.Labels)
	assert.NotNil(t, is.Reactions)
	assert.NotNil(t, is.Assignees)
	assert.NotNil(t, is.SubIssues)
	assert.NotNil(t, is.Fixes)
	assert.NotNil(t, is.Timeline.Items)
	assert.Equal(t, []Project{{
		Title: "Roadmap", Status: "Todo",
		Fields: []ProjectField{{Name: "Priority", Value: "P1", Color: "#ff0000"}},
	}}, is.Projects)
	assert.Empty(t, is.FetchedAt, "an action put the read out of date")
	assert.True(t, is.Stale)
}

func TestTheFactsOfARead(t *testing.T) {
	t.Parallel()

	read := &cached{view: cachedView{
		State: StateOpen, CanClose: true, LatestMine: true, LatestAt: "l", FixCount: 2,
		Fixes:       []Fix{{PR: Linked{Repo: exampleRepo, Number: 14, State: StateOpen, Draft: true}}},
		DuplicateOf: &Linked{Repo: exampleRepo, Number: 3},
	}}

	f := factsOf(storedIssue(), read, false)
	assert.True(t, f.Read)
	assert.True(t, f.RepliedLast)
	assert.Equal(t, "l", f.RepliedAt)
	assert.Equal(t, 1, f.MoreFixes)
	assert.Equal(t, "acme-org/example-repo#3", f.DuplicateOf)
	assert.Equal(t, []FixFacts{{Ref: "acme-org/example-repo#14", Open: true, Draft: true}}, f.Fixes)

	require.Equal(t, " as not planned", closedAs(ReasonNotPlanned))
	require.Empty(t, closedAs(ReasonReopened))
	require.Empty(t, closedAs("ARCHIVED"))
}
