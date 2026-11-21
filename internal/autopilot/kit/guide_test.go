package kit_test

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// TestGuideGolden pins the guide, as autopilot_guide answers it with this build's
// vocabulary. Rewrite it with go test ./internal/autopilot/kit -update.
func TestGuideGolden(t *testing.T) {
	t.Parallel()

	wordsGolden(t, "guide.golden", kit.Guide(wordsVocabulary()))
}

// TestTopicGoldens pins every topic, as autopilot_reference answers it with this build's
// vocabulary.
func TestTopicGoldens(t *testing.T) {
	t.Parallel()

	v := wordsVocabulary()

	for _, name := range kit.Topics() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			text, ok := kit.Topic(name, v)
			require.True(t, ok)
			wordsGolden(t, "topics/"+name+".golden", text)
		})
	}
}

// TestEveryKeyOfAPartHasWords holds the topics that walk a part of the format key by key
// to the format: every key of the part's structs has a line of its own in its topic, with
// words, so a key the format adds fails here until kit says what it takes.
func TestEveryKeyOfAPartHasWords(t *testing.T) {
	t.Parallel()

	v := wordsVocabulary()

	for topic, types := range map[string][]reflect.Type{
		"watch":    {reflect.TypeFor[watch.Search]()},
		"agent":    {reflect.TypeFor[spec.AgentStep](), reflect.TypeFor[spec.Fence]()},
		"limits":   {reflect.TypeFor[spec.Limits]()},
		"schedule": {reflect.TypeFor[spec.Schedule]()},
		"prompts":  {reflect.TypeFor[kit.NewPrompt]()},
		"settings": {reflect.TypeFor[autopilotprefs.Config]()},
		"steps": {
			reflect.TypeFor[spec.NotifyStep](), reflect.TypeFor[spec.SnoozeStep](), reflect.TypeFor[spec.WaitStep](),
			reflect.TypeFor[spec.MergeStep](), reflect.TypeFor[spec.ReviewStep](), reflect.TypeFor[spec.TellStep](),
			reflect.TypeFor[spec.ScriptStep](),
		},
	} {
		text, ok := kit.Topic(topic, v)
		require.True(t, ok, topic)

		for _, typ := range types {
			for _, key := range wordsJSONKeys(typ) {
				assert.Regexp(t, "(?m)^ *- `"+regexp.QuoteMeta(key)+"`[^\n]*[:,] [^\n]*[a-z][^\n]*$", text,
					"topic %s has no words for %s.%s", topic, typ.Name(), key)
			}
		}

		assert.NotRegexp(t, "(?m)`(, needed)?: \\.?$", text, "topic %s has a key without words", topic)
	}
}

// TestTopicRefusesAnUnknownName holds Topic to its topics: another name is none.
func TestTopicRefusesAnUnknownName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", "Steps", "step", "../schema", "schema "} {
		text, ok := kit.Topic(name, wordsVocabulary())
		assert.False(t, ok, name)
		assert.Empty(t, text, name)
	}
}

// TestTheGuideListsEveryTopic holds the guide to the topics: each is listed, by name.
func TestTheGuideListsEveryTopic(t *testing.T) {
	t.Parallel()

	guide := kit.Guide(wordsVocabulary())

	require.Len(t, kit.Topics(), 16)

	for _, name := range kit.Topics() {
		assert.Contains(t, guide, "- `"+name+"`: ", name)
	}
}

// wordsAll is the guide and every topic, as one text.
func wordsAll(t *testing.T, v kit.Vocabulary) string {
	t.Helper()

	all := make([]string, 0, 1+len(kit.Topics()))
	all = append(all, kit.Guide(v))

	for _, name := range kit.Topics() {
		text, ok := kit.Topic(name, v)
		require.True(t, ok, name)

		all = append(all, text)
	}

	return strings.Join(all, "\n")
}

// TestTheGuideNamesEveryCatalogEntry is the coverage test: every name a routine or a
// proposal can hold is in the guide or one of its topics, in backticks, as the spec spells
// it, so a value the catalog, the spec, or the vocabulary adds fails here until kit has
// words for it.
func TestTheGuideNamesEveryCatalogEntry(t *testing.T) {
	t.Parallel()

	v := wordsVocabulary()
	all := wordsAll(t, v)

	for _, name := range wordsCatalogNames(v) {
		assert.Contains(t, all, "`"+name+"`", "the guide and its topics never name %q", name)
	}
}

// wordsCatalogNames are every name a routine or a proposal can hold: the catalog's
// entries, every closed set's values, the settings' and the proposal's keys, and the
// vocabulary's templates and starters.
func wordsCatalogNames(v kit.Vocabulary) []string {
	names := wordsCatalogEntries()

	for _, set := range spec.ClosedSets() {
		names = append(names, set.Values...)
	}

	for _, typ := range []reflect.Type{
		reflect.TypeFor[autopilotprefs.Config](), reflect.TypeFor[autopilotprefs.DailyCap](),
		reflect.TypeFor[kit.SettingsChange](), reflect.TypeFor[kit.RulesChange](), reflect.TypeFor[kit.Rules](),
		reflect.TypeFor[kit.Proposal](), reflect.TypeFor[kit.RoutineChange](), reflect.TypeFor[kit.NewPrompt](),
	} {
		names = append(names, wordsJSONKeys(typ)...)
	}

	for _, values := range wordsSets() {
		names = append(names, values...)
	}

	names = append(names, wordsNamesOf(v.Templates, func(t kit.Template) string { return t.Key })...)

	return append(names, wordsNamesOf(v.Starters, func(s kit.Starter) string { return s.Key })...)
}

// wordsCatalogEntries are the IDs of the catalog's entries: every trigger and its options,
// field, operator, step, GitHub action, tools preset, place, Only if outcome, inbox list,
// and schedule style.
func wordsCatalogEntries() []string {
	names := make([]string, 0, 3*len(catalog.Triggers()))

	for _, tr := range catalog.Triggers() {
		names = append(names, string(tr.Event))
		names = append(names, wordsNamesOf(tr.Options, func(o catalog.OptionInfo) string { return string(o.ID) })...)
	}

	return slices.Concat(names,
		wordsNamesOf(catalog.Fields(), func(f catalog.Field) string { return string(f.ID) }),
		wordsNamesOf(catalog.Ops(), func(o catalog.OpInfo) string { return string(o.ID) }),
		wordsNamesOf(catalog.Steps(), func(s catalog.Step) string { return string(s.Kind) }),
		wordsNamesOf(catalog.GitHubActions(), func(a catalog.GitHubAction) string { return string(a.Kind) }),
		wordsNamesOf(catalog.Presets(), func(p catalog.PresetInfo) string { return string(p.ID) }),
		wordsNamesOf(catalog.Places(), func(p catalog.PlaceInfo) string { return string(p.ID) }),
		wordsNamesOf(catalog.Outcomes(), func(o catalog.OutcomeInfo) string { return string(o.ID) }),
		wordsNamesOf(catalog.ListSources(), func(l catalog.ListSourceInfo) string { return string(l.ID) }),
		wordsNamesOf(catalog.ScheduleStyles(), func(s catalog.ScheduleStyleInfo) string { return string(s.ID) }),
	)
}

// wordsNamesOf returns the name name gives each entry of list.
func wordsNamesOf[E any](list []E, name func(E) string) []string {
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, name(e))
	}

	return out
}

// wordsJSONKeys are a struct's JSON keys.
func wordsJSONKeys(t reflect.Type) []string {
	var out []string

	for field := range t.Fields() {
		if name, _, _ := strings.Cut(field.Tag.Get("json"), ","); name != "" && name != "-" {
			out = append(out, name)
		}
	}

	return out
}

// TestTheGuideNamesEveryFieldOfARoutine holds the routine topic to the format: every key
// of every struct a spec holds is in it, as its shape writes it.
func TestTheGuideNamesEveryFieldOfARoutine(t *testing.T) {
	t.Parallel()

	text, ok := kit.Topic("routine", wordsVocabulary())
	require.True(t, ok)

	for _, typ := range wordsStructs(reflect.TypeFor[spec.Spec]()) {
		assert.Contains(t, text, typ.Name()+" {", typ.Name())

		for _, key := range wordsJSONKeys(typ) {
			assert.Regexp(t, `(?m)^  `+regexp.QuoteMeta(key)+`\??: `, text, typ.Name()+"."+key)
		}
	}
}
