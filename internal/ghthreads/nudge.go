package ghthreads

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/item"
)

// The follow-up of an item the inbox told of merged or closed. Its thread is cleared as
// soon as the state says so, but GitHub tells of the merge or the close itself a little
// after it, and of what follows it (a bot's comment, a release noted), and each brings a
// thread marked done back to the user's inbox on GitHub. A timed pass, every few minutes,
// and GitHub's own lag on the listing would leave it there for ten minutes or more, which
// is how long the user would see a notification of something they know is finished.
const (
	// FollowUpFor is how long after the inbox told of an item merged or closed the Syncer
	// keeps looking for its thread coming back.
	FollowUpFor = 10 * time.Minute

	// FollowUpEvery is how often a follow-up run lists the notifications and decides the
	// threads followed up, while any is: never sooner than GitHub's poll interval, as a
	// nudged run lists only once the mirror is that old.
	FollowUpEvery = 2 * time.Minute
)

// followUp is an item whose thread a follow-up run decides again, until.
type followUp struct {
	ref   item.Ref
	until time.Time
}

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

	// followUps are the items followed up, by key, and followAt when the next follow-up
	// run is due, zero while none is followed up.
	followUps map[string]followUp
	followAt  time.Time

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

// follow has the threads about refs, items the inbox told of merged or closed at now,
// decided again by a run every FollowUpEvery until FollowUpFor after now, and wakes the
// Syncer to count it in. An item followed up already is followed up anew.
func (n *nudges) follow(refs []item.Ref, now time.Time) {
	if len(refs) == 0 {
		return
	}

	n.mu.Lock()

	if n.followUps == nil {
		n.followUps = make(map[string]followUp, len(refs))
	}

	for _, ref := range refs {
		n.followUps[ref.Key()] = followUp{ref: ref, until: now.Add(FollowUpFor)}
	}

	if n.followAt.IsZero() {
		n.followAt = now.Add(FollowUpEvery)
	}

	n.mu.Unlock()

	n.signal()
}

// takeFollowUp returns the follow-up run due at now: a nudged run deciding the threads
// about the items still followed up, in the order of their keys, the next due
// FollowUpEvery later; ok is false when none is due, or none is followed up any more.
func (n *nudges) takeFollowUp(now time.Time) (passRequest, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.followAt.IsZero() || n.followAt.After(now) {
		return passRequest{}, false
	}

	var refs []item.Ref

	for _, key := range slices.Sorted(maps.Keys(n.followUps)) {
		f := n.followUps[key]
		if !now.Before(f.until) {
			delete(n.followUps, key)

			continue
		}

		refs = append(refs, f.ref)
	}

	if len(refs) == 0 {
		n.followAt = time.Time{}

		return passRequest{}, false
	}

	n.followAt = now.Add(FollowUpEvery)

	return passRequest{nudged: true, refs: refs}, true
}

// untilFollowUp says how long after now the next follow-up run is due; ok is false while
// none is followed up.
func (n *nudges) untilFollowUp(now time.Time) (time.Duration, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.followAt.IsZero() {
		return 0, false
	}

	return n.followAt.Sub(now), true
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

// takeFollowUp returns the follow-up run due now, if any (see nudges.takeFollowUp).
func (s *Service) takeFollowUp() (passRequest, bool) {
	return s.nudges.takeFollowUp(s.clock.Now())
}

// untilFollowUp says how long until the next follow-up run is due; ok is false while none
// is followed up.
func (s *Service) untilFollowUp() (time.Duration, bool) {
	return s.nudges.untilFollowUp(s.clock.Now())
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
