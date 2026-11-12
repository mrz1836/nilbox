package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// agentRunner runs a coding agent's command, for its --version or for the state of its
// sessions, with the command's own directory first on the search path. An agent installed
// with npm starts through a script that finds node beside it, and an app started from the
// Dock has launchd's search path, which lacks that directory.
type agentRunner struct {
	search execpath.Search

	// env is the environment the command runs with, or nil for this process's.
	env []string
}

// Run runs name with args, its directory first on the search path.
func (r agentRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	base := r.env
	if base == nil {
		base = os.Environ()
	}

	path := strings.Join([]string{filepath.Dir(name), os.Getenv("PATH")}, string(os.PathListSeparator))

	return execpath.Exec{Search: r.search, Env: shellenv.With(base, "PATH", path)}.Run(ctx, name, args...)
}

// sessionLedger hands the launcher the sessions ledger, which records each session a
// launch starts. It implements launcher.SessionLedger.
type sessionLedger struct {
	ledger *sessions.Ledger
}

// Started records a session a launch just started.
func (l sessionLedger) Started(ctx context.Context, s launcher.StartedSession) error {
	return l.ledger.Start(ctx, sessions.Record{
		ID: s.ID, AgentSessionID: s.AgentSessionID, ResumedFrom: s.ResumedFrom, Agent: s.Agent, Profile: s.Profile,
		ConfigDir: s.ConfigDir, Prompt: s.Prompt, PromptHead: s.PromptHead, Terminal: s.Terminal, ItemID: s.ItemID,
		Owner: s.Owner, Name: s.Name, Kind: s.Kind, Number: s.Number, Title: s.Title, URL: s.URL, Dir: s.Dir,
		ClonePath: s.ClonePath, Worktree: s.Worktree, WorktreeAction: sessions.WorktreeAction(s.WorktreeAction),
		Permission: s.Permission, Notice: s.Notice, LaunchedAt: s.LaunchedAt,
	})
}

// Resumable returns a recorded session, for resuming it.
func (l sessionLedger) Resumable(ctx context.Context, id string) (launcher.ResumableSession, error) {
	r, err := l.ledger.Get(ctx, id)
	if err != nil {
		return launcher.ResumableSession{}, err
	}

	carried := false
	if !r.State.Live() {
		if carried, err = l.ledger.ConversationLive(ctx, r.AgentSessionID); err != nil {
			return launcher.ResumableSession{}, err
		}
	}

	return launcher.ResumableSession{
		ID: r.ID, AgentSessionID: r.AgentSessionID, Agent: r.Agent, Profile: r.Profile, ItemID: r.ItemID, Dir: r.Dir,
		Worktree: r.Worktree, Live: r.State.Live(), Carried: carried,
	}, nil
}

// Running returns the sessions that have not ended running in the folder dir, newest
// first.
func (l sessionLedger) Running(ctx context.Context, dir string) ([]launcher.RunningSession, error) {
	running, err := l.ledger.Running(ctx, dir)
	if err != nil {
		return nil, err
	}

	out := make([]launcher.RunningSession, 0, len(running))
	for _, s := range running {
		out = append(out, launcher.RunningSession{
			SessionID: s.ID, Agent: s.Agent, Doing: s.State.Phrase(), Since: s.StateAt, Dir: s.Dir, Worktree: s.Worktree,
			Repo: s.Repo, Number: s.Number, CanFocus: s.CanFocus,
		})
	}

	return out, nil
}

// ShellSessions are the agent sessions as the desktop shell's menu bar reads them: the
// sessions waiting for the user, and the terminal a click on one brings forward. It
// implements desktop.Sessions.
type ShellSessions struct {
	ledger  *sessions.Ledger
	service *sessions.Service
}

// Waiting returns the running sessions waiting for the user, oldest first.
func (s ShellSessions) Waiting(ctx context.Context) ([]sessions.Session, error) {
	return s.ledger.Waiting(ctx)
}

// Focus brings forward the terminal tab a running session is in.
func (s ShellSessions) Focus(ctx context.Context, id string) error {
	return s.service.Focus(ctx, id)
}

// terminalFocuser hands the sessions service the launcher's terminal drivers, which bring
// a session's tab forward. It implements sessions.Focuser.
type terminalFocuser struct {
	registry terminal.Registry
}

// Focus brings forward the tab that runs on tty in the terminal named by its settings ID.
// A terminal that cannot is told in words.
func (f terminalFocuser) Focus(ctx context.Context, id, tty string) error {
	err := f.registry.Focus(ctx, terminal.ID(id), tty)

	switch {
	case errors.Is(err, terminal.ErrNoSuchTab):
		return uierr.New("Couldn't find the session's tab. It may have been closed, or moved to another app.", err)
	case errors.Is(err, terminal.ErrNoFocus), errors.Is(err, terminal.ErrUnknownTerminal),
		errors.Is(err, terminal.ErrUnsupportedOS):
		return uierr.New("This terminal can't show a session's tab.", err)
	default:
		return err
	}
}
