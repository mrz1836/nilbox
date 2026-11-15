//go:build desktop

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
)

// testHost is the runtime updater's host in a test: it opens no window and quits nothing.
type testHost struct{}

func (testHost) Emit(string, ...any) bool { return true }

func (testHost) OnEvent(string, func(any)) func() { return func() {} }

func (testHost) OpenWindow(updater.WindowOptions) updater.WindowHandle { return nil }

func (testHost) Quit() {}

// bundleZip writes a zip of a small app bundle into dir, named as 0.3.0's release's, and
// returns its path, size, and SHA-512 in hex.
func bundleZip(t *testing.T, dir string) (string, int64, string) {
	t.Helper()

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	for name, body := range map[string]string{
		brand.Name + ".app/Contents/Info.plist":          "<plist/>",
		brand.Name + ".app/Contents/MacOS/" + brand.Slug: "binary",
	} {
		f, err := w.Create(name)
		require.NoError(t, err)

		_, err = f.Write([]byte(body))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())

	path := filepath.Join(dir, brand.Name+"_0.3.0_darwin_universal.zip")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))

	sum := sha512.Sum512(buf.Bytes())

	return path, int64(buf.Len()), hex.EncodeToString(sum[:])
}

// artifact is the update in the zip at path, as the installer hands it over.
func artifact(t *testing.T, path, version string, size int64, sum string) desktop.Artifact {
	t.Helper()

	a, err := desktop.NewArtifact(path, version, size, sum)
	require.NoError(t, err)

	return a
}

func TestTheCacheProviderOffersTheZipWithItsDigest(t *testing.T) {
	t.Parallel()

	path, size, sum := bundleZip(t, t.TempDir())
	p := &cacheProvider{}
	req := updater.CheckRequest{CurrentVersion: "0.2.0", Platform: "darwin", Arch: "arm64"}

	rel, err := p.Check(t.Context(), req)
	require.NoError(t, err)
	assert.Nil(t, rel, "nothing offered is up to date")

	a := artifact(t, path, "0.3.0", size, sum)
	p.offer(a)

	rel, err = p.Check(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, rel)
	assert.Equal(t, "0.3.0", rel.Version)
	assert.Equal(t, updater.Artifact{
		Filename: brand.Name + "_0.3.0_darwin_universal.zip", Filetype: "zip", Size: size, Platform: "darwin", Arch: "arm64",
	}, rel.Artifact, "named .zip, so the updater extracts it")
	require.NotNil(t, rel.Verification)
	assert.Equal(t, "sha512", rel.Verification.DigestAlgo)
	assert.Equal(t, a.Digest, rel.Verification.Digest)
	assert.Len(t, rel.Verification.Digest, sha512.Size, "never an empty digest, which would install unchecked bytes")
	assert.Empty(t, rel.Verification.Signature, "the app verified the signature; the updater checks the digest")

	var (
		got      bytes.Buffer
		progress [][2]int64
	)

	require.NoError(t, p.Download(t.Context(), rel, &got, func(written, total int64) {
		progress = append(progress, [2]int64{written, total})
	}))

	body, err := os.ReadFile(path) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, body, got.Bytes())
	require.NotEmpty(t, progress)
	assert.Equal(t, [2]int64{size, size}, progress[len(progress)-1])

	other := *rel
	other.Version = "0.4.0"
	require.ErrorIs(t, p.Download(t.Context(), &other, &got, func(int64, int64) {}), errOtherRelease)
}

func TestTheInstallerStagesTheZipBesideTheApp(t *testing.T) {
	t.Parallel()

	u := updater.New(testHost{})
	i, err := newInstaller(u, "0.2.0")
	require.NoError(t, err)

	path, size, sum := bundleZip(t, t.TempDir())
	require.NoError(t, i.Install(t.Context(), path, "0.3.0", size, sum))

	staged := u.DownloadedPath()
	require.NotEmpty(t, staged)

	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(staged)) })

	assert.Equal(t, brand.Name+".app", filepath.Base(staged), "extracted to the bundle")
	assert.Regexp(t, `^wails-update-`, filepath.Base(filepath.Dir(staged)), "in the runtime's staging folder")
	assert.FileExists(t, filepath.Join(staged, "Contents", "MacOS", brand.Slug))
	assert.Equal(t, updater.StateReady, u.State())
}

func TestTheInstallerRefusesAZipThatDoesntMatchItsDigest(t *testing.T) {
	t.Parallel()

	u := updater.New(testHost{})
	i, err := newInstaller(u, "0.2.0")
	require.NoError(t, err)

	path, size, _ := bundleZip(t, t.TempDir())
	other := sha512.Sum512([]byte("another zip"))

	err = i.Install(t.Context(), path, "0.3.0", size, hex.EncodeToString(other[:]))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stage the update beside the app")
	assert.Empty(t, u.DownloadedPath(), "nothing staged")
}

func TestTheInstallerHandsOverNothingWithoutADigest(t *testing.T) {
	t.Parallel()

	u := updater.New(testHost{})
	i, err := newInstaller(u, "0.2.0")
	require.NoError(t, err)

	path, size, _ := bundleZip(t, t.TempDir())

	require.ErrorIs(t, i.Install(t.Context(), path, "0.3.0", size, ""), desktop.ErrNotInstallable)
	assert.Nil(t, i.cache.current(), "nothing offered")
	assert.Empty(t, u.DownloadedPath())
}
