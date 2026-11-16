package autopilot_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
)

// unicodeNames are pairs of names that are the same ignoring case in an alphabet SQLite's
// NOCASE doesn't fold, and pairs that differ.
func unicodeNames() []struct {
	a, b string
	same bool
} {
	return []struct {
		a, b string
		same bool
	}{
		{"Über checks", "über checks", true},
		{"ÜBER CHECKS", "über checks", true},
		{"Σύνοψη", "σύνοψη", true},
		{"Kelvin", "Kelvin", true},   // the Kelvin sign folds with K
		{"Straße", "STRASSE", false}, // simple folding never turns one rune into two
		{"über checks", "uber checks", false},
		{"Fix checks", "fix checks", true},
	}
}

func TestNamesFoldAsEqualFoldDoes(t *testing.T) {
	t.Parallel()

	for _, c := range unicodeNames() {
		assert.Equal(t, strings.EqualFold(c.a, c.b), c.same, "%q and %q", c.a, c.b)
		assert.Equal(t, c.same, autopilot.NameKey(c.a) == autopilot.NameKey(c.b), "%q and %q", c.a, c.b)
	}
}

func TestANameTakenInAnyAlphabetIsRefused(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	first := r.save(t, draft("Über checks", notifySpec()))

	for _, name := range []string{"über checks", "ÜBER CHECKS"} {
		_, err := r.svc.Save(t.Context(), draft(name, notifySpec()))
		require.ErrorIs(t, err, autopilot.ErrNameTaken, name)
	}

	other := r.save(t, draft("Andere", notifySpec()))

	_, err := r.svc.Save(t.Context(), autopilot.Draft{ID: other.ID, Name: "über CHECKS", Spec: notifySpec()})
	require.ErrorIs(t, err, autopilot.ErrNameTaken, "nor can another routine be renamed to it")

	renamed := r.save(t, autopilot.Draft{ID: first.ID, Name: "über checks", Spec: notifySpec()})
	assert.Equal(t, "über checks", renamed.Name, "a routine can change its own name's case")

	copied, err := r.svc.Duplicate(t.Context(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, "über checks copy", copied.Name)
}

func TestTheDatabaseHoldsOneLiveRoutineToAFoldedName(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	r.save(t, draft("Über checks", notifySpec()))

	_, err := r.db.ExecContext(t.Context(), `INSERT INTO autopilot_routines
		(name, name_key, mode, spec, created_at, updated_at) VALUES (?, ?, 'ask', '{}', ?, ?)`,
		"über checks", autopilot.NameKey("über checks"), startedAt(), startedAt())
	require.Error(t, err, "the unique index refuses a second routine of the folded name")
}

func TestAnImportMatchesANameInAnyAlphabet(t *testing.T) {
	t.Parallel()

	here := newRig(t, autopilotprefs.ModeAsk)
	kept := here.save(t, draft("Über checks", agentSpec()))

	b := autopilot.Bundle{Format: autopilot.BundleFormat, Routines: []autopilot.BundleRoutine{
		{Name: "über checks", Mode: autopilotprefs.ModeDryRun, Spec: mustSpec(t, notifySpec())},
	}}

	plan, count := importInto(t, here, b, backup.ModeReplace)
	assert.Equal(t, map[string]backup.Change{"über checks": backup.ChangeUpdate}, changes(plan.Plan),
		"one routine, saved over, never one added and one deleted")
	assert.Equal(t, backup.Count{Update: 1}, count)

	got := here.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, kept.ID, got[0].ID)
	assert.Equal(t, "Über checks", got[0].Name, "the match keeps its own name")
}
