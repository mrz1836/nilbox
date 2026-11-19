//go:build mage

package main

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

// runner is what a target's steps run with: the checkout they work in, where they print,
// the environment they read, and how they find and start the tools they run. A target
// checks the OS and reads its arguments, then hands the work to its steps with
// hostRunner, this process's own; a test hands the steps a runner of its own (a checkout
// in a temporary folder, a buffer, fake tools), so it changes nothing of the process's
// and runs in parallel with the others.
type runner struct {
	// dir is the checkout's root, which every path the steps name is under, and the folder
	// the tools run in: "" for the working directory, the repository's root as magex runs a
	// target.
	dir string

	// tmp is the folder the steps make their scratch folders in: "" for the system's.
	tmp string

	// stdout takes what the steps report and the output of the tools they stream, and
	// stderr those tools' errors; stdin is what the tools read, nothing when nil.
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader

	// getenv reads the environment: os.Getenv for a target.
	getenv func(key string) string

	// lookPath finds a tool by its name: exec.LookPath for a target, which reads PATH.
	lookPath func(file string) (string, error)

	// command makes the command that runs the tool name, found as lookPath finds it, with
	// args: exec.CommandContext for a target. An environment it sets on the command is the
	// one the tool starts with, under what a step adds.
	command func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// hostRunner is this process's runner: its working directory and temporary folder, its
// standard streams, its environment, and the tools on its PATH.
func hostRunner() runner {
	return runner{
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		stdin:    os.Stdin,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		command:  exec.CommandContext,
	}
}

// path returns the path p names: p itself when it is absolute, else p, slash-separated,
// under the checkout. With dir "", that is p as the working directory reads it.
func (r runner) path(p string) string {
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) {
		return p
	}

	return filepath.Join(r.dir, p)
}

// root is the checkout's root as a folder's path: "." for the working directory.
func (r runner) root() string {
	return cmp.Or(r.dir, ".")
}

// tempDir is the folder the steps make their scratch folders in.
func (r runner) tempDir() string {
	return cmp.Or(r.tmp, os.TempDir())
}

// workDir returns the checkout's root as an absolute path.
func (r runner) workDir() (string, error) {
	if r.dir != "" {
		return filepath.Abs(r.dir)
	}

	return os.Getwd()
}

// cmd returns the command that runs the tool name with args in the checkout, with env,
// key=value entries, added to the environment it starts with. The caller sets its
// streams.
func (r runner) cmd(ctx context.Context, env []string, name string, args ...string) *exec.Cmd {
	cmd := r.command(ctx, name, args...)
	cmd.Dir = r.dir

	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}

	return cmd
}

// run runs the tool name with args as sh.Run does: its output shows only when mage runs
// verbose, its errors always.
func (r runner) run(name string, args ...string) error {
	var out io.Writer
	if mg.Verbose() {
		out = r.stdout
	}

	return r.exec(nil, out, name, args...)
}

// runV runs the tool name with args as sh.RunV does, streaming its output.
func (r runner) runV(name string, args ...string) error {
	return r.exec(nil, r.stdout, name, args...)
}

// runWithV runs the tool name with args as sh.RunWithV does, streaming its output, with
// env, key=value entries, added to its environment.
func (r runner) runWithV(env []string, name string, args ...string) error {
	return r.exec(env, r.stdout, name, args...)
}

// output runs the tool name with args as sh.Output does, and returns what it wrote to
// standard output, without its last newline.
func (r runner) output(name string, args ...string) (string, error) {
	var buf bytes.Buffer

	err := r.exec(nil, &buf, name, args...)

	return strings.TrimSuffix(buf.String(), "\n"), err
}

// exec runs the tool name with args, with env added to its environment, writing its
// output to stdout and its errors to the runner's, and fails as sh.Exec does: with the
// tool's exit code, which mage exits with, when it ran and failed, and saying it could not
// run otherwise. The arguments reach the tool as they are.
func (r runner) exec(env []string, stdout io.Writer, name string, args ...string) error {
	cmd := r.cmd(context.Background(), env, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = r.stderr
	cmd.Stdin = r.stdin

	if mg.Verbose() {
		quoted := make([]string, 0, len(args))
		for _, arg := range args {
			quoted = append(quoted, fmt.Sprintf("%q", arg))
		}

		log.Println("exec:", name, strings.Join(quoted, " "))
	}

	err := cmd.Run()

	switch {
	case err == nil:
		return nil
	case sh.CmdRan(err):
		code := sh.ExitStatus(err)

		return mg.Fatalf(code, `running "%s %s" failed with exit code %d`, name, strings.Join(args, " "), code)
	default:
		return fmt.Errorf(`failed to run "%s %s: %w"`, name, strings.Join(args, " "), err)
	}
}
