package store

import (
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
)

// Row is one row a Snapshot read, by column name, each value as the driver read it:
// int64, float64, string, []byte, time.Time for a DATETIME column, or nil for NULL. A
// column the table lacks is missing, and each accessor gives it its empty value, so a
// caller reads a backup any build wrote the same way.
type Row map[string]any

// Text returns a column as text: a number in decimal, a time as RFC 3339, or "" for NULL,
// a missing column, or any other value.
func (r Row) Text(column string) string {
	switch v := r[column].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case time.Time:
		return clock.Format(v)
	default:
		return ""
	}
}

// Int returns an integer column, and whether it holds one.
func (r Row) Int(column string) (int64, bool) {
	v, ok := r[column].(int64)

	return v, ok
}

// Bool returns a boolean column, stored as 0 or 1, or nil for NULL, a missing column, or
// any other value.
func (r Row) Bool(column string) *bool {
	switch v := r[column].(type) {
	case int64:
		on := v != 0

		return &on
	case bool:
		return &v
	default:
		return nil
	}
}

// Strings returns a JSON array of strings, or an empty list when the column holds none.
func (r Row) Strings(column string) []string {
	raw := r.Text(column)
	if raw == "" {
		return []string{}
	}

	list, err := DecodeJSON[[]string](raw)
	if err != nil || list == nil {
		return []string{}
	}

	return list
}

// timeLayouts are the forms a stored time may take as text.
func timeLayouts() []string {
	return []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", time.DateTime}
}

// Time returns a time column as RFC 3339, or "" when it holds no time.
func (r Row) Time(column string) string {
	switch v := r[column].(type) {
	case time.Time:
		return clock.Format(v)
	case string:
		for _, layout := range timeLayouts() {
			if t, err := time.Parse(layout, v); err == nil {
				return clock.Format(t)
			}
		}

		return ""
	default:
		return ""
	}
}

// StringMap returns a JSON object of strings, or an empty map when the column holds none.
func (r Row) StringMap(column string) map[string]string {
	return decodeObject[string](r.Text(column))
}

// StringLists returns a JSON object of string arrays, or an empty map when the column
// holds none.
func (r Row) StringLists(column string) map[string][]string {
	return decodeObject[[]string](r.Text(column))
}

// Has reports whether the row holds column: whether the table it was read from has it.
func (r Row) Has(column string) bool {
	_, ok := r[column]

	return ok
}

// decodeObject decodes a JSON object of V values, or returns an empty map when raw is
// not one.
func decodeObject[V any](raw string) map[string]V {
	if raw == "" {
		return map[string]V{}
	}

	m, err := DecodeJSON[map[string]V](raw)
	if err != nil || m == nil {
		return map[string]V{}
	}

	return m
}
