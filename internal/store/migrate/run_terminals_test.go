package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestRunTerminalsLeaveEveryRunAsItWas migrates a database to 0116, stores a run in the
// app and one in the terminal, then applies 0117_run_terminals: both read the terminal they
// opened in as empty, so the window names the one chosen now for them, as it did, keep
// every other column, and a run recorded after it keeps the terminal it opened in.
func TestRunTerminalsLeaveEveryRunAsItWas(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	through116, _ := embeddedThrough(t, 116)
	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through116})
	require.NoError(t, err)

	execAll(t, db,
		`INSERT INTO script_runs (kind, title, dir, runs_in, status, pid, started_at)
			VALUES ('script', 'Lint', '/work/example-repo', 'app', 'succeeded', 0, `+at+`),
				('release', 'Release v1.4.3', '/work/example-repo', 'terminal', 'running', 4100, `+at+`)`,
	)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	assert.Equal(t, []string{"", ""}, testutil.Column[string](t, db, `SELECT terminal FROM script_runs ORDER BY id`),
		"a run from before records no terminal")
	assert.Equal(t, []string{"app|succeeded|0", "terminal|running|4100"},
		testutil.Column[string](t, db, `SELECT runs_in || '|' || status || '|' || pid FROM script_runs ORDER BY id`),
		"and keeps what it held")

	execAll(t, db, `UPDATE script_runs SET terminal = 'warp' WHERE id = 2`)
	assert.Equal(t, []string{"", "warp"}, testutil.Column[string](t, db, `SELECT terminal FROM script_runs ORDER BY id`))
}
