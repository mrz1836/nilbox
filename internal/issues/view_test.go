package issues

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/timeline"
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
	v.SubIssues = []github.SubIssue{
		{
			Ref:       github.ItemRef{Repo: exampleRepo, Number: 20, Title: "Handle CRLF", State: github.StateOpen},
			Assignees: github.People{Logins: []string{"Octo-User", octoOther}, Total: 4},
			SubIssues: github.SubIssues{Total: 3, Completed: 1},
		},
		{
			Ref: github.ItemRef{
				Repo: exampleRepo, Number: 21, State: github.StateClosed, StateReason: github.StateReasonNotPlanned,
			},
			ClosedAt: merged,
		},
	}
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
	assert.Equal(t, &timeline.Ref{
		Kind: item.KindIssue, Repo: exampleRepo, Number: 2, Title: "Parser",
		State: timeline.StateOpen,
	}, view.Parent)
	assert.Equal(t, timeline.ReasonCompleted, view.DuplicateOf.StateReason)
	assert.Equal(t, []cachedSubIssue{
		{
			Kind: item.KindIssue, Repo: exampleRepo, Number: 20, Title: "Handle CRLF", State: timeline.StateOpen,
			Assignees: []string{"Octo-User", octoOther}, AssigneesTotal: 4, Total: 3, Done: 1,
		},
		{
			Kind: item.KindIssue, Repo: exampleRepo, Number: 21, State: timeline.StateClosed,
			StateReason: timeline.ReasonNotPlanned,
			Assignees:   []string{}, ClosedAt: stamp(merged),
		},
	}, view.SubIssues)
	assert.Equal(t, []Fix{{
		PR: timeline.Ref{
			Kind: item.KindPR, Repo: exampleRepo, Number: 14, Title: "Keep the trailing newline",
			State: timeline.StateMerged,
		},
		Base: "main", MergedAt: stamp(merged), Checks: ChecksPassing, Review: ReviewApproved,
	}}, view.Fixes)
	assert.Equal(t, 3, view.FixCount)
	assert.True(t, view.LatestMine, "the user's comment, by their login in any case")
	assert.Equal(t, "acme-org/example-repo#14", view.ClosedBy, "the newest close names what closed it")

	last := view.Timeline[len(view.Timeline)-1]
	assert.Equal(t, timeline.Entry{
		Kind: timeline.KindRenamed, ID: "RTE_1", At: stamp(latestAt), Actor: octoOther, From: "Parser bug", To: parserBug,
		LineComments: []timeline.LineComment{},
	}, last)
	assert.True(t, view.Timeline[len(view.Timeline)-2].ActorIsBot)

	v.Timeline.Items = v.Timeline.Items[:len(v.Timeline.Items)-2]
	assert.Equal(t, octoOther, viewOf(v, octoUser).ClosedBy, "or who did")

	v.Timeline.Items = nil
	assert.Empty(t, viewOf(v, octoUser).ClosedBy, "a page with no close names nobody")
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

	pr14 := timeline.Ref{Kind: item.KindPR, Repo: exampleRepo, Number: 14}
	issue9 := timeline.Ref{Kind: item.KindIssue, Repo: "acme-org/api", Number: 9}

	got := mentioned(&cachedView{
		Timeline: []timeline.Entry{
			{Kind: timeline.KindMentioned, Ref: &pr14},
			{Kind: timeline.KindMentioned, Ref: &issue9},
			{Kind: timeline.KindLinked, Ref: &timeline.Ref{Repo: exampleRepo, Number: 15}},
			{Kind: timeline.KindMentioned, Ref: &timeline.Ref{Kind: item.KindIssue, Repo: "ACME-org/API", Number: 9}},
			{Kind: timeline.KindMentioned},
		},
		Fixes: []Fix{{PR: pr14}},
	})

	assert.Equal(t, []timeline.Ref{issue9}, got)
}

func TestMentionedLeavesOutWhatHasItsOwnPlace(t *testing.T) {
	t.Parallel()

	ref := func(number int) timeline.Ref {
		return timeline.Ref{Kind: item.KindIssue, Repo: exampleRepo, Number: number}
	}
	sub20, sub21, parent, duplicate, other := ref(20), ref(21), ref(2), ref(3), ref(9)

	view := &cachedView{
		Timeline: []timeline.Entry{
			{Kind: timeline.KindMentioned, Ref: &sub20},
			{Kind: timeline.KindMentioned, Ref: &parent},
			{Kind: timeline.KindMentioned, Ref: &other},
			{Kind: timeline.KindMentioned, Ref: &timeline.Ref{Kind: item.KindIssue, Repo: "ACME-org/Example-Repo", Number: 21}},
			{Kind: timeline.KindMentioned, Ref: &duplicate},
		},
		SubIssues:   []cachedSubIssue{{Ref: sub20}, {Ref: sub21}},
		Parent:      &parent,
		DuplicateOf: &duplicate,
	}

	assert.Equal(t, []timeline.Ref{other}, mentioned(view),
		"the sub-issues, in any case, the parent, and the issue it duplicates each have their place")

	view.SubIssues, view.Parent, view.DuplicateOf = nil, nil, nil
	assert.Len(t, mentioned(view), 5, "with none of them, each mention shows")
}

func TestBuildShowsEachSubIssue(t *testing.T) {
	t.Parallel()

	card := storedIssue()
	sub := timeline.Ref{Kind: item.KindIssue, Repo: exampleRepo, Number: 20, State: timeline.StateOpen}
	read := &cached{view: cachedView{State: timeline.StateOpen, SubIssues: []cachedSubIssue{
		{Ref: sub, Assignees: []string{"Octo-User", octoOther}, AssigneesTotal: 5, Total: 3, Done: 1},
		{Ref: sub, Total: 2, Done: 7},
		{Ref: sub, Total: -1, Done: -1, ClosedAt: "2026-11-10T08:00:00Z"},
	}}, fetchedAt: testNow}

	is := build(card, read, "", false)

	assert.Equal(t, []SubIssue{
		{
			Issue: sub, Assignees: []Person{{Login: "Octo-User", Mine: true}, {Login: octoOther}}, MoreAssignees: 3,
			SubIssuesTotal: 3, SubIssuesDone: 1,
		},
		{Issue: sub, Assignees: []Person{}, SubIssuesTotal: 2, SubIssuesDone: 2},
		{Issue: sub, Assignees: []Person{}, ClosedAt: "2026-11-10T08:00:00Z"},
	}, is.SubIssues, "the user's assignment marked, and the counts kept in bounds")
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

	is := build(card, &cached{view: cachedView{State: timeline.StateOpen}}, "", false)

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
		State: timeline.StateOpen, CanClose: true, LatestMine: true, LatestAt: "l", FixCount: 2,
		Fixes:       []Fix{{PR: timeline.Ref{Repo: exampleRepo, Number: 14, State: timeline.StateOpen, Draft: true}}},
		DuplicateOf: &timeline.Ref{Repo: exampleRepo, Number: 3},
	}}

	f := factsOf(storedIssue(), read, false)
	assert.True(t, f.Read)
	assert.True(t, f.RepliedLast)
	assert.Equal(t, "l", f.RepliedAt)
	assert.Equal(t, 1, f.MoreFixes)
	assert.Equal(t, "acme-org/example-repo#3", f.DuplicateOf)
	assert.Equal(t, []FixFacts{{Ref: "acme-org/example-repo#14", Open: true, Draft: true}}, f.Fixes)

	require.Equal(t, " as not planned", closedAs(timeline.ReasonNotPlanned))
	require.Empty(t, closedAs(timeline.ReasonReopened))
	require.Empty(t, closedAs("ARCHIVED"))
}
