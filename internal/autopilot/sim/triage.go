package sim

import (
	"context"
	"database/sql"
	"slices"
	"sync"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/inbox"
)

// Triage runs the routines' triage steps, and their Undo, through the inbox's own triage
// of the app as Begin last built it, as core's adapter does, and records each step it was
// asked to run.
type Triage struct {
	sim *Sim

	mu    sync.Mutex
	calls []autopilot.TriageRequest

	// gate holds a step until Release, as a step on its way while the user acts.
	gate chan struct{}
}

// Hold holds every step until Release, before it reaches the inbox.
func (t *Triage) Hold() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.gate = make(chan struct{})
}

// Release lets the steps held through.
func (t *Triage) Release() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.gate != nil {
		close(t.gate)
		t.gate = nil
	}
}

// Apply runs req through the inbox's triage, once a hold lets it.
func (t *Triage) Apply(ctx context.Context, req autopilot.TriageRequest,
	record func(ctx context.Context, tx *sql.Tx, change autopilot.TriageChange) error,
) (autopilot.TriageChange, error) {
	t.mu.Lock()
	t.calls = append(t.calls, req)
	gate := t.gate
	t.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return autopilot.TriageChange{}, ctx.Err()
		}
	}

	got, err := t.sim.Routines.Triage(ctx, inbox.RoutineTriage{
		Routine: req.RoutineID, Item: req.ItemID, Action: inbox.TriageAction(req.Action), Days: req.Days,
		Expect: req.Expect,
		Record: func(ctx context.Context, tx *sql.Tx, c inbox.TriageChange) error {
			return record(ctx, tx, changeOf(c))
		},
	})

	return changeOf(got), err
}

// Undo puts back what undos changed, through the inbox's triage.
func (t *Triage) Undo(ctx context.Context, undos []autopilot.TriageUndo,
	record func(ctx context.Context, tx *sql.Tx, put []bool) error,
) ([]bool, error) {
	return t.sim.Routines.UndoTriage(ctx, inboxUndos(undos), record)
}

// AsLeft reports which of undos' items are still as they left them.
func (t *Triage) AsLeft(ctx context.Context, undos []autopilot.TriageUndo) ([]bool, error) {
	return t.sim.Routines.AsLeft(ctx, inboxUndos(undos))
}

// Calls returns every triage step the engine asked for, in order.
func (t *Triage) Calls() []autopilot.TriageRequest {
	t.mu.Lock()
	defer t.mu.Unlock()

	return slices.Clone(t.calls)
}

// changeOf is the inbox's change in Autopilot's words.
func changeOf(c inbox.TriageChange) autopilot.TriageChange {
	return autopilot.TriageChange{Before: c.Before, After: c.After, Kept: autopilot.TriageKept(c.Kept)}
}

// inboxUndos is undos in the inbox's words.
func inboxUndos(undos []autopilot.TriageUndo) []inbox.TriageUndo {
	out := make([]inbox.TriageUndo, 0, len(undos))
	for _, u := range undos {
		out = append(out, inbox.TriageUndo{
			Routine: u.RoutineID, Item: u.ItemID, Action: inbox.TriageAction(u.Action), Before: u.Before, After: u.After,
		})
	}

	return out
}

// InboxItem returns the item number as the inbox's snapshot or its routines' rows show it,
// listed or only watched, with its triage.
func (s *Sim) InboxItem(number int) inbox.Item {
	s.t.Helper()

	id := s.ItemID(number)

	got, err := s.Inbox.ItemsByID(context.Background(), []int64{id})
	require.NoError(s.t, err)
	require.Len(s.t, got, 1, "#%d is stored", number)

	return got[0]
}

// ItemID returns the stored ID of the item number.
func (s *Sim) ItemID(number int) int64 {
	s.t.Helper()

	for _, m := range s.Matches() {
		if m.Item.Number == number && m.Item.ID != 0 {
			return m.Item.ID
		}
	}

	if id, ok := s.listedID(number); ok {
		return id
	}

	if id, ok := s.watchedID(number); ok {
		return id
	}

	require.FailNow(s.t, "no stored item", "#%d", number)

	return 0
}

// listedID returns the stored ID of the item number in an inbox list.
func (s *Sim) listedID(number int) (int64, bool) {
	s.t.Helper()

	snap, err := s.Inbox.Snapshot(context.Background())
	require.NoError(s.t, err)

	for _, l := range snap.Lists() {
		for _, it := range l.Items {
			if it.Number == number {
				return it.ID, true
			}
		}
	}

	return 0, false
}

// watchedID returns the stored ID of the item number a routine watches.
func (s *Sim) watchedID(number int) (int64, bool) {
	s.t.Helper()

	ctx := context.Background()

	routines, err := s.Service.List(ctx)
	require.NoError(s.t, err)

	for _, r := range routines {
		ids, watchErr := s.Service.Watching(ctx, r.ID)
		require.NoError(s.t, watchErr)

		got, itemsErr := s.Inbox.ItemsByID(ctx, ids)
		require.NoError(s.t, itemsErr)

		for _, it := range got {
			if it.Number == number {
				return it.ID, true
			}
		}
	}

	return 0, false
}

// Effects returns what every match's local steps did, oldest first.
func (s *Sim) Effects() []autopilot.Effect {
	s.t.Helper()

	var out []autopilot.Effect
	for _, m := range s.Matches() {
		out = append(out, m.Effects...)
	}

	return out
}
