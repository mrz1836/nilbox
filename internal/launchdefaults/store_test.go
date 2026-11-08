package launchdefaults

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// acmeOrg is the fake org these tests use.
const acmeOrg = "acme-org"

// errRollback makes a test transaction roll back.
var errRollback = errors.New("roll back")

// stamps returns the stored created_at and updated_at of an org's (name "") or a
// repository's launch defaults as the database holds them.
func stamps(t *testing.T, db *sql.DB, owner, name string) (created, updated string) {
	t.Helper()

	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT CAST(created_at AS TEXT), CAST(updated_at AS TEXT) FROM launch_defaults WHERE owner = ? AND name = ?",
		owner, name,
	).Scan(&created, &updated))

	return created, updated
}

// toggles returns the stored plan_mode and worktree of an org's launch defaults as the
// database holds them: NULL, 0, or 1.
func toggles(t *testing.T, db *sql.DB, owner string) (planMode, worktree sql.NullInt64) {
	t.Helper()

	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT plan_mode, worktree FROM launch_defaults WHERE owner = ? AND name = ''", owner,
	).Scan(&planMode, &worktree))

	return planMode, worktree
}

func TestSaveStampsTheRowFromTheClock(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	clk := testutil.NewClock(time.Date(2026, 1, 10, 7, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60)))
	s := NewStore(db, clk)
	ctx := t.Context()

	saved, err := s.save(ctx, Input{Owner: "Acme-Org", Name: "Example-Repo", Model: "opus"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-01-10T12:00:00Z", saved.UpdatedAt)

	createdAt, updatedAt := stamps(t, db, "Acme-Org", "Example-Repo")
	assert.Equal(t, "2026-01-10 12:00:00+00:00", createdAt, "stored in UTC")
	assert.Equal(t, createdAt, updatedAt)

	clk.Advance(time.Hour)

	saved, err = s.save(ctx, Input{Owner: "acme-org", Name: "example-repo", Effort: "low"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "2026-01-10T13:00:00Z", saved.UpdatedAt)
	assert.Equal(t, "Acme-Org", saved.Owner, "the row keeps the casing it was first saved with")
	assert.Equal(t, "Example-Repo", saved.Name)

	again, updatedAt := stamps(t, db, "Acme-Org", "Example-Repo")
	assert.Equal(t, "2026-01-10 13:00:00+00:00", updatedAt, "a save over stored defaults advances updated_at")
	assert.Equal(t, createdAt, again, "and keeps created_at")
}

func TestSaveRoundTripsInheritedToggles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		planMode, worktree *bool
		wantPlan, wantTree sql.NullInt64
	}{
		{name: "both inherit", wantPlan: sql.NullInt64{}, wantTree: sql.NullInt64{}},
		{
			name: "on and off", planMode: new(true), worktree: new(false),
			wantPlan: sql.NullInt64{Int64: 1, Valid: true}, wantTree: sql.NullInt64{Int64: 0, Valid: true},
		},
		{
			name: "off and inherit", planMode: new(false),
			wantPlan: sql.NullInt64{Int64: 0, Valid: true}, wantTree: sql.NullInt64{},
		},
		{
			name: "inherit and on", worktree: new(true),
			wantPlan: sql.NullInt64{}, wantTree: sql.NullInt64{Int64: 1, Valid: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := testutil.DB(t)
			s := NewStore(db, nil)
			ctx := t.Context()

			// Stored defaults that set both first show a later save replacing them.
			_, err := s.save(ctx, Input{Owner: acmeOrg, Model: "opus", PlanMode: new(true), Worktree: new(true)}, nil)
			require.NoError(t, err)

			saved, err := s.save(ctx, Input{Owner: acmeOrg, Model: "opus", PlanMode: tt.planMode, Worktree: tt.worktree}, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.planMode, saved.PlanMode)
			assert.Equal(t, tt.worktree, saved.Worktree)

			plan, tree := toggles(t, db, acmeOrg)
			assert.Equal(t, tt.wantPlan, plan, "plan_mode is NULL when it inherits")
			assert.Equal(t, tt.wantTree, tree, "worktree is NULL when it inherits")

			all, err := s.All(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.Equal(t, tt.planMode, all[0].PlanMode)
			assert.Equal(t, tt.worktree, all[0].Worktree)
		})
	}
}

func TestStoreRemovesIgnoringCase(t *testing.T) {
	t.Parallel()

	s := NewStore(testutil.DB(t), nil)
	ctx := t.Context()

	_, err := s.save(ctx, Input{Owner: "Acme-Org", Name: "Example-Repo", Model: "opus"}, nil)
	require.NoError(t, err)

	err = s.remove(ctx, acmeOrg, "")
	require.ErrorIs(t, err, ErrNotFound, "an owner's own defaults are a separate row")
	require.EqualError(t, err, "no launch defaults are stored for that org or repository: acme-org")

	require.NoError(t, s.remove(ctx, acmeOrg, "example-repo"))
	require.ErrorIs(t, s.remove(ctx, acmeOrg, "example-repo"), ErrNotFound, "removed once")

	all, err := s.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
	assert.NotNil(t, all, "the UI always gets a list")
}

func TestStoreJoinsATransaction(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	s := NewStore(db, nil)
	ctx := t.Context()

	err := store.InTx(ctx, db, func(tx *sql.Tx) error {
		joined := s.WithTx(tx)

		saved, saveErr := joined.save(ctx, Input{Owner: acmeOrg, Model: "opus"}, []string{"--verbose"})
		require.NoError(t, saveErr)
		assert.Equal(t, []string{"--verbose"}, saved.ExtraArgs)

		all, allErr := joined.All(ctx)
		require.NoError(t, allErr)
		assert.Len(t, all, 1, "a read inside the transaction sees its write")

		return errRollback
	})
	require.ErrorIs(t, err, errRollback)

	all, err := s.All(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "the save rolled back with the transaction")

	require.NoError(t, store.InTx(ctx, db, func(tx *sql.Tx) error {
		_, saveErr := s.WithTx(tx).save(ctx, Input{Owner: acmeOrg, Model: "opus"}, nil)

		return saveErr
	}))

	all, err = s.All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1, "the save committed with the transaction")
}

func TestStoreReportsUnreadableExtraArguments(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	s := NewStore(db, nil)

	// A JSON array the schema accepts, but not a list of strings.
	_, err := db.ExecContext(t.Context(), `INSERT INTO launch_defaults (owner, extra_args, created_at, updated_at)
		VALUES ('acme-org', '[1]', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`)
	require.NoError(t, err)

	_, err = s.All(t.Context())
	require.ErrorContains(t, err, "read the launch defaults: acme-org: extra arguments")
}

func TestSaveReportsARefusedWrite(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	s := NewStore(db, nil)
	ctx := t.Context()

	_, err := db.ExecContext(ctx, `CREATE TRIGGER refuse_launch_default_inserts BEFORE INSERT ON launch_defaults
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	_, err = s.save(ctx, Input{Owner: acmeOrg, Name: "example-repo", Model: "opus"}, nil)
	require.ErrorContains(t, err, "save the launch defaults for acme-org/example-repo")
	require.ErrorContains(t, err, "refused by the test")
}
