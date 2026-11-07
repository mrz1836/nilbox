package clock

import "time"

// StampLayout is the layout of Stamp: a compact UTC time such as 20260102T150405Z.
const StampLayout = "20060102T150405Z"

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// System is the real clock.
type System struct{}

// Now returns the current time.
func (System) Now() time.Time { return time.Now() }

// Or returns c, or the real clock for nil.
func Or(c Clock) Clock {
	if c == nil {
		return System{}
	}

	return c
}

// Format writes t in UTC as RFC 3339, the way every time the UI or the database reads is
// written.
func Format(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// Stamp writes t in UTC in StampLayout, for file names.
func Stamp(t time.Time) string {
	return t.UTC().Format(StampLayout)
}
