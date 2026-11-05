// Package model holds the persisted coding-agent launch profiles.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrUnsupportedScanType is returned when a JSON column holds neither text nor bytes.
var ErrUnsupportedScanType = errors.New("unsupported column value for a JSON field")

// ClaudeProfile is one launch profile: which config directory to run the agent with and
// the flags it starts with.
type ClaudeProfile struct {
	ID             int64      `gorm:"column:id;primaryKey"`
	Name           string     `gorm:"column:name"`
	ConfigDir      string     `gorm:"column:config_dir"`
	Model          string     `gorm:"column:model"`
	Effort         string     `gorm:"column:effort"`
	PermissionMode string     `gorm:"column:permission_mode"`
	PlanMode       bool       `gorm:"column:plan_mode"`
	AddDirs        StringList `gorm:"column:add_dirs"`
	ExtraArgs      StringList `gorm:"column:extra_args"`
	Env            StringMap  `gorm:"column:env"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

// TableName maps ClaudeProfile to its table.
func (ClaudeProfile) TableName() string { return "claude_profiles" }

// StringList is a list of strings stored as a JSON array. A nil list is stored as [].
//
// Value has a value receiver so a plain field is a driver.Valuer; Scan needs a pointer.
type StringList []string //nolint:recvcheck // Valuer on the value, Scanner on the pointer

// Value encodes the list as a JSON array.
func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}

	return encodeJSON(l)
}

// Scan decodes a JSON array.
func (l *StringList) Scan(src any) error {
	return decodeJSON(src, l)
}

// StringMap is a string-to-string map stored as a JSON object. A nil map is stored as {}.
//
// Value has a value receiver so a plain field is a driver.Valuer; Scan needs a pointer.
type StringMap map[string]string //nolint:recvcheck // Valuer on the value, Scanner on the pointer

// Value encodes the map as a JSON object.
func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}

	return encodeJSON(m)
}

// Scan decodes a JSON object.
func (m *StringMap) Scan(src any) error {
	return decodeJSON(src, m)
}

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&ClaudeProfile{}}
}

// encodeJSON marshals v to a JSON string column value.
func encodeJSON(v any) (driver.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode JSON column: %w", err)
	}

	return string(b), nil
}

// decodeJSON unmarshals a text or byte column value into dst.
func decodeJSON(src, dst any) error {
	var raw []byte

	switch v := src.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedScanType, src)
	}

	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("decode JSON column: %w", err)
	}

	return nil
}
