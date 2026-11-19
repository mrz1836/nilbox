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
	"github.com/mrz1836/nilbox/internal/brandgen"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestBuildUnsignedBundleFailures(t *testing.T) {
	t.Parallel()

	// bundleRig returns a rig whose checkout holds the generated brand files and current
	// icons, with the app targets' tools, and the bundle to build in it.
	bundleRig := func(t *testing.T) (*rig, bundleSpec) {
		t.Helper()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)

		return g, bundleSpec{bundle: g.path(devBundle), signer: signer{identity: adHoc}}
	}

	t.Run("no Info.plist to copy", func(t *testing.T) {
		t.Parallel()

		g, spec := bundleRig(t)
		require.NoError(t, os.Remove(g.path(brandgen.InfoPlistPath)))

		_, err := g.runner().buildUnsignedBundle(spec)

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "read the Info.plist")
	})

	t.Run("no icon set to copy", func(t *testing.T) {
		t.Parallel()

		g, spec := bundleRig(t)
		require.NoError(t, os.Remove(g.path(brandgen.IconSetPath)))

		_, err := g.runner().buildUnsignedBundle(spec)

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "read the app icon set")
	})

	t.Run("a previous bundle that can't be removed", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		g, spec := bundleRig(t)
		contents := filepath.Join(spec.bundle, "Contents")
		testutil.WriteFile(t, filepath.Join(contents, "Info.plist"), "<plist/>")
		require.NoError(t, os.Chmod(contents, 0o500))       //nolint:gosec // a folder nothing can be removed from is the fixture
		t.Cleanup(func() { _ = os.Chmod(contents, 0o700) }) //nolint:gosec // lets the test's cleanup remove it

		_, err := g.runner().buildUnsignedBundle(spec)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Contains(t, err.Error(), "remove the previous bundle")
		assert.Empty(t, g.linesOf("go"), "nothing is compiled over it")
	})
}

func TestBuildExecutableFailures(t *testing.T) {
	t.Parallel()

	meta := buildMeta{version: "v1.0.0", bundleBuild: "7"}

	t.Run("no folder for the slices", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("go", releaseGo)

		err := g.runner().buildExecutable(filepath.Join(t.TempDir(), brand.Slug), meta, releaseArches(),
			filepath.Join(t.TempDir(), "missing"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create a folder for the slices")
		assert.Empty(t, g.lines(), "nothing is compiled")
	})

	t.Run("a slice the compiler didn't write", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("go", exits(0))

		err := g.runner().buildExecutable(filepath.Join(t.TempDir(), brand.Slug), meta, releaseArches(), t.TempDir())

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "read the arm64 slice's size")
		assert.Len(t, g.linesOf("go"), 1, "it stops at the first slice")
	})
}
