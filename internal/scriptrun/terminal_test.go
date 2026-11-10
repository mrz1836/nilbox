//go:build unix

package scriptrun

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// terminalHarness returns a harness whose runs in the terminal open in term.
func terminalHarness(t *testing.T, term TerminalOpener) *harness {
	t.Helper()

	return newHarness(t, func(cfg *Config) { cfg.Terminal = term })
}

// terminalSpec returns a run in the terminal of a fake tool that plays run.
func (h *harness) terminalSpec(run testutil.FakeRun) Spec {
	h.t.Helper()

	spec := h.spec(run)
	spec.RunsIn = RunsInTerminal
	spec.Env = spec.Env[len(spec.Env)-1:] // only what the script exports: the fake tool's own entry
	spec.Tool = "mage-x"

	return spec
}

func TestATerminalRunIsFollowedToItsExitFile(t *testing.T) {
	t.Parallel()

	term := newFakeTerminal(t)
	h := terminalHarness(t, term)
	out := &collector{}

	spec := h.terminalSpec(testutil.FakeRun{Stdout: "in the terminal\n", Sleep: 500 * time.Millisecond, Exit: 3})
	spec.Output = out

	id := h.start(spec)

	opened := term.runs()
	require.Len(t, opened, 1)
	assert.Equal(t, "run-1", opened[0].Name)
	assert.Equal(t, spec.Dir, opened[0].Dir)
	assert.Equal(t, spec.Argv, opened[0].Argv)
	assert.Equal(t, spec.Env, opened[0].Env)
	assert.Equal(t, h.e.files.log(id), opened[0].Log)
	assert.Equal(t, h.e.files.pid(id), opened[0].PIDFile)
	assert.Equal(t, h.e.files.exit(id), opened[0].ExitFile)

	holder, held := h.locks.Holder(spec.Dir)
	require.True(t, held, "the clone is held while the terminal runs it")
	assert.Equal(t, clonelock.KindScript, holder.Kind)
	assert.False(t, h.e.Active(), "a run in the terminal doesn't keep the app from quitting")
	assert.True(t, h.e.InUse("mage-x"), "but it uses its tool")

	require.Eventually(t, func() bool { return h.record(id).PID > 0 }, wait, poll, "the shell's PID is recorded")

	rec := h.record(id)
	assert.False(t, rec.ProcStartedAt.IsZero(), "with its start time")
	assert.Equal(t, StatusRunning, rec.Status)

	err := h.s.Stop(t.Context(), id)
	require.ErrorIs(t, err, ErrNoStop, "a run in the terminal stops there")

	rec = h.ended(id)
	assert.Equal(t, StatusFailed, rec.Status)
	require.NotNil(t, rec.ExitCode)
	assert.Equal(t, 3, *rec.ExitCode, "the exit file's status")
	assert.Equal(t, "in the terminal\n", out.String(), "its log was tailed into the reader")
	assert.Equal(t, int64(len("in the terminal\n")), rec.LogBytes)

	_, held = h.locks.Holder(spec.Dir)
	assert.False(t, held, "the clone is free once the exit file appeared")
	assert.False(t, h.e.InUse("mage-x"))
}

func TestATerminalRunThatSucceeds(t *testing.T) {
	t.Parallel()

	h := terminalHarness(t, newFakeTerminal(t))
	rec := h.ended(h.start(h.terminalSpec(testutil.FakeRun{Stdout: "ok\n"})))

	assert.Equal(t, StatusSucceeded, rec.Status)
	assert.Equal(t, RunsInTerminal, rec.RunsIn)
}

func TestATerminalClosedMidRunIsInterrupted(t *testing.T) {
	t.Parallel()

	term := newFakeTerminal(t)
	term.noExit = true

	h := terminalHarness(t, term)
	spec := h.terminalSpec(testutil.FakeRun{Stdout: "partial\n", Sleep: 300 * time.Millisecond})

	id := h.start(spec)
	rec := h.ended(id)

	assert.Equal(t, StatusInterrupted, rec.Status, "its shell is gone and it left no exit status")
	assert.Equal(t, terminalClosed, rec.Summary)
	assert.Nil(t, rec.ExitCode)

	_, held := h.locks.Holder(spec.Dir)
	assert.False(t, held, "the clone is free once the shell is gone")
}

func TestATerminalThatNeverStartsTheRun(t *testing.T) {
	t.Parallel()

	term := newFakeTerminal(t)
	term.nothing = true

	h := terminalHarness(t, term)
	h.e.pidWait = 300 * time.Millisecond

	rec := h.ended(h.start(h.terminalSpec(testutil.FakeRun{})))

	assert.Equal(t, StatusInterrupted, rec.Status)
	assert.Equal(t, terminalIdle, rec.Summary)
}

func TestATerminalThatCantOpenFailsTheRunCleanly(t *testing.T) {
	t.Parallel()

	term := newFakeTerminal(t)
	term.err = uierr.New("Runs in your terminal need macOS.", errUnsupported)

	h := terminalHarness(t, term)
	spec := h.terminalSpec(testutil.FakeRun{})

	id, err := h.e.Start(t.Context(), spec)
	require.ErrorIs(t, err, errUnsupported)
	assert.Positive(t, id)

	rec := h.ended(id)
	assert.Equal(t, StatusFailed, rec.Status)
	assert.Equal(t, "Runs in your terminal need macOS.", rec.Summary)

	_, held := h.locks.Holder(spec.Dir)
	assert.False(t, held)
}

func TestWithoutATerminalARunInItFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.terminalSpec(testutil.FakeRun{})

	id, err := h.e.Start(t.Context(), spec)
	require.ErrorIs(t, err, ErrNoTerminal)
	assert.Equal(t, "Runs in your terminal aren't available here.", uierr.Message(err))

	rec := h.ended(id)
	assert.Equal(t, StatusFailed, rec.Status)
	assert.Equal(t, "Runs in your terminal aren't available here.", rec.Summary)
}

func TestATerminalRunIsLeftRunningWhenTheAppQuits(t *testing.T) {
	t.Parallel()

	h := terminalHarness(t, newFakeTerminal(t))
	id := h.start(h.terminalSpec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	h.waitReady(id)

	h.e.StopAll()

	rec := h.record(id)
	assert.Equal(t, StatusRunning, rec.Status, "the next start follows it again")
	assert.True(t, strings.HasSuffix(h.logText(id), readyLine))
}

func TestATerminalRunClearsAForgottenRunsFiles(t *testing.T) {
	t.Parallel()

	h := terminalHarness(t, newFakeTerminal(t))
	require.NoError(t, h.e.files.ensureDir())
	testutil.WriteFile(t, h.e.files.exit(1), "0\n")
	testutil.WriteFile(t, h.e.files.pid(1), "1\n")

	id := h.start(h.terminalSpec(testutil.FakeRun{Stdout: "fresh\n", Exit: 5}))
	require.Equal(t, int64(1), id)

	rec := h.ended(id)
	require.NotNil(t, rec.ExitCode)
	assert.Equal(t, 5, *rec.ExitCode, "a stale exit file never ends a new run")
}
