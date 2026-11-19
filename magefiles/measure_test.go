//go:build mage

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/measure"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errWrite stands in for standard output refusing a write.
var errWrite = errors.New("the output is closed")

// failingWriter is an output that refuses every write.
type failingWriter struct{}

// Write refuses p.
func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

// fakeBundle writes the bundle built from source into the rig's checkout, its executable
// a file of its own, and returns the executable's path. A test that runs it makes a fake
// tool named brand.Slug, the executable's name, which plays it.
func fakeBundle(t *testing.T, g *rig) string {
	t.Helper()

	exe := bundleExecutable(g.path(devBundle))
	testutil.WriteFile(t, exe, "binary")

	return exe
}

func TestPrintLines(t *testing.T) {
	t.Parallel()

	t.Run("prints each line", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)

		require.NoError(t, g.runner().printLines([]string{"size.binary_bytes=1", "size.bundle_bytes=2"}))

		assert.Equal(t, "size.binary_bytes=1\nsize.bundle_bytes=2\n", g.out.String())
	})

	t.Run("reports a failed write", func(t *testing.T) {
		t.Parallel()

		r := newRig(t).runner()
		r.stdout = failingWriter{}

		err := r.printLines([]string{"a"})

		require.ErrorIs(t, err, errWrite)
		assert.Contains(t, err.Error(), "print the report")
	})
}

func TestSay(t *testing.T) {
	t.Parallel()

	g := newRig(t)

	g.runner().say("hello")

	assert.Equal(t, "hello\n", g.out.String())
}

func TestMeasure(t *testing.T) {
	t.Parallel()

	t.Run("size reports the binary and the bundle", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		exe := fakeBundle(t, g)
		testutil.WriteFile(t, filepath.Join(filepath.Dir(filepath.Dir(exe)), "Info.plist"), strings.Repeat("x", 100))

		info, err := os.Stat(exe)
		require.NoError(t, err)

		require.NoError(t, g.runner().measureSize())

		assert.Equal(t, strings.Join(measure.SizeReport{
			BinaryBytes: info.Size(),
			BundleBytes: info.Size() + 100,
		}.Lines(), "\n")+"\n", g.out.String())
	})

	t.Run("refresh runs the app's measurement mode", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		exe := fakeBundle(t, g)
		g.tool(brand.Slug, exits(0))

		require.NoError(t, g.runner().measureRefresh())

		assert.Equal(t, []toolCall{{name: exe, args: []string{measure.RefreshFlag}}}, g.toolCalls(), "the bundle's own binary runs")
	})

	t.Run("refresh reads routine searches when asked", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeBundle(t, g)
		g.tool(brand.Slug, exits(0))
		g.setenv(mageArgsJSON, `["searches=5"]`)

		require.NoError(t, g.runner().measureRefresh())

		assert.Equal(t, []string{measure.RefreshFlag + " " + measure.SearchesFlag + "5"}, g.linesOf(brand.Slug))
	})

	t.Run("every measurement needs a built bundle", func(t *testing.T) {
		t.Parallel()

		r := newRig(t).runner()

		require.ErrorIs(t, r.measureSize(), errNoBundle)
		require.ErrorIs(t, r.measureRefresh(), errNoBundle)
		require.ErrorIs(t, r.measureStartup(t.Context(), func(context.Context, string) (measure.Startup, error) {
			t.Error("nothing to launch")

			return measure.Startup{}, nil
		}), errNoBundle)
	})
}

func TestMageArg(t *testing.T) {
	t.Parallel()

	g := newRig(t)
	r := g.runner()

	g.setenv(mageArgsJSON, `["agent=codex","verbose","other=a=b"]`)
	assert.Equal(t, "codex", r.mageArg("agent"))
	assert.Equal(t, "a=b", r.mageArg("other"))
	assert.Empty(t, r.mageArg("missing"))

	g.setenv(mageArgsJSON, "not json")
	assert.Empty(t, r.mageArg("agent"))
}
