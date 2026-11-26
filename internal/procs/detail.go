package procs

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Detail is more of one process than a listing reads: its whole command line, and whether
// it has a terminal. DetailOf reads it for the few processes that need it.
type Detail struct {
	// Args is its command line, its arguments joined by spaces on one line, as in
	// "/bin/sh -c sleep 5", cut short past maxArgs runes, or "" when it can't be read.
	Args string

	// Terminal reports a process with a controlling terminal: one a terminal's shell
	// started, as a session the user runs is.
	Terminal bool
}

// maxArgs is the most runes of a command line Detail keeps: enough to tell a program and
// what it runs, short of a long script's whole text.
const maxArgs = 1000

// argsLine joins a process's arguments into Detail.Args: one line of plain text, cut
// short past maxArgs runes.
func argsLine(args []string) string {
	return textutil.Truncate(textutil.OneLine(strings.Join(args, " ")), maxArgs)
}

// ParseProcArgs reads the arguments from macOS's kern.procargs2 for a process: its
// argument count as a 32-bit integer in the machine's byte order, the path it was
// executed from, NUL padding, then that many arguments, each ending in a NUL, and its
// environment after them, which it never reads. It returns nil for a buffer it can't read.
func ParseProcArgs(buf []byte) []string {
	const countSize = 4
	if len(buf) < countSize {
		return nil
	}

	argc := int(binary.NativeEndian.Uint32(buf[:countSize]))

	path := bytes.IndexByte(buf[countSize:], 0)
	if argc <= 0 || path < 0 {
		return nil
	}

	rest := bytes.TrimLeft(buf[countSize+path:], "\x00")
	out := make([]string, 0, min(argc, 64))

	for len(out) < argc && len(rest) > 0 {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			out = append(out, string(rest))

			break
		}

		out = append(out, string(rest[:end]))
		rest = rest[end+1:]
	}

	return out
}

// ParseProcEnv reads one environment variable, name, from macOS's kern.procargs2 for a
// process, laid out as ParseProcArgs reads it: it skips the argument count, the path the
// process was executed from and its NUL padding, and that many arguments, then looks for
// name among the environment's NAME=value strings, each ending in a NUL, which end at an
// empty one. It returns name's value alone, copied out, and reports false for a name that
// isn't set there (one only among the arguments included), for a buffer it can't read, and
// for an empty name or one holding '=' or a NUL. The environment holds the process's
// secrets, so it keeps nothing else of it.
func ParseProcEnv(buf []byte, name string) (string, bool) {
	const countSize = 4
	if !envName(name) || len(buf) < countSize {
		return "", false
	}

	argc := binary.NativeEndian.Uint32(buf[:countSize])

	path := bytes.IndexByte(buf[countSize:], 0)
	if path < 0 {
		return "", false
	}

	rest := bytes.TrimLeft(buf[countSize+path:], "\x00")

	for ; argc > 0; argc-- {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return "", false
		}

		rest = rest[end+1:]
	}

	// The buffer is the process's memory as it is now, not as it was at exec: a process that
	// renamed itself, as node does for process.title, wrote the new name over its arguments
	// and NULs over the rest of them, so after argc strings the read can still be among
	// those NULs. An environment string is never empty, so skipping them can't make an
	// argument read as a variable.
	rest = bytes.TrimLeft(rest, "\x00")

	return envValue(rest, name)
}

// envName reports whether name can be an environment variable's name: not empty, and
// holding neither the '=' that ends one nor the NUL that ends the string.
func envName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "=\x00")
}

// envValue returns the value of name among env's NAME=value strings, each ending in a NUL,
// which end at an empty one or the buffer's end; a string the buffer cuts short, with no
// NUL, is not one. The first one set wins, as getenv's does.
func envValue(env []byte, name string) (string, bool) {
	prefix := []byte(name + "=")

	for {
		end := bytes.IndexByte(env, 0)
		if end <= 0 {
			return "", false
		}

		if value, ok := bytes.CutPrefix(env[:end], prefix); ok {
			return string(value), true
		}

		env = env[end+1:]
	}
}

// ByPID returns list's processes by PID.
func ByPID(list []Process) map[int]Process {
	out := make(map[int]Process, len(list))
	for _, p := range list {
		out[p.PID] = p
	}

	return out
}

// Adopted reports a process the system's process manager adopted: its parent is PID 1,
// or a process of byPID named launchd, init, or systemd (a user's own systemd too). Either
// what started it exited and it was handed over, or it was started as a background
// service.
func Adopted(p Process, byPID map[int]Process) bool {
	if p.PPID == 1 {
		return true
	}

	parent, ok := byPID[p.PPID]

	return ok && parent.PID != p.PID && slices.Contains(managers, filepath.Base(parent.Command))
}
