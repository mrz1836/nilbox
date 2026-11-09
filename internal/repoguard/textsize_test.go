package repoguard_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fixedTextSize matches a text size or line height fixed in px in a frontend source: a
// Tailwind text-[11px], text-[length:11px], or leading-[18px]; a CSS font-size or
// line-height in px, or a font shorthand with a px size; or a React style's fontSize as a
// number, which React reads as px, or as a px string, and a lineHeight px string. The text
// size setting scales the root's font size, which only rem lengths follow.
var fixedTextSize = regexp.MustCompile(strings.Join([]string{
	`\b(?:text|leading)-\[(?:length:)?[0-9.]+px\]`,
	`\b(?:font-size|line-height)\s*:\s*[0-9.]+px`,
	`\bfont\s*:[^;{}]*?\b[0-9.]+px`,
	`\bfontSize\s*:\s*(?:[0-9.]+\s*(?:[,}]|$)|['"][0-9.]+px)`,
	`\blineHeight\s*:\s*['"][0-9.]+px`,
}, "|"))

func TestFixedTextSizePattern(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		`className="text-[11px] font-medium"`:                   true,
		`className="px-1.5 leading-[18px]"`:                     true,
		`className="hover:text-[12.5px]"`:                       true,
		`className="text-[length:13px]"`:                        true,
		`  font-size: 13px;`:                                    true,
		`  line-height:18px;`:                                   true,
		`  font: 600 13px/1.4 system-ui;`:                       true,
		`style={{ fontSize: 12 }}`:                              true,
		`style={{ fontSize: 12, color }}`:                       true,
		`style={{ fontSize: '12px' }}`:                          true,
		`style={{ lineHeight: "18px" }}`:                        true,
		`className="text-[0.6875rem] leading-4"`:                false,
		`className="text-xs leading-[1.125rem]"`:                false,
		`className="h-[52px] pl-[80px] border-[1.5px]"`:         false,
		`className="text-[var(--color-muted)]"`:                 false,
		`  font-size: 0.8125rem;`:                               false,
		`  font: inherit;`:                                      false,
		`  outline-offset: 2px;`:                                false,
		`style={{ fontSize: '0.75rem', lineHeight: 1.5 }}`:      false,
		"{ color: p.color, fontSize: `${String(p.height)}px` }": false,
		`const mirrored = ['fontSize', 'lineHeight'] as const`:  false,
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
