package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestSessionLedgerRecordsWhatTheLauncherStarted(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	events := &testutil.Events{}
	l := sessionLedger{ledger: sessions.NewLedger(sessions.Config{Store: store, Events: events})}

	at := time.Date(2026, 11, 9, 5, 11, 0, 0, time.UTC)
	started := launcher.StartedSession{
		ID: "s1", AgentSessionID: "s1", Agent: agent.Claude, Profile: "default", ConfigDir: "/opt/profiles/work",
		Prompt: "Review", PromptHead: "Review it", Terminal: "iterm2", ItemID: 7, Owner: boundOwner,
		Name: "example-repo", Kind: item.KindPR, Number: 12, Title: "Fix the widget",
		URL: "https://github.com/acme-org/example-repo/pull/12", Dir: "/srv/wt/example-repo/pr-12",
		ClonePath: "/srv/projects/example-repo", Worktree: "pr-12", LaunchedAt: at,
	}

	require.NoError(t, l.Started(t.Context(), started))
	assert.Equal(t, 1, events.Count())

	got, err := l.Resumable(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, launcher.ResumableSession{
		ID: "s1", AgentSessionID: "s1", Agent: agent.Claude, Profile: "default", ItemID: 7,
		Dir: "/srv/wt/example-repo/pr-12", Worktree: "pr-12", Live: true,
	}, got)

	stored, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "/opt/profiles/work", stored.ConfigDir)
	assert.Equal(t, "Review it", stored.PromptHead)

	_, err = l.Resumable(t.Context(), "missing")
	require.ErrorIs(t, err, sessions.ErrNotFound)

	_, err = store.Apply(t.Context(), at.Add(time.Minute), []sessions.Observation{
		{ID: "s1", State: sessions.StateNeedsApproval, PID: 4100},
	})
	require.NoError(t, err)

	running, err := l.Running(t.Context(), boundOwner, "example-repo", 12)
	require.NoError(t, err)
	assert.Equal(t, []launcher.RunningSession{{
		SessionID: "s1", Agent: agent.Claude, Doing: "needs approval", Since: "2026-11-09T05:12:00Z",
		Dir: "/srv/wt/example-repo/pr-12", Worktree: "pr-12", CanFocus: true,
	}}, running)
}

// focusDriver is a terminal driver that can focus, failing with err.
type focusDriver struct {
	err error
}

func (focusDriver) ID() terminal.ID                       { return terminal.ITerm2 }
func (focusDriver) Open(context.Context, string) error    { return nil }
func (d focusDriver) Focus(context.Context, string) error { return d.err }

func TestTerminalFocuserSaysWhyItCannot(t *testing.T) {
	t.Parallel()

	require.NoError(t, terminalFocuser{registry: terminal.NewRegistry(focusDriver{})}.Focus(t.Context(), "iterm2", "/dev/ttys004"))

	err := terminalFocuser{registry: terminal.NewRegistry(focusDriver{err: terminal.ErrNoSuchTab})}.
		Focus(t.Context(), "iterm2", "/dev/ttys004")
	require.ErrorIs(t, err, terminal.ErrNoSuchTab)
	assert.Contains(t, uierr.Message(err), "Couldn't find the session's tab")

	err = terminalFocuser{registry: terminal.NewRegistry(focusDriver{})}.Focus(t.Context(), "terminal", "/dev/ttys004")
	require.ErrorIs(t, err, terminal.ErrUnknownTerminal)
	assert.Contains(t, uierr.Message(err), "can't show a session's tab")

	err = terminalFocuser{registry: terminal.NewRegistry(focusDriver{err: terminal.ErrAutomationDenied})}.
		Focus(t.Context(), "iterm2", "/dev/ttys004")
	require.ErrorIs(t, err, terminal.ErrAutomationDenied, "the Automation refusal keeps its own words")
}

func TestShellSessions(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	cfg := sessions.Config{
		Store: store, PS: &testutil.Runner{Stdout: "ttys004\n"},
		Focuser: terminalFocuser{registry: terminal.NewRegistry(focusDriver{})},
	}
	s := ShellSessions{ledger: sessions.NewLedger(cfg), service: sessions.NewService(cfg)}

	got, err := s.Waiting(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got)

	require.ErrorIs(t, s.Focus(t.Context(), "missing"), sessions.ErrNotFound)
}
