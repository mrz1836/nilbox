//go:build mage

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
)

// outputWriter is a fake tool body that creates the file named after --output, as
// rsvg-convert and iconutil do.
const outputWriter = `while [ $# -gt 0 ]; do
  if [ "$1" = --output ]; then out="$2"; fi
  shift
done
: > "$out"`

// fakeRenderer puts an rsvg-convert into the PATH directory dir that logs its arguments,
// then runs body, and returns the log's path.
func fakeRenderer(t *testing.T, dir, body string) string {
	t.Helper()

	return fakeLoggedTool(t, dir, svgRenderer, body)
}

//nolint:paralleltest // changes the process's working directory
func TestBrandGenerate(t *testing.T) {
	dir := inTempDir(t)

	require.NoError(t, Brand{}.Generate())

	require.NoError(t, brandgen.Check(dir), "every generated file is written and current")
}

//nolint:paralleltest // changes the process's PATH
func TestRenderSVG(t *testing.T) {
	t.Run("renders a square PNG of the given side", func(t *testing.T) {
		log := fakeRenderer(t, isolatedPath(t), outputWriter)
		out := filepath.Join(t.TempDir(), "tray.png")

		require.NoError(t, renderSVG(brandgen.TraySVGPath, brandgen.TrayIconSize, out))

		assert.FileExists(t, out)
		assert.Equal(t, []string{
			"--width 66 --height 66 --output " + out + " " + filepath.FromSlash(brandgen.TraySVGPath),
		}, logLines(t, log))
	})

	t.Run("names the render that failed", func(t *testing.T) {
		fakeRenderer(t, isolatedPath(t), "exit 1")

		err := renderSVG(brandgen.TraySVGPath, brandgen.TrayIconSize, filepath.Join(t.TempDir(), "tray.png"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render the 66-pixel trayicon.svg")
	})
}

//nolint:paralleltest // changes the process's PATH
func TestRenderIconSet(t *testing.T) {
	t.Run("renders every size at 1x and 2x under the names iconutil expects", func(t *testing.T) {
		log := fakeRenderer(t, isolatedPath(t), outputWriter)
		dir := filepath.Join(t.TempDir(), brandgen.IconFile+".iconset")

		require.NoError(t, renderIconSet(dir))

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
		assert.Contains(t, logLines(t, log),
			"--width 1024 --height 1024 --output "+filepath.Join(dir, "icon_512x512@2x.png")+" "+filepath.FromSlash(brandgen.IconSVGPath))
	})

	t.Run("the icon set already exists", func(t *testing.T) {
		dir := t.TempDir()

		err := renderIconSet(dir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create the icon set")
	})

	t.Run("a 1x render fails", func(t *testing.T) {
		fakeRenderer(t, isolatedPath(t), "exit 1")

		err := renderIconSet(filepath.Join(t.TempDir(), "icons.iconset"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render the 16-pixel")
	})

	t.Run("a 2x render fails", func(t *testing.T) {
		fakeRenderer(t, isolatedPath(t), `case "$*" in *@2x*) exit 1;; esac`+"\n"+outputWriter)

		err := renderIconSet(filepath.Join(t.TempDir(), "icons.iconset"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "render the 32-pixel")
	})
}

//nolint:paralleltest // changes the process's working directory, PATH, and standard output
func TestBrandIconsOnMacOS(t *testing.T) {
	onlyOnMacOS(t)

	t.Run("needs rsvg-convert", func(t *testing.T) {
		inTempDir(t)
		isolatedPath(t)

		require.ErrorIs(t, Brand{}.Icons(), errNoSVGRenderer)
	})

	t.Run("renders every icon and records the artwork", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", brand.Slug), 0o750))

		tools := isolatedPath(t)
		fakeRenderer(t, tools, outputWriter)
		fakeTool(t, tools, "iconutil", outputWriter)

		var err error

		out := captureStdout(t, func() { err = Brand{}.Icons() })

		require.NoError(t, err)
		assert.Contains(t, out, "Rendered "+brandgen.IconSetPath)
		require.NoError(t, checkBrandFiles(), "the brand files and every icon are current")
	})
}

//nolint:paralleltest // changes the process's working directory, PATH, environment, and standard output
func TestRenderIcons(t *testing.T) {
	// artwork writes the brand files, the icon artwork among them, into a fresh working
	// directory, with the directory the development icons go to.
	artwork := func(t *testing.T) {
		t.Helper()

		dir := inTempDir(t)
		require.NoError(t, brandgen.Write(dir))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", brand.Slug), 0o750))
	}

	t.Run("renders every icon and records the artwork", func(t *testing.T) {
		artwork(t)

		tools := isolatedPath(t)
		fakeRenderer(t, tools, outputWriter)
		fakeTool(t, tools, "iconutil", outputWriter)

		var err error

		out := captureStdout(t, func() { err = renderIcons() })

		require.NoError(t, err)
		assert.Equal(t, "✅ Rendered "+brandgen.IconSetPath+", "+brandgen.DevIconPath+", and "+brandgen.TrayIconPath+"\n", out)
		require.NoError(t, brandgen.CheckIcons("."), "every icon exists and the stamp names the current artwork")
	})

	tests := []struct {
		name     string
		renderer string
		iconutil string
		wantErr  string
	}{
		{name: "the icon set render fails", renderer: "exit 1", iconutil: outputWriter, wantErr: "render the 16-pixel appicon.svg"},
		{name: "iconutil fails", renderer: outputWriter, iconutil: "exit 1", wantErr: "build the icon set"},
		{
			name:     "the Dock icon render fails",
			renderer: `case "$*" in *` + filepath.Base(brandgen.DevIconPath) + `*) exit 1;; esac` + "\n" + outputWriter,
			iconutil: outputWriter,
			wantErr:  "render the 512-pixel appicon.svg",
		},
		{
			name:     "the menu bar icon render fails",
			renderer: `case "$*" in *` + filepath.Base(brandgen.TrayIconPath) + `*) exit 1;; esac` + "\n" + outputWriter,
			iconutil: outputWriter,
			wantErr:  "render the 66-pixel trayicon.svg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artwork(t)

			tools := isolatedPath(t)
			fakeRenderer(t, tools, tt.renderer)
			fakeTool(t, tools, "iconutil", tt.iconutil)

			err := renderIcons()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("the stamp cannot be written", func(t *testing.T) {
		artwork(t)
		require.NoError(t, os.Mkdir(filepath.FromSlash(brandgen.IconStampPath), 0o750))

		tools := isolatedPath(t)
		fakeRenderer(t, tools, outputWriter)
		fakeTool(t, tools, "iconutil", outputWriter)

		require.Error(t, renderIcons())
	})

	t.Run("no temporary directory", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing")
		artwork(t)
		t.Setenv("TMPDIR", missing)

		err := renderIcons()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create a temporary directory")
	})
}
