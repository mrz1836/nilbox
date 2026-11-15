package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

func TestUpdateModeDefaultsToDownload(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, updateprefs.ModeDownload, got.UpdateMode)
}

func TestSetUpdateModeStoresEachChoiceAndTellsTheUpdateCheck(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	for i, mode := range []updateprefs.Mode{updateprefs.ModeNotify, updateprefs.ModeOff, updateprefs.ModeDownload} {
		got, err := svc.SetUpdateMode(t.Context(), mode)
		require.NoError(t, err)
		assert.Equal(t, mode, got.UpdateMode)

		again, err := svc.Get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, mode, again.UpdateMode, "the choice is stored")
		assert.Equal(t, i+1, events.Count(), "each choice tells the update check once")
	}

	assert.Equal(t, []event.Event{event.UpdatesChanged{}, event.UpdatesChanged{}, event.UpdatesChanged{}}, events.All())
}

func TestSetUpdateModeRefusesAnUnknownModeAndStoresNothing(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	_, err := svc.SetUpdateMode(t.Context(), updateprefs.ModeNotify)
	require.NoError(t, err)

	for _, mode := range []updateprefs.Mode{"", "auto", "Download"} {
		_, err = svc.SetUpdateMode(t.Context(), mode)
		require.ErrorIs(t, err, updateprefs.ErrUnknownMode, mode)
		assert.Equal(t, "That isn't one of the choices for new versions.", uierr.Message(err))
	}

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, updateprefs.ModeNotify, got.UpdateMode, "a refused choice stores nothing")
	assert.Equal(t, 1, events.Count(), "a refused choice tells nothing")
}

func TestAStoredUpdateModeThatIsNoLongerAChoiceReadsAsTheDefault(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	require.NoError(t, svc.prefs.Set(t.Context(), updateprefs.KeyMode, "weekly"))

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, updateprefs.DefaultMode, got.UpdateMode)
}
