package settings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/heldruns"
	"github.com/mrz1836/nilbox/internal/prefs"
)

func TestSetIgnoredWorkflowsStoresAndReadsBack(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.IgnoredWorkflows, "a fresh install ignores no workflow")

	got, err = svc.SetIgnoredWorkflows(ctx, []string{" Auto-merge Trigger ", "", "Deploy Preview", "Auto-merge Trigger"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Auto-merge Trigger", "Deploy Preview"}, got.IgnoredWorkflows,
		"trimmed, blanks and repeats left out, in order")

	stored, ok, err := prefs.New(db, nil).Get(ctx, KeyIgnoredWorkflows)
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `["Auto-merge Trigger", "Deploy Preview"]`, stored)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got, again, "the setter returns the settings as stored")

	got, err = svc.SetIgnoredWorkflows(ctx, []string{"Deploy Preview", "deploy preview"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Deploy Preview", "deploy preview"}, got.IgnoredWorkflows,
		"names are GitHub's, matched case and all")

	got, err = svc.SetIgnoredWorkflows(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.IgnoredWorkflows, "an empty list ignores none")

	// What no build stores reads as none.
	setPref(t, db, KeyIgnoredWorkflows, "Auto-merge Trigger")

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.IgnoredWorkflows)
}

func TestSetIgnoredWorkflowsRefusesWhatCantBeAName(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	_, err := svc.SetIgnoredWorkflows(ctx, []string{"Deploy Preview"})
	require.NoError(t, err)

	many := make([]string, heldruns.MaxIgnored+1)
	for i := range many {
		many[i] = strings.Repeat("w", i+1)
	}

	for _, names := range [][]string{many, {strings.Repeat("x", heldruns.MaxNameLength+1)}, {"CI\nDeploy"}} {
		got, setErr := svc.SetIgnoredWorkflows(ctx, names)
		require.Error(t, setErr)
		assert.Equal(t, Settings{}, got)
	}

	stored, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"Deploy Preview"}, stored.IgnoredWorkflows, "a refused list stores nothing")
}

func TestSetIgnoredWorkflowsThatCannotWriteStoresNothing(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	_, err := db.ExecContext(ctx, refusePrefs)
	require.NoError(t, err)

	got, err := svc.SetIgnoredWorkflows(ctx, []string{"Auto-merge Trigger"})
	require.ErrorContains(t, err, KeyIgnoredWorkflows)
	assert.Equal(t, Settings{}, got)

	stored, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, stored.IgnoredWorkflows)
}
