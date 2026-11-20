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

// formatTypes walks the format from Spec and returns each of its struct types, in the
// order they are first reached.
func formatTypes() []reflect.Type {
	var (
		types []reflect.Type
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
		types = append(types, typ)

		for i := range typ.NumField() {
			if inner := structOf(typ.Field(i).Type); inner != nil && !seen[inner] {
				queue = append(queue, inner)
			}
		}
	}

	return types
}

// structKeys writes each of the format's structs' JSON keys, with the Go type each holds,
// in the order formatTypes reaches the structs.
func structKeys() ([]string, []string) {
	var lines, keys []string

	for _, typ := range formatTypes() {
		lines = append(lines, typ.String())

		for i := range typ.NumField() {
			f := typ.Field(i)
			name := jsonName(f)
			keys = append(keys, name)
			lines = append(lines, fmt.Sprintf("  %s: %s", name, f.Type))
		}
	}

	return lines, keys
}

// jsonName is a struct field's JSON key.
func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")

	return name
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

// enums lists every closed set of values the format holds (spec.ClosedSets), by where it
// is used, each value with what the catalog says of it where the golden shows that: a
// field's type and choices, an operator's type and arity, a trigger's options, a step's
// dispatcher, and the step an outcome comes from.
func enums() map[string][]string {
	out := map[string][]string{}

	for _, set := range spec.ClosedSets() {
		suffix, say := details(set.Name)
		out[set.Name+suffix] = each(set.Values, say)
	}

	return out
}

// details returns how enums writes the set it names: the words after its name, and each
// value with the catalog's details.
func details(name string) (string, func(string) string) {
	switch name {
	case "condition.field":
		return " (type: choices)", func(v string) string {
			f, _ := catalog.FieldByID(catalog.FieldID(v))

			return fieldLine(f)
		}
	case "condition.op":
		return " (type, arity)", func(v string) string {
			o, _ := catalog.OpByID(catalog.Op(v))

			return v + " (" + string(o.Type) + ", " + string(o.Arity) + ")"
		}
	case "trigger.event":
		return " (its options)", func(v string) string {
			t, _ := catalog.TriggerByEvent(catalog.Event(v))

			return v + " (" + strings.Join(each(t.Options, func(o catalog.OptionInfo) string { return string(o.ID) }), ", ") + ")"
		}
	case "step kinds":
		return "", func(v string) string {
			s, _ := catalog.StepByKind(catalog.StepKind(v))

			return v + " (" + string(s.Dispatcher) + ")"
		}
	case "step.onlyIf":
		return "", func(v string) string {
			o, _ := catalog.OutcomeByID(catalog.Outcome(v))

			return v + " (from " + string(o.From) + ")"
		}
	default:
		return "", func(v string) string { return v }
	}
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
