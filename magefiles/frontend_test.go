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

// fakePNPM puts a pnpm on an isolated PATH that appends its arguments to a log, runs
// body, and returns the log's path.
func fakePNPM(t *testing.T, body string) string {
	t.Helper()

	return fakeLoggedTool(t, isolatedPath(t), "pnpm", body)
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestRequirePNPM(t *testing.T) {
	t.Run("pnpm on PATH", func(t *testing.T) {
		fakeTool(t, isolatedPath(t), "pnpm", "exit 0")

		require.NoError(t, requirePNPM())
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
			dir := inTempDir(t)
			isolatedPath(t)

			if tt.manifest != "" {
				testutil.WriteFile(t, filepath.Join(dir, frontendDir, "package.json"), tt.manifest)
			}

			err := requirePNPM()

			require.ErrorIs(t, err, errPNPMMissing)
			assert.EqualError(t, err, tt.want)
		})
	}
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestRequirePNPMNamesTheRepositoryPin(t *testing.T) {
	inRepoRoot(t)
	isolatedPath(t)

	err := requirePNPM()

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

//nolint:paralleltest // changes the process's working directory and PATH
func TestPnpmCommand(t *testing.T) {
	t.Run("runs a native launcher directly", func(t *testing.T) {
		pnpm := fakeTool(t, isolatedPath(t), "pnpm", "exit 0")

		cmd, err := pnpmCommand(t.Context(), frontendDir, "install")
		require.NoError(t, err)

		assert.Equal(t, pnpm, cmd.Path)
		assert.Equal(t, []string{pnpm, "install"}, cmd.Args)
		assert.Equal(t, frontendDir, cmd.Dir)
	})

	t.Run("runs a launcher without a shebang through sh", func(t *testing.T) {
		pnpm := filepath.Join(isolatedPath(t), "pnpm")
		testutil.WriteExecutable(t, pnpm, "exit 0\n")

		cmd, err := pnpmCommand(t.Context(), frontendDir, "install")
		require.NoError(t, err)

		assert.Equal(t, "/bin/sh", cmd.Path)
		assert.Equal(t, []string{"/bin/sh", pnpm, "install"}, cmd.Args)
	})

	t.Run("pnpm missing", func(t *testing.T) {
		inTempDir(t)
		isolatedPath(t)

		_, err := pnpmCommand(t.Context(), frontendDir, "install")

		require.ErrorIs(t, err, errPNPMMissing)
	})
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestRunPNPM(t *testing.T) {
	t.Run("passes the arguments", func(t *testing.T) {
		log := fakePNPM(t, "exit 0")

		require.NoError(t, runPNPM(t.TempDir(), "install", "--frozen-lockfile"))

		assert.Equal(t, []string{"install --frozen-lockfile"}, logLines(t, log))
	})

	t.Run("reports a failure", func(t *testing.T) {
		fakePNPM(t, "exit 3")

		err := runPNPM(t.TempDir(), "build")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "pnpm [build] in ")
	})

	t.Run("pnpm missing", func(t *testing.T) {
		inTempDir(t)
		isolatedPath(t)

		require.ErrorIs(t, runPNPM(t.TempDir(), "build"), errPNPMMissing)
	})
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestFrontendInstall(t *testing.T) {
	t.Run("installs from the lockfile", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, os.Mkdir(filepath.Join(dir, frontendDir), 0o750))

		log := fakePNPM(t, "exit 0")

		require.NoError(t, Frontend{}.Install())

		assert.Equal(t, []string{"install --frozen-lockfile"}, logLines(t, log))
	})

	t.Run("pnpm missing", func(t *testing.T) {
		inTempDir(t)
		isolatedPath(t)

		require.ErrorIs(t, Frontend{}.Install(), errPNPMMissing)
	})
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestFrontendBuild(t *testing.T) {
	t.Run("installs, then builds", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, os.Mkdir(filepath.Join(dir, frontendDir), 0o750))

		log := fakePNPM(t, "exit 0")

		require.NoError(t, Frontend{}.Build())

		assert.Equal(t, []string{"install --frozen-lockfile", "build"}, logLines(t, log))
	})

	t.Run("stops when the install fails", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, os.Mkdir(filepath.Join(dir, frontendDir), 0o750))

		log := fakePNPM(t, `[ "$1" = install ] && exit 1`+"\nexit 0")

		require.Error(t, Frontend{}.Build())

		assert.Equal(t, []string{"install --frozen-lockfile"}, logLines(t, log))
	})
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestFrontendGate(t *testing.T) {
	allSteps := []string{
		"install --frozen-lockfile",
		"run format:check",
		"run lint",
		"run typecheck",
		"run test:coverage",
		"run test:renders",
	}

	t.Run("installs, then runs every check in order", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, os.Mkdir(filepath.Join(dir, frontendDir), 0o750))

		log := fakePNPM(t, "exit 0")

		require.NoError(t, Frontend{}.Gate())

		assert.Equal(t, allSteps, logLines(t, log))
	})

	t.Run("stops at the first check that fails", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, os.Mkdir(filepath.Join(dir, frontendDir), 0o750))

		log := fakePNPM(t, `[ "$2" = lint ] && exit 1`+"\nexit 0")

		err := Frontend{}.Gate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "pnpm [run lint]")
		assert.Equal(t, allSteps[:3], logLines(t, log))
	})

	t.Run("stops when the install fails", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, os.Mkdir(filepath.Join(dir, frontendDir), 0o750))

		log := fakePNPM(t, `[ "$1" = install ] && exit 1`+"\nexit 0")

		require.Error(t, Frontend{}.Gate())

		assert.Equal(t, allSteps[:1], logLines(t, log))
	})

	t.Run("pnpm missing", func(t *testing.T) {
		inTempDir(t)
		isolatedPath(t)

		require.ErrorIs(t, Frontend{}.Gate(), errPNPMMissing)
	})
}

//nolint:paralleltest // changes the process's working directory
func TestFrontendGateScriptsExist(t *testing.T) {
	inRepoRoot(t)

	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}

	require.NoError(t, json.Unmarshal([]byte(readFile(t, filepath.Join(frontendDir, "package.json"))), &manifest))

	for _, script := range frontendGateScripts() {
		assert.Contains(t, manifest.Scripts, script, "frontend/package.json must define every script the gate runs")
	}
}
