package textutil

import "strings"

// shellPunct is the punctuation, besides =, that every POSIX shell (sh, bash, zsh, dash,
// ksh, mksh) reads as itself anywhere in a word.
const shellPunct = "-_./:,+@%"

// ShellQuote quotes s as one word for a POSIX shell, whatever s holds: in single quotes,
// inside which nothing is special, each single quote in s closing them, written as \',
// and opening them again. The empty string comes out as a pair of quotes. It quotes every
// word of a script the app writes, the ssh command it hands git, and the program of a
// command line a shell runs unless that is a path that starts with a slash (ShellWord).
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ShellWord quotes s as one word for a POSIX shell only when it needs quotes: as it is
// when ShellSafe passes it, else as ShellQuote quotes it. Every POSIX shell reads what it
// returns as the one word s, so it suits a command shown to the user, or copied, where a
// plain word reads better, and a folder or an argument of a command line a shell runs. A
// shell reads a command's first word as more than itself, though, when it is a reserved
// word, an alias, or, as in A=b, sets a variable, so a command line a shell runs quotes
// its program with ShellQuote unless it is a path that starts with a slash. A command
// only shown (scriptrun.DisplayCommand) passes its program here too: a run's is always
// such a path.
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
