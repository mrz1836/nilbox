package describeit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// snapshotTTL is how long the live snapshot is kept: the window reads a conversation up to
// four times a second while a turn runs, and each read checks its cards against the setup
// as it is now, which this keeps from reading the setup each time. A change the worker is
// told of forgets it at once.
const snapshotTTL = 2 * time.Second

// errNoSetup is returned for the snapshot of a service built without a Setup.
var errNoSetup = errors.New("describe it reads no setup")

// snapshotCache keeps the live snapshot for snapshotTTL. Its generation moves on with each
// change the worker is told of, so a snapshot read across a change is never kept, and one
// kept from before a change is no longer fresh.
type snapshotCache struct {
	// build lets one read of the setup run at a time; the readers that wait for it take
	// what it read.
	build sync.Mutex

	mu   sync.Mutex
	snap kit.Snapshot
	at   time.Time
	gen  uint64
	ok   bool

	generation atomic.Uint64
}

// kept returns the snapshot kept, when it is still fresh at now.
func (c *snapshotCache) kept(now time.Time) (kit.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.ok || c.gen != c.generation.Load() || now.Before(c.at) || now.Sub(c.at) >= snapshotTTL {
		return kit.Snapshot{}, false
	}

	return c.snap, true
}

// keep keeps snap, read at now in generation gen, unless a change came since.
func (c *snapshotCache) keep(snap kit.Snapshot, now time.Time, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if gen != c.generation.Load() {
		return
	}

	c.snap, c.at, c.gen, c.ok = snap, now, gen, true
}

// last returns the last snapshot read, however old, for when the setup can't be read.
func (c *snapshotCache) last() (kit.Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.snap, c.ok
}

// forget leaves the snapshot kept no longer fresh, as something it holds may have changed.
// It never blocks.
func (c *snapshotCache) forget() {
	c.generation.Add(1)
}

// forget leaves the live snapshot kept no longer fresh, as something it holds may have
// changed.
func (s *Service) forget() {
	s.snap.forget()
}

// liveSnapshot returns the snapshot of the setup as it is now: the one kept, while it is
// fresh, or one read from Setup and kept. Should the setup fail to read, the last snapshot
// read stands in for it, when there is one.
func (s *Service) liveSnapshot(ctx context.Context) (kit.Snapshot, error) {
	if snap, ok := s.snap.kept(s.clock.Now()); ok {
		return snap, nil
	}

	s.snap.build.Lock()
	defer s.snap.build.Unlock()

	if snap, ok := s.snap.kept(s.clock.Now()); ok {
		return snap, nil
	}

	gen := s.snap.generation.Load()

	snap, err := s.readSetup(ctx)
	if err != nil {
		if last, ok := s.snap.last(); ok {
			s.logger.WarnContext(ctx, "describe it couldn't read the setup; using the last snapshot", "error", err)

			return last, nil
		}

		return kit.Snapshot{}, err
	}

	s.snap.keep(snap, s.clock.Now(), gen)

	return snap, nil
}

// readSetup reads the snapshot from Setup.
func (s *Service) readSetup(ctx context.Context) (kit.Snapshot, error) {
	if s.cfg.Setup == nil {
		return kit.Snapshot{}, uierr.New("Describe it can't read your setup in this build.", errNoSetup)
	}

	snap, err := s.cfg.Setup.Snapshot(ctx)
	if err != nil {
		return kit.Snapshot{}, uierr.New("Describe it couldn't read your setup.", err)
	}

	return snap, nil
}

// writeContext writes the setup as it is now to the snapshot the tool mode reads.
func (s *Service) writeContext(ctx context.Context) error {
	snap, err := s.liveSnapshot(ctx)
	if err != nil {
		return err
	}

	data, err := kit.EncodeSnapshot(snap)
	if err != nil {
		return uierr.New("Your setup is too large for Describe it to hand "+agentWords+".", err)
	}

	if err = ensureDir(s.cfg.Dir); err != nil {
		return uierr.New("Describe it couldn't make its folder.", err)
	}

	if err = appdata.WriteFile(s.contextFile(), data); err != nil {
		return uierr.New("Describe it couldn't write your setup for "+agentWords+".", err)
	}

	return nil
}

// writeInstructions writes the in-app agent's instructions, from this build's words, when
// the file holds others.
func (s *Service) writeInstructions() error {
	if _, err := writeIfChanged(s.instructionsFile(), []byte(kit.SkillText(kit.InApp))); err != nil {
		return uierr.New("Describe it couldn't write its instructions for "+agentWords+".", err)
	}

	return nil
}
