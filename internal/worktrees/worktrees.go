// Package worktrees lists the git worktrees of the user's local clones and removes them
// safely.
//
// It covers every git repository directly under the clone root (the same scan clone
// resolution uses), so worktrees left by sessions started elsewhere show up too. Removal
// is deliberately narrow:
//
//   - Remove runs `git worktree remove` without --force, so git itself refuses a worktree
//     with uncommitted changes or a lock;
//   - ForceRemove passes a single --force and only for an unlocked worktree with
//     uncommitted changes, so a locked worktree (usually a live agent session) is never
//     removed;
//   - the main worktree is never removed, and no branch is ever deleted.
//
// Every git command runs as an argv `git -C <dir> ...` from a short allowlist, never
// through a shell.
package worktrees

// Snapshot is every repository under the clone root that has linked worktrees, as the
// Worktrees view shows it. Every field is a plain value.
type Snapshot struct {
	// Root is the clone root, shown relative to home with ~.
	Root string `json:"root"`

	// Repos are the repositories with linked worktrees (or that could not be listed),
	// sorted by path.
	Repos []Repo `json:"repos"`

	// CleanCount is how many worktrees "Remove all clean" would remove.
	CleanCount int `json:"cleanCount"`

	// ListedAt is when the snapshot was taken, in RFC 3339.
	ListedAt string `json:"listedAt"`
}

// Repo is one clone under the clone root and its worktrees.
type Repo struct {
	// Path is the clone's directory under the clone root; pass it back to the removal
	// and prune calls.
	Path string `json:"path"`

	// DisplayPath is Path relative to home with ~.
	DisplayPath string `json:"displayPath"`

	// Name is the clone directory's name.
	Name string `json:"name"`

	// HasStale reports worktree entries whose directories are gone, which Prune clears.
	HasStale bool `json:"hasStale"`

	// Error explains why the repository's worktrees could not be listed, or is "".
	Error string `json:"error"`

	// Worktrees lists the main worktree first, then the linked ones in git's order.
	Worktrees []Worktree `json:"worktrees"`
}

// Worktree is one row of the Worktrees view.
type Worktree struct {
	// Path is the worktree's directory as git reports it.
	Path string `json:"path"`

	// DisplayPath is Path relative to home with ~.
	DisplayPath string `json:"displayPath"`

	// Branch is the checked-out branch, or "" when detached.
	Branch string `json:"branch"`

	// Detached reports a detached HEAD.
	Detached bool `json:"detached"`

	// Main reports the repository's main worktree, which is never removable.
	Main bool `json:"main"`

	// Locked reports a `git worktree lock`; the agent locks a worktree while a session
	// runs in it.
	Locked bool `json:"locked"`

	// LockReason is the lock's reason, if any.
	LockReason string `json:"lockReason"`

	// StaleLock reports an agent lock whose session process is no longer running. It is
	// only a hint: the worktree stays locked and is still not removable.
	StaleLock bool `json:"staleLock"`

	// Prunable reports an entry whose directory is gone.
	Prunable bool `json:"prunable"`

	// PrunableReason is git's explanation for Prunable.
	PrunableReason string `json:"prunableReason"`

	// Dirty reports uncommitted changes, counting untracked files.
	Dirty bool `json:"dirty"`

	// DirtyCount is the number of changed and untracked files.
	DirtyCount int `json:"dirtyCount"`

	// LastCommitUnix is the commit time of HEAD in Unix seconds, or 0 when unknown.
	LastCommitUnix int64 `json:"lastCommitUnix"`

	// ClaudeCreated reports a worktree under the repository's .claude/worktrees
	// directory, where the coding agent creates its worktrees.
	ClaudeCreated bool `json:"claudeCreated"`

	// CanRemove reports a clean, unlocked linked worktree that Remove accepts.
	CanRemove bool `json:"canRemove"`

	// CanForceRemove reports an unlocked linked worktree with uncommitted changes that
	// ForceRemove accepts after the user confirms.
	CanForceRemove bool `json:"canForceRemove"`

	// Error explains why the worktree's status could not be read, or is "".
	Error string `json:"error"`
}

// RemoveResult is the outcome of removing one worktree in a bulk removal.
type RemoveResult struct {
	// RepoPath is the clone the worktree belongs to.
	RepoPath string `json:"repoPath"`

	// Path is the worktree's directory.
	Path string `json:"path"`

	// DisplayPath is Path relative to home with ~.
	DisplayPath string `json:"displayPath"`

	// Removed reports whether the worktree was removed.
	Removed bool `json:"removed"`

	// Reason explains why it was not removed, or is "".
	Reason string `json:"reason"`
}
