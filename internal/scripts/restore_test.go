package scripts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// setAsideWith writes a database set aside at a fresh path holding scripts, in their
// order, stored as they are, unchecked, and returns its path once it is closed.
func setAsideWith(t *testing.T, scripts ...UserScript) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.db.reset-20261115T000000Z")
	db := testutil.DBAt(t, path)
	y := newYourStore(db, nil)

	for i, u := range scripts {
		_, err := y.insert(t.Context(), y.q, u, int64(i))
		require.NoError(t, err)
	}

	require.NoError(t, db.Close())

	return path
}

// storedNames returns the names of the scripts y holds, in their order.
func storedNames(t *testing.T, y yourStore) []string {
	t.Helper()

	all, err := y.all(t.Context())
	require.NoError(t, err)

	out := make([]string, 0, len(all))
	for _, u := range all {
		out = append(out, u.Name)
	}

	return out
}

func TestRestoreBringsBackTheScriptsInTheirOrder(t *testing.T) {
	t.Parallel()

	repos := UserScript{
		UUID: keptUUID, Name: "Deploy", Command: "make deploy", Icon: IconBuild, Scope: ScopeRepos,
		Repos:  []string{"acme-org/example-repo"},
		RunsIn: integrationprefs.RunsInTerminal, Confirm: true,
	}
	path := setAsideWith(t, named(firstName), repos, named(testName))

	db := testutil.DB(t)

	n, err := Restore(t.Context(), db, nil, path)
	require.NoError(t, err)
	assert.Equal(t, 3, n)

	y := newYourStore(db, nil)
	assert.Equal(t, []string{firstName, "Deploy", testName}, storedNames(t, y))

	all, err := y.all(t.Context())
	require.NoError(t, err)

	got := all[1]
	got.ID = 0
	assert.Equal(t, repos, got, "every field comes back, its UUID too")
}

func TestRestoreLeavesOutAScriptSaveWouldRefuse(t *testing.T) {
	t.Parallel()

	hidden := UserScript{Name: "Hidden", Command: "echo one\necho two", Icon: IconPlay, Scope: ScopeAll}
	unknownIcon := UserScript{Name: "Lint", Command: "magex lint", Icon: "sparkles", Scope: ScopeAll}
	path := setAsideWith(t, named(firstName), hidden, unknownIcon)

	db := testutil.DB(t)

	n, err := Restore(t.Context(), db, nil, path)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	y := newYourStore(db, nil)
	assert.Equal(t, []string{firstName, "Lint"}, storedNames(t, y))

	all, err := y.all(t.Context())
	require.NoError(t, err)
	assert.Equal(t, IconPlay, all[1].Icon, "an icon this build doesn't know reads as play")
}

func TestRestoreOnlyIntoAnEmptyList(t *testing.T) {
	t.Parallel()

	path := setAsideWith(t, named(firstName))

	db := testutil.DB(t)
	y := newYourStore(db, nil)
	_, err := y.create(t.Context(), named(testName))
	require.NoError(t, err)

	n, err := Restore(t.Context(), db, nil, path)
	require.NoError(t, err)
	assert.Zero(t, n, "scripts saved since are never mixed with the old ones")
	assert.Equal(t, []string{testName}, storedNames(t, y))
}

func TestRestoreWithNothingSetAside(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	n, err := Restore(t.Context(), db, nil, "")
	require.NoError(t, err)
	assert.Zero(t, n)

	// An older build's database has no table for the user's scripts.
	older := filepath.Join(t.TempDir(), "app.db.broken-20261115T000000Z")
	raw := testutil.DBAt(t, older)
	testutil.Exec(t, raw, "DROP TABLE user_scripts")
	require.NoError(t, raw.Close())

	n, err = Restore(t.Context(), db, nil, older)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestRestoreReportsADatabaseItCannotRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db.broken-20261115T000000Z")
	testutil.WriteFile(t, path, "not a database at all, only text long enough to have a header")

	n, err := Restore(t.Context(), testutil.DB(t), nil, path)
	require.Error(t, err)
	assert.Zero(t, n)

	_, err = Restore(t.Context(), nil, nil, path)
	require.ErrorIs(t, err, ErrNotSetUp)
}

func TestRestoreStoresNothingWhenAWriteFails(t *testing.T) {
	t.Parallel()

	path := setAsideWith(t, named(firstName), named(testName))

	db := testutil.DB(t)
	testutil.Exec(t, db, `CREATE TRIGGER no_second BEFORE INSERT ON user_scripts
WHEN NEW.name = 'Test' BEGIN SELECT RAISE(ABORT, 'no'); END`)

	n, err := Restore(t.Context(), db, nil, path)
	require.Error(t, err)
	assert.Zero(t, n)
	assert.Empty(t, storedNames(t, newYourStore(db, nil)), "all of them or none")
}
