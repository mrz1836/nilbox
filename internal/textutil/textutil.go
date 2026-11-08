package textutil

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ellipsis ends text that was cut.
const ellipsis = "…"

// Truncate cuts s to at most n runes, ending it with an ellipsis (which counts toward n)
// when it was cut. Space before the ellipsis is dropped. An n below 1 leaves s as it is.
func Truncate(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}

	runes := []rune(s)

	return strings.TrimRightFunc(string(runes[:n-1]), unicode.IsSpace) + ellipsis
}

// Clip cuts s to at most n runes, without an ellipsis, and trims the space around what is
// left when it was cut. An n below 1 leaves s as it is.
func Clip(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}

	return strings.TrimSpace(string([]rune(s)[:n]))
}

// Capitalize upper-cases the first letter of s.
func Capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}

	return string(unicode.ToUpper(r)) + s[size:]
}

// Sentence turns a message such as an error's into a sentence: trimmed, its first letter
// upper-cased, and ending with a period. An empty message stays empty.
func Sentence(msg string) string {
	msg = Capitalize(strings.TrimSpace(msg))
	if msg == "" || strings.HasSuffix(msg, ".") {
		return msg
	}

	return msg + "."
}

// NonNil returns list, or an empty list for nil, so JSON carries [] rather than null and
// the UI always gets an array.
func NonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}

	return list
}

// Join joins string-typed values with sep, as strings.Join joins strings, for listing the
// choices of a named string type in a message.
func Join[S ~string](elems []S, sep string) string {
	parts := make([]string, len(elems))
	for i, e := range elems {
		parts[i] = string(e)
	}

	return strings.Join(parts, sep)
}
