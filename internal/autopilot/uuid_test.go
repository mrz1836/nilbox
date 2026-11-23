package autopilot_test

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// uuidPattern matches a UUID the store gives a routine: a random one, in its canonical form.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// routineUUIDs lists the UUIDs of r's routines, in their order.
func routineUUIDs(t *testing.T, r *rig) []string {
	t.Helper()

	routines := r.list(t)
	out := make([]string, 0, len(routines))

	for _, rt := range routines {
		out = append(out, rt.UUID)
	}

	return out
}

func TestEveryRoutineHasAUUIDOfItsOwnThatNeverChanges(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	first := r.save(t, draft(firstName, notifySpec()))
	second := r.save(t, draft("Second", agentSpec()))

	assert.Regexp(t, uuidPattern, first.UUID)
	assert.Regexp(t, uuidPattern, second.UUID)
	assert.NotEqual(t, first.UUID, second.UUID)

	d := draft("First, renamed", notifySpec())
	d.ID = first.ID
	renamed := r.save(t, d)
	assert.Equal(t, first.UUID, renamed.UUID, "an edit keeps the UUID")

	dup, err := r.svc.Duplicate(t.Context(), first.ID)
	require.NoError(t, err)
	assert.Regexp(t, uuidPattern, dup.UUID)
	assert.NotEqual(t, first.UUID, dup.UUID, "a copy has a UUID of its own")
}

func TestRestoreBringsEachRoutineBackUnderItsUUID(t *testing.T) {
	t.Parallel()

	used, setAside := usedRig(t)
	want := routineUUIDs(t, used)
	require.Len(t, want, 2)

	t.Run("from the database set aside", func(t *testing.T) {
		t.Parallel()

		fresh := newRig(t, autopilotprefs.ModeAuto)

		_, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, setAside, fresh.file)
		require.NoError(t, err)
		assert.Equal(t, want, routineUUIDs(t, fresh))
	})

	t.Run("from the copy", func(t *testing.T) {
		t.Parallel()

		fresh := newRig(t, autopilotprefs.ModeAuto)

		_, err := autopilot.Restore(t.Context(), fresh.db, fresh.clock, "", used.file)
		require.NoError(t, err)
		assert.Equal(t, want, routineUUIDs(t, fresh))
	})
}

func TestAnImportKeepsARoutinesUUIDWhereNoRoutineHasIt(t *testing.T) {
	t.Parallel()

	from := newRig(t, autopilotprefs.ModeAsk)
	mine := from.save(t, draft(firstName, notifySpec()))

	path := filepath.Join(t.TempDir(), "routines.json")
	_, err := from.svc.Export(t.Context(), path, nil, false)
	require.NoError(t, err)

	to := newRig(t, autopilotprefs.ModeAsk)
	_, err = to.svc.Import(t.Context(), autopilot.ImportRequest{Path: path})
	require.NoError(t, err)
	assert.Equal(t, []string{mine.UUID}, routineUUIDs(t, to), "the routine keeps its UUID on another Mac")

	// Brought in where it came from, it is a routine of its own, beside the one it was.
	_, err = from.svc.Import(t.Context(), autopilot.ImportRequest{Path: path})
	require.NoError(t, err)

	got := routineUUIDs(t, from)
	require.Len(t, got, 2)
	assert.Equal(t, mine.UUID, got[0])
	assert.Regexp(t, uuidPattern, got[1])
	assert.NotEqual(t, mine.UUID, got[1], "a UUID a routine here has is never given twice")
}
