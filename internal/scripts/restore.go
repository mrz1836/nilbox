package scripts

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/scripts/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// userScriptsTable is the table the user's own scripts are kept in, in this build and in a
// database set aside.
const userScriptsTable = "user_scripts"

// restoreColumns are the columns a database set aside holds the user's scripts in. A build
// that lacks one reads it as its empty value, which the script's checks then take or refuse,
// and a script without a UUID, as an older build's, is given a new one.
func restoreColumns() []string {
	return []string{"id", "name", "command", "icon", "scope", "scope_repos", "runs_in", "confirm", "position", "uuid"}
}

// Restore brings back the user's own scripts, which the app lost with a database it set
// aside at startup (setAside, "" for none): a reset the user asked for, or a database it
// could not use. It runs only while db holds no script of the user's, reads the database
// set aside without changing it, whichever build wrote it, and stores every script in it
// in its order under the UUID it had there, each checked as Save checks it: one Save would
// refuse is left out. It stores them all in one transaction, or none, and returns how many
// it stored. A database set aside that holds no scripts, as one an older build wrote,
// brings back none.
func Restore(ctx context.Context, db *sql.DB, clk clock.Clock, setAside string) (int, error) {
	if setAside == "" {
		return 0, nil
	}

	if db == nil {
		return 0, ErrNotSetUp
	}

	kept, err := readSetAside(ctx, setAside)
	if err != nil {
		return 0, err
	}

	if len(kept) == 0 {
		return 0, nil
	}

	y := newYourStore(db, clk)
	stored := 0

	err = store.InTx(ctx, db, func(tx *sql.Tx) error {
		var storeErr error

		stored, storeErr = y.storeKept(ctx, y.q.WithTx(tx), kept)

		return storeErr
	})
	if err != nil {
		return 0, fmt.Errorf("bring back your scripts: %w", err)
	}

	return stored, nil
}

// storeKept stores kept, in their order, through q, inside the transaction Restore runs it
// in, when the user has no script yet, and returns how many it stored.
func (y yourStore) storeKept(ctx context.Context, q *queries.Queries, kept []UserScript) (int, error) {
	rows, err := q.ListUserScripts(ctx)
	if err != nil {
		return 0, fmt.Errorf("read your scripts: %w", err)
	}

	if len(rows) > 0 {
		return 0, nil
	}

	for i, u := range kept {
		if _, err = y.insert(ctx, q, u, int64(i)); err != nil {
			return 0, err
		}
	}

	return len(kept), nil
}

// readSetAside reads the user's scripts in the database at path, in their order, each as
// Save would store it; one it would refuse is left out.
func readSetAside(ctx context.Context, path string) ([]UserScript, error) {
	snap, err := store.OpenSnapshot(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read your scripts in the database set aside: %w", err)
	}

	defer func() { _ = snap.Close() }()

	rows, err := snap.Rows(ctx, userScriptsTable, restoreColumns()...)
	if errors.Is(err, store.ErrNoTable) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("read your scripts in the database set aside: %w", err)
	}

	slices.SortStableFunc(rows, func(a, b store.Row) int {
		pa, _ := a.Int("position")
		pb, _ := b.Int("position")
		ia, _ := a.Int("id")
		ib, _ := b.Int("id")

		return cmp.Or(cmp.Compare(pa, pb), cmp.Compare(ia, ib))
	})

	out := make([]UserScript, 0, len(rows))

	for _, row := range rows {
		if u, checkErr := checked(keptScript(row)); checkErr == nil {
			out = append(out, u)
		}
	}

	return out, nil
}

// keptScript reads one row of a database set aside as a script, as fromRow reads one of
// this build's: a place or an icon this build doesn't know reads as the setting and play,
// and a scope it lacks as every clone.
func keptScript(row store.Row) UserScript {
	confirm := row.Bool("confirm")

	u := UserScript{
		UUID: row.Text("uuid"), Name: row.Text("name"), Command: row.Text("command"), Icon: Icon(row.Text("icon")),
		Scope: Scope(cmp.Or(row.Text("scope"), string(ScopeAll))), Repos: row.Strings("scope_repos"),
		RunsIn: integrationprefs.RunsIn(row.Text("runs_in")), Confirm: confirm != nil && *confirm,
	}

	if !validPlace(u.RunsIn) {
		u.RunsIn = ""
	}

	if !u.Icon.Valid() {
		u.Icon = IconPlay
	}

	return u
}
