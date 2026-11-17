package autopilot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// digestNow is when the digests below are gathered.
func digestNow() time.Time { return time.Date(2026, time.November, 16, 8, 30, 0, 0, time.UTC) }

// pull is a pull request's facts with values set.
func pull(number int, values map[catalog.FieldID]facts.Value) digestEntry {
	ref := ItemRef{ID: int64(number), Kind: item.KindPR, Repo: "acme-org/example-repo", Number: number}

	return digestEntry{
		matchID: int64(100 + number), item: ref,
		facts: facts.Facts{Item: facts.Item{ID: ref.ID, Kind: item.KindPR}, Values: values, Updated: digestNow()},
	}
}

// issue is an issue's facts, last active at updated.
func issue(number int, updated time.Time) digestEntry {
	ref := ItemRef{ID: int64(number), Kind: item.KindIssue, Repo: "acme-org/example-repo", Number: number}

	return digestEntry{
		matchID: int64(100 + number), item: ref,
		facts: facts.Facts{Item: facts.Item{ID: ref.ID, Kind: item.KindIssue}, Updated: updated},
	}
}

func TestADigestGroupsEachItemByWhereItStands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		entry digestEntry
		want  string
	}{
		{name: "failing checks", entry: pull(1, map[catalog.FieldID]facts.Value{
			catalog.FieldChecksState: facts.Text("failure"), catalog.FieldConflicts: facts.Bool(true),
		}), want: digestFailing},
		{name: "erroring checks", entry: pull(2, map[catalog.FieldID]facts.Value{
			catalog.FieldChecksState: facts.Text("error"),
		}), want: digestFailing},
		{name: "a conflict, its checks passing", entry: pull(3, map[catalog.FieldID]facts.Value{
			catalog.FieldChecksState: facts.Text("success"), catalog.FieldConflicts: facts.Bool(true),
		}), want: digestConflicts},
		{name: "changes requested", entry: pull(4, map[catalog.FieldID]facts.Value{
			catalog.FieldReviewDecision: facts.Text("changesRequested"), catalog.FieldBehind: facts.Bool(true),
		}), want: digestChanges},
		{name: "behind its base", entry: pull(5, map[catalog.FieldID]facts.Value{
			catalog.FieldBehind: facts.Bool(true),
		}), want: digestBehind},
		{name: "checks running", entry: pull(6, map[catalog.FieldID]facts.Value{
			catalog.FieldChecksState: facts.Text("pending"),
		}), want: digestRunning},
		{name: "a draft", entry: pull(7, map[catalog.FieldID]facts.Value{
			catalog.FieldDraft: facts.Bool(true),
		}), want: digestDraft},
		{name: "approved", entry: pull(8, map[catalog.FieldID]facts.Value{
			catalog.FieldReviewDecision: facts.Text("approved"),
		}), want: digestApproved},
		{name: "nothing known", entry: pull(9, map[catalog.FieldID]facts.Value{
			catalog.FieldChecksState: facts.Unknown("not read", facts.NeedDetails),
			catalog.FieldConflicts:   facts.Unknown("not read", facts.NeedDetails),
		}), want: digestWaiting},
		{name: "a quiet issue", entry: issue(10, digestNow().Add(-8*24*time.Hour)), want: digestQuiet},
		{name: "an issue active lately", entry: issue(11, digestNow().Add(-time.Hour)), want: digestOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, digestGroup(tt.entry.facts, digestNow()))
		})
	}
}

func TestADigestCountsAndListsTheMostPressingFirst(t *testing.T) {
	t.Parallel()

	failing := map[catalog.FieldID]facts.Value{catalog.FieldChecksState: facts.Text("failure")}
	conflicting := map[catalog.FieldID]facts.Value{catalog.FieldConflicts: facts.Bool(true)}

	d := digestOf([]digestEntry{
		pull(1, conflicting), pull(2, failing), pull(3, conflicting), pull(4, failing), pull(5, failing),
	}, digestNow())

	assert.Equal(t, []DigestGroup{{Label: digestFailing, Count: 3}, {Label: digestConflicts, Count: 2}}, d.Groups)

	numbers := make([]int, 0, len(d.Items))
	for _, it := range d.Items {
		numbers = append(numbers, it.Item.Number)
	}

	assert.Equal(t, []int{2, 4, 5, 1, 3}, numbers, "failing first, each group in the order decided")
	assert.Equal(t, "pull requests", d.Noun)
	assert.Equal(t, "5 pull requests need you", d.Title())
	assert.Equal(t, "3 failing, 2 conflicting: acme-org/example-repo #2, acme-org/example-repo #4, "+
		"acme-org/example-repo #5, acme-org/example-repo #1, and acme-org/example-repo #3.", d.Body(""))
}

func TestADigestNamesMoreThanItListsByCount(t *testing.T) {
	t.Parallel()

	var entries []digestEntry
	for n := 1; n <= 8; n++ {
		entries = append(entries, pull(n, nil))
	}

	d := digestOf(entries, digestNow())

	assert.Equal(t, "8 waiting for review: acme-org/example-repo #1, acme-org/example-repo #2, acme-org/example-repo #3, "+
		"acme-org/example-repo #4, acme-org/example-repo #5, and 3 more. Waiting on you.", d.Body("Waiting on you"))
}

func TestADigestNamesItsItems(t *testing.T) {
	t.Parallel()

	bot := func(n int, tool string) digestEntry {
		return pull(n, map[catalog.FieldID]facts.Value{catalog.FieldDepTool: facts.Text(tool)})
	}

	tests := []struct {
		name    string
		entries []digestEntry
		title   string
	}{
		{name: "one pull request", entries: []digestEntry{pull(1, nil)}, title: "1 pull request needs you"},
		{
			name: "Dependabot's", entries: []digestEntry{bot(1, "dependabot"), bot(2, "dependabot")},
			title: "2 Dependabot pull requests need you",
		},
		{
			name: "two bots'", entries: []digestEntry{bot(1, "dependabot"), bot(2, "renovate")},
			title: "2 pull requests need you",
		},
		{
			name: "a bot's and a person's", entries: []digestEntry{bot(1, "renovate"), bot(2, "none")},
			title: "2 pull requests need you",
		},
		{name: "issues", entries: []digestEntry{issue(1, digestNow()), issue(2, digestNow())}, title: "2 issues need you"},
		{name: "one of each", entries: []digestEntry{issue(1, digestNow()), pull(2, nil)}, title: "2 items need you"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.title, digestOf(tt.entries, digestNow()).Title())
		})
	}
}

func TestADigestsNoticeOpensItsRoutineAndIsToldOnce(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	d := digestOf([]digestEntry{pull(1, nil), pull(2, nil)}, digestNow())
	d.RoutineCreated = created

	quiet := false
	job := Job{
		RoutineID: 3, RoutineName: "Weekday nudge", Digest: &d,
		Step: spec.Step{Notify: &spec.NotifyStep{Note: "Waiting on you", Banner: &quiet}},
	}

	n := digestNotice(job)
	assert.Equal(t, NoticeAlert, n.Kind)
	assert.Nil(t, n.Item, "about no one item")
	assert.Equal(t, int64(3), n.RoutineID)
	assert.Zero(t, n.MatchID, "it opens the routine")
	assert.Equal(t, created, n.CreatedAt)
	assert.Equal(t, "2 pull requests need you", n.Title)
	assert.Contains(t, n.Body, "Waiting on you.")
	require.NotNil(t, n.Banner)
	assert.False(t, *n.Banner, "the step's own banner switch")

	assert.Equal(t, n.Key, digestNotice(job).Key, "told again, it is the same notice")

	other := digestOf([]digestEntry{pull(3, nil)}, digestNow())
	other.RoutineCreated = created
	assert.NotEqual(t, n.Key, digestNotice(Job{RoutineID: 3, Digest: &other, Step: job.Step}).Key,
		"a later digest of other matches is its own")
}

func TestOnlyADigestRoutinesNotifyMeIsGathered(t *testing.T) {
	t.Parallel()

	digest := &routineRun{spec: spec.Spec{Trigger: spec.Trigger{
		Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryDay, At: "08:30", Style: catalog.ScheduleDigest},
	}}}
	digest.row.ID = 1
	each := &routineRun{spec: spec.Spec{Trigger: spec.Trigger{
		Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: catalog.EveryDay, At: "08:30"},
	}}}
	each.row.ID = 2

	p := &pass{now: digestNow(), byID: map[int64]*routineRun{1: digest, 2: each}, facts: map[int64]facts.Facts{}}
	notify := spec.Step{Notify: &spec.NotifyStep{}}
	jobs := []Job{
		{MatchID: 11, RoutineID: 1, Step: notify, Item: ItemRef{ID: 1, Kind: item.KindPR}},
		{MatchID: 12, RoutineID: 1, Step: notify, Item: ItemRef{ID: 2, Kind: item.KindPR}},
		{MatchID: 13, RoutineID: 1, Step: notify, Item: ItemRef{ID: 3, Kind: item.KindPR}, Manual: true},
		{MatchID: 14, RoutineID: 1, Step: spec.Step{TellMe: &spec.TellStep{}}, Item: ItemRef{ID: 4, Kind: item.KindPR}},
		{MatchID: 21, RoutineID: 2, Step: notify, Item: ItemRef{ID: 5, Kind: item.KindPR}},
	}

	rest, digests := p.digests(jobs)

	require.Len(t, digests, 1)
	assert.Equal(t, []int64{11, 12}, digests[0].matches)
	assert.Len(t, digests[0].job.Digest.Items, 2)

	left := make([]int64, 0, len(rest))
	for _, j := range rest {
		left = append(left, j.MatchID)
	}

	assert.Equal(t, []int64{13, 14, 21}, left, "a manual run, a Tell me, and each match's routine tell on their own")
}
