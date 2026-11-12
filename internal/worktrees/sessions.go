package worktrees

import (
	"context"
	"os"
	"slices"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/procs"
)

// processes lists the running processes that can be a session: every process but this
// app and the commands it runs, and a coding agent's background service with everything
// under it (see procs.WithoutServices). Codex's app-server daemon keeps the folder it was
// started from as its working directory long after the session that started it ended, so
// it never counts. It reports whether the processes could be listed; without a lister
// none is listed, and that is known.
func (s *Service) processes(ctx context.Context) ([]procs.Process, bool) {
	if s.lister == nil {
		return nil, true
	}

	list, err := s.lister.Processes(ctx)
	if err != nil {
		s.logger.DebugContext(ctx, "the running processes could not be listed", "error", err)

		return nil, false
	}

	return procs.WithoutServices(procs.Others(list, os.Getpid()), agentCommands()), true
}

// agentCommands are the coding agents' commands, whose background services are no
// session.
func agentCommands() []string {
	agents := agent.All()
	out := make([]string, 0, len(agents))

	for _, a := range agents {
		out = append(out, a.Command)
	}

	return out
}

// sessionsIn returns the sessions working in dir, naming the agent of each the app
// follows, and whether the processes could be listed. The followed sessions are read
// only when some process works in dir.
func (s *Service) sessionsIn(ctx context.Context, dir string) ([]Session, bool) {
	running, known := s.processes(ctx)

	within := procs.Within(running, dir)
	if len(within) == 0 {
		return []Session{}, known
	}

	return sessionsOf(within, s.agentProcesses(ctx)), known
}

// AgentProcesses names the processes of the agent sessions the app follows. The app
// adapts the sessions ledger to it.
type AgentProcesses interface {
	// AgentProcesses returns the coding agent of each followed session's process, by PID.
	AgentProcesses(ctx context.Context) (map[int]agent.ID, error)
}

// agentProcesses returns the agent of each followed session's process, or none when
// they cannot be read, which only costs the view its names.
func (s *Service) agentProcesses(ctx context.Context) map[int]agent.ID {
	if s.agents == nil {
		return nil
	}

	named, err := s.agents.AgentProcesses(ctx)
	if err != nil {
		s.logger.DebugContext(ctx, "the agent sessions' processes could not be read", "error", err)

		return nil
	}

	return named
}

// checkSessions refuses a worktree a running process works in. strict refuses it as well
// when the processes cannot be listed, as the automatic cleanup and a force removal do; a
// plain removal the user asks for goes ahead then, since git refuses a worktree with
// changes, and the locks still guard it.
func (s *Service) checkSessions(ctx context.Context, dir string, strict bool) error {
	sessions, known := s.sessionsIn(ctx, dir)

	switch {
	case len(sessions) > 0:
		return &InUseError{Sessions: sessions}
	case !known && strict:
		return ErrSessionsUnknown
	default:
		return nil
	}
}

// markSessions fills in the sessions working in each linked worktree, naming the agent of
// each the app follows. Each process's directory is resolved once for the whole listing.
func markSessions(repos []Repo, running []procs.Process, agents map[int]agent.ID) {
	located := procs.Locate(running)

	for _, wt := range rows(repos) {
		if !wt.Main && !wt.Prunable {
			wt.InUse = sessionsOf(located.Within(wt.Path), agents)
		}
	}
}

// sessionsOf returns processes as sessions, never nil, with agents naming the ones the app
// follows. A process another in the list started is folded into the furthest of its
// ancestors there: a session's own helpers are part of it. A followed agent folded into a
// process stands for it, with its own PID and program, since it is the process the user
// would look up or stop, as when the user resumed the session from a shell in the folder,
// unless that process is a followed agent itself.
func sessionsOf(list []procs.Process, agents map[int]agent.ID) []Session {
	parents := make(map[int]int, len(list))
	for _, p := range list {
		parents[p.PID] = p.PPID
	}

	roots := make(map[int]int, len(list))
	kept := make(map[int]int, len(list))
	out := make([]Session, 0, len(list))

	for _, p := range list {
		root := rootOf(p.PID, parents)
		roots[p.PID] = root

		if root == p.PID {
			kept[p.PID] = len(out)
			out = append(out, Session{PID: p.PID, Command: p.Command, Agent: agents[p.PID]})
		}
	}

	for _, p := range list {
		if i, ok := kept[roots[p.PID]]; ok && out[i].Agent == "" && agents[p.PID] != "" {
			out[i] = Session{PID: p.PID, Command: p.Command, Agent: agents[p.PID]}
		}
	}

	return out
}

// rootOf returns the process pid is folded into: its furthest ancestor among parents,
// which holds each listed process's parent by PID. Where the chain loops, as a reused PID
// can make it, it is the lowest PID in the loop, so a loop is still listed once.
func rootOf(pid int, parents map[int]int) int {
	path := []int{pid}
	seen := map[int]int{pid: 0}
	cur := pid

	for {
		parent := parents[cur]
		if _, listed := parents[parent]; parent == 0 || parent == cur || !listed {
			return cur
		}

		if i, looped := seen[parent]; looped {
			return slices.Min(path[i:])
		}

		seen[parent] = len(path)
		path = append(path, parent)
		cur = parent
	}
}
