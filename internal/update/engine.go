package update

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// The update.* preferences this package alone writes: the app's memory of its updates,
// which a backup never holds. The setting, update.mode, is the settings package's.
const (
	// KeyLastCheck is when the last check that reached the releases ended, as RFC 3339.
	KeyLastCheck = "update.lastCheck"

	// KeyNotified is the last version a notification was sent for (or that a manual check
	// showed), so each version is told once.
	KeyNotified = "update.notified"

	// KeyBannerHidden is the banner hidden with Later: {version, until}. A newer version
	// shows it again.
	KeyBannerHidden = "update.bannerHiddenUntil"

	// KeyLastRunRelease is the version the app last ran as, set on every start once the
	// outcome of the last restart is read.
	KeyLastRunRelease = "update.lastRunRelease"

	// KeyPendingInstall is the install a restart began, {from, to, at, pid}, set just
	// before the app quits for it and read and cleared at the next start.
	KeyPendingInstall = "update.pendingInstall"
)

// Timing.
const (
	// FirstCheckAfter is how long after the deferred work starts the first check runs, so
	// a check never lands on the cold start.
	FirstCheckAfter = time.Minute

	// TickEvery is how often the Worker looks whether a check is due. Timers don't advance
	// while a Mac sleeps, so the wall clock decides, at each tick, against CheckEvery.
	TickEvery = 15 * time.Minute

	// CheckEvery is how long after the last check the next is due.
	CheckEvery = 6 * time.Hour

	// DownloadLimit bounds a whole download; GitHub's download hosts stop one that stalls
	// for 30 seconds sooner.
	DownloadLimit = 10 * time.Minute

	// ManifestLimit bounds a read of the latest release's manifest, which holds the one
	// operation while it runs.
	ManifestLimit = time.Minute

	// HideFor is how long Later hides the banner.
	HideFor = 24 * time.Hour

	// BackupReuse is how long a backup a restart took serves a restart tried again, so
	// retries never push out the older backups the restore needs.
	BackupReuse = 10 * time.Minute

	// HelperWindow is how long after a restart the runtime's helper may still be at work
	// (30 seconds waiting for the app to quit, up to 90 checking the new bundle's
	// signature, its swaps and restores): while its log hasn't ended, the start leaves its
	// files, and the record of the install, alone until then.
	HelperWindow = 3 * time.Minute
)

// offInSource is why a build made from source doesn't update.
const offInSource = "Updates are off in builds made from source."

// Memory is where the engine keeps its state, the update.* preferences, and reads the
// setting: a *prefs.Store.
type Memory interface {
	GetMany(ctx context.Context, keys ...string) (map[string]string, error)
	SetMany(ctx context.Context, values map[string]string) error
	Delete(ctx context.Context, key string) error
}

// Installer installs an update and restarts into it: the desktop shell's adapter over the
// desktop runtime's updater, which stages the zip beside the running bundle and swaps it in
// after the app quits.
type Installer interface {
	// Install stages the update in zip, of version, whose size and SHA-512 (hex) the
	// staging checks the zip against as it copies it.
	Install(ctx context.Context, zip, version string, size int64, sha512 string) error

	// Restart starts the runtime's helper, waits for it to stand by, and quits the app,
	// which the helper waits for before it swaps the bundles and opens the new one. The
	// process exits inside it: it returns only when something stopped it first.
	Restart(ctx context.Context) error
}

// Runs says which runs are going in the app, which a restart would stop: core adapts the
// runs' engine.
type Runs interface {
	// Active reports whether a run is going in the app. It never waits.
	Active() bool

	// Going lists the runs going in the app.
	Going(ctx context.Context) ([]Run, error)
}

// Run is a run going in the app, as the window names it when it asks before a restart.
type Run struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`

	// Repo is the run's repository, "owner/name", or "", and Dir its folder, with ~ for
	// home.
	Repo string `json:"repo"`
	Dir  string `json:"dir"`

	// StopRefused says why the run can't be stopped right now, or "": quitting then waits a
	// few seconds for it.
	StopRefused string `json:"stopRefused"`
}

// DataBackup backs up the database before an install: core adapts store.Backup. It
// returns the backup's path.
type DataBackup interface {
	BackUp(ctx context.Context) (string, error)
}

// Notifier tells the user once that a new version is ready, or available: core records a
// notification about no item (update_ready), with its banner.
type Notifier interface {
	UpdateFound(ctx context.Context, n Notice)
}

// Notice is a new version the user is told of.
type Notice struct {
	// Version is the new version.
	Version string

	// Ready reports that it is downloaded and checked, waiting for a restart; otherwise it
	// is only available.
	Ready bool
}

// Revealer shows a file in Finder: core's open -R.
type Revealer interface {
	Reveal(ctx context.Context, path string) error
}

// Config holds an Engine's dependencies. Memory is required for any flavor that updates.
type Config struct {
	// Flavor is the kind of build; an empty or unknown one is FlavorSource, which never
	// updates.
	Flavor Flavor

	// Current is the running release's version (main.release).
	Current string

	// Feed is where the releases are read from: GitHub's for a release, a folder for a
	// rehearsal.
	Feed Feed

	// Key is the public key a rehearsal trusts, as ReleaseKey holds one. A release
	// ignores it and trusts ReleaseKey alone.
	Key string

	// Memory keeps the engine's state and reads the setting.
	Memory Memory

	// Cache is the folder the downloaded updates are kept in (appdata.Paths.Updates), and
	// Logs the app's logs folder, where a failed install's helper log is kept.
	Cache string
	Logs  string

	// Bundle is the running app's bundle, as the desktop shell finds it, or "" when it
	// runs outside one; Home is the home folder. Applications is the Applications folder
	// every user shares, /Applications when "" (tests point it at a folder of their own).
	Bundle       string
	Home         string
	Applications string

	// RehearsalFolder is a folder a rehearsal build updates itself in, as it would in an
	// Applications folder: the one magex app:rehearse builds it into, which core works out
	// from the rehearsal's feed, so a rehearsal runs end to end without touching an
	// Applications folder. Every other flavor ignores it: a release updates itself only in
	// an Applications folder.
	RehearsalFolder string

	// TempDir is where the runtime's helper writes its log and readiness files
	// (os.TempDir() when ""), and PID the running app's process (os.Getpid() when 0),
	// which names the helper's log.
	TempDir string
	PID     int

	// Backup backs up the database before an install; Runs says which runs a restart
	// would stop; Notifier tells the user of a new version; Reveal shows a file in Finder.
	Backup   DataBackup
	Runs     Runs
	Notifier Notifier
	Reveal   Revealer

	// Writable reports whether the user may create files in a folder; nil asks the system.
	Writable func(dir string) bool

	// Events hears event.UpdatesUpdated whenever the status changes, and, around a
	// restart, event.QuitConfirmed and event.UpdateRestartFailed. Clock stamps the checks
	// and the banner's Later. Logger hears what the engine can't report.
	Events event.Sink
	Clock  clock.Clock
	Logger *slog.Logger
}

// Engine checks for, downloads, and installs the app's updates. It is never bound: the
// window reaches it through Service, the desktop shell attaches the Installer, and the
// Worker runs its timed checks.
//
// One operation runs at a time (a check, a download, or a restart), so checks never
// overlap; the status a window reads is the engine's memory and the update.* preferences,
// never the network.
type Engine struct {
	flavor  Flavor
	off     string
	current string
	key     ed25519.PublicKey
	feed    Feed
	cache   *Cache

	memory   Memory
	logs     string
	bundle   string
	home     string
	tempDir  string
	pid      int
	backup   DataBackup
	runs     Runs
	notifier Notifier
	reveal   Revealer
	events   event.Sink
	clock    clock.Clock
	logger   *slog.Logger
	location Location

	// op is held by the one operation running.
	op chan struct{}

	// settleMu guards settled, which is set once the start's outcome, cleanup, and cache
	// load have run.
	settleMu sync.Mutex
	settled  bool

	// mu guards the fields below.
	mu         sync.Mutex
	st         state
	installer  Installer
	wake       func()
	lastBackup backupTaken
}

// backupTaken is the backup the last restart took: where, and when.
type backupTaken struct {
	path string
	at   time.Time
}

// state is the engine's memory of where the update stands.
type state struct {
	checking    bool
	downloading *Manifest
	written     int64
	installing  bool

	// found is the newest release the last check verified, newer than the running build;
	// ready is the update downloaded and checked, in the cache.
	found *Manifest
	ready *Entry

	lastErr     string
	justUpdated string
	failed      InstallFailure

	// failedDownload names the zip (downloadKey) whose download the Worker tried last and
	// that failed, which only a due check fetches again; quiet is the version a manual check
	// showed found, which is marked told without a notification once it is ready.
	failedDownload string
	quiet          string

	// quitHeld is set once a restart's Installer returned with the app still running: the
	// runtime refuses another restart until the app quits, so this run installs nothing more.
	quitHeld bool
}

// NewEngine returns an Engine over cfg. A source build, a release with no key pinned, or a
// rehearsal without its key or folder is off: it reads, downloads, and installs nothing,
// and its status says why.
func NewEngine(cfg Config) *Engine {
	e := &Engine{
		current: cfg.Current, memory: cfg.Memory, logs: cfg.Logs, bundle: cfg.Bundle, home: cfg.Home,
		tempDir: cfg.TempDir, pid: cfg.PID, backup: cfg.Backup, runs: cfg.Runs, notifier: cfg.Notifier,
		reveal: cfg.Reveal, events: event.Or(cfg.Events), clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger),
		op: make(chan struct{}, 1),
	}

	if e.tempDir == "" {
		e.tempDir = os.TempDir()
	}

	if e.pid == 0 {
		e.pid = os.Getpid()
	}

	writable := cfg.Writable
	if writable == nil {
		writable = canWrite
	}

	applications := cfg.Applications
	if applications == "" {
		applications = systemApplications
	}

	// A rehearsal alone updates itself in its own folder too.
	rehearsal := ""
	if cfg.Flavor == FlavorRehearsal {
		rehearsal = cfg.RehearsalFolder
	}

	e.location = locate(cfg.Bundle, cfg.Home, applications, rehearsal, writable)
	e.flavor, e.key, e.off = resolve(cfg)
	e.feed = cfg.Feed

	if e.off == "" {
		e.cache = NewCache(cfg.Cache, e.key)
	}

	return e
}

// resolve works out what a build of cfg's flavor trusts, or why it is off: a source build
// never updates; a release trusts ReleaseKey alone, whatever cfg.Key says; a rehearsal
// trusts cfg.Key. Each needs a feed, the store of its memory, and a release's version to
// compare a newer one with.
func resolve(cfg Config) (Flavor, ed25519.PublicKey, string) {
	flavor, key := cfg.Flavor, cfg.Key

	switch flavor {
	case FlavorRelease:
		key = ReleaseKey
	case FlavorRehearsal:
	case FlavorSource:
		return FlavorSource, nil, offInSource
	default:
		return FlavorSource, nil, offInSource
	}

	if !ValidVersion(cfg.Current) {
		return flavor, nil, "Updates are off: this build's version isn't a release's."
	}

	if cfg.Feed == nil || cfg.Memory == nil || cfg.Cache == "" {
		return flavor, nil, "Updates are off: this build has nowhere to read them from."
	}

	if key == "" {
		return flavor, nil, "Updates are off: this build has no release key pinned to check them with."
	}

	public, err := runtimeKey(key)
	if err != nil {
		return flavor, nil, "Updates are off: this build's release key can't be read."
	}

	return flavor, public, ""
}

// Enabled reports whether this build updates.
func (e *Engine) Enabled() bool {
	return e.off == ""
}

// Attach hands the engine the Installer it restarts with. The desktop shell calls it once
// the desktop runtime is created; until then, and in a build with none, Restart refuses.
func (e *Engine) Attach(installer Installer) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.installer = installer
}

// Status returns the update as the window shows it, from the engine's memory and the
// update.* preferences.
func (e *Engine) Status(ctx context.Context) (Status, error) {
	out := Status{
		Flavor: e.flavor, Enabled: e.Enabled(), Off: e.off, Current: e.current, Mode: updateprefs.DefaultMode,
		State: StateOff, Location: e.location,
	}

	if !e.Enabled() {
		// A build that doesn't update still says what the user chose, as the bug report does.
		out.Mode = e.storedMode(ctx)

		return out, nil
	}

	stored, err := e.memory.GetMany(ctx, updateprefs.KeyMode, KeyLastCheck, KeyBannerHidden)
	if err != nil {
		return Status{}, fmt.Errorf("read the update's status: %w", err)
	}

	out.Mode = updateprefs.Decode(stored[updateprefs.KeyMode])
	out.LastChecked = stored[KeyLastCheck]

	e.mu.Lock()
	st := e.st
	e.mu.Unlock()

	out.State, out.Latest = st.phase()
	out.Checking, out.LastError, out.JustUpdated, out.InstallFailed = st.checking, st.lastErr, st.justUpdated, st.failed

	if st.downloading != nil && st.downloading.Size > 0 {
		out.Progress = int(min(100, st.written*100/st.downloading.Size))
	}

	switch {
	case out.Latest != "":
		out.ReleaseURL = ReleaseURL(out.Latest)
	default:
		out.ReleaseURL = ReleaseURL(e.current)
	}

	hidden := decodeHidden(stored[KeyBannerHidden])
	out.BannerHidden = out.Latest != "" && hidden.Version == out.Latest && e.clock.Now().Before(hidden.Until)

	return out, nil
}

// storedMode reads the mode the user chose, or the default when it can't be read.
func (e *Engine) storedMode(ctx context.Context) updateprefs.Mode {
	if e.memory == nil {
		return updateprefs.DefaultMode
	}

	stored, err := e.memory.GetMany(ctx, updateprefs.KeyMode)
	if err != nil {
		return updateprefs.DefaultMode
	}

	return updateprefs.Decode(stored[updateprefs.KeyMode])
}

// phase works out the state and the version it is about from what the engine holds.
func (st state) phase() (State, string) {
	switch {
	case st.installing && st.ready != nil:
		return StateInstalling, st.ready.Manifest.Version
	case st.downloading != nil:
		return StateDownloading, st.downloading.Version
	case st.found != nil && (st.ready == nil || st.ready.Manifest.Version != st.found.Version):
		return StateAvailable, st.found.Version
	case st.ready != nil:
		return StateReady, st.ready.Manifest.Version
	case st.checking:
		return StateChecking, ""
	default:
		return StateIdle, ""
	}
}

// acquire waits for the operation running, if any, and returns what lets go of the one
// starting, or ctx's error once ctx ends first.
func (e *Engine) acquire(ctx context.Context) (func(), error) {
	select {
	case e.op <- struct{}{}:
		return func() { <-e.op }, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for the update's last step: %w", ctx.Err())
	}
}

// change applies fn to the engine's state under its lock, and tells the window.
func (e *Engine) change(fn func(st *state)) {
	e.mu.Lock()
	fn(&e.st)
	e.mu.Unlock()

	e.events.Emit(event.UpdatesUpdated{})
}

// view returns a copy of the engine's state.
func (e *Engine) view() state {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.st
}

// mode reads the setting; one that can't be read counts as the default, logged.
func (e *Engine) mode(ctx context.Context) updateprefs.Mode {
	stored, err := e.memory.GetMany(ctx, updateprefs.KeyMode)
	if err != nil {
		if ctx.Err() == nil {
			e.logger.WarnContext(ctx, "could not read what to do with a new version; using the default", "error", err)
		}

		return updateprefs.DefaultMode
	}

	return updateprefs.Decode(stored[updateprefs.KeyMode])
}

// bannerHidden is the Later the user chose: the version it hid, and until when.
type bannerHidden struct {
	Version string    `json:"version"`
	Until   time.Time `json:"until"`
}

// decodeHidden reads KeyBannerHidden; anything unreadable hides nothing.
func decodeHidden(stored string) bannerHidden {
	var h bannerHidden
	if stored == "" || json.Unmarshal([]byte(stored), &h) != nil {
		return bannerHidden{}
	}

	return h
}

// pendingInstall is the install a restart began.
type pendingInstall struct {
	From string    `json:"from"`
	To   string    `json:"to"`
	At   time.Time `json:"at"`
	PID  int       `json:"pid"`
}

// tilde writes path with ~ for home.
func (e *Engine) tilde(path string) string {
	return paths.Tilde(path, e.home)
}

// parseTime reads a time stored as RFC 3339; ok is false for none or another text.
func parseTime(stored string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, stored)

	return t, err == nil
}
