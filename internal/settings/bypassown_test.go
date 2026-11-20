package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBypassOwn(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	on, err := svc.BypassOwn(t.Context())
	require.NoError(t, err)
	assert.False(t, on, "off by default")
	assert.Equal(t, "review.bypassOwn", KeyBypassOwn)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, DefaultBypassOwn, got.BypassOwn)

	got, err = svc.SetBypassOwn(t.Context(), true)
	require.NoError(t, err)
	assert.True(t, got.BypassOwn)

	on, err = svc.BypassOwn(t.Context())
	require.NoError(t, err)
	assert.True(t, on)

	got, err = svc.SetBypassOwn(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.BypassOwn)

	setPref(t, db, KeyBypassOwn, "maybe")

	on, err = svc.BypassOwn(t.Context())
	require.NoError(t, err)
	assert.False(t, on, "a value that cannot be read is the default")

	require.NoError(t, db.Close())

	on, err = svc.BypassOwn(t.Context())
	require.Error(t, err)
	assert.Equal(t, DefaultBypassOwn, on)
}
