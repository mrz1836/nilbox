package spec_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
)

// closedSets returns spec.ClosedSets by name.
func closedSets() map[string]spec.ClosedSet {
	out := map[string]spec.ClosedSet{}
	for _, set := range spec.ClosedSets() {
		out[set.Name] = set
	}

	return out
}

// namedField is a field of the format that holds a named string type.
type namedField struct {
	// path is the struct and the key: "spec.MergeStep.method".
	path string

	// held is the named string type it holds: catalog.MergeMethod.
	held reflect.Type
}

// namedStringFields walks the format as structKeys does and returns every field whose
// type is a named string type, alone, in a list, or behind a pointer.
func namedStringFields() []namedField {
	var out []namedField

	for _, typ := range formatTypes() {
		for f := range typ.Fields() {
			if held := namedString(f.Type); held != nil {
				out = append(out, namedField{path: typ.String() + "." + jsonName(f), held: held})
			}
		}
	}

	return out
}

// namedString returns the named string type t holds, through pointers and slices, or nil.
func namedString(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	if t.Kind() == reflect.String && t.PkgPath() != "" {
		return t
	}

	return nil
}

// TestEveryNamedStringTypeHasAClosedSet holds every field of the format whose type is a
// named string type to a closed set of that type, so no vocabulary enters the format
// without its values, and every set but the two of keys (TestTheStructuralSetsAreTheFormatsKeys)
// to a field that holds it, so none outlives its use. A plain string (a label, a login, a
// prompt's nickname, an agent's ID, a model's name) is free text and needs none.
func TestEveryNamedStringTypeHasAClosedSet(t *testing.T) {
	t.Parallel()

	fields := namedStringFields()
	require.NotEmpty(t, fields, "the walk reaches the format's named types")

	sets := closedSets()
	covered, held := map[reflect.Type]bool{}, map[reflect.Type]bool{}

	for _, set := range sets {
		covered[set.Type] = true
	}

	for _, f := range fields {
		held[f.held] = true

		assert.True(t, covered[f.held], "%s holds %s, which no closed set covers", f.path, f.held)
	}

	for name, set := range sets {
		if name != "step kinds" && name != "github actions" {
			assert.True(t, held[set.Type], "a field of the format holds %s's %s", name, set.Type)
		}
	}
}

// TestTheClosedSetsAreWhole checks each set: its name and its type given once, its type a
// named string type, and its values some, each once, and never "", which Blank stands for.
func TestTheClosedSetsAreWhole(t *testing.T) {
	t.Parallel()

	names := map[string]bool{}
	types := map[reflect.Type]bool{}

	for _, set := range spec.ClosedSets() {
		require.NotNil(t, set.Type, set.Name)
		assert.False(t, names[set.Name], "%s is one set", set.Name)
		assert.False(t, types[set.Type], "%s holds one set", set.Type)

		names[set.Name], types[set.Type] = true, true

		assert.Equal(t, reflect.String, set.Type.Kind(), "%s holds strings", set.Name)
		assert.NotEmpty(t, set.Type.PkgPath(), "%s is held by a named type", set.Name)
		assert.NotEmpty(t, set.Values, "%s has values", set.Name)
		assert.NotContains(t, set.Values, "", "%s says a field may be left out with Blank", set.Name)

		seen := map[string]bool{}

		for _, v := range set.Values {
			assert.False(t, seen[v], "%s lists %q once", set.Name, v)

			seen[v] = true
		}
	}
}

// TestEnumsWriteEveryClosedSet checks that enums, which the schema golden and
// TestFormatHasNoForbiddenWords read, writes every value of every closed set and nothing
// more, so the golden pins each set and the guard reads every value.
func TestEnumsWriteEveryClosedSet(t *testing.T) {
	t.Parallel()

	bare := func(s string) string {
		before, _, _ := strings.Cut(s, " (")

		return before
	}

	got := map[string][]string{}
	for name, values := range enums() {
		got[bare(name)] = each(values, bare)
	}

	want := map[string][]string{}
	for _, set := range spec.ClosedSets() {
		want[set.Name] = set.Values
	}

	assert.Equal(t, want, got)
}

// TestTheStructuralSetsAreTheFormatsKeys holds the two sets of keys rather than values to
// the format: the step kinds are a step's keys but onlyIf, and the GitHub actions a GitHub
// step's action's.
func TestTheStructuralSetsAreTheFormatsKeys(t *testing.T) {
	t.Parallel()

	keys := func(typ reflect.Type) []string {
		out := make([]string, 0, typ.NumField())
		for field := range typ.Fields() {
			out = append(out, jsonName(field))
		}

		return out
	}

	sets := closedSets()
	steps := slices.DeleteFunc(keys(reflect.TypeFor[spec.Step]()), func(k string) bool { return k == "onlyIf" })

	assert.ElementsMatch(t, steps, sets["step kinds"].Values)
	assert.ElementsMatch(t, keys(reflect.TypeFor[spec.GitHubAction]()), sets["github actions"].Values)
}

// stranger is a value no closed set holds.
const stranger = "bogus"

// placement puts a value where the format holds one of a set's values, in a spec Validate
// otherwise takes there.
type placement struct {
	// set names the closed set.
	set string

	// path is where Validate says a problem with the value is.
	path string

	// place returns a spec holding value at path.
	place func(t *testing.T, value string) spec.Spec

	// refusal is the problem the stranger gets, "" for one the watch words.
	refusal string
}

// placements put each set's values where the format holds them; the sets Validate checks
// with the values around them (a condition's field and operator, a step's Only if, and
// the keys of a step and of an action) are tested on their own.
func placements() []placement {
	return slices.Concat(watchPlacements(), triggerPlacements(), stepPlacements(), []placement{
		{
			set: "conditions.match", path: "conditions.match", refusal: `match "all" or "any" of the conditions`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				s := loadTemplate(t, flagship)
				s.Conditions.Match = spec.Match(v)

				return s
			},
		},
		{
			set: "limits.oncePer", path: "limits.oncePer", refusal: `pick "item", "headCommit", or "day"`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				s := loadTemplate(t, flagship)
				s.Limits.OncePer = catalog.OncePer(v)

				return s
			},
		},
	})
}

// merged returns the flagship's fix, followed by a Wait for the checks on its push and a
// Merge it, how and with method, once they pass.
func merged(t *testing.T, how catalog.MergeHow, method catalog.MergeMethod) spec.Spec {
	t.Helper()

	wait := spec.Step{WaitForChecks: &spec.WaitStep{}, OnlyIf: catalog.OutcomePushed}
	merge := spec.Step{Merge: &spec.MergeStep{How: how, Method: method}, OnlyIf: catalog.OutcomePassedChecks}

	return withSteps(t, agentStep(catalog.PresetEditTestPush), wait, merge)
}

// stepPlacements put the steps' sets' values in the steps that hold them.
func stepPlacements() []placement {
	return []placement{
		{
			set: "agent.where", path: "steps[0].agent.where", refusal: `run it "background" or "terminal"`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				return withSteps(t, spec.Step{Agent: &spec.AgentStep{
					Prompt: spec.PromptRef{Starter: "fix-checks"}, Where: catalog.Where(v),
				}})
			},
		},
		{
			set: "fence.tools", path: "steps[0].agent.fence.tools", refusal: `"bogus" isn't a tools preset`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				return withSteps(t, agentStep(catalog.Preset(v)))
			},
		},
		{
			set: "merge.how", path: "steps[2].merge.how", refusal: `merge "autoMergeIfItWasOn" or "whenMergeable"`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				return merged(t, catalog.MergeHow(v), catalog.MergeDefault)
			},
		},
		{
			set: "merge.method", path: "steps[2].merge.method",
			refusal: "pick a merge method: merge, squash, or rebase, or leave it out for the repository's own",
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				return merged(t, catalog.MergeWhenMergeable, catalog.MergeMethod(v))
			},
		},
		{
			set: "postReview.as", path: "steps[1].postReview.as", refusal: `post it as "comment" or "verdict"`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				return withSteps(t, agentStep(catalog.PresetReadOnly), spec.Step{
					PostReview: &spec.ReviewStep{As: catalog.ReviewAs(v)},
				})
			},
		},
		{
			set: "tellMe.when", path: "steps[0].tellMe.when", refusal: `say "reached" or "verdict"`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				return withSteps(t, spec.Step{TellMe: &spec.TellStep{When: catalog.TellWhen(v)}})
			},
		},
	}
}

// watchPlacements put the watch's sets' values in the flagship's watch.
func watchPlacements() []placement {
	search := func(change func(*watch.Search, string)) func(*testing.T, string) spec.Spec {
		return func(t *testing.T, v string) spec.Spec {
			t.Helper()

			s := loadTemplate(t, flagship)
			change(s.Watch.Sources[0].Search, v)

			return s
		}
	}

	return []placement{
		{
			set: "source.lists", path: "watch.sources[0].lists[0]", refusal: `"bogus" isn't a reason an item is in your inbox`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				s := loadTemplate(t, flagship)
				s.Watch.Sources = []spec.Source{{Lists: []catalog.ListSource{catalog.ListSource(v)}}}

				return s
			},
		},
		{
			set: "search.kind", path: "watch.sources[0].search.kind",
			place: search(func(s *watch.Search, v string) { s.Kind = item.Kind(v) }),
		},
		{
			set: "search.involving", path: "watch.sources[0].search.involving[0]",
			place: search(func(s *watch.Search, v string) { s.Involving = []watch.Involving{watch.Involving(v)} }),
		},
		{
			set: "search.state", path: "watch.sources[0].search.state[0]",
			place: search(func(s *watch.Search, v string) { s.State = []watch.State{watch.State(v)} }),
		},
	}
}

// triggerPlacements put the trigger's sets' values in a trigger, a schedule, and the
// active hours.
func triggerPlacements() []placement {
	schedule := func(change func(*spec.Schedule, string)) func(*testing.T, string) spec.Spec {
		return func(t *testing.T, v string) spec.Spec {
			t.Helper()

			s := loadTemplate(t, "weekday-nudge")
			change(s.Trigger.Schedule, v)

			return s
		}
	}

	return []placement{
		{
			set: "trigger.event", path: "trigger.event", refusal: `"bogus" isn't a trigger`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				s := withSteps(t, spec.Step{Notify: &spec.NotifyStep{}})
				s.Watch = spec.Watch{Sources: []spec.Source{inbox()}}
				s.Trigger = spec.Trigger{Event: catalog.Event(v)}
				s.Conditions = spec.Conditions{Match: spec.MatchAll}

				return s
			},
		},
		{
			set: "schedule.every", path: "trigger.schedule.every", refusal: `pick "day", "weekday", or "week"`,
			place: schedule(func(s *spec.Schedule, v string) {
				s.Every, s.On = catalog.Every(v), ""
				if s.Every == catalog.EveryWeek {
					s.On = catalog.Monday
				}
			}),
		},
		{
			set: "schedule.on, activeHours.days", path: "trigger.schedule.on", refusal: "pick the day of the week it fires",
			place: schedule(func(s *spec.Schedule, v string) { s.Every, s.On = catalog.EveryWeek, catalog.Weekday(v) }),
		},
		{
			set: "schedule.on, activeHours.days", path: "limits.activeHours.days[0]", refusal: `"bogus" isn't a day of the week`,
			place: func(t *testing.T, v string) spec.Spec {
				t.Helper()

				s := loadTemplate(t, flagship)
				s.Limits.ActiveHours = &spec.ActiveHours{Days: []catalog.Weekday{catalog.Weekday(v)}, From: "08:00", To: "18:00"}

				return s
			},
		},
		{
			set: "schedule.style", path: "trigger.schedule.style", refusal: `pick "each" or "digest"`,
			place: schedule(func(s *spec.Schedule, v string) { s.Style = catalog.ScheduleStyle(v) }),
		},
	}
}

// problemsAt validates s and returns the words of each problem at path.
func problemsAt(t *testing.T, s spec.Spec, path string) []string {
	t.Helper()

	err := spec.Validate(s)
	if err == nil {
		return nil
	}

	var verr *spec.ValidationError

	require.ErrorAs(t, err, &verr)

	var out []string

	for _, p := range verr.Problems {
		if p.Path() == path {
			out = append(out, p.Message)
		}
	}

	return out
}

// TestValidateTakesEachSetsValues places every value of a closed set where the format
// holds it, "" (a value left out) where the set lets it be, and a stranger, and checks that
// Validate takes exactly the set: each value, "" only where Blank says so, and never the
// stranger, refused in the words that list the set.
func TestValidateTakesEachSetsValues(t *testing.T) {
	t.Parallel()

	sets := closedSets()
	placed := map[string]bool{}

	for _, p := range placements() {
		placed[p.set] = true

		set, ok := sets[p.set]
		require.True(t, ok, p.set)

		t.Run(p.path, func(t *testing.T) {
			t.Parallel()

			for _, v := range set.Values {
				assert.Empty(t, problemsAt(t, p.place(t, v), p.path), "%s takes %q", p.set, v)
			}

			blank := problemsAt(t, p.place(t, ""), p.path)
			assert.Equal(t, set.Blank, len(blank) == 0, "%s takes a value left out only when Blank says so", p.set)

			refused := problemsAt(t, p.place(t, stranger), p.path)
			require.Len(t, refused, 1, "%s refuses %q", p.set, stranger)

			if p.refusal != "" {
				assert.Equal(t, p.refusal, refused[0])
			}
		})
	}

	others := []string{"condition.field", "condition.op", "step.onlyIf", "step kinds", "github actions"}
	for name := range sets {
		assert.True(t, placed[name] || slices.Contains(others, name), "%s is placed, or tested on its own", name)
	}
}
