package scripts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/scripts/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// yourStore keeps the user's own scripts, in user_scripts, in the order they put them in:
// positions 0, 1, 2, and so on, numbered again in the transaction of every change that
// adds, removes, or moves one. A store without a database keeps none, and refuses a
// change.
type yourStore struct {
	db    *sql.DB
	q     *queries.Queries
	clock clock.Clock
}

// newYourStore returns the store over db, which may be nil.
func newYourStore(db *sql.DB, clk clock.Clock) yourStore {
	y := yourStore{db: db, clock: clock.Or(clk)}
	if db != nil {
		y.q = queries.New(db)
	}

	return y
}

// all returns every script the user saved, in their order.
func (y yourStore) all(ctx context.Context) ([]UserScript, error) {
	out := []UserScript{}
	if y.q == nil {
		return out, nil
	}

	rows, err := y.q.ListUserScripts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read your scripts: %w", err)
	}

	for _, r := range rows {
		out = append(out, fromRow(r))
	}

	return out, nil
}

// get returns the script id, and whether the user still has it.
func (y yourStore) get(ctx context.Context, id int64) (UserScript, bool, error) {
	if y.q == nil {
		return UserScript{}, false, nil
	}

	row, err := y.q.GetUserScript(ctx, id)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return UserScript{}, false, nil
	case err != nil:
		return UserScript{}, false, fmt.Errorf("read your script %d: %w", id, err)
	}

	return fromRow(row), true, nil
}

// create saves u, checked, as a new script at the end of the order, and returns its ID.
func (y yourStore) create(ctx context.Context, u UserScript) (int64, error) {
	var id int64

	err := y.reorder(ctx, func(q *queries.Queries, rows []queries.UserScript) ([]queries.UserScript, error) {
		row, err := y.insert(ctx, q, u, int64(len(rows)))
		id = row.ID

		return append(rows, row), err
	})

	return id, err
}

// update saves u, checked, in place of the script with its ID, which keeps its place.
func (y yourStore) update(ctx context.Context, u UserScript) error {
	if y.q == nil {
		return ErrNotSetUp
	}

	repos, err := store.EncodeJSON(u.Repos)
	if err != nil {
		return err
	}

	n, err := y.q.UpdateUserScript(ctx, queries.UpdateUserScriptParams{
		ID: u.ID, Name: u.Name, Command: u.Command, Icon: string(u.Icon), Scope: string(u.Scope), ScopeRepos: repos,
		RunsIn: string(u.RunsIn), Confirm: boolInt(u.Confirm), UpdatedAt: y.clock.Now(),
	})

	switch {
	case err != nil:
		return fmt.Errorf("save your script %d: %w", u.ID, err)
	case n == 0:
		return goneError(u.ID)
	}

	return nil
}

// remove deletes the script id.
func (y yourStore) remove(ctx context.Context, id int64) error {
	return y.reorder(ctx, func(q *queries.Queries, rows []queries.UserScript) ([]queries.UserScript, error) {
		n, err := q.DeleteUserScript(ctx, id)

		switch {
		case err != nil:
			return nil, fmt.Errorf("delete your script %d: %w", id, err)
		case n == 0:
			return nil, goneError(id)
		}

		return slices.DeleteFunc(rows, func(r queries.UserScript) bool { return r.ID == id }), nil
	})
}

// duplicate saves a copy of the script id, called copyName of its name, right after it in
// the order, and returns the copy.
func (y yourStore) duplicate(ctx context.Context, id int64) (UserScript, error) {
	var made UserScript

	err := y.reorder(ctx, func(q *queries.Queries, rows []queries.UserScript) ([]queries.UserScript, error) {
		i := slices.IndexFunc(rows, func(r queries.UserScript) bool { return r.ID == id })
		if i < 0 {
			return nil, goneError(id)
		}

		made = fromRow(rows[i])
		made.Name = copyName(made.Name)

		row, err := y.insert(ctx, q, made, int64(i+1))
		made.ID = row.ID

		return slices.Insert(rows, i+1, row), err
	})
	if err != nil {
		return UserScript{}, err
	}

	return made, nil
}

// move moves the script id one place up or down the order: past the nearest script that
// way that shown reports, leaving it where it is when there is none.
func (y yourStore) move(ctx context.Context, id int64, way Direction, shown func(UserScript) bool) error {
	return y.reorder(ctx, func(_ *queries.Queries, rows []queries.UserScript) ([]queries.UserScript, error) {
		i := slices.IndexFunc(rows, func(r queries.UserScript) bool { return r.ID == id })
		if i < 0 {
			return nil, goneError(id)
		}

		step := 1
		if way == MoveUp {
			step = -1
		}

		for j := i + step; j >= 0 && j < len(rows); j += step {
			if shown(fromRow(rows[j])) {
				// Taken out, the script leaves its neighbor at j going up and at j-1 going
				// down: put back at j, it sits just before it, or just after it.
				moving := rows[i]

				return slices.Insert(slices.Delete(rows, i, i+1), j, moving), nil
			}
		}

		return rows, nil
	})
}

// insert stores u as a new script at position, and returns its row.
func (y yourStore) insert(ctx context.Context, q *queries.Queries, u UserScript, position int64,
) (queries.UserScript, error) {
	repos, err := store.EncodeJSON(u.Repos)
	if err != nil {
		return queries.UserScript{}, err
	}

	now := y.clock.Now()
	row := queries.UserScript{
		Name: u.Name, Command: u.Command, Icon: string(u.Icon), Scope: string(u.Scope), ScopeRepos: repos,
		RunsIn: string(u.RunsIn), Confirm: boolInt(u.Confirm), Position: position, CreatedAt: now, UpdatedAt: now,
	}

	row.ID, err = q.InsertUserScript(ctx, queries.InsertUserScriptParams{
		Name: row.Name, Command: row.Command, Icon: row.Icon, Scope: row.Scope, ScopeRepos: row.ScopeRepos,
		RunsIn: row.RunsIn, Confirm: row.Confirm, Position: row.Position, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return queries.UserScript{}, fmt.Errorf("save your script: %w", err)
	}

	return row, nil
}

// reorder runs change in one transaction over the scripts as stored, in their order, and
// stores the order it returns, numbering their positions 0, 1, 2, and so on.
func (y yourStore) reorder(ctx context.Context,
	change func(q *queries.Queries, rows []queries.UserScript) ([]queries.UserScript, error),
) error {
	if y.q == nil {
		return ErrNotSetUp
	}

	return store.InTx(ctx, y.db, func(tx *sql.Tx) error {
		q := y.q.WithTx(tx)

		rows, err := q.ListUserScripts(ctx)
		if err != nil {
			return fmt.Errorf("read your scripts: %w", err)
		}

		order, err := change(q, rows)
		if err != nil {
			return err
		}

		return renumber(ctx, q, order)
	})
}

// renumber stores order's positions as 0, 1, 2, and so on, writing only those that moved.
func renumber(ctx context.Context, q *queries.Queries, order []queries.UserScript) error {
	for i, r := range order {
		if r.Position == int64(i) {
			continue
		}

		err := q.SetUserScriptPosition(ctx, queries.SetUserScriptPositionParams{Position: int64(i), ID: r.ID})
		if err != nil {
			return fmt.Errorf("order your scripts: %w", err)
		}
	}

	return nil
}

// fromRow reads a stored script. A place or an icon this build doesn't know reads as the
// setting and play, and repositories that can't be read as none.
func fromRow(r queries.UserScript) UserScript {
	repos, err := store.DecodeJSON[[]string](r.ScopeRepos)
	if err != nil || repos == nil {
		repos = []string{}
	}

	u := UserScript{
		ID: r.ID, Name: r.Name, Command: r.Command, Icon: Icon(r.Icon), Scope: Scope(r.Scope), Repos: repos,
		RunsIn: RunsIn(r.RunsIn), Confirm: r.Confirm != 0,
	}

	if !u.RunsIn.Valid() {
		u.RunsIn = RunsInSetting
	}

	if !u.Icon.Valid() {
		u.Icon = IconPlay
	}

	return u
}

// goneError says the user's script id was deleted.
func goneError(id int64) error {
	return uierr.New("The script was deleted.", fmt.Errorf("%w: your script %d", ErrNotFound, id))
}

// boolInt stores a bool as SQLite does: 1 or 0.
func boolInt(b bool) int64 {
	if b {
		return 1
	}

	return 0
}
