package review

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Runes whose case folds in ways a plain lowercase compare misses, and a space
// strings.TrimSpace trims that a plain " " compare misses.
const (
	// dottedI is LATIN CAPITAL LETTER I WITH DOT ABOVE, which lowercases to "i" but
	// folds to no other letter.
	dottedI = "\u0130"

	// kelvin is KELVIN SIGN, which folds to "k" and "K".
	kelvin = "\u212a"

	// sharpS is LATIN SMALL LETTER SHARP S, which folds to capitalSharpS, LATIN CAPITAL
	// LETTER SHARP S, but never to "ss".
	sharpS        = "\u00df"
	capitalSharpS = "\u1e9e"

	// nbsp is NO-BREAK SPACE.
	nbsp = "\u00a0"
)

func TestNamesOnceTrimsDropsTheEmptyAndKeepsTheFirstSpelling(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		list []string
		want []string
	}{
		{name: "nil", list: nil, want: []string{}},
		{name: "empty", list: []string{}, want: []string{}},
		{name: "only blanks", list: []string{"", "  ", "\t\n", nbsp}, want: []string{}},
		{
			name: "trimmed of any space, inner space kept",
			list: []string{" \tflaky\n", nbsp + "docs" + nbsp, "good first issue "},
			want: []string{"flaky", "docs", "good first issue"},
		},
		{
			name: "in the order given",
			list: []string{"zeta", "alpha", "mu"},
			want: []string{"zeta", "alpha", "mu"},
		},
		{
			name: "the first spelling of each, ignoring case",
			list: []string{"Chore", " chore ", "CHORE", "octo-user", "Octo-User"},
			want: []string{"Chore", "octo-user"},
		},
		{
			name: "the dotted I folds to no other letter",
			list: []string{dottedI + "stanbul", "istanbul", "Istanbul", dottedI + "STANBUL"},
			want: []string{dottedI + "stanbul", "istanbul"},
		},
		{
			name: "the Kelvin sign folds to k",
			list: []string{kelvin + "elvin", "kelvin", "KELVIN"},
			want: []string{kelvin + "elvin"},
		},
		{
			name: "sharp s folds to its capital, not to ss",
			list: []string{"stra" + sharpS + "e", "STRASSE", "STRA" + capitalSharpS + "E", "strasse"},
			want: []string{"stra" + sharpS + "e", "STRASSE"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := slices.Clone(tc.list)
			got := namesOnce(tc.list)

			require.NotNil(t, got, "never nil, so the window gets []")
			assert.Equal(t, tc.want, got)
			assert.Equal(t, before, tc.list, "the list is left as it was")
		})
	}
}

func TestNamesApartLeavesOutTheNamesBothHold(t *testing.T) {
	t.Parallel()

	// A nil want is a nil list: assert.Equal tells nil from empty.
	for _, tc := range []struct {
		name                string
		add, remove         []string
		wantAdd, wantRemove []string
	}{
		{name: "nil stays nil"},
		{
			name: "empty stays empty", add: []string{}, remove: []string{},
			wantAdd: []string{}, wantRemove: []string{},
		},
		{name: "nil beside some", remove: []string{"p1"}, wantRemove: []string{"p1"}},
		{name: "some beside nil", add: []string{"ux", "Docs"}, wantAdd: []string{"ux", "Docs"}},
		{
			name: "in order, ignoring case", add: []string{"perf", "Docs", "ci", "wontfix"},
			remove:  []string{"DOCS", "triage", "WontFix"},
			wantAdd: []string{"perf", "ci"}, wantRemove: []string{"triage"},
		},
		{
			name: "all of both, left empty", add: []string{"flaky"}, remove: []string{"FLAKY"},
			wantAdd: []string{}, wantRemove: []string{},
		},
		{
			name: "a name given twice stays twice", add: []string{"chore", "chore", "ci"}, remove: []string{"docs"},
			wantAdd: []string{"chore", "chore", "ci"}, wantRemove: []string{"docs"},
		},
		{
			name: "a name given twice goes twice", add: []string{"perf", "ci", "Perf"}, remove: []string{"PERF"},
			wantAdd: []string{"ci"}, wantRemove: []string{},
		},
		{
			name: "nothing is trimmed", add: []string{" ux"}, remove: []string{"ux"},
			wantAdd: []string{" ux"}, wantRemove: []string{"ux"},
		},
		{
			name: "the dotted I is not i", add: []string{dottedI}, remove: []string{"i", "I"},
			wantAdd: []string{dottedI}, wantRemove: []string{"i", "I"},
		},
		{
			name: "the Kelvin sign is k", add: []string{kelvin + "elvin"}, remove: []string{"kelvin"},
			wantAdd: []string{}, wantRemove: []string{},
		},
		{
			name: "sharp s is not ss", add: []string{"stra" + sharpS + "e"},
			remove:  []string{"strasse", "STRA" + capitalSharpS + "E"},
			wantAdd: []string{}, wantRemove: []string{"strasse"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			addBefore, removeBefore := slices.Clone(tc.add), slices.Clone(tc.remove)
			gotAdd, gotRemove := namesApart(tc.add, tc.remove)

			assert.Equal(t, tc.wantAdd, gotAdd)
			assert.Equal(t, tc.wantAdd == nil, gotAdd == nil, "nil only for nil")
			assert.Equal(t, tc.wantRemove, gotRemove)
			assert.Equal(t, tc.wantRemove == nil, gotRemove == nil, "nil only for nil")
			assert.Equal(t, addBefore, tc.add, "add is left as it was")
			assert.Equal(t, removeBefore, tc.remove, "remove is left as it was")
		})
	}
}
