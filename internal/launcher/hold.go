package launcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/sensitive"
)

// HoldReason is why a launch is held back.
type HoldReason string

// Why a launch is held back.
const (
	// HoldAgentFiles is a pull request that changes files coding agents load or run:
	// settings with hooks, MCP servers, and instructions. A session that starts where
	// they are checked out, or checks them out, loads them, and hooks run commands as
	// soon as a session starts, in plan mode too.
	HoldAgentFiles HoldReason = "agentFiles"

	// HoldUnchecked is a pull request whose files could not all be checked, so it may
	// change agent files without the app knowing.
	HoldUnchecked HoldReason = "unchecked"

	// HoldRunning is an item with a session still running in the folder the launch would
	// start in. Two agents working in one folder can undo each other's changes, so a second
	// starts only once the user says so; LaunchRequest.Alongside carries that.
	HoldRunning HoldReason = "running"

	// HoldDirty is a routine's launch into a worktree with uncommitted changes, which may
	// be the user's own work in progress: it waits for the user to confirm, which
	// RoutineLaunch.Confirmed carries. A click's launch is never held for it.
	HoldDirty HoldReason = "dirty"
)

// FileChecker checks the files of the pull requests the inbox checks; the app adapts the
// inbox to it. Each method returns sensitive.ErrNotChecked for an item whose files are not
// checked: an issue, or for CheckFiles and StoredFiles a pull request from the repository's
// own people, a trusted bot, or the signed-in user, and for CheckFilesUnattended one the
// signed-in user opened.
type FileChecker interface {
	// CheckFiles checks the item's files against GitHub as of now and stores the
	// outcome. A check that cannot finish comes back with sensitive.StatusFailed.
	CheckFiles(ctx context.Context, itemID int64) (*sensitive.Check, error)

	// CheckFilesUnattended checks the item's files as CheckFiles does, for a routine's
	// launch: under the unattended scope, every pull request the signed-in user didn't
	// open, trusted bots' and members' included.
	CheckFilesUnattended(ctx context.Context, itemID int64) (*sensitive.Check, error)

	// StoredFiles returns the last check, without asking GitHub.
	StoredFiles(ctx context.Context, itemID int64) (*sensitive.Check, error)
}

// Hold is a launch held back for the user to confirm: nothing was started, written, or
// trusted. Every field is a plain value.
type Hold struct {
	// Key is what LaunchRequest.Confirmed carries to launch anyway. It names what the
	// user was shown (the head commit, and whether every file was checked), so a
	// confirmation never carries over to a later push.
	Key string `json:"key"`

	// Reason is HoldAgentFiles, HoldUnchecked, or HoldRunning.
	Reason HoldReason `json:"reason"`

	// Running are the sessions running where the launch would start, newest first, for
	// HoldRunning; empty otherwise.
	Running []RunningSession `json:"running"`

	// Head is the head commit the files were checked at, or "".
	Head string `json:"head"`

	// Files are the agent files the pull request changes; empty for HoldUnchecked.
	Files []sensitive.Finding `json:"files"`

	// Checked is how many files were checked, out of Total; for HoldUnchecked, Checked
	// falls short of Total, or the check failed.
	Checked int `json:"checked"`
	Total   int `json:"total"`

	// Error says why the check failed, or is "".
	Error string `json:"error"`

	// AtHead reports that the session would start in the pull request's worktree, where
	// the agent loads its files the moment it starts. Otherwise they load once the
	// session, or a later one in the same folder, checks the pull request out.
	AtHead bool `json:"atHead"`

	// Dir is where the session would start, with ~ for home, and Worktree its worktree,
	// or "".
	Dir      string `json:"dir"`
	Worktree string `json:"worktree"`

	// Dirty counts the files with uncommitted changes in the worktree, for HoldDirty; 0
	// otherwise.
	Dirty int `json:"dirty"`
}

// holdDirty is the hold for a routine's launch into a worktree with dirty files, named by
// the worktree and the head it is at, or nil for a worktree that is clean, or not made yet.
func holdDirty(wt *Worktree, dir, name string) *Hold {
	if wt == nil || wt.Dirty <= 0 {
		return nil
	}

	return &Hold{
		Key:    fmt.Sprintf("%s:%s:%s:%d", HoldDirty, name, strings.ToLower(wt.Head), wt.Dirty),
		Reason: HoldDirty, Head: wt.Head, Dir: dir, Worktree: name, Dirty: wt.Dirty, AtHead: false,
		Files: []sensitive.Finding{}, Running: []RunningSession{},
	}
}

// holdFor decides whether a launch on a checked pull request waits for the user to
// confirm, and returns nil when it does not: when its files are not checked (check is
// nil), or every one was checked and none is an agent file. Files of the other
// categories never hold a launch back; the card and the prompt point them out. atHead
// reports a session that would start in the pull request's worktree, dir is where it
// would start, and worktree the worktree's name, or "".
func holdFor(check *sensitive.Check, atHead bool, dir, worktree string) *Hold {
	if check == nil {
		return nil
	}

	hold := &Hold{
		Head: check.Head, Checked: check.Checked, Total: check.Total, Error: check.Error, AtHead: atHead,
		Dir: dir, Worktree: worktree, Files: []sensitive.Finding{}, Running: []RunningSession{},
	}

	if g, ok := check.Category(sensitive.CategoryAgent); ok {
		hold.Reason, hold.Files = HoldAgentFiles, g.Files
		hold.Key = string(HoldAgentFiles) + ":" + strings.ToLower(check.Head)

		return hold
	}

	if check.Complete && check.Status != sensitive.StatusFailed && check.Status != sensitive.StatusPending {
		return nil
	}

	hold.Reason = HoldUnchecked
	hold.Key = string(HoldUnchecked) + ":" + string(check.Status) + ":" + strings.ToLower(check.Head)

	return hold
}
