package textutil_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/textutil"
)

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "short", in: "abc", n: 5, want: "abc"},
		{name: "exact", in: "abcde", n: 5, want: "abcde"},
		{name: "cut", in: "abcdef", n: 5, want: "abcd…"},
		{name: "space before the ellipsis", in: "abc defg", n: 5, want: "abc…"},
		{name: "runes, not bytes", in: "ééééé€", n: 5, want: "éééé…"},
		{name: "one rune", in: "abc", n: 1, want: "…"},
		{name: "no limit", in: "abc", n: 0, want: "abc"},
		{name: "empty", in: "", n: 3, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, textutil.Truncate(tt.in, tt.n))
		})
	}
}

func TestClip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{name: "short", in: " abc ", n: 10, want: " abc "},
		{name: "cut", in: "abcdef", n: 3, want: "abc"},
		{name: "cut and trimmed", in: "ab cdef", n: 3, want: "ab"},
		{name: "runes, not bytes", in: "éééé", n: 2, want: "éé"},
		{name: "no limit", in: "abc", n: -1, want: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, textutil.Clip(tt.in, tt.n))
		})
	}
}

func TestCapitalize(t *testing.T) {
	t.Parallel()

	assert.Empty(t, textutil.Capitalize(""))
	assert.Equal(t, "Éclair", textutil.Capitalize("éclair"))
	assert.Equal(t, "Already", textutil.Capitalize("Already"))
	assert.Equal(t, " leading", textutil.Capitalize(" leading"))
}

func TestSentence(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":                          "",
		"  ":                        "",
		"no prompt applies":         "No prompt applies.",
		"Done.":                     "Done.",
		"  --model is set by it \n": "--model is set by it.",
		"éclair":                    "Éclair.",
	}

	for in, want := range tests {
		assert.Equal(t, want, textutil.Sentence(in), in)
	}
}

func TestNonNil(t *testing.T) {
	t.Parallel()

	var none []string

	assert.NotNil(t, textutil.NonNil(none))
	assert.Empty(t, textutil.NonNil(none))
	assert.Equal(t, []int{1, 2}, textutil.NonNil([]int{1, 2}))
}

func TestJoin(t *testing.T) {
	t.Parallel()

	type choice string

	assert.Equal(t, "a, b", textutil.Join([]choice{"a", "b"}, ", "))
	assert.Empty(t, textutil.Join([]choice(nil), ", "))
}
