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
//   - Unlock releases only a lock the coding agent wrote for a session that has certainly
//     ended, never a lock set by hand or one whose session may still run;
//   - the main worktree is never removed.
//
// A worktree the agent made for a pull request or issue (pr-<n> or issue-<n> under the
// clone's .claude/worktrees) is linked to that item, and GitHub says whether it is done.
// GitHub decides when a worktree is finished; git alone decides whether removing it is
// safe. CleanUp, CleanUpFinished, and the automatic Sweep remove a worktree only when its
// item is merged or closed, it has no uncommitted changes, every commit it has is on
// GitHub, and no session runs in it. Then they also delete the agent's own
// worktree-<name> branch, and only while it still points at the commit checked. No other
// branch is ever deleted, and nothing on GitHub is touched.
//
// Every git command runs as an argv `git -C <dir> ...` from a short allowlist, never
// through a shell.
package worktrees

import "github.com/mrz1836/nilbox/internal/item"

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

	// FinishedCount is how many worktrees "Clean up finished" would remove.
	FinishedCount int `json:"finishedCount"`

	// ReviewCount is how many worktrees are done on GitHub but hold work that is not.
	ReviewCount int `json:"reviewCount"`

	// WorktreeCount is how many linked worktrees the snapshot lists, the main worktrees
	// aside.
	WorktreeCount int `json:"worktreeCount"`

	// LinkError explains why the pull requests and issues could not be read from GitHub,
	// or is "". The worktrees are listed without their items then.
	LinkError string `json:"linkError"`

	// ListedAt is when the snapshot was taken, in RFC 3339.
	ListedAt string `json:"listedAt"`
}

// Stage is where a worktree stands with its pull request or issue.
type Stage string

// Stages of a worktree's pull request or issue, as Worktree.Stage reports them.
const (
	// StageNone is a worktree with no item, or one locked by hand.
	StageNone Stage = ""

	// StageInUse is a worktree a running agent session holds.
	StageInUse Stage = "in-use"

	// StageActive is a worktree whose pull request or issue is still open.
	StageActive Stage = "active"

	// StageFinished is a worktree whose item is done and that CleanUp removes without
	// losing anything.
	StageFinished Stage = "finished"

	// StageNeedsReview is a worktree whose item is done but that holds uncommitted
	// changes or commits that are not on GitHub, or whose state could not be read.
	StageNeedsReview Stage = "needs-review"
)

// ItemStatus is whether a pull request or issue is open, merged, or closed.
type ItemStatus string

// Item states, as LinkedItem.State reports them.
const (
	ItemOpen   ItemStatus = "open"
	ItemMerged ItemStatus = "merged"
	ItemClosed ItemStatus = "closed"
)

// LinkedItem is the pull request or issue a worktree was made for, as GitHub reports it
// now.
type LinkedItem struct {
	Kind item.Kind `json:"kind"`

	// Owner and Name are the item's repository.
	Owner string `json:"owner"`
	Name  string `json:"name"`

	// Number is the item's number.
	Number int `json:"number"`

	Title string `json:"title"`
	URL   string `json:"url"`

	// State is ItemOpen, ItemMerged, or ItemClosed.
	State ItemStatus `json:"state"`

	// ClosedAt is when the item was merged or closed, in RFC 3339, or "" while open.
	ClosedAt string `json:"closedAt"`

	// HeadOID is a pull request's head commit, or "".
	HeadOID string `json:"headOid"`

	// Launched reports a worktree the app launched the session in, which the automatic
	// cleanup looks after. Others are linked by their name alone.
	Launched bool `json:"launched"`
}

// done reports a merged or closed item.
func (i *LinkedItem) done() bool {
	return i != nil && (i.State == ItemMerged || i.State == ItemClosed)
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

	// AgentLock reports a lock the coding agent wrote for a session.
	AgentLock bool `json:"agentLock"`

	// StaleLock reports an agent lock whose session has certainly ended: its process is
	// gone, or the PID now belongs to a process that started later.
	StaleLock bool `json:"staleLock"`

	// CanUnlock reports a stale agent lock, which Unlock releases.
	CanUnlock bool `json:"canUnlock"`

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

	// Head is the commit the worktree has checked out.
	Head string `json:"head"`

	// Unpushed is how many commits on HEAD no remote-tracking branch has, or -1 when it
	// could not be counted (or was not, for the main worktree).
	Unpushed int `json:"unpushed"`

	// OnGitHub reports that every commit the worktree has is on GitHub: none is missing
	// from the remote-tracking branches, or HEAD is its pull request's head commit, which
	// GitHub keeps.
	OnGitHub bool `json:"onGitHub"`

	// Item is the pull request or issue the worktree was made for, or nil.
	Item *LinkedItem `json:"item"`

	// Stage is where the worktree stands.
	Stage Stage `json:"stage"`

	// OwnBranch reports that the checked-out branch is the agent's worktree-<name> branch
	// for this worktree, which CleanUp deletes with it.
	OwnBranch bool `json:"ownBranch"`

	// ClaudeCreated reports a worktree under the repository's .claude/worktrees
	// directory, where the coding agent creates its worktrees.
	ClaudeCreated bool `json:"claudeCreated"`

	// CanRemove reports a clean, unlocked linked worktree that Remove accepts.
	CanRemove bool `json:"canRemove"`

	// CanForceRemove reports an unlocked linked worktree with uncommitted changes that
	// ForceRemove accepts after the user confirms.
	CanForceRemove bool `json:"canForceRemove"`

	// CanCleanUp reports a finished worktree that CleanUp removes, with its own branch.
	CanCleanUp bool `json:"canCleanUp"`

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

	// DeletedBranch is the branch deleted with the worktree, or "".
	DeletedBranch string `json:"deletedBranch"`

	// Item names the pull request or issue a cleaned-up worktree was for, e.g.
	// "acme-org/example-repo#184", or is "".
	Item string `json:"item"`

	// Reason explains why it was not removed, or why its branch was kept, or is "".
	Reason string `json:"reason"`
}

// Cleanup is one worktree the app cleaned up, as the Worktrees view's history shows it.
type Cleanup struct {
	// ID identifies the record.
	ID int64 `json:"id"`

	// CleanedAt is when the worktree was removed, in RFC 3339.
	CleanedAt string `json:"cleanedAt"`

	// Automatic reports a cleanup by the sweep rather than by the user.
	Automatic bool `json:"automatic"`

	// Repo is the clone's directory name.
	Repo string `json:"repo"`

	// Worktree is the worktree's directory name, e.g. "pr-184".
	Worktree string `json:"worktree"`

	// DisplayPath was the worktree's path relative to home with ~.
	DisplayPath string `json:"displayPath"`

	// DeletedBranch is the branch deleted with it, or "" when it was kept.
	DeletedBranch string `json:"deletedBranch"`

	// Head is the commit it had checked out, which is on GitHub.
	Head string `json:"head"`

	// Item is the pull request or issue it was for, or nil.
	Item *LinkedItem `json:"item"`
}

// SweepReport is what the latest automatic sweep found and did.
type SweepReport struct {
	// Seq numbers the sweeps since the app started, from 1; 0 means none has run yet.
	Seq int `json:"seq"`

	// At is when the sweep finished, in RFC 3339, or "".
	At string `json:"at"`

	// Enabled reports whether automatic cleanup was on.
	Enabled bool `json:"enabled"`

	// Cleaned lists the worktrees the sweep removed.
	Cleaned []RemoveResult `json:"cleaned"`

	// Failed lists the worktrees it tried to remove and could not, with why.
	Failed []RemoveResult `json:"failed"`

	// Leftover is how many worktrees are done on GitHub but still on disk and not in use,
	// so the user may want to look at them.
	Leftover int `json:"leftover"`

	// WorktreeCount is how many linked worktrees the clones under the clone root have
	// once the sweep is done, as a listing's Snapshot.WorktreeCount would count them. It
	// is read from each clone's git directory, without running git.
	WorktreeCount int `json:"worktreeCount"`

	// Error explains why the sweep could not run, or is "".
	Error string `json:"error"`
}
