// Package model holds the persisted launch defaults for orgs and repositories.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import (
	"time"

	profilesmodel "github.com/mrz1836/nilbox/internal/profiles/model"
)

// LaunchDefault is one org's (Name "") or one repository's launch defaults. Every setting
// inherits when it is "" or nil.
type LaunchDefault struct {
	ID            int64                    `gorm:"column:id;primaryKey"`
	Owner         string                   `gorm:"column:owner"`
	Name          string                   `gorm:"column:name"`
	Agent         string                   `gorm:"column:agent"`
	Profile       string                   `gorm:"column:profile"`
	PRPromptID    *int64                   `gorm:"column:pr_prompt_id"`
	IssuePromptID *int64                   `gorm:"column:issue_prompt_id"`
	Model         string                   `gorm:"column:model"`
	Effort        string                   `gorm:"column:effort"`
	PlanMode      *bool                    `gorm:"column:plan_mode"`
	Worktree      *bool                    `gorm:"column:worktree"`
	ExtraArgs     profilesmodel.StringList `gorm:"column:extra_args"`
	CreatedAt     time.Time                `gorm:"column:created_at"`
	UpdatedAt     time.Time                `gorm:"column:updated_at"`
}

// TableName maps LaunchDefault to its table.
func (LaunchDefault) TableName() string { return "launch_defaults" }

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&LaunchDefault{}}
}
