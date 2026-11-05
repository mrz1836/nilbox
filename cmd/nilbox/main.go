// Command nilbox is the application entry point.
//
// The desktop shell is not wired in yet. Until it is, the binary reports its identity and
// build metadata, which keeps the build, version-injection, and release plumbing honest.
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
	if err := run(os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(1)
	}
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
