//go:build mage

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

// writeUnsignedBundle writes an unsigned bundle as app:buildRelease builds it, alone in a
// fresh folder under dir, with the modes a build artifact leaves (0600 files, the binary
// included), and returns its absolute path.
func writeUnsignedBundle(t *testing.T, dir string) string {
	t.Helper()

	bundle := filepath.Join(dir, "artifact", bundleName)
	testutil.WriteFile(t, filepath.Join(bundle, "Contents", "Info.plist"), "<plist/>")
	testutil.WriteFile(t, bundleExecutable(bundle), "universal")
	testutil.WriteFile(t, filepath.Join(bundle, "Contents", "Resources", "icons.icns"), "icns")

	abs, err := filepath.Abs(bundle)
	require.NoError(t, err)

	return abs
}

// treeOf returns every path under root, relative to it, with its mode, for a test to
// compare before and after.
func treeOf(t *testing.T, root string) map[string]os.FileMode {
	t.Helper()

	tree := map[string]os.FileMode{}

	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)

		info, err := d.Info()
		require.NoError(t, err)

		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)

		tree[rel] = info.Mode()

		return nil
	}))

	return tree
}

func TestBuildReleaseBundle(t *testing.T) {
	keys := releaseKeyPair(1)
	pinned := update.EncodeKey(keys.public)

	t.Run("builds the universal bundle, unsigned, alone in its folder", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := releaseTools(t, "v"+testVersion)

		original := syscall.Umask(0o077)

		t.Cleanup(func() { syscall.Umask(original) })

		var (
			bundle string
			err    error
		)

		stdout := captureStdout(t, func() { bundle, err = buildReleaseBundle(testVersion, pinned) })

		require.NoError(t, err)
		assert.Equal(t, unsignedBundle(), bundle)
		requireLines(t, releaseBuildLines(), toolLines(t, log))
		assert.NotContains(t, readFile(t, log), "codesign", "nothing is signed")
		assert.NotContains(t, readFile(t, log), "security", "no keychain is touched")
		assert.Contains(t, stdout, "✅ Built the unsigned bundle "+bundle)

		entries, err := os.ReadDir(filepath.Dir(bundle))
		require.NoError(t, err)
		require.Len(t, entries, 1, "the slices' folder is gone")
		assert.Equal(t, bundleName, entries[0].Name())

		tree := treeOf(t, bundle)
		assert.NotContains(t, tree, filepath.Join("Contents", "_CodeSignature"))
		assert.Equal(t, os.FileMode(0o755), tree[filepath.Join("Contents", "MacOS", brand.Slug)].Perm())
		assert.Equal(t, os.FileMode(0o644), tree[filepath.Join("Contents", "Info.plist")].Perm(),
			"the modes don't depend on the umask")
		assert.NoDirExists(t, filepath.Join("bin", "release"), "nothing is packaged")

		require.NoError(t, checkUnsignedBundle(bundle), "app:signRelease takes what it builds")
	})

	tests := []struct {
		name    string
		version string
		key     string
		tag     string
		at      string // the commit the tag names, when not HEAD's
		dirty   string // what git status prints
		wantErr error
	}{
		{name: "no release key pinned", version: testVersion, tag: "v" + testVersion, wantErr: errNoReleaseKey},
		{name: "no version", key: pinned, tag: "v" + testVersion, wantErr: errBadVersion},
		{name: "a version with its v", version: "v" + testVersion, key: pinned, tag: "v" + testVersion, wantErr: errBadVersion},
		{name: "no tag on HEAD", version: testVersion, key: pinned, wantErr: errNoReleaseTag},
		{name: "the version's tag on another commit", version: testVersion, key: pinned, tag: "v" + testVersion, at: "othercommit", wantErr: errTagMismatch},
		{name: "changes in the working tree", version: testVersion, key: pinned, tag: "v" + testVersion, dirty: "?? notes.go\n", wantErr: errDirtyTree},
	}

	for _, tt := range tests {
		t.Run("refuses "+tt.name+" before anything builds", func(t *testing.T) {
			dir := inTempDir(t)
			checkout(t, dir)
			_, log := releaseTools(t, tt.tag)
			t.Setenv("FAKE_TAG_AT", tt.at)
			t.Setenv("FAKE_DIRTY", tt.dirty)

			_, err := buildReleaseBundle(tt.version, tt.key)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, toolLines(t, log))
			assert.NoDirExists(t, filepath.Join("bin", "unsigned"))
		})
	}
}

//nolint:paralleltest // changes the process's working directory, environment, PATH, and standard output
func TestSignReleaseBundle(t *testing.T) {
	keys := releaseKeyPair(1)
	out := filepath.Join(releaseRoot, testVersion)
	signed := filepath.Join(out, bundleName)

	t.Run("signs a copy of the bundle and packages the release", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := releaseTools(t, "v"+testVersion)
		keychain := keychainFile(t)
		bundle := writeUnsignedBundle(t, dir)
		before := treeOf(t, bundle)

		var err error

		stdout := captureStdout(t, func() { err = signReleaseBundle(testConfig(t, keys, keychain), bundle) })

		require.NoError(t, err)
		requireLines(t, releaseSignLines(bundle, keychain), toolLines(t, log))
		assert.NotContains(t, readFile(t, log), "go GOARCH", "nothing is built")
		assert.NotContains(t, readFile(t, log), "pnpm", "no frontend tool runs")
		assert.Equal(t, before, treeOf(t, bundle), "the unsigned bundle is left as it was")
		assert.Equal(t, originalKeychains, readFile(t, os.Getenv("KEYCHAIN_LIST")), "the search list is back as it was")

		tree := treeOf(t, signed)
		assert.Equal(t, os.FileMode(0o755), tree[filepath.Join("Contents", "MacOS", brand.Slug)].Perm(),
			"the modes the artifact lost are set before signing")
		assert.Equal(t, os.FileMode(0o644), tree[filepath.Join("Contents", "_CodeSignature", "CodeResources")].Perm())
		assert.Equal(t, "universal", readFile(t, bundleExecutable(signed)))

		require.NoError(t, verifyRelease(out, testVersion, keys.public))
		assert.Contains(t, stdout, "matches its signed manifest ("+testVersion)
	})

	t.Run("signs from the search list when no keychain is named", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := releaseTools(t, "v"+testVersion)
		bundle := writeUnsignedBundle(t, dir)

		captureStdout(t, func() { require.NoError(t, signReleaseBundle(testConfig(t, keys, ""), bundle)) })

		requireLines(t, releaseSignLines(bundle, ""), toolLines(t, log))
	})
}

// TestSignReleaseBundleRefusesBeforeAnyToolRuns: each of these refuses before any tool
// runs, the search list included.
func TestSignReleaseBundleRefusesBeforeAnyToolRuns(t *testing.T) {
	keys := releaseKeyPair(1)
	out := filepath.Join(releaseRoot, testVersion)

	early := []struct {
		name    string
		bundle  func(t *testing.T, dir string) string
		tag     string
		at      string
		dirty   string
		change  func(t *testing.T, cfg *packageConfig)
		wantErr error
	}{
		{name: "no bundle", bundle: func(*testing.T, string) string { return "" }, wantErr: errBundlePath},
		{name: "a relative bundle", bundle: func(*testing.T, string) string {
			return filepath.Join("artifact", bundleName)
		}, wantErr: errBundlePath},
		{name: "a bundle in the release's own folder", bundle: func(t *testing.T, dir string) string {
			bundle := filepath.Join(dir, filepath.FromSlash(releaseRoot), testVersion, bundleName)
			testutil.WriteFile(t, bundleExecutable(bundle), "universal")

			return bundle
		}, wantErr: errBundlePath},
		{name: "ad hoc", change: func(_ *testing.T, cfg *packageConfig) { cfg.identity = adHoc }, wantErr: errAdHoc},
		{name: "a signing key the app doesn't pin", change: func(t *testing.T, cfg *packageConfig) {
			cfg.signingKey = writeSigningKey(t, releaseKeyPair(2))
		}, wantErr: errKeyMismatch},
		{name: "no release key pinned", change: func(_ *testing.T, cfg *packageConfig) {
			cfg.releaseKey = ""
		}, wantErr: errNoReleaseKey},
		{name: "a relative keychain", change: func(_ *testing.T, cfg *packageConfig) {
			cfg.keychain = "build.keychain-db"
		}, wantErr: errKeychainPath},
		{name: "no tag on HEAD", tag: "-", wantErr: errNoReleaseTag},
		{name: "the version's tag on another commit", at: "othercommit", wantErr: errTagMismatch},
		{name: "changes in the working tree", dirty: " M main.go\n", wantErr: errDirtyTree},
	}

	for _, tt := range early {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			dir := inTempDir(t)
			checkout(t, dir)

			tag := "v" + testVersion
			if tt.tag == "-" {
				tag = ""
			}

			_, log := releaseTools(t, tag)
			t.Setenv("FAKE_TAG_AT", tt.at)
			t.Setenv("FAKE_DIRTY", tt.dirty)

			bundle := writeUnsignedBundle(t, dir)
			if tt.bundle != nil {
				bundle = tt.bundle(t, dir)
			}

			cfg := testConfig(t, keys, keychainFile(t))
			if tt.change != nil {
				tt.change(t, &cfg)
			}

			err := signReleaseBundle(cfg, bundle)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, toolLines(t, log), "nothing runs, and the search list is left alone")
			assert.NoFileExists(t, filepath.Join(out, update.ManifestName))
		})
	}
}

// TestSignReleaseBundleRefusesOtherBundles: each of these is a bundle app:buildRelease
// would never hand over; it is refused before the keychain search list is touched or
// anything is signed.
func TestSignReleaseBundleRefusesOtherBundles(t *testing.T) {
	keys := releaseKeyPair(1)
	out := filepath.Join(releaseRoot, testVersion)

	bundles := []struct {
		name    string
		make    func(t *testing.T, dir string) string
		archs   string // what lipo reads in the binary, when not both
		wantErr error
		wantMsg string
	}{
		{name: "a missing bundle", make: func(_ *testing.T, dir string) string {
			return filepath.Join(dir, "artifact", bundleName)
		}, wantErr: fs.ErrNotExist},
		{name: "a file, not a folder", make: func(t *testing.T, dir string) string {
			path := filepath.Join(dir, "artifact", bundleName)
			testutil.WriteFile(t, path, "not a bundle")

			return path
		}, wantErr: errUnsignedBundle, wantMsg: "is not a folder"},
		{name: "a link to a bundle", make: func(t *testing.T, dir string) string {
			target := writeUnsignedBundle(t, dir)
			link := filepath.Join(dir, "linked", bundleName)
			require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o750))
			require.NoError(t, os.Symlink(target, link))

			return link
		}, wantErr: errUnsignedBundle, wantMsg: "is not a folder"},
		{name: "another name", make: func(t *testing.T, dir string) string {
			bundle := writeUnsignedBundle(t, dir)
			other := filepath.Join(filepath.Dir(bundle), "Other.app")
			require.NoError(t, os.Rename(bundle, other))

			return other
		}, wantErr: errUnsignedBundle, wantMsg: "isn't named " + bundleName},
		{name: "a file beside it", make: func(t *testing.T, dir string) string {
			bundle := writeUnsignedBundle(t, dir)
			testutil.WriteFile(t, filepath.Join(filepath.Dir(bundle), "README"), "stray")

			return bundle
		}, wantErr: errUnsignedBundle, wantMsg: "alone in its folder, which also holds README"},
		{name: "a link inside it that climbs out", make: func(t *testing.T, dir string) string {
			bundle := writeUnsignedBundle(t, dir)
			require.NoError(t, os.Symlink("../../..", filepath.Join(bundle, "Contents", "Resources", "up")))

			return bundle
		}, wantErr: errUnsignedBundle, wantMsg: "Contents/Resources/up is a link"},
		{name: "a signed bundle", make: func(t *testing.T, dir string) string {
			bundle := writeUnsignedBundle(t, dir)
			testutil.WriteFile(t, filepath.Join(bundle, "Contents", "_CodeSignature", "CodeResources"), "seal")

			return bundle
		}, wantErr: errUnsignedBundle, wantMsg: "signed already"},
		{name: "no binary", make: func(t *testing.T, dir string) string {
			bundle := writeUnsignedBundle(t, dir)
			require.NoError(t, os.Remove(bundleExecutable(bundle)))

			return bundle
		}, wantErr: errUnsignedBundle, wantMsg: "lacks Contents/MacOS/" + brand.Slug},
		{name: "no Info.plist", make: func(t *testing.T, dir string) string {
			bundle := writeUnsignedBundle(t, dir)
			require.NoError(t, os.Remove(filepath.Join(bundle, "Contents", "Info.plist")))

			return bundle
		}, wantErr: errUnsignedBundle, wantMsg: "lacks Contents/Info.plist"},
		{
			name: "a binary for Apple silicon alone", make: writeUnsignedBundle, archs: "arm64",
			wantErr: errArches, wantMsg: `lipo -archs reads "arm64", not "arm64 x86_64"`,
		},
		{name: "a binary for Intel alone", make: writeUnsignedBundle, archs: "x86_64", wantErr: errArches},
	}

	for _, tt := range bundles {
		t.Run("refuses "+tt.name, func(t *testing.T) {
			dir := inTempDir(t)
			checkout(t, dir)
			_, log := releaseTools(t, "v"+testVersion)

			if tt.archs != "" {
				t.Setenv("LIPO_ARCHS", tt.archs)
			}

			bundle := tt.make(t, dir)

			err := signReleaseBundle(testConfig(t, keys, keychainFile(t)), bundle)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.wantMsg)

			for _, line := range toolLines(t, log) {
				assert.Regexp(t, "^lipo -archs ", line, "only the bundle's architectures are read")
			}

			assert.NoDirExists(t, out, "nothing is packaged")
		})
	}
}

//nolint:paralleltest // swaps the pinned key and changes the process's working directory, environment, PATH, and standard output
func TestAppReleaseHalvesOnMacOS(t *testing.T) {
	onlyOnMacOS(t)

	keys := releaseKeyPair(1)

	t.Run("app:buildRelease, then app:signRelease of what it built", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		releaseTools(t, "v"+testVersion)
		pinKey(t, update.EncodeKey(keys.public))
		t.Setenv(envReleaseVersion, testVersion)

		captureStdout(t, func() { require.NoError(t, App{}.BuildRelease()) })

		bundle, err := filepath.Abs(unsignedBundle())
		require.NoError(t, err)

		t.Setenv(envCodesignIdentity, testIdentity)
		t.Setenv(envCodesignKeychain, keychainFile(t))
		t.Setenv(envUpdateSigningKey, writeSigningKey(t, keys))
		t.Setenv(mageArgsJSON, `["bundle=`+bundle+`"]`)

		captureStdout(t, func() { require.NoError(t, App{}.SignRelease()) })

		require.NoError(t, verifyRelease(filepath.Join(releaseRoot, testVersion), testVersion, keys.public))
	})

	t.Run("app:buildRelease refuses while no key is pinned", func(t *testing.T) {
		pinKey(t, "")
		t.Setenv(envReleaseVersion, testVersion)

		require.ErrorIs(t, App{}.BuildRelease(), errNoReleaseKey)
	})

	t.Run("app:signRelease needs its bundle", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		releaseTools(t, "v"+testVersion)
		pinKey(t, update.EncodeKey(keys.public))
		t.Setenv(envReleaseVersion, testVersion)
		t.Setenv(envCodesignIdentity, testIdentity)
		t.Setenv(envUpdateSigningKey, writeSigningKey(t, keys))
		t.Setenv(mageArgsJSON, `[]`)

		require.ErrorIs(t, App{}.SignRelease(), errBundlePath)
	})

	t.Run("app:signRelease names the environment it lacks", func(t *testing.T) {
		pinKey(t, update.EncodeKey(keys.public))

		for _, key := range []string{envReleaseVersion, envCodesignIdentity, envCodesignKeychain, envUpdateSigningKey} {
			unsetenv(t, key)
		}

		require.ErrorIs(t, App{}.SignRelease(), errMissingEnv)
	})
}
