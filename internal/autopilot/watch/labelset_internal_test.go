package watch

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The runes whose case the tests turn on. Go's case-insensitive compare folds simply: the
// Kelvin sign is a k, and the capital sharp s is a sharp s, but the dotted capital I is no
// i (though it lower-cases to one), and a sharp s is no "ss".
const (
	dottedI     = "\u0130" // the dotted capital I, which strings.ToLower makes "i" but strings.EqualFold keeps apart
	kelvin      = "\u212a" // the Kelvin sign, which folds to k
	sharpS      = "\u00df" // the sharp s
	capitalSzet = "\u1e9e" // the capital sharp s, which folds to the sharp s
	noBreak     = "\u00a0" // a no-break space, which strings.TrimSpace trims
)

// TestLabelSetContract pins what labelSet returns, which Normalize stores for a search's
// labels and labels left out, in the spec and so in the query: every label trimmed, an
// empty one dropped, a repeat ignoring case dropped with the first spelling kept, sorted
// by strings.ToLower, and nil, never an empty list, for none.
//
// Its sort isn't stable, so the order of two labels that lower-case alike but don't fold
// alike (a dotted I and an i, the one such pair a repeat ignoring case doesn't drop) is
// the sort's to choose; with more than 12 labels it may differ from the order written.
// The cases here hold no such pair, except the last, which pins only what is kept.
func TestLabelSetContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil is none", in: nil, want: nil},
		{name: "an empty list is none", in: []string{}, want: nil},
		{name: "only space is none", in: []string{"", " ", "\t\n", noBreak}, want: nil},
		{name: "trimmed, then sorted ignoring case", in: []string{" b ", "a", "C"}, want: []string{"a", "b", "C"}},
		{name: "trimmed before it is compared", in: []string{"  zeta", "Alpha  ", "beta", "alpha"}, want: []string{"Alpha", "beta", "zeta"}},
		{name: "the first spelling is kept", in: []string{"Bug", "bug", "BUG", " bug "}, want: []string{"Bug"}},
		{name: "the first spelling is kept, lower first", in: []string{"bug", "Bug"}, want: []string{"bug"}},
		{name: "the Kelvin sign is a k", in: []string{kelvin, "k", "K"}, want: []string{kelvin}},
		{name: "a k is the Kelvin sign", in: []string{"k", kelvin}, want: []string{"k"}},
		{name: "a sharp s is no ss, and a capital sharp s is one", in: []string{sharpS, "SS", "ss", capitalSzet}, want: []string{"SS", sharpS}},
		{
			name: "many labels, sorted",
			in:   []string{"needs-review", "Bug", "wontfix", "dependencies", "good first issue", "a11y", "UI", "docs", "help wanted", "P1", "p2", "Security", "triage", "bug"},
			want: []string{"a11y", "Bug", "dependencies", "docs", "good first issue", "help wanted", "needs-review", "P1", "p2", "Security", "triage", "UI", "wontfix"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := slices.Clone(tt.in)
			got := labelSet(in)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.in, in, "the labels passed in are left as they were")

			if tt.want == nil {
				assert.Nil(t, got, "none is nil, so an empty list is left out of the spec")
			}
		})
	}

	in := []string{"n", "m", dottedI, "l", "k", "j", "h", "g", "f", "e", "d", "c", "b", "i", "a"}
	got := labelSet(slices.Clone(in))
	assert.ElementsMatch(t, in, got, "a dotted I and an i are two labels, whichever the sort puts first")
	assert.Equal(t, []string{"a", "b", "c", "d", "e", "f", "g", "h"}, got[:8])
	assert.Equal(t, []string{"j", "k", "l", "m", "n"}, got[10:])
}

// TestLabelSetIsWhatNormalizeStores pins that labelSet is what Normalize stores for a
// search's labels and labels left out.
func TestLabelSetIsWhatNormalizeStores(t *testing.T) {
	t.Parallel()

	labels, notLabels := []string{" Bug", "bug", "a11y"}, []string{"wontfix", " "}
	s := Normalize(Search{Labels: slices.Clone(labels), NotLabels: slices.Clone(notLabels)})

	assert.Equal(t, labelSet(labels), s.Labels)
	assert.Equal(t, []string{"a11y", "Bug"}, s.Labels)
	assert.Equal(t, []string{"wontfix"}, s.NotLabels)
	assert.Nil(t, Normalize(Search{Labels: []string{"\t"}}).Labels)
}
