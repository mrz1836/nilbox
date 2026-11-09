// Package procs lists the running processes of the signed-in user with the directory each
// works in, so the app can tell a worktree some session still runs in, whichever coding
// agent or tool that is.
//
// On macOS it asks lsof, on Linux it reads /proc; on any other OS it reports
// ErrUnsupported, and callers treat whether a directory is in use as unknown.
package procs

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/paths"
)

// ErrUnsupported is returned where this OS's processes cannot be listed.
var ErrUnsupported = errors.New("listing processes is not supported on this OS")

// Process is one running process.
type Process struct {
	// PID is its process ID, and PPID its parent's, or 0 when unknown.
	PID  int
	PPID int

	// Command is its program's name, such as "codex", as the OS reports it.
	Command string

	// Dir is its working directory, an absolute path.
	Dir string
}

// Lister lists the running processes. System is the real one.
type Lister interface {
	// Processes returns the signed-in user's running processes, by PID, or
	// ErrUnsupported.
	Processes(ctx context.Context) ([]Process, error)
}

// System lists this machine's processes. Run runs lsof where the OS needs it; nil runs
// programs directly.
type System struct {
	Run execpath.Runner
}

// Processes returns the signed-in user's running processes, by PID.
func (s System) Processes(ctx context.Context) ([]Process, error) {
	run := s.Run
	if run == nil {
		run = execpath.Exec{}
	}

	list, err := processes(ctx, run)
	if err != nil {
		return nil, err
	}

	slices.SortFunc(list, func(a, b Process) int { return a.PID - b.PID })

	return list, nil
}

// Within returns the processes whose working directory is dir or inside it, by PID. dir
// and each process's directory are compared with symbolic links resolved where they can
// be, so a link to a worktree still counts.
func Within(list []Process, dir string) []Process {
	root := resolved(dir)

	var out []Process

	for _, p := range list {
		if p.Dir == "" {
			continue
		}

		if d := resolved(p.Dir); d == root || strings.HasPrefix(d, root+string(filepath.Separator)) {
			out = append(out, p)
		}
	}

	return out
}

// In returns the processes whose working directory is dir itself, not one inside it, by
// PID, compared as Within compares them.
func In(list []Process, dir string) []Process {
	root := resolved(dir)

	var out []Process

	for _, p := range list {
		if p.Dir != "" && resolved(p.Dir) == root {
			out = append(out, p)
		}
	}

	return out
}

// resolved returns path cleaned, with symbolic links resolved, in the form the OS
// reports for it.
func resolved(path string) string {
	return paths.Resolved(path)
}

// MaxPID is the largest process ID any OS the app runs on hands out. A larger one would
// be cut short by the kernel, so kill(2) could read it as -1 and reach every process.
const MaxPID = math.MaxInt32

// ValidPID reports whether pid can be a process's ID: positive, and no larger than
// MaxPID.
func ValidPID(pid int) bool {
	return pid > 0 && pid <= MaxPID
}

// ParseLsof reads `lsof -F pcRn` output: a "p<pid>" line starts each process, an
// "R<ppid>" line gives its parent, a "c<command>" line names it, and the first "n<path>"
// line after it is its working directory when lsof is asked for the cwd descriptor alone.
// Other lines are skipped, and a process with no directory is left out.
func ParseLsof(out string) []Process {
	var p lsofParser

	for line := range strings.SplitSeq(out, "\n") {
		if line != "" {
			p.field(line[0], line[1:])
		}
	}

	p.flush()

	return p.list
}

// lsofParser accumulates processes while ParseLsof reads lsof's fields.
type lsofParser struct {
	list []Process
	cur  *Process
}

// field reads one field: its identifying letter and its value.
func (p *lsofParser) field(id byte, value string) {
	if id == 'p' {
		p.flush()

		if pid, err := strconv.Atoi(value); err == nil && pid > 0 {
			p.cur = &Process{PID: pid}
		}

		return
	}

	if p.cur == nil {
		return
	}

	switch id {
	case 'R':
		p.cur.PPID, _ = strconv.Atoi(value) // an unreadable parent is unknown, 0
	case 'c':
		p.cur.Command = value
	case 'n':
		if p.cur.Dir == "" && filepath.IsAbs(value) {
			p.cur.Dir = value
		}
	}
}

// flush ends the process being read, keeping it when it has a directory.
func (p *lsofParser) flush() {
	if p.cur != nil && p.cur.Dir != "" {
		p.list = append(p.list, *p.cur)
	}

	p.cur = nil
}

// managers are the commands of the processes that start and adopt background services:
// launchd on macOS, and init or systemd (a user's own systemd too) on Linux.
var managers = []string{"launchd", "init", "systemd"} //nolint:gochecknoglobals // a fixed table

// maxDepth bounds the walk up a process's parents, which a reused PID could turn into a
// loop.
const maxDepth = 64

// WithoutServices returns list without the background services of the commands named, and
// every process under one. A service is a process of one of commands whose parent is the
// system's process manager (PID 1, or launchd, init, or systemd), as a daemon's is, such
// as Codex's app-server daemon. It outlives the session that started it and keeps that
// session's folder as its working directory, but works for no session there. The same
// command run by a terminal or an editor, as a session is, has that as its parent, and
// stays.
func WithoutServices(list []Process, commands []string) []Process {
	byPID := make(map[int]Process, len(list))
	for _, p := range list {
		byPID[p.PID] = p
	}

	isService := func(p Process) bool {
		if !slices.Contains(commands, filepath.Base(p.Command)) {
			return false
		}

		parent, ok := byPID[p.PPID]

		return p.PPID == 1 || (ok && slices.Contains(managers, filepath.Base(parent.Command)))
	}

	var out []Process

	for _, p := range list {
		if !underService(p, byPID, isService) {
			out = append(out, p)
		}
	}

	return out
}

// underService reports p, or a process above it, as a service.
func underService(p Process, byPID map[int]Process, isService func(Process) bool) bool {
	for range maxDepth {
		if isService(p) {
			return true
		}

		parent, ok := byPID[p.PPID]
		if !ok || parent.PID == p.PID {
			return false
		}

		p = parent
	}

	return false
}

// Others returns the processes that are neither self nor its children, such as the git
// commands self runs in a directory while it looks at that directory.
func Others(list []Process, self int) []Process {
	var out []Process

	for _, p := range list {
		if p.PID != self && p.PPID != self {
			out = append(out, p)
		}
	}

	return out
}

// CwdsOf returns the working directory of each listed process, by PID.
func CwdsOf(list []Process) map[int]string {
	out := make(map[int]string, len(list))
	for _, p := range list {
		out[p.PID] = p.Dir
	}

	return out
}
