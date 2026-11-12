// Package clonelock keeps one clone-changing job at a time on each clone: a worktree made
// or removed, a release, a hooks install, a checks run, a script, or a pull. A tool updating
// itself, or being installed, holds its folder the same way. A job that finds its clone
// busy learns what holds it, which the window shows: "A release is using acme-org/sdk
// right now."
//
// A clone is keyed by paths.Resolved, so a path through a symbolic link shares the lock of
// the folder it names. The locks are in-process: they keep the app's own jobs apart, not
// another program's.
package clonelock

import (
	"cmp"
	"context"
	"errors"
	"sync"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
)

// Kind is the kind of job that holds a clone.
type Kind string

// The kinds of job that hold a clone.
const (
	KindRelease  Kind = "release"
	KindHooks    Kind = "hooks"
	KindChecks   Kind = "checks"
	KindScript   Kind = "script"
	KindWorktree Kind = "worktree"

	// KindPull is a clone's branch pulled from origin, fast-forward only.
	KindPull Kind = "gitPull"

	// KindUpdate is a tool updating itself, which holds the folder its copy is in, and
	// KindInstall the app installing a tool, which holds the folder it places it in.
	KindUpdate  Kind = "update"
	KindInstall Kind = "install"

	// KindOrphan is a run from before the app restarted, still running, which the app
	// follows under a placeholder lock.
	KindOrphan Kind = "orphan"
)

// ErrBusy is matched by every *BusyError.
var ErrBusy = errors.New("the clone is busy")

// Holder says what holds a clone, for the user.
type Holder struct {
	Kind Kind

	// Repo is what the window calls the clone: its repository on GitHub, as in
	// "acme-org/sdk", or else its folder. Empty, the sentence says "this clone".
	Repo string

	// Title is the run's title, and RunID the run, for the window to offer; a worktree
	// change has neither.
	Title string
	RunID int64
}

// Sentence says what the holder is doing, for the user: "A release is using acme-org/sdk
// right now."
func (h Holder) Sentence() string {
	clone := cmp.Or(h.Repo, "this clone")

	return h.doer() + " is using " + clone + " right now."
}

// doer names the holder's job as a sentence's subject.
func (h Holder) doer() string {
	switch h.Kind {
	case KindRelease:
		return "A release"
	case KindHooks:
		return "A hooks install"
	case KindChecks:
		return "A checks run"
	case KindScript:
		return "A script"
	case KindWorktree:
		return "A worktree change"
	case KindPull:
		return "A pull"
	case KindUpdate:
		return "A tool update"
	case KindInstall:
		return "A tool install"
	case KindOrphan:
		return "A run from before " + brand.Name + " restarted"
	default:
		return "Another job"
	}
}

// BusyError is returned for a clone another job holds, naming it. Its text is the
// holder's Sentence.
type BusyError struct {
	Holder Holder
}

// Error returns the holder's Sentence.
func (e *BusyError) Error() string {
	return e.Holder.Sentence()
}

// Is reports ErrBusy.
func (e *BusyError) Is(target error) bool {
	return target == ErrBusy
}

// Set holds a lock per clone. The zero Set is ready to use, and it is safe for concurrent
// use. One Set serves the whole app, so every job that changes a clone sees every other.
type Set struct {
	mu   sync.Mutex
	held map[string]*hold
}

// hold is one job's hold on a clone.
type hold struct {
	holder Holder

	// done is closed when the job lets go.
	done chan struct{}
}

// New returns an empty Set.
func New() *Set {
	return &Set{}
}

// TryLock takes path's clone for h when it is free, and returns the release, which may be
// called more than once. When another job holds the clone, it returns that job's Holder
// and false, without waiting.
func (s *Set) TryLock(path string, h Holder) (release func(), held Holder, ok bool) {
	key := paths.Resolved(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	if cur, busy := s.held[key]; busy {
		return nil, cur.holder, false
	}

	return s.take(key, h), Holder{}, true
}

// Lock takes path's clone for h, waiting while another job holds it, until ctx ends.
func (s *Set) Lock(ctx context.Context, path string, h Holder) (release func(), err error) {
	return s.LockWaitingFor(ctx, path, h, func(Holder) bool { return true })
}

// LockWaitingFor takes path's clone for h. While a job that waitFor accepts holds it, it
// waits, until ctx ends; a job waitFor refuses fails it at once with a *BusyError naming
// that job. It fails at once, too, when ctx has already ended. A ctx that ends fails it
// with ctx.Err() itself, as a semaphore does, so callers word it as they word any other.
func (s *Set) LockWaitingFor(ctx context.Context, path string, h Holder, waitFor func(held Holder) bool,
) (release func(), err error) {
	key := paths.Resolved(path)

	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}

		s.mu.Lock()

		cur, busy := s.held[key]
		if !busy {
			release = s.take(key, h)
			s.mu.Unlock()

			return release, nil
		}

		s.mu.Unlock()

		if !waitFor(cur.holder) {
			return nil, &BusyError{Holder: cur.holder}
		}

		select {
		case <-cur.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Holder returns the job that holds path's clone, and true, or false when it is free.
func (s *Set) Holder(path string) (Holder, bool) {
	key := paths.Resolved(path)

	s.mu.Lock()
	defer s.mu.Unlock()

	if cur, busy := s.held[key]; busy {
		return cur.holder, true
	}

	return Holder{}, false
}

// take records h's hold on key, which s.mu guards, and returns its release.
func (s *Set) take(key string, h Holder) func() {
	if s.held == nil {
		s.held = map[string]*hold{}
	}

	cur := &hold{holder: h, done: make(chan struct{})}
	s.held[key] = cur

	return sync.OnceFunc(func() {
		s.mu.Lock()
		delete(s.held, key) // nothing else replaces a hold while it is held
		s.mu.Unlock()

		close(cur.done)
	})
}
