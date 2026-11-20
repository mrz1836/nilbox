package describeit

import (
	"context"
	"sync"
)

// Worker is what Describe it does in the background while the app runs, which the shell
// runs as a deferred worker and core tells of changes: none of it is the window's to
// call, so it is a type of its own beside the bound Service.
type Worker struct {
	s *Service

	// changed wakes the snapshot's writer, once per burst of changes, and, while Autopilot
	// is off, the worker's read of its switch.
	changed chan struct{}
	once    sync.Once
}

// NewWorker returns the background worker of s.
func NewWorker(s *Service) *Worker {
	return &Worker{s: s, changed: make(chan struct{}, 1)}
}

// Changed says something the snapshot holds may have changed: a routine, a setting
// (Autopilot (beta) among them), a rule, or the inbox. The worker writes the snapshot again
// soon after, once per burst, or, while Autopilot is off, reads its switch again. It never
// blocks.
func (w *Worker) Changed() {
	w.s.forget()

	select {
	case w.changed <- struct{}{}:
	default:
	}
}

// Run does Describe it's background work until ctx ends, while Autopilot (beta) is on: it
// readies the describe folder (an interrupted turn closed, old conversations pruned, the
// snapshot and the instructions written), keeps the plugin's folder and its link current,
// follows the outside folder for new proposals, and writes the snapshot again after each
// burst of changes. While Autopilot is off, as on a fresh database, it writes nothing and
// follows nothing: it reads the switch as it starts and again after each change it is told
// of, catches up on all of the above once Autopilot is turned on, and stops again once it
// is turned off. As ctx ends, it stops a turn still running.
func (w *Worker) Run(ctx context.Context) {
	w.once.Do(func() {
		defer w.s.shutdown()

		for w.s.awaitOn(ctx, w.changed) {
			w.s.whileOn(ctx, w.changed)
		}
	})
}
