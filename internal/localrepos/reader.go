package localrepos

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// DefaultReadLimit is how long a read of one of the other repository locations may take
// before the location is skipped as not answering.
const DefaultReadLimit = 5 * time.Second

// ErrNotAnswering is returned for a location whose read took longer than its Reader's
// limit, as on a network share that hangs.
var ErrNotAnswering = errors.New("the location didn't answer in time")

// Reader bounds how long a read of one of the other repository locations may take, and
// keeps one read of each location going at a time. A location on a network share that
// hangs is then skipped as not answering (StateNotAnswering), rather than stalling every
// listing and launch. The read that hangs is left to end on its own, and every later read
// of the location joins it until it does, so reads of it never stack up. Resolving a
// location's links and classifying a folder to add touch it too, and are bounded the same
// way, one of each at a time. The clone root is never read through a Reader. Build one
// with NewReader; a nil *Reader reads directly, with no limit.
type Reader struct {
	limit time.Duration

	// read reads a location, resolve resolves a path's links, and addable classifies a
	// folder to add: readPlace, paths.Resolved, and classifyAddable, which tests swap.
	read    func(Place) placeRead
	resolve func(string) string
	addable func(dir, home string) (Kind, error)

	// mu guards going, the reads under way.
	mu    sync.Mutex
	going map[readJob]*pending
}

// NewReader returns a Reader that waits at most limit for a read, or DefaultReadLimit when
// limit isn't positive.
func NewReader(limit time.Duration) *Reader {
	if limit <= 0 {
		limit = DefaultReadLimit
	}

	return &Reader{
		limit: limit, read: readPlace, resolve: paths.Resolved, addable: classifyAddable, going: map[readJob]*pending{},
	}
}

// readOp is what a Reader does with a location.
type readOp int

const (
	// opRead lists a location's clones (readPlace).
	opRead readOp = iota

	// opResolve resolves its path's links (paths.Resolved).
	opResolve

	// opAddable classifies a folder to add and checks it can be a location.
	opAddable
)

// readJob is one read a Reader runs: what it does, to which location, and, for opAddable,
// the home it words its refusal with.
type readJob struct {
	op    readOp
	place Place
	home  string
}

// pending is a read under way: done closes once its result is in.
type pending struct {
	done   chan struct{}
	result any
}

// addable is what opAddable found: the folder's kind, or why it can't be a location.
type addable struct {
	kind Kind
	err  error
}

// Addable works out what the folder at dir is and checks that it can be a repository
// location, as Classify and then CheckAddable do, waiting at most the limit, or until ctx
// ends. A folder that takes longer is refused with ErrNotAnswering, in words the window
// shows, with home written as ~. A nil Reader waits for them.
func (r *Reader) Addable(ctx context.Context, dir, home string) (Kind, error) {
	if r == nil {
		return classifyAddable(dir, home)
	}

	job := readJob{op: opAddable, place: Place{Path: dir}, home: home}

	result, err := r.await(ctx.Done(), job, func() any {
		kind, addErr := r.addable(dir, home)

		return addable{kind: kind, err: addErr}
	})

	switch {
	case errors.Is(err, ErrNotAnswering):
		return "", uierr.New(paths.Tilde(dir, home)+" didn't answer in time.", fmt.Errorf("%w: %s", ErrNotAnswering, dir))
	case err != nil:
		return "", ctx.Err()
	}

	found, _ := result.(addable)

	return found.kind, found.err
}

// Resolved returns dir resolved through its links, as paths.Resolved does, and true, or,
// when that takes longer than the limit, dir cleaned and false. A nil Reader waits for it.
func (r *Reader) Resolved(dir string) (string, bool) {
	if r == nil {
		return paths.Resolved(dir), true
	}

	// No context ends it: the limit alone does.
	result, err := r.await(nil, readJob{op: opResolve, place: Place{Path: dir}}, func() any { return r.resolve(dir) })
	if err != nil {
		return filepath.Clean(dir), false
	}

	resolved, _ := result.(string)

	return resolved, true
}

// within reads the location p as readPlace does, waiting at most the limit. A read that
// takes longer reads as StateNotAnswering, and so does one ctx ends first, with ctx's
// error.
func (r *Reader) within(ctx context.Context, p Place) placeRead {
	if r == nil {
		return readPlace(p)
	}

	result, err := r.await(ctx.Done(), readJob{op: opRead, place: p}, func() any { return r.read(p) })

	switch {
	case errors.Is(err, ErrNotAnswering):
		return placeRead{state: StateNotAnswering}
	case err != nil:
		return placeRead{state: StateNotAnswering, err: ctx.Err()}
	}

	read, _ := result.(placeRead)

	return read
}

// errEnded is a wait that ended, its context done, before the read did.
var errEnded = errors.New("the wait ended")

// await joins the read j already under way, or starts it with fn, and waits for it at
// most the limit (ErrNotAnswering) or until ended closes (errEnded); a nil ended never
// does.
func (r *Reader) await(ended <-chan struct{}, j readJob, fn func() any) (any, error) {
	job := r.start(j, fn)

	timer := time.NewTimer(r.limit)
	defer timer.Stop()

	select {
	case <-job.done:
		return job.result, nil
	case <-timer.C:
	case <-ended:
		return nil, errEnded
	}

	// The limit passed: a result that came in at the same moment still counts.
	select {
	case <-job.done:
		return job.result, nil
	default:
		return nil, ErrNotAnswering
	}
}

// start returns the read j under way, or starts it with fn in a goroutine of its own that
// keeps its result, closes done, and leaves going once it ends.
func (r *Reader) start(j readJob, fn func() any) *pending {
	r.mu.Lock()
	defer r.mu.Unlock()

	if job, ok := r.going[j]; ok {
		return job
	}

	job := &pending{done: make(chan struct{})}
	r.going[j] = job

	go func() {
		job.result = fn()
		close(job.done)

		r.mu.Lock()
		delete(r.going, j)
		r.mu.Unlock()
	}()

	return job
}

// classifyAddable is Classify, then CheckAddable.
func classifyAddable(dir, home string) (Kind, error) {
	kind, err := Classify(dir, home)
	if err != nil {
		return "", err
	}

	if err = CheckAddable(dir, home); err != nil {
		return "", err
	}

	return kind, nil
}
