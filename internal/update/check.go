package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/progress"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

var (
	// ErrNothingToDownload is returned by Download when no newer version is known.
	ErrNothingToDownload = errors.New("no newer version to download")

	// ErrLocation is returned by Download and Restart while the app runs where it can't
	// replace itself: a translocated copy, a disk image, or a folder the user can't write to.
	ErrLocation = errors.New("the app runs where it can't update itself")
)

// tick is the Worker's look at whether something is due: a check, once CheckEvery has
// passed on the wall clock since the last one, downloading what it finds when the update
// downloads on its own; or, then, a version found that isn't downloaded, unless its
// download failed already, which only the next due check tries again. With Don't check it
// does nothing.
func (e *Engine) tick(ctx context.Context) error {
	if !e.Enabled() {
		return nil
	}

	mode := e.mode(ctx)
	if !mode.Checks() {
		return nil
	}

	due, err := e.checkDue(ctx)
	if err != nil {
		return err
	}

	release, err := e.acquire(ctx)
	if err != nil {
		return err
	}

	defer release()

	if due {
		return e.background(ctx, mode)
	}

	m := e.toDownload()
	if m == nil || !e.downloads(mode) || e.failedBefore(*m) {
		return nil
	}

	return e.fetch(ctx, *m)
}

// downloads reports whether the update downloads on its own: with Download it and tell me,
// where the app can replace itself. Elsewhere a check only tells.
func (e *Engine) downloads(mode updateprefs.Mode) bool {
	return mode == updateprefs.ModeDownload && e.location.OK
}

// checkDue reports whether CheckEvery has passed since the last check, by the wall clock,
// or the clock went back past it; a check that never ran is due.
func (e *Engine) checkDue(ctx context.Context) (bool, error) {
	stored, err := e.memory.GetMany(ctx, KeyLastCheck)
	if err != nil {
		return false, fmt.Errorf("read when the last update check ran: %w", err)
	}

	last, ok := parseTime(stored[KeyLastCheck])
	if !ok {
		return true, nil
	}

	now := e.clock.Now()

	return now.Sub(last) >= CheckEvery || now.Before(last), nil
}

// background runs a timed check, the caller holding the operation. When the update
// downloads on its own, a version found is downloaded, a failed one included (once per due
// check), and the user told it is ready; otherwise told it is available. Each version is
// told once.
func (e *Engine) background(ctx context.Context, mode updateprefs.Mode) error {
	m, found, err := e.check(ctx)
	if err != nil || !found {
		return err
	}

	if !e.downloads(mode) {
		e.tell(ctx, Notice{Version: m.Version, Ready: !e.needsDownload(m)})

		return nil
	}

	if e.needsDownload(m) {
		return e.fetch(ctx, m)
	}

	e.toldReady(ctx, m.Version)

	return nil
}

// fetch downloads m on the Worker's behalf, the caller holding the operation, and tells the
// user it is ready. A download that fails is remembered, so the ticks between due checks
// never fetch it again, and the user, who never saw it ready, is told once a later one
// works.
func (e *Engine) fetch(ctx context.Context, m Manifest) error {
	if err := e.download(ctx, m); err != nil {
		e.change(func(st *state) {
			st.failedDownload = downloadKey(m)

			if st.quiet == m.Version {
				st.quiet = ""
			}
		})

		return err
	}

	e.toldReady(ctx, m.Version)

	return nil
}

// toldReady tells the user version is ready, once: quietly, marked told alone, when a
// manual check showed it found.
func (e *Engine) toldReady(ctx context.Context, version string) {
	if e.view().quiet == version {
		e.markTold(ctx, version)

		return
	}

	e.tell(ctx, Notice{Version: version, Ready: true})
}

// failedBefore reports whether m's download failed last time it was fetched.
func (e *Engine) failedBefore(m Manifest) bool {
	return e.view().failedDownload == downloadKey(m)
}

// downloadKey names a release's zip: its version and SHA-512.
func downloadKey(m Manifest) string {
	return m.Version + " " + m.SHA512
}

// checkNow is a check the user asked for, which works in every mode. A newer version it
// finds that is available or ready at once is marked told without a notification, since
// the user is looking at the result; one the Worker downloads now (downloads on) is marked
// told, quietly, only once it is ready, so a download that fails still lets a later timed
// check tell of it.
func (e *Engine) checkNow(ctx context.Context) error {
	if e.view().downloading != nil {
		return nil
	}

	release, err := e.acquire(ctx)
	if err != nil {
		return err
	}

	// A check that failed is told by the status's LastError, not as this call's error.
	m, found, _ := e.check(ctx)

	release()

	if !found {
		return nil
	}

	if e.downloads(e.mode(ctx)) && e.needsDownload(m) {
		e.change(func(st *state) { st.quiet = m.Version })
		e.wakeWorker()

		return nil
	}

	e.markTold(ctx, m.Version)

	return nil
}

// downloadNow downloads the newer version found, as Update asks with Just tell me before
// it restarts, a download that failed before included. The caller sees a failure, which
// the status's LastError keeps too.
func (e *Engine) downloadNow(ctx context.Context) error {
	if !e.location.OK {
		return locationError()
	}

	release, err := e.acquire(ctx)
	if err != nil {
		return err
	}

	defer release()

	st := e.view()
	if st.found == nil {
		return uierr.New("There's no new version to download. Check for updates first.", ErrNothingToDownload)
	}

	if !e.needsDownload(*st.found) {
		return nil
	}

	if err = e.download(ctx, *st.found); err != nil {
		return uierr.New("Couldn't download "+st.found.Version+": "+downloadWords(err), err)
	}

	e.markTold(ctx, st.found.Version)

	return nil
}

// locationError refuses what the app can't do where it runs.
func locationError() error {
	return uierr.New("Move "+brand.Name+" to Applications to update it.", ErrLocation)
}

// check reads the latest release and takes what it says, the caller holding the
// operation: a newer version it verifies is found, and returned with found set; no
// release, or none newer, means up to date, and an update waiting that the releases no
// longer offer is dropped (deleting a release's manifest withdraws it). A failure is kept
// as LastError and changes nothing else. A check counts as the last check once it reached
// the releases, a manifest it refused included.
func (e *Engine) check(ctx context.Context) (Manifest, bool, error) {
	e.change(func(st *state) { st.checking = true })
	defer e.change(func(st *state) { st.checking = false })

	m, found, reached, err := e.latest(ctx)
	if reached {
		if setErr := e.memory.SetMany(ctx, map[string]string{KeyLastCheck: clock.Format(e.clock.Now())}); setErr != nil {
			e.logger.WarnContext(ctx, "could not remember when the update check ran", "error", setErr)
		}
	}

	if err != nil {
		e.failed(ctx, "the update check failed", err)

		return Manifest{}, false, err
	}

	if !found {
		e.change(func(st *state) { st.found, st.ready, st.lastErr = nil, nil, "" })
		e.prune(ctx, "")

		return Manifest{}, false, nil
	}

	e.change(func(st *state) { st.found, st.lastErr = &m, "" })

	return m, true, nil
}

// latest reads the latest release's manifest, within ManifestLimit, and returns it, with
// found set, once it checks out and is newer than the running build; found is false when
// there is none, or none newer. reached reports that the releases answered, a manifest
// refused included.
func (e *Engine) latest(ctx context.Context) (m Manifest, found, reached bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, ManifestLimit)
	defer cancel()

	data, err := e.feed.Manifest(ctx)
	if errors.Is(err, ErrNoRelease) {
		return Manifest{}, false, true, nil
	}

	if err != nil {
		return Manifest{}, false, false, uierr.New("Couldn't reach the releases: "+reachWords(err), err)
	}

	m, err = Parse(data)
	if err != nil {
		return Manifest{}, false, true, uierr.New("The latest release's update manifest can't be read.", err)
	}

	if err = accept(m, e.key); err != nil {
		return Manifest{}, false, true, uierr.New("The latest release's update didn't check out, so it wasn't used.", err)
	}

	return m, newer(m.Version, e.current), true, nil
}

// needsDownload reports whether m isn't the update downloaded already.
func (e *Engine) needsDownload(m Manifest) bool {
	st := e.view()

	return st.ready == nil || st.ready.Manifest.Version != m.Version
}

// toDownload is the version found that isn't downloaded, or nil.
func (e *Engine) toDownload() *Manifest {
	st := e.view()
	if st.found == nil || (st.ready != nil && st.ready.Manifest.Version == st.found.Version) {
		return nil
	}

	m := *st.found

	return &m
}

// download fetches m's zip into the cache, the caller holding the operation, within
// DownloadLimit, telling the window how far it has got a few times a second. A zip that
// doesn't match m is thrown away; one that does is ready, and the cache keeps it alone.
func (e *Engine) download(ctx context.Context, m Manifest) error {
	ctx, cancel := context.WithTimeout(ctx, DownloadLimit)
	defer cancel()

	tracker := progress.New(e.events, progress.Every, func(string, int, int) event.Event { return event.UpdatesUpdated{} })
	tracker.Start("download", int(m.Size))

	e.change(func(st *state) { st.downloading, st.written = &m, 0 })

	entry, err := e.cache.Put(m, func(w io.Writer) error {
		return e.feed.Download(ctx, m.Version, m.File, w)
	}, func(written int64) {
		e.mu.Lock()
		delta := written - e.st.written
		e.st.written = written
		e.mu.Unlock()

		tracker.Add(int(delta))
	})

	tracker.End()

	if err != nil {
		e.change(func(st *state) { st.downloading, st.written = nil, 0 })
		e.failed(ctx, "the update's download failed", uierr.New("Couldn't download "+m.Version+": "+downloadWords(err), err))

		return err
	}

	e.change(func(st *state) {
		st.downloading, st.written, st.ready, st.lastErr, st.failedDownload = nil, 0, &entry, "", ""
	})
	e.prune(ctx, m.Version)

	return nil
}

// tryAgain is why a read of the releases failed when nothing says more.
const tryAgain = "check the connection, then try again."

// reachWords says why the latest release's manifest couldn't be read. Like downloadWords,
// it never repeats a raw error, which could name a path on this Mac or a URL: only a time
// limit, or words a feed gave its failure (uierr).
func reachWords(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "they didn't answer within " + ManifestLimit.String() + "."
	case worded(err):
		return uierr.Message(err)
	default:
		return tryAgain
	}
}

// downloadWords says why a download failed, never repeating a raw error.
func downloadWords(err error) string {
	var pathErr *fs.PathError

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "it took longer than " + DownloadLimit.String() + "."
	case errors.Is(err, ErrMismatch):
		return "it didn't match the release's manifest, so it was thrown away."
	case worded(err):
		return uierr.Message(err)
	case errors.As(err, &pathErr):
		return "it couldn't be saved on this Mac."
	default:
		return tryAgain
	}
}

// worded reports whether err carries words for the user (a uierr.Error in its chain).
func worded(err error) bool {
	_, ok := errors.AsType[*uierr.Error](err)

	return ok
}

// tell tells the user of n once per version, through the Notifier.
func (e *Engine) tell(ctx context.Context, n Notice) {
	stored, err := e.memory.GetMany(ctx, KeyNotified)
	if err != nil {
		e.logger.WarnContext(ctx, "could not read which update was told; telling nothing", "error", err)

		return
	}

	if stored[KeyNotified] == n.Version {
		return
	}

	if !e.markTold(ctx, n.Version) || e.notifier == nil {
		return
	}

	e.notifier.UpdateFound(ctx, n)
}

// markTold remembers that the user was told of version, and reports whether it could.
func (e *Engine) markTold(ctx context.Context, version string) bool {
	if err := e.memory.SetMany(ctx, map[string]string{KeyNotified: version}); err != nil {
		e.logger.WarnContext(ctx, "could not remember the update told; telling nothing", "error", err)

		return false
	}

	return true
}

// failed keeps err as LastError and logs it, unless ctx ended.
func (e *Engine) failed(ctx context.Context, what string, err error) {
	if ctx.Err() != nil {
		return
	}

	e.logger.WarnContext(ctx, what, "error", err)
	e.change(func(st *state) { st.lastErr = uierr.Message(err) })
}

// prune keeps keep's folder in the cache alone, "" none; a failure is logged.
func (e *Engine) prune(ctx context.Context, keep string) {
	if err := e.cache.Prune(keep); err != nil {
		e.logger.WarnContext(ctx, "could not prune the updates kept", "error", err)
	}
}

// setWake hands the engine what wakes the Worker.
func (e *Engine) setWake(wake func()) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.wake = wake
}

// wakeWorker wakes the Worker, if one runs.
func (e *Engine) wakeWorker() {
	e.mu.Lock()
	wake := e.wake
	e.mu.Unlock()

	if wake != nil {
		wake()
	}
}
