package settings

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheProjectsTipShowsUntilDismissed(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.False(t, got.ProjectsTipDismissed, "a fresh database shows the tip")

	got, err = svc.SetProjectsTipDismissed(t.Context(), true)
	require.NoError(t, err)
	assert.True(t, got.ProjectsTipDismissed)

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.True(t, got.ProjectsTipDismissed, "the choice is stored")

	got, err = svc.SetProjectsTipDismissed(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.ProjectsTipDismissed)
}

// An earlier build remembered that the window said why the app kept running after a close
// (ui.keptRunningTold). Nothing reads that key now: a value left stored is ignored.
func TestAStoredKeptRunningNoteIsIgnored(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, "ui.keptRunningTold", "true")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)

	data, err := json.Marshal(got)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "keptRunning", "the window gets nothing of it")
	assert.NotContains(t, settingKeys(), "ui.keptRunningTold")
}

func TestAnUnreadableProjectsTipSettingShowsTheTip(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyProjectsTipDismissed, "sometimes")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.False(t, got.ProjectsTipDismissed)
}
