// Package textutil holds the small text helpers the services share for what they show:
// cutting text to a length, turning an error message into a sentence, counting a noun and
// listing names as a sentence does, handing the UI an empty list instead of a nil one,
// treating a list of names as a set that ignores case, as GitHub does (ContainsFold,
// UniqueFold, ApartFold), quoting a word for a POSIX shell, always (ShellQuote) or only
// when it needs it (ShellWord), and minting a random UUID (NewUUID) and reading one in its
// canonical form (CanonicalUUID).
package textutil
