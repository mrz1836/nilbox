//go:build mage

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// frontendRig returns a rig whose checkout holds the frontend's folder, with a pnpm that
// answers each call with a.
func frontendRig(t *testing.T, a answer) *rig {
	t.Helper()

	g := newRig(t)
	mkdir(t, g.path(frontendDir))
	g.tool("pnpm", a)

	return g
}

// failsAt is a fake pnpm's answer that fails when its argument at index i is arg, and
// succeeds otherwise.
func failsAt(i int, arg string) answer {
	return func(args []string) testutil.FakeRun {
		if len(args) > i && args[i] == arg {
			return testutil.FakeRun{Exit: 1}
		}

		return testutil.FakeRun{}
	}
}

func TestRequirePNPM(t *testing.T) {
	t.Parallel()

	t.Run("pnpm on PATH", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("pnpm", exits(0))

		require.NoError(t, g.runner().requirePNPM())
	})

	tests := []struct {
		name     string
		manifest string // frontend/package.json, or "" for none
		want     string
	}{
		{
			name:     "names the pinned version",
			manifest: `{"packageManager": "pnpm@12.16.1"}`,
			want:     "pnpm is required. Install it once with: npm install -g pnpm@12.16.1",
		},
		{
			name: "no package.json",
			want: "pnpm is required. Install it once with: npm install -g pnpm",
		},
		{
			name:     "package.json without a pin",
			manifest: `{"name": "frontend"}`,
			want:     "pnpm is required. Install it once with: npm install -g pnpm",
		},
		{
			name:     "unreadable package.json",
			manifest: `{not json`,
			want:     "pnpm is required. Install it once with: npm install -g pnpm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newRig(t)

			if tt.manifest != "" {
				testutil.WriteFile(t, g.path(frontendDir+"/package.json"), tt.manifest)
			}

			err := g.runner().requirePNPM()

			require.ErrorIs(t, err, errPNPMMissing)
			assert.EqualError(t, err, tt.want)
		})
	}
}

func TestRequirePNPMNamesTheRepositoryPin(t *testing.T) {
	t.Parallel()

	g := newRig(t)
	g.dir = repoRoot(t) // read only: no pnpm, so nothing runs

	err := g.runner().requirePNPM()

	require.ErrorIs(t, err, errPNPMMissing)
	assert.Regexp(t, `npm install -g pnpm@\d+\.\d+\.\d+$`, err.Error(), "frontend/package.json pins packageManager")
}

func TestShebangless(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content []byte
		want    bool
	}{
		{name: "script with a shebang", content: []byte("#!/bin/sh\nexit 0\n"), want: false},
		{name: "script without a shebang", content: []byte("exec node pnpm.cjs \"$@\"\n"), want: true},
		{name: "short script", content: []byte("ls"), want: true},
		{name: "empty file", content: nil, want: false},
		{name: "ELF binary", content: []byte{0x7f, 'E', 'L', 'F', 2, 1}, want: false},
		{name: "64-bit Mach-O binary", content: []byte{0xcf, 0xfa, 0xed, 0xfe, 7, 0}, want: false},
		{name: "32-bit Mach-O binary", content: []byte{0xce, 0xfa, 0xed, 0xfe}, want: false},
		{name: "big-endian Mach-O binary", content: []byte{0xfe, 0xed, 0xfa, 0xcf}, want: false},
		{name: "big-endian 32-bit Mach-O binary", content: []byte{0xfe, 0xed, 0xfa, 0xce}, want: false},
		{name: "universal binary", content: []byte{0xca, 0xfe, 0xba, 0xbe, 0, 0}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "pnpm")
			require.NoError(t, os.WriteFile(path, tt.content, 0o600))

			assert.Equal(t, tt.want, shebangless(path))
		})
	}

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		assert.False(t, shebangless(filepath.Join(t.TempDir(), "missing")))
	})
}

func TestNeedsPOSIXShell(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := filepath.Join(dir, "pnpm")
	require.NoError(t, os.WriteFile(script, []byte("exec node pnpm.cjs \"$@\"\n"), 0o600))

	shebang := filepath.Join(dir, "pnpm-shebang")
	require.NoError(t, os.WriteFile(shebang, []byte("#!/bin/sh\nexit 0\n"), 0o600))

	tests := []struct {
		name string
		goos string
		path string
		want bool
	}{
		{name: "script without a shebang on macOS", goos: "darwin", path: script, want: true},
		{name: "script without a shebang on Linux", goos: "linux", path: script, want: true},
		{name: "script with a shebang on Linux", goos: "linux", path: shebang, want: false},
		{name: "Windows starts its launchers itself", goos: "windows", path: script, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, needsPOSIXShell(tt.goos, tt.path))
		})
	}
}

func TestPnpmCommand(t *testing.T) {
	t.Parallel()

	t.Run("runs a native launcher directly", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("pnpm", exits(0))

		pnpm, err := g.lookPath("pnpm")
		require.NoError(t, err)

		cmd, err := g.runner().pnpmCommand(t.Context(), "install")
		require.NoError(t, err)

		assert.Equal(t, []toolCall{{name: pnpm, args: []string{"install"}}}, g.toolCalls(), "the launcher found on PATH runs")
		assert.Equal(t, g.path(frontendDir), cmd.Dir)
	})

	t.Run("runs a launcher without a shebang through sh", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("sh", exits(0))

		// pnpm's launcher, read for its first line and never run, as a shell runs it.
		launcher := filepath.Join(t.TempDir(), "pnpm")
		testutil.WriteFile(t, launcher, "exec node pnpm.cjs \"$@\"\n")

		r := g.runner()
		r.lookPath = func(string) (string, error) { return launcher, nil }

		cmd, err := r.pnpmCommand(t.Context(), "install")
		require.NoError(t, err)

		assert.Equal(t, []toolCall{{name: "/bin/sh", args: []string{launcher, "install"}}}, g.toolCalls())
		assert.Equal(t, g.path(frontendDir), cmd.Dir)
	})

	t.Run("pnpm missing", func(t *testing.T) {
		t.Parallel()

		_, err := newRig(t).runner().pnpmCommand(t.Context(), "install")

		require.ErrorIs(t, err, errPNPMMissing)
	})
}

func TestRunPNPM(t *testing.T) {
	t.Parallel()

	t.Run("passes the arguments", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, exits(0))

		require.NoError(t, g.runner().runPNPM("install", "--frozen-lockfile"))

		assert.Equal(t, []string{"install --frozen-lockfile"}, g.linesOf("pnpm"))
	})

	t.Run("reports a failure", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, exits(3))

		err := g.runner().runPNPM("build")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "pnpm [build] in "+g.path(frontendDir))
	})

	t.Run("pnpm missing", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, newRig(t).runner().runPNPM("build"), errPNPMMissing)
	})
}

func TestFrontendInstall(t *testing.T) {
	t.Parallel()

	t.Run("installs from the lockfile", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, exits(0))

		require.NoError(t, g.runner().installFrontend())

		assert.Equal(t, []string{"install --frozen-lockfile"}, g.linesOf("pnpm"))
	})

	t.Run("pnpm missing", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, newRig(t).runner().installFrontend(), errPNPMMissing)
	})
}

func TestFrontendBuild(t *testing.T) {
	t.Parallel()

	t.Run("installs, then builds", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, exits(0))

		require.NoError(t, g.runner().buildFrontend())

		assert.Equal(t, []string{"install --frozen-lockfile", "build"}, g.linesOf("pnpm"))
	})

	t.Run("stops when the install fails", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, failsAt(0, "install"))

		require.Error(t, g.runner().buildFrontend())

		assert.Equal(t, []string{"install --frozen-lockfile"}, g.linesOf("pnpm"))
	})
}

func TestFrontendGate(t *testing.T) {
	t.Parallel()

	allSteps := []string{
		"install --frozen-lockfile",
		"run format:check",
		"run lint",
		"run typecheck",
		"run test:coverage",
		"run test:renders",
	}

	t.Run("installs, then runs every check in order", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, exits(0))

		require.NoError(t, g.runner().gateFrontend())

		assert.Equal(t, allSteps, g.linesOf("pnpm"))
	})

	t.Run("stops at the first check that fails", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, failsAt(1, "lint"))

		err := g.runner().gateFrontend()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "pnpm [run lint]")
		assert.Equal(t, allSteps[:3], g.linesOf("pnpm"))
	})

	t.Run("stops when the install fails", func(t *testing.T) {
		t.Parallel()

		g := frontendRig(t, failsAt(0, "install"))

		require.Error(t, g.runner().gateFrontend())

		assert.Equal(t, allSteps[:1], g.linesOf("pnpm"))
	})

	t.Run("pnpm missing", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, newRig(t).runner().gateFrontend(), errPNPMMissing)
	})
}

func TestFrontendGateScriptsExist(t *testing.T) {
	t.Parallel()

	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}

	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(repoRoot(t), frontendDir, "package.json"))), &manifest))

	for _, script := range frontendGateScripts() {
		assert.Contains(t, manifest.Scripts, script, "frontend/package.json must define every script the gate runs")
	}
}
