package prompts

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"text/template/parse"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// allowedFuncs are the template functions a prompt may use, in the order help text
// lists them. Go's other built-ins (call, index, slice, html, js, urlquery) are refused.
func allowedFuncs() []string {
	return []string{"and", "or", "not", "eq", "ne", "lt", "le", "gt", "ge", "len", "printf", "print", "println"}
}

// funcHelp lists the functions a prompt may use, for error messages.
const funcHelp = "You can use and, or, not, eq, ne, lt, le, gt, ge, len, and printf."

// parseErrorPattern matches Go's template parse errors: "template: prompt:3: message".
func parseErrorPattern() *regexp.Regexp {
	return regexp.MustCompile(`^template: ` + templateName + `:(\d+): (.*)$`)
}

// execErrorPattern matches Go's template execution errors:
// `template: prompt:3:14: executing "prompt" at <gt .Title 3>: message`, where 14 is the
// 0-based byte offset within line 3.
func execErrorPattern() *regexp.Regexp {
	return regexp.MustCompile(`template: ` + templateName + `:(\d+):(\d+): executing "[^"]*" at <.*?>: (.*)$`)
}

// checker collects the diagnostics for one prompt text.
type checker struct {
	body      string
	appliesTo string
	known     map[string]Variable
	diags     []Diagnostic
}

// newChecker returns a checker for body, a prompt that applies to appliesTo.
func newChecker(body, appliesTo string) *checker {
	known := make(map[string]Variable)
	for _, v := range Variables() {
		known[v.Name] = v
	}

	return &checker{body: body, appliesTo: appliesTo, known: known}
}

// check runs every check and returns the template, or nil when there were errors.
func (c *checker) check() *Template {
	if !c.checkText() {
		return nil
	}

	tmpl, err := newTemplate().Parse(c.body)
	if err != nil {
		c.parseError(err)

		return nil
	}

	c.walkList(tmpl.Root, true)
	c.checkDefinitions(tmpl)
	c.checkStart(tmpl.Root)

	if c.hasErrors() {
		return nil
	}

	t := &Template{tmpl: tmpl}
	c.trySamples(t)

	if c.hasErrors() {
		return nil
	}

	return t
}

// hasErrors reports whether any diagnostic is an error.
func (c *checker) hasErrors() bool {
	return slices.ContainsFunc(c.diags, func(d Diagnostic) bool { return d.Severity == SeverityError })
}

// addWhole records an error with the whole prompt.
func (c *checker) addWhole(message string) {
	c.diags = append(c.diags, Diagnostic{Severity: SeverityError, Message: message})
}

// addAt records a problem with the text from byte offset start, length bytes long.
func (c *checker) addAt(severity string, start, length int, message string) {
	start = min(max(start, 0), len(c.body))
	end := min(start+max(length, 0), len(c.body))
	lineStart := strings.LastIndexByte(c.body[:start], '\n') + 1

	d := Diagnostic{
		Severity: severity,
		Line:     strings.Count(c.body[:start], "\n") + 1,
		Column:   utf16Len(c.body[lineStart:start]) + 1,
		Length:   utf16Len(c.body[start:end]),
		Message:  message,
	}

	if !slices.Contains(c.diags, d) {
		c.diags = append(c.diags, d)
	}
}

// addOnLine records a problem on a 1-based line, placed on the action found there by
// find, or on the line's first action, or on the whole line.
func (c *checker) addOnLine(line int, find func(action) bool, message string) {
	actions := scanActions(c.body)

	for _, pick := range []func(action) bool{find, func(action) bool { return true }} {
		for _, a := range actions {
			if c.lineOf(a.start) == line && pick(a) {
				c.addAt(SeverityError, a.start, a.end-a.start, message)

				return
			}
		}
	}

	start, end := c.lineSpan(line)
	c.addAt(SeverityError, start, end-start, message)
}

// lineOf returns the 1-based line holding byte offset pos.
func (c *checker) lineOf(pos int) int {
	return strings.Count(c.body[:min(pos, len(c.body))], "\n") + 1
}

// lineSpan returns the byte offsets where a 1-based line starts and ends.
func (c *checker) lineSpan(line int) (start, end int) {
	for i := 1; i < line; i++ {
		next := strings.IndexByte(c.body[start:], '\n')
		if next < 0 {
			return len(c.body), len(c.body)
		}

		start += next + 1
	}

	end = len(c.body)
	if next := strings.IndexByte(c.body[start:], '\n'); next >= 0 {
		end = start + next
	}

	return start, end
}

// parseError turns a Go template parse error into a diagnostic that says how to fix it.
func (c *checker) parseError(err error) {
	m := parseErrorPattern().FindStringSubmatch(err.Error())
	if m == nil {
		c.addWhole(sentence(err.Error()))

		return
	}

	line, _ := strconv.Atoi(m[1]) // the pattern only matches digits
	msg := m[2]

	switch msg {
	case "unexpected EOF":
		c.unclosedBlock()
	case "unclosed action":
		c.unclosedAction(line)
	default:
		find, text := explainParseError(msg)
		c.addOnLine(line, find, text)
	}
}

// explainParseError rewrites a parse error message, and says which action on its line
// it is about.
func explainParseError(msg string) (find func(action) bool, text string) {
	keyword := func(kw string) func(action) bool { return func(a action) bool { return a.keyword == kw } }
	anything := func(action) bool { return true }

	switch {
	case strings.HasPrefix(msg, "unexpected {{end}}"):
		return keyword("end"), "This {{end}} has no {{if}} or {{with}} to close."
	case strings.HasPrefix(msg, "unexpected {{else}}"):
		return keyword("else"), "This {{else}} isn't inside an {{if}} or {{with}}."
	case strings.HasPrefix(msg, "missing value for "):
		kw := strings.TrimPrefix(msg, "missing value for ")

		return keyword(kw), fmt.Sprintf("{{%s}} needs something to test, such as {{%s .InWorktree}}.", kw, kw)
	case strings.HasPrefix(msg, "function "):
		name := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(msg, "function "), " not defined"), `"`)

		return anything, fmt.Sprintf("%s isn't a function prompts can use. %s", name, funcHelp)
	case msg == "unterminated quoted string":
		return anything, "A quoted string is missing its closing quote."
	case msg == "unclosed comment":
		return keyword(keywordComment), "This comment is never closed: end it with */}}."
	default:
		return anything, sentence(msg)
	}
}

// unclosedBlock reports the innermost {{if}}, {{with}}, or other block that is never
// closed with {{end}}.
func (c *checker) unclosedBlock() {
	var open []action

	for _, a := range scanActions(c.body) {
		switch {
		case blockKeywords()[a.keyword]:
			open = append(open, a)
		case a.keyword == "end" && len(open) > 0:
			open = open[:len(open)-1]
		}
	}

	if len(open) == 0 {
		c.addWhole("The prompt ends before an {{if}} or {{with}} is closed with {{end}}.")

		return
	}

	a := open[len(open)-1]
	c.addAt(SeverityError, a.start, a.end-a.start,
		fmt.Sprintf("This {{%s}} is never closed: add {{end}} where its text ends.", a.keyword))
}

// unclosedAction reports the {{ on a line that has no }}.
func (c *checker) unclosedAction(line int) {
	for _, a := range scanActions(c.body) {
		if !a.closed {
			c.addAt(SeverityError, a.start, 2, "This {{ is never closed with }}.")

			return
		}
	}

	c.addOnLine(line, func(action) bool { return true }, "An action is never closed with }}.")
}

// execError turns an error from filling in sample data into a diagnostic. hint names the
// sample, as in "for pull requests".
func (c *checker) execError(err error, hint string) {
	switch {
	case errors.Is(err, ErrEmptyPrompt):
		c.addWhole(fmt.Sprintf("The prompt is empty once filled in %s.", hint))

		return
	case errors.Is(err, ErrLeadingDash):
		c.addWhole(fmt.Sprintf(
			`Once filled in %s, the prompt starts with "-", which the agent would read as an option. Start it with a word.`,
			hint,
		))

		return
	case errors.Is(err, ErrPromptTooLong):
		c.addWhole("The prompt is longer than 64 KB once filled in.")

		return
	}

	m := execErrorPattern().FindStringSubmatch(err.Error())
	if m == nil {
		c.addWhole(sentence(err.Error()))

		return
	}

	line, _ := strconv.Atoi(m[1]) // the pattern only matches digits
	col, _ := strconv.Atoi(m[2])
	start, _ := c.lineSpan(line)
	c.addAt(SeverityError, start+col, 0, sentence(m[3]))
}

// trySamples fills the prompt in with sample data for every kind it applies to, each in
// every session it can start in, and once more with the yes/no values flipped and the
// optional text empty, so errors in conditional text show up too.
func (c *checker) trySamples(t *Template) {
	for _, kind := range []string{KindPR, KindIssue} {
		if !AppliesTo(c.appliesTo, kind) {
			continue
		}

		for _, data := range sampleVariants(kind) {
			if _, err := t.Execute(data); err != nil {
				c.execError(err, "for "+plural(kind))
			}
		}
	}
}

// sampleVariants returns the sample data for kind in each session it can start in, both
// as is and with every yes/no flipped and the optional text empty.
func sampleVariants(kind string) []Data {
	flipped := Sample(kind)
	flipped.IsDraft, flipped.HasConflicts, flipped.OutsideContributor = flipped.IsPR, flipped.IsPR, true
	flipped.IsBot, flipped.Author = true, "dependabot"
	flipped.Labels, flipped.Branch, flipped.Checks, flipped.Review, flipped.DefaultBranch = "", "", "", "", ""

	var out []Data

	for _, base := range []Data{Sample(kind), flipped} {
		out = append(out, base.WithSession(false, false), base.WithSession(true, false))

		if kind == KindPR {
			out = append(out, base.WithSession(true, true))
		}
	}

	return out
}

// plural names a kind of item in the plural.
func plural(kind string) string {
	if kind == KindPR {
		return "pull requests"
	}

	return "issues"
}

// sentence capitalizes msg and ends it with a period.
func sentence(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return msg
	}

	r, size := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[size:]

	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}

	return msg
}

// utf16Len counts s in UTF-16 code units.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}

	return n
}

// Placement for the walker's diagnostics: the node a problem is about, as a byte range.
type span struct {
	start, length int
}

// nodeSpan returns the byte range of a field or variable node whose text is text. Go
// places a dotted name such as .Title.Foo.Bar at one of its later parts, so the name is
// found in the body at or before that position.
func nodeSpan(body string, n parse.Node, text string) span {
	pos := int(n.Position())

	for start := min(pos, len(body)-len(text)); start >= max(pos-len(text), 0); start-- {
		if body[start:start+len(text)] == text {
			return span{start: start, length: len(text)}
		}
	}

	return span{start: max(pos, 0), length: len(text)}
}

// blockKeywords are the actions {{end}} closes.
func blockKeywords() map[string]bool {
	return map[string]bool{"if": true, "with": true, "range": true, "define": true, "block": true}
}
