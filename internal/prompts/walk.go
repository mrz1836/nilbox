package prompts

import (
	"fmt"
	"slices"
	"strings"
	"text/template"
	"text/template/parse"
	"unicode"
)

// keywordComment is the keyword scanActions gives a {{/* comment */}}.
const keywordComment = "comment"

// action is one {{...}} in a prompt's text, found by scanActions to place diagnostics.
type action struct {
	// start is the byte offset of "{{", end the offset just past "}}" (or the end of
	// the text when the action is never closed).
	start, end int

	// keyword is the action's first word, such as "if", "else", or "end", "comment" for
	// a comment, or "" for an action that prints a value.
	keyword string

	// closed reports whether the action ends with "}}".
	closed bool
}

// scanActions finds every {{...}} in body, in order. It only places diagnostics, so it
// looks no further than the braces: a "}}" inside a quoted string ends the action early.
func scanActions(body string) []action {
	var actions []action

	for pos := 0; pos < len(body); {
		i := strings.Index(body[pos:], "{{")
		if i < 0 {
			break
		}

		a := scanAction(body, pos+i)
		actions = append(actions, a)
		pos = a.end
	}

	return actions
}

// scanAction reads the action that starts at body[start:].
func scanAction(body string, start int) action {
	inner := strings.TrimLeft(strings.TrimPrefix(body[start+2:], "-"), " \t\r\n")
	from := start + 2

	if strings.HasPrefix(inner, "/*") {
		closeAt := strings.Index(body[from:], "*/")
		if closeAt < 0 {
			return action{start: start, end: len(body), keyword: keywordComment}
		}

		from += closeAt + 2
	}

	a := action{start: start, end: len(body), keyword: firstWord(inner)}

	if strings.HasPrefix(inner, "/*") {
		a.keyword = keywordComment
	}

	if j := strings.Index(body[from:], "}}"); j >= 0 {
		a.end, a.closed = from+j+2, true
	}

	return a
}

// firstWord returns the keyword an action starts with, or "" when it starts with
// anything else, such as a variable.
func firstWord(inner string) string {
	end := strings.IndexFunc(inner, func(r rune) bool { return !unicode.IsLetter(r) })
	if end < 0 {
		end = len(inner)
	}

	word := inner[:end]

	switch word {
	case "if", "else", "end", "with", "range", "define", "block", "template", "break", "continue":
		return word
	default:
		return ""
	}
}

// walkList checks every node of a list. dotIsData reports whether the dot is the prompt's
// data there, as it is everywhere but inside {{with}}.
func (c *checker) walkList(list *parse.ListNode, dotIsData bool) {
	if list == nil {
		return
	}

	for _, n := range list.Nodes {
		c.walkNode(n, dotIsData)
	}
}

// walkNode checks one node: the values an action uses, and the actions a prompt may not
// use at all.
func (c *checker) walkNode(n parse.Node, dotIsData bool) {
	switch n := n.(type) {
	case *parse.ActionNode:
		c.walkPipe(n.Pipe, dotIsData)
	case *parse.IfNode:
		c.walkBranch(&n.BranchNode, dotIsData, dotIsData)
	case *parse.WithNode:
		c.walkBranch(&n.BranchNode, dotIsData, false)
	case *parse.RangeNode:
		c.refuseAction(n, "range", "{{range}} isn't available in prompts: there are no lists to go through.")
	case *parse.TemplateNode:
		c.refuseAction(n, "template", "{{template}} isn't available in prompts.")
	}
}

// walkBranch checks an {{if}} or {{with}}: its test, its text (where the dot is the data
// when bodyDotIsData), and its {{else}} text.
func (c *checker) walkBranch(b *parse.BranchNode, dotIsData, bodyDotIsData bool) {
	c.walkPipe(b.Pipe, dotIsData)
	c.walkList(b.List, bodyDotIsData)
	c.walkList(b.ElseList, dotIsData)
}

// walkPipe checks every argument of every command in a pipeline.
func (c *checker) walkPipe(p *parse.PipeNode, dotIsData bool) {
	if p == nil {
		return
	}

	for _, cmd := range p.Cmds {
		for _, arg := range cmd.Args {
			c.walkArg(arg, dotIsData)
		}
	}
}

// walkArg checks one argument: a variable must exist, a function must be one prompts
// can use, and the dot alone is no variable.
func (c *checker) walkArg(arg parse.Node, dotIsData bool) {
	switch a := arg.(type) {
	case *parse.FieldNode:
		c.checkField(nodeSpan(c.body, a, a.String()), a.Ident, dotIsData)
	case *parse.VariableNode:
		if len(a.Ident) > 1 && a.Ident[0] == "$" {
			c.checkField(nodeSpan(c.body, a, a.String()), a.Ident[1:], true)
		}
	case *parse.PipeNode:
		c.walkPipe(a, dotIsData)
	default:
		c.checkOperand(arg, dotIsData)
	}
}

// checkOperand checks a function name, a chained field, or the dot.
func (c *checker) checkOperand(arg parse.Node, dotIsData bool) {
	switch a := arg.(type) {
	case *parse.IdentifierNode:
		if !slices.Contains(allowedFuncs(), a.Ident) {
			c.addAt(SeverityError, int(a.Position()), len(a.Ident),
				fmt.Sprintf("%s isn't available in prompts. %s", a.Ident, funcHelp))
		}
	case *parse.ChainNode:
		c.addAt(SeverityError, int(a.Position()), len("."+strings.Join(a.Field, ".")),
			"Write a variable as .Name, such as .Title.")
	case *parse.DotNode:
		if dotIsData {
			c.addAt(SeverityError, int(a.Position()), 1, "{{.}} isn't a variable. Name one, such as {{.Title}}.")
		}
	}
}

// checkField checks a variable reference: it must name one of Data's fields, with
// nothing after it, and outside a {{with}} unless written $.Name. A pull-request-only
// variable in an issue-only prompt is a warning, since it is always empty there.
func (c *checker) checkField(at span, ident []string, dotIsData bool) {
	name := ident[0]

	switch {
	case !dotIsData:
		c.addAt(SeverityError, at.start, at.length, fmt.Sprintf(
			"Inside {{with}}, the dot is the value being tested. Use {{.}} for it, or $.%s for the variable.", name,
		))
	case !c.isKnown(name):
		c.addAt(SeverityError, at.start, at.length, c.unknownMessage(name))
	case len(ident) > 1:
		c.addAt(SeverityError, at.start, at.length, fmt.Sprintf(".%s has no parts to pick: write .%s on its own.", name, name))
	case c.appliesTo == AppliesToIssue && c.known[name].PROnly:
		c.addAt(SeverityWarning, at.start, at.length, fmt.Sprintf(
			".%s is always empty here: it's only set for pull requests, and this prompt is for issues.", name,
		))
	case c.reply && sessionOnly(name):
		c.addAt(SeverityWarning, at.start, at.length, fmt.Sprintf(
			".%s means nothing in a reply: it describes an agent's session, and a reply starts none.", name,
		))
	}
}

// sessionOnly reports a variable that describes an agent's session, and so means nothing
// in a review reply.
func sessionOnly(name string) bool {
	switch name {
	case "InWorktree", "WorktreeOnPRHead", "Worktree", "Profile":
		return true
	default:
		return false
	}
}

// isKnown reports whether name is one of Data's fields.
func (c *checker) isKnown(name string) bool {
	_, ok := c.known[name]

	return ok
}

// unknownMessage says a name is no variable, suggesting the one it most likely meant.
func (c *checker) unknownMessage(name string) string {
	if guess := c.suggest(name); guess != "" {
		return fmt.Sprintf(".%s isn't a variable. Did you mean .%s?", name, guess)
	}

	return fmt.Sprintf(".%s isn't a variable. See the variables list for the ones you can use.", name)
}

// suggest returns the variable name most likely meant by name: the same name in any
// case, else the closest within two edits (or a third of its length, for longer names).
func (c *checker) suggest(name string) string {
	best, bestDist := "", max(2, len(name)/3)+1

	for _, v := range Variables() {
		if strings.EqualFold(v.Name, name) {
			return v.Name
		}

		if d := editDistance(strings.ToLower(v.Name), strings.ToLower(name)); d < bestDist {
			best, bestDist = v.Name, d
		}
	}

	return best
}

// refuseAction reports an action a prompt may not use, placed on its {{...}}.
func (c *checker) refuseAction(n parse.Node, keyword, message string) {
	a := c.actionAround(int(n.Position()))
	if a.end == a.start {
		a.end = a.start + len(keyword)
	}

	c.addAt(SeverityError, a.start, a.end-a.start, message)
}

// actionAround returns the action holding byte offset pos.
func (c *checker) actionAround(pos int) action {
	for _, a := range scanActions(c.body) {
		if a.start <= pos && pos < a.end {
			return a
		}
	}

	return action{start: pos, end: pos}
}

// checkDefinitions refuses {{define}} and {{block}}, which add templates beside the
// prompt's own.
func (c *checker) checkDefinitions(tmpl *template.Template) {
	if len(tmpl.Templates()) <= 1 {
		return
	}

	for _, a := range scanActions(c.body) {
		if a.keyword == "define" || a.keyword == "block" {
			c.addAt(SeverityError, a.start, a.end-a.start, "{{define}} and {{block}} aren't available in prompts.")

			return
		}
	}

	c.addWhole("{{define}} and {{block}} aren't available in prompts.")
}

// checkStart looks at how the prompt begins. The prompt is the agent's first argument,
// so text that starts with "-" would be read as an option: that is an error, and a value
// printed first is a warning, since the launch is refused whenever it starts with "-".
// Tidying drops blank lines at the start but keeps a line's leading spaces, so only the
// first character of the first line with content counts.
func (c *checker) checkStart(root *parse.ListNode) {
	switch n := firstOutput(root).(type) {
	case *parse.TextNode:
		text := string(n.Text)
		rest := dropBlankLines(text)

		if strings.HasPrefix(rest, "-") {
			c.addAt(SeverityError, int(n.Position())+len(text)-len(rest), 1,
				`Start the prompt with a word: the agent reads a prompt that starts with "-" as an option.`)
		}
	case *parse.ActionNode:
		if len(n.Pipe.Decl) == 0 {
			a := c.actionAround(int(n.Position()))
			c.addAt(SeverityWarning, a.start, a.end-a.start,
				`Start the prompt with a word. If this value ever began with "-", the agent would read the `+
					`prompt as an option, so that launch would be refused.`)
		}
	}
}

// dropBlankLines removes the lines at the start of s that hold only whitespace, newline
// included, the way tidying drops them from a filled-in prompt.
func dropBlankLines(s string) string {
	for {
		i := strings.IndexByte(s, '\n')
		if i < 0 || strings.TrimSpace(s[:i]) != "" {
			return s
		}

		s = s[i+1:]
	}
}

// firstOutput returns the first node that can start the prompt: comments and text made
// only of blank lines are skipped, since tidying drops leading blank lines. Text that
// leaves spaces on the first line counts, since those spaces start the prompt. It is nil
// for a prompt with nothing else.
func firstOutput(root *parse.ListNode) parse.Node {
	for _, n := range root.Nodes {
		switch n := n.(type) {
		case *parse.CommentNode:
			continue
		case *parse.TextNode:
			if dropBlankLines(string(n.Text)) == "" {
				continue
			}
		}

		return n
	}

	return nil
}

// editDistance is the Levenshtein distance between a and b, counted in bytes (variable
// names are ASCII).
func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i

		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}

			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}

		prev = cur
	}

	return prev[len(b)]
}
