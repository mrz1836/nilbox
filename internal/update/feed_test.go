package update_test

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

func TestTheURLsAreBuiltByTheApp(t *testing.T) {
	t.Parallel()

	assert.Equal(t, brand.Repository+"/releases/latest/download/manifest-darwin.json", update.ManifestURL())
	assert.Equal(t, brand.Repository+"/releases/download/v0.3.0/"+brand.Name+"_0.3.0_darwin_universal.zip",
		update.ZipURL("0.3.0", update.FileName(update.Platform, "0.3.0")))
	assert.Equal(t, brand.Repository+"/releases/tag/v0.3.0", update.ReleaseURL("0.3.0"))
}

func TestAFolderFeed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	feed := update.FolderFeed{Dir: dir}

	_, err := feed.Manifest(t.Context())
	require.ErrorIs(t, err, update.ErrNoRelease, "a folder with no manifest has no release")

	file := update.FileName(update.Platform, next)

	testutil.WriteFile(t, filepath.Join(dir, update.ManifestName), `{"version":"0.3.0"}`)
	testutil.WriteFile(t, filepath.Join(dir, file), string(zipOf(next)))

	raw, err := feed.Manifest(t.Context())
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":"0.3.0"}`, string(raw))

	var out bytes.Buffer
	require.NoError(t, feed.Download(t.Context(), next, file, &out))
	assert.Equal(t, zipOf(next), out.Bytes())

	for _, bad := range []string{"", ".", "..", "../" + file, filepath.Join("sub", file)} {
		require.ErrorIs(t, feed.Download(t.Context(), next, bad, &out), update.ErrInvalid, bad)
	}

	err = feed.Download(t.Context(), next, "missing.zip", &out)
	require.Error(t, err)
	assert.Equal(t, "The rehearsal's folder doesn't hold the update's zip.", uierr.Message(err), "worded with no path")

	_, err = update.FolderFeed{Dir: filepath.Join(dir, file)}.Manifest(t.Context())
	require.Error(t, err, "a folder that is a file can't be read")
	assert.Equal(t, "The rehearsal's folder can't be read.", uierr.Message(err))

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, feed.Download(canceled, next, file, &out), context.Canceled)
}
