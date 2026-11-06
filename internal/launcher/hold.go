package launcher

import (
	"context"
	"strings"

	"github.com/mrz1836/nilbox/internal/sensitive"
)

// Why a launch is held back.
const (
	// HoldAgentFiles is a pull request that changes files coding agents load or run:
	// settings with hooks, MCP servers, and instructions. A session that starts where
	// they are checked out, or checks them out, loads them, and hooks run commands as
	// soon as a session starts, in plan mode too.
	HoldAgentFiles = "agentFiles"

	// HoldUnchecked is a pull request whose files could not all be checked, so it may
	// change agent files without the app knowing.
	HoldUnchecked = "unchecked"
)

// FileChecker checks the files of the pull requests the inbox checks. *inbox.Service
// implements it. Both methods return sensitive.ErrNotChecked for an item whose files are
// not checked: an issue, or a pull request from the repository's own people, a trusted
// bot, or the signed-in user.
type FileChecker interface {
	// CheckFiles checks the item's files against GitHub as of now and stores the
	// outcome. A check that cannot finish comes back with sensitive.StatusFailed.
	CheckFiles(ctx context.Context, itemID int64) (*sensitive.Check, error)

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

	// Reason is HoldAgentFiles or HoldUnchecked.
	Reason string `json:"reason"`

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

	// AtHead reports that the session would start in a worktree at the pull request's
	// head, where the agent loads its files the moment it starts. Otherwise they load
	// once the session, or a later one in the same folder, checks the pull request out.
	AtHead bool `json:"atHead"`

	// Dir is where the session would start, with ~ for home, and Worktree its worktree,
	// or "".
	Dir      string `json:"dir"`
	Worktree string `json:"worktree"`
}

// holdFor decides whether a launch on a checked pull request waits for the user to
// confirm, and returns nil when it does not: when its files are not checked (check is
// nil), or every one was checked and none is an agent file. Files of the other
// categories never hold a launch back; the card and the prompt point them out.
func holdFor(check *sensitive.Check, p Plan, dir string) *Hold {
	if check == nil {
		return nil
	}

	hold := &Hold{
		Head: check.Head, Checked: check.Checked, Total: check.Total, Error: check.Error, AtHead: p.WorktreeOnPRHead,
		Dir: dir, Worktree: worktreeDirName(p.Worktree), Files: []sensitive.Finding{},
	}

	if g, ok := check.Category(sensitive.CategoryAgent); ok {
		hold.Reason, hold.Files = HoldAgentFiles, g.Files
		hold.Key = HoldAgentFiles + ":" + strings.ToLower(check.Head)

		return hold
	}

	if check.Complete && check.Status != sensitive.StatusFailed && check.Status != sensitive.StatusPending {
		return nil
	}

	hold.Reason = HoldUnchecked
	hold.Key = HoldUnchecked + ":" + check.Status + ":" + strings.ToLower(check.Head)

	return hold
}
