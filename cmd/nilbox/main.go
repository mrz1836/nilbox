//go:build !desktop

// Command nilbox is the application entry point.
//
// This file is the headless build: it reports the product identity and build metadata,
// which keeps the build, version-injection, and release plumbing honest on every OS
// without a webview. The desktop app builds from the same package with the desktop build
// tag (see main_desktop.go); `magex app:build` produces it.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mrz1836/nilbox/internal/brand"
)

// Build metadata, injected at build time via -ldflags (see .mage.yaml).
//
//nolint:gochecknoglobals // set by the linker
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	os.Exit(exitStatus(run(os.Stdout), os.Stderr))
}

// run writes the product identity and build metadata to w.
func run(w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s %s (commit %s, built %s)\n%s\n",
		brand.Name, version, commit, buildDate, brand.Tagline)
	if err != nil {
		return fmt.Errorf("write identity: %w", err)
	}

	return nil
}
