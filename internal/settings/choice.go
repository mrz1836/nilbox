package settings

import (
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// choice is a setting that takes one of a closed set of values, such as the theme: its
// values, what a stored value that isn't one of them reads as, and the sentinel a setter
// refuses one with.
type choice[T ~string] struct {
	// values are the choices, in the order Settings offers them.
	values []T

	// fallback is what a stored value that is unset or no longer a choice reads as.
	fallback T

	// invalid is the sentinel check wraps for a value that is not a choice.
	invalid error
}

// valid reports whether v is one of the choices.
func (c choice[T]) valid(v T) bool {
	return slices.Contains(c.values, v)
}

// decode reads a stored value: as stored, when it is one of the choices, else the
// fallback.
func (c choice[T]) decode(stored string) T {
	if v := T(stored); c.valid(v) {
		return v
	}

	return c.fallback
}

// check refuses a value that is not one of the choices with the sentinel, naming the
// value and every choice: `unknown theme "dim": choose one of system, light, dark`.
func (c choice[T]) check(v T) error {
	if c.valid(v) {
		return nil
	}

	return fmt.Errorf("%w %q: choose one of %s", c.invalid, v, textutil.Join(c.values, ", "))
}
