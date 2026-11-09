//go:build mage

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/mg"
)

// frontendDir holds the desktop frontend (its package.json and lockfile).
const frontendDir = "frontend"

// errPNPMMissing is returned when pnpm is not on PATH.
var errPNPMMissing = errors.New("pnpm is required")

// Frontend groups the targets for the desktop frontend under frontend/.
type Frontend mg.Namespace

// Install installs the frontend dependencies exactly as pnpm-lock.yaml pins them.
//
//	magex frontend:install
func (Frontend) Install() error {
	if err := requirePNPM(); err != nil {
		return err
	}

	return runPNPM(frontendDir, "install", "--frozen-lockfile")
}

// Build type-checks the frontend and builds it into frontend/dist.
//
//	magex frontend:build
func (Frontend) Build() error {
	if err := (Frontend{}).Install(); err != nil {
		return err
	}

	return runPNPM(frontendDir, "build")
}

// Gate runs the frontend gate, the one entry point for every frontend check: the
// frozen-lockfile install, then each of frontendGateScripts in order. Any failure stops
// the gate. The frontend CI workflow runs this same target, so local runs and CI cannot
// drift apart.
//
//	magex frontend:gate
func (Frontend) Gate() error {
	if err := (Frontend{}).Install(); err != nil {
		return err
	}

	for _, script := range frontendGateScripts() {
		if err := runPNPM(frontendDir, "run", script); err != nil {
			return err
		}
	}

	return nil
}

// frontendGateScripts are the frontend/package.json scripts the gate runs, in order: the
// format check, lint, typecheck, the tests with coverage, and the render profile through
// the React Compiler.
func frontendGateScripts() []string {
	return []string{"format:check", "lint", "typecheck", "test:coverage", "test:renders"}
}

// requirePNPM returns an actionable error naming the pinned pnpm version when pnpm is
// not installed.
func requirePNPM() error {
	if _, err := exec.LookPath("pnpm"); err == nil {
		return nil
	}

	pinned := "pnpm"

	body, err := os.ReadFile(filepath.Join(frontendDir, "package.json"))
	if err == nil {
		var manifest struct {
			PackageManager string `json:"packageManager"`
		}

		if json.Unmarshal(body, &manifest) == nil && manifest.PackageManager != "" {
			pinned = manifest.PackageManager
		}
	}

	return fmt.Errorf("%w. Install it once with: npm install -g %s", errPNPMMissing, pinned)
}

// runPNPM runs pnpm with args in dir, streaming its output.
func runPNPM(dir string, args ...string) error {
	cmd, err := pnpmCommand(context.Background(), dir, args...)
	if err != nil {
		return err
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err = cmd.Run(); err != nil {
		return fmt.Errorf("pnpm %v in %s: %w", args, dir, err)
	}

	return nil
}

// pnpmCommand returns a command that runs pnpm with args in dir. Until pnpm's native
// binary is installed, its launcher on macOS and Linux is a shell script without a
// shebang line: a shell runs it, but the OS will not exec it directly, so such a launcher
// runs through /bin/sh. Windows runs pnpm's .exe or .cmd launcher directly.
func pnpmCommand(ctx context.Context, dir string, args ...string) (*exec.Cmd, error) {
	if err := requirePNPM(); err != nil {
		return nil, err
	}

	path, err := exec.LookPath("pnpm")
	if err != nil {
		return nil, fmt.Errorf("find pnpm: %w", err)
	}

	name, argv := path, args
	if needsPOSIXShell(runtime.GOOS, path) {
		name, argv = "/bin/sh", append([]string{path}, args...)
	}

	cmd := exec.CommandContext(ctx, name, argv...) //nolint:gosec // argv only, no shell command line
	cmd.Dir = dir

	return cmd, nil
}

// needsPOSIXShell reports whether the launcher at path must run through /bin/sh on goos:
// a script without a #! line, anywhere but Windows, which has no /bin/sh and starts its
// .cmd launchers itself.
func needsPOSIXShell(goos, path string) bool {
	return goos != "windows" && shebangless(path)
}

// executableMagic are the leading bytes of native executables: ELF, Mach-O in both byte
// orders and word sizes, and universal binaries.
func executableMagic() [][]byte {
	return [][]byte{
		{0x7f, 'E', 'L', 'F'},
		{0xcf, 0xfa, 0xed, 0xfe},
		{0xce, 0xfa, 0xed, 0xfe},
		{0xfe, 0xed, 0xfa, 0xcf},
		{0xfe, 0xed, 0xfa, 0xce},
		{0xca, 0xfe, 0xba, 0xbe},
	}
}

// shebangless reports whether the file at path is a script with no #! line.
func shebangless(path string) bool {
	f, err := os.Open(path) //nolint:gosec // the pnpm launcher found on PATH
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, 4)

	n, _ := io.ReadFull(f, head)
	if n == 0 || bytes.HasPrefix(head[:n], []byte("#!")) {
		return false
	}

	for _, magic := range executableMagic() {
		if bytes.Equal(head[:n], magic) {
			return false
		}
	}

	return true
}
