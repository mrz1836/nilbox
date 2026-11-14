package measure

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// requested is when a measured launch in these tests was requested.
func requested() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

func TestLaunchStepsSplit(t *testing.T) {
	t.Parallel()

	t0 := requested()

	type mark struct {
		step step
		at   time.Duration
	}

	tests := []struct {
		name  string
		marks []mark
		found time.Duration
		want  LaunchSteps
	}{
		{
			name: "held back, with the token borrowed",
			marks: []mark{
				{stepTokenStart, 300 * time.Millisecond},
				{stepTokenEnd, 800 * time.Millisecond},
				{stepHeld, 1200 * time.Millisecond},
				{stepTerminalStart, 2100 * time.Millisecond},
				{stepTerminalEnd, 2900 * time.Millisecond},
			},
			found: 6900 * time.Millisecond,
			want: LaunchSteps{
				Known: true, Held: 1200 * time.Millisecond, Token: 500 * time.Millisecond,
				Confirmed: 900 * time.Millisecond, Terminal: 800 * time.Millisecond, Agent: 4 * time.Second,
			},
		},
		{
			name:  "not held, with the token held already",
			marks: []mark{{stepTerminalStart, time.Second}, {stepTerminalEnd, 1500 * time.Millisecond}},
			found: 3 * time.Second,
			want: LaunchSteps{
				Known: true, Confirmed: time.Second, Terminal: 500 * time.Millisecond, Agent: 1500 * time.Millisecond,
			},
		},
		{
			name: "a step noted twice keeps its first time",
			marks: []mark{
				{stepTerminalStart, time.Second},
				{stepTerminalEnd, 2 * time.Second},
				{stepTerminalStart, 3 * time.Second},
				{stepTerminalEnd, 4 * time.Second},
			},
			found: 5 * time.Second,
			want:  LaunchSteps{Known: true, Confirmed: time.Second, Terminal: time.Second, Agent: 3 * time.Second},
		},
		{
			name: "steps noted before the request aren't the launch's",
			marks: []mark{
				{stepTokenStart, -2 * time.Second},
				{stepTokenEnd, -time.Second},
				{stepTerminalStart, -time.Second},
				{stepHeld, time.Second},
			},
			found: 3 * time.Second,
		},
		{
			name:  "the terminal's program never seen",
			marks: []mark{{stepHeld, time.Second}, {stepTerminalStart, 2 * time.Second}},
			found: 3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clock := testutil.NewClock(t0)
			steps := newLaunchSteps(clock.Now)

			for _, m := range tt.marks {
				clock.Set(t0.Add(m.at))
				steps.mark(m.step)
			}

			assert.Equal(t, tt.want, steps.split(t0, t0.Add(tt.found)))
		})
	}

	t.Run("without a recorder", func(t *testing.T) {
		t.Parallel()

		var steps *launchSteps

		steps.mark(stepHeld)
		assert.Zero(t, steps.split(t0, t0.Add(time.Second)))
	})
}

func TestStepRunnerNotesTheTerminalAndTheToken(t *testing.T) {
	t.Parallel()

	t0 := requested()

	clock := testutil.NewClock(t0)
	inner := scripted(answers{
		"gh": func([]string) ([]byte, []byte, error) {
			clock.Advance(500 * time.Millisecond)

			return []byte("token\n"), nil, nil
		},
		"osascript": func([]string) ([]byte, []byte, error) {
			clock.Advance(800 * time.Millisecond)

			return nil, nil, errTerminal
		},
		"open": func([]string) ([]byte, []byte, error) {
			clock.Advance(100 * time.Millisecond)

			return nil, nil, nil
		},
		"git": func([]string) ([]byte, []byte, error) {
			clock.Advance(time.Second)

			return nil, nil, nil
		},
	})
	steps := newLaunchSteps(clock.Now)
	runner := stepRunner{inner: inner, steps: steps}

	_, _, err := runner.Run(t.Context(), "/opt/homebrew/bin/gh", "api", "user")
	require.NoError(t, err)

	_, _, err = runner.Run(t.Context(), "git", "status")
	require.NoError(t, err)
	assert.Empty(t, steps.at, "neither gh api nor git is a step")

	out, _, err := runner.Run(t.Context(), "/opt/homebrew/bin/gh", "auth", "token", "--hostname", "github.com")
	require.NoError(t, err)
	assert.Equal(t, "token\n", string(out), "the program's output passes through")

	_, _, err = runner.Run(t.Context(), "osascript", "-e", "tell application \"iTerm\" to select (tty \"ttys004\")")
	require.ErrorIs(t, err, errTerminal, "and its error")

	_, _, err = runner.Run(t.Context(), "open", "-R", "/tmp/run.log")
	require.NoError(t, err)
	assert.NotContains(t, steps.at, stepTerminalStart, "a focus or a reveal doesn't open the launch")

	_, _, err = runner.Run(t.Context(), "osascript", "-e", "write text \"'/tmp/launches/pr-1.command'\"")
	require.ErrorIs(t, err, errTerminal)

	// gh api took 0.5 s and git 1 s before the token's 0.5 s; then a focus took 0.8 s and a
	// reveal 0.1 s before the launch script's 0.8 s.
	assert.Equal(t, map[step]time.Time{
		stepTokenStart:    t0.Add(1500 * time.Millisecond),
		stepTokenEnd:      t0.Add(2 * time.Second),
		stepTerminalStart: t0.Add(2900 * time.Millisecond),
		stepTerminalEnd:   t0.Add(3700 * time.Millisecond),
	}, steps.at)
}

func TestLaunchTimerSplitsItsTime(t *testing.T) {
	t.Parallel()

	t0 := requested()

	clone := testutil.RealTempDir(t)
	probe := &fakeProbe{pids: [][]int{{}, {20}}, cwds: map[int]string{20: clone}}

	// Every read of the clock is a second after the one before: the request, then each
	// step the launch notes, then the agent found.
	clock := testutil.NewSteppingClock(t0, time.Second)
	steps := newLaunchSteps(clock.Now)

	timer := newTimer(probe, clone, func(ctx context.Context) (launcher.LaunchResult, error) {
		steps.mark(stepHeld)
		steps.mark(stepTerminalStart)
		steps.mark(stepTerminalEnd)

		return launched(ctx)
	})
	timer.now, timer.steps = clock.Now, steps

	report, err := timer.run(t.Context())
	require.NoError(t, err)

	assert.Equal(t, 4*time.Second, report.Elapsed)
	assert.Equal(t, LaunchSteps{
		Known: true, Held: time.Second, Confirmed: time.Second, Terminal: time.Second, Agent: time.Second,
	}, report.Steps)
	assert.Equal(t, []string{
		"launch.ms=4000 s=4.00",
		"launch.steps held_ms=1000 token_ms=0 confirmed_ms=1000 terminal_ms=1000 agent_ms=1000",
		"launch.unheld_ms=3000 (without the held-back first request: a launch whose files check out, in a running app)",
		"launch.terminal=iterm2",
		"launch.permission=plan mode (outside contributor)",
		"launch.pid=20 (stopped)",
	}, report.Lines())
}

func TestLaunchReportLinesWithoutAHold(t *testing.T) {
	t.Parallel()

	report := LaunchReport{
		Elapsed: 3 * time.Second, Steps: LaunchSteps{Known: true, Confirmed: time.Second, Agent: 2 * time.Second},
		PID: 7, Terminal: "iterm2", Permission: "full access",
	}

	assert.Equal(t, []string{
		"launch.ms=3000 s=3.00",
		"launch.steps held_ms=0 token_ms=0 confirmed_ms=1000 terminal_ms=0 agent_ms=2000",
		"launch.terminal=iterm2",
		"launch.permission=full access",
		"launch.pid=7 (stopped)",
	}, report.Lines(), "a launch that wasn't held back gives no unheld time")
}
