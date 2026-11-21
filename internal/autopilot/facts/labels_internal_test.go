package facts

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The runes whose case the tests turn on. Go's case-insensitive compare folds simply: the
// Kelvin sign is a k, and the capital sharp s is a sharp s, but the dotted capital I is no
// i, and a sharp s is no "ss".
const (
	dottedI     = "\u0130" // the dotted capital I, which strings.EqualFold keeps apart from an i
	kelvin      = "\u212a" // the Kelvin sign, which folds to k
	sharpS      = "\u00df" // the sharp s
	capitalSzet = "\u1e9e" // the capital sharp s, which folds to the sharp s
	noBreak     = "\u00a0" // a no-break space
	rtlOverride = "\u202e" // a right-to-left override, a bidi control
	popBidi     = "\u202c" // the pop of a bidi override
)

// TestLabelsContract pins what labels returns for a row's labels: each made one line and
// cut to maxNameRunes, an empty one dropped, a repeat ignoring case once cut dropped with
// the first spelling kept, in the order the row lists them, never sorted, and an empty
// list, never nil, for none.
func TestLabelsContract(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", maxNameRunes)

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil is an empty list", in: nil, want: []string{}},
		{name: "an empty list", in: []string{}, want: []string{}},
		{name: "only space is an empty list", in: []string{"", " ", "\t\n", noBreak}, want: []string{}},
		{name: "in the order listed, never sorted", in: []string{"wontfix", "Bug", "a11y"}, want: []string{"wontfix", "Bug", "a11y"}},
		{name: "the first spelling is kept", in: []string{"Bug", "bug", "BUG"}, want: []string{"Bug"}},
		{name: "the first spelling is kept, lower first", in: []string{"bug", "Bug"}, want: []string{"bug"}},
		{name: "made one line, then compared", in: []string{"needs\nreview", "Needs review", "  needs   review "}, want: []string{"needs review"}},
		{name: "bidi controls dropped, then compared", in: []string{rtlOverride + "bug" + popBidi, "BUG"}, want: []string{"bug"}},
		{name: "the Kelvin sign is a k", in: []string{kelvin, "k", "K"}, want: []string{kelvin}},
		{name: "a k is the Kelvin sign", in: []string{"k", kelvin}, want: []string{"k"}},
		{name: "a sharp s is no ss, and a capital sharp s is one", in: []string{sharpS, "SS", capitalSzet, "ss"}, want: []string{sharpS, "SS"}},
		{name: "a dotted I is no i", in: []string{dottedI, "i", "I"}, want: []string{dottedI, "i"}},
		{name: "cut to the cap", in: []string{long + "yz"}, want: []string{long}},
		{name: "alike once cut", in: []string{long + "a", strings.ToUpper(long) + "b"}, want: []string{long}},
		{name: "cut, then trimmed", in: []string{long[1:] + " tail"}, want: []string{long[1:]}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := slices.Clone(tt.in)
			got := labels(in)

			assert.Equal(t, tt.want, got)
			assert.NotNil(t, got, "none is an empty list, never nil")
			assert.Equal(t, tt.in, in, "the labels passed in are left as they were")
		})
	}
}
