//go:build !unix

package agentrun

import (
	"context"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/procs"
)

// ProcSweeper ends nothing on an OS without process groups, where no run starts.
type ProcSweeper struct {
	Lister procs.Lister
	Logger *slog.Logger
}

// Sweep does nothing.
func (ProcSweeper) Sweep(context.Context, string) {}
