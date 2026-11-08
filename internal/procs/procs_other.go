//go:build !darwin && !linux

package procs

import (
	"context"

	"github.com/mrz1836/nilbox/internal/execpath"
)

// processes cannot list processes here.
func processes(context.Context, execpath.Runner) ([]Process, error) {
	return nil, ErrUnsupported
}
