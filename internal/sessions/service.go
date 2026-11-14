package sessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// psBin is ps's fixed path on macOS and Linux; it names the terminal a process runs in.
const psBin = "/bin/ps"

var (
	// ErrEnded is returned by Focus for a session that has ended.
	ErrEnded = errors.New("the session has ended")

	// ErrNoTerminal is returned by Focus for a session whose terminal cannot be found.
	ErrNoTerminal = errors.New("the session's terminal is not known")

	// ErrNoFolder is returned by ShowFolder for a session whose folder is gone.
	ErrNoFolder = errors.New("the session's folder is gone")

	// ErrNoFinder is returned by ShowFolder where there is no Finder to show it in.
	ErrNoFinder = errors.New("no file manager to show the folder in")
)

// Focuser brings a terminal's tab that runs on a tty forward. The app adapts the
// launcher's terminal drivers to it.
type Focuser interface {
	// Focus selects the tab, in the terminal named by its settings ID, whose tty is
	// tty, such as /dev/ttys004, and brings its window forward.
	Focus(ctx context.Context, terminal, tty string) error
}

// Revealer shows a file or a folder in Finder. Core adapts open -R, on macOS only.
type Revealer interface {
	Reveal(ctx context.Context, path string) error
}

// Config holds a Service's and a Ledger's dependencies. Store is required.
type Config struct {
	Store *Store

	// Watcher is woken when a launch records a session. Optional.
	Watcher *Watcher

	// Switch is the user's master switch; without it, sessions are always followed.
	Switch Switch

	// Focuser shows a session's terminal; without it, Focus fails.
	Focuser Focuser

	// PS runs ps, which names the terminal a session's process runs in.
	PS execpath.Runner

	// Reveal shows a session's folder in Finder; without it, ShowFolder fails.
	Reveal Revealer

	// Home shows home as ~.
	Home string

	// Events hears an event.SessionsUpdated when a launch records a session, and when the
	// user forgets one.
	Events event.Sink

	// Words reads each agent's sessions' last messages in full, for LastWords; an agent
	// with none falls back to the start the ledger keeps.
	Words map[agent.ID]WordsReader

	Clock  clock.Clock
	Logger *slog.Logger
}

// Service is the sessions as the window binds them.
type Service struct {
	store   *Store
	toggle  Switch
	focuser Focuser
	ps      execpath.Runner
	reveal  Revealer
	home    string
	events  event.Sink
	words   map[agent.ID]WordsReader
	clock   clock.Clock
	logger  *slog.Logger
}

// NewService returns the Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		store: cfg.Store, toggle: cfg.Switch, focuser: cfg.Focuser, ps: cfg.PS, reveal: cfg.Reveal, home: cfg.Home,
		events: event.Or(cfg.Events), words: wordsReaders(cfg.Words), clock: clock.Or(cfg.Clock),
		logger: logging.Or(cfg.Logger),
	}
}

// List returns every session still running and the others launched in the last week,
// newest first, and whether the app follows sessions.
func (s *Service) List(ctx context.Context) (Snapshot, error) {
	following := true

	if s.toggle != nil {
		on, err := s.toggle.FollowSessions(ctx)
		if err != nil {
			return Snapshot{}, err
		}

		following = on
	}

	records, err := s.store.Recent(ctx, s.clock.Now().Add(-RecentWindow))
	if err != nil {
		return Snapshot{}, err
	}

	return Snapshot{Following: following, Sessions: shown(records, s.home)}, nil
}

// shown is records as the window shows them, with home as ~. A conversation a running
// session carries on, as one resumed does, is not offered for resuming again from a
// session of it that ended.
func shown(records []Record, home string) []Session {
	carried := map[string]bool{}

	for _, r := range records {
		if r.State.Live() && r.AgentSessionID != "" {
			carried[r.AgentSessionID] = true
		}
	}

	out := make([]Session, 0, len(records))

	for _, r := range records {
		session := r.Session(home)
		session.CanResume = session.CanResume && !carried[r.AgentSessionID]
		out = append(out, session)
	}

	return out
}

// Focus brings forward the terminal tab a running session is in.
func (s *Service) Focus(ctx context.Context, id string) error {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	switch {
	case !r.State.Live():
		return uierr.New("This session has ended.", ErrEnded)
	case r.PID <= 0:
		return uierr.New("The session's terminal isn't known yet. Try again once it has started.", ErrNoTerminal)
	case s.focuser == nil || s.ps == nil:
		return uierr.New("Showing a session's terminal isn't supported here.", ErrNoTerminal)
	}

	tty, err := TTY(ctx, s.ps, r.PID)
	if err != nil {
		return uierr.New("Couldn't find the session's terminal. It may have just ended.", err)
	}

	return s.focuser.Focus(ctx, r.Terminal, tty)
}

// ShowFolder shows the folder a session runs in, its worktree or the clone, in Finder,
// whether or not the session still runs.
func (s *Service) ShowFolder(ctx context.Context, id string) error {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}

	if info, statErr := os.Stat(r.Dir); statErr != nil || !info.IsDir() {
		return uierr.New(paths.Tilde(r.Dir, s.home)+" isn't there anymore.", fmt.Errorf("%w: %s", ErrNoFolder, r.Dir))
	}

	if s.reveal == nil {
		return uierr.New(brand.Name+" can't open Finder here.", ErrNoFinder)
	}

	if err = s.reveal.Reveal(ctx, r.Dir); err != nil {
		return fmt.Errorf("show the session's folder: %w", err)
	}

	return nil
}

// Forget forgets a session that has ended, on the user's click, so it leaves the card and
// the panel, and tells the window. A session already forgotten counts as done, and one
// still running is refused and kept.
func (s *Service) Forget(ctx context.Context, id string) error {
	err := s.store.Forget(ctx, id)

	switch {
	case err == nil, errors.Is(err, ErrNotFound):
		s.events.Emit(event.SessionsUpdated{})

		return nil
	case errors.Is(err, ErrLive):
		return uierr.New("This session is still running. Remove it once it has ended.", err)
	default:
		return err
	}
}

// TTY returns the terminal device the process pid runs on, such as /dev/ttys004, asking
// ps through run. A process with no terminal, or none at all, is ErrNoTerminal.
func TTY(ctx context.Context, run execpath.Runner, pid int) (string, error) {
	out, _, err := run.Run(ctx, psBin, "-o", "tty=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", fmt.Errorf("%w: ps: %w", ErrNoTerminal, err)
	}

	name := strings.TrimSpace(execpath.FirstLine(out))
	if name == "" || strings.Trim(name, "?") == "" || strings.ContainsAny(name, " \t") {
		return "", fmt.Errorf("%w: process %d has no terminal", ErrNoTerminal, pid)
	}

	return "/dev/" + strings.TrimPrefix(name, "/dev/"), nil
}

// Ledger records the sessions launches start, and reads them for the launcher and the
// menu bar. It is not bound to the window.
type Ledger struct {
	store   *Store
	watcher *Watcher
	toggle  Switch
	events  event.Sink
	home    string
}

// NewLedger returns the Ledger over cfg.
func NewLedger(cfg Config) *Ledger {
	return &Ledger{store: cfg.Store, watcher: cfg.Watcher, toggle: cfg.Switch, events: event.Or(cfg.Events), home: cfg.Home}
}

// Start records a session a launch just started, tells the window, and wakes the
// Watcher, so it follows the session at once.
func (l *Ledger) Start(ctx context.Context, r Record) error {
	if err := l.store.Insert(ctx, r); err != nil {
		return err
	}

	l.events.Emit(event.SessionsUpdated{})

	if l.watcher != nil {
		l.watcher.Wake()
	}

	return nil
}

// Get returns one recorded session, or ErrNotFound.
func (l *Ledger) Get(ctx context.Context, id string) (Record, error) {
	return l.store.Get(ctx, id)
}

// AgentProcesses returns the coding agent of each running session's process, by PID, or
// none while the app does not follow sessions, when the processes it knows may be stale.
func (l *Ledger) AgentProcesses(ctx context.Context) (map[int]agent.ID, error) {
	out := map[int]agent.ID{}
	if !l.following(ctx) {
		return out, nil
	}

	live, err := l.store.Live(ctx)
	if err != nil {
		return nil, err
	}

	for _, r := range live {
		if r.PID > 0 {
			out[r.PID] = r.Agent
		}
	}

	return out, nil
}

// Running returns the sessions that have not ended running in the folder dir, whichever
// item each was started on, newest first, or none while the app does not follow sessions.
// Folders are compared with their symbolic links resolved.
func (l *Ledger) Running(ctx context.Context, dir string) ([]Session, error) {
	out := []Session{}
	if !l.following(ctx) {
		return out, nil
	}

	live, err := l.store.Live(ctx)
	if err != nil {
		return nil, err
	}

	for _, r := range slices.Backward(live) {
		if paths.Same(r.Dir, dir) {
			out = append(out, r.Session(l.home))
		}
	}

	return out, nil
}

// ConversationLive reports whether a session that has not ended carries on the agent's
// conversation agentSessionID, as one resumed does; false while the app does not follow
// sessions, when what it knows may be stale.
func (l *Ledger) ConversationLive(ctx context.Context, agentSessionID string) (bool, error) {
	if !l.following(ctx) {
		return false, nil
	}

	return l.store.ConversationLive(ctx, agentSessionID)
}

// Waiting returns the running sessions waiting for the user, the longest waiting first,
// or none while the app does not follow sessions.
func (l *Ledger) Waiting(ctx context.Context) ([]Session, error) {
	if l.toggle != nil {
		on, err := l.toggle.FollowSessions(ctx)
		if err != nil || !on {
			return []Session{}, err
		}
	}

	live, err := l.store.Live(ctx)
	if err != nil {
		return nil, err
	}

	waiting := slices.DeleteFunc(live, func(r Record) bool { return !r.State.NeedsYou() })
	slices.SortStableFunc(waiting, func(a, b Record) int { return a.StateAt.Compare(b.StateAt) })

	out := make([]Session, 0, len(waiting))
	for _, r := range waiting {
		out = append(out, r.Session(l.home))
	}

	return out, nil
}

// following reports whether the app follows sessions; a switch that cannot be read counts
// as on, as the Watcher counts it.
func (l *Ledger) following(ctx context.Context) bool {
	if l.toggle == nil {
		return true
	}

	on, err := l.toggle.FollowSessions(ctx)

	return err != nil || on
}
