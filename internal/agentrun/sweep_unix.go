//go:build unix

package agentrun

import (
	"context"
	"log/slog"
	"os"
	"syscall"

	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/procs"
)

// ProcSweeper ends the processes working in a run's worktree once the run's group got
// SIGKILL (Sweeper): an agent's background shell calls setsid and so leaves the group, and
// SIGKILL, unlike SIGINT, leaves it alive, with the worktree as its folder (S1). The app
// and its own children are left alone, and so is every process outside the worktree.
type ProcSweeper struct {
	Lister procs.Lister
	Logger *slog.Logger
}

// Sweep sends SIGKILL to every process whose folder is dir or inside it, but the app's.
func (s ProcSweeper) Sweep(ctx context.Context, dir string) {
	if s.Lister == nil || dir == "" {
		return
	}

	list, err := s.Lister.Processes(ctx)
	if err != nil {
		logging.Or(s.Logger).DebugContext(ctx, "could not list what an agent's run left running", "error", err)

		return
	}

	for _, p := range procs.Within(procs.Others(list, os.Getpid()), dir) {
		if !procs.ValidPID(p.PID) {
			continue
		}

		if err = syscall.Kill(p.PID, syscall.SIGKILL); err == nil {
			logging.Or(s.Logger).InfoContext(ctx, "ended what an agent's run left running in its worktree",
				"pid", p.PID, "command", p.Command)
		}
	}
}
