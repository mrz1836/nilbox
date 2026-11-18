package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
)

func TestSetCardAvatarsStoresAndReadsBack(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.True(t, got.CardAvatars, "the cards show the authors' pictures until turned off")

	got, err = svc.SetCardAvatars(ctx, false)
	require.NoError(t, err)
	assert.False(t, got.CardAvatars)

	stored, ok, err := prefs.New(db, nil).Get(ctx, KeyCardAvatars)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "false", stored)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got, again, "the setter returns the settings as stored")

	got, err = svc.SetCardAvatars(ctx, true)
	require.NoError(t, err)
	assert.True(t, got.CardAvatars)

	// A value no build stores reads as the default.
	setPref(t, db, KeyCardAvatars, "sometimes")

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, DefaultCardAvatars, got.CardAvatars)
}

func TestSetCardAvatarsThatCannotWriteStoresNothing(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	_, err := db.ExecContext(ctx, refusePrefs)
	require.NoError(t, err)

	got, err := svc.SetCardAvatars(ctx, false)
	require.ErrorContains(t, err, KeyCardAvatars)
	assert.Equal(t, Settings{}, got)

	stored, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.True(t, stored.CardAvatars)
}

func TestSetCardDensityStoresAndReadsBack(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, CardComfortable, got.CardDensity, "a fresh database shows comfortable cards")

	got, err = svc.SetCardDensity(ctx, CardCompact)
	require.NoError(t, err)
	assert.Equal(t, CardCompact, got.CardDensity)

	stored, ok, err := prefs.New(db, nil).Get(ctx, KeyCardDensity)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "compact", stored)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got, again, "the setter returns the settings as stored")
}

func TestSetCardDensityRefusesAnUnknownDensityAndStoresNothing(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	_, err := svc.SetCardDensity(ctx, CardCompact)
	require.NoError(t, err)

	got, err := svc.SetCardDensity(ctx, "dense")
	require.ErrorIs(t, err, ErrInvalidCardDensity)
	require.ErrorContains(t, err, `unknown card density "dense": choose one of comfortable, compact`)
	assert.Equal(t, Settings{}, got)

	stored, ok, err := prefs.New(db, nil).Get(ctx, KeyCardDensity)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "compact", stored, "a refused density stores nothing")
}

func TestAnUnknownStoredCardDensityReadsAsComfortable(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyCardDensity, "dense")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, CardComfortable, got.CardDensity)
}

func TestSetCardUnreadStoresAndReadsBack(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.True(t, got.CardUnread, "the cards show the unread dots until turned off")

	got, err = svc.SetCardUnread(ctx, false)
	require.NoError(t, err)
	assert.False(t, got.CardUnread)

	stored, ok, err := prefs.New(db, nil).Get(ctx, KeyCardUnread)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "false", stored)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got, again, "the setter returns the settings as stored")

	got, err = svc.SetCardUnread(ctx, true)
	require.NoError(t, err)
	assert.True(t, got.CardUnread)

	// A value no build stores reads as the default.
	setPref(t, db, KeyCardUnread, "sometimes")

	got, err = svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, DefaultCardUnread, got.CardUnread)
}

func TestSetCardUnreadThatCannotWriteStoresNothing(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	_, err := db.ExecContext(ctx, refusePrefs)
	require.NoError(t, err)

	got, err := svc.SetCardUnread(ctx, false)
	require.ErrorContains(t, err, KeyCardUnread)
	assert.Equal(t, Settings{}, got)

	stored, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.True(t, stored.CardUnread)
}
