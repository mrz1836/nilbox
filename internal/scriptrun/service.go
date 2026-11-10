package scriptrun

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Service is the runs as the window sees them: the history, a run's output, Stop, Run
// again, and the quit guard's answer. The window binds it; it starts nothing new itself
// but Run again.
type Service struct {
	e *Engine
}

// NewService returns the window's face of e.
func NewService(e *Engine) *Service {
	return &Service{e: e}
}

// Filter picks the runs Runs lists.
type Filter struct {
	// Kinds are the kinds listed; none lists every kind.
	Kinds []Kind `json:"kinds"`

	// Repo is part of a repository's "owner/name" to look for, in any case; "" lists
	// every run.
	Repo string `json:"repo"`
}

// matches reports whether f lists rec.
func (f Filter) matches(rec Record) bool {
	if len(f.Kinds) > 0 && !slices.Contains(f.Kinds, rec.Kind) {
		return false
	}

	want := strings.ToLower(strings.TrimSpace(f.Repo))

	return want == "" || (rec.Repo.Owner != "" && strings.Contains(rec.Repo.Key(), want))
}

// Run is one run as the window shows it. Every field is a plain value.
type Run struct {
	ID     int64  `json:"id"`
	Kind   Kind   `json:"kind"`
	Title  string `json:"title"`
	Source string `json:"source"`

	// Repo is the clone's repository, "owner/name", or "" for a folder that isn't one,
	// and Dir the folder, with ~ for home.
	Repo string `json:"repo"`
	Dir  string `json:"dir"`

	// Argv is the command as it started, and Command its display form.
	Argv    []string `json:"argv"`
	Command string   `json:"command"`

	RunsIn RunsIn `json:"runsIn"`
	Status Status `json:"status"`

	// ExitCode is the program's exit status, or null when unknown or still running.
	ExitCode *int `json:"exitCode"`

	// Summary says how it ended, or for an orphaned run, that it runs on.
	Summary string `json:"summary"`
	Result  Result `json:"result"`

	// LogBytes is its log's size: so far, while it runs.
	LogBytes int64 `json:"logBytes"`

	// Seen reports that the user has seen it, which clears the tab's red dot.
	Seen bool `json:"seen"`

	// StartedAt is when it started, and FinishedAt when it ended, or "", as RFC 3339.
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`

	// CanStop reports a run in the app going now; CanRunAgain a run whose command can be
	// worked out again, never a release.
	CanStop     bool `json:"canStop"`
	CanRunAgain bool `json:"canRunAgain"`
}

// Runs returns the runs filter picks, newest first.
func (s *Service) Runs(ctx context.Context, filter Filter) ([]Run, error) {
	recs, err := s.e.store.list(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Run, 0, len(recs))

	for _, rec := range recs {
		if filter.matches(rec) {
			out = append(out, s.view(rec))
		}
	}

	return out, nil
}

// Run returns one run.
func (s *Service) Run(ctx context.Context, id int64) (Run, error) {
	rec, err := s.e.store.get(ctx, id)
	if err != nil {
		return Run{}, err
	}

	return s.view(rec), nil
}

// Output returns up to limit bytes of a run's log from offset, cut between characters; a
// limit of 0 or more than 256 KB reads 256 KB. A negative offset reads the log's last
// limit bytes. A log not written yet reads as empty.
func (s *Service) Output(ctx context.Context, id, offset, limit int64) (Chunk, error) {
	rec, err := s.e.store.get(ctx, id)
	if err != nil {
		return Chunk{}, err
	}

	return readChunk(s.e.files.log(id), offset, limit, !rec.Status.Live())
}

// Stop starts stopping a run in the app: SIGINT to its process group, SIGTERM five
// seconds later, and SIGKILL ten after that. It returns at once; the run's record says
// it stopped once it has. A run in the terminal stops there.
func (s *Service) Stop(ctx context.Context, id int64) error {
	return s.e.stop(ctx, id)
}

// RunAgain starts a run again, its command worked out again from its source by its kind's
// Rerunner, and returns the new run's ID. A release is refused: it runs again from its
// sheet.
func (s *Service) RunAgain(ctx context.Context, id int64) (int64, error) {
	rec, err := s.e.store.get(ctx, id)
	if err != nil {
		return 0, err
	}

	if rec.Kind == KindRelease {
		return 0, uierr.New("A release runs again from its sheet, never from here.", ErrNoRerun)
	}

	r := s.e.rerunner(rec.Kind)
	if r == nil {
		return 0, uierr.New("This run can't run again.", fmt.Errorf("%w: no Rerunner for %q", ErrNoRerun, rec.Kind))
	}

	spec, err := r.Rerun(ctx, rec)
	if err != nil {
		return 0, fmt.Errorf("work the run out again: %w", err)
	}

	if spec.Kind == "" {
		spec.Kind = rec.Kind
	}

	if spec.Kind != rec.Kind {
		return 0, fmt.Errorf("%w: run again as a %s, not a %s", ErrInvalidSpec, spec.Kind, rec.Kind)
	}

	return s.e.Start(ctx, spec)
}

// OpenInTerminal opens the terminal chosen for launches in a run's folder, the clone, so
// the user can look at it or finish by hand what the run left; the app runs nothing there.
func (s *Service) OpenInTerminal(ctx context.Context, id int64) error {
	rec, err := s.e.store.get(ctx, id)
	if err != nil {
		return err
	}

	if s.e.terminal == nil {
		return uierr.New("Your terminal can't be opened here.", ErrNoTerminal)
	}

	if info, statErr := os.Stat(rec.Dir); statErr != nil || !info.IsDir() {
		return uierr.New(paths.Tilde(rec.Dir, s.e.home)+" isn't there anymore.", fmt.Errorf("%w: %s", ErrNoFolder, rec.Dir))
	}

	if err = s.e.terminal.OpenFolder(ctx, fmt.Sprintf("run-%d", id), rec.Dir); err != nil {
		return fmt.Errorf("open the terminal in the run's folder: %w", err)
	}

	return nil
}

// ShowLog shows a run's log file in Finder.
func (s *Service) ShowLog(ctx context.Context, id int64) error {
	if _, err := s.e.store.get(ctx, id); err != nil {
		return err
	}

	if s.e.reveal == nil {
		return uierr.New(brand.Name+" can't open Finder here.", ErrNoReveal)
	}

	path := s.e.files.log(id)

	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return uierr.New("This run's log is gone.", fmt.Errorf("%w: run %d", ErrNoLog, id))
	}

	if err := s.e.reveal.Reveal(ctx, path); err != nil {
		return fmt.Errorf("show the run's log: %w", err)
	}

	return nil
}

// MarkSeen records that the user has seen a run.
func (s *Service) MarkSeen(ctx context.Context, id int64) error {
	if err := s.e.store.markSeen(ctx, id, s.e.clock.Now()); err != nil {
		return err
	}

	s.e.notify()

	return nil
}

// ClearHistory forgets every run that has ended, with its log. Runs going are kept.
func (s *Service) ClearHistory(ctx context.Context) error {
	return s.e.clearHistory(ctx)
}

// QuitAnyway is the user's answer to quitting while runs go in the app: stop them and
// quit. The desktop shell then lets the quit through, and shutdown stops the runs.
func (s *Service) QuitAnyway(_ context.Context) {
	if s.e.quit != nil {
		s.e.quit()
	}
}

// view returns rec as the window shows it.
func (s *Service) view(rec Record) Run {
	l := s.e.liveRun(rec.ID)

	run := Run{
		ID: rec.ID, Kind: rec.Kind, Title: rec.Title, Source: rec.Source, Repo: repoText(rec.Repo),
		Dir: paths.Tilde(rec.Dir, s.e.home), Argv: rec.Argv, Command: rec.Command, RunsIn: rec.RunsIn,
		Status: rec.Status, ExitCode: rec.ExitCode, Summary: rec.Summary, Result: rec.Result, LogBytes: rec.LogBytes,
		Seen: !rec.SeenAt.IsZero(), StartedAt: clock.Format(rec.StartedAt), FinishedAt: formatTime(rec.FinishedAt),
		CanStop:     l != nil && l.runsIn == RunsInApp && !l.settling,
		CanRunAgain: rec.Kind != KindRelease && s.e.rerunner(rec.Kind) != nil,
	}

	if rec.Status.Live() {
		run.LogBytes = s.e.files.size(rec.ID)
	}

	return run
}

// repoText writes a repository as "owner/name", or "" for none.
func repoText(name repo.Name) string {
	if name.Owner == "" && name.Repo == "" {
		return ""
	}

	return name.String()
}

// formatTime writes t as RFC 3339, or "" for the zero time.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}
