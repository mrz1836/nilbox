//go:build mage

package main

import (
	"io/fs"
	"os"
	"path/filepath"
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
	t.Parallel()

	keys := releaseKeyPair(1)
	pinned := update.EncodeKey(keys.public)

	t.Run("builds the universal bundle, unsigned, alone in its folder", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})

		bundle, err := g.runner().buildReleaseBundle(testVersion, pinned)

		require.NoError(t, err)
		assert.Equal(t, unsignedBundle(g.dir), bundle)
		requireLines(t, releaseBuildLines(g.dir), toolLines(g))
		assert.Empty(t, g.linesOf("codesign"), "nothing is signed")
		assert.Empty(t, g.linesOf("security"), "no keychain is touched")
		assert.Contains(t, g.out.String(), "✅ Built the unsigned bundle "+bundle)

		entries, err := os.ReadDir(filepath.Dir(bundle))
		require.NoError(t, err)
		require.Len(t, entries, 1, "the slices' folder is gone")
		assert.Equal(t, bundleName, entries[0].Name())

		tree := treeOf(t, bundle)
		assert.NotContains(t, tree, filepath.Join("Contents", "_CodeSignature"))
		assert.Equal(t, os.FileMode(0o755), tree[filepath.Join("Contents", "MacOS", brand.Slug)].Perm())
		assert.Equal(t, os.FileMode(0o644), tree[filepath.Join("Contents", "Info.plist")].Perm())
		assert.NoDirExists(t, g.path("bin/release"), "nothing is packaged")

		require.NoError(t, g.runner().checkUnsignedBundle(bundle), "app:signRelease takes what it builds")
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
			t.Parallel()

			g, _ := releaseRig(t, releaseFake{tag: tt.tag, at: tt.at, dirty: tt.dirty})

			_, err := g.runner().buildReleaseBundle(tt.version, tt.key)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, toolLines(g))
			assert.NoDirExists(t, g.path("bin/unsigned"))
		})
	}
}

func TestSignReleaseBundle(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)

	t.Run("signs a copy of the bundle and packages the release", func(t *testing.T) {
		t.Parallel()

		g, state := releaseRig(t, releaseFake{tag: "v" + testVersion})
		keychain := keychainFile(t)
		bundle := writeUnsignedBundle(t, t.TempDir())
		before := treeOf(t, bundle)
		out := filepath.Join(g.path(releaseRoot), testVersion)
		signed := filepath.Join(out, bundleName)

		require.NoError(t, g.runner().signReleaseBundle(testConfig(t, keys, keychain), bundle))

		requireLines(t, releaseSignLines(g.dir, bundle, keychain), toolLines(g))
		assert.Empty(t, g.linesOf("go"), "nothing is built")
		assert.Empty(t, g.linesOf("pnpm"), "no frontend tool runs")
		assert.Equal(t, before, treeOf(t, bundle), "the unsigned bundle is left as it was")
		assert.Equal(t, originalKeychains, state.keychains.printed(), "the search list is back as it was")

		tree := treeOf(t, signed)
		assert.Equal(t, os.FileMode(0o755), tree[filepath.Join("Contents", "MacOS", brand.Slug)].Perm(),
			"the modes the artifact lost are set before signing")
		assert.Equal(t, os.FileMode(0o644), tree[filepath.Join("Contents", "_CodeSignature", "CodeResources")].Perm())
		assert.Equal(t, "universal", readFile(t, bundleExecutable(signed)))

		require.NoError(t, g.runner().verifyRelease(out, testVersion, keys.public))
		assert.Contains(t, g.out.String(), "matches its signed manifest ("+testVersion)
	})

	t.Run("signs from the search list when no keychain is named", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		bundle := writeUnsignedBundle(t, t.TempDir())

		require.NoError(t, g.runner().signReleaseBundle(testConfig(t, keys, ""), bundle))

		requireLines(t, releaseSignLines(g.dir, bundle, ""), toolLines(g))
	})
}

// TestSignReleaseBundleRefusesBeforeAnyToolRuns: each of these refuses before any tool
// runs, the search list included.
func TestSignReleaseBundleRefusesBeforeAnyToolRuns(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)

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
			t.Parallel()

			tag := "v" + testVersion
			if tt.tag == "-" {
				tag = ""
			}

			g, _ := releaseRig(t, releaseFake{tag: tag, at: tt.at, dirty: tt.dirty})

			bundle := writeUnsignedBundle(t, t.TempDir())
			if tt.bundle != nil {
				bundle = tt.bundle(t, g.dir)
			}

			cfg := testConfig(t, keys, keychainFile(t))
			if tt.change != nil {
				tt.change(t, &cfg)
			}

			err := g.runner().signReleaseBundle(cfg, bundle)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, toolLines(g), "nothing runs, and the search list is left alone")
			assert.NoFileExists(t, filepath.Join(g.path(releaseRoot), testVersion, update.ManifestName))
		})
	}
}

// TestSignReleaseBundleRefusesOtherBundles: each of these is a bundle app:buildRelease
// would never hand over; it is refused before the keychain search list is touched or
// anything is signed.
func TestSignReleaseBundleRefusesOtherBundles(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)

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
			t.Parallel()

			g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion, archs: tt.archs})

			bundle := tt.make(t, t.TempDir())

			err := g.runner().signReleaseBundle(testConfig(t, keys, keychainFile(t)), bundle)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.wantMsg)

			for _, line := range toolLines(g) {
				assert.Regexp(t, "^lipo -archs ", line, "only the bundle's architectures are read")
			}

			assert.NoDirExists(t, filepath.Join(g.path(releaseRoot), testVersion), "nothing is packaged")
		})
	}
}

func TestAppReleaseHalves(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)
	pinned := update.EncodeKey(keys.public)

	t.Run("app:buildRelease, then app:signRelease of what it built", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		g.setenv(envReleaseVersion, testVersion)

		require.NoError(t, g.runner().buildReleaseFromEnv(pinned))

		g.setenv(envCodesignIdentity, testIdentity)
		g.setenv(envCodesignKeychain, keychainFile(t))
		g.setenv(envUpdateSigningKey, writeSigningKey(t, keys))
		g.setenv(mageArgsJSON, `["bundle=`+unsignedBundle(g.dir)+`"]`)

		require.NoError(t, g.runner().signReleaseFromEnv(pinned))

		require.NoError(t, g.runner().verifyRelease(filepath.Join(g.path(releaseRoot), testVersion), testVersion, keys.public))
	})

	t.Run("app:buildRelease refuses while no key is pinned", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.setenv(envReleaseVersion, testVersion)

		require.ErrorIs(t, g.runner().buildReleaseFromEnv(""), errNoReleaseKey)
	})

	t.Run("app:signRelease needs its bundle", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		g.setenv(envReleaseVersion, testVersion)
		g.setenv(envCodesignIdentity, testIdentity)
		g.setenv(envUpdateSigningKey, writeSigningKey(t, keys))
		g.setenv(mageArgsJSON, `[]`)

		require.ErrorIs(t, g.runner().signReleaseFromEnv(pinned), errBundlePath)
	})

	t.Run("app:signRelease names the environment it lacks", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, newRig(t).runner().signReleaseFromEnv(pinned), errMissingEnv)
	})
}
