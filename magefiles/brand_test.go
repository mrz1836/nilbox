//go:build mage

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// writesOutput is the answer of a fake rsvg-convert or iconutil: it writes the file named
// after --output, as both do.
func writesOutput(args []string) testutil.FakeRun {
	return writes(argAfter(args, "--output"), "")
}

// failsFor is an answer that fails a call whose arguments hold part, and otherwise writes
// the file named after --output.
func failsFor(part string) answer {
	return func(args []string) testutil.FakeRun {
		if strings.Contains(strings.Join(args, " "), part) {
			return testutil.FakeRun{Exit: 1}
		}

		return writesOutput(args)
	}
}

// artwork returns a rig whose checkout holds the brand files, the icon artwork among them,
// and the folder the development icons go to.
func artwork(t *testing.T) *rig {
	t.Helper()

	g := newRig(t)
	require.NoError(t, brandgen.Write(g.dir))
	require.NoError(t, os.MkdirAll(g.path("cmd/"+brand.Slug), 0o750))

	return g
}

func TestBrandGenerate(t *testing.T) {
	t.Parallel()

	g := newRig(t)

	require.NoError(t, g.runner().generateBrand())

	require.NoError(t, brandgen.Check(g.dir), "every generated file is written and current")
}

func TestRenderSVG(t *testing.T) {
	t.Parallel()

	t.Run("renders a square PNG of the given side", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool(svgRenderer, writesOutput)

		out := filepath.Join(t.TempDir(), "tray.png")

		require.NoError(t, g.runner().renderSVG(brandgen.TraySVGPath, brandgen.TrayIconSize, out))

		assert.FileExists(t, out)
		assert.Equal(t, []string{
			"--width 66 --height 66 --output " + out + " " + g.path(brandgen.TraySVGPath),
		}, g.linesOf(svgRenderer))
	})

	t.Run("names the render that failed", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool(svgRenderer, exits(1))

		err := g.runner().renderSVG(brandgen.TraySVGPath, brandgen.TrayIconSize, filepath.Join(t.TempDir(), "tray.png"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render the 66-pixel trayicon.svg")
	})
}

func TestRenderIconSet(t *testing.T) {
	t.Parallel()

	t.Run("renders every size at 1x and 2x under the names iconutil expects", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool(svgRenderer, writesOutput)

		dir := filepath.Join(t.TempDir(), brandgen.IconFile+".iconset")

		require.NoError(t, g.runner().renderIconSet(dir))

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)

		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}

		assert.ElementsMatch(t, []string{
			"icon_16x16.png", "icon_16x16@2x.png",
			"icon_32x32.png", "icon_32x32@2x.png",
			"icon_128x128.png", "icon_128x128@2x.png",
			"icon_256x256.png", "icon_256x256@2x.png",
			"icon_512x512.png", "icon_512x512@2x.png",
		}, names)
		assert.Contains(t, g.linesOf(svgRenderer),
			"--width 1024 --height 1024 --output "+filepath.Join(dir, "icon_512x512@2x.png")+" "+g.path(brandgen.IconSVGPath))
	})

	t.Run("the icon set already exists", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().renderIconSet(t.TempDir())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create the icon set")
	})

	t.Run("a 1x render fails", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool(svgRenderer, exits(1))

		err := g.runner().renderIconSet(filepath.Join(t.TempDir(), "icons.iconset"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render the 16-pixel")
	})

	t.Run("a 2x render fails", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool(svgRenderer, failsFor("@2x"))

		err := g.runner().renderIconSet(filepath.Join(t.TempDir(), "icons.iconset"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render the 32-pixel")
	})
}

func TestBrandIcons(t *testing.T) {
	t.Parallel()

	t.Run("needs rsvg-convert", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().brandIcons(func() { t.Error("the brand files are generated without a renderer") })

		require.ErrorIs(t, err, errNoSVGRenderer)
	})

	t.Run("renders every icon and records the artwork", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		require.NoError(t, os.MkdirAll(g.path("cmd/"+brand.Slug), 0o750))
		g.tool(svgRenderer, writesOutput)
		g.tool("iconutil", writesOutput)

		r := g.runner()

		require.NoError(t, r.brandIcons(func() { require.NoError(t, r.generateBrand()) }))

		assert.Contains(t, g.out.String(), "Rendered "+brandgen.IconSetPath)
		require.NoError(t, r.checkBrandFiles(), "the brand files and every icon are current")
	})
}

func TestRenderIcons(t *testing.T) {
	t.Parallel()

	t.Run("renders every icon and records the artwork", func(t *testing.T) {
		t.Parallel()

		g := artwork(t)
		g.tool(svgRenderer, writesOutput)
		g.tool("iconutil", writesOutput)

		require.NoError(t, g.runner().renderIcons())

		assert.Equal(t, "✅ Rendered "+brandgen.IconSetPath+", "+brandgen.DevIconPath+", and "+brandgen.TrayIconPath+"\n",
			g.out.String())
		require.NoError(t, brandgen.CheckIcons(g.dir), "every icon exists and the stamp names the current artwork")
	})

	tests := []struct {
		name     string
		renderer answer
		iconutil answer
		wantErr  string
	}{
		{name: "the icon set render fails", renderer: exits(1), iconutil: writesOutput, wantErr: "render the 16-pixel appicon.svg"},
		{name: "iconutil fails", renderer: writesOutput, iconutil: exits(1), wantErr: "build the icon set"},
		{
			name:     "the Dock icon render fails",
			renderer: failsFor(filepath.Base(brandgen.DevIconPath)),
			iconutil: writesOutput,
			wantErr:  "render the 512-pixel appicon.svg",
		},
		{
			name:     "the menu bar icon render fails",
			renderer: failsFor(filepath.Base(brandgen.TrayIconPath)),
			iconutil: writesOutput,
			wantErr:  "render the 66-pixel trayicon.svg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := artwork(t)
			g.tool(svgRenderer, tt.renderer)
			g.tool("iconutil", tt.iconutil)

			err := g.runner().renderIcons()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("the stamp cannot be written", func(t *testing.T) {
		t.Parallel()

		g := artwork(t)
		require.NoError(t, os.Mkdir(g.path(brandgen.IconStampPath), 0o750))
		g.tool(svgRenderer, writesOutput)
		g.tool("iconutil", writesOutput)

		require.Error(t, g.runner().renderIcons())
	})

	t.Run("no temporary directory", func(t *testing.T) {
		t.Parallel()

		r := artwork(t).runner()
		r.tmp = filepath.Join(t.TempDir(), "missing")

		err := r.renderIcons()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create a temporary directory")
	})
}
