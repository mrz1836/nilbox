package core

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/notify"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/update"
)

// updateDeps are the services and building blocks the app's updates use.
type updateDeps struct {
	client     *github.Client
	runs       *scriptrun.Engine
	history    *scriptrun.Service
	dispatcher *notify.Dispatcher
	reveal     scriptrun.Revealer
}

// newUpdates builds what updates the app: the engine, over the feed and key opts' flavor
// trusts, the update.* preferences, the cache in the data folder, a backup of the database
// before every install, the runs a restart would stop, and the update_ready notification;
// and the Worker that runs its timed checks. A build that doesn't update gets both, off.
func newUpdates(pool *sql.DB, opts Options, deps updateDeps, logger *slog.Logger) (*update.Engine, *update.Worker) {
	flavor, feed, key := updateSource(opts, deps.client)

	engine := update.NewEngine(update.Config{
		Flavor: flavor, Current: opts.Release, Feed: feed, Key: key,
		Memory: prefsstore.New(pool, opts.Clock), Cache: opts.Paths.Updates, Logs: opts.Paths.Logs,
		Bundle: opts.Bundle, Home: opts.Home,
		Backup:   dataBackup{pool: pool, path: opts.Paths.DB},
		Runs:     updateRuns{engine: deps.runs, history: deps.history},
		Notifier: updateNotices{dispatcher: deps.dispatcher},
		Reveal:   deps.reveal,
		Events:   opts.Events, Clock: opts.Clock, Logger: logger,
	})

	return engine, update.NewWorker(engine)
}

// updateSource is what a build of opts' flavor reads its releases from, and the key it
// hands the engine: a release reads GitHub's latest release and trusts update.ReleaseKey
// alone, whatever opts.UpdateFeed and opts.UpdateKey say; a rehearsal reads the folder
// opts.UpdateFeed names and trusts opts.UpdateKey; any other build reads nothing.
func updateSource(opts Options, client *github.Client) (update.Flavor, update.Feed, string) {
	switch opts.Flavor {
	case update.FlavorRelease:
		return update.FlavorRelease, githubFeed{client: client}, ""
	case update.FlavorRehearsal:
		if opts.UpdateFeed == "" {
			return update.FlavorRehearsal, nil, opts.UpdateKey
		}

		return update.FlavorRehearsal, update.FolderFeed{Dir: opts.UpdateFeed}, opts.UpdateKey
	case update.FlavorSource:
		return update.FlavorSource, nil, ""
	default:
		return update.FlavorSource, nil, ""
	}
}

// githubFeed reads the releases from GitHub's release download hosts through the shared
// client's DownloadAsset (update.Feed): https only, every redirect on the download hosts,
// no token, and no API budget. Its two requests are the latest release's manifest, at most
// update.MaxManifestBytes, and a release's zip, pinned to its tag, at most
// update.MaxFileBytes.
type githubFeed struct {
	client *github.Client
}

// Manifest downloads the latest release's manifest; a latest release without one (404)
// is update.ErrNoRelease.
func (f githubFeed) Manifest(ctx context.Context) ([]byte, error) {
	var buf bytes.Buffer

	_, err := f.client.DownloadAsset(ctx, update.ManifestURL(), &buf, update.MaxManifestBytes)
	if errors.Is(err, github.ErrDownloadMissing) {
		return nil, update.ErrNoRelease
	}

	if err != nil {
		return nil, fmt.Errorf("read the latest release's update manifest: %w", err)
	}

	return buf.Bytes(), nil
}

// Download downloads the zip of the release of version into w.
func (f githubFeed) Download(ctx context.Context, version, file string, w io.Writer) error {
	if _, err := f.client.DownloadAsset(ctx, update.ZipURL(version, file), w, update.MaxFileBytes); err != nil {
		return fmt.Errorf("download the update: %w", err)
	}

	return nil
}

// dataBackup backs the database up before an install (update.DataBackup).
type dataBackup struct {
	pool *sql.DB
	path string
}

// BackUp backs the database up beside it, as the migrations do, and returns the backup.
func (b dataBackup) BackUp(ctx context.Context) (string, error) {
	return store.Backup(ctx, b.pool, b.path) //nolint:wrapcheck // store words its own failure
}

// updateRuns tells the update which runs a restart would stop (update.Runs): the runs
// going in the app, as the quit guard counts them, listed as the quit dialog names them.
type updateRuns struct {
	engine  *scriptrun.Engine
	history *scriptrun.Service
}

// Active reports whether a run is going in the app.
func (u updateRuns) Active() bool {
	return u.engine.Active()
}

// Going lists the runs going in the app, the newest first.
func (u updateRuns) Going(ctx context.Context) ([]update.Run, error) {
	runs, err := u.history.Runs(ctx, scriptrun.Filter{})
	if err != nil {
		return nil, fmt.Errorf("list the runs going: %w", err)
	}

	out := []update.Run{}

	for _, r := range runs {
		if r.RunsIn != scriptrun.RunsInApp || !r.Status.Live() {
			continue
		}

		out = append(out, update.Run{
			ID: r.ID, Kind: string(r.Kind), Title: r.Title, Repo: r.Repo, Dir: r.Dir, StopRefused: r.Progress.StopRefused,
		})
	}

	return out, nil
}

// updateNotices tells the user once that a new version is ready, or available, with a
// notification about no item (update_ready), and its banner when that kind is on
// (update.Notifier). It names no page: a click brings the window forward, where the
// update's banner shows.
type updateNotices struct {
	dispatcher *notify.Dispatcher
}

// UpdateFound records n as a notification, and shows its banner.
func (u updateNotices) UpdateFound(ctx context.Context, n update.Notice) {
	u.dispatcher.Notify(ctx, updateEntry(n))
}

// updateEntry is the notification of n.
func updateEntry(n update.Notice) notify.Entry {
	if n.Ready {
		return notify.Entry{
			Kind: notify.KindUpdateReady, Title: brand.Name + " " + n.Version + " is ready",
			Body: "Restart to update when you're ready.",
		}
	}

	return notify.Entry{
		Kind: notify.KindUpdateReady, Title: brand.Name + " " + n.Version + " is available",
		Body: "Update when you're ready.",
	}
}
