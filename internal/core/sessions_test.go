package core

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/repo"
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
		ClonePath: "/srv/projects/example-repo", Worktree: "pr-12", WorktreeAction: launcher.WorktreeAdopt,
		Permission: "full access", Notice: "Started from the last fetched copy.", LaunchedAt: at,
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
	assert.Equal(t, sessions.WorktreeAdopt, stored.WorktreeAction, "the two packages name the actions alike")
	assert.Equal(t, "full access", stored.Permission)
	assert.Equal(t, "Started from the last fetched copy.", stored.Notice)

	_, err = l.Resumable(t.Context(), "missing")
	require.ErrorIs(t, err, sessions.ErrNotFound)

	_, err = store.Apply(t.Context(), at.Add(time.Minute), []sessions.Observation{
		{ID: "s1", State: sessions.StateNeedsApproval, PID: 4100},
	})
	require.NoError(t, err)

	running, err := l.Running(t.Context(), "/srv/wt/example-repo/pr-12")
	require.NoError(t, err)
	assert.Equal(t, []launcher.RunningSession{{
		SessionID: "s1", Agent: agent.Claude, Doing: "needs approval", Since: "2026-11-09T05:12:00Z",
		Dir: "/srv/wt/example-repo/pr-12", Worktree: "pr-12", Repo: boundOwner + "/example-repo", Number: 12,
		CanFocus: true,
	}}, running)
}

// focusDriver is a terminal driver that can focus, failing with err, and records the tab
// it was last asked for when asked is set.
type focusDriver struct {
	id    terminal.ID
	err   error
	asked *terminal.Tab
}

func (d focusDriver) ID() terminal.ID {
	if d.id == "" {
		return terminal.ITerm2
	}

	return d.id
}

func (focusDriver) Open(context.Context, string) error { return nil }

func (d focusDriver) Focus(_ context.Context, tab terminal.Tab) error {
	if d.asked != nil {
		*d.asked = tab
	}

	return d.err
}

func TestTerminalFocuserSaysWhyItCannot(t *testing.T) {
	t.Parallel()

	var asked terminal.Tab

	require.NoError(t, terminalFocuser{registry: terminal.NewRegistry(focusDriver{asked: &asked})}.
		Focus(t.Context(), "iterm2", "/dev/ttys004", 4100))
	assert.Equal(t, terminal.Tab{TTY: "/dev/ttys004", PID: 4100}, asked, "the driver gets the session's tty and process")

	err := terminalFocuser{registry: terminal.NewRegistry(focusDriver{err: terminal.ErrNoSuchTab})}.
		Focus(t.Context(), "iterm2", "/dev/ttys004", 4100)
	require.ErrorIs(t, err, terminal.ErrNoSuchTab)
	assert.Contains(t, uierr.Message(err), "Couldn't find the session's tab")

	err = terminalFocuser{registry: terminal.NewRegistry(focusDriver{})}.Focus(t.Context(), "terminal", "/dev/ttys004", 4100)
	require.ErrorIs(t, err, terminal.ErrUnknownTerminal)
	assert.Contains(t, uierr.Message(err), "can't show a session's tab")

	err = terminalFocuser{registry: terminal.NewRegistry(focusDriver{err: terminal.ErrAutomationDenied})}.
		Focus(t.Context(), "iterm2", "/dev/ttys004", 4100)
	require.ErrorIs(t, err, terminal.ErrAutomationDenied, "the Automation refusal keeps its own words")

	err = terminalFocuser{registry: terminal.NewRegistry(focusDriver{id: terminal.Warp, err: terminal.ErrNoFocusLink})}.
		Focus(t.Context(), "warp", "/dev/ttys004", 4100)
	require.ErrorIs(t, err, terminal.ErrNoFocusLink)
	assert.Equal(t, "Couldn't tell which Warp tab runs this session. Warp v0.2026.05.27 or newer can; "+
		"update Warp, and sessions launched afterwards can be shown.", uierr.Message(err))
}

// procEnv is the agents' processes' environments as terminalFocuser's env reads them: each
// process's variables by PID. It records what it was asked, as "<pid> <name>".
type procEnv struct {
	vars  map[int]map[string]string
	asked []string
}

func (e *procEnv) read(pid int, name string) (string, bool) {
	e.asked = append(e.asked, fmt.Sprintf("%d %s", pid, name))
	value, ok := e.vars[pid][name]

	return value, ok
}

// focusedIn is a terminal registry of the three terminals' drivers, each recording in
// focused which terminal was asked to bring a tab forward.
func focusedIn(focused *[]terminal.ID) terminal.Registry {
	drivers := make([]terminal.Driver, 0, len(terminal.All()))
	for _, id := range terminal.All() {
		drivers = append(drivers, recordingFocus{id: id, focused: focused})
	}

	return terminal.NewRegistry(drivers...)
}

// recordingFocus is a terminal driver that records its ID in focused when asked to focus.
type recordingFocus struct {
	id      terminal.ID
	focused *[]terminal.ID
}

func (d recordingFocus) ID() terminal.ID { return d.id }

func (recordingFocus) Open(context.Context, string) error { return nil }

func (d recordingFocus) Focus(context.Context, terminal.Tab) error {
	*d.focused = append(*d.focused, d.id)

	return nil
}

func TestTerminalFocuserShowsTheTerminalTheSessionRunsInNow(t *testing.T) {
	t.Parallel()

	const pid = 4100

	link := map[string]string{"WARP_FOCUS_URL": "warp://session/0123456789abcdef0123456789abcdef"}
	with := func(termProgram string) map[string]string {
		vars := maps.Clone(link)
		vars["TERM_PROGRAM"] = termProgram

		return vars
	}

	for name, c := range map[string]struct {
		recorded string
		vars     map[string]string
		want     terminal.ID
	}{
		"launched in Warp, resumed in iTerm2":         {recorded: "warp", vars: with("iTerm.app"), want: terminal.ITerm2},
		"launched in iTerm2, resumed in Warp":         {recorded: "iterm2", vars: with("WarpTerminal"), want: terminal.Warp},
		"launched in Terminal.app, resumed in Warp":   {recorded: "terminal", vars: with("WarpTerminal"), want: terminal.Warp},
		"launched in Warp, resumed in Terminal.app":   {recorded: "warp", vars: with("Apple_Terminal"), want: terminal.TerminalApp},
		"still in the terminal it was launched in":    {recorded: "iterm2", vars: with("iTerm.app"), want: terminal.ITerm2},
		"recorded in none, as a background run's was": {recorded: "", vars: with("WarpTerminal"), want: terminal.Warp},

		// The recorded terminal, when the variable names none of them or can't be read.
		"in tmux, which sets its own":        {recorded: "warp", vars: with("tmux"), want: terminal.Warp},
		"another terminal's own value":       {recorded: "terminal", vars: with("vscode"), want: terminal.TerminalApp},
		"an empty value":                     {recorded: "iterm2", vars: with(""), want: terminal.ITerm2},
		"no TERM_PROGRAM":                    {recorded: "iterm2", vars: link, want: terminal.ITerm2},
		"an environment that can't be read":  {recorded: "warp", want: terminal.Warp},
		"a value that only looks like one":   {recorded: "iterm2", vars: with("WarpTerminal\n"), want: terminal.ITerm2},
		"a settings ID rather than its name": {recorded: "terminal", vars: with("warp"), want: terminal.TerminalApp},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var focused []terminal.ID

			env := &procEnv{vars: map[int]map[string]string{}}
			if c.vars != nil {
				env.vars[pid] = c.vars
			}

			f := terminalFocuser{registry: focusedIn(&focused), env: env.read}
			require.NoError(t, f.Focus(t.Context(), c.recorded, "/dev/ttys004", pid))
			assert.Equal(t, []terminal.ID{c.want}, focused)
			assert.Equal(t, []string{"4100 TERM_PROGRAM"}, env.asked,
				"it reads that one variable of the session's process, once a click, and never Warp's link itself")
		})
	}
}

func TestTerminalFocuserWithoutAnEnvironmentReaderUsesTheRecordedTerminal(t *testing.T) {
	t.Parallel()

	var focused []terminal.ID

	require.NoError(t, terminalFocuser{registry: focusedIn(&focused)}.Focus(t.Context(), "warp", "/dev/ttys004", 4100))
	assert.Equal(t, []terminal.ID{terminal.Warp}, focused)
}

func TestTerminalFocuserWordsTheDetectedTerminalsFailure(t *testing.T) {
	t.Parallel()

	env := &procEnv{vars: map[int]map[string]string{4100: {"TERM_PROGRAM": "iTerm.app"}}}
	f := terminalFocuser{
		registry: terminal.NewRegistry(focusDriver{id: terminal.Warp, err: terminal.ErrNoFocusLink},
			focusDriver{id: terminal.ITerm2, err: terminal.ErrNoSuchTab}),
		env: env.read,
	}

	err := f.Focus(t.Context(), "warp", "/dev/ttys004", 4100)
	require.ErrorIs(t, err, terminal.ErrNoSuchTab, "a session launched in Warp and resumed in iTerm2 is iTerm2's")
	assert.Contains(t, uierr.Message(err), "Couldn't find the session's tab")
	assert.NotContains(t, uierr.Message(err), "update Warp", "it never tells the user to update a terminal it doesn't run in")
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

func TestSessionsKnowEveryWorktreeActionTheLauncherTakes(t *testing.T) {
	t.Parallel()

	for _, a := range []launcher.WorktreeAction{
		launcher.WorktreeResume, launcher.WorktreeAdopt, launcher.WorktreeAttach, launcher.WorktreeCreate,
	} {
		assert.True(t, sessions.WorktreeAction(a).Valid(), a)
	}
}

func TestSessionOutcomesMapsEachKind(t *testing.T) {
	t.Parallel()

	ref := item.Ref{Repo: repo.Name{Owner: boundOwner, Repo: boundRepo}, Number: 12, Kind: item.KindPR}
	at := time.Date(2026, 11, 9, 5, 41, 0, 0, time.UTC)
	fact := func(state, commit string) inbox.GitHubFact {
		return inbox.GitHubFact{State: state, Commit: commit, At: at}
	}

	got := gitHubNotes([]inbox.Outcome{
		{Kind: inbox.OutcomeReviewed, Ref: ref, GitHub: fact(github.ReviewStateApproved, "abc1234")},
		{Kind: inbox.OutcomeReviewed, Ref: ref, GitHub: fact(github.ReviewStateChangesRequested, "def5678")},
		{Kind: inbox.OutcomeMerged, Ref: ref, GitHub: fact(github.StateMerged, "")},
		{Kind: inbox.OutcomeClosed, Ref: ref, GitHub: fact(github.StateClosed, "")},
		{Kind: inbox.OutcomeSeenClosed, Ref: ref, GitHub: fact(github.StateMerged, "")},
		{Kind: inbox.OutcomeSeenClosed, Ref: ref, GitHub: fact(github.StateClosed, "")},

		// Nothing that came of the item on GitHub, or a fact with no time.
		{Kind: inbox.OutcomeDismissed, Ref: ref},
		{Kind: inbox.OutcomeUnlisted, Ref: ref},
		{Kind: inbox.OutcomeReviewed, Ref: ref, GitHub: inbox.GitHubFact{State: github.ReviewStateApproved}},
		{Kind: inbox.OutcomeSeenClosed, Ref: ref, GitHub: fact(github.StateOpen, "")},
	})

	note := func(ev sessions.GitHubEvent, commit string) sessions.GitHubNote {
		return sessions.GitHubNote{Ref: ref, Event: ev, Commit: commit, At: at}
	}

	assert.Equal(t, []sessions.GitHubNote{
		note(sessions.GitHubApproved, "abc1234"),
		note(sessions.GitHubChangesRequested, "def5678"),
		note(sessions.GitHubMerged, ""),
		note(sessions.GitHubClosed, ""),
		note(sessions.GitHubMerged, ""),
		note(sessions.GitHubClosed, ""),
	}, got)
}

func TestSessionOutcomesRecordOnTheSessionsOnceFollowed(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	events := &testutil.Events{}
	at := time.Date(2026, 11, 9, 5, 11, 0, 0, time.UTC)
	require.NoError(t, store.Insert(t.Context(), sessions.Record{
		ID: "s1", AgentSessionID: "s1", Agent: agent.Claude, Owner: boundOwner, Name: boundRepo, Kind: item.KindPR,
		Number: 12, Dir: "/srv/wt/example-repo/pr-12", LaunchedAt: at,
	}))

	outcome := inbox.Outcome{
		Kind: inbox.OutcomeReviewed,
		Ref:  item.Ref{Repo: repo.Name{Owner: boundOwner, Repo: boundRepo}, Number: 12, Kind: item.KindPR},
		GitHub: inbox.GitHubFact{
			State: github.ReviewStateApproved, Commit: "abc1234", At: at.Add(time.Minute),
		},
	}

	// The inbox may tell before the sessions are followed.
	notes := &sessionOutcomes{}
	notes.Settle(t.Context(), []inbox.Outcome{outcome})
	(*sessionOutcomes)(nil).Settle(t.Context(), []inbox.Outcome{outcome})

	notes.ledger = sessions.NewLedger(sessions.Config{Store: store, Events: events})
	notes.Settle(t.Context(), []inbox.Outcome{outcome})

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.GitHubApproved, got.GitHubEvent)
	assert.Equal(t, "abc1234", got.GitHubEventCommit)
	assert.Equal(t, 1, events.Count())
}

func TestABackgroundRunsTokensWaitForTheEngine(t *testing.T) {
	t.Parallel()

	got, ok, err := backgroundTokens{engine: &engineWaker{}}.RunTokens(t.Context(), 7)
	require.NoError(t, err)
	assert.False(t, ok, "no engine, no run recorded")
	assert.Equal(t, sessions.Usage{}, got)
}
