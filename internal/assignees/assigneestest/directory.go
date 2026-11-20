// Package assigneestest is the shared fake of internal/assignees for the sheets' tests: a
// Directory in memory. Only tests import it.
package assigneestest

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Directory is an assignees.Directory in memory: a read kept makes the whole list current,
// or covers the search's words, and lists the people it found; a whole list kept that named
// everyone is complete. It is safe for concurrent use.
type Directory struct {
	mu sync.Mutex

	known    assignees.Known
	covered  map[string]bool
	kept     []string
	failed   []string
	assigned []string
}

// NewDirectory returns a Directory that knows no one, with a read of the whole list due.
func NewDirectory() *Directory {
	return &Directory{known: assignees.Known{Stale: true}, covered: map[string]bool{}}
}

// Known returns what the directory knows.
func (d *Directory) Known(context.Context, repo.Name) (assignees.Known, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	k := d.known
	k.Assignable = slices.Clone(k.Assignable)

	return k, nil
}

// Covers reports a search for query kept, by its words lower-cased.
func (d *Directory) Covers(_ context.Context, _ repo.Name, query string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.covered[strings.ToLower(query)], nil
}

// Keep keeps what a read for query found.
func (d *Directory) Keep(_ context.Context, _ repo.Name, query string, found assignees.Found) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.kept, d.covered[strings.ToLower(query)] = append(d.kept, query), true

	if query == "" {
		d.known.Stale, d.known.Total, d.known.Complete = false, found.Total, len(found.People) >= found.Total
	}

	for _, p := range found.People {
		d.known.Assignable = append(d.known.Assignable, assignees.Assignable{Login: p.Login, Name: p.Name, Listed: true})
	}

	return nil
}

// MarkFailed records a read for query that failed: one of the whole list holds the next off.
func (d *Directory) MarkFailed(_ context.Context, _ repo.Name, query string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.failed = append(d.failed, query)

	if query == "" {
		d.known.Stale = false
	}

	return nil
}

// Assigned records logins as assigned.
func (d *Directory) Assigned(_ context.Context, _ repo.Name, logins []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.assigned = append(d.assigned, logins...)

	return nil
}

// Kept lists the words of every read kept, "" for the whole list, in order.
func (d *Directory) Kept() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.kept)
}

// Failed lists the words of every read that failed, in order.
func (d *Directory) Failed() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.failed)
}

// SeenAssigned lists everyone the directory was told is assigned, in order.
func (d *Directory) SeenAssigned() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	return slices.Clone(d.assigned)
}
