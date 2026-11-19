package eval_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// unread is a pull request whose head is known and whose details aren't read yet.
func unread() facts.Facts {
	return facts.Facts{
		Item: facts.Item{ID: 1, Kind: item.KindPR, Repo: "acme-org/example-repo", Number: 1},
		Head: facts.Text("abc"), Values: map[catalog.FieldID]facts.Value{},
	}
}

// TestATriggerOnDetailsNotReadYetIsUnknown checks that each trigger that reads an item's
// details waits for them, saying so, rather than deciding no.
func TestATriggerOnDetailsNotReadYetIsUnknown(t *testing.T) {
	t.Parallel()

	for _, event := range []catalog.Event{
		catalog.EventChecksPassed, catalog.EventChangesRequested, catalog.EventApproved, catalog.EventLabeled,
		catalog.EventReadyForReview, catalog.EventReviewRequested, catalog.EventNewComments,
	} {
		d := eval.Evaluate(spec.Spec{Trigger: spec.Trigger{Event: event, Label: "bug"}}, unread(), eval.History{}, now)

		assert.Equal(t, eval.Unknown, d.Match, event)
		require.NotEmpty(t, d.Trace, event)
		assert.NotEmpty(t, d.Trace[0].Actual, "%s says what it waits for", event)
	}
}

func TestChecksPassedWaitsForEveryCheckToFinish(t *testing.T) {
	t.Parallel()

	f := unread()
	f.Values[catalog.FieldChecksFailed] = facts.Number(0)
	f.Values[catalog.FieldChecksPending] = facts.Number(2)
	f.Values[catalog.FieldChecksPassed] = facts.Number(3)
	f.Values[catalog.FieldAwaitingApproval] = facts.Bool(false)

	d := eval.Evaluate(spec.Spec{Trigger: spec.Trigger{Event: catalog.EventChecksPassed}}, f, eval.History{}, now)
	assert.Equal(t, eval.No, d.Match)
	assert.Equal(t, "2 checks are still running", d.Trace[0].Actual)
	assert.Contains(t, d.Trace[0].Condition, "Checks pass")
}

func TestAScheduleThatCantBeReadIsNeverDue(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC)

	for _, c := range []struct {
		name     string
		schedule *spec.Schedule
		want     string
	}{
		{"no schedule", nil, "it has no schedule"},
		{"a time with no colon", &spec.Schedule{Every: catalog.EveryDay, At: "0800"}, "it isn't due today"},
		{"a cadence this build doesn't know", &spec.Schedule{Every: "fortnight", At: "08:00"}, "it isn't due today"},
	} {
		d := eval.Evaluate(spec.Spec{Trigger: spec.Trigger{Event: catalog.EventSchedule, Schedule: c.schedule}}, unread(),
			eval.History{}, monday)

		assert.Equal(t, eval.No, d.Match, c.name)
		assert.Equal(t, c.want, d.Trace[0].Actual, c.name)
	}
}

func TestACommitShorterThanAnAbbreviationIsWrittenWhole(t *testing.T) {
	t.Parallel()

	f := unread()
	f.Head = facts.Text("abc12")

	d := eval.Evaluate(spec.Spec{Trigger: spec.Trigger{Event: catalog.EventNewCommits, FirstToo: new(true)}}, f,
		eval.History{}, now)
	assert.Equal(t, eval.Yes, d.Match)
	assert.Equal(t, "its head is abc12", d.Trace[0].Actual)
}
