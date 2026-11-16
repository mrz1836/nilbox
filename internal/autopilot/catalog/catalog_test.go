package catalog_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// forbidden are the actions Autopilot never takes, in the review and issue sheets' own
// words: no entry of the catalog, nor any string the autopilot packages spell, may be one.
func forbidden() []string {
	return []string{
		"merge_bypass", "approve_merge", "approve_auto_merge", "close", "close_completed", "close_not_planned",
		"approve_runs",
	}
}

// squash writes a name without case or separators, so "approveRuns", "approve_runs",
// and "approve-runs" compare as one.
func squash(s string) string {
	return strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(strings.ToLower(s))
}

// ids returns every ID the catalog lists.
func ids() []string {
	c := catalog.All()
	out := []string{}

	for _, t := range c.Triggers {
		out = append(out, string(t.Event))
		out = append(out, each(t.Options, func(o catalog.OptionInfo) string { return string(o.ID) })...)
	}

	for _, f := range c.Fields {
		out = append(out, string(f.ID))
		out = append(out, choiceValues(f.Choices)...)
	}

	for _, s := range slices.Concat(c.AgentSettings, c.Limits, c.StepSettings) {
		out = append(out, s.ID)
		out = append(out, choiceValues(s.Choices)...)
	}

	out = append(out, each(c.Ops, func(o catalog.OpInfo) string { return string(o.ID) })...)
	out = append(out, each(c.Steps, func(s catalog.Step) string { return string(s.Kind) })...)
	out = append(out, each(c.Steps, func(s catalog.Step) string { return string(s.Dispatcher) })...)
	out = append(out, each(c.GitHubActions, func(a catalog.GitHubAction) string { return string(a.Kind) })...)
	out = append(out, each(c.Outcomes, func(o catalog.OutcomeInfo) string { return string(o.ID) })...)

	return append(out, each(c.Presets, func(p catalog.PresetInfo) string { return string(p.ID) })...)
}

// each returns what id says of every element of list.
func each[T any](list []T, id func(T) string) []string {
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, id(e))
	}

	return out
}

// choiceValues returns the values of choices.
func choiceValues(choices []catalog.Choice) []string {
	return each(choices, func(c catalog.Choice) string { return c.Value })
}

// TestCatalogHasNoForbiddenAction is the guard: the catalog has no entry for a close,
// an approval of workflow runs, or a merge past GitHub's rules, under any spelling.
func TestCatalogHasNoForbiddenAction(t *testing.T) {
	t.Parallel()

	for _, id := range ids() {
		for _, word := range forbidden() {
			assert.NotContains(t, squash(id), squash(word), "the catalog lists %q", id)
		}
	}

	for _, kind := range []catalog.GitHubActionKind{
		catalog.GitHubRebase, catalog.GitHubUpdateBranch, catalog.GitHubReply, catalog.GitHubLabels,
	} {
		assert.NotContains(t, []string{"merge", "approve", "close", "autoMerge"}, string(kind))
	}
}

// TestNoAutopilotCodeSpellsAForbiddenAction is the guard over the code: no string the
// autopilot packages spell outside their tests is one of the forbidden actions.
func TestNoAutopilotCodeSpellsAForbiddenAction(t *testing.T) {
	t.Parallel()

	root := ".."
	words := forbidden()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}

		for _, lit := range stringLiterals(t, path) {
			for _, word := range words {
				assert.NotEqual(t, squash(word), squash(lit), "%s spells %q", path, lit)
			}
		}

		return nil
	})
	require.NoError(t, err)
}

// stringLiterals returns every string literal in a Go file.
func stringLiterals(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	var out []string

	ast.Inspect(file, func(n ast.Node) bool {
		if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if s, err := strconv.Unquote(lit.Value); err == nil {
				out = append(out, s)
			}
		}

		return true
	})

	return out
}

// TestEveryStepNamesItsDispatcher checks that each step and each GitHub action names a
// dispatcher the catalog lists, so a later build adds the code that runs it, and that the
// GitHub actions go through the GitHub dispatcher alone.
func TestEveryStepNamesItsDispatcher(t *testing.T) {
	t.Parallel()

	for _, s := range catalog.Steps() {
		assert.Contains(t, catalog.Dispatchers(), s.Dispatcher, s.Kind)
		assert.NotEmpty(t, s.Label, s.Kind)
		assert.NotEmpty(t, s.Kinds, s.Kind)
		assert.False(t, s.Local && s.AsksFirst, "%s runs at once or asks first, not both", s.Kind)
	}

	for _, a := range catalog.GitHubActions() {
		assert.Equal(t, catalog.DispatchGitHub, a.Dispatcher, a.Kind)
	}

	used := map[catalog.Dispatcher]bool{}
	for _, s := range catalog.Steps() {
		used[s.Dispatcher] = true
	}

	for _, d := range catalog.Dispatchers() {
		assert.True(t, used[d], "%s runs a step", d)
	}
}

// builtDispatchers are the parts whose dispatchers this build has: the inbox's triage, and
// the notices (Notify me and Tell me).
func builtDispatchers() []catalog.Dispatcher {
	return []catalog.Dispatcher{catalog.DispatchTriage, catalog.DispatchNotices}
}

func TestAStepIsOfferedOnceItsDispatcherIsBuilt(t *testing.T) {
	t.Parallel()

	for _, s := range catalog.Steps() {
		built := slices.Contains(builtDispatchers(), s.Dispatcher)
		assert.Equal(t, built, s.Offered, "%s: offered once its dispatcher, %s, is built", s.Kind, s.Dispatcher)
	}

	assert.Empty(t, catalog.GitHubActions(), "the GitHub actions are added with their dispatcher")
}

func TestTheLocalStepsAreTheInboxsOwn(t *testing.T) {
	t.Parallel()

	var local, asks []catalog.StepKind

	for _, s := range catalog.Steps() {
		if s.Local {
			local = append(local, s.Kind)
		}

		if s.AsksFirst {
			asks = append(asks, s.Kind)
		}
	}

	assert.Equal(t, []catalog.StepKind{
		catalog.StepNotify, catalog.StepPin, catalog.StepSnooze, catalog.StepDismiss, catalog.StepRestore,
		catalog.StepMarkDone, catalog.StepTellMe,
	}, local)
	assert.Equal(t, []catalog.StepKind{catalog.StepGitHub, catalog.StepAgent, catalog.StepMerge, catalog.StepPostReview}, asks)
}

func TestFields(t *testing.T) {
	t.Parallel()

	seen := map[catalog.FieldID]bool{}
	groups := map[catalog.Group]bool{}

	for _, g := range catalog.Groups() {
		groups[g.ID] = true
	}

	for _, f := range catalog.Fields() {
		assert.False(t, seen[f.ID], "%s is listed once", f.ID)
		seen[f.ID] = true

		prefix, _, ok := strings.Cut(string(f.ID), ".")
		assert.True(t, ok && prefix == string(f.Group), "%s is named for its group", f.ID)
		assert.True(t, groups[f.Group], f.ID)
		assert.Contains(t, catalog.Types(), f.Type, f.ID)
		assert.NotEmpty(t, f.Label, f.ID)
		assert.NotEmpty(t, f.Kinds, f.ID)
		assert.NotEmpty(t, catalog.OpsFor(f), "%s takes an operator", f.ID)

		checkWords(t, f)

		got, ok := catalog.FieldByID(f.ID)
		assert.True(t, ok)
		assert.Equal(t, f, got)
	}

	_, ok := catalog.FieldByID("checks.flaky")
	assert.False(t, ok)
}

// checkWords checks that a field has the words its type's sentences use.
func checkWords(t *testing.T, f catalog.Field) {
	t.Helper()

	has, ok := wordsFor()[f.Type]
	require.True(t, ok, f.ID)
	assert.True(t, has(f), "%s has the words its type's sentences use", f.ID)
}

// wordsFor are what each type of field needs to be said.
func wordsFor() map[catalog.Type]func(catalog.Field) bool {
	noun := func(f catalog.Field) bool { return f.Words.Noun != "" && len(f.Choices) == 0 }

	return map[catalog.Type]func(catalog.Field) bool{
		catalog.TypeBool:     func(f catalog.Field) bool { return f.Words.Yes != "" && f.Words.No != "" },
		catalog.TypeList:     listWords,
		catalog.TypeChoice:   func(f catalog.Field) bool { return len(f.Choices) >= 2 && f.Words.Noun != "" },
		catalog.TypeText:     noun,
		catalog.TypeNumber:   noun,
		catalog.TypeDuration: noun,
	}
}

// listWords reports a list field with its noun, one element's, the words for empty and
// not, and its choices or how its patterns match.
func listWords(f catalog.Field) bool {
	w := f.Words
	said := w.Plural && w.Noun != "" && w.One != "" && w.Yes != "" && w.No != ""

	return said && (len(f.Choices) > 0 || f.Patterns != "")
}

func TestOps(t *testing.T) {
	t.Parallel()

	seen := map[catalog.Op]bool{}
	types := map[catalog.Type]bool{}

	for _, o := range catalog.Ops() {
		assert.False(t, seen[o.ID], "%s is listed once", o.ID)
		seen[o.ID] = true
		types[o.Type] = true

		assert.Contains(t, []catalog.Arity{catalog.ArityNone, catalog.ArityOne, catalog.ArityMany}, o.Arity)

		if o.Negation == "" {
			continue
		}

		neg, ok := catalog.OpByID(o.Negation)
		require.True(t, ok, o.ID)
		assert.Equal(t, o.ID, neg.Negation, "%s and %s negate each other", o.ID, neg.ID)
		assert.Equal(t, o.Type, neg.Type)
		assert.Equal(t, o.Arity, neg.Arity)
	}

	for _, typ := range catalog.Types() {
		assert.True(t, types[typ], "%s has operators", typ)
	}

	assert.True(t, seen[catalog.OpAnyContains], "the flagship's operator")
}

func TestAllOperatorsSayAnEmptyListDoesntMatch(t *testing.T) {
	t.Parallel()

	for _, id := range []catalog.Op{catalog.OpAllMatch, catalog.OpHasAll} {
		o, ok := catalog.OpByID(id)
		require.True(t, ok)
		assert.Contains(t, o.Help, "empty list", "%s says what it does with an empty list", id)
	}
}

func TestChoiceListsTakeOnlySetOperators(t *testing.T) {
	t.Parallel()

	lists, _ := catalog.FieldByID(catalog.FieldLists)
	assert.Equal(t, []catalog.Op{
		catalog.OpHasAny, catalog.OpHasAll, catalog.OpHasNone, catalog.OpIsEmpty, catalog.OpNotEmpty,
	}, catalog.OpsFor(lists))
	assert.False(t, catalog.Takes(lists, catalog.OpAnyContains))

	names, _ := catalog.FieldByID(catalog.FieldFailedNames)
	assert.True(t, catalog.Takes(names, catalog.OpAnyContains))
	assert.False(t, catalog.Takes(names, catalog.OpOneOf))
}

func TestTriggers(t *testing.T) {
	t.Parallel()

	seen := map[catalog.Event]bool{}

	for _, tr := range catalog.Triggers() {
		assert.False(t, seen[tr.Event], "%s is listed once", tr.Event)
		seen[tr.Event] = true

		assert.NotEmpty(t, tr.Label, tr.Event)
		assert.NotEmpty(t, tr.Help, tr.Event)
		assert.NotEmpty(t, tr.OncePer, tr.Event)
		assert.NotEmpty(t, tr.Kinds, tr.Event)

		for _, o := range tr.Options {
			assert.True(t, tr.Takes(o.ID))

			if o.Range != nil {
				assert.True(t, o.Range.Holds(o.Range.Default), "%s's %s default is in range", tr.Event, o.ID)
			}
		}

		got, ok := catalog.TriggerByEvent(tr.Event)
		assert.True(t, ok)
		assert.Equal(t, tr, got)
	}

	failed, _ := catalog.TriggerByEvent(catalog.EventChecksFailed)
	assert.True(t, failed.Takes(catalog.OptionWaitForChecks), "the flagship's option")
	assert.False(t, failed.Applies(item.KindIssue))
	assert.True(t, failed.Applies(item.KindPR))
}

func TestOutcomesComeFromSteps(t *testing.T) {
	t.Parallel()

	for _, o := range catalog.Outcomes() {
		_, ok := catalog.StepByKind(o.From)
		assert.True(t, ok, o.ID)

		got, ok := catalog.OutcomeByID(o.ID)
		assert.True(t, ok)
		assert.Equal(t, o, got)
	}

	_, ok := catalog.OutcomeByID("merged")
	assert.False(t, ok)
}

func TestPresets(t *testing.T) {
	t.Parallel()

	var push, read []catalog.Preset

	for _, p := range catalog.Presets() {
		if p.MayPush {
			push = append(push, p.ID)
		}

		if p.ReadOnly {
			read = append(read, p.ID)
		}
	}

	assert.Equal(t, []catalog.Preset{catalog.PresetEditTestPush, catalog.PresetProfile}, push)
	assert.Equal(t, []catalog.Preset{catalog.PresetReadOnly}, read)

	def, ok := catalog.PresetByID(catalog.DefaultPreset)
	assert.True(t, ok)
	assert.True(t, def.MayPush, "the default lets the flagship push its fix")
}

// TestEverySettingsDefaultIsInItsRange checks the settings the builder reads its numbers
// from: each one's default, where it has one, is a value it takes, and the step settings
// name a step the catalog lists.
func TestEverySettingsDefaultIsInItsRange(t *testing.T) {
	t.Parallel()

	c := catalog.All()

	for _, s := range slices.Concat(c.AgentSettings, c.Limits, c.StepSettings) {
		if s.Range != nil && s.Range.Default != 0 {
			assert.True(t, s.Range.Holds(s.Range.Default), "%s's default is in range", s.ID)
		}
	}

	for _, s := range c.StepSettings {
		kind, _, ok := strings.Cut(s.ID, ".")
		require.True(t, ok, s.ID)

		_, listed := catalog.StepByKind(catalog.StepKind(kind))
		assert.True(t, listed, "%s names a step", s.ID)
		assert.NotNil(t, s.Range, "%s is a whole number", s.ID)
	}
}

// TestRangesMatchTheirOwners checks the ranges the catalog keeps beside the packages
// that own them, so the two can't drift.
func TestRangesMatchTheirOwners(t *testing.T) {
	t.Parallel()

	assert.Equal(t, snooze.MinDays, catalog.MinSnoozeDays)
	assert.Equal(t, snooze.MaxDays, catalog.MaxSnoozeDays)
	assert.Equal(t, snooze.DefaultDays, catalog.DefaultSnoozeDays)

	ecosystems := bump.Ecosystems()
	eco, _ := catalog.FieldByID(catalog.FieldEcosystem)
	require.Len(t, eco.Choices, len(ecosystems))

	for i, e := range ecosystems {
		assert.Equal(t, string(e.ID), eco.Choices[i].Value)
	}
}

func TestSizeOfMatchesTheInboxCard(t *testing.T) {
	t.Parallel()

	for lines, want := range map[int]string{0: "XS", 10: "XS", 11: "S", 50: "S", 51: "M", 200: "M", 201: "L", 500: "L", 501: "XL"} {
		assert.Equal(t, want, catalog.SizeOf(lines), lines)
	}
}

func TestParseLength(t *testing.T) {
	t.Parallel()

	for s, want := range map[string]time.Duration{"30m": 30 * time.Minute, "2h": 2 * time.Hour, "14d": 14 * 24 * time.Hour, "99999d": 99999 * 24 * time.Hour} {
		got, err := catalog.ParseLength(s)
		require.NoError(t, err, s)
		assert.Equal(t, want, got, s)
	}

	for _, s := range []string{"", "m", "0m", "05m", "+5m", "-5m", "5", "5w", "1.5h", "100000d", " 5m"} {
		_, err := catalog.ParseLength(s)
		require.ErrorIs(t, err, catalog.ErrInvalidLength, s)
	}
}

func TestAllHasNoNilList(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(catalog.All())
	require.NoError(t, err)

	// A setting without a range has none; every list is an array.
	lists := strings.ReplaceAll(string(data), `"range":null`, "")
	assert.NotContains(t, lists, "null", "the window gets an array for every list")
}
