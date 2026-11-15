package migrate_test

import (
	"database/sql"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// embeddedBefore returns the embedded migrations whose files sort before name.
func embeddedBefore(t *testing.T, name string) fstest.MapFS {
	t.Helper()

	entries, err := fs.ReadDir(migrate.Embedded(), ".")
	require.NoError(t, err)

	out := fstest.MapFS{}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") || e.Name() >= name {
			continue
		}

		data, err := fs.ReadFile(migrate.Embedded(), e.Name())
		require.NoError(t, err)

		out[e.Name()] = &fstest.MapFile{Data: data}
	}

	return out
}

// prefsOf returns the stored prefs whose keys start with prefix.
func prefsOf(t *testing.T, db *sql.DB, prefix string) map[string]string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `SELECT key, value FROM prefs WHERE key LIKE ? ORDER BY key`, prefix+"%")
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	out := map[string]string{}

	for rows.Next() {
		var key, value string
		require.NoError(t, rows.Scan(&key, &value))

		out[key] = value
	}

	require.NoError(t, rows.Err())

	return out
}

func TestTheServiceStatusMigrationCarriesTheIntervalOver(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		before, want map[string]string
	}{
		"the interval the user set moves to the new key": {
			before: map[string]string{"refresh.githubStatus": "600", "refresh.inbox": "300"},
			want:   map[string]string{"refresh.serviceStatus": "600", "refresh.inbox": "300"},
		},
		"a database with both keeps the new one": {
			before: map[string]string{"refresh.githubStatus": "600", "refresh.serviceStatus": "0"},
			want:   map[string]string{"refresh.serviceStatus": "0"},
		},
		"nothing set, nothing written": {
			before: map[string]string{"refresh.inbox": "120"},
			want:   map[string]string{"refresh.inbox": "120"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			db, path := openDB(t)

			_, err := migrate.Apply(t.Context(), db, path, migrate.Options{
				Source: embeddedBefore(t, "0004_service_status.sql"),
			})
			require.NoError(t, err)

			for key, value := range tc.before {
				_, err = db.ExecContext(t.Context(),
					`INSERT INTO prefs (key, value, updated_at) VALUES (?, ?, '2026-11-01 10:00:00+00:00')`, key, value)
				require.NoError(t, err)
			}

			_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
			require.NoError(t, err)

			assert.Equal(t, tc.want, prefsOf(t, db, "refresh."))
		})
	}
}
