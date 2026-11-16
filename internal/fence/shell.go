package fence

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The fence reads a Bash call's command line as a POSIX shell (bash, zsh) would split it,
// closely enough to find every command it runs: each simple command, with its variable
// assignments, its words with their quotes removed, and its redirections, including the
// commands inside a command substitution, a process substitution, a group, a function's
// body, or an unquoted here-document. A word holding anything the shell expands at run
// time (a variable, a substitution, a glob, or braces) is marked dynamic, since what it
// becomes can't be known from the line. A line it can't read is refused, never guessed.

// maxShellDepth bounds the nesting of substitutions the reader follows; a line nested
// deeper is refused.
const maxShellDepth = 8

// errShell is a command line the fence can't read.
var errShell = errors.New("the fence can't read this command line")

// word is one word of a command line.
type word struct {
	// text is the word with its quotes removed and its escapes applied; an expansion is
	// kept as it was written.
	text string

	// dynamic reports a word the shell changes at run time: one holding a variable, a
	// substitution, an unquoted glob, or brace expansion.
	dynamic bool

	// quoted reports a word with a quote or a backslash in it.
	quoted bool

	// assignWord reports a word that sets a variable: a name, then an unquoted =.
	assignWord bool
}

// redirect is one redirection of a simple command.
type redirect struct {
	op     string
	target word
}

// writes reports a redirection that writes its target.
func (r redirect) writes() bool {
	return strings.Contains(r.op, ">")
}

// simple is one simple command: its leading variable assignments, its words, the program
// first, and its redirections.
type simple struct {
	assigns   []word
	words     []word
	redirects []redirect
}

// script is a command line as the shell reads it.
type script struct {
	commands []simple

	// compound reports a line that is more than one plain command: separators, pipes,
	// groups, redirections, substitutions, or here-documents.
	compound bool
}

// heredoc is a here-document whose body follows the next newline.
type heredoc struct {
	delim  string
	strip  bool
	quoted bool
}

// lexer reads one command line.
type lexer struct {
	src      []rune
	i        int
	depth    int
	out      script
	cur      simple
	pending  string // a redirection waiting for its target
	heredocs []heredoc
	skipList bool // the words of a for, select, or case header, which are data
}

// readShell reads a command line.
func readShell(line string) (script, error) {
	return readShellAt(line, 0)
}

// readShellAt reads a command line nested depth substitutions deep.
func readShellAt(line string, depth int) (script, error) {
	if depth > maxShellDepth {
		return script{}, errShell
	}

	if !utf8.ValidString(line) {
		return script{}, errShell
	}

	l := &lexer{src: []rune(line), depth: depth}
	if err := l.run(); err != nil {
		return script{}, err
	}

	return l.out, nil
}

// run reads the whole line.
func (l *lexer) run() error {
	for l.i < len(l.src) {
		if err := l.step(); err != nil {
			return err
		}
	}

	if l.pending != "" {
		return errShell
	}

	l.end()

	// A here-document with no line after it reads to the end, as the shell does.
	return l.readHeredocs()
}

// step reads what starts at the current rune: a blank, a line's end, a comment, a
// redirection, an operator, or a word.
func (l *lexer) step() error {
	c := l.src[l.i]

	switch {
	case isBlank(c):
		l.i++
	case c == '\\' && l.peek(1) == '\n':
		l.i += 2
	case c == '\n':
		return l.newline()
	case c == '#':
		l.skipComment()
	case l.atRedirect():
		return l.redirectOp()
	case isOperator(c):
		l.operator()
	default:
		return l.word()
	}

	return nil
}

// newline ends the command and reads the here-documents its line opened.
func (l *lexer) newline() error {
	l.i++
	l.end()

	return l.readHeredocs()
}

// atRedirect reports a redirection at the current rune: <, >, or &>.
func (l *lexer) atRedirect() bool {
	c := l.src[l.i]

	return c == '<' || c == '>' || c == '&' && l.peek(1) == '>'
}

// word reads a word and files it, but for a redirection's file descriptor, as 2 in 2>&1.
func (l *lexer) word() error {
	w, err := l.readWord()
	if err != nil {
		return err
	}

	if !l.fdNumber(w) {
		l.add(w)
	}

	return nil
}

// isBlank reports a blank between words.
func isBlank(c rune) bool {
	return c == ' ' || c == '\t' || c == '\r'
}

// isOperator reports a separator, a pipe, or a parenthesis.
func isOperator(c rune) bool {
	return strings.ContainsRune(";&|()", c)
}

// wordEnd reports a rune that ends an unquoted word.
func wordEnd(c rune) bool {
	return isBlank(c) || c == '\n' || isOperator(c) || c == '<' || c == '>'
}

// fdNumber reports a word of digits right before a redirection: its file descriptor.
func (l *lexer) fdNumber(w word) bool {
	c := l.peek(0)

	return (c == '<' || c == '>') && !w.quoted && !w.dynamic && digits(w.text)
}

// peek returns the rune n past the current one, or 0.
func (l *lexer) peek(n int) rune {
	if l.i+n < len(l.src) {
		return l.src[l.i+n]
	}

	return 0
}

// skipComment skips a comment to the end of its line.
func (l *lexer) skipComment() {
	for l.i < len(l.src) && l.src[l.i] != '\n' {
		l.i++
	}
}

// operator reads a separator, a pipe, or a parenthesis, which ends the command.
func (l *lexer) operator() {
	c := l.src[l.i]
	l.i++

	if (c == '&' || c == '|' || c == ';') && l.i < len(l.src) && (l.src[l.i] == c || c == '|' && l.src[l.i] == '&') {
		l.i++
	}

	l.end()
	l.out.compound = true
}

// redirectOp reads a redirection's operator; its target is the next word. A process
// substitution, <(…) or >(…), is read as the commands it runs.
func (l *lexer) redirectOp() error {
	if l.pending != "" {
		return errShell
	}

	l.out.compound = true

	if (l.src[l.i] == '<' || l.src[l.i] == '>') && l.peek(1) == '(' {
		l.i += 2

		inner, err := l.readParen()
		if err != nil {
			return err
		}

		return l.nested(inner)
	}

	start := l.i

	for l.i < len(l.src) && strings.ContainsRune("<>&|-", l.src[l.i]) && l.i-start < 3 {
		l.i++
	}

	l.pending = string(l.src[start:l.i])

	return nil
}

// add files a word under the current command: a redirection's target, a variable
// assignment before the program, a reserved word, or the next word.
func (l *lexer) add(w word) {
	switch {
	case l.pending != "":
		l.target(w)
	case l.skipList:
		// The variable and list of a for, select, or case header are data.
	case len(l.cur.words) == 0 && w.assign():
		l.cur.assigns = append(l.cur.assigns, w)
	case len(l.cur.words) == 0 && reservedWord(w):
		l.out.compound = true

		if listWord(w) {
			l.skipList = true
		}
	default:
		l.cur.words = append(l.cur.words, w)
	}
}

// target files a redirection's target, and for a here-document, its delimiter.
func (l *lexer) target(w word) {
	op := l.pending
	l.pending = ""

	if strings.HasPrefix(op, "<<") && op != "<<<" {
		l.heredocs = append(l.heredocs, heredoc{
			delim: w.text, strip: strings.HasSuffix(op, "-"), quoted: w.dynamic || w.quotedDelim(),
		})

		return
	}

	l.cur.redirects = append(l.cur.redirects, redirect{op: op, target: w})
}

// end ends the current command.
func (l *lexer) end() {
	if len(l.cur.words) > 0 || len(l.cur.assigns) > 0 || len(l.cur.redirects) > 0 {
		l.out.commands = append(l.out.commands, l.cur)
	}

	l.cur, l.skipList = simple{}, false
}

// nested reads the commands of a substitution and adds them to the line's.
func (l *lexer) nested(inner string) error {
	s, err := readShellAt(inner, l.depth+1)
	if err != nil {
		return err
	}

	l.out.commands = append(l.out.commands, s.commands...)
	l.out.compound = true

	return nil
}

// readHeredocs reads the bodies of the here-documents the last line opened. A body whose
// delimiter is unquoted is expanded by the shell, so the commands of its substitutions are
// read; any other body is data.
func (l *lexer) readHeredocs() error {
	docs := l.heredocs
	l.heredocs = nil

	for _, doc := range docs {
		l.out.compound = true

		body := l.heredocBody(doc)

		if doc.quoted {
			continue
		}

		if err := l.expandBody(body); err != nil {
			return err
		}
	}

	return nil
}

// heredocBody reads lines up to the one that is the delimiter, and returns them.
func (l *lexer) heredocBody(doc heredoc) string {
	var body strings.Builder

	for l.i < len(l.src) {
		end := l.i
		for end < len(l.src) && l.src[end] != '\n' {
			end++
		}

		line := string(l.src[l.i:end])
		l.i = min(end+1, len(l.src))

		check := line
		if doc.strip {
			check = strings.TrimLeft(line, "\t")
		}

		if check == doc.delim {
			break
		}

		body.WriteString(line)
		body.WriteByte('\n')
	}

	return body.String()
}

// expandBody reads the commands of every substitution in an expanded here-document.
func (l *lexer) expandBody(body string) error {
	sub := &lexer{src: []rune(body), depth: l.depth}

	for sub.i < len(sub.src) {
		c := sub.src[sub.i]

		switch {
		case c == '\\':
			sub.i += 2
		case c == '$' && sub.peek(1) == '(' || c == '`':
			if err := sub.readDollar(); err != nil {
				return err
			}
		default:
			sub.i++
		}
	}

	l.out.commands = append(l.out.commands, sub.out.commands...)

	return nil
}

// wordState is what readWord has gathered of one word.
type wordState struct {
	text    strings.Builder
	dynamic bool

	// plain reports only unquoted literal characters so far, and assign that the word is
	// an assignment: a name then an unquoted =. quoted reports a quote or a backslash.
	plain  bool
	assign bool
	quoted bool
}

// readWord reads one word, up to an unquoted blank or operator.
func (l *lexer) readWord() (word, error) {
	st := &wordState{plain: true}

	for l.i < len(l.src) && !wordEnd(l.src[l.i]) {
		if err := l.wordRune(st, l.src[l.i]); err != nil {
			return word{}, err
		}
	}

	return word{text: st.text.String(), dynamic: st.dynamic, quoted: st.quoted, assignWord: st.assign}, nil
}

// wordRune reads the next part of a word that starts with c.
func (l *lexer) wordRune(st *wordState, c rune) error {
	switch c {
	case '\'':
		st.plain, st.quoted = false, true

		return l.singleQuoted(st)
	case '"':
		st.plain, st.quoted = false, true

		return l.doubleQuoted(st)
	case '\\':
		st.plain, st.quoted = false, true
		l.backslash(st)

		return nil
	case '$', '`':
		st.plain = false

		return l.expansion(st)
	}

	l.literal(st, c)

	return nil
}

// backslash reads an unquoted backslash and the rune it escapes; a backslash before a
// newline joins the lines.
func (l *lexer) backslash(st *wordState) {
	l.i++

	if l.i >= len(l.src) {
		return
	}

	if l.src[l.i] != '\n' {
		st.text.WriteRune(l.src[l.i])
	}

	l.i++
}

// literal reads an unquoted rune, noting an assignment's =, a glob, and brace expansion.
func (l *lexer) literal(st *wordState, c rune) {
	switch c {
	case '=':
		st.assign = st.assign || st.plain && validName(st.text.String())
		st.plain = false
	case '*', '?', '[':
		// An unquoted glob, which the shell may turn into other words.
		st.dynamic = true
	case '{':
		// Brace expansion, {a,b} or {1..3}, turns one word into several.
		st.dynamic = st.dynamic || l.braceExpands()
	}

	st.text.WriteRune(c)

	l.i++
}

// braceExpands reports an unquoted { at the current rune that starts a brace expansion:
// a , or .. before its } in the same word. A lone {} (find's, xargs') stays as written.
func (l *lexer) braceExpands() bool {
	for j := l.i + 1; j < len(l.src); j++ {
		c := l.src[j]
		if c == '}' || wordEnd(c) {
			return false
		}

		if c == ',' || c == '.' && j+1 < len(l.src) && l.src[j+1] == '.' {
			return true
		}
	}

	return false
}

// singleQuoted reads a '…' part, in which nothing is special.
func (l *lexer) singleQuoted(st *wordState) error {
	l.i++

	for l.i < len(l.src) {
		if l.src[l.i] == '\'' {
			l.i++

			return nil
		}

		st.text.WriteRune(l.src[l.i])
		l.i++
	}

	return errShell
}

// doubleQuoted reads a "…" part, in which only $, `, and \ are special.
func (l *lexer) doubleQuoted(st *wordState) error {
	l.i++

	for l.i < len(l.src) {
		if l.src[l.i] == '"' {
			l.i++

			return nil
		}

		if err := l.doubleQuotedRune(st, l.src[l.i]); err != nil {
			return err
		}
	}

	return errShell
}

// doubleQuotedRune reads the next part of a "…" part that starts with c.
func (l *lexer) doubleQuotedRune(st *wordState, c rune) error {
	switch {
	case c == '\\' && l.i+1 < len(l.src) && strings.ContainsRune("$`\"\\\n", l.src[l.i+1]):
		if l.src[l.i+1] != '\n' {
			st.text.WriteRune(l.src[l.i+1])
		}

		l.i += 2
	case c == '$' || c == '`':
		return l.expansion(st)
	default:
		st.text.WriteRune(c)

		l.i++
	}

	return nil
}

// expansion reads a part that starts with $ or `: an ANSI-C quoted $'…', which is static,
// or a variable, a substitution, or arithmetic, which make the word dynamic. The commands
// of a substitution are read too.
func (l *lexer) expansion(st *wordState) error {
	if l.src[l.i] == '$' && l.peek(1) == '\'' {
		l.i++

		return l.ansiQuoted(st)
	}

	start := l.i

	if err := l.readDollar(); err != nil {
		return err
	}

	st.dynamic = true
	st.text.WriteString(string(l.src[start:l.i]))

	return nil
}

// readDollar reads past a $… or `…` expansion, reading the commands of a substitution
// into the line's.
func (l *lexer) readDollar() error {
	switch {
	case l.src[l.i] == '`':
		return l.backquoteSubstitution()
	case l.peek(1) == '(' && l.peek(2) == '(':
		return l.arithmetic()
	case l.peek(1) == '(':
		return l.commandSubstitution()
	case l.peek(1) == '{':
		l.i += 2

		return l.skipTo('}')
	default:
		// A variable, a special parameter, or a lone $ (as in $"…", a translated string,
		// read on as a double-quoted one).
		l.i++
		l.skipParameter()

		return nil
	}
}

// backquoteSubstitution reads a `…` substitution's commands.
func (l *lexer) backquoteSubstitution() error {
	l.i++

	inner, err := l.readBackquote()
	if err != nil {
		return err
	}

	return l.nested(inner)
}

// commandSubstitution reads a $(…) substitution's commands.
func (l *lexer) commandSubstitution() error {
	l.i += 2

	inner, err := l.readParen()
	if err != nil {
		return err
	}

	return l.nested(inner)
}

// arithmetic reads $((…)), which holds no command but its own substitutions.
func (l *lexer) arithmetic() error {
	l.i += 3

	inner, err := l.readParen()
	if err != nil {
		return err
	}

	if l.peek(0) != ')' {
		return errShell
	}

	l.i++

	return l.expandBody(inner)
}

// skipParameter skips a variable's name, or one special parameter ($@, $1, ...).
func (l *lexer) skipParameter() {
	next := l.peek(0)

	if next == '_' || next >= 'a' && next <= 'z' || next >= 'A' && next <= 'Z' {
		for l.i < len(l.src) && isNameRune(l.src[l.i]) {
			l.i++
		}

		return
	}

	if next != 0 && strings.ContainsRune("@*#?$!-0123456789", next) {
		l.i++
	}
}

// skipTo skips past the next unquoted close, minding quotes and nested opens of it.
func (l *lexer) skipTo(closer rune) error {
	opener := map[rune]rune{'}': '{', ')': '('}[closer]
	depth := 1

	for l.i < len(l.src) {
		done, err := l.skipStep(opener, closer, &depth)
		if err != nil || done {
			return err
		}
	}

	return errShell
}

// skipStep skips the part at the current rune, and reports the close that ends skipTo.
func (l *lexer) skipStep(opener, closer rune, depth *int) (bool, error) {
	switch c := l.src[l.i]; c {
	case '\\':
		l.i += 2
	case '\'', '"':
		return false, l.skipQuote(c)
	case opener:
		*depth++
		l.i++
	case closer:
		*depth--
		l.i++

		return *depth == 0, nil
	default:
		l.i++
	}

	return false, nil
}

// skipQuote skips a quoted part that starts at the current rune.
func (l *lexer) skipQuote(q rune) error {
	l.i++

	for l.i < len(l.src) {
		switch {
		case q == '"' && l.src[l.i] == '\\':
			l.i += 2
		case l.src[l.i] == q:
			l.i++

			return nil
		default:
			l.i++
		}
	}

	return errShell
}

// readParen reads up to the ) that closes the current (, and returns what is between.
func (l *lexer) readParen() (string, error) {
	start := l.i

	if err := l.skipTo(')'); err != nil {
		return "", err
	}

	return string(l.src[start : l.i-1]), nil
}

// readBackquote reads up to the closing `, and returns what is between, unescaped.
func (l *lexer) readBackquote() (string, error) {
	var inner strings.Builder

	for l.i < len(l.src) {
		c := l.src[l.i]

		switch {
		case c == '\\' && l.i+1 < len(l.src):
			if !strings.ContainsRune("$`\\", l.src[l.i+1]) {
				inner.WriteRune(c)
			}

			inner.WriteRune(l.src[l.i+1])
			l.i += 2
		case c == '`':
			l.i++

			return inner.String(), nil
		default:
			inner.WriteRune(c)

			l.i++
		}
	}

	return "", errShell
}

// ansiQuoted reads a $'…' part, decoding its escapes as bash does.
func (l *lexer) ansiQuoted(st *wordState) error {
	l.i++ // the opening quote

	for l.i < len(l.src) {
		c := l.src[l.i]

		switch {
		case c == '\'':
			l.i++

			return nil
		case c == '\\' && l.i+1 < len(l.src):
			l.i++
			st.text.WriteString(l.ansiEscape())
		default:
			st.text.WriteRune(c)

			l.i++
		}
	}

	return errShell
}

// ansiEscape decodes the escape after a backslash in $'…'.
func (l *lexer) ansiEscape() string {
	c := l.src[l.i]
	l.i++

	simpleEscapes := map[rune]string{
		'n': "\n", 't': "\t", 'r': "\r", 'a': "\a", 'b': "\b", 'f': "\f", 'v': "\v", 'e': "\x1b", 'E': "\x1b",
		'\\': "\\", '\'': "'", '"': "\"", '?': "?",
	}

	if s, ok := simpleEscapes[c]; ok {
		return s
	}

	switch c {
	case 'x':
		return l.ansiNumber(16, 2)
	case 'u':
		return l.ansiNumber(16, 4)
	case 'U':
		return l.ansiNumber(16, 8)
	case '0', '1', '2', '3', '4', '5', '6', '7':
		l.i--

		return l.ansiNumber(8, 3)
	default:
		return "\\" + string(c)
	}
}

// ansiNumber decodes up to n digits in base as one character.
func (l *lexer) ansiNumber(base, n int) string {
	start := l.i

	for l.i < len(l.src) && l.i-start < n && strings.ContainsRune("0123456789abcdefABCDEF"[:base+max(base-10, 0)], l.src[l.i]) {
		l.i++
	}

	v, err := strconv.ParseUint(string(l.src[start:l.i]), base, 32)
	if err != nil || l.i == start {
		return ""
	}

	return string(rune(v)) //nolint:gosec // a rune from at most 8 hex digits, which string() bounds
}

// assign reports a word that sets a variable: a name, then an unquoted =.
func (w word) assign() bool {
	return w.assignWord
}

// quotedDelim reports a here-document delimiter written with a quote or a backslash,
// whose body the shell doesn't expand.
func (w word) quotedDelim() bool {
	return w.quoted
}

// validName reports a shell variable name.
func validName(s string) bool {
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		return false
	}

	for _, r := range s {
		if !isNameRune(r) {
			return false
		}
	}

	return true
}

// isNameRune reports a rune a variable name may hold.
func isNameRune(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// reservedWord reports a shell reserved word at a command's start, which is not the
// program: the program is the word after it.
func reservedWord(w word) bool {
	switch w.text {
	case "if", "then", "else", "elif", "fi", "while", "until", "do", "done", "esac", "!", "{", "}", "time",
		"for", "select", "case", "function", "in":
		return true
	default:
		return false
	}
}

// listWord reports a reserved word whose header's words are data, up to the separator.
func listWord(w word) bool {
	return w.text == "for" || w.text == "select" || w.text == "case"
}
