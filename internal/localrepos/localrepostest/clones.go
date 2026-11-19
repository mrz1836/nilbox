// Package localrepostest is the fake of the clone root the suites of the services that
// read it share (hooks, scripts, and release): Clones, the clone root and the other
// repository locations as a service's Roots, the clones listed in them, and each clone's
// GitHub origin.
//
// It is test support only: only _test.go files may import it, which a repoguard test
// enforces, so none of it reaches the app.
package localrepostest

import (
	"context"
	"sync"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Clones is the clone root and the other repository locations, as a service reads them:
// Roots holds them, Dirs lists the clones in them as localrepos.Roots.Clones does, and
// Origin is each clone's GitHub repository, as SetOrigin recorded it. It counts each of
// the three reads, fails Roots or Dirs once a test says to, and calls a test's hooks as
// the clones are listed and as an origin is read. It is safe for concurrent use, and its
// zero value is a clone root of "" with no other location and no origin.
type Clones struct {
	mu sync.Mutex

	root  string
	extra []localrepos.Location

	rootsErr error
	dirsErr  error
	origins  map[string]repo.Name

	// onDirs, when set, is called as the clones are listed, and onOrigin as an origin is
	// read.
	onDirs   func()
	onOrigin func()

	reads Reads
}

// Reads counts what a Clones was asked: its Roots, its listings of the clones (Dirs), and
// the origins it was asked for.
type Reads struct {
	Roots   int
	Dirs    int
	Origins int
}

// New returns the clone root root, with no other location and no origin.
func New(root string) *Clones {
	return &Clones{root: root}
}

// Roots returns the clone root and the other locations, or the error FailRoots set.
func (c *Clones) Roots(context.Context) (localrepos.Roots, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.reads.Roots++

	if c.rootsErr != nil {
		return localrepos.Roots{}, c.rootsErr
	}

	return localrepos.NewRoots(c.root, c.extra, "")
}

// Dirs lists the clones in roots, as localrepos.Roots.Clones does, after calling the hook
// set for it, or returns the error FailDirs set.
func (c *Clones) Dirs(ctx context.Context, roots localrepos.Roots) ([]string, error) {
	c.mu.Lock()
	c.reads.Dirs++
	err, hook := c.dirsErr, c.onDirs
	c.mu.Unlock()

	if hook != nil {
		hook()
	}

	if err != nil {
		return nil, err
	}

	found, err := roots.Clones(ctx)
	if err != nil {
		return nil, err
	}

	return found.Dirs, nil
}

// Origin returns the GitHub repository SetOrigin recorded for the clone at dir, and
// whether there is one, after calling the hook set for it.
func (c *Clones) Origin(_ context.Context, dir string) (repo.Name, bool, error) {
	c.mu.Lock()
	c.reads.Origins++
	name, ok := c.origins[dir]
	hook := c.onOrigin
	c.mu.Unlock()

	if hook != nil {
		hook()
	}

	return name, ok, nil
}

// SetRoot moves the clone root to root.
func (c *Clones) SetRoot(root string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.root = root
}

// SetExtra makes extra the other repository locations.
func (c *Clones) SetExtra(extra ...localrepos.Location) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.extra = extra
}

// FailRoots makes Roots fail with err from now on, or, with nil, read them again.
func (c *Clones) FailRoots(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.rootsErr = err
}

// FailDirs makes Dirs fail with err from now on, or, with nil, list the clones again.
func (c *Clones) FailDirs(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.dirsErr = err
}

// SetOrigin records name as the GitHub repository of the clone at dir.
func (c *Clones) SetOrigin(dir string, name repo.Name) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.origins == nil {
		c.origins = map[string]repo.Name{}
	}

	c.origins[dir] = name
}

// ForgetOrigin forgets the GitHub repository of the clone at dir, as for a clone whose
// origin is off GitHub.
func (c *Clones) ForgetOrigin(dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.origins, dir)
}

// Hook sets what Dirs calls as the clones are listed, and Origin as an origin is read;
// nil calls nothing.
func (c *Clones) Hook(onDirs, onOrigin func()) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.onDirs, c.onOrigin = onDirs, onOrigin
}

// Reads returns how often each read was asked so far.
func (c *Clones) Reads() Reads {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.reads
}
