package update

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/progress"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// ErrNothingToDownload is returned by Download when no newer version is known.
var ErrNothingToDownload = errors.New("no newer version to download")

// tick is the Worker's look at whether something is due: a check, once CheckEvery has
// passed on the wall clock since the last one, downloading what it finds as the setting
// says; or, with downloads on and a version found that isn't downloaded, the download. With
// Don't check it does nothing.
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

	if m := e.toDownload(); mode == updateprefs.ModeDownload && m != nil {
		if err = e.download(ctx, *m); err != nil {
			return err
		}

		e.tell(ctx, Notice{Version: m.Version, Ready: true})
	}

	return nil
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

// background runs a timed check, the caller holding the operation: with downloads on, a
// version found is downloaded and the user told it is ready; with Just tell me, told it is
// available. Each version is told once.
func (e *Engine) background(ctx context.Context, mode updateprefs.Mode) error {
	m, found, err := e.check(ctx)
	if err != nil || !found {
		return err
	}

	if mode == updateprefs.ModeDownload {
		if e.needsDownload(m) {
			if err = e.download(ctx, m); err != nil {
				return err
			}
		}

		e.tell(ctx, Notice{Version: m.Version, Ready: true})

		return nil
	}

	e.tell(ctx, Notice{Version: m.Version, Ready: !e.needsDownload(m)})

	return nil
}

// checkNow is a check the user asked for, which works in every mode: it reads the latest
// release, and a newer version it finds is marked told without a notification, since the
// user is looking at the result. With downloads on, the Worker downloads it at once.
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

	e.markTold(ctx, m.Version)

	if e.mode(ctx) == updateprefs.ModeDownload && e.needsDownload(m) {
		e.wakeWorker()
	}

	return nil
}

// downloadNow downloads the newer version found, as Update asks with Just tell me before
// it restarts. The caller sees a failure, which the status's LastError keeps too.
func (e *Engine) downloadNow(ctx context.Context) error {
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
		return uierr.New("Couldn't download "+st.found.Version+": "+uierr.Message(err), err)
	}

	e.markTold(ctx, st.found.Version)

	return nil
}

// check reads the latest release and takes what it says, the caller holding the
// operation: a newer version it verifies is found, and returned with found set; no
// release, or none newer, means up to date, and an update waiting that the releases no
// longer offer is dropped. A failure is kept as LastError and changes nothing else; only a
// check that reached the releases counts as the last check.
func (e *Engine) check(ctx context.Context) (Manifest, bool, error) {
	e.change(func(st *state) { st.checking = true })
	defer e.change(func(st *state) { st.checking = false })

	m, found, err := e.latest(ctx)
	if err != nil {
		e.failed(ctx, "the update check failed", err)

		return Manifest{}, false, err
	}

	if err = e.memory.SetMany(ctx, map[string]string{KeyLastCheck: clock.Format(e.clock.Now())}); err != nil {
		e.logger.WarnContext(ctx, "could not remember when the update check ran", "error", err)
	}

	if !found {
		e.change(func(st *state) { st.found, st.ready, st.lastErr = nil, nil, "" })
		e.prune(ctx, "")

		return Manifest{}, false, nil
	}

	e.change(func(st *state) { st.found, st.lastErr = &m, "" })

	return m, true, nil
}

// latest reads the latest release's manifest and returns it, with found set, once it checks
// out and is newer than the running build; found is false when there is none, or none
// newer.
func (e *Engine) latest(ctx context.Context) (Manifest, bool, error) {
	data, err := e.feed.Manifest(ctx)
	if errors.Is(err, ErrNoRelease) {
		return Manifest{}, false, nil
	}

	if err != nil {
		return Manifest{}, false, uierr.New("Couldn't reach the releases: "+uierr.Message(err), err)
	}

	m, err := Parse(data)
	if err != nil {
		return Manifest{}, false, uierr.New("The latest release's update manifest can't be read.", err)
	}

	if err = accept(m, e.key); err != nil {
		return Manifest{}, false, uierr.New("The latest release's update didn't check out, so it wasn't used.", err)
	}

	return m, newer(m.Version, e.current), nil
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

	e.change(func(st *state) { st.downloading, st.written, st.ready, st.lastErr = nil, 0, &entry, "" })
	e.prune(ctx, m.Version)

	return nil
}

// downloadWords says why a download failed.
func downloadWords(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "it took longer than " + DownloadLimit.String() + "."
	case errors.Is(err, ErrMismatch):
		return "it didn't match the release's manifest, so it was thrown away."
	default:
		return uierr.Message(err)
	}
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
