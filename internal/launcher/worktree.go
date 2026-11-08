package launcher

import (
	"context"
	"errors"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
)

// ErrNoWorktrees is returned for a worktree launch when the Service has no WorktreeMaker.
var ErrNoWorktrees = errors.New("worktrees are not available")

// WorktreeAction is what a launch does to give its item a worktree.
type WorktreeAction string

// What a launch does to give its item a worktree.
const (
	// WorktreeResume reuses the item's worktree.
	WorktreeResume WorktreeAction = "resume"

	// WorktreeAdopt takes over a worktree found on the item's branch somewhere else,
	// such as one Claude Code made under the clone's .claude/worktrees.
	WorktreeAdopt WorktreeAction = "adopt"

	// WorktreeAttach makes a worktree on the item's branch left from an earlier one.
	WorktreeAttach WorktreeAction = "attach"

	// WorktreeCreate fetches the pull request's head, or the default branch for an
	// issue, and makes a worktree from it.
	WorktreeCreate WorktreeAction = "create"
)

// WorktreeRequest asks for an item's worktree in a clone.
type WorktreeRequest struct {
	// Clone is the clone's absolute directory, and Remote its remote for the item's
	// repository.
	Clone  string
	Remote string

	// Owner and Name are the item's repository.
	Owner string
	Name  string

	// Kind and Number are the item.
	Kind   item.Kind
	Number int

	// DefaultBranch is the repository's default branch as GitHub reported it, or "".
	DefaultBranch string
}

// Worktree is an item's worktree, as a WorktreeMaker found or made it, or would.
type Worktree struct {
	// Path is the worktree's directory, and Branch the branch it has checked out.
	Path   string
	Branch string

	// Action is what was done, or would be.
	Action WorktreeAction

	// Base names what a created worktree starts from, such as "origin/main", or "".
	Base string

	// Dirty is how many files have uncommitted changes in an existing worktree, or -1
	// when unknown.
	Dirty int

	// Notice says what was worked around, or is "".
	Notice string
}

// WorktreeMaker finds and makes the worktrees launches start in. The app adapts the
// worktrees package's Maker to it.
type WorktreeMaker interface {
	// PlanWorktree says what EnsureWorktree would do, changing nothing and asking no
	// remote.
	PlanWorktree(ctx context.Context, req WorktreeRequest) (Worktree, error)

	// EnsureWorktree finds the item's worktree, or makes it, and records it.
	EnsureWorktree(ctx context.Context, req WorktreeRequest) (Worktree, error)
}

// SessionRecorder records each session started in an item's worktree. *worklog.Store
// implements it.
type SessionRecorder interface {
	Touch(ctx context.Context, clonePath, worktree string, id agent.ID, at time.Time) error
}

// Truster marks directories trusted in one agent's config, so a session started there
// does not stop at the agent's workspace trust question. The app gives the launcher one
// per agent.
type Truster interface {
	// Trust marks dirs trusted in the config the agent reads with configDir: a profile's
	// config directory, or "" for the agent's default.
	Trust(ctx context.Context, configDir string, dirs []string) error
}

// WorktreePlan is the worktree a planned launch starts in, as the preview shows it.
type WorktreePlan struct {
	// Name is the worktree's name, pr-<n> or issue-<n>.
	Name string `json:"name"`

	// Path is its directory, with ~ for home.
	Path string `json:"path"`

	// Branch is the branch it has, or will have, checked out.
	Branch string `json:"branch"`

	// Action is what the launch does: resume, adopt, attach, or create.
	Action WorktreeAction `json:"action"`

	// Base names what a created worktree starts from, or "".
	Base string `json:"base"`

	// Dirty is how many files have uncommitted changes in an existing worktree, or -1
	// when unknown.
	Dirty int `json:"dirty"`
}
