// Package integrations finds the tools the app drives, mage-x's magex and go-pre-commit,
// says what the settings show about each, and checks once a day for a newer release.
//
// Each tool is a Definition. Detection looks for it on the user's login shell's PATH
// (shellenv), then in the well-known tool folders and go install's, runs each copy's
// --version with the tool's quiet environment, and works out where each came from. It
// runs after the window's first paint, never on the cold start, and its result is kept
// in tools.<id>.seen, which List serves at once, marked as checking, until this run's
// detection finishes. The latest releases come from one GitHub query a day, kept in
// tools.<id>.latest. This package writes the tools.* preferences, and only reads the
// integrations.* ones, which the settings write.
package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/repo"
)

const (
	// staleAfter is how old a detection may be before opening the settings looks again.
	staleAfter = 10 * time.Minute

	// updateEvery is how often the latest releases are checked.
	updateEvery = 24 * time.Hour

	// updateTick is how often the worker sees whether the daily check is due.
	updateTick = time.Hour

	// recheckAfter is how old the latest releases must be before Check again asks GitHub
	// again, so a few clicks in a row send one query.
	recheckAfter = 10 * time.Minute
)

// ShellEnv is the user's login shell's environment. *shellenv.Reader implements it.
type ShellEnv interface {
	// Env returns the environment, and whether it was read; until it was, the app's own,
	// with the well-known tool folders on PATH.
	Env() ([]string, bool)

	// Ready is closed once the first read has ended.
	Ready() <-chan struct{}

	// Read reads it again.
	Read(ctx context.Context) error
}

// Releases looks up repositories' latest releases on GitHub, keyed by each repository's
// Key. A repository with no release is left out. Core adapts the GitHub client to it.
type Releases interface {
	LatestReleases(ctx context.Context, repos []repo.Name) (map[string]Release, error)
}

// Release is a repository's latest release.
type Release struct {
	Tag         string
	URL         string
	PublishedAt time.Time
	Assets      []Asset
}

// Asset is one file of a release.
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

// Config holds a Service's dependencies. Prefs and Env are required.
type Config struct {
	// Prefs reads the integrations' settings and keeps the tools' state.
	Prefs *prefs.Store

	// Env is the user's login shell's environment.
	Env ShellEnv

	// Releases looks up the latest releases. Without it, no update is ever checked.
	Releases Releases

	// Home is the user's home folder, for the well-known tool folders and ~ in paths.
	Home string

	// Clock stamps detections and checks. It defaults to the system clock.
	Clock clock.Clock

	// Events hears an event.ToolsUpdated whenever what List returns changed.
	Events event.Sink

	// Logger gets one record per detection and per failed check.
	Logger *slog.Logger

	// Definitions are the tools. They default to Definitions().
	Definitions []Definition

	// Runner returns what runs a tool with env, the whole environment it gets. It
	// defaults to execpath.Exec, with stdin from /dev/null; tests hand in one that runs
	// nothing.
	Runner func(env []string) execpath.Runner
}

// Service says what the settings show about each tool, and looks again when asked.
type Service struct {
	prefs    *prefs.Store
	env      ShellEnv
	releases Releases
	home     string
	clock    clock.Clock
	events   event.Sink
	logger   *slog.Logger
	defs     []Definition
	run      runFunc

	// dirsFor works out where to look from the shell's environment; tests replace it, so
	// no test looks in the machine's own tool folders.
	dirsFor func(env []string, home string) searchDirs

	// wake asks the worker to detect again.
	wake chan struct{}

	// detectMu runs one detection at a time, and checkMu one update check.
	detectMu sync.Mutex
	checkMu  sync.Mutex

	// mu guards detected and detectedAt, this run's last detection, nil until one ends;
	// pending, set while a detection is asked for; and running, set while one runs.
	mu         sync.Mutex
	detected   map[string]detection
	detectedAt time.Time
	pending    bool
	running    bool
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	defs := cfg.Definitions
	if defs == nil {
		defs = Definitions()
	}

	run := execRun
	if cfg.Runner != nil {
		run = func(ctx context.Context, path string, args, env []string) ([]byte, []byte, error) {
			return cfg.Runner(env).Run(ctx, path, args...)
		}
	}

	return &Service{
		prefs: cfg.Prefs, env: cfg.Env, releases: cfg.Releases, home: cfg.Home, clock: clock.Or(cfg.Clock),
		events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger), defs: defs, run: run, dirsFor: dirsFrom,
		wake: make(chan struct{}, 1),
	}
}

// List returns what the settings show about every tool. It never waits for a detection:
// until this run's first one ends, it serves the last one kept, and while one is asked
// for or runs, the last one found; either way each tool is marked as checking, and an
// event.ToolsUpdated follows when the detection ends. A detection more than ten minutes
// old asks for a new one.
func (s *Service) List(ctx context.Context) (Tools, error) {
	stored, err := s.prefs.GetMany(ctx, s.keys()...)
	if err != nil {
		return Tools{}, fmt.Errorf("read the integrations: %w", err)
	}

	s.mu.Lock()
	detected, at := s.detected, s.detectedAt
	s.mu.Unlock()

	if detected != nil && s.clock.Now().Sub(at) > staleAfter {
		s.wakeUp()
	}

	s.mu.Lock()
	checking := detected == nil || s.pending || s.running
	s.mu.Unlock()

	cfg := integrationprefs.Decode(stored)
	out := Tools{Tools: make([]Tool, 0, len(s.defs))}

	for _, def := range s.defs {
		d, checkedAt := detected[string(def.ID)], at

		if detected == nil {
			seen := decodeSeen(stored[seenKey(def.ID)])
			d, checkedAt = seen.detection(), seen.CheckedAt
		}

		out.Tools = append(out.Tools, tool(def, cfg, d, decodeLatest(stored[latestKey(def.ID)]), checking, checkedAt, s.home))
	}

	select {
	case <-s.env.Ready():
		out.ShellChecked = true
		_, out.ShellRead = s.env.Env()
	default:
	}

	return out, nil
}

// CheckAgain reads the user's shell's environment again and looks for every tool again,
// and meanwhile checks for newer releases unless that was done in the last ten minutes,
// then returns what the settings show.
func (s *Service) CheckAgain(ctx context.Context) (Tools, error) {
	checked := make(chan struct{})

	go func() {
		defer close(checked)

		_ = s.checkUpdates(ctx, recheckAfter) // a failed check is logged, and keeps the last answer
	}()

	_ = s.env.Read(ctx) // a failed read is logged, and keeps the last good environment
	err := s.detect(ctx)

	<-checked

	if err != nil {
		return Tools{}, err
	}

	return s.List(ctx)
}

// keys returns every preference List reads.
func (s *Service) keys() []string {
	keys := integrationprefs.Keys()
	for _, def := range s.defs {
		keys = append(keys, seenKey(def.ID), latestKey(def.ID))
	}

	return keys
}

// wakeUp asks the worker to detect again, unless it already was asked.
func (s *Service) wakeUp() {
	s.mu.Lock()
	s.pending = true
	s.mu.Unlock()

	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// detect looks for every tool, keeps what it found, and emits an event.ToolsUpdated.
func (s *Service) detect(ctx context.Context) error {
	s.detectMu.Lock()
	defer s.detectMu.Unlock()

	s.mu.Lock()
	s.pending, s.running = false, true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	start := time.Now()

	stored, err := s.prefs.GetMany(ctx, integrationprefs.Keys()...)
	if err != nil {
		return fmt.Errorf("read the integrations: %w", err)
	}

	cfg := integrationprefs.Decode(stored)

	chosen := make(map[string]string, len(cfg.Paths))
	for id, path := range cfg.Paths {
		chosen[string(id)] = path
	}

	env, _ := s.env.Env()
	got := detectAll(ctx, s.defs, chosen, s.dirsFor(env, s.home), env, s.home, s.run)
	now := s.clock.Now()

	if ctx.Err() != nil {
		return ctx.Err() //nolint:wrapcheck // the caller's own context ended
	}

	values := make(map[string]string, len(got))

	found := 0

	for _, def := range s.defs {
		d := got[string(def.ID)]
		if d.has {
			found++
		}

		raw, encodeErr := json.Marshal(recordOf(d, now))
		if encodeErr != nil {
			return fmt.Errorf("keep what was found of %s: %w", def.Name, encodeErr)
		}

		values[seenKey(def.ID)] = string(raw)
	}

	if err = s.prefs.SetMany(ctx, values); err != nil {
		s.logger.WarnContext(ctx, "couldn't keep what was found of the tools", "error", err)
	}

	s.mu.Lock()
	s.detected, s.detectedAt = got, now
	s.mu.Unlock()

	s.logger.InfoContext(ctx, "looked for the tools", "duration", time.Since(start).Round(time.Millisecond),
		"found", found, "of", len(s.defs))
	s.events.Emit(event.ToolsUpdated{})

	return nil
}

// checkUpdates looks up every tool's latest release and keeps it, unless every tool was
// checked within maxAge. A failure is logged and keeps the last answer, so the next look
// tries again.
func (s *Service) checkUpdates(ctx context.Context, maxAge time.Duration) error {
	if s.releases == nil {
		return nil
	}

	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	keys := make([]string, 0, len(s.defs))
	for _, def := range s.defs {
		keys = append(keys, latestKey(def.ID))
	}

	stored, err := s.prefs.GetMany(ctx, keys...)
	if err != nil {
		return fmt.Errorf("read the latest releases: %w", err)
	}

	now := s.clock.Now()
	if !s.updatesDue(stored, now, maxAge) {
		return nil
	}

	repos := make([]repo.Name, 0, len(s.defs))
	for _, def := range s.defs {
		repos = append(repos, def.Repo)
	}

	got, err := s.releases.LatestReleases(ctx, repos)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.InfoContext(ctx, "couldn't check the tools for updates", "error", err)
		}

		return fmt.Errorf("check for updates: %w", err)
	}

	if err = s.keepLatest(ctx, got, now); err != nil {
		return err
	}

	s.events.Emit(event.ToolsUpdated{})

	return nil
}

// keepLatest keeps each tool's latest release from got, or none for a tool without one,
// as checked at now.
func (s *Service) keepLatest(ctx context.Context, got map[string]Release, now time.Time) error {
	values := make(map[string]string, len(s.defs))

	for _, def := range s.defs {
		r := got[def.Repo.Key()]
		rec := latestRecord{
			Version: cleanVersion(r.Tag), Tag: r.Tag, URL: r.URL, PublishedAt: r.PublishedAt.UTC(), Assets: r.Assets,
			CheckedAt: now.UTC(),
		}

		raw, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("keep the latest release of %s: %w", def.Name, err)
		}

		values[latestKey(def.ID)] = string(raw)
	}

	if err := s.prefs.SetMany(ctx, values); err != nil {
		return fmt.Errorf("keep the latest releases: %w", err)
	}

	return nil
}

// updatesDue reports whether any tool's latest release is unknown or maxAge old.
func (s *Service) updatesDue(stored map[string]string, now time.Time, maxAge time.Duration) bool {
	for _, def := range s.defs {
		if checked := decodeLatest(stored[latestKey(def.ID)]).CheckedAt; checked.IsZero() || now.Sub(checked) >= maxAge {
			return true
		}
	}

	return false
}

// Worker looks for the tools after the window's first paint, again when woken, and checks
// for newer releases once a day. The desktop shell runs it as deferred work; it is not
// bound.
type Worker struct {
	s    *Service
	tick time.Duration
}

// NewWorker returns the worker that keeps s current.
func NewWorker(s *Service) *Worker {
	return &Worker{s: s, tick: updateTick}
}

// Wake asks the worker to look for the tools again, as when the user flips a switch or
// chooses a copy.
func (w *Worker) Wake() {
	w.s.wakeUp()
}

// Run waits for the first read of the user's shell's environment, looks for the tools,
// checks for updates when a day has passed, and then looks again whenever woken, until
// ctx ends.
func (w *Worker) Run(ctx context.Context) {
	select {
	case <-w.s.env.Ready():
	case <-ctx.Done():
		return
	}

	w.detect(ctx)
	_ = w.s.checkUpdates(ctx, updateEvery) // logged; the next tick tries again

	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.s.wake:
			w.detect(ctx)
		case <-ticker.C:
			_ = w.s.checkUpdates(ctx, updateEvery) // logged; the next tick tries again
		}
	}
}

// detect runs a detection, logging a failure.
func (w *Worker) detect(ctx context.Context) {
	if err := w.s.detect(ctx); err != nil && ctx.Err() == nil {
		w.s.logger.WarnContext(ctx, "couldn't look for the tools", "error", err)
	}
}
