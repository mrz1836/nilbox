// Package textutil holds the small text helpers the services share for what they show:
// cutting text to a length, turning an error message into a sentence, handing the UI an
// empty list instead of a nil one, and quoting a word for a POSIX shell, always
// (ShellQuote) or only when it needs it (ShellWord).
package textutil
