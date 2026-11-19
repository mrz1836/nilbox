//go:build mage

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
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

func TestMeasureStartup(t *testing.T) {
	t.Parallel()

	t.Run("times every launch of the bundle's binary", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		exe := fakeBundle(t, g)

		var launched []string

		readings := []int64{900, 410, 400, 420, 390, 405}
		launch := func(_ context.Context, path string) (measure.Startup, error) {
			launched = append(launched, path)

			return measure.Startup{FirstPaintMS: readings[len(launched)-1], Source: "os"}, nil
		}

		require.NoError(t, g.runner().measureStartup(t.Context(), launch))

		assert.Equal(t, slices.Repeat([]string{exe}, startupRuns), launched, "the first launch, then five for the median")

		runs := make([]measure.Startup, 0, len(readings))
		for _, ms := range readings {
			runs = append(runs, measure.Startup{FirstPaintMS: ms, Source: "os"})
		}

		assert.Equal(t, strings.Join(measure.StartupReport{Runs: runs}.Lines(), "\n")+"\n", g.out.String())
	})

	t.Run("names the launch that failed", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeBundle(t, g)

		err := g.runner().measureStartup(t.Context(), func(context.Context, string) (measure.Startup, error) {
			return measure.Startup{}, measure.ErrNoTimingLine
		})

		require.ErrorIs(t, err, measure.ErrNoTimingLine)
		assert.Contains(t, err.Error(), "measure startup: launch 1 of 6")
		assert.Empty(t, g.out.String())
	})
}

func TestMeasureLaunch(t *testing.T) {
	t.Parallel()

	home := func() (string, error) { return "/Users/example", nil }

	t.Run("launches the agent magex names, from the home folder", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.setenv(mageArgsJSON, `["agent=codex"]`)

		var got measure.LaunchOptions

		report := measure.LaunchReport{Elapsed: 1500 * time.Millisecond, PID: 42}

		require.NoError(t, g.runner().measureLaunch(t.Context(), home,
			func(_ context.Context, opts measure.LaunchOptions) (measure.LaunchReport, error) {
				got = opts

				return report, nil
			}))

		assert.Equal(t, measure.LaunchOptions{Home: "/Users/example", Agent: agent.ID("codex"), Version: "dev"}, got,
			"outside a git checkout, the build's version is dev")
		assert.Equal(t, strings.Join(report.Lines(), "\n")+"\n", g.out.String())
	})

	t.Run("says when macOS needs to let it control iTerm2", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		report := measure.LaunchReport{NeedsAutomation: true, Reason: "not authorized"}

		require.NoError(t, g.runner().measureLaunch(t.Context(), home,
			func(_ context.Context, opts measure.LaunchOptions) (measure.LaunchReport, error) {
				assert.Equal(t, agent.Inherit, opts.Agent, "the default agent")

				return report, nil
			}))

		assert.Equal(t, "NEEDS AUTOMATION PERMISSION: allow this terminal to control iTerm2 in System Settings → "+
			"Privacy & Security → Automation, then run magex measure:launch again\n"+strings.Join(report.Lines(), "\n")+"\n",
			g.out.String())
	})

	refusals := []struct {
		name    string
		args    string
		home    func() (string, error)
		run     error
		wantErr error
		wantMsg string
	}{
		{name: "an agent this build doesn't know", args: `["agent=nope"]`, home: home, wantErr: agent.ErrUnknownAgent},
		{
			name: "no home folder", home: func() (string, error) { return "", appdata.ErrNoHomeDir }, wantErr: appdata.ErrNoHomeDir,
			wantMsg: "find the home directory",
		},
		{name: "a launch that fails", home: home, run: errStep, wantErr: errStep, wantMsg: "measure the launch"},
	}

	for _, tt := range refusals {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newRig(t)
			g.setenv(mageArgsJSON, tt.args)

			ran := false
			err := g.runner().measureLaunch(t.Context(), tt.home,
				func(context.Context, measure.LaunchOptions) (measure.LaunchReport, error) {
					ran = true

					return measure.LaunchReport{}, tt.run
				})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.Equal(t, tt.run != nil, ran, "nothing is launched before the agent and the home folder are found")
			assert.Empty(t, g.out.String())
		})
	}
}

func TestMeasureSizeOfAnUnreadableBundle(t *testing.T) {
	t.Parallel()

	skipAsRoot(t)

	g := newRig(t)
	exe := fakeBundle(t, g)
	resources := filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources")
	mkdir(t, resources)
	require.NoError(t, os.Chmod(resources, 0o000))
	t.Cleanup(func() { _ = os.Chmod(resources, 0o700) }) //nolint:gosec // lets the test's cleanup remove it

	require.Error(t, g.runner().measureSize())
	assert.Empty(t, g.out.String(), "no report half measured")
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
