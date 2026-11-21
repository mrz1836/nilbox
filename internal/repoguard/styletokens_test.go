package repoguard_test

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The window's colors, text sizes, and shadows come from the theme's tokens: the --color-*
// colors internal/brandgen writes from the brand palette, and the type scale, shadows,
// scrim, paper, washes, disabled fade, and popover layer that frontend/src/index.css
// declares in @theme. The generated theme resets Tailwind's own palette (--color-*:
// initial), so a palette utility such as text-white or bg-red-500 builds to nothing, and
// says so nowhere; TestFrontendStylesComeFromTokens fails on one first, and on the other
// ways around the tokens.
//
// It reads each line on its own, so it misses what only a line's context makes a color: a
// literal kept in a variable or a constant first and styled from there (const ink = '#fff',
// then style={{ color: ink }}), a style property named at run time ({ [key]: '#fff' }), a
// key that goes on past the style it names (colorDark: '#fff', see styleKey), a color put
// together in a template (`#${hex}`), and the CSS color keywords other than white and black
// (red, navy, ...), which no line of the window has ever needed.

// paletteColor matches a color of Tailwind's own palette as a class or a variable names
// it: white, black, or a hue at one of its steps (red-500, slate-50, stone-950).
const paletteColor = `(?:white|black|(?:slate|gray|zinc|neutral|stone|mauve|olive|mist|taupe|red|orange|amber|` +
	`yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-(?:50|[1-9]00|950))`

// colorLiteral matches a color written out: a hex color or a CSS color function. An
// underscore, a class's space, ends a word, so rgb( in 0_1px_rgb(...) is one too.
const colorLiteral = `(?:#[0-9A-Fa-f]{3,8}(?:$|[^0-9A-Za-z])|(?:^|[^0-9A-Za-z])(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\()`

// namedColor matches white or black as a CSS color keyword, never inside a longer name
// such as white-space.
const namedColor = `(?:^|[^\w-])(?:white|black)(?:$|[^\w-])`

// classStart is where a class or a variant starts: a line's start, a space, a quote, a
// backtick, or the colon after a variant, and the ! that marks a class important.
const classStart = "(?:^|[\\s'\"`:{(,!])"

// styleKey matches a style property that takes a color, as a style object or an SVG
// attribute names it: a color, fill, or stroke, a background, border, outline, or mask, a
// shadow, a filter, or an image, after any prefix (backgroundColor, stopColor, boxShadow,
// maskImage), and a border's sides (borderTop, borderInlineStart). The name ends the key,
// so a key that only starts with one, such as filterHint or backgroundRun, is no style's,
// and an issue's #1234 it holds reads as no color.
const styleKey = `\b[A-Za-z]*(?:[Cc]olor|[Bb]ackground|[Bb]order(?:Top|Right|Bottom|Left|Block|Inline|Start|End)*|` +
	`[Oo]utline|[Ss]hadow|[Ff]ill|[Ss]troke|[Ff]ilter|[Mm]ask|[Ii]mage)`

// disabledVariant matches a variant that styles a disabled control: disabled itself, one
// built on it (has-disabled, aria-disabled, group-disabled, peer-disabled), also for a
// named group (group-disabled/row), and an arbitrary one that names it ([&:disabled],
// data-[disabled]), even [&:not(:disabled)], which not-disabled says instead.
const disabledVariant = "(?:(?:group|peer|in|has|aria|data)-)*" +
	"(?:disabled|\\[[^\\s'\"`]*?disabled[^\\s'\"`]*?\\])(?:/[\\w-]+)?"

// styleRule is one way around the tokens the guard refuses: what a hit says, whether it
// is read in stylesheets, in scripts, or both, and the pattern of a line that breaks it.
// With within set, a line breaks the rule only when within matches what the pattern's
// first group captured, in one of its matches: the value of a class or a style.
type styleRule struct {
	says        string
	css, script bool
	pattern     *regexp.Regexp
	within      *regexp.Regexp
}

// breaks reports whether line breaks the rule.
func (r styleRule) breaks(line string) bool {
	if r.within == nil {
		return r.pattern.MatchString(line)
	}

	for _, m := range r.pattern.FindAllStringSubmatch(line, -1) {
		if r.within.MatchString(m[1]) {
			return true
		}
	}

	return false
}

// styleRules are the rules TestFrontendStylesComeFromTokens holds every line to.
func styleRules() []styleRule {
	literal := regexp.MustCompile(colorLiteral)

	return []styleRule{
		{
			says: "a Tailwind palette color, which the theme resets to nothing; use a --color-* token", css: true, script: true,
			pattern: regexp.MustCompile(classStart + `(?:bg|text|border(?:-[xytrblse])?|divide|outline|ring|ring-offset|` +
				`inset-ring|shadow|inset-shadow|drop-shadow|text-shadow|fill|stroke|from|via|to|decoration|placeholder|` +
				`caret|accent)-` + paletteColor + `(?:$|[^\w-])`),
		},
		{
			says: "a Tailwind palette variable, which the theme resets to nothing; read a --color-* token", css: true, script: true,
			pattern: regexp.MustCompile(`--color-` + paletteColor + `(?:$|[^\w-])`),
		},
		{
			// An arbitrary value (bg-[...]) or an arbitrary property ([color:...]).
			says: "a color written into a class; use a --color-* token", css: true, script: true,
			pattern: regexp.MustCompile("(?:-\\[|" + classStart + "\\[[a-z-]+:)([^\\]\\s'\"`]*)"),
			within:  literal,
		},
		{
			// A style's color property, quoted or not, a custom property a style object sets
			// ('--glow'), or an SVG's color attribute, given a string.
			says: "a color written into a style; read a --color-* token", script: true,
			pattern: regexp.MustCompile(`(?:` + styleKey + `['"]?|['"]--[\w-]+['"])\s*[:=]\s*\{?\s*['"` +
				"`]([^'\"`]*)"),
			within: regexp.MustCompile(colorLiteral + `|` + namedColor),
		},
		{
			says: "a color written into the stylesheet; declare it once as a token in @theme", css: true,
			pattern: regexp.MustCompile(colorLiteral + `|` + namedColor),
		},
		{
			says: "a text size of its own; size it from the type scale (text-caption, text-body, ...)", css: true, script: true,
			pattern: regexp.MustCompile(classStart + `text-\[(?:length:)?[0-9.]+[a-z%]*\]`),
		},
		{
			says: "a dialog backdrop of its own; use backdrop:bg-scrim", css: true, script: true,
			pattern: regexp.MustCompile(classStart + `backdrop:(?:[\w-]+:)*!?bg-\[`),
		},
		{
			says: "a disabled fade of its own; fade it with opacity-disabled", css: true, script: true,
			pattern: regexp.MustCompile(classStart + disabledVariant + `:(?:[\w-]+:|\[[^\]\s]*\]:)*!?opacity-(?:\d|\[)`),
		},
	}
}

// styleException is a line the guard lets through, and why: a color no token can stand
// for. line matches the whole of the line's text, trimmed, so it lets through only the
// shape it is for, and every exception must still match a line.
type styleException struct {
	file string
	line *regexp.Regexp
	why  string
}

// styleExceptions are the only lines that write a color out, each with its reason.
func styleExceptions() []styleException {
	return []styleException{
		{
			file: "frontend/src/index.css",
			line: regexp.MustCompile(`^background-image: url\("data:image/svg\+xml,[^"]*stroke='white'[^"]*"\);$`),
			why: "a checkbox's check and dash are data-URL SVGs, which can't read a variable; they are white on the " +
				"accent fill, as the paper knob of a switch is",
		},
		{
			file: "frontend/src/index.css", line: regexp.MustCompile(`^box-shadow: 0 1px 2px rgb\(0 0 0 / 0\.3\);$`),
			why: "the slider thumb's lift, a shadow on a -webkit- pseudo-element that no shadow utility reaches",
		},
		{
			file: "frontend/src/nil.css",
			line: regexp.MustCompile(`^mask-image: radial-gradient\([^#;]*, #000 [^#;]*\);$`),
			why:  "the inbox-nil grid's masks: a mask reads only alpha, so black there means opaque, never a color on screen",
		},
		{
			file: "frontend/src/lib/cardGuide.ts",
			line: regexp.MustCompile(`^(?:labels: \[)?\{ name: '[\w-]+', color: '#[0-9a-f]{6}' \}\]?,$`),
			why: "the card guide's sample labels carry GitHub label colors, data the card draws as GitHub sends a real " +
				"label's, never a theme color",
		},
	}
}

// styleFile reports a frontend source the guard reads: a script or stylesheet under
// frontend/src that the window runs, so neither a test nor the tests' shared support in
// frontend/src/test (whose fixtures carry GitHub's own label colors), nor the generated
// files.
func styleFile(rel string) bool {
	if rel == "frontend/index.html" || !frontendBrandFile(rel) || strings.HasPrefix(rel, "frontend/src/test/") {
		return false
	}

	return !strings.HasSuffix(rel, ".test.ts") && !strings.HasSuffix(rel, ".test.tsx")
}

// quoteEnd returns the index of the quote that closes the string text opens at i, or
// len(text) for one left open.
func quoteEnd(text string, i int) int {
	quote := text[i]

	for j := i + 1; j < len(text); j++ {
		switch text[j] {
		case '\\':
			j++
		case quote:
			return j
		}
	}

	return len(text)
}

// cssComments returns where each comment of a stylesheet's text starts and ends. A quoted
// string is read as it is, so a glob such as './**/*.ts' starts none.
func cssComments(text string) [][2]int {
	var spans [][2]int

	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\'', '"':
			i = quoteEnd(text, i)
		case '/':
			if !strings.HasPrefix(text[i:], "/*") {
				continue
			}

			stop := len(text)
			if end := strings.Index(text[i+2:], "*/"); end >= 0 {
				stop = i + 2 + end + 2
			}

			spans = append(spans, [2]int{i, stop})
			i = stop - 1
		}
	}

	return spans
}

// cssCode returns a stylesheet's text with each comment blanked out and every line kept
// where it is, so a comment's words are never read as a declaration.
func cssCode(text string) string {
	code := []byte(text)

	for _, span := range cssComments(text) {
		for j := span[0]; j < span[1]; j++ {
			if code[j] != '\n' {
				code[j] = ' '
			}
		}
	}

	return string(code)
}

// cssBlocks follows a stylesheet's open blocks as its code is read line by line.
type cssBlocks struct {
	open   []bool          // each open block, innermost last: whether it is an @theme block
	header strings.Builder // what was read since the last {, }, or ;
}

// inTheme reports whether the innermost open block is an @theme block.
func (b *cssBlocks) inTheme() bool {
	return len(b.open) > 0 && b.open[len(b.open)-1]
}

// read reads one line of code, its comments blanked, skipping over quoted strings.
func (b *cssBlocks) read(line string) {
	for i := 0; i < len(line); i++ {
		switch c := line[i]; c {
		case '\'', '"':
			i = quoteEnd(line, i)
		case '{':
			b.open = append(b.open, strings.HasPrefix(strings.TrimSpace(b.header.String()), "@theme"))
			b.header.Reset()
		case '}':
			b.open = b.open[:max(len(b.open)-1, 0)]
			b.header.Reset()
		case ';':
			b.header.Reset()
		default:
			b.header.WriteByte(c)
		}
	}

	b.header.WriteByte('\n')
}

// themeTokenLines returns the numbers (from 1) of the lines of a stylesheet's code, its
// comments blanked, that declare a custom property right inside an @theme block: the
// tokens themselves, the one place a color is written out.
func themeTokenLines(code string) map[int]bool {
	tokens := map[int]bool{}

	var blocks cssBlocks

	for n, line := range strings.Split(code, "\n") {
		if blocks.inTheme() && strings.HasPrefix(strings.TrimSpace(line), "--") {
			tokens[n+1] = true
		}

		blocks.read(line)
	}

	return tokens
}

// styleHit is a line that breaks a rule: where it is, its text, and what the rule says.
type styleHit struct {
	file string
	line int
	text string
	says string
}

func (h styleHit) String() string {
	return fmt.Sprintf("%s:%d: %s (%s)", h.file, h.line, h.text, h.says)
}

// styleHits returns every line of the frontend source rel, whose text is text, that
// breaks one of rules, with the first rule it breaks. A stylesheet is read without its
// comments, and its tokens' own declarations in @theme break none.
func styleHits(rel, text string, rules []styleRule) []styleHit {
	css := path.Ext(rel) == ".css"

	code, tokens := text, map[int]bool{}
	if css {
		code = cssCode(text)
		tokens = themeTokenLines(code)
	}

	lines := strings.Split(text, "\n")

	var found []styleHit

	for i, line := range strings.Split(code, "\n") {
		if tokens[i+1] {
			continue
		}

		if says, ok := firstBroken(line, css, rules); ok {
			found = append(found, styleHit{file: rel, line: i + 1, text: strings.TrimSpace(lines[i]), says: says})
		}
	}

	return found
}

// firstBroken returns what the first of rules that line breaks says, reading line as a
// stylesheet's (css) or a script's, or ok=false for a line that breaks none.
func firstBroken(line string, css bool, rules []styleRule) (says string, ok bool) {
	for _, rule := range rules {
		reads := rule.script
		if css {
			reads = rule.css
		}

		if reads && rule.breaks(line) {
			return rule.says, true
		}
	}

	return "", false
}

// excepted returns the index of the exception that lets hit through, or -1.
func excepted(hit styleHit, exceptions []styleException) int {
	for i, e := range exceptions {
		if e.file == hit.file && e.line.MatchString(hit.text) {
			return i
		}
	}

	return -1
}

// frontendStyleHits returns every line that breaks one of rules in the frontend sources
// styleFile names.
func frontendStyleHits(t *testing.T, rules []styleRule) []styleHit {
	t.Helper()

	var found []styleHit

	for _, rel := range repoFiles(t) {
		if !styleFile(rel) {
			continue
		}

		if text, ok := readText(t, rel); ok {
			found = append(found, styleHits(rel, text, rules)...)
		}
	}

	return found
}

func TestFrontendStylesComeFromTokens(t *testing.T) {
	t.Parallel()

	exceptions := styleExceptions()
	used := make([]bool, len(exceptions))

	var found []string

	for _, hit := range frontendStyleHits(t, styleRules()) {
		if i := excepted(hit, exceptions); i >= 0 {
			used[i] = true

			continue
		}

		found = append(found, hit.String())
	}

	assert.Empty(t, found, "colors, text sizes, backdrops, and disabled fades come from the theme's tokens "+
		"(frontend/src/index.css and the generated theme)")

	for i, e := range exceptions {
		assert.True(t, used[i], "no line of %s matches %s any more; drop its exception", e.file, e.line)
	}
}

func TestStyleRulesPatterns(t *testing.T) {
	t.Parallel()

	script := map[string]bool{
		`className="text-white font-medium"`:                                                  true,
		`className="card-lit:text-white"`:                                                     true,
		"`${base} hover:bg-red-500/20`":                                                       true,
		`'before:bg-white'`:                                                                   true,
		`className="shadow-xl shadow-black/20"`:                                               true,
		`className="border-t-slate-200 ring-offset-white"`:                                    true,
		`className="fill-zinc-950"`:                                                           true,
		`style={{ color: 'var(--color-gray-500)' }}`:                                          true,
		`className="bg-[#fff]"`:                                                               true,
		`className="hover:text-[rgb(0_0_0/0.5)]"`:                                             true,
		`className="[mask-image:linear-gradient(#000,transparent)]"`:                          true,
		`className="inset-shadow-[0_1px_0_rgb(255_255_255/0.06)]"`:                            true,
		`style={{ backgroundColor: '#fff' }}`:                                                 true,
		`style={{ color: 'rgb(0 0 0)' }}`:                                                     true,
		`style={{ boxShadow: '0 1px 2px rgba(0, 0, 0, 0.3)' }}`:                               true,
		`<stop stopColor="white" />`:                                                          true,
		"style={{ stopColor: `color-mix(in oklab, #fff 60%, var(--x))` }}":                    true,
		`{ name: 'bug', color: '#d73a4a' }`:                                                   true,
		`className="text-[0.6875rem] leading-4"`:                                              true,
		`className="hover:text-[13px]"`:                                                       true,
		`className="backdrop:bg-[rgb(0_0_0/0.4)]"`:                                            true,
		`className="disabled:opacity-50"`:                                                     true,
		`className="has-disabled:opacity-[0.55]"`:                                             true,
		`className="!bg-white"`:                                                               true,
		`className="hover:!text-black"`:                                                       true,
		`className="!text-[13px]"`:                                                            true,
		`className="![color:#fff]"`:                                                           true,
		`className="backdrop:!bg-[rgb(0_0_0/0.4)]"`:                                           true,
		`className="disabled:!opacity-50"`:                                                    true,
		`className="disabled:opacity-50!"`:                                                    true,
		`className="group-disabled:opacity-50"`:                                               true,
		`className="peer-disabled:opacity-40"`:                                                true,
		`className="group-disabled/row:opacity-50"`:                                           true,
		`className="[&:disabled]:opacity-50"`:                                                 true,
		`className="data-[disabled]:opacity-[0.4]"`:                                           true,
		`className="group-has-[:disabled]:hover:opacity-30"`:                                  true,
		`style={{ '--x': '#fff' }}`:                                                           true,
		`style={{ "--glow": "rgb(0 0 0 / 0.3)" }}`:                                            true,
		`style={{ 'background-color': 'white' }}`:                                             true,
		`style={{ borderBottom: '1px solid #000' }}`:                                          true,
		`className="text-surface card-lit:text-surface"`:                                      false,
		`className="bg-danger/10 border-warning/45 text-muted"`:                               false,
		`className="bg-transparent text-current border-inherit"`:                              false,
		`className="text-caption shadow-menu backdrop:bg-scrim"`:                              false,
		`className="disabled:opacity-disabled aria-disabled:opacity-disabled"`:                false,
		`className="group-disabled:opacity-disabled [&:disabled]:opacity-disabled"`:           false,
		`className="opacity-60 not-disabled:hover:opacity-100"`:                               false,
		`className="!font-medium disabled:cursor-not-allowed"`:                                false,
		`style={{ '--tint': 'var(--color-accent)', '--delay': '120ms' }}`:                     false,
		`const filterHint = '#1234'`:                                                          false,
		`{ backgroundRun: 'Fix #214', imageAlt: 'PR #1234' }`:                                 false,
		`className="whitespace-nowrap text-xs text-left"`:                                     false,
		`className="max-w-[calc(100vw-2rem)] grid-cols-[minmax(0,1fr)_var(--x)]"`:             false,
		"style={{ stopColor: `color-mix(in oklab, var(--color-accent) 60%, ${highlight})` }}": false,
		`style={label.color === '' ? undefined : { backgroundColor: label.color }}`:           false,
		`<path fill="currentColor" stroke="none" />`:                                          false,
		`// itemRef names a pull request or issue in a few words: "PR #214" or "Issue #88".`:  false,
		`const whiteSpace = 'nowrap'`:                                                         false,
	}

	css := map[string]bool{
		`  background: #fff;`:                       true,
		`  color: white;`:                           true,
		`  box-shadow: 0 1px 2px rgb(0 0 0 / 0.3);`: true,
		`  border-color: var(--color-red-500);`:     true,
		`  @apply text-white;`:                      true,
		`  white-space: nowrap;`:                    false,
		`  color: var(--color-text);`:               false,
		`  background: color-mix(in oklab, var(--color-accent) 18%, transparent);`: false,
	}

	rules := styleRules()

	for line, want := range script {
		assert.Equal(t, want, len(styleHits("frontend/src/a.tsx", line, rules)) > 0, line)
	}

	for line, want := range css {
		assert.Equal(t, want, len(styleHits("frontend/src/a.css", line, rules)) > 0, line)
	}
}

func TestStyleExceptionsLetThroughOnlyTheirShape(t *testing.T) {
	t.Parallel()

	checkbox := `background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' ` +
		`fill='none' stroke='white'%3E%3Cpath d='M4.5 8h7'/%3E%3C/svg%3E");`
	cases := []struct {
		file, text string
		want       bool
	}{
		{"frontend/src/index.css", checkbox, true},
		{"frontend/src/index.css", `color: white; /* as stroke='white' draws */`, false},
		{"frontend/src/index.css", `box-shadow: 0 1px 2px rgb(0 0 0 / 0.3);`, true},
		{"frontend/src/index.css", `box-shadow: 0 1px 2px rgb(0 0 0 / 0.3), 0 0 0 1px #fff;`, false},
		{"frontend/src/nil.css", `mask-image: radial-gradient(ellipse 70% 62% at 50% 26%, #000 20%, transparent 100%);`, true},
		{"frontend/src/nil.css", `mask-image: radial-gradient(ellipse 38% 220% at 0% 50%, #000 10%, #fff 100%);`, false},
		{"frontend/src/nil.css", `background: #000;`, false},
		{"frontend/src/lib/cardGuide.ts", `{ name: 'fork-pr', color: '#6e40c9' },`, true},
		{"frontend/src/lib/cardGuide.ts", `labels: [{ name: 'bug', color: '#d73a4a' }],`, true},
		{"frontend/src/lib/cardGuide.ts", `style={{ color: '#d73a4a' }}`, false},
		{"frontend/src/lib/cardGuide.ts", `{ name: 'bug', color: '#d73a4a', ink: { color: '#fff' } },`, false},
		{"frontend/src/lib/tone.ts", `{ name: 'bug', color: '#d73a4a' },`, false},
	}

	exceptions := styleExceptions()

	for _, c := range cases {
		assert.Equal(t, c.want, excepted(styleHit{file: c.file, text: c.text}, exceptions) >= 0, "%s: %s", c.file, c.text)
	}
}

func TestStylesheetTokensAndComments(t *testing.T) {
	t.Parallel()

	sheet := strings.Join([]string{
		"@source not './**/*.test.{ts,tsx}';",
		"/* A token is written out once: #fff",
		"   and white. */",
		"@theme inline {",
		"  --color-paper: #ffffff;",
		"  @keyframes glow {",
		"    50% {",
		"      --tint: #000;",
		"    }",
		"  }",
		"  --shadow-menu: 0 1px 2px rgb(0 0 0 / 0.2);",
		"}",
		".knob {",
		"  --color-paper: #fff;",
		"  color: white; /* white */",
		"}",
	}, "\n")

	code := cssCode(sheet)
	lines := strings.Split(code, "\n")

	assert.Equal(t, "@source not './**/*.test.{ts,tsx}';", lines[0], "a quoted glob starts no comment")
	assert.Empty(t, strings.TrimSpace(lines[1]+lines[2]), "a comment is blanked")
	assert.Equal(t, "  color: white;", strings.TrimRight(lines[14], " "), "a comment after a declaration is blanked")
	assert.Equal(t, map[int]bool{5: true, 11: true}, themeTokenLines(code),
		"only a custom property right inside @theme is a token")

	hits := styleHits("frontend/src/a.css", sheet, styleRules())
	hit := make([]int, 0, len(hits))

	for _, h := range hits {
		hit = append(hit, h.line)
	}

	assert.Equal(t, []int{8, 14, 15}, hit, "a color outside a token's declaration breaks the stylesheet rule")
}

func TestStyleFile(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"frontend/src/App.tsx":                       true,
		"frontend/src/index.css":                     true,
		"frontend/src/lib/cardGuide.ts":              true,
		"frontend/src/components/App.test.tsx":       false,
		"frontend/src/lib/tone.test.ts":              false,
		"frontend/src/test/fixtures.ts":              false,
		"frontend/src/generated/theme.css":           false,
		"frontend/index.html":                        false,
		"frontend/bindings/github.com/x/y/models.ts": false,
	}

	for rel, want := range cases {
		assert.Equal(t, want, styleFile(rel), rel)
	}
}
