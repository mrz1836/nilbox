package autopilot

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// The ledger's keys in memory. Every cycle needs, of each routine it runs, its matches by
// the occurrence each is for (whether one is recorded, and how it ended) and what it first
// saw of each item it watches: over 20 routines and 500 items, 3,500 matches and 10,000
// baselines, whose reads were half of a steady cycle. The engine is the ledger's only
// writer, so it keeps them in memory between its cycles (ledgerIndex) and changes them
// where it writes the rows: the ledger's insert, update, and saveBaseline each note what
// they wrote, a transaction's notes applied once it commits and dropped when it rolls back,
// and the prune notes the matches it deletes. They are built from the database when the
// engine takes the lock, since another copy of the app may have written the ledger while it
// held it, and whenever the rows' counts differ from what is kept: the one other change to
// these rows is a deletion, as the inbox's prune of an old item takes the item's baselines
// with it and leaves its matches without it, so a count tells it. Only the engine's loop
// reads or changes them.

// keyRow is what the keys keep of a match: its ID, its state, why it closed, and whether it
// started a step (startedAny), which together say whether an occurrence is recorded, how it
// ended (lastClosed), and whether it opens again when it matches again (reopensRow).
type keyRow struct {
	ID      int64
	State   State
	Reason  Reason
	Started bool
}

// keyRowOf returns what the keys keep of a match read from the ledger.
func keyRowOf(id int64, state, reason string, attempt, step int64, steps string) keyRow {
	return keyRow{ID: id, State: State(state), Reason: Reason(reason), Started: startedAny(attempt, step, steps)}
}

// keyPlace is where the keys keep a match: its routine and the occurrence it is for.
type keyPlace struct {
	routine int64
	key     matchKey
}

// ledgerIndex is the ledger's keys in memory (see the notes above).
type ledgerIndex struct {
	// valid reports keys built from the database and kept in step since; false, the next
	// use builds them again.
	valid atomic.Bool

	// matches holds each routine's matches with an item, by occurrence; places where each
	// is, by its ID; seen what each routine saw first of each item, by item; and rows how
	// many baselines the ledger holds, one that won't read among them, which seen leaves out
	// as the cycle always did.
	matches map[int64]map[matchKey]keyRow
	places  map[int64]keyPlace
	seen    map[int64]map[int64]baseline
	rows    int

	// builds counts how often they were built from the database.
	builds int

	// mu guards txs: the notes of each transaction open (inTx), applied once it commits.
	mu  sync.Mutex
	txs map[*queries.Queries][]func(x *ledgerIndex)
}

// newLedgerIndex returns keys that are built on their first use.
func newLedgerIndex() *ledgerIndex {
	x := &ledgerIndex{txs: map[*queries.Queries][]func(x *ledgerIndex){}}
	x.reset()

	return x
}

// reset empties the keys.
func (x *ledgerIndex) reset() {
	x.matches, x.places = map[int64]map[matchKey]keyRow{}, map[int64]keyPlace{}
	x.seen, x.rows = map[int64]map[int64]baseline{}, 0
}

// invalidate has the next use build the keys again.
func (x *ledgerIndex) invalidate() {
	if x != nil {
		x.valid.Store(false)
	}
}

// ensure makes the keys hold what the ledger does: kept, unless they were never built, an
// engine took the lock since, or the ledger's counts of its rows differ from theirs (one
// read), in which case it builds them again.
func (x *ledgerIndex) ensure(ctx context.Context, q *queries.Queries) error {
	if x.valid.Load() {
		n, err := q.CountLedgerKeys(ctx)
		if err != nil {
			return fmt.Errorf("count the ledger's matches: %w", err)
		}

		if n.Matches == int64(len(x.places)) && n.Baselines == int64(x.rows) {
			return nil
		}
	}

	return x.build(ctx, q)
}

// build builds the keys from the ledger, in two reads.
func (x *ledgerIndex) build(ctx context.Context, q *queries.Queries) error {
	x.valid.Store(false)

	keys, err := q.ListMatchKeys(ctx)
	if err != nil {
		return fmt.Errorf("read the routines' matches: %w", err)
	}

	seen, err := q.ListAllBaselines(ctx)
	if err != nil {
		return fmt.Errorf("read what the routines saw first: %w", err)
	}

	x.reset()

	for _, k := range keys {
		x.put(k.RoutineID, matchKey{item: store.Deref(k.ItemID), fire: k.FireKey, dryRun: k.DryRun},
			keyRowOf(k.ID, k.State, k.Reason, k.Attempt, k.Step, k.Steps))
	}

	for _, b := range seen {
		x.rows++

		if got, decodeErr := store.DecodeJSON[baseline](b.State); decodeErr == nil {
			x.seenOf(b.RoutineID)[b.ItemID] = got
		}
	}

	x.builds++
	x.valid.Store(true)

	return nil
}

// matchesOf returns the routine id's matches as kept, which the keys change in place as
// the engine writes them.
func (x *ledgerIndex) matchesOf(id int64) map[matchKey]keyRow {
	m, ok := x.matches[id]
	if !ok {
		m = map[matchKey]keyRow{}
		x.matches[id] = m
	}

	return m
}

// seenOf returns what the routine id saw first of each item, as kept, which the keys change
// in place as the engine writes it.
func (x *ledgerIndex) seenOf(id int64) map[int64]baseline {
	m, ok := x.seen[id]
	if !ok {
		m = map[int64]baseline{}
		x.seen[id] = m
	}

	return m
}

// put keeps row, the match of routine for the occurrence key.
func (x *ledgerIndex) put(routine int64, key matchKey, row keyRow) {
	m := x.matchesOf(routine)
	if was, ok := m[key]; ok && was.ID != row.ID {
		delete(x.places, was.ID)
	}

	m[key] = row
	x.places[row.ID] = keyPlace{routine: routine, key: key}
}

// apply applies change to the keys once the write it is for holds: at once for one made
// through direct, the ledger's own queries, or once the transaction q is of commits. A
// write through a transaction the ledger didn't open has the keys built again.
func (x *ledgerIndex) apply(q, direct *queries.Queries, change func(x *ledgerIndex)) {
	if x == nil {
		return
	}

	if q == direct {
		change(x)

		return
	}

	x.mu.Lock()
	defer x.mu.Unlock()

	if notes, ok := x.txs[q]; ok {
		x.txs[q] = append(notes, change)

		return
	}

	x.valid.Store(false)
}

// begin opens the notes of the transaction q is of.
func (x *ledgerIndex) begin(q *queries.Queries) {
	if x == nil {
		return
	}

	x.mu.Lock()
	defer x.mu.Unlock()

	x.txs[q] = nil
}

// end closes the notes of the transaction q is of, applying them when it committed.
func (x *ledgerIndex) end(q *queries.Queries, committed bool) {
	if x == nil || q == nil {
		return
	}

	x.mu.Lock()
	notes := x.txs[q]
	delete(x.txs, q)
	x.mu.Unlock()

	if !committed {
		return
	}

	for _, change := range notes {
		change(x)
	}
}

// wroteMatch notes m as written through q: inserted, or moved on.
func (x *ledgerIndex) wroteMatch(q, direct *queries.Queries, m *match, steps string) {
	if m.ItemID == nil {
		return
	}

	key := matchKey{item: *m.ItemID, fire: m.FireKey, dryRun: m.DryRun}
	row := keyRowOf(m.ID, m.State, m.Reason, m.Attempt, m.Step, steps)
	routine := m.RoutineID

	x.apply(q, direct, func(x *ledgerIndex) { x.put(routine, key, row) })
}

// wroteBaseline notes b as what routine saw of item, written through q, inserted when first.
func (x *ledgerIndex) wroteBaseline(q, direct *queries.Queries, routine, item int64, b baseline, first bool) {
	x.apply(q, direct, func(x *ledgerIndex) {
		if first {
			x.rows++
		}

		x.seenOf(routine)[item] = b
	})
}

// deletedMatches notes the matches ids as deleted through q, with their runs.
func (x *ledgerIndex) deletedMatches(q, direct *queries.Queries, ids []int64) {
	ids = slices.Clone(ids)

	x.apply(q, direct, func(x *ledgerIndex) {
		for _, id := range ids {
			if at, ok := x.places[id]; ok {
				delete(x.matches[at.routine], at.key)
				delete(x.places, id)
			}
		}
	})
}

// errKeysDrifted is keys in memory that differ from the ledger's rows.
var errKeysDrifted = errors.New("the ledger's keys in memory differ from the ledger")

// diff compares the keys with the ledger's rows, read again through q, and says how they
// differ, nil when they don't, or while they wait to be built again.
func (x *ledgerIndex) diff(ctx context.Context, q *queries.Queries) error {
	if !x.valid.Load() {
		return nil
	}

	fresh := newLedgerIndex()
	if err := fresh.build(ctx, q); err != nil {
		return err
	}

	var diffs []string

	if x.rows != fresh.rows {
		diffs = append(diffs, fmt.Sprintf("%d baselines kept, %d stored", x.rows, fresh.rows))
	}

	if !maps.Equal(x.places, fresh.places) {
		diffs = append(diffs, fmt.Sprintf("%d matches kept, %d stored", len(x.places), len(fresh.places)))
	}

	for _, id := range x.routines(fresh) {
		if !maps.Equal(x.matches[id], fresh.matches[id]) {
			diffs = append(diffs, fmt.Sprintf("routine %d's matches: kept %v, stored %v", id, x.matches[id], fresh.matches[id]))
		}

		if !maps.EqualFunc(x.seen[id], fresh.seen[id], sameBaseline) {
			diffs = append(diffs, fmt.Sprintf("routine %d's baselines: kept %v, stored %v", id, x.seen[id], fresh.seen[id]))
		}
	}

	if len(diffs) == 0 {
		return nil
	}

	return fmt.Errorf("%w: %s", errKeysDrifted, strings.Join(diffs, "; "))
}

// routines returns, in order, every routine x or other keeps anything of.
func (x *ledgerIndex) routines(other *ledgerIndex) []int64 {
	ids := map[int64]bool{}

	for _, of := range []*ledgerIndex{x, other} {
		for id := range of.matches {
			ids[id] = true
		}

		for id := range of.seen {
			ids[id] = true
		}
	}

	return slices.Sorted(maps.Keys(ids))
}

// sameBaseline reports two baselines alike.
func sameBaseline(a, b baseline) bool {
	return a.Head == b.Head && sameCount(a.Comments, b.Comments)
}
