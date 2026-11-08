package settings

import (
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

func TestAnUnreadableProjectsTipSettingShowsTheTip(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyProjectsTipDismissed, "sometimes")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.False(t, got.ProjectsTipDismissed)
}
