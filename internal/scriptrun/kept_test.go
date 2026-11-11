package scriptrun

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// keptState is what the tests' runs keep beside their records.
type keptState struct {
	Files []string `json:"files"`
}

// keptFiles returns the names of the kept states in the log folder.
func (h *harness) keptFiles() []string {
	h.t.Helper()

	names, err := filepath.Glob(filepath.Join(h.e.files.dir, "*"+keptSuffix))
	require.NoError(h.t, err)

	return names
}

func TestARunKeepsItsStateBesideItsRecordUntilItsEndIsRecorded(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	state := &keptState{}
	spec := h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	spec.Intent, spec.Keep = map[string]any{"scope": "all"}, state
	spec.Before = func(ctx context.Context) ([]string, error) {
		state.Files = []string{"README.md"} // Before fills it in, as a checks run's does

		return nil, nil
	}

	id := h.start(spec)
	h.waitReady(id)

	var got keptState

	found, err := h.e.Kept(id, &got)
	require.NoError(t, err)
	require.True(t, found, "kept while the run goes")
	assert.Equal(t, keptState{Files: []string{"README.md"}}, got)

	run, err := h.s.Run(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"scope": "all"}, run.Result.Intent, "the window never gets it")

	require.NoError(t, h.s.Stop(t.Context(), id))
	rec := h.ended(id)

	assert.Equal(t, map[string]any{"scope": "all"}, rec.Result.Intent, "nor does the record of its end")
	assert.Empty(t, h.keptFiles(), "removed once the end is recorded")

	found, err = h.e.Kept(id, &got)
	require.NoError(t, err)
	assert.False(t, found)
}

func TestAKeptStateStaysForTheNextStartWhenTheEndCantBeRecorded(t *testing.T) {
	t.Parallel()

	var db *sql.DB

	h := newHarness(t, func(cfg *Config) { db = cfg.DB })

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	spec := h.spec(testutil.FakeRun{})
	spec.Keep = keptState{Files: []string{"a"}}

	id := h.start(spec)
	require.Eventually(t, func() bool { return h.e.liveRun(id) == nil }, wait, poll, "run %d ends", id)

	var got keptState

	found, err := h.e.Kept(id, &got)
	require.NoError(t, err)
	assert.True(t, found, "the run is still recorded as going, so its Verifier needs it")
}

func TestARunThatCantStartKeepsNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{})
	spec.Argv = []string{filepath.Join(t.TempDir(), "missing")}
	spec.Keep = keptState{Files: []string{"a"}}

	id, err := h.e.Start(t.Context(), spec)
	require.Error(t, err)

	h.ended(id)
	assert.Empty(t, h.keptFiles())
}

func TestAKeptStateGoesWithTheRunsFiles(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.e.files.ensureDir())
	require.NoError(t, h.e.files.writeKept(7, []byte(`{"files":[]}`)))

	orphans, err := h.e.files.orphans(map[int64]bool{})
	require.NoError(t, err)
	assert.Equal(t, []int64{7}, orphans, "a kept state alone is a forgotten run's file")

	require.NoError(t, h.e.files.clear(7))
	assert.Empty(t, h.keptFiles())

	_, err = os.Stat(h.e.files.kept(7))
	require.ErrorIs(t, err, os.ErrNotExist)

	var got keptState

	require.NoError(t, os.WriteFile(h.e.files.kept(8), []byte("not json"), 0o600))

	found, err := h.e.Kept(8, &got)
	require.Error(t, err)
	assert.False(t, found, "a state that can't be read is none")
}
