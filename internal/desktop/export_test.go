package desktop

import "github.com/mrz1836/nilbox/internal/execpath"

// Unquarantine takes the quarantine mark off every file of the bundle at bundle.
func Unquarantine(bundle string) error {
	return unquarantine(bundle)
}

// NewMoverClearing is NewMover with unmark standing in for the removal of the quarantine
// mark, so a test of the commands needs no copy on disk.
func NewMoverClearing(runner execpath.Runner, shell *Shell, unmark func(bundle string) error) *Mover {
	m := NewMover(runner, shell)
	m.unmark = unmark

	return m
}
