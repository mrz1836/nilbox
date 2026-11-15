package appdata_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestARequestToResetIsReadBack(t *testing.T) {
	t.Parallel()

	path := appdata.NewPaths(filepath.Join(t.TempDir(), "new-data")).ResetRequest
	at := time.Date(2026, time.November, 15, 9, 30, 0, 0, time.FixedZone("EST", -5*60*60))

	require.NoError(t, appdata.RequestReset(path, appdata.ResetByMagex, at), "the data folder is made when missing")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateFilePerm, info.Mode().Perm(), "the request is the user's alone")

	got, found, err := appdata.ReadResetRequest(path)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, appdata.ResetRequest{
		Version: appdata.ResetRequestVersion, RequestedAt: at.UTC(), By: appdata.ResetByMagex,
	}, got)

	require.NoError(t, appdata.ClearResetRequest(path))
	assert.NoFileExists(t, path)

	_, found, err = appdata.ReadResetRequest(path)
	require.NoError(t, err)
	assert.False(t, found, "a cleared request is no request")
	require.NoError(t, appdata.ClearResetRequest(path), "clearing twice is fine")
}

func TestARequestToResetReplacesTheLastOne(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "reset-request.json")
	first := time.Date(2026, time.November, 14, 0, 0, 0, 0, time.UTC)

	require.NoError(t, appdata.RequestReset(path, appdata.ResetByMagex, first))
	require.NoError(t, appdata.RequestReset(path, appdata.ResetBySettings, first.Add(time.Hour)))

	got, found, err := appdata.ReadResetRequest(path)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, appdata.ResetBySettings, got.By)
	assert.Equal(t, first.Add(time.Hour), got.RequestedAt)
}

func TestAResetRequestThatCannotBeReadHoldsNone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	tests := []struct {
		name string
		body string
	}{
		{name: "not JSON", body: "reset please"},
		{name: "no version", body: `{"by":"settings"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(dir, tt.name+".json")
			testutil.WriteFile(t, path, tt.body)

			_, found, err := appdata.ReadResetRequest(path)
			require.Error(t, err)
			assert.False(t, found)
		})
	}

	_, found, err := appdata.ReadResetRequest(dir)
	require.ErrorIs(t, err, appdata.ErrNotAFile)
	assert.False(t, found)
}
