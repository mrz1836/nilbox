package prompts

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dirtyText holds everything sanitizeLine takes out of a value: a newline, a tab, an
// escape, a bidirectional-text control, and runs of spaces.
const dirtyText = "  a\nb\tc\x1bd \u202ee  "

// fillText sets every text v holds, at any depth, to dirtyText: each string, and each
// string in a struct's exported fields, a slice's or an array's elements, a pointer's
// target, or a map's keys and values, a slice, a pointer, or a map made to hold one. A
// number or a yes/no is left as it is; a kind that could hold text it can't reach fails t.
func fillText(t *testing.T, v reflect.Value, path string) {
	t.Helper()

	switch kind := v.Kind(); {
	case kind == reflect.String:
		v.SetString(dirtyText)
	case kind == reflect.Struct:
		for i, field := range exportedFields(v) {
			fillText(t, v.Field(i), path+"."+field)
		}
	case kind == reflect.Slice || kind == reflect.Array || kind == reflect.Pointer || kind == reflect.Map:
		fillHolder(t, v, path)
	case kind < reflect.Bool || kind > reflect.Float64:
		require.Failf(t, "update this guard", "%s is a %s, which it can't fill", path, kind)
	}
}

// fillHolder is fillText for a slice, an array, a pointer, or a map.
func fillHolder(t *testing.T, v reflect.Value, path string) {
	t.Helper()

	if v.Kind() == reflect.Map {
		key, value := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fillText(t, key, path+"{key}")
		fillText(t, value, path+"{value}")
		v.Set(reflect.MakeMap(v.Type()))
		v.SetMapIndex(key, value)

		return
	}

	if v.Kind() == reflect.Pointer {
		v.Set(reflect.New(v.Type().Elem()))
		fillText(t, v.Elem(), "*"+path)

		return
	}

	if v.Kind() == reflect.Slice {
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	}

	for i := range v.Len() {
		fillText(t, v.Index(i), fmt.Sprintf("%s[%d]", path, i))
	}
}

// exportedFields returns the names of a struct's exported fields, by index.
func exportedFields(v reflect.Value) map[int]string {
	fields := map[int]string{}

	for i := range v.NumField() {
		if field := v.Type().Field(i); field.IsExported() {
			fields[i] = field.Name
		}
	}

	return fields
}

// texts returns every text v holds where fillText puts one, by its path.
func texts(v reflect.Value, path string, out map[string]string) map[string]string {
	switch kind := v.Kind(); {
	case kind == reflect.String:
		out[path] = v.String()
	case kind == reflect.Struct:
		for i, field := range exportedFields(v) {
			texts(v.Field(i), path+"."+field, out)
		}
	case kind == reflect.Slice || kind == reflect.Array:
		for i := range v.Len() {
			texts(v.Index(i), fmt.Sprintf("%s[%d]", path, i), out)
		}
	case kind == reflect.Pointer && !v.IsNil():
		texts(v.Elem(), "*"+path, out)
	case kind == reflect.Map:
		mapTexts(v, path, out)
	}

	return out
}

// mapTexts adds to out every text the map v holds in its keys and values.
func mapTexts(v reflect.Value, path string, out map[string]string) {
	for _, key := range v.MapKeys() {
		texts(key, fmt.Sprintf("%s{key %v}", path, key), out)
		texts(v.MapIndex(key), fmt.Sprintf("%s{%v}", path, key), out)
	}
}

// TestEveryDataTextIsSanitized fills every text Data holds with text that isn't one
// plain line and checks that Data.sanitized leaves each one a plain line, so a text
// field added to Data without sanitizing it fails here.
func TestEveryDataTextIsSanitized(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, dirtyText, sanitizeLine(dirtyText), "the dirty text must be something sanitizing changes")

	var d Data

	fillText(t, reflect.ValueOf(&d).Elem(), "Data")

	before := texts(reflect.ValueOf(d), "Data", map[string]string{})
	require.NotEmpty(t, before, "Data holds no text; update this guard")

	for path, s := range before {
		require.Equal(t, dirtyText, s, "%s wasn't filled", path)
	}

	after := texts(reflect.ValueOf(d.sanitized()), "Data", map[string]string{})
	assert.Len(t, after, len(before))

	for path, s := range after {
		assert.Equal(t, sanitizeLine(s), s, "Data.sanitized doesn't sanitize %s", path)
	}
}

// TestFillTextReachesNestedText checks the guard's own walk: text in a nested struct, a
// slice, an array, a pointer, and a map is filled and found, and an unexported field and
// a number are left alone.
func TestFillTextReachesNestedText(t *testing.T) {
	t.Parallel()

	type inner struct {
		Name string
		Tags []string
	}

	var nested struct {
		Top     string
		Count   int
		Inner   inner
		List    []inner
		Pair    [2]string
		Pointer *string
		Lookup  map[string]string
		hidden  string
	}

	fillText(t, reflect.ValueOf(&nested).Elem(), "N")

	got := texts(reflect.ValueOf(nested), "N", map[string]string{})
	assert.Len(t, got, 10)

	for path, s := range got {
		assert.Equal(t, dirtyText, s, path)
	}

	assert.Empty(t, nested.hidden)
	assert.Zero(t, nested.Count)
}
