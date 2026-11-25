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
// follows, and the app's own runs there, and whether the processes could be listed. The
// followed sessions are read only when some process works in dir.
func (s *Service) sessionsIn(ctx context.Context, dir string) ([]Session, bool) {
	running, known := s.processes(ctx)
	runs := s.runSessions(ctx)

	within := procs.Within(running, dir)
	if len(within) == 0 {
		return append([]Session{}, runs.within(dir)...), known
	}

	return append(sessionsOf(within, s.agentProcesses(ctx)), runs.within(dir)...), known
}

// RunFolder is a folder one of the app's own runs works in now.
type RunFolder struct {
	// Dir is the folder, and PID the run's program, or 0 while it starts.
	Dir string
	PID int

	// Agent is the agent it runs, and By names it: "⚡ Fix checks".
	Agent agent.ID
	By    string
}

// RunFolders names the folders the app's own runs work in now: an agent's background run,
// one from before the app restarted included, which the process list leaves out (the
// app's own children, and an orphan whose parent is the process manager). Core adapts
// the runs.
type RunFolders interface {
	RunFolders(ctx context.Context) []RunFolder
}

// runSessions are the app's own runs, as sessions, located once.
type runSessions struct {
	located *procs.Located
	runs    []RunFolder
}

// runSessions returns the app's own runs working now.
func (s *Service) runSessions(ctx context.Context) runSessions {
	if s.runs == nil {
		return runSessions{}
	}

	runs := s.runs.RunFolders(ctx)
	list := make([]procs.Process, 0, len(runs))

	for i, r := range runs {
		list = append(list, procs.Process{PID: i + 1, Dir: r.Dir})
	}

	return runSessions{located: procs.Locate(list), runs: runs}
}

// within returns the runs working in dir or inside it, as sessions.
func (r runSessions) within(dir string) []Session {
	if r.located == nil {
		return nil
	}

	var out []Session

	for _, p := range r.located.Within(dir) {
		run := r.runs[p.PID-1]
		command := ""

		if a, err := agent.Lookup(run.Agent); err == nil {
			command = a.Command
		}

		out = append(out, Session{PID: run.PID, Command: command, Agent: run.Agent, By: run.By})
	}

	return out
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
// each the app follows, and the app's own runs there. Each process's directory is resolved
// once for the whole listing.
func markSessions(repos []Repo, running []procs.Process, agents map[int]agent.ID, runs ...runSessions) {
	located := procs.Locate(running)

	for _, wt := range rows(repos) {
		if !wt.Main && !wt.Prunable {
			wt.InUse = sessionsOf(located.Within(wt.Path), agents)

			for _, r := range runs {
				wt.InUse = append(wt.InUse, r.within(wt.Path)...)
			}
		}
	}
}

// describeSessions fills in, for each process working in a linked worktree, its whole
// command line and whether it is detached: it has no terminal, and the system's process
// manager adopted it from running, the listing's processes. It reads each process once,
// and none of the app's own runs, which it starts itself. Without describe it does
// nothing.
func describeSessions(repos []Repo, running []procs.Process, describe func(int) (procs.Detail, bool)) {
	if describe == nil {
		return
	}

	byPID := procs.ByPID(running)
	read := onceEach(describe)

	for _, wt := range rows(repos) {
		for i := range wt.InUse {
			describeSession(&wt.InUse[i], byPID, read)
		}
	}
}

// describeSession fills in s from its process's detail, read with read, unless it is one
// of the app's own runs or byPID, the listing's processes, doesn't hold its process.
func describeSession(s *Session, byPID map[int]procs.Process, read func(int) (procs.Detail, bool)) {
	p, listed := byPID[s.PID]
	if s.By != "" || !listed {
		return
	}

	if d, ok := read(s.PID); ok {
		s.Args, s.Detached = d.Args, !d.Terminal && procs.Adopted(p, byPID)
	}
}

// onceEach returns describe, asking it of each PID once and answering again from what it
// said.
func onceEach(describe func(int) (procs.Detail, bool)) func(int) (procs.Detail, bool) {
	type answer struct {
		detail procs.Detail
		ok     bool
	}

	answers := make(map[int]answer)

	return func(pid int) (procs.Detail, bool) {
		a, seen := answers[pid]
		if !seen {
			a.detail, a.ok = describe(pid)
			answers[pid] = a
		}

		return a.detail, a.ok
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
