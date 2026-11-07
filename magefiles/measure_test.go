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
	"github.com/mrz1836/nilbox/internal/measure"
)

// fakeBundle writes an app bundle under bin/ in the working directory, with an executable
// running body, and returns the executable's path.
func fakeBundle(t *testing.T, body string) string {
	t.Helper()

	exe := filepath.Join("bin", brand.Name+".app", "Contents", "MacOS", brand.Slug)
	writeExecutable(t, exe, "#!/bin/sh\n"+body+"\n")

	return exe
}

func TestPrintLines(t *testing.T) {
	t.Run("prints each line", func(t *testing.T) {
		var err error

		out := captureStdout(t, func() { err = printLines([]string{"size.binary_bytes=1", "size.bundle_bytes=2"}) })

		require.NoError(t, err)
		assert.Equal(t, "size.binary_bytes=1\nsize.bundle_bytes=2\n", out)
	})

	t.Run("reports a failed write", func(t *testing.T) {
		closed, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
		require.NoError(t, err)
		require.NoError(t, closed.Close())

		original := os.Stdout
		os.Stdout = closed

		defer func() { os.Stdout = original }()

		err = printLines([]string{"a"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "print the report")
	})
}

func TestSay(t *testing.T) {
	assert.Equal(t, "hello\n", captureStdout(t, func() { say("hello") }))
}

func TestMeasureOnMacOS(t *testing.T) {
	onlyOnMacOS(t)

	t.Run("size reports the binary and the bundle", func(t *testing.T) {
		inTempDir(t)
		exe := fakeBundle(t, "exit 0")
		writeFile(t, filepath.Join(filepath.Dir(filepath.Dir(exe)), "Info.plist"), strings.Repeat("x", 100))

		info, err := os.Stat(exe)
		require.NoError(t, err)

		out := captureStdout(t, func() { err = Measure{}.Size() })

		require.NoError(t, err)
		assert.Equal(t, strings.Join(measure.SizeReport{
			BinaryBytes: info.Size(),
			BundleBytes: info.Size() + 100,
		}.Lines(), "\n")+"\n", out)
	})

	t.Run("refresh runs the app's measurement mode", func(t *testing.T) {
		inTempDir(t)

		log := filepath.Join(t.TempDir(), "app.log")
		t.Setenv("APP_LOG", log)
		fakeBundle(t, `printf '%s\n' "$*" >> "$APP_LOG"`)

		require.NoError(t, Measure{}.Refresh())

		assert.Equal(t, []string{measure.RefreshFlag}, logLines(t, log))
	})

	t.Run("every measurement needs a built bundle", func(t *testing.T) {
		inTempDir(t)

		require.ErrorIs(t, Measure{}.All(), errNoBundle)
		require.ErrorIs(t, Measure{}.Startup(), errNoBundle)
	})
}
