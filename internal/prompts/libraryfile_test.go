package prompts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// refusePref is a trigger that makes storing the preference key fail.
func refusePref(key string) string {
	return `CREATE TRIGGER refuse_pref BEFORE INSERT ON prefs WHEN NEW.key = '` + key + `'
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`
}

func TestSaveWritesNoFileForALibraryItCannotRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "prompts.json")
	file := NewLibraryFile(path, "", nil)

	require.ErrorContains(t, file.Save(t.Context(), NewStore(testutil.ClosedDB(t), nil)), "read setting "+prefDefaultPR)
	assert.NoFileExists(t, path)

	s, _ := seeded(t, nil)
	require.NoError(t, s.store.prefs.Set(t.Context(), prefSeededSlots, "not a list"))

	require.ErrorContains(t, file.Save(t.Context(), s.store), "read setting "+prefSeededSlots)
	assert.NoFileExists(t, path)
}

func TestRestoreRestoresAllOfTheLibraryOrNothing(t *testing.T) {
	t.Parallel()

	b := bundleOf(mine("Audit"))
	b.Defaults = BundleDefaults{Issue: "Audit"}
	b.StartersAdded = []string{StarterReviewPR}
	b.SlotsSeeded = []string{string(DefaultSlotChecksFailing)}
	path := writeBundle(t, b)

	tests := map[string]string{
		"a prompt that cannot be added":       refusePromptInserts,
		"the starters that cannot be noted":   refusePref(prefSeeded),
		"the slots that cannot be noted":      refusePref(prefSeededSlots),
		"a default that cannot be made again": refusePref(prefDefaultIssue),
	}

	for name, refuse := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			s, db := empty(t, nil)
			testutil.Exec(t, db, refuse)

			restored, err := Restore(t.Context(), s.store, "", NewLibraryFile(path, "", nil))
			require.ErrorContains(t, err, "restore the prompts")
			require.ErrorContains(t, err, "refused by the test")
			assert.Equal(t, Restored{}, restored)

			all, err := s.store.All(t.Context())
			require.NoError(t, err)
			assert.Empty(t, all, "no prompt is restored")
			assert.Empty(t, promptPrefs(t, db), "and the library stays new for Seed to start")
		})
	}
}
