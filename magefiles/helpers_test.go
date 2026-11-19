//go:build mage

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// answer is what a fake tool does for one call, given the arguments it was called with:
// it makes any folder the tool would make, which a FakeRun can't, and returns the run the
// fake plays, its output, the files it writes, and its exit status. It runs in the test,
// just before the fake starts.
type answer func(args []string) testutil.FakeRun

// toolCall is one call of a fake tool: the name the step ran it by (a path, for a tool
// run by its path) and its arguments.
type toolCall struct {
	name string
	args []string
}

// line is the call as one line, the tool's name first: "go build -o bin/app".
func (c toolCall) line() string {
	return strings.Join(append([]string{filepath.Base(c.name)}, c.args...), " ")
}

// lockedBuffer is a buffer the steps and the tools they stream can write at once.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write appends p to the buffer.
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

// String returns what was written so far.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// rig is what a test runs a target's steps in, through its runner: a checkout of its own
// in a temporary folder, an environment that holds only what the test sets, fake tools
// (testutil.FakeTool, played by this test binary), and buffers for what the steps print.
// Nothing the steps do reaches the process's working directory, environment, PATH, or
// standard streams, so the test runs in parallel with the others.
type rig struct {
	t   *testing.T
	dir string // the checkout

	out  lockedBuffer // what the steps and the tools print
	errs lockedBuffer // what the tools print as errors

	mu      sync.Mutex
	env     map[string]string // the environment the steps read
	answers map[string]answer // each fake tool, by its name
	links   map[string]string // where lookPath finds each fake tool
	calls   []toolCall        // every call of a fake tool, in order
}

// newRig returns a rig with a fresh, empty checkout, an empty environment, and no tools.
func newRig(t *testing.T) *rig {
	t.Helper()

	return &rig{
		t:       t,
		dir:     t.TempDir(),
		env:     map[string]string{},
		answers: map[string]answer{},
		links:   map[string]string{},
	}
}

// runner returns the runner the test hands the steps: the rig's checkout, a scratch
// folder of the test's own, its buffers, its environment, and its fake tools.
func (g *rig) runner() runner {
	return runner{
		dir:      g.dir,
		tmp:      g.t.TempDir(),
		stdout:   &g.out,
		stderr:   &g.errs,
		getenv:   g.getenv,
		lookPath: g.lookPath,
		command:  g.command,
	}
}

// path returns rel, slash-separated, in the rig's checkout.
func (g *rig) path(rel string) string {
	return filepath.Join(g.dir, filepath.FromSlash(rel))
}

// getenv reads the rig's environment.
func (g *rig) getenv(key string) string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.env[key]
}

// setenv sets key in the rig's environment.
func (g *rig) setenv(key, value string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.env[key] = value
}

// tool makes name a fake tool on the rig's PATH that answers each call with a, in place of
// any fake of that name before it.
func (g *rig) tool(name string, a answer) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.answers[name] = a
}

// fixed makes name a fake tool that plays run on every call.
func (g *rig) fixed(name string, run testutil.FakeRun) {
	g.tool(name, func([]string) testutil.FakeRun { return run })
}

// lookPath finds a fake tool as exec.LookPath finds a real one on PATH: a file named as
// the tool, or an error for a tool the rig has no fake of.
func (g *rig) lookPath(file string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	name := filepath.Base(file)
	if _, ok := g.answers[name]; !ok {
		return "", &exec.Error{Name: file, Err: exec.ErrNotFound}
	}

	if link, ok := g.links[name]; ok {
		return link, nil
	}

	link, _ := testutil.FakeTool(g.t, name, testutil.FakeRun{})
	g.links[name] = link

	return link, nil
}

// command makes the command for one call of the tool name, by its name or its path: one
// that plays what the tool's answer returns for args, or, for a tool the rig has no fake
// of, one that fails to start, as a tool missing from PATH does. It records the call.
func (g *rig) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	g.mu.Lock()
	a, ok := g.answers[filepath.Base(name)]

	if ok {
		g.calls = append(g.calls, toolCall{name: name, args: slices.Clone(args)})
	}
	g.mu.Unlock()

	if !ok {
		return &exec.Cmd{Path: name, Args: append([]string{name}, args...), Err: &exec.Error{Name: name, Err: exec.ErrNotFound}}
	}

	path, entry := testutil.FakeTool(g.t, filepath.Base(name), a(args))

	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // the fake this test made, run as the step named the tool

	cmd.Env = append(fakeEnv(), entry)

	return cmd
}

// toolCalls returns every call of a fake tool so far, in order.
func (g *rig) toolCalls() []toolCall {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.calls)
}

// lines returns every call of a fake tool so far, a line each.
func (g *rig) lines() []string {
	calls := g.toolCalls()
	lines := make([]string, 0, len(calls))

	for _, c := range calls {
		lines = append(lines, c.line())
	}

	return lines
}

// linesOf returns the lines of the calls of the tool name alone, without its name.
func (g *rig) linesOf(name string) []string {
	var lines []string

	for _, c := range g.toolCalls() {
		if filepath.Base(c.name) == name {
			lines = append(lines, strings.Join(c.args, " "))
		}
	}

	return lines
}

// fakeEnv is the environment a fake tool starts with: the test's, but GOARCH, which a
// step sets only for a build for another processor, so a fake reports only what the step
// gave it, and with the race detector's pause at exit off. A test binary built with -race
// sleeps atexit_sleep_ms, a second unless GORACE says otherwise, before it exits with
// status 0, which every fake that succeeds would otherwise spend.
func fakeEnv() []string {
	env := slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "GOARCH=") || strings.HasPrefix(entry, "GORACE=")
	})

	return append(env, "GORACE="+strings.TrimSpace(os.Getenv("GORACE")+" atexit_sleep_ms=0"))
}

// argAfter returns the argument after flag in args, or "" when there is none.
func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}

	return ""
}

// lastArg returns the last of args, or "" when there are none.
func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[len(args)-1]
}

// exits is an answer that exits with code and does nothing else.
func exits(code int) answer {
	return func([]string) testutil.FakeRun { return testutil.FakeRun{Exit: code} }
}

// writes is the run of a tool that writes content to the file at path.
func writes(path, content string) testutil.FakeRun {
	return testutil.FakeRun{Files: []testutil.FakeFile{{Path: path, Content: content}}}
}

// mkdir makes the folder at path, and its parents, as a tool about to write into it would.
func mkdir(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(path, 0o750))
}

// realRunner returns a runner whose checkout is dir and whose tools are the real ones on
// PATH, started with env as their whole environment, or the test's own when env is nil.
// The steps read an empty environment, and print to the buffer it returns.
func realRunner(t *testing.T, dir string, env []string) (runner, *lockedBuffer) {
	t.Helper()

	out := &lockedBuffer{}

	return runner{
		dir:      dir,
		tmp:      t.TempDir(),
		stdout:   out,
		stderr:   out,
		getenv:   func(string) string { return "" },
		lookPath: exec.LookPath,
		command: func(ctx context.Context, name string, args ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the real tool a step names, as a target runs it

			cmd.Env = env

			return cmd
		},
	}, out
}

// repoRoot returns the repository's root, the folder magex runs the targets from.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs("..")
	require.NoError(t, err)

	return root
}

// gitIn runs git with args in dir with env as its whole environment, and returns its
// trimmed output, failing the test when git fails.
func gitIn(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // test fixture setup with fixed arguments
	cmd.Dir = dir
	cmd.Env = env

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)

	return strings.TrimSpace(string(out))
}

// readFile returns the content of path, failing the test when it cannot be read.
func readFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // a path the test itself created
	require.NoError(t, err)

	return string(body)
}

// startProcess starts cmd, a process of the test's own, and returns its ID and a channel
// that is closed once it has exited. The test kills and reaps it when it ends.
func startProcess(t *testing.T, cmd *exec.Cmd) (int, <-chan struct{}) {
	t.Helper()

	require.NoError(t, cmd.Start())

	done := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(done)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()

		<-done
	})

	return cmd.Process.Pid, done
}

// startSleeper starts a process of the test's own that sleeps until it is stopped, a fake
// tool, and returns its ID. The test reaps it when it ends.
func startSleeper(t *testing.T) int {
	t.Helper()

	path, entry := testutil.FakeTool(t, "sleeper", testutil.FakeRun{Sleep: time.Minute})

	cmd := exec.CommandContext(t.Context(), path) //nolint:gosec // the test's own fake

	cmd.Env = append(os.Environ(), entry)

	pid, _ := startProcess(t, cmd)

	return pid
}

// skipAsRoot skips a test that relies on file permissions, which root ignores.
func skipAsRoot(t *testing.T) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

// requireTool skips the test when name is not on PATH, and returns its path.
func requireTool(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name + " is not installed")
	}

	return path
}

// skipOnMacOS skips a test of the guard that stops macOS-only targets elsewhere.
func skipOnMacOS(t *testing.T) {
	t.Helper()

	if runtime.GOOS == goosMacOS {
		t.Skip("covers the refusal on other systems")
	}
}

// onlyOnMacOS skips a test that needs macOS.
func onlyOnMacOS(t *testing.T) {
	t.Helper()

	if runtime.GOOS != goosMacOS {
		t.Skip("needs macOS")
	}
}
