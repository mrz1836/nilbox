package textutil_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// The runes whose case the tests turn on. strings.EqualFold folds simply: the Kelvin sign
// is a k, and the capital sharp s is a sharp s, but the dotted capital I is no i (though it
// lower-cases to one), and a sharp s is no "ss".
const (
	dottedI       = "\u0130" // LATIN CAPITAL LETTER I WITH DOT ABOVE
	kelvin        = "\u212a" // KELVIN SIGN, which folds to k and K
	sharpS        = "\u00df" // LATIN SMALL LETTER SHARP S
	capitalSharpS = "\u1e9e" // LATIN CAPITAL LETTER SHARP S, which folds to sharpS
	nbsp          = "\u00a0" // NO-BREAK SPACE, which strings.TrimSpace trims
	ideographic   = "\u3000" // IDEOGRAPHIC SPACE, which strings.TrimSpace trims
)

func TestContainsFold(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		list []string
		s    string
		want bool
	}{
		{name: "nil holds nothing", list: nil, s: "bug", want: false},
		{name: "an empty list holds nothing", list: []string{}, s: "", want: false},
		{name: "the same spelling", list: []string{"docs", "bug"}, s: "bug", want: true},
		{name: "another case", list: []string{"docs", "Bug"}, s: "BUG", want: true},
		{name: "an empty name, held", list: []string{"docs", ""}, s: "", want: true},
		{name: "another name", list: []string{"docs", "bug"}, s: "bugs", want: false},
		{name: "nothing is trimmed", list: []string{" ux"}, s: "ux", want: false},
		{name: "nor the name looked for", list: []string{"ux"}, s: "ux" + nbsp, want: false},
		{name: "the Kelvin sign is a k", list: []string{"kelvin"}, s: kelvin + "elvin", want: true},
		{name: "the dotted I is no i", list: []string{"i", "I"}, s: dottedI, want: false},
		{name: "a capital sharp s is a sharp s", list: []string{"stra" + sharpS + "e"}, s: "STRA" + capitalSharpS + "E", want: true},
		{name: "a sharp s is no ss", list: []string{"strasse"}, s: "stra" + sharpS + "e", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, textutil.ContainsFold(tc.list, tc.s))
		})
	}
}

func TestUniqueFoldTrimmedKeepsTheFirstSpelling(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		list []string
		want []string
	}{
		{name: "nil", list: nil, want: []string{}},
		{name: "empty", list: []string{}, want: []string{}},
		{name: "only blanks", list: []string{"", "  ", "\t\n", nbsp, ideographic + " "}, want: []string{}},
		{
			name: "trimmed of any space, inner space kept",
			list: []string{" \tflaky\n", nbsp + "docs" + nbsp, "good first issue "},
			want: []string{"flaky", "docs", "good first issue"},
		},
		{name: "in the order given", list: []string{"zeta", "alpha", "mu"}, want: []string{"zeta", "alpha", "mu"}},
		{
			name: "trimmed before it is compared",
			list: []string{"  zeta", "Alpha  ", "beta", "alpha"},
			want: []string{"zeta", "Alpha", "beta"},
		},
		{
			name: "the first spelling of each, ignoring case",
			list: []string{"Chore", " chore ", "CHORE", "octo-user", "Octo-User"},
			want: []string{"Chore", "octo-user"},
		},
		{name: "the first spelling, lower first", list: []string{"bug", "Bug"}, want: []string{"bug"}},
		{
			name: "the dotted I folds to no other letter",
			list: []string{dottedI + "stanbul", "istanbul", "Istanbul", dottedI + "STANBUL"},
			want: []string{dottedI + "stanbul", "istanbul"},
		},
		{name: "the Kelvin sign folds to k", list: []string{kelvin + "elvin", "kelvin", "KELVIN"}, want: []string{kelvin + "elvin"}},
		{name: "a k folds to the Kelvin sign", list: []string{"k", kelvin}, want: []string{"k"}},
		{
			name: "sharp s folds to its capital, not to ss",
			list: []string{"stra" + sharpS + "e", "STRASSE", "STRA" + capitalSharpS + "E", "strasse"},
			want: []string{"stra" + sharpS + "e", "STRASSE"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			before := slices.Clone(tc.list)
			got := textutil.UniqueFold(tc.list, strings.TrimSpace)

			require.NotNil(t, got, "never nil, so the window gets []")
			assert.Equal(t, tc.want, got)
			assert.Equal(t, before, tc.list, "the list is left as it was")
		})
	}
}

func TestUniqueFoldCleansWithWhatItIsGiven(t *testing.T) {
	t.Parallel()

	// clip keeps a name's first three bytes, as a caller cuts a name to its cap.
	clip := func(s string) string { return s[:min(len(s), 3)] }

	for _, tc := range []struct {
		name  string
		list  []string
		clean func(string) string
		want  []string
	}{
		{name: "nil cleans nothing", list: nil, want: []string{}},
		{
			name: "nil keeps space, and drops only the empty",
			list: []string{"", " ", "ux", " ux", "UX", " "}, want: []string{" ", "ux", " ux"},
		},
		{
			name: "compared once cleaned", list: []string{"abcde", "ABCxy", "ab", "AB "},
			clean: clip, want: []string{"abc", "ab", "AB "},
		},
		{
			name: "dropped when cleaned empty", list: []string{"keep", "drop", "Drop"},
			clean: func(s string) string { return strings.TrimPrefix(strings.ToLower(s), "drop") },
			want:  []string{"keep"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := textutil.UniqueFold(tc.list, tc.clean)

			require.NotNil(t, got)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestUniqueFoldReturnsAListOfItsOwn(t *testing.T) {
	t.Parallel()

	list := []string{"b", "a", "c"}
	got := textutil.UniqueFold(list, nil)
	require.Equal(t, list, got)

	slices.Sort(got)
	got[0] = "changed"

	assert.Equal(t, []string{"b", "a", "c"}, list, "sorting or changing the result leaves list as it was")
}

// TestApartFoldLeavesOutTheNamesBothHold reads an edit's two lists apart, as the sheets
// do: what to add without what both name, and what to take off without what both name.
func TestApartFoldLeavesOutTheNamesBothHold(t *testing.T) {
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
			gotAdd, gotRemove := textutil.ApartFold(tc.add, tc.remove), textutil.ApartFold(tc.remove, tc.add)

			assert.Equal(t, tc.wantAdd, gotAdd)
			assert.Equal(t, tc.wantAdd == nil, gotAdd == nil, "nil only for nil")
			assert.Equal(t, tc.wantRemove, gotRemove)
			assert.Equal(t, tc.wantRemove == nil, gotRemove == nil, "nil only for nil")
			assert.Equal(t, addBefore, tc.add, "add is left as it was")
			assert.Equal(t, removeBefore, tc.remove, "remove is left as it was")
		})
	}
}

func TestApartFoldReturnsAListOfItsOwn(t *testing.T) {
	t.Parallel()

	list := []string{"a", "b"}
	got := textutil.ApartFold(list, nil)
	require.Equal(t, list, got)

	got[0] = "changed"

	assert.Equal(t, []string{"a", "b"}, list, "changing the result leaves list as it was")
}
