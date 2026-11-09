package repoguard_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fixedTextSize matches a text size or line height fixed in px in a frontend source: a
// Tailwind text-[11px] or leading-[18px], or a CSS font-size or line-height in px. The
// text size setting scales the root's font size, which only rem lengths follow.
var fixedTextSize = regexp.MustCompile(`\b(?:text|leading)-\[[0-9.]+px\]|\b(?:font-size|line-height)\s*:\s*[0-9.]+px`)

func TestFixedTextSizePattern(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		`className="text-[11px] font-medium"`:           true,
		`className="px-1.5 leading-[18px]"`:             true,
		`className="hover:text-[12.5px]"`:               true,
		`  font-size: 13px;`:                            true,
		`  line-height:18px;`:                           true,
		`className="text-[0.6875rem] leading-4"`:        false,
		`className="text-xs leading-[1.125rem]"`:        false,
		`className="h-[52px] pl-[80px] border-[1.5px]"`: false,
		`className="text-[var(--color-muted)]"`:         false,
		`  font-size: 0.8125rem;`:                       false,
		`  outline-offset: 2px;`:                        false,
	}

	for line, want := range cases {
		assert.Equal(t, want, fixedTextSize.MatchString(line), line)
	}
}

func TestFrontendTextFollowsTheTextSize(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)

	var found []string

	for _, rel := range repoFiles(t, root) {
		if !frontendBrandFile(rel) {
			continue
		}

		text, ok := readText(t, root, rel)
		if !ok {
			continue
		}

		found = append(found, hits(rel, text, fixedTextSize.MatchString)...)
	}

	assert.Empty(t, found, "size text in rem (text-[0.6875rem], not text-[11px]), so it follows the text size setting")
}
