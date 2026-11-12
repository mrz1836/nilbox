package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestSetNotificationBanner(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	require.NoError(t, testutil.ErrOf(svc.SetNotificationBanner(t.Context(), notifyprefs.KindWoke, false)))
	require.NoError(t, testutil.ErrOf(svc.SetNotificationBanner(t.Context(), notifyprefs.KindReviewRequested, false)))

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []notifyprefs.Kind{
		notifyprefs.KindReviewRequested, notifyprefs.KindWoke, notifyprefs.KindSessionNeedsYou,
		notifyprefs.KindSessionFinished,
	}, got.BannersOff, "the sessions' kinds start off")

	require.NoError(t, testutil.ErrOf(svc.SetNotificationBanner(t.Context(), notifyprefs.KindReviewRequested, true)))
	require.NoError(t, testutil.ErrOf(svc.SetNotificationBanner(t.Context(), notifyprefs.KindSessionFinished, true)))

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []notifyprefs.Kind{notifyprefs.KindWoke, notifyprefs.KindSessionNeedsYou}, got.BannersOff)

	stored, err := prefs.New(svc.pool, nil).GetMany(t.Context(), notifyprefs.KeyBannersOff, notifyprefs.KeyBannersOn)
	require.NoError(t, err)
	assert.JSONEq(t, `["woke"]`, stored[notifyprefs.KeyBannersOff])
	assert.JSONEq(t, `["session_finished"]`, stored[notifyprefs.KeyBannersOn], "a kind that starts off is stored once on")
}

func TestCodexSettingsStartOff(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.False(t, got.CodexEffort, "Codex keeps its own effort until the user says otherwise")
	assert.False(t, got.CodexFast)

	setPref(t, db, KeyCodexEffort, "yes please")
	setPref(t, db, KeyCodexFast, "true")

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.False(t, got.CodexEffort, "a value that cannot be read is off")
	assert.True(t, got.CodexFast)
}

func TestFollowSessions(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	on, err := svc.FollowSessions(t.Context())
	require.NoError(t, err)
	assert.True(t, on, "on by default")

	require.NoError(t, testutil.ErrOf(svc.SetFollowSessions(t.Context(), false)))

	on, err = svc.FollowSessions(t.Context())
	require.NoError(t, err)
	assert.False(t, on)

	setPref(t, db, KeyFollowSessions, "maybe")

	on, err = svc.FollowSessions(t.Context())
	require.NoError(t, err)
	assert.True(t, on, "a value that cannot be read is the default")

	require.NoError(t, db.Close())

	on, err = svc.FollowSessions(t.Context())
	require.Error(t, err)
	assert.Equal(t, DefaultFollowSessions, on)
}

func TestSetFollowSessionsTellsTheShell(t *testing.T) {
	t.Parallel()

	_, db, home := newTestService(t)
	events := &testutil.Events{}
	svc := serviceOver(t, db, home, WithEvents(events))

	require.NoError(t, testutil.ErrOf(svc.SetFollowSessions(t.Context(), false)))
	require.NoError(t, testutil.ErrOf(svc.SetFollowSessions(t.Context(), true)))
	assert.Equal(t, []event.Event{event.SessionsUpdated{}, event.SessionsUpdated{}}, events.All(),
		"the window and the menu bar show the sessions, or stop, at once")

	require.NoError(t, db.Close())

	_, err := svc.SetFollowSessions(t.Context(), false)
	require.Error(t, err)
	assert.Len(t, events.All(), 2, "nothing is said of a switch that was not stored")
}

func TestSetNotificationBannerRejectsAnUnknownKind(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	_, err := svc.SetNotificationBanner(t.Context(), "mentions", false)
	require.ErrorIs(t, err, notifyprefs.ErrUnknownKind)
	assert.Contains(t, err.Error(), `"mentions"`)
}

func TestSetNotificationBannerFailsOnAClosedDatabase(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	require.NoError(t, db.Close())

	require.Error(t, testutil.ErrOf(svc.SetNotificationBanner(t.Context(), notifyprefs.KindWoke, false)))
}
