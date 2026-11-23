package migrate_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// v4UUID is a version 4 UUID with the RFC 9562 variant, in its canonical form.
var v4UUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestRecordUUIDsGiveEveryRowItsOwn migrates a database to 0115, stores prompts, user
// scripts, and routines, one routine deleted softly, then applies 0116_record_uuids: every
// row of the three tables holds its own canonical version 4 UUID, and keeps every other
// column.
func TestRecordUUIDsGiveEveryRowItsOwn(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	through115, _ := embeddedThrough(t, 115)
	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through115})
	require.NoError(t, err)

	execAll(t, db,
		`INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
			VALUES ('Fix checks', 'fix-checks', 'pr', 'Fix the checks.', `+at+`, `+at+`),
				('Triage', 'triage', 'issue', 'Triage it.', `+at+`, `+at+`),
				('Upgrade', 'upgrade', 'repo', 'Upgrade it.', `+at+`, `+at+`)`,
		`INSERT INTO user_scripts (name, command, created_at, updated_at)
			VALUES ('Lint', 'magex lint', `+at+`, `+at+`), ('Test', 'magex test', `+at+`, `+at+`)`,
		`INSERT INTO autopilot_routines (name, name_key, mode, spec, created_at, updated_at)
			VALUES ('Fix my PRs', 'fix my prs', 'ask', '{"version":1}', `+at+`, `+at+`)`,
		`INSERT INTO autopilot_routines (name, name_key, mode, spec, created_at, updated_at, deleted_at)
			VALUES ('Gone', 'gone', 'dryRun', '{"version":1}', `+at+`, `+at+`, `+at+`)`,
	)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	var all []string

	for table, want := range map[string]int{"prompts": 3, "user_scripts": 2, "autopilot_routines": 2} {
		uuids := testutil.Column[string](t, db, `SELECT uuid FROM `+table+` ORDER BY id`)
		require.Len(t, uuids, want, table)

		for _, u := range uuids {
			assert.Regexp(t, v4UUID, u, table)
		}

		all = append(all, uuids...)
	}

	seen := make(map[string]bool, len(all))
	for _, u := range all {
		assert.False(t, seen[u], "every row has its own UUID")
		seen[u] = true
	}

	assert.Equal(t, []string{"fix-checks", "triage", "upgrade"},
		testutil.Column[string](t, db, `SELECT nickname FROM prompts ORDER BY id`), "the prompts keep what they held")
	assert.Equal(t, []string{"magex lint", "magex test"},
		testutil.Column[string](t, db, `SELECT command FROM user_scripts ORDER BY id`), "the scripts keep what they held")
	assert.Equal(t, []string{"Fix my PRs", "Gone"},
		testutil.Column[string](t, db, `SELECT name FROM autopilot_routines ORDER BY id`),
		"the routines keep what they held, the one deleted too")
}

// TestRecordUUIDsLetRowsWaitWithoutOne checks the unique index on each uuid column: two
// rows a build from before 0116 inserts with no UUID may wait for one side by side, but two
// rows may never share a UUID.
func TestRecordUUIDsLetRowsWaitWithoutOne(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	execAll(t, db,
		`INSERT INTO user_scripts (name, command, created_at, updated_at)
			VALUES ('Lint', 'magex lint', `+at+`, `+at+`), ('Test', 'magex test', `+at+`, `+at+`)`,
		`INSERT INTO user_scripts (uuid, name, command, created_at, updated_at)
			VALUES ('6f9619ff-8b86-4d01-b42d-00cf4fc964ff', 'Build', 'magex build', `+at+`, `+at+`)`,
	)

	_, err = db.ExecContext(t.Context(), `INSERT INTO user_scripts (uuid, name, command, created_at, updated_at)
		VALUES ('6f9619ff-8b86-4d01-b42d-00cf4fc964ff', 'Again', 'magex build', `+at+`, `+at+`)`)
	require.Error(t, err, "two scripts never share a UUID")
}
