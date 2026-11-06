// Package model holds the persisted record of worktree launches.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import "time"

// WorktreeLaunch is one worktree the app started an agent session in, and the pull
// request or issue it is for.
type WorktreeLaunch struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	ClonePath  string    `gorm:"column:clone_path"`
	Worktree   string    `gorm:"column:worktree"`
	Owner      string    `gorm:"column:owner"`
	Name       string    `gorm:"column:name"`
	Kind       string    `gorm:"column:kind"`
	Number     int       `gorm:"column:number"`
	LaunchedAt time.Time `gorm:"column:launched_at"`
}

// TableName maps WorktreeLaunch to its table.
func (WorktreeLaunch) TableName() string { return "worktree_launches" }

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&WorktreeLaunch{}}
}
