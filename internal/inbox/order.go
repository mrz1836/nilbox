package inbox

import (
	"context"
	"database/sql"
	"sync"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/store"
)

// The inbox fetches from GitHub without holding a lock, so a whole refresh, an item's
// own refresh, and a check of an item's files can all be fetching at once. What they
// store is ordered by when each fetch started, not by when it finished, so data is never
// replaced by data that GitHub sent earlier:
//
//   - Every fetch takes the next number of one sequence as it starts.
//   - Stores are serialized by writeOrder.mu, which is held only to write.
//   - A whole refresh that finishes after an item's own refresh or file check that
//     started later applies that item write again, on top of its own, in its own
//     transaction. So a pull request merged and refreshed on its own while a slower
//     refresh was fetching stays out of the lists, as if the two had run one after the
//     other. The refresh does not store its own, older, details and file check of an
//     item whose own refresh it applies again.
//   - An item's own refresh or file check that finishes after a whole refresh that
//     started later, and stored that item (or checked its files), stores nothing: the
//     refresh's data is the newer. Each item keeps the number of the last refresh that
//     stored it and the last that checked its files, which may be an earlier one when no
//     check was due since.
//   - A merge the user made from the review sheet unlists its item as an item write of its
//     own, numbered after GitHub answered the merge. It costs no request, a whole refresh
//     still fetching applies it again, and one that started later sees the pull request
//     merged and drops it (see dropClosed), so nothing ever supersedes it.
//   - Two pieces of work on one item never overlap at all: itemLocks runs them one at a
//     time.
//
// A refresh writes the triage columns only to end snoozes (see wakeSnoozed), comparing
// each running snooze's baseline with the details stored, inside its own transaction, so
// it sees the triage stored before it. Since it leaves the details of an item a newer
// write stored as they are, a snooze set after that write is compared with the details
// it was set against, never with the refresh's older ones.

// writeOrder keeps the stores of what GitHub said in the order their fetches started.
type writeOrder struct {
	// mu is held for each store of what GitHub said, and guards the fields below.
	mu sync.Mutex

	// next is the number the last fetch took.
	next uint64

	// refresh is the whole refresh fetching now, or nil.
	refresh *refreshInFlight

	// refreshed holds, for each item the last whole refresh listed, the number of the
	// last whole refresh that stored it, and checked the number of the last whole refresh
	// that stored its file check. Items no longer listed are dropped, so both stay as
	// small as the inbox.
	refreshed map[int64]uint64
	checked   map[int64]uint64
}

// refreshInFlight is a whole refresh between the start of its fetch and its store: its
// number, and the item writes stored meanwhile by fetches that started after it, in the
// order they were stored.
type refreshInFlight struct {
	seq     uint64
	replays []itemWrite
}

// supersedes reports whether a whole refresh that started after w already stored what w
// would: the item's card for an item's own refresh, its file check for a file check. A
// merge's unlisting is never superseded. The caller holds mu.
func (o *writeOrder) supersedes(w itemWrite) bool {
	switch {
	case w.unlist:
		return false
	case w.update != nil:
		return o.refreshed[w.id] > w.seq
	default:
		return o.checked[w.id] > w.seq
	}
}

// itemWrite is what one item's own refresh, a check of its files, or a merge stores:
// exactly one of update, files, and unlist is set.
type itemWrite struct {
	// id is the item's id, and seq the number its fetch took.
	id  int64
	seq uint64

	// update is what the item's own refresh read.
	update *itemUpdate

	// files is a file check's outcome.
	files *filesRow

	// unlist takes the item out of every list, after a merge.
	unlist bool
}

// itemUpdate is what an item's own refresh read: the account the item is stored for,
// GitHub's answer, and the outcome of the file check it ran, if one was due.
type itemUpdate struct {
	accountID int64
	update    *github.ItemUpdate
	files     map[string]fileCheck
}

// startFetch numbers a fetch for one item as it starts.
func (s *Service) startFetch() uint64 {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	s.order.next++

	return s.order.next
}

// startRefresh numbers a whole refresh's fetch as it starts and marks it in flight.
func (s *Service) startRefresh() uint64 {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	s.order.next++
	s.order.refresh = &refreshInFlight{seq: s.order.next}

	return s.order.next
}

// endRefresh marks the refresh with seq as no longer in flight, if it still is.
func (s *Service) endRefresh(seq uint64) {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	if r := s.order.refresh; r != nil && r.seq == seq {
		s.order.refresh = nil
	}
}

// takeReplays returns the item writes the refresh with seq must apply again, and marks it
// no longer in flight. The caller holds order.mu.
func (s *Service) takeReplays(seq uint64) []itemWrite {
	r := s.order.refresh
	if r == nil || r.seq != seq {
		return nil
	}

	s.order.refresh = nil

	return r.replays
}

// storeItemWrite stores w in one transaction, unless a whole refresh that started later
// already stored newer data about the item. It reports whether it stored w.
func (s *Service) storeItemWrite(ctx context.Context, w itemWrite) (bool, error) {
	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	if s.order.supersedes(w) {
		return false, nil
	}

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		_, err := s.applyItemWrite(ctx, tx, w, true)

		return err
	})
	if err != nil {
		return false, err
	}

	if r := s.order.refresh; r != nil && w.seq > r.seq {
		r.replays = append(r.replays, w)
	}

	return true, nil
}

// applyItemWrite writes w in tx and reports whether it took the item out of the lists.
// The first time, an item's own refresh records the GraphQL budget its query left; a
// replay does not record it again.
func (s *Service) applyItemWrite(ctx context.Context, tx *sql.Tx, w itemWrite, first bool) (bool, error) {
	switch {
	case w.unlist:
		return true, s.newWriter(tx).unlistMerged(ctx, w.id)
	case w.files != nil:
		return false, upsertFiles(ctx, s.q.WithTx(tx), *w.files)
	}

	u, it := w.update, w.update.update.Item
	wr := s.newWriter(tx)
	wr.accountID, wr.files = u.accountID, u.files
	wr.details = map[string]github.ItemDetails{it.NodeID: u.update.Details}

	if first {
		if err := wr.saveGraphQLRate(ctx, u.update.RateLimit); err != nil {
			return false, err
		}
	}

	if !u.update.Found || it.Closed() || it.Repo.IsArchived {
		return true, wr.unlist(ctx, w.id, u.update)
	}

	return false, wr.rewrite(ctx, w.id, it)
}

// replay applies again, in tx, the item writes stored while a refresh was fetching, and
// returns the items they took out of the lists.
func (s *Service) replay(ctx context.Context, tx *sql.Tx, writes []itemWrite) (map[int64]bool, error) {
	unlisted := make(map[int64]bool)

	for _, w := range writes {
		gone, err := s.applyItemWrite(ctx, tx, w, false)
		if err != nil {
			return nil, err
		}

		if gone {
			unlisted[w.id] = true
		}
	}

	return unlisted, nil
}

// rewritten returns the items whose own refresh is among writes: a refresh applying them
// again leaves those items' details and file check as that newer refresh stored them.
func rewritten(writes []itemWrite) map[int64]bool {
	ids := make(map[int64]bool)

	for _, w := range writes {
		if w.update != nil {
			ids[w.id] = true
		}
	}

	return ids
}

// storedBy records what the whole refresh with seq stored, through its writer: every
// item it listed, and the items whose file check it stored. An item still listed keeps
// the number of the refresh that checked its files last, however long ago; an item no
// longer listed is forgotten. The caller holds order.mu.
func (s *Service) storedBy(seq uint64, w *writer) {
	refreshed := make(map[int64]uint64, len(w.saved))
	checked := make(map[int64]uint64, len(w.saved))

	for _, saved := range w.saved {
		refreshed[saved.id] = seq

		if at, ok := s.order.checked[saved.id]; ok {
			checked[saved.id] = at
		}
	}

	for id := range w.checked {
		checked[id] = seq
	}

	s.order.refreshed, s.order.checked = refreshed, checked
}

// itemLocks holds a lock per item while work on it runs, so an item's own refresh and the
// checks of its files run one at a time, while work on other items goes on. A lock is
// dropped once no one holds it or waits for it.
type itemLocks struct {
	mu    sync.Mutex
	locks map[int64]*itemLock
}

// itemLock is one item's lock: held while its channel is full. users counts its holder
// and its waiters.
type itemLock struct {
	held  chan struct{}
	users int
}

// lock waits for the item's lock, or for ctx to end, and returns what releases it.
func (l *itemLocks) lock(ctx context.Context, id int64) (func(), error) {
	l.mu.Lock()

	if l.locks == nil {
		l.locks = make(map[int64]*itemLock)
	}

	il := l.locks[id]
	if il == nil {
		il = &itemLock{held: make(chan struct{}, 1)}
		l.locks[id] = il
	}

	il.users++
	l.mu.Unlock()

	select {
	case il.held <- struct{}{}:
		return func() {
			<-il.held
			l.leave(id, il)
		}, nil
	case <-ctx.Done():
		l.leave(id, il)

		return nil, ctx.Err()
	}
}

// leave counts one user of the item's lock out, dropping the lock once none is left.
func (l *itemLocks) leave(id int64, il *itemLock) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if il.users--; il.users == 0 {
		delete(l.locks, id)
	}
}
