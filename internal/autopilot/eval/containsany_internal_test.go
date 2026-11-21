package eval

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContainsAnyContract pins containsAny, which the triggers ask whether a routine's
// own run pushed the head commit, the item carries a label, and the item is in an inbox
// list: whether the list holds the text as strings.EqualFold compares, nothing trimmed.
// Go folds simply: the Kelvin sign is a k, and the capital sharp s is a sharp s, but the
// dotted capital I is no i, and a sharp s is no "ss".
func TestContainsAnyContract(t *testing.T) {
	t.Parallel()

	const (
		dottedI     = "\u0130" // the dotted capital I
		kelvin      = "\u212a" // the Kelvin sign
		sharpS      = "\u00df" // the sharp s
		capitalSzet = "\u1e9e" // the capital sharp s
		commit      = "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567"
	)

	tests := []struct {
		name string
		list []string
		s    string
		want bool
	}{
		{name: "nothing holds nothing", list: nil, s: "", want: false},
		{name: "nil holds no label", list: nil, s: "bug", want: false},
		{name: "an empty list holds nothing", list: []string{}, s: "", want: false},
		{name: "an empty text is held only by one", list: []string{"", "bug"}, s: "", want: true},
		{name: "the same text", list: []string{"bug"}, s: "bug", want: true},
		{name: "ignoring case", list: []string{"a11y", "Needs-Review"}, s: "needs-review", want: true},
		{name: "a commit in upper case", list: []string{strings.ToUpper(commit)}, s: commit, want: true},
		{name: "no other text", list: []string{"bug", "docs"}, s: "bugs", want: false},
		{name: "nothing trimmed", list: []string{" bug"}, s: "bug", want: false},
		{name: "the Kelvin sign is a k", list: []string{kelvin}, s: "k", want: true},
		{name: "a K is the Kelvin sign", list: []string{"K"}, s: kelvin, want: true},
		{name: "a capital sharp s is a sharp s", list: []string{capitalSzet}, s: sharpS, want: true},
		{name: "a sharp s is no ss", list: []string{sharpS}, s: "SS", want: false},
		{name: "a dotted I is no i", list: []string{dottedI}, s: "i", want: false},
		{name: "an i is no dotted I", list: []string{"i", "I"}, s: dottedI, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, containsAny(tt.list, tt.s))
		})
	}
}
