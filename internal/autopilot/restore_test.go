package autopilot_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// usedRig returns a rig over a database at its own path whose user made three routines,
// switched the first on, and deleted the third.
func usedRig(t *testing.T) (*rig, string) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "set-aside.db")
	r := rigAt(t, testutil.DBAt(t, path), filepath.Join(dir, "autopilot.json"), autopilotprefs.ModeAsk)

	first := r.save(t, autopilot.Draft{Name: firstName, Description: "Mine.", Spec: notifySpec(), TemplateKey: "first"})
	r.save(t, draft("Second", agentSpec()))
	third := r.save(t, draft("Third", notifySpec()))

	_, err := r.svc.SetEnabled(t.Context(), first.ID, true)
	require.NoError(t, err)
	require.NoError(t, r.svc.Delete(t.Context(), third.ID))

	return r, path
}

func TestRestoreBringsTheRoutinesBackOffFromTheDatabaseSetAside(t *testing.T) {
	t.Parallel()

	used, setAside := usedRig(t)
	fresh := newRig(t, autopilotprefs.ModeAuto)

	got, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, setAside, fresh.file)
	require.NoError(t, err)
	assert.Equal(t, autopilot.Restored{Count: 2, From: setAside}, got)

	routines := fresh.list(t)
	require.Len(t, routines, 2, "a deleted routine stays deleted")
	assert.Equal(t, []string{firstName, "Second"}, []string{routines[0].Name, routines[1].Name}, "in their order")

	before := used.list(t)

	for i, rt := range routines {
		assert.False(t, rt.Enabled, "%s arrives off", rt.Name)
		assert.Equal(t, before[i].Spec, rt.Spec)
		assert.Equal(t, before[i].Mode, rt.Mode)
		assert.Equal(t, viewer, rt.Account, "the account it belonged to")
	}

	assert.Equal(t, "Mine.", routines[0].Description)
	assert.Equal(t, "first", routines[0].TemplateKey)

	again, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, setAside, fresh.file)
	require.NoError(t, err)
	assert.Zero(t, again, "a table that isn't new is left as it is")
	assert.Len(t, fresh.list(t), 2)
}

func TestRestoreBringsTheRoutinesBackOffFromTheCopyAlone(t *testing.T) {
	t.Parallel()

	used, _ := usedRig(t)
	fresh := newRig(t, autopilotprefs.ModeAuto)

	got, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, "", used.file)
	require.NoError(t, err)
	assert.Equal(t, autopilot.Restored{Count: 2, From: used.file.Path()}, got)

	routines := fresh.list(t)
	require.Len(t, routines, 2)
	assert.Equal(t, firstName, routines[0].Name)
	assert.False(t, routines[0].Enabled, "it was on, and arrives off")
	assert.Empty(t, routines[0].Account, "the copy names no account: it is the account signed in's")
	assert.Equal(t, used.list(t)[0].Spec, routines[0].Spec)
}

func TestRestoreFallsBackToTheCopyWhenTheDatabaseSetAsideHasNoRoutines(t *testing.T) {
	t.Parallel()

	used, _ := usedRig(t)

	older := filepath.Join(t.TempDir(), "older.db")
	olderDB := testutil.DBAt(t, older)
	testutil.Exec(t, olderDB, `DROP TABLE autopilot_routines`)

	fresh := newRig(t, autopilotprefs.ModeAuto)

	got, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, older, used.file)
	require.NoError(t, err, "an older build's database simply has no routines")
	assert.Equal(t, used.file.Path(), got.From)
	assert.Equal(t, 2, got.Count)
}

func TestRestoreNamesADatabaseItCantReadAndUsesTheCopy(t *testing.T) {
	t.Parallel()

	used, _ := usedRig(t)
	broken := filepath.Join(t.TempDir(), "broken.db")
	testutil.WriteFile(t, broken, "this is not a database file, just text long enough to have a header")

	fresh := newRig(t, autopilotprefs.ModeAuto)

	got, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, broken, used.file)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read the routines in the database set aside")
	assert.Equal(t, 2, got.Count, "the copy still brings them back")
}

func TestRestoreLeavesATableThatIsntNewAsItIs(t *testing.T) {
	t.Parallel()

	used, _ := usedRig(t)
	fresh := newRig(t, autopilotprefs.ModeAuto)

	gone := fresh.save(t, draft("Deleted here", notifySpec()))
	require.NoError(t, fresh.svc.Delete(t.Context(), gone.ID))

	got, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, "", used.file)
	require.NoError(t, err)
	assert.Zero(t, got, "a routine deleted here was the user's choice, never undone")
	assert.Empty(t, fresh.list(t))
}

func TestRestoreWithNothingToBringBack(t *testing.T) {
	t.Parallel()

	fresh := newRig(t, autopilotprefs.ModeAuto)

	got, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, "", fresh.file)
	require.NoError(t, err, "no copy is no error")
	assert.Zero(t, got)

	got, err = autopilot.Restore(t.Context(), fresh.db, fresh.clock, "", nil)
	require.NoError(t, err)
	assert.Zero(t, got)
}

func TestRestoreKeepsANewerBuildsRoutineAsItWas(t *testing.T) {
	t.Parallel()

	const newer = `{"version":2,"somethingNew":true}`

	path := filepath.Join(t.TempDir(), "autopilot.json")
	testutil.WriteFile(t, path, `{
  "format": "`+autopilot.BundleFormat+`",
  "routines": [
    {"name": "From the future", "mode": "auto", "enabled": true, "spec": `+newer+`},
    {"name": "  Two\nlines ", "mode": "sometimes", "spec": {"version": 1}},
    {"name": "Not a spec", "mode": "ask", "spec": "text"}
  ]
}`)

	fresh := newRig(t, autopilotprefs.ModeAsk)

	got, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, "", autopilot.NewFile(path, "", nil))
	require.NoError(t, err)
	assert.Equal(t, 2, got.Count, "a spec that isn't JSON at all is left out")

	routines := fresh.list(t)
	require.Len(t, routines, 2)

	future := routines[0]
	assert.Equal(t, spec.ReasonNewer, future.BrokenReason)
	assert.Equal(t, newer, fresh.storedSpec(t, future.ID), "stored as that build wrote it")
	assert.Equal(t, autopilotprefs.ModeAuto, future.Mode)
	assert.False(t, future.Enabled)

	assert.Equal(t, "Two lines", routines[1].Name, "a name is made one line")
	assert.Equal(t, autopilotprefs.ModeDryRun, routines[1].Mode, "a mode this build doesn't know is Dry run")
	assert.Equal(t, spec.ReasonInvalid, routines[1].BrokenReason)
}
