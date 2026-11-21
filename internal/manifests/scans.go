package manifests

import "context"

// Scans is the desktop worker that holds the Deps page's calls to the app's lifetime: each
// List call, and the read of the clones it shares, which runs apart from the window's call
// that started it, within ScanTimeout, so a window that asks again doesn't throw it away.
// The app's quit ends them all, and Run returns only once every one under way has, so the
// desktop shell, which waits for its workers before it closes, never leaves one reading
// behind it. It is not bound.
type Scans struct {
	s *Service
}

// NewScans returns the worker that holds s's calls and reads to the app's lifetime.
func NewScans(s *Service) Scans {
	return Scans{s: s}
}

// Run waits for ctx to end, then ends every call and read under way, refuses any later
// one, and returns once those under way have.
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

// hold counts a call or a read under way, which cancel ends, for Scans to end and wait
// for, and returns what counts it done; once the app is quitting, it refuses it.
func (s *Service) hold(cancel context.CancelFunc) (func(), error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()

	if s.quitting {
		return nil, errQuitting
	}

	s.lastHeld++
	n := s.lastHeld
	s.cancels[n] = cancel
	s.going.Add(1)

	return func() {
		s.lifeMu.Lock()
		delete(s.cancels, n)
		s.lifeMu.Unlock()

		s.going.Done()
	}, nil
}
