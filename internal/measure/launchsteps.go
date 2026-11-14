package measure

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
)

// step names a moment in a measured launch that the report splits its time at.
type step int

const (
	// stepHeld is the first request returning, held back for the pull request's files.
	stepHeld step = iota

	// stepTokenStart and stepTokenEnd bound the GitHub CLI handing over its token, which a
	// freshly started app borrows on its first request to GitHub.
	stepTokenStart
	stepTokenEnd

	// stepTerminalStart and stepTerminalEnd bound the terminal's program (osascript, for
	// iTerm2) accepting the launch.
	stepTerminalStart
	stepTerminalEnd
)

// LaunchSteps splits a measured launch's time between its steps.
type LaunchSteps struct {
	// Known reports that the terminal's program was seen, so the steps below add up to the
	// launch's time; every other field is zero without it.
	Known bool

	// Held is the first request, which the placeholder repository's file check holds
	// back, or zero when it wasn't held.
	Held time.Duration

	// Token is how long the GitHub CLI took to hand over its token, within Held (or
	// Confirmed, when nothing was held), or zero when it wasn't asked: an app that has been
	// running holds one already.
	Token time.Duration

	// Confirmed runs from the request that started the launch to the terminal's program
	// starting: the app's own work, the file check on GitHub included.
	Confirmed time.Duration

	// Terminal is the terminal's program accepting the launch: osascript and iTerm2
	// opening a tab.
	Terminal time.Duration

	// Agent runs from the terminal accepting the launch to the agent running in the clone:
	// the new tab's shell starting with your startup files, the script, and the agent's
	// own start.
	Agent time.Duration
}

// line renders the steps as one key=value line.
func (s LaunchSteps) line() string {
	return fmt.Sprintf("launch.steps held_ms=%s token_ms=%s confirmed_ms=%s terminal_ms=%s agent_ms=%s",
		milliseconds(s.Held), milliseconds(s.Token), milliseconds(s.Confirmed), milliseconds(s.Terminal),
		milliseconds(s.Agent))
}

// launchSteps notes when each step of one measured launch happened, on its clock.
type launchSteps struct {
	mu  sync.Mutex
	now func() time.Time
	at  map[step]time.Time
}

// newLaunchSteps returns a launchSteps that reads the time from now.
func newLaunchSteps(now func() time.Time) *launchSteps {
	return &launchSteps{now: now, at: map[step]time.Time{}}
}

// mark notes that s happened now, the first time only. A nil launchSteps notes nothing.
func (l *launchSteps) mark(s step) {
	if l == nil {
		return
	}

	at := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if _, ok := l.at[s]; !ok {
		l.at[s] = at
	}
}

// split works out the steps of a launch requested at start whose agent was found at
// found, from the steps noted since start. It is the zero LaunchSteps when the terminal's
// program wasn't seen, or for a nil launchSteps.
func (l *launchSteps) split(start, found time.Time) LaunchSteps {
	if l == nil {
		return LaunchSteps{}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// A step noted before the request isn't the launch's.
	at := func(s step) (time.Time, bool) {
		t, ok := l.at[s]

		return t, ok && !t.Before(start)
	}

	termStart, started := at(stepTerminalStart)
	termEnd, ended := at(stepTerminalEnd)

	if !started || !ended {
		return LaunchSteps{}
	}

	out := LaunchSteps{Known: true, Terminal: termEnd.Sub(termStart), Agent: found.Sub(termEnd)}

	from := start
	if held, ok := at(stepHeld); ok {
		out.Held, from = held.Sub(start), held
	}

	out.Confirmed = termStart.Sub(from)

	tokenStart, asked := at(stepTokenStart)
	if tokenEnd, handed := at(stepTokenEnd); asked && handed {
		out.Token = tokenEnd.Sub(tokenStart)
	}

	return out
}

// stepRunner runs programs with inner, noting in steps when the terminal's program and
// the GitHub CLI's token began and ended.
type stepRunner struct {
	inner execpath.Runner
	steps *launchSteps
}

// Run runs the program with the inner runner.
func (r stepRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	begin, end, ok := stepsOf(name, args)
	if ok {
		r.steps.mark(begin)
	}

	stdout, stderr, err = r.inner.Run(ctx, name, args...)

	if ok {
		r.steps.mark(end)
	}

	return stdout, stderr, err
}

// stepsOf names the steps a program's run begins and ends, when the report splits it out:
// the terminal's program opening the launch script (osascript for iTerm2, open for
// Terminal.app, told from a focus or a reveal by the script's name in its arguments), and
// gh auth token.
func stepsOf(name string, args []string) (begin, end step, ok bool) {
	switch filepath.Base(name) {
	case "osascript", "open":
		script := terminal.DefaultRenderer().Extension()

		if slices.ContainsFunc(args, func(a string) bool { return strings.Contains(a, script) }) {
			return stepTerminalStart, stepTerminalEnd, true
		}
	case "gh":
		if len(args) >= 2 && args[0] == "auth" && args[1] == "token" {
			return stepTokenStart, stepTokenEnd, true
		}
	}

	return 0, 0, false
}
