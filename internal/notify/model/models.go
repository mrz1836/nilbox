// Package model holds the persisted record of the notifications the app gave.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import "time"

// Notification is one thing the app told the user about an item.
type Notification struct {
	ID        int64      `gorm:"column:id;primaryKey"`
	Kind      string     `gorm:"column:kind"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	ReadAt    *time.Time `gorm:"column:read_at"`
	ItemID    *int64     `gorm:"column:item_id"`
	ItemKind  string     `gorm:"column:item_kind"`
	RepoOwner string     `gorm:"column:repo_owner"`
	RepoName  string     `gorm:"column:repo_name"`
	Number    int        `gorm:"column:number"`
	Title     string     `gorm:"column:title"`
	URL       string     `gorm:"column:url"`
	Author    string     `gorm:"column:author"`
	Detail    string     `gorm:"column:detail"`
}

// TableName maps Notification to its table.
func (Notification) TableName() string { return "notifications" }

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&Notification{}}
}
