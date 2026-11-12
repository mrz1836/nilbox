package textutil

import "strings"

// shellPunct is the punctuation, besides =, that every POSIX shell (sh, bash, zsh, dash,
// ksh, mksh) reads as itself anywhere in a word.
const shellPunct = "-_./:,+@%"

// ShellQuote quotes s as one word for a POSIX shell, whatever s holds: in single quotes,
// inside which nothing is special, each single quote in s closing them, written as \',
// and opening them again. The empty string comes out as a pair of quotes. It is the
// quoting for every word of a command a shell runs, the program's name included.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellWord quotes s as one word for a POSIX shell only when it needs quotes: as it is
// when ShellSafe passes it, else as ShellQuote quotes it. It suits a command shown to the
// user, or copied, where a plain word reads better. A shell reads a command's first word
// as more than itself when it is a reserved word, an alias, or, as in A=b, sets a
// variable, so a program's name is quoted with ShellQuote unless it is a path that starts
// with a slash.
func ShellWord(s string) string {
	if ShellSafe(s) {
		return s
	}

	return ShellQuote(s)
}

// ShellSafe reports whether every POSIX shell reads s, unquoted, as exactly one word that
// holds s: s isn't empty, holds only ASCII letters and digits, =, and -_./:,+@%, and
// doesn't start with =, which zsh reads as a command's path. Every other character,
// spaces, quotes, $, `, \, globs, ~, #, control characters, and anything outside ASCII
// among them, needs quotes.
func ShellSafe(s string) bool {
	if s == "" || s[0] == '=' {
		return false
	}

	for i := range len(s) {
		if !shellSafeByte(s[i]) {
			return false
		}
	}

	return true
}

// shellSafeByte reports a byte ShellSafe lets stand unquoted.
func shellSafeByte(c byte) bool {
	switch {
	case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '=':
		return true
	default:
		return strings.IndexByte(shellPunct, c) >= 0
	}
}
