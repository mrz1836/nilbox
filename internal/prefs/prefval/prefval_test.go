package prefval_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

func TestBoolReadsEveryBooleanSpelling(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"1", "t", "T", "TRUE", "true", "True"} {
		assert.True(t, prefval.Bool(stored, false), "%q is on", stored)
	}

	for _, stored := range []string{"0", "f", "F", "FALSE", "false", "False"} {
		assert.False(t, prefval.Bool(stored, true), "%q is off", stored)
	}
}

func TestBoolKeepsTheFallbackForAnythingElse(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"", "yes", "no", "on", "off", " true", "true ", "tRUE", "2", "maybe"} {
		assert.True(t, prefval.Bool(stored, true), "%q keeps a fallback of on", stored)
		assert.False(t, prefval.Bool(stored, false), "%q keeps a fallback of off", stored)
	}
}

func TestEncodeBoolReadsBackAsItself(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "true", prefval.EncodeBool(true))
	assert.Equal(t, "false", prefval.EncodeBool(false))

	for _, on := range []bool{true, false} {
		assert.Equal(t, on, prefval.Bool(prefval.EncodeBool(on), !on))
	}
}

func TestList(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		stored string
		want   []string
		ok     bool
	}{
		{stored: `["a","b"]`, want: []string{"a", "b"}, ok: true},
		{stored: `[]`, want: []string{}, ok: true},
		{stored: `null`, want: nil, ok: true},
		{stored: ``, want: nil, ok: false},
		{stored: `["a",`, want: nil, ok: false},
		{stored: `"a"`, want: nil, ok: false},
		{stored: `{"a":true}`, want: nil, ok: false},
		{stored: `["a",1]`, want: nil, ok: false},
	} {
		got, ok := prefval.List[string](tc.stored)
		assert.Equal(t, tc.ok, ok, "%q", tc.stored)
		assert.Equal(t, tc.want, got, "%q", tc.stored)
	}
}

func TestListReadsANamedStringType(t *testing.T) {
	t.Parallel()

	type kind string

	got, ok := prefval.List[kind](`["woke","merged"]`)
	assert.True(t, ok)
	assert.Equal(t, []kind{"woke", "merged"}, got)
}

func TestEncodeList(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "[]", prefval.EncodeList[string](nil), "nil is stored as an empty list, never null")
	assert.Equal(t, "[]", prefval.EncodeList([]string{}))
	assert.Equal(t, `["a","b"]`, prefval.EncodeList([]string{"a", "b"}))
	assert.Equal(t, `["\u003cmain\u003e \u0026 \"x\""]`, prefval.EncodeList([]string{`<main> & "x"`}),
		"escaped as encoding/json escapes it")

	type kind string

	assert.Equal(t, `["woke"]`, prefval.EncodeList([]kind{"woke"}))

	got, ok := prefval.List[string](prefval.EncodeList([]string{"a", "", "b"}))
	assert.True(t, ok)
	assert.Equal(t, []string{"a", "", "b"}, got, "a list reads back as itself")
}
