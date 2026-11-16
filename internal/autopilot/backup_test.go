package autopilot_test

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/store"
)

// exported returns r's routines in the export format, as a backup holds them.
func exported(t *testing.T, r *rig) autopilot.Bundle {
	t.Helper()

	b, err := autopilot.NewBackupPart(r.svc).Bundle(t.Context(), "v0.5.0", startedAt())
	require.NoError(t, err)

	data, err := autopilot.EncodeBundle(b)
	require.NoError(t, err)

	read, err := autopilot.DecodeBundle(data)
	require.NoError(t, err)

	return read
}

// importInto plans b under mode for r and applies the plan, as a backup's import does, and
// returns the plan and what it did.
func importInto(t *testing.T, r *rig, b autopilot.Bundle, mode backup.Mode) (autopilot.BackupPlan, backup.Count) {
	t.Helper()

	part := autopilot.NewBackupPart(r.svc)

	plan, err := part.PlanBackup(t.Context(), b, mode)
	require.NoError(t, err)

	var count backup.Count

	require.NoError(t, store.InTx(t.Context(), r.db, func(tx *sql.Tx) error {
		var applyErr error

		count, applyErr = part.ApplyBackup(t.Context(), tx, plan)

		return applyErr
	}))

	part.BackupDone(t.Context())

	return plan, count
}

// changes lists each item's label and change.
func changes(p backup.Plan) map[string]backup.Change {
	out := map[string]backup.Change{}
	for _, item := range p.Items {
		out[item.Label] = item.Change
	}

	return out
}

func TestABackupHoldsEveryRoutineButTheDeletedOnes(t *testing.T) {
	t.Parallel()

	used, _ := usedRig(t)
	b := exported(t, used)

	assert.Equal(t, autopilot.BundleFormat, b.Format)
	assert.Equal(t, "v0.5.0", b.App)
	require.Len(t, b.Routines, 2)
	assert.Equal(t, firstName, b.Routines[0].Name)
	assert.True(t, b.Routines[0].Enabled, "it says the routine was on")
	assert.Equal(t, autopilotprefs.ModeDryRun, b.Routines[0].Mode)
	assert.Equal(t, "first", b.Routines[0].Template)
	assert.Equal(t, "Second", b.Routines[1].Name)

	data, err := json.Marshal(b)
	require.NoError(t, err)
	assert.NotContains(t, string(data), viewer, "a backup names no account")
}

func TestImportUnderMergeAddsAndUpdatesAndArrivesOff(t *testing.T) {
	t.Parallel()

	used, _ := usedRig(t)
	b := exported(t, used)

	here := newRig(t, autopilotprefs.ModeAsk)
	kept := here.save(t, draft("Kept here", notifySpec()))
	first := here.save(t, autopilot.Draft{Name: "first", Spec: agentSpec()})

	_, err := here.svc.SetEnabled(t.Context(), first.ID, true)
	require.NoError(t, err)

	plan, count := importInto(t, here, b, backup.ModeMerge)
	assert.Equal(t, map[string]backup.Change{firstName: backup.ChangeUpdate, "Second": backup.ChangeAdd}, changes(plan.Plan))
	assert.Empty(t, plan.Notes)
	assert.Equal(t, backup.Count{Add: 1, Update: 1}, count)

	got := here.list(t)
	require.Len(t, got, 3, "Merge deletes nothing")
	assert.Equal(t, []int64{kept.ID, first.ID, got[2].ID}, ids(got), "an added routine goes at the end")

	assert.Equal(t, "first", got[1].Name, "a match keeps its own name")
	assert.Equal(t, used.list(t)[0].Spec, got[1].Spec, "and takes the file's definition")
	assert.False(t, got[1].Enabled, "and arrives off")
	assert.Equal(t, "Mine.", got[1].Description)

	assert.Equal(t, "Second", got[2].Name)
	assert.False(t, got[2].Enabled)
	assert.Equal(t, viewer, got[2].Account, "an added routine is the account signed in's")

	again, err := autopilot.NewBackupPart(here.svc).PlanBackup(t.Context(), b, backup.ModeMerge)
	require.NoError(t, err)
	assert.Equal(t, map[string]backup.Change{firstName: backup.ChangeSame, "Second": backup.ChangeSame}, changes(again.Plan),
		"importing the same routines again changes nothing")

	assert.Equal(t, []string{"Kept here", "first", "Second"}, copyNames(t, here.file), "the copy is written again")
}

func TestImportUnderReplaceMakesTheRoutinesTheFiles(t *testing.T) {
	t.Parallel()

	used, _ := usedRig(t)
	b := exported(t, used)

	here := newRig(t, autopilotprefs.ModeAsk)
	gone := here.save(t, draft("Only here", notifySpec()))
	second := here.save(t, draft("Second", notifySpec()))
	_, err := here.svc.SetEnabled(t.Context(), second.ID, true)
	require.NoError(t, err)

	plan, count := importInto(t, here, b, backup.ModeReplace)
	assert.Equal(t, map[string]backup.Change{
		firstName: backup.ChangeAdd, "Second": backup.ChangeUpdate, "Only here": backup.ChangeRemove,
	}, changes(plan.Plan))
	assert.Equal(t, backup.Count{Add: 1, Update: 1, Remove: 1}, count)

	got := here.list(t)
	assert.Equal(t, []string{firstName, "Second"}, []string{got[0].Name, got[1].Name}, "exactly the file's, in its order")
	assert.Equal(t, second.ID, got[1].ID, "a match keeps its record")
	assert.False(t, got[1].Enabled, "and arrives off")

	_, err = here.svc.Get(t.Context(), gone.ID)
	require.ErrorIs(t, err, autopilot.ErrNotFound, "a routine the file lacks is deleted")
}

func TestImportLeavesOutWhatItCantUseWithANote(t *testing.T) {
	t.Parallel()

	b := autopilot.Bundle{Format: autopilot.BundleFormat, Routines: []autopilot.BundleRoutine{
		{Name: "From the future", Mode: autopilotprefs.ModeAuto, Spec: json.RawMessage(`{"version":2}`)},
		{Name: "", Mode: autopilotprefs.ModeAsk, Spec: mustSpec(t, notifySpec())},
		{Name: "Twice", Mode: autopilotprefs.ModeAsk, Spec: mustSpec(t, notifySpec())},
		{Name: "TWICE", Mode: autopilotprefs.ModeAsk, Spec: mustSpec(t, notifySpec())},
		{Name: "Garbled", Mode: autopilotprefs.ModeAsk, Spec: json.RawMessage(`[1]`)},
		{Name: "Unknown mode", Mode: "sometimes", Spec: mustSpec(t, notifySpec())},
	}}

	here := newRig(t, autopilotprefs.ModeAuto)
	plan, count := importInto(t, here, b, backup.ModeMerge)

	assert.Equal(t, []string{
		"The routine “From the future” was left out. Made by a newer " + brand.Name + ": update to edit it.",
		"A routine was left out. Give the routine a name.",
		"The routine “TWICE” was left out: the file holds another of that name.",
		"The routine “Garbled” was left out. Its definition can't be read.",
	}, plan.Notes)
	assert.Equal(t, backup.Count{Add: 2}, count)

	got := here.list(t)
	require.Len(t, got, 2)
	assert.Equal(t, autopilotprefs.ModeDryRun, got[1].Mode,
		"an unknown mode takes the default, and a step this build can't run keeps it in Dry run")
}

// mustSpec returns s as stored.
func mustSpec(t *testing.T, s spec.Spec) json.RawMessage {
	t.Helper()

	data, err := spec.Encode(spec.Normalize(s))
	require.NoError(t, err)

	return data
}
