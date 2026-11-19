package spec_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// inbox is a source of the user's own pull requests.
func inbox() spec.Source {
	return spec.Source{Lists: []catalog.ListSource{catalog.SourceAuthored}}
}

// onSchedule is a trigger on a schedule of every, at, on, in style.
func onSchedule(every catalog.Every, at string, on catalog.Weekday, style catalog.ScheduleStyle) spec.Trigger {
	return spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: every, At: at, On: on, Style: style}}
}

// TestValidateNamesEachTriggerScheduleAndLimitProblem breaks the flagship's trigger and
// limits one way at a time, as a hand-edited file or a newer build's might.
func TestValidateNamesEachTriggerScheduleAndLimitProblem(t *testing.T) {
	t.Parallel()

	usd := func(v float64) *float64 { return &v }
	tokens := func(v int64) *int64 { return &v }

	tests := []struct {
		name   string
		mutate func(*spec.Spec)
		want   string
	}{
		{"Labeled with no label", func(s *spec.Spec) { s.Trigger = spec.Trigger{Event: catalog.EventLabeled} }, "trigger.label"},
		{"a schedule with no time", func(s *spec.Spec) { s.Trigger = spec.Trigger{Event: catalog.EventSchedule} }, "trigger.schedule"},
		{
			"a daily schedule on a day", func(s *spec.Spec) { s.Trigger = onSchedule(catalog.EveryDay, "08:00", catalog.Monday, "") },
			"trigger.schedule.on",
		},
		{
			"a weekly schedule on no day", func(s *spec.Spec) { s.Trigger = onSchedule(catalog.EveryWeek, "08:00", "someday", "") },
			"trigger.schedule.on",
		},
		{
			"a monthly schedule", func(s *spec.Spec) { s.Trigger = onSchedule("month", "08:00", "", "") },
			"trigger.schedule.every",
		},
		{
			"a time that isn't one", func(s *spec.Spec) { s.Trigger = onSchedule(catalog.EveryWeekday, "8 am", "", "") },
			"trigger.schedule.at",
		},
		{
			"a style that isn't one", func(s *spec.Spec) { s.Trigger = onSchedule(catalog.EveryDay, "08:00", "", "weekly") },
			"trigger.schedule.style",
		},
		{
			"a digest that tells after another step",
			func(s *spec.Spec) { s.Trigger = onSchedule(catalog.EveryDay, "08:00", "", catalog.ScheduleDigest) },
			"trigger.schedule.style",
		},
		{"once per month", func(s *spec.Spec) { s.Limits.OncePer = "month" }, "limits.oncePer"},
		{
			"active hours on no day",
			func(s *spec.Spec) { s.Limits.ActiveHours = &spec.ActiveHours{From: "08:00", To: "18:00"} },
			"limits.activeHours.days",
		},
		{
			"active hours on a day that isn't one",
			func(s *spec.Spec) {
				s.Limits.ActiveHours = &spec.ActiveHours{Days: []catalog.Weekday{"funday"}, From: "08:00", To: "18:00"}
			},
			"limits.activeHours.days[0]",
		},
		{
			"active hours from a time that isn't one",
			func(s *spec.Spec) {
				s.Limits.ActiveHours = &spec.ActiveHours{Days: []catalog.Weekday{catalog.Monday}, From: "8", To: "18:00"}
			},
			"limits.activeHours.from",
		},
		{
			"active hours that end as they start",
			func(s *spec.Spec) {
				s.Limits.ActiveHours = &spec.ActiveHours{Days: []catalog.Weekday{catalog.Monday}, From: "08:00", To: "08:00"}
			},
			"limits.activeHours.to",
		},
		{"a budget in no unit", func(s *spec.Spec) { s.Limits.BudgetPerDay = &spec.Budget{} }, "limits.budgetPerDay"},
		{
			"a budget in both units",
			func(s *spec.Spec) { s.Limits.BudgetPerDay = &spec.Budget{USD: usd(5), Tokens: tokens(1000)} },
			"limits.budgetPerDay",
		},
		{"a budget past the cent", func(s *spec.Spec) { s.Limits.BudgetPerDay = &spec.Budget{USD: usd(5.005)} }, "limits.budgetPerDay.usd"},
		{"a budget of too few tokens", func(s *spec.Spec) { s.Limits.BudgetPerDay = &spec.Budget{Tokens: tokens(1)} }, "limits.budgetPerDay.tokens"},
		{"too many tools", func(s *spec.Spec) { s.Requires = make([]string, spec.MaxRequires+1) }, "requires"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := loadTemplate(t, flagship)
			tt.mutate(&s)

			assert.Contains(t, problemPaths(t, s), tt.want)
		})
	}
}

// TestValidateNamesEachWatchAndConditionProblem breaks the flagship's watch and conditions
// one way at a time.
func TestValidateNamesEachWatchAndConditionProblem(t *testing.T) {
	t.Parallel()

	many := func(n int, src spec.Source) []spec.Source {
		out := make([]spec.Source, n)
		for i := range out {
			out[i] = src
		}

		return out
	}
	clause := func(field catalog.FieldID, op catalog.Op, value string) spec.Clause {
		return spec.Clause{Field: field, Op: op, Value: value}
	}

	tests := []struct {
		name   string
		mutate func(*spec.Spec)
		want   string
	}{
		{"too many sources", func(s *spec.Spec) { s.Watch.Sources = many(spec.MaxSources+1, inbox()) }, "watch.sources"},
		{
			"too many searches",
			func(s *spec.Spec) { s.Watch.Sources = many(spec.MaxSearches+1, s.Watch.Sources[0]) },
			"watch.sources",
		},
		{
			"a source that is both",
			func(s *spec.Spec) { s.Watch.Sources[0].Lists = []catalog.ListSource{catalog.SourceAuthored} },
			"watch.sources[0]",
		},
		{
			"an inbox with no reason",
			func(s *spec.Spec) { s.Watch.Sources = []spec.Source{{Lists: []catalog.ListSource{}}} },
			"watch.sources[0].lists",
		},
		{
			"an inbox reason that isn't one",
			func(s *spec.Spec) { s.Watch.Sources = []spec.Source{{Lists: []catalog.ListSource{"starred"}}} },
			"watch.sources[0].lists[0]",
		},
		{
			"too many conditions",
			func(s *spec.Spec) {
				s.Conditions = spec.Conditions{Match: spec.MatchAll}
				for range spec.MaxConditions + 1 {
					s.Conditions.Of = append(s.Conditions.Of, clause(catalog.FieldChecksFailed, catalog.OpGt, "0"))
				}
			},
			"conditions.of",
		},
		{"a match of some", func(s *spec.Spec) { s.Conditions.Match = "some" }, "conditions.match"},
		{
			"an empty group",
			func(s *spec.Spec) {
				s.Conditions.Of = append(s.Conditions.Of, spec.Clause{Group: &spec.Conditions{Match: spec.MatchAny}})
			},
			"conditions.of[1].group.of",
		},
		{
			"a field and a group",
			func(s *spec.Spec) {
				c := clause(catalog.FieldChecksFailed, catalog.OpGt, "0")
				c.Group = &spec.Conditions{Match: spec.MatchAny, Of: []spec.Clause{c}}
				s.Conditions.Of = []spec.Clause{c}
			},
			"conditions.of[0]",
		},
		{
			"a group in a group",
			func(s *spec.Spec) {
				inner := spec.Clause{Group: &spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
					clause(catalog.FieldChecksFailed, catalog.OpGt, "0"),
				}}}
				s.Conditions.Of = []spec.Clause{{Group: &spec.Conditions{Match: spec.MatchAny, Of: []spec.Clause{inner}}}}
			},
			"conditions.of[0].group.of[0].group",
		},
		{
			"a field that isn't one",
			func(s *spec.Spec) { s.Conditions.Of = []spec.Clause{clause("item.mood", catalog.OpIs, "calm")} },
			"conditions.of[0].field",
		},
		{
			"one value in a list",
			func(s *spec.Spec) {
				c := clause(catalog.FieldChecksFailed, catalog.OpGt, "0")
				c.Values = []string{"1"}
				s.Conditions.Of = []spec.Clause{c}
			},
			"conditions.of[0].values",
		},
		{
			"a value too long",
			func(s *spec.Spec) {
				s.Conditions.Of = []spec.Clause{clause(catalog.FieldTitle, catalog.OpContains, strings.Repeat("x", spec.MaxTextRunes+1))}
			},
			"conditions.of[0].value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := loadTemplate(t, flagship)
			tt.mutate(&s)

			assert.Contains(t, problemPaths(t, s), tt.want)
		})
	}
}
