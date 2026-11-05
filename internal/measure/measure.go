// Package measure times the app for its performance record: startup to the first painted
// frame, the batched inbox refresh, an agent launch, and the size of the app bundle.
//
// The magefile's measure targets and the desktop binary's measurement flags drive it.
// Everything here is plain Go, tested with fakes on any OS; only the live runs need a Mac,
// a GitHub login, and a terminal app. Each report renders as key=value lines, so a run's
// raw output can be kept next to the numbers derived from it.
package measure

import (
	"fmt"
	"slices"
	"time"
)

// Median returns the middle value of values, or the mean of the two middle values when
// the count is even. It is zero for no values, and values is left unchanged.
func Median[T ~int64](values []T) T {
	if len(values) == 0 {
		return 0
	}

	sorted := slices.Clone(values)
	slices.Sort(sorted)

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}

	return (sorted[mid-1] + sorted[mid]) / 2
}

// milliseconds formats a duration as whole milliseconds.
func milliseconds(d time.Duration) string {
	return fmt.Sprintf("%d", d.Milliseconds())
}

// seconds formats a duration as seconds with two decimals, e.g. "0.84".
func seconds(d time.Duration) string {
	return fmt.Sprintf("%.2f", d.Seconds())
}
