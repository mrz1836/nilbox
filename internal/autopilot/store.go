package autopilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// routineStore keeps the routines in autopilot_routines, in the user's order: places 0, 1,
// 2, and so on among the routines not deleted, numbered again in the transaction of every
// change that adds, deletes, or moves one. A store without a database keeps none, and
// refuses a change.
type routineStore struct {
	db    *sql.DB
	q     *queries.Queries
	clock clock.Clock
}

// newRoutineStore returns the store over db, which may be nil.
func newRoutineStore(db *sql.DB, clk clock.Clock) routineStore {
	r := routineStore{db: db, clock: clock.Or(clk)}
	if db != nil {
		r.q = queries.New(db)
	}

	return r
}

// all returns every routine not deleted, in the user's order.
func (r routineStore) all(ctx context.Context) ([]queries.AutopilotRoutine, error) {
	if r.q == nil {
		return []queries.AutopilotRoutine{}, nil
	}

	rows, err := r.q.ListRoutines(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the routines: %w", err)
	}

	return rows, nil
}

// get returns the routine id, refusing one that is gone or deleted.
func (r routineStore) get(ctx context.Context, q *queries.Queries, id int64) (queries.AutopilotRoutine, error) {
	row, err := q.GetRoutine(ctx, id)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return queries.AutopilotRoutine{}, goneError(id)
	case err != nil:
		return queries.AutopilotRoutine{}, fmt.Errorf("read the routine %d: %w", id, err)
	}

	return row, nil
}

// inTx runs fn in one transaction over the store's queries.
func (r routineStore) inTx(ctx context.Context, fn func(q *queries.Queries) error) error {
	if r.q == nil {
		return ErrNotSetUp
	}

	return store.InTx(ctx, r.db, func(tx *sql.Tx) error {
		return fn(r.q.WithTx(tx))
	})
}

// sameName reports whether two routines' names are the same, ignoring case, in any
// alphabet (draft.NameKey, which every comparison of names uses).
func sameName(a, b string) bool {
	return draft.NameKey(a) == draft.NameKey(b)
}

// checkFree refuses name when a routine not deleted other than id has it, ignoring case.
func checkFree(ctx context.Context, q *queries.Queries, name string, id int64) error {
	_, err := q.RoutineNamed(ctx, queries.RoutineNamedParams{NameKey: draft.NameKey(name), OtherThan: id})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read the routines' names: %w", err)
	}

	return uierr.New(fmt.Sprintf("Another routine is named “%s”. Give this one a name of its own.", name),
		fmt.Errorf("%w: %q", ErrNameTaken, name))
}

// renumber stores order's places as 0, 1, 2, and so on, writing only those that moved.
func renumber(ctx context.Context, q *queries.Queries, order []queries.AutopilotRoutine) error {
	for i, row := range order {
		if row.SortOrder == int64(i) {
			continue
		}

		if err := q.SetRoutineOrder(ctx, queries.SetRoutineOrderParams{SortOrder: int64(i), ID: row.ID}); err != nil {
			return fmt.Errorf("order the routines: %w", err)
		}
	}

	return nil
}

// freeName returns name, or the first of "name 2", "name 3", and so on that no routine in
// rows has, ignoring case, cut to fit MaxNameRunes (draft.FreeName).
func freeName(name string, rows []queries.AutopilotRoutine) string {
	return draft.FreeName(name, func(n string) bool {
		return slices.ContainsFunc(rows, func(r queries.AutopilotRoutine) bool { return sameName(r.Name, n) })
	})
}

// copyName returns the name of a copy of the routine name: "name copy", or "name copy 2"
// and so on when another routine in rows has it.
func copyName(name string, rows []queries.AutopilotRoutine) string {
	return freeName(draft.FitName(name, " copy"), rows)
}

// goneError says the routine id was deleted.
func goneError(id int64) error {
	return uierr.New("The routine was deleted.", fmt.Errorf("%w: %d", ErrNotFound, id))
}
