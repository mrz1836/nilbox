// Package model holds the persisted user preferences.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import "time"

// Pref is one user preference stored as a key/value pair.
type Pref struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Value     string    `gorm:"column:value"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName maps Pref to its table.
func (Pref) TableName() string { return "prefs" }

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&Pref{}}
}
