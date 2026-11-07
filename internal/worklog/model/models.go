// Package model holds the persisted record of worktree launches and cleanups.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import (
	"time"

	"github.com/mrz1836/nilbox/internal/item"
)

// WorktreeLaunch is one worktree the app started an agent session in, and the pull
// request or issue it is for.
type WorktreeLaunch struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	ClonePath  string    `gorm:"column:clone_path"`
	Worktree   string    `gorm:"column:worktree"`
	Owner      string    `gorm:"column:owner"`
	Name       string    `gorm:"column:name"`
	Kind       item.Kind `gorm:"column:kind"`
	Number     int       `gorm:"column:number"`
	LaunchedAt time.Time `gorm:"column:launched_at"`
}

// TableName maps WorktreeLaunch to its table.
func (WorktreeLaunch) TableName() string { return "worktree_launches" }

// WorktreeCleanup is one worktree the app cleaned up, and the item it was for.
type WorktreeCleanup struct {
	ID            int64     `gorm:"column:id;primaryKey"`
	CleanedAt     time.Time `gorm:"column:cleaned_at"`
	Automatic     bool      `gorm:"column:automatic"`
	Repo          string    `gorm:"column:repo"`
	Worktree      string    `gorm:"column:worktree"`
	DisplayPath   string    `gorm:"column:display_path"`
	DeletedBranch string    `gorm:"column:deleted_branch"`
	Head          string    `gorm:"column:head"`
	ItemKind      string    `gorm:"column:item_kind"`
	ItemOwner     string    `gorm:"column:item_owner"`
	ItemName      string    `gorm:"column:item_name"`
	ItemNumber    int       `gorm:"column:item_number"`
	ItemTitle     string    `gorm:"column:item_title"`
	ItemURL       string    `gorm:"column:item_url"`
	ItemState     string    `gorm:"column:item_state"`
}

// TableName maps WorktreeCleanup to its table.
func (WorktreeCleanup) TableName() string { return "worktree_cleanups" }

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&WorktreeLaunch{}, &WorktreeCleanup{}}
}
