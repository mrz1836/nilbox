package prompts

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestAddStarterBringsBackADeletedStarter checks that a starter the user deleted, which
// Seed never adds back, comes back on AddStarter, once, as the build ships it.
func TestAddStarterBringsBackADeletedStarter(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	gone := byNickname(t, s, "Resolve conflicts")
	require.NoError(t, s.SetSlotDefault(ctx, DefaultSlotConflicts, 0))
	require.NoError(t, s.Delete(ctx, gone.ID))
	require.NoError(t, Seed(ctx, s.store))
	assert.NotContains(t, nicknames(t, s), "Resolve conflicts", "Seed leaves a deleted starter deleted")

	back, err := s.AddStarter(ctx, StarterResolveConflicts)
	require.NoError(t, err)
	assert.Equal(t, StarterResolveConflicts, back.StarterKey)
	assert.Equal(t, "Resolve conflicts", back.Nickname)
	assert.Equal(t, resolveConflictsBody, back.Body)
	assert.False(t, back.Modified)

	again, err := s.AddStarter(ctx, StarterResolveConflicts)
	require.NoError(t, err)
	assert.Equal(t, back.ID, again.ID, "a starter the library holds isn't added twice")

	require.NoError(t, Seed(ctx, s.store))

	count := 0

	for _, n := range nicknames(t, s) {
		if n == "Resolve conflicts" {
			count++
		}
	}

	assert.Equal(t, 1, count, "Seed adds no second copy after it")
}

// TestAddStarterKeepsAnEditedCopy checks that a starter the library still holds, edited or
// not, is the one AddStarter returns, untouched.
func TestAddStarterKeepsAnEditedCopy(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	heal := byNickname(t, s, "Heal Dependabot PR")
	in := toInput(heal)
	in.Body = "Heal {{.URL}} gently."

	_, err := s.Update(ctx, heal.ID, in)
	require.NoError(t, err)

	got, err := s.AddStarter(ctx, StarterHealDependabot)
	require.NoError(t, err)
	assert.Equal(t, heal.ID, got.ID)
	assert.Equal(t, "Heal {{.URL}} gently.", got.Body)
}

// TestAddStarterRefusesAnUnknownKey checks that only a starter this build ships is added.
func TestAddStarterRefusesAnUnknownKey(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	_, err := s.AddStarter(t.Context(), "a-later-starter")
	require.ErrorIs(t, err, ErrUnknownStarter)
}

// TestAddStarterAddsNothingWhenAStepFails checks that a starter whose adding fails part of
// the way leaves the library as it was, and says what failed.
func TestAddStarterAddsNothingWhenAStepFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		spoil func(t *testing.T, s *Service, db *sql.DB)
		want  string
	}{
		{
			name:  "the prompts cannot be read",
			spoil: func(t *testing.T, _ *Service, db *sql.DB) { t.Helper(); testutil.Exec(t, db, "DROP TABLE prompts") },
			want:  "read the prompts",
		},
		{
			name:  "the defaults cannot be read",
			spoil: func(t *testing.T, _ *Service, db *sql.DB) { t.Helper(); testutil.Exec(t, db, "DROP TABLE prefs") },
			want:  "read setting " + prefDefaultPR,
		},
		{
			name:  "the starter cannot be stored",
			spoil: func(t *testing.T, _ *Service, db *sql.DB) { t.Helper(); testutil.Exec(t, db, refusePromptInserts) },
			want:  `add the "Resolve a pull request's merge conflicts" starter prompt`,
		},
		{
			name: "the record of starters cannot be read",
			spoil: func(t *testing.T, s *Service, _ *sql.DB) {
				t.Helper()
				require.NoError(t, s.store.prefs.Set(t.Context(), prefSeeded, "not a list"))
			},
			want: "read setting " + prefSeeded,
		},
		{
			name:  "the record of starters cannot be saved",
			spoil: func(t *testing.T, _ *Service, db *sql.DB) { t.Helper(); testutil.Exec(t, db, refusePref(prefSeeded)) },
			want:  "save setting " + prefSeeded,
		},
		{
			name: "the starter is not stored as one",
			spoil: func(t *testing.T, _ *Service, db *sql.DB) {
				t.Helper()
				testutil.Exec(t, db, `CREATE TRIGGER drop_starter_key AFTER INSERT ON prompts
BEGIN UPDATE prompts SET starter_key = '' WHERE id = NEW.id; END`)
			},
			want: "the starter prompt isn't stored",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := empty(t, nil)
			tt.spoil(t, s, db)

			_, err := s.AddStarter(t.Context(), StarterResolveConflicts)
			require.ErrorContains(t, err, tt.want)

			if tt.name != "the prompts cannot be read" {
				all, allErr := s.store.All(t.Context())
				require.NoError(t, allErr)
				assert.Empty(t, all, "nothing is added")
			}
		})
	}

	_, err := closedService(t).AddStarter(t.Context(), StarterResolveConflicts)
	require.Error(t, err, "a database that cannot start the transaction")
}
