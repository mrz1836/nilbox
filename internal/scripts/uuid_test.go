package scripts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestASavedScriptKeepsItsUUIDAndANewOneNeverTakesTheWindows(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	sent := fixAll()
	sent.UUID = keptUUID

	saved := r.save(sent)
	assert.Regexp(t, uuidPattern, saved.UUID)
	assert.NotEqual(t, keptUUID, saved.UUID, "Save gives a new script a UUID of its own")

	stored := saved.UUID
	saved.Name, saved.UUID = "Format", keptUUID
	edited := r.save(saved)
	assert.Equal(t, "Format", edited.Name)
	assert.Equal(t, stored, edited.UUID, "an edit keeps the UUID stored, whatever the window sent")

	got, _, err := r.svc.yours.get(t.Context(), saved.ID)
	require.NoError(t, err)
	assert.Equal(t, edited, got)
}

func TestGiveUUIDsGivesEachScriptWithoutOne(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	y := newYourStore(db, nil)

	kept, err := y.insert(t.Context(), y.q, named(fixAllName), 0)
	require.NoError(t, err)

	// A build from before UUIDs inserts its scripts with none.
	_, err = db.ExecContext(t.Context(), `INSERT INTO user_scripts (name, command, created_at, updated_at)
		VALUES ('Old', 'echo old', '2026-11-01 10:00:00+00:00', '2026-11-01 10:00:00+00:00'),
			('Older', 'echo older', '2026-11-01 10:00:00+00:00', '2026-11-01 10:00:00+00:00')`)
	require.NoError(t, err)

	given, err := GiveUUIDs(t.Context(), db)
	require.NoError(t, err)
	assert.Equal(t, 2, given)

	all, err := y.all(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, kept.UUID, all[0].UUID, "a script with a UUID keeps it")
	assert.Regexp(t, uuidPattern, all[1].UUID)
	assert.Regexp(t, uuidPattern, all[2].UUID)
	assert.NotEqual(t, all[1].UUID, all[2].UUID)

	again, err := GiveUUIDs(t.Context(), db)
	require.NoError(t, err)
	assert.Zero(t, again, "nothing to give the next time")

	none, err := GiveUUIDs(t.Context(), nil)
	require.NoError(t, err)
	assert.Zero(t, none)
}

func TestRunAgainRefusesAScriptWhoseIDNowNamesAnother(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: yoursRef(u.ID)})
	require.NoError(t, err)
	r.ended(id)

	// Renamed, it is the same script, and runs again.
	u.Name = "Format"
	r.save(u)

	again, err := r.runs.RunAgain(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, scriptrun.StatusSucceeded, r.ended(again).Status)

	// Deleted, its ID is given to the next script saved, which is another.
	require.NoError(t, r.svc.Delete(t.Context(), u.ID))

	other := r.save(named("Deploy"))
	require.Equal(t, u.ID, other.ID, "the ID is given again")

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, "The script was deleted.", uierr.Message(err))
	assert.Equal(t, 2, r.recorded(), "nothing else ran")
}

func TestRunAgainOfARunFromBeforeUUIDsFindsItsScriptByID(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	_, err := NewRerunner(r.svc).Rerun(t.Context(), scriptrun.Record{
		Kind: scriptrun.KindScript, Dir: dir,
		Result: scriptrun.Result{Intent: Intent{Ref: yoursRef(u.ID), Inputs: map[string]string{}, Args: []string{}}},
	})
	require.NoError(t, err)
}

func TestABackupFindsAScriptByItsUUID(t *testing.T) {
	t.Parallel()

	setUp := func(t *testing.T) (*rig, UserScript, PortableScripts) {
		t.Helper()

		r := newRig(t)
		u := r.save(fixAll())
		r.save(named(testName))

		// The file's copy of Fix all was renamed and changed since.
		p := portable(u)
		p.Name, p.Command = "Format", "magex format:fix --all"

		return r, u, PortableScripts{Scripts: []PortableScript{p}}
	}

	t.Run("replace saves the file's over it in place", func(t *testing.T) {
		t.Parallel()

		r, u, in := setUp(t)

		plan, count := r.importScripts(in, backup.ModeReplace)
		assert.Equal(t, []string{"update Format", "remove " + testName}, changes(plan.Plan))
		assert.Equal(t, "was “Fix all”", plan.Items[0].Detail)
		assert.Equal(t, backup.Count{Update: 1, Remove: 1}, count)

		all, err := r.svc.yours.all(t.Context())
		require.NoError(t, err)
		require.Len(t, all, 1)
		assert.Equal(t, u.ID, all[0].ID, "the script keeps its ID")
		assert.Equal(t, u.UUID, all[0].UUID, "and its UUID")
		assert.Equal(t, "magex format:fix --all", all[0].Command)
	})

	t.Run("merge adds the file's as a copy", func(t *testing.T) {
		t.Parallel()

		r, u, in := setUp(t)

		plan, count := r.importScripts(in, backup.ModeMerge)
		assert.Equal(t, []string{"add Format"}, changes(plan.Plan))
		assert.Equal(t, backup.Count{Add: 1}, count)

		all, err := r.svc.yours.all(t.Context())
		require.NoError(t, err)
		require.Len(t, all, 3)
		assert.Equal(t, u, all[0], "the user's own stays as it is")
		assert.Equal(t, "Format", all[2].Name)
		assert.Regexp(t, uuidPattern, all[2].UUID)
		assert.NotEqual(t, u.UUID, all[2].UUID, "the copy has a UUID of its own")
	})

	t.Run("one the same is kept", func(t *testing.T) {
		t.Parallel()

		r, u, _ := setUp(t)

		plan, count := r.importScripts(PortableScripts{Scripts: []PortableScript{portable(u)}}, backup.ModeReplace)
		assert.Equal(t, []string{"same " + fixAllName, "remove " + testName}, changes(plan.Plan))
		assert.Equal(t, backup.Count{Same: 1, Remove: 1}, count)
	})
}
