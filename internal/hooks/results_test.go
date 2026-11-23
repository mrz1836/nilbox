package hooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// fakeHistory is the runner's history: one run, or a failure.
type fakeHistory struct {
	run scriptrun.Run
	err error
}

func (f fakeHistory) Run(context.Context, int64) (scriptrun.Run, error) {
	return f.run, f.err
}

func (f fakeHistory) Runs(context.Context, scriptrun.Filter) ([]scriptrun.Run, error) {
	return []scriptrun.Run{f.run}, f.err
}

func TestResultsReadARunsOutcomeTyped(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	ended := func(kind scriptrun.Kind, outcome any) *Service {
		return NewService(Config{History: fakeHistory{run: scriptrun.Run{
			ID: 7, Kind: kind, Status: scriptrun.StatusFailed, Result: scriptrun.Result{Outcome: outcome},
		}}})
	}

	got, err := ended(scriptrun.KindHooks, map[string]any{"action": "replace", "outcome": "replaced"}).ActionResult(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, ActionResult{Action: ActionReplace, Outcome: OutcomeReplaced, Removed: []string{}}, got)

	checks, err := ended(scriptrun.KindChecks, map[string]any{
		"scope": "all", "checks": []any{map[string]any{"name": "eof"}}, "changed": []any{map[string]any{"path": "a"}},
	}).ChecksResult(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, []CheckResult{{Name: CheckEOF, Detail: []string{}, ChangedFiles: []string{}}}, checks.Checks)
	assert.Equal(t, []ChangedFile{{Path: "a", By: []Check{}}}, checks.Changed)
	assert.NotNil(t, checks.Installs)
	assert.NotNil(t, checks.Errors)

	for _, outcome := range []any{nil, map[string]any{"action": "explode"}, "not an object", map[string]any{"action": 3}} {
		_, err = ended(scriptrun.KindHooks, outcome).ActionResult(ctx, 7)
		require.ErrorIs(t, err, ErrNoResult, "%v", outcome)
	}

	_, err = ended(scriptrun.KindChecks, map[string]any{"scope": "everything"}).ChecksResult(ctx, 7)
	require.ErrorIs(t, err, ErrNoResult)

	_, err = ended(scriptrun.KindChecks, map[string]any{"scope": "all"}).ActionResult(ctx, 7)
	require.ErrorIs(t, err, ErrNoResult)

	_, err = NewService(Config{History: fakeHistory{err: scriptrun.ErrNotFound}}).ChecksResult(ctx, 7)
	require.ErrorIs(t, err, scriptrun.ErrNotFound)
}

func TestAPageWhoseHistoryCantBeRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.svc.history = fakeHistory{err: errFake}

	assert.Zero(t, row(t, r.page(false), dir).LastChecks)
	assert.Contains(t, r.logs.String(), "couldn't read the checks runs")
}

func TestAServiceWithoutItsOptionalParts(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{})

	assert.Equal(t, ToolFacts{Features: svc.tool(t.Context()).Features, Copies: []string{}}, svc.tool(t.Context()))
	assert.Empty(t, svc.lastChecks(t.Context()))
	assert.Equal(t, ToolNote{Show: true, Open: true}, svc.toolNote(t.Context(), factsAt("/opt/go-pre-commit", "1.13.1")))

	env, read := svc.env()
	assert.True(t, read)
	assert.NotEmpty(t, env)

	where, err := svc.where(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, scriptrun.RunsInApp, where)
}
