//go:build mage

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	// frontendDir holds the desktop frontend (its package.json and lockfile).
	frontendDir = "frontend"

	// frontendGateScript is the single frontend gate shared with CI.
	frontendGateScript = "frontend/scripts/gate.sh"
)

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

	return runInDir(frontendDir, "pnpm", "install", "--frozen-lockfile")
}

// Build type-checks the frontend and builds it into frontend/dist.
//
//	magex frontend:build
func (Frontend) Build() error {
	if err := (Frontend{}).Install(); err != nil {
		return err
	}

	return runInDir(frontendDir, "pnpm", "build")
}

// Gate runs the shared frontend gate: frozen-lockfile install, format check, lint,
// typecheck, and tests with coverage. CI runs the same script.
//
//	magex frontend:gate
func (Frontend) Gate() error {
	return sh.RunV("bash", frontendGateScript)
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

// runInDir runs a command in dir, streaming its output.
func runInDir(dir, name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // argv only, no shell
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v in %s: %w", name, args, dir, err)
	}

	return nil
}
