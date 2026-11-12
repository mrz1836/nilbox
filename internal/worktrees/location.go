package worktrees

import (
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/worklog"
)

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
