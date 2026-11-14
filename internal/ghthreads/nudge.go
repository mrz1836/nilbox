package ghthreads

import (
	"sync"

	"github.com/mrz1836/nilbox/internal/item"
)

// nudges holds, between the Syncer's runs, what it was asked to run soon: the items whose
// threads are decided again (what the inbox told the Settler of), those whose state is
// checked first (one the inbox saw close or leave its lists), and whether the rules changed,
// which decides every thread again. What is asked for while a run goes on waits for the
// next.
type nudges struct {
	mu      sync.Mutex
	pending bool
	refs    []item.Ref
	check   []item.Ref
	all     bool

	// wake tells the Syncer to look again: a nudge waits, or a clear was queued to go now.
	wake chan struct{}
}

// add asks for a run that decides refs, checks the state of check first, and, when all,
// decides every thread, and wakes the Syncer.
func (n *nudges) add(refs, check []item.Ref, all bool) {
	n.mu.Lock()
	n.pending = true
	n.refs = append(n.refs, refs...)
	n.check = append(n.check, check...)
	n.all = n.all || all
	n.mu.Unlock()

	n.signal()
}

// signal wakes the Syncer, without waiting: a wake already waiting covers this one.
func (n *nudges) signal() {
	select {
	case n.wake <- struct{}{}:
	default:
	}
}

// take returns the run asked for, and forgets it; ok is false when none was.
func (n *nudges) take() (passRequest, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.pending {
		return passRequest{}, false
	}

	req := passRequest{nudged: true, refs: n.refs, check: n.check, all: n.all}
	n.pending, n.refs, n.check, n.all = false, nil, nil, false

	return req, true
}

// takeNudge returns the run the Syncer was nudged to, and forgets it; ok is false when none
// waits.
func (s *Service) takeNudge() (passRequest, bool) {
	return s.nudges.take()
}

// woken is what wakes the Syncer between its runs: a nudge, or a clear queued to go now.
func (s *Service) woken() <-chan struct{} {
	return s.nudges.wake
}

// rulesChanged asks for a run that decides every thread again, under the rules as they are
// now.
func (s *Service) rulesChanged() {
	s.nudges.add(nil, nil, true)
}
