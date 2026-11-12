package agent

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Split reads extra flags typed on one line into one argument each, the way a POSIX shell
// splits words, but without expanding anything: whitespace separates arguments, single
// quotes keep everything up to the next single quote, double quotes keep everything but
// an escaped \" or \\, and a backslash outside quotes keeps the next character. Two
// single quotes in a row make an empty argument. An unclosed quote is an
// ErrUnterminatedQuote. Split(Join(args)) returns args.
func Split(line string) ([]string, error) {
	s := splitter{runes: []rune(line), args: []string{}}

	return s.split()
}

// Join writes arguments on one line that Split reads back unchanged, each as
// textutil.ShellWord writes it: as is when a shell reads it as itself, as --add-dir=/srv/x
// is, and single-quoted otherwise.
func Join(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, textutil.ShellWord(arg))
	}

	return strings.Join(quoted, " ")
}

// splitter is Split's state: the input, the read position, the arguments so far, and the
// argument being read.
type splitter struct {
	runes  []rune
	pos    int
	args   []string
	word   strings.Builder
	inWord bool
}

// split reads every argument.
func (s *splitter) split() ([]string, error) {
	for s.pos < len(s.runes) {
		r := s.next()

		if err := s.consume(r); err != nil {
			return nil, err
		}
	}

	s.flush()

	return s.args, nil
}

// next returns the rune at the read position and moves past it.
func (s *splitter) next() rune {
	r := s.runes[s.pos]
	s.pos++

	return r
}

// consume handles one rune outside quotes.
func (s *splitter) consume(r rune) error {
	switch {
	case unicode.IsSpace(r):
		s.flush()
	case r == '\'' || r == '"':
		return s.quoted(r)
	case r == '\\':
		s.escaped()
	default:
		s.add(r)
	}

	return nil
}

// quoted reads up to the closing quote. In double quotes, \" and \\ stand for the
// character after the backslash; every other character is kept as it is.
func (s *splitter) quoted(quote rune) error {
	s.inWord = true

	for s.pos < len(s.runes) {
		r := s.next()

		switch {
		case r == quote:
			return nil
		case quote == '"' && r == '\\' && s.pos < len(s.runes) && strings.ContainsRune(`"\`, s.runes[s.pos]):
			s.word.WriteRune(s.next())
		default:
			s.word.WriteRune(r)
		}
	}

	return fmt.Errorf("%w: %c", ErrUnterminatedQuote, quote)
}

// escaped keeps the character after a backslash, or the backslash itself at the end.
func (s *splitter) escaped() {
	if s.pos < len(s.runes) {
		s.add(s.next())

		return
	}

	s.add('\\')
}

// add appends r to the argument being read.
func (s *splitter) add(r rune) {
	s.inWord = true
	s.word.WriteRune(r)
}

// flush ends the argument being read, if any.
func (s *splitter) flush() {
	if !s.inWord {
		return
	}

	s.args = append(s.args, s.word.String())
	s.word.Reset()
	s.inWord = false
}
