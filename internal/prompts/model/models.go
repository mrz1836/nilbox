// Package model holds the persisted prompt library.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import (
	"time"

	profilesmodel "github.com/mrz1836/nilbox/internal/profiles/model"
)

// Prompt is one saved prompt: a Go text/template with its title, its short nickname, the
// kind of item it applies to, and the launch settings it sets when chosen. Model and
// Effort are "" and PlanMode and Worktree nil when the prompt inherits them.
type Prompt struct {
	ID         int64                    `gorm:"column:id;primaryKey"`
	Name       string                   `gorm:"column:name"`
	Nickname   string                   `gorm:"column:nickname"`
	AppliesTo  string                   `gorm:"column:applies_to"`
	Body       string                   `gorm:"column:body"`
	Model      string                   `gorm:"column:model"`
	Effort     string                   `gorm:"column:effort"`
	PlanMode   *bool                    `gorm:"column:plan_mode"`
	Worktree   *bool                    `gorm:"column:worktree"`
	ExtraArgs  profilesmodel.StringList `gorm:"column:extra_args"`
	StarterKey string                   `gorm:"column:starter_key"`
	CreatedAt  time.Time                `gorm:"column:created_at"`
	UpdatedAt  time.Time                `gorm:"column:updated_at"`
}

// TableName maps Prompt to its table.
func (Prompt) TableName() string { return "prompts" }

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&Prompt{}}
}
