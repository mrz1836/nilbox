package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
)

// maxHelperLog is how much of the end of the runtime's helper log is kept.
const maxHelperLog = 1 << 20

// settle reads how the last restart to update went, cleans up what updates leave behind,
// and loads the update kept in the cache, once per run, in that order, so the helper's log
// is read before the cleanup removes it. The Worker runs it first, and every bound action
// before it acts. A build that doesn't update does nothing.
func (e *Engine) settle(ctx context.Context) {
	e.settled.Do(func() {
		if !e.Enabled() {
			return
		}

		pid := e.readOutcome(ctx)

		for _, err := range cleanUp(e.bundle, e.tempDir, pid, e.clock.Now()) {
			e.logger.WarnContext(ctx, "could not clean up after an update", "error", err)
		}

		e.loadCache(ctx)
		e.events.Emit(event.UpdatesUpdated{})
	})
}

// readOutcome reads the install the last restart began, if any, and words how it went:
// the running build is the version it installed (JustUpdated), or isn't (InstallFailed,
// with the helper's log kept in the logs folder as update-<version>.log). With no install
// recorded, a build newer than the one that ran last was installed by hand: that's
// JustUpdated too. It then records this build as the one that ran last, clears the record,
// and returns the process the record names, whose helper files the cleanup removes.
func (e *Engine) readOutcome(ctx context.Context) int {
	stored, err := e.memory.GetMany(ctx, KeyPendingInstall, KeyLastRunRelease)
	if err != nil {
		e.logger.WarnContext(ctx, "could not read how the last update went", "error", err)

		return 0
	}

	pending, hasPending := decodePending(stored[KeyPendingInstall])
	updated, failed := e.outcome(ctx, pending, hasPending, stored[KeyLastRunRelease])

	e.mu.Lock()
	e.st.justUpdated, e.st.failed = updated, failed
	e.mu.Unlock()

	switch {
	case hasPending && failed.Version == "":
		e.logger.InfoContext(ctx, "updated", "from", pending.From, "to", e.current)
	case hasPending:
		e.logger.WarnContext(ctx, "the last update didn't install", "version", failed.Version, "reason", failed.Reason)
	}

	e.rememberRun(ctx, stored[KeyPendingInstall] != "")

	return pending.PID
}

// outcome works out what the start tells: the version this launch updated to, or the
// update the last restart didn't install.
func (e *Engine) outcome(ctx context.Context, p pendingInstall, hasPending bool, last string) (string, InstallFailure) {
	switch {
	case hasPending && p.To == e.current:
		return e.current, InstallFailure{}
	case hasPending:
		return "", e.keepHelperLog(ctx, p)
	case last != "" && newer(e.current, last):
		return e.current, InstallFailure{}
	default:
		return "", InstallFailure{}
	}
}

// rememberRun records this build as the one that ran last, and clears the record of the
// last install when there was one.
func (e *Engine) rememberRun(ctx context.Context, clearPending bool) {
	if err := e.memory.SetMany(ctx, map[string]string{KeyLastRunRelease: e.current}); err != nil {
		e.logger.WarnContext(ctx, "could not remember the version that ran", "error", err)
	}

	if !clearPending {
		return
	}

	if err := e.memory.Delete(ctx, KeyPendingInstall); err != nil {
		e.logger.WarnContext(ctx, "could not clear the install's record", "error", err)
	}
}

// decodePending reads KeyPendingInstall; ok is false when there is none, or it names no
// release.
func decodePending(stored string) (pendingInstall, bool) {
	var p pendingInstall
	if stored == "" || json.Unmarshal([]byte(stored), &p) != nil || !ValidVersion(p.To) {
		return pendingInstall{}, false
	}

	return p, true
}

// keepHelperLog copies the end of the helper's log of the install p into the logs folder,
// as update-<version>.log, and words why the install failed from it. A missing log is
// worded as one.
func (e *Engine) keepHelperLog(ctx context.Context, p pendingInstall) InstallFailure {
	out := InstallFailure{Version: p.To}

	data, err := readTail(helperLog(e.tempDir, p.PID), maxHelperLog)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			e.logger.WarnContext(ctx, "could not read the update's log", "error", err)
		}

		out.Reason = "The update didn't install, and left no log saying why."

		return out
	}

	out.Reason = helperReason(data)

	if e.logs == "" {
		return out
	}

	if err = appdata.WriteFile(e.installLog(p.To), data); err != nil {
		e.logger.WarnContext(ctx, "could not keep the update's log", "error", err)

		return out
	}

	out.Log = true

	return out
}

// installLog is where the helper's log of the install of version is kept.
func (e *Engine) installLog(version string) string {
	return filepath.Join(e.logs, "update-"+version+".log")
}

// loadCache takes the update kept in the cache, checked again, as the one ready.
func (e *Engine) loadCache(ctx context.Context) {
	entry, ok, err := e.cache.Load(e.current)
	if err != nil {
		e.logger.WarnContext(ctx, "could not tidy the updates kept", "error", err)
	}

	if !ok {
		return
	}

	m := entry.Manifest

	e.mu.Lock()
	e.st.ready, e.st.found = &entry, &m
	e.mu.Unlock()
}

// readTail reads the last limit bytes of the regular file at path.
func readTail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // the runtime's helper log, named for a process of the app's
	if err != nil {
		return nil, fmt.Errorf("read the update's log: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read the update's log: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read the update's log: %w", appdata.ErrNotAFile)
	}

	if size := info.Size(); size > limit {
		if _, err = f.Seek(size-limit, io.SeekStart); err != nil {
			return nil, fmt.Errorf("read the update's log: %w", err)
		}
	}

	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, fmt.Errorf("read the update's log: %w", err)
	}

	return data, nil
}

// helperReason words why an install failed from the runtime's helper's log, whose lines
// are "<time>: <message>" and never print the exit code. The messages it ends on tell the
// code apart: a swap that failed and whose restore failed too (14, 16, 20) left a broken
// install; every other kept or put back the old version. Nothing of the log, its paths
// included, is repeated.
func helperReason(log []byte) string {
	broken := bytes.Contains(log, []byte("restore failed"))

	for _, ending := range helperEndings() {
		if !bytes.Contains(log, []byte(ending.message)) {
			continue
		}

		if broken && ending.broken != "" {
			return ending.broken + " Rename " + brand.Name + ".app.bak to " + brand.Name +
				".app in its folder, or install " + brand.Name + " again."
		}

		return ending.words
	}

	return "The update didn't install, and its log doesn't say why."
}

// helperEnding is a message the runtime's helper logs on its way out, and what it means:
// words, or, when a restore failed after it (the helper's codes 14, 16, and 20, which leave
// a broken install), broken.
type helperEnding struct {
	message string
	words   string
	broken  string
}

// helperEndings are the helper's endings, the most telling first.
func helperEndings() []helperEnding {
	return []helperEnding{
		{message: "helper done", words: "The update installed, but this isn't the copy of " + brand.Name + " it replaced."},
		{message: "parent did not exit within timeout", words: brand.Name + " didn't quit in time, so nothing was changed."},
		{
			message: "staged update is not installable",
			words:   "The downloaded update couldn't be installed, so nothing was changed.",
		},
		{message: "backup failed", words: brand.Name + " couldn't be copied aside first, so nothing was changed."},
		{
			message: "installed application failed validation",
			words:   "The new version failed its signature check, so the old one was put back.",
			broken:  "The new version failed its signature check, and the old one couldn't be put back.",
		},
		{
			message: "launch new failed",
			words:   "The new version wouldn't open, so the old one was put back.",
			broken:  "The new version wouldn't open, and the old one couldn't be put back.",
		},
		{
			message: "all swap attempts exhausted",
			words:   "macOS didn't let " + brand.Name + " be replaced, so the old version was kept.",
			broken:  "macOS didn't let " + brand.Name + " be replaced, and the old version couldn't be put back.",
		},
		{message: "stat target failed", words: "The update's files were missing, so nothing was changed."},
		{message: "stat new failed", words: "The update's files were missing, so nothing was changed."},
	}
}
