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
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
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

// resolved returns path cleaned, with symbolic links resolved. A path that does not
// exist is resolved through its nearest existing parent, so it still matches the form the
// OS reports for it.
func resolved(path string) string {
	clean := filepath.Clean(path)

	for dir := clean; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			rest, relErr := filepath.Rel(dir, clean)
			if relErr != nil {
				return clean
			}

			return filepath.Join(r, rest)
		}

		if filepath.Dir(dir) == dir {
			return clean
		}
	}
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
