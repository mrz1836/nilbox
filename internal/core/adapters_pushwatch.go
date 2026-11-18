package core

import (
	"context"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/pushwatch"
	"github.com/mrz1836/nilbox/internal/settings"
)

// pushTargets reads the pull requests the push watch follows from the inbox's store
// (pushwatch.Targets).
type pushTargets struct {
	store *inbox.Store
}

// PushTargets returns the inbox's open pull requests whose head branch is in their own
// repository.
func (p pushTargets) PushTargets(ctx context.Context) ([]pushwatch.Target, error) {
	got, err := p.store.PushTargets(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]pushwatch.Target, 0, len(got))
	for _, t := range got {
		out = append(out, pushwatch.Target{ItemID: t.ID, Repo: t.Repo, Branch: t.Branch, Head: t.Head})
	}

	return out, nil
}

// pushClones lists the clones in the repository locations the settings name, for the
// push watch (pushwatch.Clones), reading no git.
type pushClones struct {
	settings *settings.Service
	home     string
	reader   *localrepos.Reader
}

// CloneDirs returns the folders of the clones in the clone root and the other locations.
func (c pushClones) CloneDirs(ctx context.Context) ([]string, error) {
	roots, err := releaseClones{settings: c.settings, home: c.home, reader: c.reader}.Roots(ctx)
	if err != nil {
		return nil, err
	}

	found, err := roots.Clones(ctx)
	if err != nil {
		return nil, err
	}

	return found.Dirs, nil
}

// pushWaker wakes the push watch once it is built: the inbox is built before it, and
// tells it of every change through pushesOnInbox. It is set once, before any work starts.
type pushWaker struct {
	watcher *pushwatch.Watcher
}

// Wake wakes the push watch, once there is one.
func (w *pushWaker) Wake() {
	if w.watcher != nil {
		w.watcher.Wake()
	}
}

// pushesOnInbox passes the inbox's events on, waking the push watch for an
// event.InboxUpdated, so a pull request newly listed, or a head GitHub newly reported, is
// watched at once.
type pushesOnInbox struct {
	sink   event.Sink
	pushes *pushWaker
}

// Emit passes e on, waking the push watch for an event.InboxUpdated.
func (o pushesOnInbox) Emit(e event.Event) {
	event.Or(o.sink).Emit(e)

	if _, ok := e.(event.InboxUpdated); ok {
		o.pushes.Wake()
	}
}

// pushWatchDeps is what the push watch is built from.
type pushWatchDeps struct {
	settings  *settings.Service
	store     *inbox.Store
	rereads   *inbox.Rereads
	runner    execpath.Runner
	git       *gitread.Git
	locations *localrepos.Reader
	home      string
	logger    *slog.Logger
}

// newPushWatcher builds the push watch: it follows the branches of the inbox's pull
// requests in the user's clones while Settings → Refreshing's switch is on, reads each
// clone's origin as the Release page does, a reftable clone's refs with gitread, and has
// the inbox's Rereads read a pull request again once its branch moves.
func newPushWatcher(d pushWatchDeps) *pushwatch.Watcher {
	return pushwatch.NewWatcher(pushwatch.Config{
		Targets:  pushTargets{store: d.store},
		Clones:   pushClones{settings: d.settings, home: d.home, reader: d.locations},
		Origins:  releaseOrigins{git: d.runner},
		Rereader: d.rereads,
		Switch:   d.settings,
		Git:      d.git,
		Logger:   d.logger,
	})
}
