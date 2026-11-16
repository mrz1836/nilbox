package spec_test

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
)

// forbidden are the words no part of the format, nor any value it can hold, may be:
// Autopilot never closes anything, approves workflow runs, or merges past GitHub's rules.
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

// structKeys walks the format from Spec and writes each struct's JSON keys, with the Go
// type each holds, in the order the types are first reached.
func structKeys() ([]string, []string) {
	var (
		lines []string
		keys  []string
		seen  = map[reflect.Type]bool{}
		queue = []reflect.Type{reflect.TypeFor[spec.Spec]()}
	)

	for len(queue) > 0 {
		typ := queue[0]
		queue = queue[1:]

		if seen[typ] {
			continue
		}

		seen[typ] = true
		lines = append(lines, typ.String())

		for i := range typ.NumField() {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			keys = append(keys, name)
			lines = append(lines, fmt.Sprintf("  %s: %s", name, f.Type))

			if inner := structOf(f.Type); inner != nil && !seen[inner] {
				queue = append(queue, inner)
			}
		}
	}

	return lines, keys
}

// structOf returns the struct a field holds, through pointers and slices, or nil.
func structOf(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	if t.Kind() == reflect.Struct {
		return t
	}

	return nil
}

// enums lists every closed set of values the format holds, by where it is used.
func enums() map[string][]string {
	words := func(values ...string) []string { return values }
	out := map[string][]string{
		"conditions.match": words(string(spec.MatchAll), string(spec.MatchAny)),
		"limits.oncePer":   words(string(catalog.OncePerItem), string(catalog.OncePerHeadCommit), string(catalog.OncePerDay)),
		"agent.where":      words(string(catalog.WhereBackground), string(catalog.WhereTerminal)),
		"merge.how":        words(string(catalog.MergeAutoIfItWasOn), string(catalog.MergeWhenMergeable)),
		"merge.method":     words(string(catalog.MergeCommit), string(catalog.MergeSquash), string(catalog.MergeRebase)),
		"postReview.as":    words(string(catalog.ReviewAsComment), string(catalog.ReviewAsVerdict)),
		"tellMe.when":      words(string(catalog.TellReached), string(catalog.TellVerdict)),
		"schedule.every":   words(string(catalog.EveryDay), string(catalog.EveryWeekday), string(catalog.EveryWeek)),
	}

	out["schedule.on, activeHours.days"] = each(catalog.Weekdays(), func(d catalog.Weekday) string { return string(d) })
	out["source.lists"] = each(catalog.ListSources(), func(l catalog.ListSourceInfo) string { return string(l.ID) })
	out["search.involving"] = each(watch.Involvings(), func(i watch.Involving) string { return string(i) })
	out["search.state"] = each(watch.States(), func(s watch.State) string { return string(s) })
	out["fence.tools"] = each(catalog.Presets(), func(p catalog.PresetInfo) string { return string(p.ID) })
	out["condition.field (type: choices)"] = each(catalog.Fields(), fieldLine)
	out["github actions"] = each([]catalog.GitHubActionKind{
		catalog.GitHubRebase, catalog.GitHubUpdateBranch, catalog.GitHubReply, catalog.GitHubLabels,
	}, func(a catalog.GitHubActionKind) string { return string(a) })
	out["step kinds"] = each(catalog.Steps(), func(s catalog.Step) string {
		return string(s.Kind) + " (" + string(s.Dispatcher) + ")"
	})
	out["step.onlyIf"] = each(catalog.Outcomes(), func(o catalog.OutcomeInfo) string {
		return string(o.ID) + " (from " + string(o.From) + ")"
	})
	out["condition.op (type, arity)"] = each(catalog.Ops(), func(o catalog.OpInfo) string {
		return string(o.ID) + " (" + string(o.Type) + ", " + string(o.Arity) + ")"
	})
	out["trigger.event (its options)"] = each(catalog.Triggers(), func(t catalog.Trigger) string {
		return string(t.Event) + " (" + strings.Join(each(t.Options, func(o catalog.OptionInfo) string { return string(o.ID) }), ", ") + ")"
	})

	return out
}

// each returns what say writes of every element of list.
func each[T any](list []T, say func(T) string) []string {
	out := make([]string, 0, len(list))
	for _, e := range list {
		out = append(out, say(e))
	}

	return out
}

// fieldLine writes a field, its type, and its choices.
func fieldLine(f catalog.Field) string {
	line := string(f.ID) + " (" + string(f.Type)

	if len(f.Choices) > 0 {
		values := make([]string, 0, len(f.Choices))
		for _, c := range f.Choices {
			values = append(values, c.Value)
		}

		line += ": " + strings.Join(values, ", ")
	}

	return line + ")"
}

// TestSchemaGolden pins every name the format uses, version 1's for good: a stored spec
// can't change, so a later build adds the code that runs a step, never a format change.
// After a deliberate change, which needs a new version, rewrite it with
// go test ./internal/autopilot/spec -run TestSchemaGolden -update.
func TestSchemaGolden(t *testing.T) {
	t.Parallel()

	lines, _ := structKeys()

	var b strings.Builder

	b.WriteString(strings.Join(lines, "\n") + "\n")

	e := enums()
	names := make([]string, 0, len(e))

	for name := range e {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		b.WriteString("\n" + name + "\n")

		for _, v := range e[name] {
			b.WriteString("  " + v + "\n")
		}
	}

	golden(t, filepath.Join("testdata", "schema.golden"),
		fmt.Sprintf("# The routine spec's format, version %d: each type's JSON keys and the Go type each holds,\n"+
			"# then every closed set of values. Rewrite with\n"+
			"# go test ./internal/autopilot/spec -run TestSchemaGolden -update\n\n", spec.Version),
		b.String())
}

// TestFormatHasNoForbiddenWords is the guard: no key of the format, and no value any of
// its closed sets holds, names a close, an approval of workflow runs, or a merge past
// GitHub's rules.
func TestFormatHasNoForbiddenWords(t *testing.T) {
	t.Parallel()

	_, keys := structKeys()
	names := slices.Clone(keys)

	for _, values := range enums() {
		for _, v := range values {
			name, _, _ := strings.Cut(v, " (")
			names = append(names, name)
		}
	}

	for _, f := range catalog.Fields() {
		for _, c := range f.Choices {
			names = append(names, c.Value)
		}
	}

	for _, name := range names {
		for _, word := range forbidden() {
			assert.NotContains(t, squash(name), squash(word), "%q is in the format", name)
		}
	}
}
