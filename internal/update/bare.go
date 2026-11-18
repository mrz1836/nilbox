package update

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Way is what a build can do about data a newer build saved, which it can't open: the
// startup dialog offers each its own way forward.
type Way string

// The ways.
const (
	// WayUpdate is a build that updates itself where it runs, a release in an Applications
	// folder: it checks the latest release and installs one newer than itself (Bare).
	WayUpdate Way = "update"

	// WayInstall is a release that can't update itself where it runs (outside every
	// Applications folder, or with updates off): the latest release is installed by hand,
	// with the README's install command.
	WayInstall Way = "install"

	// WaySource is a build made from source, which never downloads an update: it is pulled
	// and built again.
	WaySource Way = "source"
)

// BareConfig holds a Bare's dependencies: the Engine's Config without anything the
// database keeps.
type BareConfig struct {
	// Flavor is the kind of build, Current its release's version, Feed where the releases
	// are read from, and Key the public key a rehearsal trusts, as Config's.
	Flavor  Flavor
	Current string
	Feed    Feed
	Key     string

	// Cache is the folder the downloaded updates are kept in (appdata.Paths.Updates).
	Cache string

	// Bundle, Home, Applications, RehearsalFolder, and Writable say where the app runs and
	// whether it can replace itself there, as Config's.
	Bundle          string
	Home            string
	Applications    string
	RehearsalFolder string
	Writable        func(dir string) bool

	// Logger hears what the update did.
	Logger *slog.Logger
}

// Bare updates the app from the startup dialog that data a newer build saved stops at,
// before anything opens the database, and without it: the Engine's check, download, and
// checks, through the same Feed, Cache, key, and Installer, and nothing it keeps in the
// database. It reads and writes no update.* preference, takes no backup (the database is
// never touched: the newer version opens it as it is), records no install for the next
// start to read, and tells no one but the dialog. It runs only on the user's click, and a
// build from source never reads anything.
type Bare struct {
	flavor   Flavor
	off      string
	current  string
	key      ed25519.PublicKey
	feed     Feed
	cache    *Cache
	bundle   string
	location Location
	logger   *slog.Logger
}

// NewBare returns a Bare over cfg. What it trusts, and whether it is off, is the Engine's
// rule (resolve): a source build is off, a release trusts ReleaseKey alone, and a
// rehearsal its own key.
func NewBare(cfg BareConfig) *Bare {
	writable := cfg.Writable
	if writable == nil {
		writable = canWrite
	}

	applications := cfg.Applications
	if applications == "" {
		applications = systemApplications
	}

	rehearsal := ""
	if cfg.Flavor == FlavorRehearsal {
		rehearsal = cfg.RehearsalFolder
	}

	b := &Bare{
		current: cfg.Current, feed: cfg.Feed, bundle: cfg.Bundle, logger: logging.Or(cfg.Logger),
		location: locate(cfg.Bundle, cfg.Home, applications, rehearsal, writable),
	}

	b.flavor, b.key, b.off = trust(cfg.Flavor, cfg.Current, cfg.Key, cfg.Feed != nil && cfg.Cache != "")

	if b.off == "" {
		b.cache = NewCache(cfg.Cache, b.key)
	}

	return b
}

// Way is what this build can do about data a newer build saved: update itself where it
// can, a build from source be built again, and any other build be installed again by hand.
func (b *Bare) Way() Way {
	switch {
	case b.flavor == FlavorSource:
		return WaySource
	case b.off == "" && b.location.OK:
		return WayUpdate
	default:
		return WayInstall
	}
}

// Location is where the app runs, as the Engine's status tells it.
func (b *Bare) Location() Location {
	return b.location
}

// Latest reads the latest release's manifest, within ManifestLimit, as a check does, and
// returns it, with found set, once it checks out and is newer than this build; found is
// false when there is no release, or none newer, which no published release opens then.
// A failure is worded with no raw error, path, or URL in it. Only a build that updates
// here (WayUpdate) reads anything.
func (b *Bare) Latest(ctx context.Context) (Manifest, bool, error) {
	if err := b.refuse(); err != nil {
		return Manifest{}, false, err
	}

	m, found, _, err := readLatest(ctx, b.feed, b.key, b.current)
	if err != nil {
		b.logger.WarnContext(ctx, "the check for an update the data needs failed", "error", err)

		return Manifest{}, false, err
	}

	b.logger.InfoContext(ctx, "checked for an update the data needs", "latest", m.Version, "newer", found)

	return m, found, nil
}

// Update installs m, a newer release Latest found, in place of this build, and restarts
// into it, as a restart to update does: m checked again, its zip downloaded into the cache
// within DownloadLimit (or the one the cache holds already, which checks out), checked
// against m once more, staged by installer beside the bundle, and the app restarted by it,
// inside which the process exits. It backs nothing up and records nothing. A failure
// before the restart removes the runtime's staging folder, leaves the app as it was, and
// is worded; a download that failed leaves nothing of it in the cache. It returns nil
// only when installer.Restart did, the app still running: something held its quit, and the
// runtime's helper waits for this process to exit to install the update.
func (b *Bare) Update(ctx context.Context, m Manifest, installer Installer) error {
	if err := b.refuse(); err != nil {
		return err
	}

	if err := accept(m, b.key); err != nil || !newer(m.Version, b.current) {
		return uierr.New("The update didn't check out, so it wasn't used.",
			fmt.Errorf("%w: %s isn't a release newer than %s", ErrNothingToDownload, m.Version, b.current))
	}

	if installer == nil {
		return uierr.New("This build can't install updates.", ErrNoInstaller)
	}

	entry, err := b.fetch(ctx, m)
	if err != nil {
		return err
	}

	if err = checkZip(entry); err != nil {
		b.prune(ctx)

		return uierr.New("The downloaded "+m.Version+" was damaged, so it was thrown away.", fmt.Errorf("%w: %w", ErrDamaged, err))
	}

	if err = installer.Install(ctx, entry.Zip, m.Version, m.Size, m.SHA512); err != nil {
		return b.undo(ctx, m.Version, uierr.New("Couldn't get "+m.Version+" ready to install.", err))
	}

	b.logger.InfoContext(ctx, "restarting into the update the data needs", "from", b.current, "to", m.Version)

	if err = installer.Restart(ctx); err != nil {
		return b.undo(ctx, m.Version, uierr.New("Couldn't restart into "+m.Version+".", err))
	}

	return nil
}

// refuse says why this build doesn't update here, or returns nil when it does.
func (b *Bare) refuse() error {
	switch {
	case b.flavor == FlavorSource:
		return uierr.New(offInSource, ErrOff)
	case b.off != "":
		return uierr.New(b.off, ErrOff)
	case !b.location.OK:
		return locationError()
	default:
		return nil
	}
}

// fetch returns m's entry in the cache: the one there when it is m's and checks out, or
// else m's zip downloaded into it, within DownloadLimit. A download that fails leaves
// nothing of m in the cache, and is worded.
func (b *Bare) fetch(ctx context.Context, m Manifest) (Entry, error) {
	if kept, err := b.cache.check(m.Version, b.current); err == nil && kept.Manifest.SHA512 == m.SHA512 {
		b.logger.InfoContext(ctx, "the update the data needs is downloaded already", "version", m.Version)

		return kept, nil
	}

	ctx, cancel := context.WithTimeout(ctx, DownloadLimit)
	defer cancel()

	b.logger.InfoContext(ctx, "downloading the update the data needs", "version", m.Version, "size", m.Size)

	entry, err := b.cache.Put(m, func(w io.Writer) error {
		return b.feed.Download(ctx, m.Version, m.File, w)
	}, nil)
	if err != nil {
		err = uierr.New("Couldn't download "+m.Version+": "+downloadWords(err), err)
		b.logger.WarnContext(ctx, "the download of the update the data needs failed", "version", m.Version, "error", err)

		return Entry{}, err
	}

	return entry, nil
}

// undo takes back an update that failed before the app quit: the runtime's staging folder
// beside the bundle is removed, and the download stays in the cache for another try. It
// logs err whole and returns it.
func (b *Bare) undo(ctx context.Context, version string, err error) error {
	b.logger.WarnContext(ctx, "the update the data needs didn't install", "version", version, "error", err)

	for _, removeErr := range removeStaging(b.bundle) {
		b.logger.WarnContext(ctx, "could not remove the update's staging folder", "error", removeErr)
	}

	return err
}

// prune drops every update the cache holds; a failure is logged.
func (b *Bare) prune(ctx context.Context) {
	if err := b.cache.Prune(""); err != nil {
		b.logger.WarnContext(ctx, "could not prune the updates kept", "error", err)
	}
}
