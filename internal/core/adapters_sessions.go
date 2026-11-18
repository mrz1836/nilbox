package core

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/logging"
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
// launch starts, and the background runs' fences, which a run's session resumes under
// when it is taken over. It implements launcher.SessionLedger.
type sessionLedger struct {
	ledger *sessions.Ledger
	runs   *backgroundAgents
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
		Worktree: r.Worktree, Live: r.State.Live(), Carried: carried, RunID: r.RunID, Fence: l.runs.fenceOf(ctx, r.RunID),
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

// sessionOutcomes records on the agent sessions what the inbox learned came of their items
// on GitHub (inbox.OutcomeSink): a review of the user's that decided, a merge, or a close,
// each at GitHub's time for it, through sessions.Ledger.NoteGitHub. The inbox is built
// before the sessions are followed, so ledger is set once they are; until then, and for
// any other outcome, it records nothing. A failure is logged: what it records is only
// what the panel says.
type sessionOutcomes struct {
	ledger *sessions.Ledger
	logger *slog.Logger
}

// Settle records what outcomes say came of their items on GitHub.
func (s *sessionOutcomes) Settle(ctx context.Context, outcomes []inbox.Outcome) {
	if s == nil || s.ledger == nil {
		return
	}

	notes := gitHubNotes(outcomes)
	if len(notes) == 0 {
		return
	}

	if err := s.ledger.NoteGitHub(ctx, notes); err != nil {
		logging.Or(s.logger).WarnContext(ctx, "could not record on the sessions what came of their items", "error", err)
	}
}

// gitHubNotes is what outcomes say came of their items on GitHub, in the sessions' words.
func gitHubNotes(outcomes []inbox.Outcome) []sessions.GitHubNote {
	notes := make([]sessions.GitHubNote, 0, len(outcomes))

	for _, o := range outcomes {
		if ev := gitHubEventOf(o); ev != sessions.GitHubNone && !o.GitHub.At.IsZero() {
			notes = append(notes, sessions.GitHubNote{Ref: o.Ref, Event: ev, Commit: o.GitHub.Commit, At: o.GitHub.At})
		}
	}

	return notes
}

// gitHubEventOf is the event an outcome tells of: a review that decided, a merge, or a
// close, by what GitHub said; none for any other outcome.
func gitHubEventOf(o inbox.Outcome) sessions.GitHubEvent {
	if o.Kind == inbox.OutcomeReviewed {
		return verdictEvent(o.GitHub.State)
	}

	if o.Kind == inbox.OutcomeMerged || o.Kind == inbox.OutcomeClosed || o.Kind == inbox.OutcomeSeenClosed {
		return endEvent(o.GitHub.State)
	}

	return sessions.GitHubNone
}

// verdictEvent is the event a review's verdict state is, none for any other.
func verdictEvent(state string) sessions.GitHubEvent {
	switch state {
	case github.ReviewStateApproved:
		return sessions.GitHubApproved
	case github.ReviewStateChangesRequested:
		return sessions.GitHubChangesRequested
	default:
		return sessions.GitHubNone
	}
}

// endEvent is the event an item's closed state is, none for an open one.
func endEvent(state string) sessions.GitHubEvent {
	switch state {
	case github.StateMerged:
		return sessions.GitHubMerged
	case github.StateClosed:
		return sessions.GitHubClosed
	default:
		return sessions.GitHubNone
	}
}

// backgroundTokens reads a background run's tokens, for its session's panel, from
// Autopilot's ledger, which its runner keeps current as the run goes. It implements
// sessions.RunTokens.
type backgroundTokens struct {
	engine interface{ Built() *autopilot.Engine }
}

// RunTokens returns the tokens the background run that started the run engine's run runID
// reported so far; false before the engine is built, or for a run it doesn't record.
func (b backgroundTokens) RunTokens(ctx context.Context, runID int64) (sessions.Usage, bool, error) {
	e := b.engine.Built()
	if e == nil {
		return sessions.Usage{}, false, nil
	}

	u, ok, err := e.BackgroundTokens(ctx, runID)
	if err != nil || !ok {
		return sessions.Usage{}, false, err
	}

	return sessions.Usage{Input: u.TokensIn, Output: u.TokensOut, CacheRead: u.CacheRead}, true, nil
}
