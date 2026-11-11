package hooks

import (
	"context"
	"errors"
)

// errQuitting refuses a scan once the app is quitting.
var errQuitting = errors.New("the app is quitting")

// Scans is the desktop worker that holds the Hooks page's scans to the app's lifetime. A
// scan runs apart from the window's call that started it, within ScanTimeout, so a window
// that asks again doesn't throw it away; the app's quit ends it too, and Run returns only
// once every scan under way has, so the desktop shell, which waits for its workers before
// it closes the database, never leaves one running behind it. It is not bound.
type Scans struct {
	s *Service
}

// NewScans returns the worker that holds s's scans to the app's lifetime.
func NewScans(s *Service) Scans {
	return Scans{s: s}
}

// Run waits for ctx to end, then ends every scan under way, refuses any later one, and
// returns once those under way have.
func (w Scans) Run(ctx context.Context) {
	<-ctx.Done()

	w.s.lifeMu.Lock()
	w.s.quitting = true

	for _, cancel := range w.s.cancels {
		cancel()
	}

	w.s.lifeMu.Unlock()

	w.s.going.Wait()
}

// scanStarts counts a scan under way, which cancel ends, for Scans to end and wait for, and
// returns what counts it done; once the app is quitting, it refuses the scan.
func (s *Service) scanStarts(cancel context.CancelFunc) (func(), error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	if s.quitting {
		return nil, errQuitting
	}

	s.lastScan++
	n := s.lastScan
	s.cancels[n] = cancel
	s.going.Add(1)

	return func() {
		s.lifeMu.Lock()
		delete(s.cancels, n)
		s.lifeMu.Unlock()

		s.going.Done()
	}, nil
}
