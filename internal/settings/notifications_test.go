package settings

import (
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/notifyprefs"
)

// menuBarLog records the menu bar settings a MenuBarNotifier was told about, in order.
type menuBarLog struct {
	mu  sync.Mutex
	got []bool
}

func (l *menuBarLog) MenuBarChanged(on bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.got = append(l.got, on)
}

func (l *menuBarLog) changes() []bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.got)
}

func TestTheMenuBarIsOnByDefault(t *testing.T) {
	svc, _, _ := newTestService(t)

	on, err := svc.MenuBar(t.Context())
	require.NoError(t, err)
	assert.True(t, on)
}

func TestSetMenuBarTellsTheNotifier(t *testing.T) {
	_, db, home := newTestService(t)
	log := &menuBarLog{}
	svc := serviceOver(t, db, home, WithMenuBarNotifier(log))

	require.NoError(t, svc.SetMenuBar(t.Context(), false))

	on, err := svc.MenuBar(t.Context())
	require.NoError(t, err)
	assert.False(t, on)

	require.NoError(t, svc.SetMenuBar(t.Context(), true))
	assert.Equal(t, []bool{false, true}, log.changes())

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
	log := &menuBarLog{}
	svc.menuBar = log

	require.NoError(t, db.Close())

	on, err := svc.MenuBar(t.Context())
	require.Error(t, err)
	assert.Equal(t, DefaultMenuBar, on)

	require.Error(t, svc.SetMenuBar(t.Context(), false))
	assert.Empty(t, log.changes(), "a change that was not stored is not told")
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
