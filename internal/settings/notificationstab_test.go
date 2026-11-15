package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheNotificationsPageOpensOnTheTabLastChosen(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, NotificationsTabApp, got.NotificationsTab, "a fresh database opens on the app's own")

	got, err = svc.SetNotificationsTab(t.Context(), NotificationsTabGitHub)
	require.NoError(t, err)
	assert.Equal(t, NotificationsTabGitHub, got.NotificationsTab)

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, NotificationsTabGitHub, got.NotificationsTab, "the choice is stored")

	_, err = svc.SetNotificationsTab(t.Context(), "slack")
	require.ErrorIs(t, err, ErrInvalidNotificationsTab)

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, NotificationsTabGitHub, got.NotificationsTab, "a refused tab stores nothing")
}

func TestAnUnreadableNotificationsTabOpensOnTheAppsOwn(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyNotificationsTab, "slack")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, NotificationsTabApp, got.NotificationsTab)
}
