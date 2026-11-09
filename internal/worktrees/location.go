package worktrees

import (
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// maxBranchLen caps a branch name the app passes to git.
const maxBranchLen = 255

// ManagedRoot returns where the app makes a clone's worktrees: a hidden directory beside
// the clone, <parent>/.<slug>/worktrees/<clone's name>, so for a clone under the clone
// root, ~/projects/.<slug>/worktrees/example-repo. Keeping worktrees out of the clone keeps
// a session's tools (and the agent's own instructions) from finding them inside it, needs
// no ignore rule in the clone, and the clone scans skip hidden directories.
func ManagedRoot(clone string) string {
	clean := filepath.Clean(clone)

	return filepath.Join(filepath.Dir(clean), "."+brand.Slug, "worktrees", filepath.Base(clean))
}

// ManagedPath returns where the app makes the worktree named name (pr-<n> or issue-<n>)
// of a clone.
func ManagedPath(clone, name string) string {
	return filepath.Join(ManagedRoot(clone), name)
}

// legacyRoot returns where Claude Code made a clone's worktrees before the app made them
// itself: <clone>/.claude/worktrees.
func legacyRoot(clone string) string {
	return filepath.Join(filepath.Clean(clone), filepath.FromSlash(worklog.LegacyDir))
}

// validBranch reports a branch name git takes that cannot be read as an option or reach
// outside refs/heads: no leading "-" or ".", no "..", "//", "@{", whitespace, control
// characters, or any of ~^:?*[\, no component starting with "." or ending in ".lock",
// and no trailing "/" or ".".
func validBranch(name string) bool {
	if name == "" || name == "@" || len(name) > maxBranchLen || strings.HasPrefix(name, "-") {
		return false
	}

	for _, bad := range []string{"..", "@{"} {
		if strings.Contains(name, bad) {
			return false
		}
	}

	if strings.ContainsAny(name, "~^:?*[\\") || strings.ContainsFunc(name, unsafeRune) {
		return false
	}

	return !slices.ContainsFunc(strings.Split(name, "/"), badComponent)
}

// unsafeRune reports a rune a branch name may not hold: whitespace or a control
// character.
func unsafeRune(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r)
}

// badComponent reports a part of a branch name between slashes that git refuses: empty
// (from "//", or a leading or trailing "/"), starting with ".", or ending in ".lock" or
// ".".
func badComponent(part string) bool {
	return part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") || strings.HasSuffix(part, ".")
}
