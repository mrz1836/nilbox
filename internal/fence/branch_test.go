package fence_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/gitread"
)

// TestPlainBranchTakesWhatGitreadTakes checks that a plain branch is a plain name that
// gitread would hand git (gitread.ValidBranch), whose rules fence keeps its own copy of.
func TestPlainBranchTakesWhatGitreadTakes(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		plain bool
	}{
		{name: "main", plain: true},
		{name: "feature/x", plain: true},
		{name: "dependabot/go_modules/golang.org/x/net-0.33.0", plain: true},
		{name: "release-v1.2.3", plain: true},
		{name: "a.b/c.d", plain: true},
		{name: "a@b", plain: true},
		{name: "@a", plain: true},
		{name: "a@", plain: true},
		{name: "a+b,c%d=e_f", plain: true},
		{name: "a.locked", plain: true},
		{name: strings.Repeat("a", 255), plain: true},
		{name: strings.Repeat("ab/", 84) + "abc", plain: true},

		// Refused by git's own rules for a branch name.
		{name: ""},
		{name: "@"},
		{name: "-x"},
		{name: "--force"},
		{name: "a..b"},
		{name: "a@{1}"},
		{name: "@{-1}"},
		{name: "a//b"},
		{name: "/a"},
		{name: "a/"},
		{name: "a."},
		{name: "a.lock"},
		{name: "a.lock/b"},
		{name: "a./b"},
		{name: ".hidden"},
		{name: "x/.hidden"},
		{name: "x/./y"},
		{name: "."},
		{name: "a/.lock"},
		{name: "a:b"},
		{name: "a~1"},
		{name: "a^"},
		{name: "a?"},
		{name: "a*"},
		{name: "a[b"},
		{name: `a\b`},
		{name: "a b"},
		{name: "a\tb"},
		{name: "a\nb"},
		{name: "a\x00b"},
		{name: "a\x7fb"},
		{name: strings.Repeat("a", 256)},
		{name: strings.Repeat("ab/", 85) + "a"},

		// Names git takes that a shell doesn't read as themselves.
		{name: "=a"},
		{name: "a;b"},
		{name: "a$b"},
		{name: "a|b"},
		{name: "a&b"},
		{name: "a'b"},
		{name: `a"b`},
		{name: "a(b)"},
		{name: "a{b}"},
		{name: "a<b"},
		{name: "a#b"},
		{name: "a!b"},
		{name: "café"},
	} {
		assert.Equal(t, tc.plain, fence.PlainBranch(tc.name), "%q", tc.name)
		assert.Equal(t, fence.PlainName(tc.name) && gitread.ValidBranch(tc.name), fence.PlainBranch(tc.name),
			"%q: a plain branch is a plain name gitread takes", tc.name)
	}
}
