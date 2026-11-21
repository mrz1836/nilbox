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

// upgradeTargets are magex's targets that upgrade a repository's dependencies in the clone
// itself, on whatever branch it is on, and leave the change uncommitted: Deps does that, in
// a fresh worktree, with a pull request. deps:tidy only tidies, and stays.
func upgradeTargets() []string {
	return []string{"deps:update", "mod:update"}
}

// upgrading reports one of upgradeTargets, in any case, as magex reads them.
func upgrading(name string) bool {
	return slices.Contains(upgradeTargets(), strings.ToLower(name))
}

// taggingWord and upgradeWord match a tagging target and an upgrade target named anywhere
// in a command line, as its own word.
var (
	taggingWord = regexp.MustCompile(targetPattern(taggingTargets()))
	upgradeWord = regexp.MustCompile(targetPattern(upgradeTargets()))
)

// targetPattern matches one of targets named anywhere in a command line, as its own word,
// in any case.
func targetPattern(targets []string) string {
	return `(?i)(^|[^a-z0-9:_.-])(` + strings.Join(quoteMeta(targets), "|") + `)($|[^a-z0-9:_.-])`
}

// The package managers' commands that upgrade a repository's dependencies in the clone
// itself, as a command line names them: each program by its name, alone, quoted, or at
// the end of a path.
var (
	// goGetUpdate matches go get with -u, or -u=patch, among its arguments.
	goGetUpdate = regexp.MustCompile(`(?i)(^|[^a-z0-9_.-])go['"]?\s+['"]?get['"]?` + oneCommandWords +
		`\s+['"]?-u(=true|=patch)?['"]?($|[\s;&|)])`)

	// ncuUpgrade matches npm-check-updates, by either of its names and at any version, with
	// -u among its arguments, or -i, which asks about each dependency before writing it,
	// either short or long: it writes the newest versions into package.json.
	ncuUpgrade = regexp.MustCompile(`(?i)(^|[^a-z0-9_.-])(ncu|npm-check-updates)(@[^\s'"]*)?['"]?` +
		oneCommandWords + `\s+['"]?(-u|--upgrade|-i|--interactive)['"]?($|[\s;&|)])`)

	// managerUpdate matches a package manager's own command that upgrades the dependencies,
	// after any of its flags, such as pnpm -r up: npm's and pnpm's update (up, upgrade),
	// yarn's upgrade (up, upgrade-interactive), and bun's update, never bun's upgrade, which
	// upgrades bun itself. A script run by name, as pnpm run update runs one, is the
	// repository's own.
	managerUpdate = regexp.MustCompile(`(?i)(^|[^a-z0-9_.-])(` +
		`(npm|pnpm)` + managerFlags + `\s+['"]?(update|upgrade|up)|` +
		`yarn` + managerFlags + `\s+['"]?(upgrade-interactive|upgrade|up)|` +
		`bun` + managerFlags + `\s+['"]?update` +
		`)['"]?($|[^a-z0-9:_.-])`)
)

// The parts of the package managers' patterns: any words of one command, up to a ;, &, |,
// or parenthesis, and a program's flags after it, each a word that starts with a dash.
const (
	oneCommandWords = `(\s+[^\s;&|()]+)*`
	managerFlags    = `['"]?(\s+['"]?-[^\s'"]*['"]?)*`
)

// upgradeCommands are the package managers' commands that upgrade a repository's
// dependencies in the clone itself.
func upgradeCommands() []*regexp.Regexp {
	return []*regexp.Regexp{goGetUpdate, ncuUpgrade, managerUpdate}
}

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
// them. The words are read joined into one line, so a program and its arguments read as
// the same command written in a shell would.
func bumps(words, aliases []string) bool {
	return namesTarget(strings.Join(words, " "), taggingWord, aliases)
}

// upgrades reports whether a command, by the words it runs, would upgrade the repository's
// dependencies in the clone itself: an upgrade target named in any word, or, after magex,
// one of aliases, the other names magex gives them, or one of upgradeCommands. The words
// are read as one line, as bumps reads them.
func upgrades(words, aliases []string) bool {
	line := strings.Join(words, " ")

	return namesTarget(line, upgradeWord, aliases) ||
		slices.ContainsFunc(upgradeCommands(), func(re *regexp.Regexp) bool { return re.MatchString(line) })
}

// namesTarget reports whether line, a command's words, names a target word matches, or,
// after magex, one of aliases.
func namesTarget(line string, word *regexp.Regexp, aliases []string) bool {
	if word.MatchString(line) {
		return true
	}

	if len(aliases) == 0 {
		return false
	}

	alias := regexp.MustCompile(`(?i)(^|[^a-z0-9_.-])magex['"]?\s+['"]?(` + strings.Join(quoteMeta(aliases), "|") +
		`)($|[^a-z0-9:_.-])`)

	return alias.MatchString(line)
}

// taggingAliases are the aliases b gives the tagging targets.
func (b *builtins) taggingAliases() []string {
	return b.aliasesOf(tagging)
}

// upgradeAliases are the aliases b gives the upgrade targets.
func (b *builtins) upgradeAliases() []string {
	return b.aliasesOf(upgrading)
}

// aliasesOf are the aliases b gives the targets named reports.
func (b *builtins) aliasesOf(named func(string) bool) []string {
	var out []string

	for _, c := range b.list {
		if named(c.Name) {
			out = append(out, c.Aliases...)
		}
	}

	return out
}

// guardBuiltin greys one of magex's own updaters, points one that tags a version to
// Release, and points one that upgrades the dependencies to Deps, by its name or any alias.
func guardBuiltin(sc *script, c builtin) {
	names := append([]string{c.Name}, c.Aliases...)

	switch {
	case slices.ContainsFunc(names, selfUpdater):
		sc.entry.Runnable, sc.entry.Why = false, selfUpdaterWhy
	case slices.ContainsFunc(names, tagging):
		sc.entry.Release = true
	case slices.ContainsFunc(names, upgrading):
		sc.entry.Deps = true
	}
}

// shellWhy is why a task whose own shell isn't of the POSIX family is greyed when it has
// arguments, which the app quotes as a POSIX shell reads them.
func shellWhy(shell string) string {
	return "Its shell, " + filepath.Base(shell) + ", reads quotes differently from the POSIX shells, so " +
		brand.Name + " can't pass it the task's arguments."
}
