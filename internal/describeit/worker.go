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

	// changed wakes the snapshot's writer, once per burst of changes.
	changed chan struct{}
	once    sync.Once
}

// NewWorker returns the background worker of s.
func NewWorker(s *Service) *Worker {
	return &Worker{s: s, changed: make(chan struct{}, 1)}
}

// Changed says something the snapshot holds may have changed: a routine, a setting, a
// rule, or the inbox. The worker writes the snapshot again soon after, once per burst. It
// never blocks.
func (w *Worker) Changed() {
	w.s.forget()

	select {
	case w.changed <- struct{}{}:
	default:
	}
}

// Run does Describe it's background work until ctx ends: it readies the describe folder
// (an interrupted turn closed, old conversations pruned, the snapshot and the
// instructions written), keeps the plugin's folder and its link current, follows the
// outside folder for new proposals, and writes the snapshot again after each burst of
// changes; as ctx ends, it stops a turn still running.
func (w *Worker) Run(ctx context.Context) {
	w.once.Do(func() {
		w.s.start(ctx)
		w.s.preparePlugin(ctx)

		done := make(chan struct{})

		go func() {
			defer close(done)

			w.s.followOutside(ctx)
		}()

		w.s.keepContext(ctx, w.changed)
		<-done
		w.s.shutdown()
	})
}
