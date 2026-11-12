//go:build unix

package execpath_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// processWait bounds how long a test waits for a process to start or end.
const processWait = 10 * time.Second

// fastLadder stops a group as StopLadder does, with short waits.
func fastLadder() execpath.Ladder {
	return execpath.Ladder{
		{Signal: syscall.SIGINT, Wait: 200 * time.Millisecond},
		{Signal: syscall.SIGTERM, Wait: 200 * time.Millisecond},
		{Signal: syscall.SIGKILL},
	}
}

// startFake starts a fake tool playing run in its own group, writing into a log file it
// returns the path of.
func startFake(t *testing.T, ctx context.Context, run testutil.FakeRun) (*execpath.Proc, string) {
	t.Helper()

	path, env := testutil.FakeTool(t, "tool", run)
	logPath := filepath.Join(t.TempDir(), "run.log")

	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // the test's own temp file
	require.NoError(t, err)

	t.Cleanup(func() { _ = log.Close() })

	proc, err := execpath.Start(ctx, execpath.Cmd{Path: path, Env: append(os.Environ(), env), Output: log})
	require.NoError(t, err)

	return proc, logPath
}

// grandchild returns a grandchild script that records its PID in a fresh file.
func grandchild(t *testing.T, setsid bool) *testutil.FakeGrandchild {
	t.Helper()

	return &testutil.FakeGrandchild{PIDFile: filepath.Join(t.TempDir(), "grandchild.pid"), Sleep: time.Minute, Setsid: setsid}
}

// waitPID waits for a grandchild to record its PID, and kills it when the test ends.
func waitPID(t *testing.T, file string) int {
	t.Helper()

	var pid int

	require.Eventually(t, func() bool {
		body, err := os.ReadFile(file) //nolint:gosec // the test's own temp file
		if err != nil {
			return false
		}

		pid, err = strconv.Atoi(strings.TrimSpace(string(body)))

		return err == nil && pid > 0
	}, processWait, 10*time.Millisecond)

	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	return pid
}

// alive reports whether a process with pid runs.
func alive(pid int) bool {
	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}

func TestRunReturnsWhenAGrandchildHoldsStdout(t *testing.T) {
	t.Parallel()

	g := grandchild(t, false)
	path, env := testutil.FakeTool(t, "tool", testutil.FakeRun{Stdout: "done\n", Grandchild: g})

	start := time.Now()
	stdout, _, err := execpath.Exec{Env: append(os.Environ(), env)}.Run(t.Context(), path)

	require.NoError(t, err, "the program exited 0; its grandchild holding stdout isn't a failure")
	assert.Equal(t, "done\n", string(stdout))
	assert.Less(t, time.Since(start), processWait, "Run returned soon after the program, not after the grandchild")
	assert.True(t, alive(waitPID(t, g.PIDFile)), "the grandchild still runs; Run didn't wait for it")
}

func TestGroupStopEndsGrandchildren(t *testing.T) {
	t.Parallel()

	g := grandchild(t, false)
	proc, _ := startFake(t, t.Context(), testutil.FakeRun{Sleep: time.Minute, Grandchild: g})
	pid := waitPID(t, g.PIDFile)

	proc.Stop(fastLadder())

	code, err := proc.Wait()
	require.Error(t, err)
	assert.Equal(t, 128+int(syscall.SIGINT), code, "SIGINT ended the program")
	assert.Eventually(t, func() bool { return !alive(pid) }, processWait, 10*time.Millisecond, "the grandchild ended with its group")
}

func TestGroupEscalatesWhenSignalsAreIgnored(t *testing.T) {
	t.Parallel()

	proc, _ := startFake(t, t.Context(), testutil.FakeRun{Sleep: time.Minute, IgnoreSignals: true})

	// Let the fake start ignoring signals before the first one is sent.
	time.Sleep(200 * time.Millisecond)

	proc.Stop(fastLadder())

	code, err := proc.Wait()
	require.Error(t, err)
	assert.Equal(t, 128+int(syscall.SIGKILL), code, "only SIGKILL ended a program that ignores SIGINT and SIGTERM")
}

func TestGroupTimeoutKillsTheGroup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	g := grandchild(t, false)
	proc, _ := startFake(t, ctx, testutil.FakeRun{Sleep: time.Minute, Grandchild: g})
	pid := waitPID(t, g.PIDFile)

	code, err := proc.Wait()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 128+int(syscall.SIGKILL), code)
	assert.Eventually(t, func() bool { return !alive(pid) }, processWait, 10*time.Millisecond, "the whole group was killed")
}

func TestStdinIsDevNull(t *testing.T) {
	t.Parallel()

	proc, logPath := startFake(t, t.Context(), testutil.FakeRun{ReportStdin: true})

	code, err := proc.Wait()
	require.NoError(t, err)
	assert.Zero(t, code)

	out, err := os.ReadFile(logPath) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "stdin: 0 bytes, devnull: true\n", string(out))
}

func TestChildSeesOnlyStdio(t *testing.T) {
	t.Parallel()

	run := testutil.FakeRun{ReportFDs: true, Stderr: "to the same log\n"}

	// The baseline is the same fake started by Go's own exec with nothing extra passed: it
	// holds stdio, whatever its own runtime opens (the race runtime does on Linux), and what
	// this test process hands every child (a CI runner leaves some open). Start must add
	// nothing to that.
	path, env := testutil.FakeTool(t, "tool", run)

	var baseline bytes.Buffer

	plain := exec.CommandContext(t.Context(), path) //nolint:gosec // the test's own fake tool

	plain.Env = append(os.Environ(), env)
	plain.Stdout = &baseline
	require.NoError(t, plain.Run())
	require.True(t, strings.HasPrefix(baseline.String(), "fds: 0,1,2"), baseline.String())

	proc, logPath := startFake(t, t.Context(), run)

	_, err := proc.Wait()
	require.NoError(t, err)

	out, err := os.ReadFile(logPath) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, baseline.String()+"to the same log\n", string(out),
		"the child holds what any child does and nothing more, and stdout and stderr share the log in order")
}

func TestSetsidDescendantSurvives(t *testing.T) {
	t.Parallel()

	g := grandchild(t, true)
	proc, _ := startFake(t, t.Context(), testutil.FakeRun{Sleep: time.Minute, Grandchild: g})
	pid := waitPID(t, g.PIDFile)

	proc.Stop(fastLadder())

	_, err := proc.Wait()
	require.Error(t, err)

	// A descendant that starts its own session, as an agent does, leaves the group, so
	// stopping the group doesn't reach it. This pins that limit.
	time.Sleep(300 * time.Millisecond)
	assert.True(t, alive(pid), "a setsid descendant outlives its group")
}

func TestGroupHangsUpWhatTheProgramLeftRunning(t *testing.T) {
	t.Parallel()

	left := grandchild(t, false)
	proc, _ := startFake(t, t.Context(), testutil.FakeRun{Sleep: 300 * time.Millisecond, Grandchild: left})
	pid := waitPID(t, left.PIDFile)

	code, err := proc.Wait()
	require.NoError(t, err)
	assert.Zero(t, code)
	assert.Eventually(t, func() bool { return !alive(pid) }, processWait, 10*time.Millisecond,
		"a background job the program left in its group is hung up when the program exits")
}

func TestGroupHangUpSparesWhatIgnoresIt(t *testing.T) {
	t.Parallel()

	kept := grandchild(t, false)
	kept.IgnoreSignals = true

	proc, _ := startFake(t, t.Context(), testutil.FakeRun{Sleep: 300 * time.Millisecond, Grandchild: kept})
	pid := waitPID(t, kept.PIDFile)

	_, err := proc.Wait()
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)
	assert.True(t, alive(pid), "a job started to survive a hang-up (nohup) survives the program, as with a closed terminal")
}

func TestGroupReportsExitCodes(t *testing.T) {
	t.Parallel()

	proc, _ := startFake(t, t.Context(), testutil.FakeRun{Exit: 3})

	code, err := proc.Wait()
	require.Error(t, err)
	assert.Equal(t, 3, code)
	assert.Equal(t, 3, execpath.ExitCode(err))

	again, _ := proc.Wait()
	assert.Equal(t, 3, again, "every Wait returns the same answer")

	select {
	case <-proc.Done():
	default:
		require.FailNow(t, "Done is closed once the program exited")
	}

	assert.Equal(t, proc.Pid(), proc.Pgid(), "the program leads its own group")
}

func TestGroupStopAfterExitDoesNothing(t *testing.T) {
	t.Parallel()

	proc, _ := startFake(t, t.Context(), testutil.FakeRun{})

	code, err := proc.Wait()
	require.NoError(t, err)

	proc.Stop(fastLadder())
	proc.Stop(fastLadder())

	assert.Zero(t, code)
	require.Error(t, proc.Signal(syscall.SIGTERM), "nothing is left in the group to signal")
}

func TestStartRefusesARelativePath(t *testing.T) {
	t.Parallel()

	_, err := execpath.Start(t.Context(), execpath.Cmd{Path: "tool"})
	require.Error(t, err)
}

func TestStartReportsAProgramThatCannotRun(t *testing.T) {
	t.Parallel()

	_, err := execpath.Start(t.Context(), execpath.Cmd{Path: filepath.Join(t.TempDir(), "missing")})
	require.Error(t, err)
}

func TestStartWithoutOutputDiscardsIt(t *testing.T) {
	t.Parallel()

	path, env := testutil.FakeTool(t, "tool", testutil.FakeRun{Stdout: "discarded\n"})

	proc, err := execpath.Start(t.Context(), execpath.Cmd{Path: path, Env: append(os.Environ(), env), Dir: t.TempDir()})
	require.NoError(t, err)

	code, err := proc.Wait()
	require.NoError(t, err)
	assert.Zero(t, code)
}

func TestStopLadderEndsWithKill(t *testing.T) {
	t.Parallel()

	ladder := execpath.StopLadder()
	require.Len(t, ladder, 3)
	assert.Equal(t, syscall.SIGINT, ladder[0].Signal)
	assert.Equal(t, 5*time.Second, ladder[0].Wait)
	assert.Equal(t, syscall.SIGTERM, ladder[1].Signal)
	assert.Equal(t, 10*time.Second, ladder[1].Wait)
	assert.Equal(t, syscall.SIGKILL, ladder[2].Signal)
}

func TestLadderClimbStopsWhenTheTargetIsGone(t *testing.T) {
	t.Parallel()

	ladder := execpath.Ladder{{Signal: syscall.SIGINT}, {Signal: syscall.SIGTERM}, {Signal: syscall.SIGKILL}}

	var sent []syscall.Signal

	record := func(sig syscall.Signal) error {
		sent = append(sent, sig)

		return nil
	}

	ladder.Climb(nil, nil, record)
	assert.Equal(t, []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL}, sent, "every step, in turn")

	sent = nil

	ladder.Climb(nil, func() bool { return len(sent) < 1 }, record)
	assert.Equal(t, []syscall.Signal{syscall.SIGINT}, sent, "nothing once alive says the target is gone")

	done := make(chan struct{})
	close(done)

	sent = nil

	ladder.Climb(done, nil, record)
	assert.Empty(t, sent, "nothing once the target has ended")
}
