package eval_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// triggerCase is one trigger on one item.
type triggerCase struct {
	name    string
	trigger spec.Trigger
	mutate  func(*facts.Row)
	history eval.History
	at      time.Time
	want    eval.Result
	key     string
	need    facts.Need
}

// TestTriggers decides every trigger, with the fire key each one fires with.
func TestTriggers(t *testing.T) {
	t.Parallel()

	arrived := now.Add(-2 * time.Hour)
	quiet := now.Add(-10 * 24 * time.Hour)
	monday := now
	saturday := now.Add(5 * 24 * time.Hour)

	tests := []triggerCase{
		{name: "appears", trigger: spec.Trigger{Event: catalog.EventAppears}, want: eval.Yes, key: "appears:item"},

		{name: "new commits", trigger: spec.Trigger{Event: catalog.EventNewCommits}, want: eval.Yes, key: "new_commits:commit:" + head},
		{
			name: "new commits, a routine's own push", trigger: spec.Trigger{Event: catalog.EventNewCommits},
			history: eval.History{OwnHeads: []string{head}}, want: eval.No,
		},
		{
			name: "new commits, a routine's own push let through", trigger: spec.Trigger{Event: catalog.EventNewCommits, OwnPushesToo: ptr(true)},
			history: eval.History{OwnHeads: []string{head}}, want: eval.Yes, key: "new_commits:commit:" + head,
		},
		{
			name:    "new commits while a routine's agent works on it: told apart once it ends",
			trigger: spec.Trigger{Event: catalog.EventNewCommits}, history: eval.History{Working: true}, want: eval.Unknown,
		},
		{
			name: "new commits while an agent works, its own pushes let through", history: eval.History{Working: true},
			trigger: spec.Trigger{Event: catalog.EventNewCommits, OwnPushesToo: ptr(true)}, want: eval.Yes,
			key: "new_commits:commit:" + head,
		},
		{
			name: "new commits, not the first", trigger: spec.Trigger{Event: catalog.EventNewCommits, FirstToo: ptr(false)},
			history: eval.History{FirstHead: head}, want: eval.No,
		},
		{
			name: "new commits, after the first", trigger: spec.Trigger{Event: catalog.EventNewCommits, FirstToo: ptr(false)},
			history: eval.History{FirstHead: "1111111"}, want: eval.Yes, key: "new_commits:commit:" + head,
		},
		{
			name: "new commits, before the details", trigger: spec.Trigger{Event: catalog.EventNewCommits},
			mutate: func(r *facts.Row) { r.Details = nil }, want: eval.Unknown, need: facts.NeedDetails,
		},

		{name: "checks fail", trigger: spec.Trigger{Event: catalog.EventChecksFailed}, want: eval.Yes, key: "checks_failed:commit:" + head},
		{
			name: "checks fail, one still running", trigger: spec.Trigger{Event: catalog.EventChecksFailed},
			mutate: func(r *facts.Row) { r.Details.ChecksPending = 1 }, want: eval.No,
		},
		{
			name: "checks fail, not waiting", trigger: spec.Trigger{Event: catalog.EventChecksFailed, WaitForChecks: ptr(false)},
			mutate: func(r *facts.Row) { r.Details.ChecksPending, r.Checks = 1, nil }, want: eval.Yes, key: "checks_failed:commit:" + head,
		},
		{
			name: "checks fail, not settled yet", trigger: spec.Trigger{Event: catalog.EventChecksFailed, SettleMinutes: ptr(10)},
			want: eval.No,
		},
		{
			name: "checks fail, settled without waiting", trigger: spec.Trigger{Event: catalog.EventChecksFailed, SettleMinutes: ptr(0)},
			mutate: func(r *facts.Row) { r.Checks = nil }, want: eval.Yes, key: "checks_failed:commit:" + head,
		},
		{
			name: "checks fail, settling unknown", trigger: spec.Trigger{Event: catalog.EventChecksFailed},
			mutate: func(r *facts.Row) { r.Checks = nil }, want: eval.Unknown, need: facts.NeedChecks,
		},
		{
			name: "checks fail, none failed", trigger: spec.Trigger{Event: catalog.EventChecksFailed},
			mutate: func(r *facts.Row) { r.Details.ChecksFailed = 0 }, want: eval.No,
		},

		{
			name: "checks pass", trigger: spec.Trigger{Event: catalog.EventChecksPassed},
			mutate: passing, want: eval.Yes, key: "checks_passed:commit:" + head,
		},
		{
			name: "checks pass, runs waiting for a maintainer", trigger: spec.Trigger{Event: catalog.EventChecksPassed},
			mutate: func(r *facts.Row) { passing(r); r.Details.ChecksAwaiting = 1 }, want: eval.No,
		},
		{
			name: "checks pass, with no checks", trigger: spec.Trigger{Event: catalog.EventChecksPassed},
			mutate: func(r *facts.Row) { passing(r); r.Details.ChecksPassed = 0 }, want: eval.No,
		},
		{name: "checks pass, one failed", trigger: spec.Trigger{Event: catalog.EventChecksPassed}, want: eval.No},

		{
			name: "changes requested", trigger: spec.Trigger{Event: catalog.EventChangesRequested},
			mutate: func(r *facts.Row) { r.Details.ReviewDecision, r.Details.ChangesRequested = "CHANGES_REQUESTED", 1 },
			want:   eval.Yes, key: "changes_requested:commit:" + head + ":requests:1",
		},
		{name: "changes requested, none", trigger: spec.Trigger{Event: catalog.EventChangesRequested}, want: eval.No},

		{
			name: "approved", trigger: spec.Trigger{Event: catalog.EventApproved},
			mutate: func(r *facts.Row) { r.Details.Approvals = 1 }, want: eval.Yes, key: "approved:commit:" + head,
		},
		{name: "approved, not yet", trigger: spec.Trigger{Event: catalog.EventApproved}, want: eval.No},

		{
			name: "merge conflicts", trigger: spec.Trigger{Event: catalog.EventMergeConflicts},
			mutate: func(r *facts.Row) { r.Details.Mergeable = "CONFLICTING" }, want: eval.Yes, key: "merge_conflicts:commit:" + head,
		},
		{
			name: "merge conflicts, not worked out", trigger: spec.Trigger{Event: catalog.EventMergeConflicts},
			mutate: func(r *facts.Row) { r.Details.Mergeable = "UNKNOWN" }, want: eval.Unknown, need: facts.NeedDetails,
		},

		{
			name: "falls behind", trigger: spec.Trigger{Event: catalog.EventFallsBehind},
			mutate: func(r *facts.Row) { r.Details.MergeState = "BEHIND" }, want: eval.Yes,
			key: "falls_behind:commit:" + head + ":base:main",
		},
		{name: "falls behind, up to date", trigger: spec.Trigger{Event: catalog.EventFallsBehind}, want: eval.No},
		{
			name: "falls behind, the base not read", trigger: spec.Trigger{Event: catalog.EventFallsBehind},
			mutate: func(r *facts.Row) { r.Details.MergeState, r.Details.BaseRef = "BEHIND", "" },
			want:   eval.Unknown, need: facts.NeedDetails,
		},

		{
			name: "labeled", trigger: spec.Trigger{Event: catalog.EventLabeled, Label: "Dependencies"},
			want: eval.Yes, key: "labeled:label:dependencies",
		},
		{name: "labeled, another label", trigger: spec.Trigger{Event: catalog.EventLabeled, Label: "bug"}, want: eval.No},

		{
			name: "ready for review", trigger: spec.Trigger{Event: catalog.EventReadyForReview},
			want: eval.Yes, key: "ready_for_review:item",
		},
		{
			name: "ready for review, a draft", trigger: spec.Trigger{Event: catalog.EventReadyForReview},
			mutate: func(r *facts.Row) { r.Draft = true }, want: eval.No,
		},

		{
			name: "review requested", trigger: spec.Trigger{Event: catalog.EventReviewRequested},
			mutate: func(r *facts.Row) { r.ReviewRequestedAt = &arrived }, want: eval.Yes,
			key: "review_requested:at:2026-11-16T10:00:00Z",
		},
		{
			name: "review requested, the inbox not saying when", trigger: spec.Trigger{Event: catalog.EventReviewRequested},
			want: eval.Yes, key: "review_requested:listed",
		},
		{
			name: "review requested, not asked", trigger: spec.Trigger{Event: catalog.EventReviewRequested},
			mutate: func(r *facts.Row) { r.InReviewRequested = false }, want: eval.No,
		},

		{
			name: "assigned", trigger: spec.Trigger{Event: catalog.EventAssigned},
			mutate: func(r *facts.Row) { r.InAssigned, r.AssignedAt = true, &arrived }, want: eval.Yes,
			key: "assigned:at:2026-11-16T10:00:00Z",
		},
		{name: "assigned, to someone else", trigger: spec.Trigger{Event: catalog.EventAssigned}, want: eval.No},

		{
			name: "new comments", trigger: spec.Trigger{Event: catalog.EventNewComments, Count: ptr(3)},
			history: eval.History{CommentsSeen: ptr(1)}, want: eval.Yes, key: "new_comments:comments:4",
		},
		{
			name: "new comments, too few", trigger: spec.Trigger{Event: catalog.EventNewComments, Count: ptr(4)},
			history: eval.History{CommentsSeen: ptr(1)}, want: eval.No,
		},
		{
			name: "new comments, not counted yet", trigger: spec.Trigger{Event: catalog.EventNewComments},
			want: eval.No,
		},

		{
			name: "goes quiet", trigger: spec.Trigger{Event: catalog.EventGoesQuiet},
			mutate: func(r *facts.Row) { r.UpdatedAt = quiet }, want: eval.Yes, key: "goes_quiet:since:2026-11-06T12:00:00Z",
		},
		{name: "goes quiet, still busy", trigger: spec.Trigger{Event: catalog.EventGoesQuiet}, want: eval.No},

		{name: "a weekday schedule", trigger: weekday("08:30"), at: monday, want: eval.Yes, key: "schedule:2026-11-16"},
		{name: "a weekday schedule, later today", trigger: weekday("13:00"), at: monday, want: eval.No},
		{name: "a weekday schedule, on a Saturday", trigger: weekday("08:30"), at: saturday, want: eval.No},
		{
			name: "a weekly schedule", at: monday, want: eval.Yes, key: "schedule:2026-11-16",
			trigger: spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryWeek, On: catalog.Monday, At: "09:00"}},
		},
		{
			name: "a weekly schedule, another day", at: monday, want: eval.No,
			trigger: spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryWeek, On: catalog.Tuesday, At: "09:00"}},
		},
		{
			name: "a daily schedule on a Saturday", at: saturday, want: eval.Yes, key: "schedule:2026-11-21",
			trigger: spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryDay, At: "00:00"}},
		},

		{name: "only when I run it", trigger: spec.Trigger{Event: catalog.EventManual}, want: eval.No},
		{
			name: "only when I run it, run", trigger: spec.Trigger{Event: catalog.EventManual},
			history: eval.History{Manual: "click-1"}, want: eval.Yes, key: "manual:click-1",
		},
		{
			name: "a click on any routine", trigger: spec.Trigger{Event: catalog.EventChecksPassed},
			history: eval.History{Manual: "click-2"}, want: eval.Yes, key: "manual:click-2",
		},
	}

	fired := map[catalog.Event]bool{}

	for _, tt := range tests {
		fired[tt.trigger.Event] = fired[tt.trigger.Event] || tt.want == eval.Yes

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checkTrigger(t, tt)
		})
	}

	for _, trig := range catalog.Triggers() {
		assert.True(t, fired[trig.Event], "%s fires in a case, with its fire key", trig.Event)
	}
}

// checkTrigger decides one trigger case.
func checkTrigger(t *testing.T, tt triggerCase) {
	t.Helper()

	row := prRow()
	if tt.mutate != nil {
		tt.mutate(&row)
	}

	at := tt.at
	if at.IsZero() {
		at = now
	}

	d := eval.Evaluate(routine(tt.trigger, spec.Conditions{}), build(row), tt.history, at)
	assert.Equal(t, tt.want, d.Trace[0].Result)
	assert.Equal(t, tt.want, d.Match)
	assert.Equal(t, tt.key, d.FireKey)
	assert.Equal(t, tt.need, d.Trace[0].Need)
	assert.NotEmpty(t, d.Trace[0].Actual)
}

func TestTheInboxReasonTriggersSayTheReasonInWords(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		event  catalog.Event
		mutate func(*facts.Row)
		want   string
	}{
		{catalog.EventReviewRequested, nil, "it's asking for your review"},
		{catalog.EventReviewRequested, func(r *facts.Row) { r.InReviewRequested = false }, "it isn't asking for your review"},
		{catalog.EventAssigned, func(r *facts.Row) { r.InAssigned = true }, "it's assigned to you"},
		{catalog.EventAssigned, nil, "it isn't assigned to you"},
	} {
		row := prRow()
		if tt.mutate != nil {
			tt.mutate(&row)
		}

		d := eval.Evaluate(routine(spec.Trigger{Event: tt.event}, spec.Conditions{}), build(row), eval.History{}, now)
		assert.Equal(t, tt.want, d.Trace[0].Actual, "never a list's key, such as reviewRequested")
	}
}

// passing makes the pull request's checks all pass and settle.
func passing(r *facts.Row) {
	r.Details.ChecksFailed, r.Details.ChecksState = 0, "SUCCESS"
	r.Checks.Checks = []facts.CheckRow{{Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: now.Add(-5 * time.Minute)}}
}

// weekday is a schedule every weekday at a time.
func weekday(at string) spec.Trigger {
	return spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryWeekday, At: at}}
}

func TestPullRequestTriggersDontFireForIssues(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.Kind = item.KindIssue

	d := eval.Evaluate(routine(spec.Trigger{Event: catalog.EventMergeConflicts}, spec.Conditions{}), build(row), eval.History{}, now)
	assert.Equal(t, eval.No, d.Match)

	d = eval.Evaluate(routine(spec.Trigger{Event: catalog.EventAppears}, spec.Conditions{}), build(row), eval.History{}, now)
	assert.Equal(t, eval.Yes, d.Match)
}

func TestTriggerThisBuildDoesntRead(t *testing.T) {
	t.Parallel()

	d := eval.Evaluate(routine(spec.Trigger{Event: "deployed"}, spec.Conditions{}), build(prRow()), eval.History{}, now)
	assert.Equal(t, eval.Unknown, d.Match)
	assert.Empty(t, d.FireKey)
}

// TestOncePerOverTheTrigger checks the routine's own once-per over its trigger's fire key.
func TestOncePerOverTheTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		trigger spec.Trigger
		once    catalog.OncePer
		key     string
	}{
		{spec.Trigger{Event: catalog.EventChecksFailed}, "", "checks_failed:commit:" + head},
		{spec.Trigger{Event: catalog.EventChecksFailed}, catalog.OncePerHeadCommit, "checks_failed:commit:" + head},
		{spec.Trigger{Event: catalog.EventChecksFailed}, catalog.OncePerItem, "checks_failed:item"},
		{spec.Trigger{Event: catalog.EventChecksFailed}, catalog.OncePerDay, "checks_failed:day:2026-11-16"},
		{spec.Trigger{Event: catalog.EventLabeled, Label: "go"}, catalog.OncePerHeadCommit, "labeled:commit:" + head},
		{spec.Trigger{Event: catalog.EventAppears}, catalog.OncePerDay, "appears:day:2026-11-16"},
	}

	for _, tt := range tests {
		t.Run(string(tt.once)+" over "+string(tt.trigger.Event), func(t *testing.T) {
			t.Parallel()

			row := prRow()
			row.Details.Labels = []string{"go"}

			s := routine(tt.trigger, spec.Conditions{})
			s.Limits.OncePer = tt.once

			assert.Equal(t, tt.key, eval.Evaluate(s, build(row), eval.History{}, now).FireKey)
		})
	}

	s := routine(spec.Trigger{Event: catalog.EventAppears}, spec.Conditions{})
	s.Limits.OncePer = catalog.OncePerItem
	assert.Equal(t, "manual:click-3", eval.Evaluate(s, build(prRow()), eval.History{Manual: "click-3"}, now).FireKey,
		"a click is once per click, whatever the routine's once-per")
}
