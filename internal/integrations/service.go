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
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations/internal/queries"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/toolinstall"
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

	// DB holds tool_installs, the record of what the app installed. Without it no copy
	// reads as installed by the app, and nothing is recorded.
	DB *sql.DB

	// Installer installs releases. Without it, nothing is installed.
	Installer *toolinstall.Installer

	// Runs runs a tool's own updater, for a copy that can update itself, as a run the
	// window follows. Without it, no tool updates itself: the Installer does every update.
	Runs Runs

	// Locks is the app's one set of clone locks, which the runs hold too: the Installer
	// holds the folder it places a program in, so it never meets a tool's own update there.
	// Without it, the service keeps a set of its own.
	Locks *clonelock.Set

	// Commands runs Finder's scripts and open, for the Trash and Reveal in Finder.
	Commands execpath.Runner

	// InUse reports whether a run is using a tool, which an install then waits for.
	// Without it, none is.
	InUse func(id integrationprefs.ID) bool

	// GOOS and GOARCH pick a release's archive. They default to this build's.
	GOOS, GOARCH string
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

	queries      *queries.Queries
	installer    *toolinstall.Installer
	runs         Runs
	locks        *clonelock.Set
	commands     execpath.Runner
	inUse        func(id integrationprefs.ID) bool
	goos, goarch string

	// installMu runs one install at a time.
	installMu sync.Mutex

	// wake asks the worker to detect again.
	wake chan struct{}

	// detectMu runs one detection at a time, and checkMu one update check.
	detectMu sync.Mutex
	checkMu  sync.Mutex

	// mu guards detected and detectedAt, this run's last detection, nil until one ends,
	// and goBins, go install's folders as it found them; pending, set while a detection is
	// asked for; and running, set while one runs.
	mu         sync.Mutex
	detected   map[string]detection
	detectedAt time.Time
	goBins     goFolders
	pending    bool
	running    bool

	// installing is each running install's progress, updates each tool's last run of its
	// own updater since the app started, and hashes the files hashed, by path; signedOut
	// reports that the last update check found no one signed in to GitHub. mu guards them,
	// and the runs' own fields.
	installing map[integrationprefs.ID]InstallProgress
	updates    map[integrationprefs.ID]*selfRun
	hashes     map[string]cachedHash
	signedOut  bool
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

	s := &Service{
		prefs: cfg.Prefs, env: cfg.Env, releases: cfg.Releases, home: cfg.Home, clock: clock.Or(cfg.Clock),
		events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger), defs: defs, run: run, dirsFor: dirsFrom,
		wake: make(chan struct{}, 1), installer: cfg.Installer, runs: cfg.Runs, commands: cfg.Commands, inUse: cfg.InUse,
		goos: cmp.Or(cfg.GOOS, runtime.GOOS), goarch: cmp.Or(cfg.GOARCH, runtime.GOARCH),
	}

	if cfg.DB != nil {
		s.queries = queries.New(cfg.DB)
	}

	if s.locks = cfg.Locks; s.locks == nil {
		s.locks = clonelock.New()
	}

	if s.inUse == nil {
		s.inUse = func(integrationprefs.ID) bool { return false }
	}

	return s
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

		latest := decodeLatest(stored[latestKey(def.ID)])
		t := tool(def, cfg, d, latest, checking, checkedAt, s.home)
		t.Install = s.plan(def, d, latest, t.State)

		t.Progress, t.SelfUpdate = s.progressOf(def.ID)

		out.Tools = append(out.Tools, t)
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

// progressOf returns how far the tool's install has got, updating while its own updater
// runs, and that updater's last run since the app started, or the one the runner follows
// from before the app restarted.
func (s *Service) progressOf(id integrationprefs.ID) (InstallProgress, SelfUpdate) {
	if run := s.orphanedUpdate(id); run != 0 {
		return InstallProgress{Step: StepUpdating}, SelfUpdate{Run: run, Running: true}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	r := s.updates[id]

	switch {
	case r == nil:
		return s.installing[id], SelfUpdate{}
	case r.running:
		return InstallProgress{Step: StepUpdating}, r.view()
	default:
		return s.installing[id], r.view()
	}
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
	return s.detectThen(ctx, nil)
}

// detectThen is detect, handing seen what the look found before its copies are matched
// with the installs recorded, where a tool's own update records the copy it swapped in.
func (s *Service) detectThen(ctx context.Context, seen func(map[string]detection)) error {
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
	dirs := s.dirsFor(env, s.home)
	s.addGoEnv(ctx, env, &dirs)

	got := detectAll(ctx, s.defs, chosen, dirs, env, s.home, s.run)
	now := s.clock.Now()

	if seen != nil {
		seen(got)
	}

	s.settle(ctx, got, dirs.goBins)

	if ctx.Err() != nil {
		return ctx.Err() //nolint:wrapcheck // the caller's own context ended
	}

	found, err := s.keep(ctx, got, now)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.detected, s.detectedAt, s.goBins = got, now, dirs.goBins
	s.mu.Unlock()

	s.logger.InfoContext(ctx, "looked for the tools", "duration", time.Since(start).Round(time.Millisecond),
		"found", found, "of", len(s.defs))
	s.events.Emit(event.ToolsUpdated{})

	return nil
}

// settle marks each copy the app installed, then works out, once per detection, whether
// the copy in use updates itself.
func (s *Service) settle(ctx context.Context, got map[string]detection, bins goFolders) {
	for _, def := range s.defs {
		d := got[string(def.ID)]
		s.markInstalled(ctx, &d)
		d.selfUpdates = d.has && s.updatesItself(def, d.selected, bins)
		got[string(def.ID)] = d
	}
}

// keep keeps what a detection found of each tool, for the next start, and returns how many
// were found. A failed write is logged: the detection still stands for this run.
func (s *Service) keep(ctx context.Context, got map[string]detection, now time.Time) (int, error) {
	values := make(map[string]string, len(got))
	found := 0

	for _, def := range s.defs {
		d := got[string(def.ID)]
		if d.has {
			found++
		}

		raw, err := json.Marshal(recordOf(d, now))
		if err != nil {
			return 0, fmt.Errorf("keep what was found of %s: %w", def.Name, err)
		}

		values[seenKey(def.ID)] = string(raw)
	}

	if err := s.prefs.SetMany(ctx, values); err != nil {
		s.logger.WarnContext(ctx, "couldn't keep what was found of the tools", "error", err)
	}

	return found, nil
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

	s.mu.Lock()
	s.signedOut = errors.Is(err, ErrSignedOut)
	s.mu.Unlock()

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
	w.s.cleanup()
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
