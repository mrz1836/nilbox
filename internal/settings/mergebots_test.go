package settings

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/trust"
)

func TestSetMergeBotsOnStoresAndReadsBack(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.False(t, got.MergeBotsOn, "a fresh install says nothing of auto-merge bots")

	got, err = svc.SetMergeBotsOn(ctx, true)
	require.NoError(t, err)
	assert.True(t, got.MergeBotsOn)

	stored, ok, err := prefs.New(db, nil).Get(ctx, KeyMergeBotsOn)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "true", stored)

	got, err = svc.SetMergeBotsOn(ctx, false)
	require.NoError(t, err)
	assert.False(t, got.MergeBotsOn)

	// What no build stores reads as off.
	setPref(t, db, KeyMergeBotsOn, "maybe")

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.False(t, got.MergeBotsOn)
}

func TestSetMergeBotsStoresAndReadsBack(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.MergeBots, "a fresh install lists no bot, and ships no name")

	got, err = svc.SetMergeBots(ctx, []string{" Example-Merger[bot] ", "", "app/acme-org-automerge", "example-merger"})
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-org-automerge", "example-merger"}, got.MergeBots,
		"normalized, blanks and repeats left out, sorted")

	stored, ok, err := prefs.New(db, nil).Get(ctx, KeyMergeBots)
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `["acme-org-automerge", "example-merger"]`, stored)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got, again, "the setter returns the settings as stored")

	got, err = svc.SetMergeBots(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.MergeBots, "an empty list lists none")

	// What no build stores reads as none.
	setPref(t, db, KeyMergeBots, "acme-org-automerge")

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.MergeBots)
}

func TestSetMergeBotsRefusesWhatCantBeABot(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	_, err := svc.SetMergeBots(ctx, []string{"acme-org-automerge"})
	require.NoError(t, err)

	many := make([]string, 101)
	for i := range many {
		many[i] = "bot-" + strconv.Itoa(i)
	}

	for _, c := range []struct {
		bots []string
		want error
	}{
		{bots: []string{"not a bot"}, want: trust.ErrInvalidBot},
		{bots: many, want: trust.ErrTooManyMergeBots},
	} {
		got, setErr := svc.SetMergeBots(ctx, c.bots)
		require.ErrorIs(t, setErr, c.want)
		assert.Equal(t, Settings{}, got)
	}

	stored, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"acme-org-automerge"}, stored.MergeBots, "a refused list stores nothing")
}

func TestSetMergeBotsThatCannotWriteStoresNothing(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	_, err := db.ExecContext(ctx, refusePrefs)
	require.NoError(t, err)

	got, err := svc.SetMergeBots(ctx, []string{"acme-org-automerge"})
	require.ErrorContains(t, err, KeyMergeBots)
	assert.Equal(t, Settings{}, got)

	got, err = svc.SetMergeBotsOn(ctx, true)
	require.ErrorContains(t, err, KeyMergeBotsOn)
	assert.Equal(t, Settings{}, got)

	stored, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.False(t, stored.MergeBotsOn)
	assert.Equal(t, []string{}, stored.MergeBots)
}
