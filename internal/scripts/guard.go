package scripts

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
)

// hiddenWhy is why a command holding a character that hides what it does is greyed: a
// line break shows one line that runs two, and a direction mark reorders what is shown.
const hiddenWhy = "Holds a character that hides what it runs, such as a line break or a text direction mark."

// posixShells are the shells, by base name, whose quoting the app writes: the POSIX
// family. A command line quoted for them means something else to fish, say.
func posixShells() []string {
	return []string{"sh", "bash", "zsh", "dash", "ksh", "mksh"}
}

// posixShell reports a shell, by its path or name, of the POSIX family.
func posixShell(path string) bool {
	return slices.Contains(posixShells(), filepath.Base(path))
}

// hidden reports whether s holds a character that hides what a command does: a control
// character other than tab (a line break among them), DEL, or a bidirectional text
// control, which reorders what is shown.
func hidden(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		switch {
		case r < 0x20:
			return r != '\t'
		case r == 0x7f, r == 0x200e, r == 0x200f, r == 0x061c:
			return true
		default:
			return (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
		}
	})
}

// anyHidden reports whether a word of words hides what it does.
func anyHidden(words ...string) bool {
	return slices.ContainsFunc(words, hidden)
}

// selfUpdaterWhy is why magex's own updaters are greyed: the app updates magex itself, by
// how it was installed, and never with go install, which magex's updater runs for a copy
// in go install's folder.
const selfUpdaterWhy = "This updates magex itself. Update it in Settings → Integrations, which " +
	"updates it the way it was installed."

// selfUpdater reports one of magex's targets that updates magex itself.
func selfUpdater(name string) bool {
	return name == "update" || name == "upgrade" || name == "version:update" || strings.HasPrefix(name, "update:")
}

// taggingTargets are magex's targets that create, move, push, or remove a version tag:
// Release does that, with its checks.
func taggingTargets() []string {
	return []string{"version:bump", "version:tag", "git:tag", "git:tagupdate", "git:tagremove"}
}

// tagging reports one of taggingTargets, in any case, as magex reads them.
func tagging(name string) bool {
	return slices.Contains(taggingTargets(), strings.ToLower(name))
}

// taggingWord matches a tagging target named anywhere in a command line, as its own word.
var taggingWord = regexp.MustCompile(`(?i)(^|[^a-z0-9:_.-])(` + strings.Join(quoteMeta(taggingTargets()), "|") +
	`)($|[^a-z0-9:_.-])`)

// quoteMeta quotes each word for a regular expression.
func quoteMeta(words []string) []string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		out = append(out, regexp.QuoteMeta(w))
	}

	return out
}

// bumps reports whether a command, by the words it runs, would tag a version: a tagging
// target named in any word, or, after magex, one of aliases, the other names magex gives
// them.
func bumps(words, aliases []string) bool {
	if slices.ContainsFunc(words, taggingWord.MatchString) {
		return true
	}

	if len(aliases) == 0 {
		return false
	}

	alias := regexp.MustCompile(`(?i)(^|[^a-z0-9_.-])magex['"]?\s+['"]?(` + strings.Join(quoteMeta(aliases), "|") +
		`)($|[^a-z0-9:_.-])`)

	return slices.ContainsFunc(words, alias.MatchString)
}

// taggingAliases are the aliases b gives the tagging targets.
func (b *builtins) taggingAliases() []string {
	var out []string

	for _, c := range b.list {
		if tagging(c.Name) {
			out = append(out, c.Aliases...)
		}
	}

	return out
}

// guardBuiltin greys one of magex's own updaters, and points one that tags a version to
// Release, by its name or any alias.
func guardBuiltin(sc *script, c builtin) {
	names := append([]string{c.Name}, c.Aliases...)

	switch {
	case slices.ContainsFunc(names, selfUpdater):
		sc.entry.Runnable, sc.entry.Why = false, selfUpdaterWhy
	case slices.ContainsFunc(names, tagging):
		sc.entry.Release = true
	}
}

// shellWhy is why a task whose own shell isn't of the POSIX family is greyed when it has
// arguments, which the app quotes as a POSIX shell reads them.
func shellWhy(shell string) string {
	return "Its shell, " + filepath.Base(shell) + ", reads quotes differently from the POSIX shells, so " +
		brand.Name + " can't pass it the task's arguments."
}
