package measure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var (
	errProbe      = errors.New("probe failed")
	errTerminal   = errors.New("terminal failed")
	errExecFailed = errors.New("exit status 1")
)

// fakeProbe answers process queries from scripted lists.
type fakeProbe struct {
	mu        sync.Mutex
	pids      [][]int // returned in turn; the last repeats
	cwds      map[int]string
	pidsErr   error
	pidsErrAt int
	cwdsErr   error
	stopErr   error
	calls     int
	stopped   []int
}

func (p *fakeProbe) PIDs(context.Context, string) ([]int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.calls++
	if p.pidsErr != nil && p.calls >= p.pidsErrAt {
		return nil, p.pidsErr
	}

	return p.pids[min(p.calls, len(p.pids))-1], nil
}

func (p *fakeProbe) Cwds(context.Context, []int) (map[int]string, error) {
	return p.cwds, p.cwdsErr
}

func (p *fakeProbe) Stop(_ context.Context, pid int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stopped = append(p.stopped, pid)

	return p.stopErr
}

func newTimer(probe agentProbe, clone string, launch func(context.Context) (launcher.LaunchResult, error)) launchTimer {
	clock := testutil.NewSteppingClock(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), time.Second)

	return launchTimer{
		launch:  launch,
		probe:   probe,
		agent:   "/opt/agent/claude",
		clone:   clone,
		timeout: 200 * time.Millisecond,
		poll:    5 * time.Millisecond,
		now:     clock.Now,
	}
}

func launched(context.Context) (launcher.LaunchResult, error) {
	return launcher.LaunchResult{Terminal: "iterm2", Permission: "plan mode (outside contributor)"}, nil
}

func TestLaunchTimerFindsTheAgent(t *testing.T) {
	t.Parallel()

	clone := testutil.RealTempDir(t)

	tests := []struct {
		name string
		cwd  string
	}{
		{name: "in the clone", cwd: clone},
		{name: "in an agent worktree", cwd: filepath.Join(clone, ".claude", "worktrees", "pr-1")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := &fakeProbe{
				pids: [][]int{{10}, {10}, {10, 20, 30}},
				cwds: map[int]string{10: clone, 20: tt.cwd, 30: t.TempDir()},
			}

			report, err := newTimer(probe, clone, launched).run(t.Context())
			require.NoError(t, err)

			assert.Equal(t, 20, report.PID, "the process that ran before the launch is ignored")
			assert.Equal(t, time.Second, report.Elapsed)
			assert.Equal(t, []int{20}, probe.stopped)
			assert.Equal(t, []string{
				"launch.ms=1000 s=1.00",
				"launch.terminal=iterm2",
				"launch.permission=plan mode (outside contributor)",
				"launch.pid=20 (stopped)",
			}, report.Lines())
		})
	}
}

func TestLaunchTimerAutomation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "refused", err: fmt.Errorf("%w (iTerm2)", terminal.ErrAutomationDenied)},
		{name: "dialog never answered", err: fmt.Errorf("%w iTerm2: %w", terminal.ErrTerminalOpen, context.DeadlineExceeded)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			probe := &fakeProbe{pids: [][]int{{}}}
			launch := func(context.Context) (launcher.LaunchResult, error) { return launcher.LaunchResult{}, tt.err }

			report, err := newTimer(probe, t.TempDir(), launch).run(t.Context())
			require.NoError(t, err)

			assert.True(t, report.NeedsAutomation)
			assert.Equal(t, []string{"launch.needs_automation_permission=true", "launch.reason=" + tt.err.Error()}, report.Lines())
			assert.Empty(t, probe.stopped)
		})
	}
}

func TestLaunchTimerErrors(t *testing.T) {
	t.Parallel()

	clone := testutil.RealTempDir(t)

	t.Run("launch fails", func(t *testing.T) {
		t.Parallel()

		launch := func(context.Context) (launcher.LaunchResult, error) {
			return launcher.LaunchResult{}, fmt.Errorf("%w iTerm2: %w", terminal.ErrTerminalOpen, errTerminal)
		}

		_, err := newTimer(&fakeProbe{pids: [][]int{{}}}, clone, launch).run(t.Context())
		require.ErrorIs(t, err, errTerminal)
	})

	t.Run("agent never starts", func(t *testing.T) {
		t.Parallel()

		probe := &fakeProbe{pids: [][]int{{}, {40}}, cwds: map[int]string{40: t.TempDir()}}

		_, err := newTimer(probe, clone, launched).run(t.Context())
		require.ErrorIs(t, err, ErrAgentNotStarted)
	})

	t.Run("process list fails before the launch", func(t *testing.T) {
		t.Parallel()

		_, err := newTimer(&fakeProbe{pidsErr: errProbe, pidsErrAt: 1}, clone, launched).run(t.Context())
		require.ErrorIs(t, err, errProbe)
	})

	t.Run("process list fails while waiting", func(t *testing.T) {
		t.Parallel()

		probe := &fakeProbe{pids: [][]int{{}}, pidsErr: errProbe, pidsErrAt: 2}

		_, err := newTimer(probe, clone, launched).run(t.Context())
		require.ErrorIs(t, err, errProbe)
	})

	t.Run("working directories fail", func(t *testing.T) {
		t.Parallel()

		probe := &fakeProbe{pids: [][]int{{}, {50}}, cwdsErr: errProbe}

		_, err := newTimer(probe, clone, launched).run(t.Context())
		require.ErrorIs(t, err, errProbe)
	})

	t.Run("stopping the agent fails", func(t *testing.T) {
		t.Parallel()

		probe := &fakeProbe{pids: [][]int{{}, {60}}, cwds: map[int]string{60: clone}, stopErr: errProbe}

		report, err := newTimer(probe, clone, launched).run(t.Context())
		require.ErrorIs(t, err, errProbe)
		assert.Equal(t, 60, report.PID, "the measurement is still reported")
	})
}

func TestInClone(t *testing.T) {
	t.Parallel()

	clone := testutil.RealTempDir(t)

	assert.True(t, inClone(clone, clone))
	assert.True(t, inClone(filepath.Join(clone, ".claude", "worktrees", "issue-3"), clone))
	assert.False(t, inClone(filepath.Join(clone, "sub"), clone))
	assert.False(t, inClone(clone+"-other", clone))
	assert.False(t, inClone("", clone))
}

func TestParsePS(t *testing.T) {
	t.Parallel()

	agent := "/opt/agent/claude"
	out := strings.Join([]string{
		"    1 /sbin/launchd",
		"  101 /opt/agent/claude",
		"  102 /opt/agent/claude Review the pull request",
		"second line of a prompt argument",
		"  103 /opt/agent/claude-helper --serve",
		"  104 /bin/sh -c /opt/agent/claude",
		"  abc /opt/agent/claude",
		"",
	}, "\n")

	assert.Equal(t, []int{101, 102}, parsePS(out, agent))
	assert.Empty(t, parsePS("", agent))
}

func TestParseLsofCwds(t *testing.T) {
	t.Parallel()

	out := "p101\nfcwd\nn/tmp/one\np102\nfcwd\nn/tmp/two\nn/tmp/ignored-second-name\n"

	assert.Equal(t, map[int]string{101: "/tmp/one", 102: "/tmp/two"}, parseLsofCwds(out))
	assert.Equal(t, map[int]string{}, parseLsofCwds("nno-process\n"))
}

// answers maps a program's base name to its scripted answer, given the arguments.
type answers map[string]func(args []string) ([]byte, []byte, error)

// scripted returns a runner that answers each program from a by its base name and fails
// any other with errNotRun.
func scripted(a answers) *testutil.Runner {
	return &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		if answer, ok := a[filepath.Base(argv[0])]; ok {
			return answer(argv[1:])
		}

		return nil, nil, errNotRun
	}}
}

// ran returns every call r recorded, as the program's base name and its arguments.
func ran(r *testutil.Runner) []string {
	calls := r.Calls()
	out := make([]string, 0, len(calls))

	for _, argv := range calls {
		out = append(out, filepath.Base(argv[0])+" "+strings.Join(argv[1:], " "))
	}

	return out
}

// exitError returns a real *exec.ExitError.
func exitError(t *testing.T) error {
	t.Helper()

	err := exec.CommandContext(t.Context(), "sh", "-c", "exit 1").Run()
	require.Error(t, err)

	return err
}

func TestProcessProbe(t *testing.T) {
	t.Parallel()

	runner := scripted(answers{
		"ps":   func([]string) ([]byte, []byte, error) { return []byte(" 7 /opt/agent/claude go\n"), nil, nil },
		"lsof": func([]string) ([]byte, []byte, error) { return []byte("p7\nfcwd\nn/tmp/clone\n"), nil, exitError(t) },
		"kill": func([]string) ([]byte, []byte, error) { return nil, nil, nil },
	})
	probe := processProbe{run: runner}

	pids, err := probe.PIDs(t.Context(), "/opt/agent/claude")
	require.NoError(t, err)
	assert.Equal(t, []int{7}, pids)

	cwds, err := probe.Cwds(t.Context(), []int{7, 8})
	require.NoError(t, err, "lsof's non-zero exit for a vanished process is not a failure")
	assert.Equal(t, map[int]string{7: "/tmp/clone"}, cwds)

	require.NoError(t, probe.Stop(t.Context(), 7))
	assert.Contains(t, ran(runner), "lsof -a -p 7,8 -d cwd -Fn")
	assert.Contains(t, ran(runner), "kill -TERM 7")
}

func TestProcessProbeErrors(t *testing.T) {
	t.Parallel()

	probe := processProbe{run: scripted(nil)}

	_, err := probe.PIDs(t.Context(), "/opt/agent/claude")
	require.ErrorIs(t, err, errNotRun)

	_, err = probe.Cwds(t.Context(), []int{1})
	require.ErrorIs(t, err, errNotRun)

	require.ErrorIs(t, probe.Stop(t.Context(), 1), errNotRun)
}

func TestDeadlineRunner(t *testing.T) {
	t.Parallel()

	inner := scripted(answers{
		"osascript": func([]string) ([]byte, []byte, error) { return nil, nil, errExecFailed },
	})
	runner := deadlineRunner{inner: inner}

	_, _, err := runner.Run(t.Context(), "osascript")
	require.ErrorIs(t, err, errExecFailed)
	require.NotErrorIs(t, err, context.DeadlineExceeded)

	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()

	<-ctx.Done()

	_, _, err = runner.Run(ctx, "osascript")
	require.ErrorIs(t, err, errExecFailed)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// fakeAgent puts an executable named claude first on PATH and returns its path.
func fakeAgent(t *testing.T) string {
	t.Helper()

	bin := t.TempDir()
	agent := filepath.Join(bin, agentBin)
	require.NoError(t, os.WriteFile(agent, []byte("#!/bin/sh\n"), 0o700)) //nolint:gosec // a test executable

	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	return agent
}

// launchRunner runs git for real, isolated from the user's and the system's configuration
// through its own environment, and scripts the terminal and the process tools: the agent
// appears in the clone once the terminal was asked to open the launch script.
func launchRunner(t *testing.T, agent string, osascript func() ([]byte, []byte, error)) *testutil.Runner {
	t.Helper()

	var (
		mu     sync.Mutex
		clone  string
		opened bool
	)

	git := execpath.Exec{Env: testutil.Git(t)}

	return scripted(answers{
		"git": func(args []string) ([]byte, []byte, error) {
			if len(args) > 2 && args[0] == "-C" && args[2] == "init" {
				mu.Lock()
				clone = args[1]
				mu.Unlock()
			}

			return git.Run(t.Context(), "git", args...)
		},
		"osascript": func([]string) ([]byte, []byte, error) {
			stdout, stderr, err := osascript()

			mu.Lock()
			opened = err == nil
			mu.Unlock()

			return stdout, stderr, err
		},
		"ps": func([]string) ([]byte, []byte, error) {
			mu.Lock()
			defer mu.Unlock()

			if !opened {
				return []byte("    1 /sbin/launchd\n"), nil, nil
			}

			return fmt.Appendf(nil, "    1 /sbin/launchd\n 4242 %s 'prompt' --permission-mode plan\n", agent), nil, nil
		},
		"lsof": func([]string) ([]byte, []byte, error) {
			mu.Lock()
			defer mu.Unlock()

			return []byte("p4242\nfcwd\nn" + clone + "\n"), nil, nil
		},
		"kill": func([]string) ([]byte, []byte, error) { return nil, nil, nil },
	})
}

//nolint:paralleltest // fakeAgent puts the agent on the process's PATH, where the launch looks it up
func TestRunLaunch(t *testing.T) {
	agent := fakeAgent(t)
	tmp := t.TempDir()
	runner := launchRunner(t, agent, func() ([]byte, []byte, error) { return nil, nil, nil })

	report, err := RunLaunch(t.Context(), LaunchOptions{
		Home: t.TempDir(), Version: "test", TempDir: tmp, Runner: runner,
		AgentTimeout: 2 * time.Second, Poll: 5 * time.Millisecond,
	})

	entries, readErr := os.ReadDir(tmp)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "the scratch clone and data directory are deleted")

	if runtime.GOOS != "darwin" {
		require.ErrorIs(t, err, terminal.ErrUnsupportedOS)

		return
	}

	require.NoError(t, err)
	assert.Equal(t, 4242, report.PID)
	assert.Equal(t, terminal.ITerm2, report.Terminal)
	assert.Contains(t, report.Permission, "plan", "an outside contributor's pull request launches in plan mode")
	assert.Positive(t, report.Elapsed)
	assert.Contains(t, ran(runner), "kill -TERM 4242")
}

//nolint:paralleltest // fakeAgent puts the agent on the process's PATH, where the launch looks it up
func TestRunLaunchAutomationDenied(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("terminal drivers exist only on macOS")
	}

	agent := fakeAgent(t)
	runner := launchRunner(t, agent, func() ([]byte, []byte, error) {
		return nil, []byte("execution error: Not authorized to send Apple events to iTerm. (-1743)"), errExecFailed
	})

	report, err := RunLaunch(t.Context(), LaunchOptions{Home: t.TempDir(), TempDir: t.TempDir(), Runner: runner})
	require.NoError(t, err)
	assert.True(t, report.NeedsAutomation)
}

func TestRunLaunchSetupErrors(t *testing.T) {
	t.Parallel()

	t.Run("scratch directory", func(t *testing.T) {
		t.Parallel()

		// With no runner the app's own is used; the missing scratch parent fails first.
		_, err := RunLaunch(t.Context(), LaunchOptions{Home: t.TempDir(), TempDir: "/nonexistent-scratch-parent"})
		require.Error(t, err)
	})

	t.Run("git fails", func(t *testing.T) {
		t.Parallel()

		_, err := RunLaunch(t.Context(), LaunchOptions{Home: t.TempDir(), TempDir: t.TempDir(), Runner: scripted(nil)})
		require.ErrorIs(t, err, errNotRun)
	})

	t.Run("no home directory", func(t *testing.T) {
		t.Parallel()

		runner := launchRunner(t, "/opt/agent/claude", func() ([]byte, []byte, error) { return nil, nil, nil })

		_, err := RunLaunch(t.Context(), LaunchOptions{TempDir: t.TempDir(), Runner: runner})
		require.Error(t, err)
	})
}

func TestRunLaunchWithoutAnAgent(t *testing.T) {
	// The launch looks the agent up on the process's search path, so only that can hide it.
	t.Setenv("PATH", filepath.Dir(testutil.RequireGit(t)))

	runner := launchRunner(t, "/opt/agent/claude", func() ([]byte, []byte, error) { return nil, nil, nil })

	_, err := RunLaunch(t.Context(), LaunchOptions{Home: t.TempDir(), TempDir: t.TempDir(), Runner: runner})
	require.ErrorIs(t, err, execpath.ErrNotFound)
}
