package scripts

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// endings collects what a routine's runs told as they ended.
type routineEnds struct {
	mu   sync.Mutex
	ends []RoutineEnd
}

func (e *routineEnds) hear(_ context.Context, _ int64, end RoutineEnd) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.ends = append(e.ends, end)
}

func (e *routineEnds) told() []RoutineEnd {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]RoutineEnd{}, e.ends...)
}

func TestARoutineReadsTheUsersScriptsWithTheirLooks(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	all := r.save(fixAll())

	repos := fixAll()
	repos.Name, repos.Scope, repos.Repos = "Tidy", ScopeRepos, []string{"acme-org/sdk", "acme-org/web"}
	repos = r.save(repos)

	gomod := fixAll()
	gomod.Name, gomod.Scope, gomod.Confirm = "Vet", ScopeGo, true
	gomod = r.save(gomod)

	got, err := NewRoutines(r.svc).Mine(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 3)

	assert.Equal(t, []int64{all.ID, repos.ID, gomod.ID}, []int64{got[0].ID, got[1].ID, got[2].ID}, "in the user's order")
	assert.Equal(t, []string{"every clone", "acme-org/sdk and acme-org/web", "Go repositories"},
		[]string{got[0].Shows, got[1].Shows, got[2].Shows})
	assert.True(t, got[2].Confirm, "Ask before running comes with it")
	assert.Equal(t, yoursScript(all).look, got[0].Look)

	edited := all
	edited.Command = "magex test"
	r.save(edited)

	again, err := NewRoutines(r.svc).Mine(t.Context())
	require.NoError(t, err)
	assert.NotEqual(t, got[0].Look, again[0].Look, "an edit changes its look")
}

func TestARoutineRunsTheUsersScriptInTheAppAndHearsItsEnd(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)

	u := fixAll()
	u.RunsIn = integrationprefs.RunsInTerminal
	u = r.save(u)

	mine, err := NewRoutines(r.svc).Mine(t.Context())
	require.NoError(t, err)

	ends := &routineEnds{}

	id, err := NewRoutines(r.svc).Run(t.Context(), RoutineRun{ID: u.ID, Dir: r.shown(dir), Look: mine[0].Look, Ended: ends.hear})
	require.NoError(t, err)

	run := r.ended(id)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, scriptrun.RunsInApp, run.RunsIn, "a routine's run is in the app, whatever the script says")

	told := ends.told()
	require.Len(t, told, 1, "its end, once recorded")
	assert.Equal(t, scriptrun.StatusSucceeded, told[0].Outcome.Status)
	assert.False(t, told[0].Quit)
	assert.False(t, told[0].Stopped)
}

func TestARoutineRefusesAScriptSavedOtherwiseSinceItsCheck(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", nil)
	u := r.save(fixAll())

	mine, err := NewRoutines(r.svc).Mine(t.Context())
	require.NoError(t, err)

	u.Command = "rm -rf ~"
	r.save(u)

	_, err = NewRoutines(r.svc).Run(t.Context(), RoutineRun{ID: u.ID, Dir: r.shown(dir), Look: mine[0].Look})
	require.ErrorIs(t, err, ErrChanged)
	assert.Zero(t, r.recorded(), "nothing ran")
}

func TestARoutineRunsOnlyWhereTheScriptShows(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", "acme-org/web", nil)

	u := fixAll()
	u.Scope, u.Repos = ScopeRepos, []string{"acme-org/sdk"}
	u = r.save(u)

	_, err := NewRoutines(r.svc).Run(t.Context(), RoutineRun{ID: u.ID, Dir: r.shown(dir)})
	require.ErrorIs(t, err, ErrNotFound)

	_, err = NewRoutines(r.svc).Run(t.Context(), RoutineRun{ID: u.ID + 100, Dir: r.shown(dir)})
	require.ErrorIs(t, err, ErrNotFound, "a script deleted")
	assert.Zero(t, r.recorded())
}
