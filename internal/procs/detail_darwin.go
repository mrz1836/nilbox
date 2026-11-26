//go:build darwin

package procs

import "golang.org/x/sys/unix"

// noDev is the terminal device of a process with no controlling terminal (NODEV).
const noDev = -1

// DetailOf reads the process with pid's command line and whether it has a terminal, from
// the kernel's process table, starting nothing, and reports whether it could. A process
// of another user's has a terminal that can be read, but no command line.
func DetailOf(pid int) (Detail, bool) {
	if !ValidPID(pid) {
		return Detail{}, false
	}

	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info == nil || info.Proc.P_pid != int32(pid) { //nolint:gosec // ValidPID bounds pid by MaxInt32
		return Detail{}, false
	}

	d := Detail{Terminal: info.Eproc.Tdev != noDev}

	if raw, argsErr := unix.SysctlRaw("kern.procargs2", pid); argsErr == nil {
		d.Args = argsLine(ParseProcArgs(raw))
	}

	return d, true
}

// EnvOf returns the value of one environment variable, name, of the process with pid, as
// the process started with it, from the kernel's process table (kern.procargs2), starting
// nothing, and reports whether it could. It returns that one variable and keeps nothing
// else: the environment holds the process's secrets, so its buffer is cleared before
// EnvOf returns, and a caller never logs or stores any more of it. macOS hides the
// environment of its own platform binaries, such as /bin/zsh, from a caller that isn't
// root, and of another user's processes, so those report false.
func EnvOf(pid int, name string) (string, bool) {
	if !ValidPID(pid) || !envName(name) {
		return "", false
	}

	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil {
		return "", false
	}

	defer clear(raw)

	return ParseProcEnv(raw, name)
}
