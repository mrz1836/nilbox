package spec

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The runes whose case the tests turn on. Go's case-insensitive compare folds simply: the
// Kelvin sign is a k, and the capital sharp s is a sharp s, but the dotted capital I is no
// i (though it lower-cases to one), and a sharp s is no "ss".
const (
	dottedI     = "\u0130"  // the dotted capital I, which strings.ToLower makes "i" but strings.EqualFold keeps apart
	iWithDot    = "i\u0307" // an i and a combining dot above, two runes
	kelvin      = "\u212a"  // the Kelvin sign, which folds to k
	sharpS      = "\u00df"  // the sharp s
	capitalSzet = "\u1e9e"  // the capital sharp s, which folds to the sharp s
	noBreak     = "\u00a0"  // a no-break space, which strings.TrimSpace trims
	ideographic = "\u3000"  // an ideographic space, which strings.TrimSpace trims
)

// TestTextSetContract pins what textSet returns, which Normalize stores in a spec and so
// in the routine's hash: every value trimmed, an empty one dropped, a repeat ignoring
// case dropped with the first spelling kept, sorted by strings.ToLower with ties in the
// order written, and nil, never an empty list, for none.
func TestTextSetContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil is none", in: nil, want: nil},
		{name: "an empty list is none", in: []string{}, want: nil},
		{name: "only space is none", in: []string{"", " ", "\t\n", noBreak, ideographic + " "}, want: nil},
		{name: "trimmed, then sorted ignoring case", in: []string{" b ", "a", "C"}, want: []string{"a", "b", "C"}},
		{name: "trimmed before it is compared", in: []string{"  zeta", "Alpha  ", "beta", "alpha"}, want: []string{"Alpha", "beta", "zeta"}},
		{name: "the first spelling is kept", in: []string{"Bug", "bug", "BUG", " bug "}, want: []string{"Bug"}},
		{name: "the first spelling is kept, lower first", in: []string{"bug", "Bug"}, want: []string{"bug"}},
		{name: "the Kelvin sign is a k", in: []string{kelvin, "k", "K"}, want: []string{kelvin}},
		{name: "a k is the Kelvin sign", in: []string{"k", kelvin}, want: []string{"k"}},
		{name: "a sharp s is no ss, and a capital sharp s is one", in: []string{sharpS, "SS", "ss", capitalSzet}, want: []string{"SS", sharpS}},
		{name: "a dotted I is no i, and ties with it in the order written", in: []string{dottedI, "i"}, want: []string{dottedI, "i"}},
		{name: "an i ties with a dotted I in the order written", in: []string{"i", dottedI}, want: []string{"i", dottedI}},
		{
			name: "an i with a combining dot sorts after both",
			in:   []string{iWithDot, dottedI, "i"}, want: []string{dottedI, "i", iWithDot},
		},
		{
			// Past 12 values an unstable sort reorders this tie (TestStableTieTellsTheSortsApart
			// checks it still does): textSet's is stable, so the dotted I written first stays
			// first.
			name: "ties stay in the order written past 12 values", in: stableTieIn(), want: stableTieWant(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := slices.Clone(tt.in)
			got := textSet(in)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.in, in, "the values passed in are left as they were")

			if tt.want == nil {
				assert.Nil(t, got, "none is nil, so an empty list is left out of the spec")
			}
		})
	}
}

// stableTieIn is 13 values, none a repeat, with a tie (a dotted I written before an i)
// that slices.SortFunc's unstable sort puts the other way round.
func stableTieIn() []string {
	return []string{dottedI, "l", "k", "j", "h", "g", "i", "f", "e", "d", "c", "b", "a"}
}

// stableTieWant is stableTieIn sorted by strings.ToLower, its tie in the order written.
func stableTieWant() []string {
	return []string{"a", "b", "c", "d", "e", "f", "g", "h", dottedI, "i", "j", "k", "l"}
}

// TestStableTieTellsTheSortsApart fails when slices.SortFunc, with textSet's comparison,
// orders stableTieIn as a stable sort does, so the tie case above can't stop telling a
// stable sort from an unstable one without failing: should a Go release change the
// unstable sort, the case needs an input that this sort reorders.
func TestStableTieTellsTheSortsApart(t *testing.T) {
	t.Parallel()

	unstable := stableTieIn()
	slices.SortFunc(unstable, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})

	assert.ElementsMatch(t, stableTieWant(), unstable)
	assert.NotEqual(t, stableTieWant(), unstable, "an unstable sort reorders the tie, so only a stable one passes the case")
}

// TestTextSetIsWhatNormalizeStores pins that textSet is what Normalize stores for a list that is a
// set: the tools a routine requires among them.
func TestTextSetIsWhatNormalizeStores(t *testing.T) {
	t.Parallel()

	in := []string{" pnpm", "govulncheck", "PNPM", dottedI, "i"}
	assert.Equal(t, textSet(in), Normalize(Spec{Requires: in}).Requires)
	assert.Equal(t, []string{"govulncheck", dottedI, "i", "pnpm"}, Normalize(Spec{Requires: in}).Requires)
	assert.Nil(t, Normalize(Spec{Requires: []string{" "}}).Requires)
}
