package desktop

import "github.com/mrz1836/nilbox/internal/execpath"

// Unquarantine takes the quarantine mark off every file of the bundle at bundle.
func Unquarantine(bundle string) error {
	return unquarantine(bundle)
}

// Translocates reports whether macOS would run the bundle at bundle from a hidden copy, by
// its quarantine mark.
func Translocates(bundle string) (bool, error) {
	return translocates(bundle)
}

// NewMoverMarked is NewMover with marked standing in for the read of a copy's quarantine
// mark.
func NewMoverMarked(runner execpath.Runner, shell *Shell, marked func(bundle string) (bool, error)) *Mover {
	m := NewMover(runner, shell)
	m.marked = marked

	return m
}

// NewMoverClearing is NewMover with unmark standing in for the removal of the quarantine
// mark, so a test of the commands needs no copy on disk.
func NewMoverClearing(runner execpath.Runner, shell *Shell, unmark func(bundle string) error) *Mover {
	m := NewMover(runner, shell)
	m.unmark = unmark

	return m
}

// SignedLike has the Mover check a copy against the designated requirement of the bundle at
// bundle, in place of this process's, as a test's own process is no build of the app.
func (m *Mover) SignedLike(bundle string) *Mover {
	m.self = bundle

	return m
}
