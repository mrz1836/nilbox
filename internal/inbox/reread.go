package inbox

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
)

// rereadJob names the reads Rereads makes in log entries.
const rereadJob = "read an item a session worked on again"

// Rereads reads again, one at a time, the items whose agent sessions stopped working, as
// a desktop worker: the agent may have reviewed, commented, or pushed, so the card catches
// up as soon as the session stops, not at the next whole refresh. Each read is
// RefreshItem's, one GraphQL point, so a pull request the agent reviewed leaves Review
// requested at once. Items asked for again before their read starts are read once. It
// isn't bound: only the sessions' Watcher asks it, through core.
type Rereads struct {
	read   func(ctx context.Context, id int64) error
	logger *slog.Logger

	// wake tells Run that an item waits.
	wake chan struct{}

	// mu guards pending, the items waiting to be read.
	mu      sync.Mutex
	pending map[int64]bool
}

// NewRereads returns Rereads that read through s, logging failures to logger; a nil
// logger discards them.
func NewRereads(s *Service, logger *slog.Logger) *Rereads {
	return newRereads(func(ctx context.Context, id int64) error {
		_, err := s.RefreshItem(ctx, id)

		return err
	}, logger)
}

// newRereads returns Rereads that read each item through read.
func newRereads(read func(ctx context.Context, id int64) error, logger *slog.Logger) *Rereads {
	return &Rereads{read: read, logger: logging.Or(logger), wake: make(chan struct{}, 1), pending: map[int64]bool{}}
}

// Reread asks for the item with id to be read again. It never waits, and an ask made
// before Run starts is kept for it.
func (r *Rereads) Reread(id int64) {
	r.mu.Lock()
	r.pending[id] = true
	r.mu.Unlock()

	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run reads the items asked for, in the order of their IDs, until ctx is canceled, and
// returns once the read in flight has. A read that fails is logged, as quietly as a debug
// entry for an item no longer stored, and one that panics is recovered and logged, so the
// next still runs.
func (r *Rereads) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		}

		for _, id := range r.take() {
			if ctx.Err() != nil {
				return
			}

			r.readOne(ctx, id)
		}
	}
}

// take returns the items waiting, by ID, and forgets them.
func (r *Rereads) take() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	ids := slices.Sorted(maps.Keys(r.pending))
	clear(r.pending)

	return ids
}

// readOne reads the item with id again, logging a failure or a panic.
func (r *Rereads) readOne(ctx context.Context, id int64) {
	defer func() {
		if p := recover(); p != nil {
			r.logger.ErrorContext(ctx, rereadJob+" panicked", "item", id, "panic", p)
		}
	}()

	err := r.read(ctx, id)

	switch {
	case err == nil || ctx.Err() != nil:
	case errors.Is(err, item.ErrNotFound):
		r.logger.DebugContext(ctx, rereadJob+": it is no longer stored", "item", id)
	default:
		r.logger.WarnContext(ctx, rereadJob+" failed", "item", id, "error", err)
	}
}
