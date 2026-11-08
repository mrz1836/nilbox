package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTheMenuBarIsOnByDefault(t *testing.T) {
	svc, _, _ := newTestService(t)

	on, err := svc.MenuBar(t.Context())
	require.NoError(t, err)
	assert.True(t, on)
}

func TestSetMenuBarEmitsOnceStored(t *testing.T) {
	_, db, home := newTestService(t)
	events := &testutil.Events{}
	svc := serviceOver(t, db, home, WithEvents(events))

	require.NoError(t, svc.SetMenuBar(t.Context(), false))

	on, err := svc.MenuBar(t.Context())
	require.NoError(t, err)
	assert.False(t, on)

	require.NoError(t, svc.SetMenuBar(t.Context(), true))
	assert.Equal(t, []event.Event{event.MenuBarChanged{On: false}, event.MenuBarChanged{On: true}}, events.All())

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.True(t, got.MenuBar)
}

func TestAnUnreadableMenuBarSettingIsOn(t *testing.T) {
	svc, db, _ := newTestService(t)
	setPref(t, db, KeyMenuBar, "sometimes")

	on, err := svc.MenuBar(t.Context())
	require.NoError(t, err)
	assert.True(t, on)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.True(t, got.MenuBar)
}

func TestTheMenuBarSettingFailsOnAClosedDatabase(t *testing.T) {
	svc, db, _ := newTestService(t)
	events := &testutil.Events{}
	svc.events = events

	require.NoError(t, db.Close())

	on, err := svc.MenuBar(t.Context())
	require.Error(t, err)
	assert.Equal(t, DefaultMenuBar, on)

	require.Error(t, svc.SetMenuBar(t.Context(), false))
	assert.Empty(t, events.All(), "a change that was not stored is not told")
}

func TestSetNotificationBanner(t *testing.T) {
	svc, _, _ := newTestService(t)

	require.NoError(t, svc.SetNotificationBanner(t.Context(), notifyprefs.KindWoke, false))
	require.NoError(t, svc.SetNotificationBanner(t.Context(), notifyprefs.KindReviewRequested, false))

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{notifyprefs.KindReviewRequested, notifyprefs.KindWoke}, got.BannersOff)

	require.NoError(t, svc.SetNotificationBanner(t.Context(), notifyprefs.KindReviewRequested, true))

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{notifyprefs.KindWoke}, got.BannersOff)
}

func TestSetNotificationBannerRejectsAnUnknownKind(t *testing.T) {
	svc, _, _ := newTestService(t)

	err := svc.SetNotificationBanner(t.Context(), "mentions", false)
	require.ErrorIs(t, err, notifyprefs.ErrUnknownKind)
	assert.Contains(t, err.Error(), `"mentions"`)
}

func TestSetNotificationBannerFailsOnAClosedDatabase(t *testing.T) {
	svc, db, _ := newTestService(t)
	require.NoError(t, db.Close())

	require.Error(t, svc.SetNotificationBanner(t.Context(), notifyprefs.KindWoke, false))
}
