package scriptrun

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// What the engine does for one kind at a time, the agents' runs above all: Stop all,
// which stops every run of a kind and leaves the engine open; the folders the runs going
// work in, which the worktrees and the launcher count as in use; the run's own folder,
// which goes with its log; and the agents' history, kept and cleared apart.

// folderSweepAfter is how old a folder in Config.Folders that no run names must be before
// the engine removes it: a run's Before makes its folder before the run is recorded.
const folderSweepAfter = time.Hour

// LiveRun is a run going now, as the folders it works in are counted: one the engine
// started, or one from before the app restarted that it follows again.
type LiveRun struct {
	ID    int64
	Kind  Kind
	Title string

	// Dir is the folder it runs in, and PID its program's process, or 0 while it starts.
	Dir string
	PID int

	// Orphaned reports a run from before the app restarted.
	Orphaned bool
}

// Live returns the runs of kind going now, in the app or from before the app restarted,
// those still starting included: every folder an agent works in, though the process list
// leaves the app's own children and an orphan out. A run whose end is recorded is no
// longer going.
func (e *Engine) Live(kind Kind) []LiveRun {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := []LiveRun{}

	for id, l := range e.live {
		if l.kind != kind || l.settling || l.ended {
			continue
		}

		out = append(out, LiveRun{
			ID: id, Kind: l.kind, Title: l.title, Dir: l.dir, PID: l.pid, Orphaned: l.proc == nil && l.runsIn == RunsInApp,
		})
	}

	return out
}

// StopKind stops every run of kind going in the app, from before the app restarted too, as
// Stop does each, and returns how many it stopped: Stop all for the agents' runs. Unlike
// StopAll, the quit path, it leaves the engine open, so a run can start after it, and it
// leaves every other kind's runs alone. A run whose kind refuses to stop now is left going.
func (e *Engine) StopKind(ctx context.Context, kind Kind) (int, error) {
	live := e.Live(kind)
	ids := make([]int64, 0, len(live))

	for _, r := range live {
		ids = append(ids, r.ID)
	}

	stopped := 0

	var errs []error

	for _, id := range ids {
		err := e.stop(ctx, id)

		switch {
		case err == nil:
			stopped++
		case errors.Is(err, ErrNotRunning), errors.Is(err, ErrNotFound):
		default:
			errs = append(errs, err)
		}
	}

	if err := errors.Join(errs...); err != nil {
		return stopped, uierr.New("Some runs couldn't be stopped.", err)
	}

	return stopped, nil
}

// LetFinish says whether the runs that may outlive a quit (Spec.OutlivesQuit) are let go as
// the app quits rather than stopped: the user's "Let them finish". StopAll reads it.
func (e *Engine) LetFinish(on bool) {
	e.letFinish.Store(on)
}

// ClearAgentHistory forgets every agent's run that has ended, with its log and its folder,
// as Autopilot's Clear history does; the runs going are kept, and so is every other kind's.
func (e *Engine) ClearAgentHistory(ctx context.Context) error {
	e.pruneMu.Lock()
	defer e.pruneMu.Unlock()

	recs, err := e.store.list(ctx)
	if err != nil {
		return err
	}

	var drop []Record

	for _, rec := range recs {
		if rec.Kind == KindAgent && !rec.Status.Live() {
			drop = append(drop, rec)
		}
	}

	err = e.forget(ctx, drop)

	e.notify()

	return err
}

// LogKept reports whether the history still holds run id's log: false once the retention
// or Clear history forgot the run.
func (e *Engine) LogKept(ctx context.Context, id int64) bool {
	if _, err := e.store.get(ctx, id); err != nil {
		return false
	}

	_, err := os.Stat(e.files.log(id))

	return err == nil
}

// openStdin opens the file a run reads on its stdin.
func openStdin(path string) (*os.File, error) {
	f, err := os.Open(path) //nolint:gosec // the run's own file, under the app's data folder
	if err != nil {
		return nil, fmt.Errorf("open the run's input: %w", err)
	}

	return f, nil
}

// ownFolder reports whether dir is directly inside Config.Folders, where a run's own
// folder must be.
func (e *Engine) ownFolder(dir string) bool {
	if e.folders == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return false
	}

	return filepath.Dir(dir) == filepath.Clean(e.folders) && filepath.Base(dir) != "." && filepath.Base(dir) != ".."
}

// clearFolder removes a run's own folder, once it is sure the folder is one: directly
// inside Config.Folders.
func (e *Engine) clearFolder(dir string) error {
	if dir == "" || !e.ownFolder(dir) {
		return nil
	}

	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the run's folder: %w", err)
	}

	return nil
}

// sweepFolders removes the folders in Config.Folders that no run names, as after the
// database was set aside, once they are older than folderSweepAfter: a run's Before makes
// its folder before the run is recorded. The caller holds regMu.
func (e *Engine) sweepFolders(ctx context.Context, recs []Record) {
	if e.folders == "" {
		return
	}

	entries, err := os.ReadDir(e.folders)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}

	if err != nil {
		e.logger.WarnContext(ctx, "couldn't read the runs' own folders", "error", err)

		return
	}

	named := map[string]bool{}
	for _, rec := range recs {
		named[rec.Result.Folder] = true
	}

	for _, entry := range entries {
		dir := filepath.Join(e.folders, entry.Name())
		if named[dir] || !e.stale(entry) {
			continue
		}

		if err = e.clearFolder(dir); err != nil {
			e.logger.WarnContext(ctx, "couldn't remove a forgotten run's folder", "error", err)
		}
	}
}

// stale reports a folder older than folderSweepAfter.
func (e *Engine) stale(entry fs.DirEntry) bool {
	info, err := entry.Info()

	return entry.IsDir() && err == nil && e.clock.Now().Sub(info.ModTime()) >= folderSweepAfter
}

// agentLogsFor returns how long an agent's run that ended is kept.
func (e *Engine) agentLogsFor(ctx context.Context) time.Duration {
	if e.logsFor == nil {
		return DefaultAgentLogsFor
	}

	if d := e.logsFor(ctx); d > 0 {
		return d
	}

	return DefaultAgentLogsFor
}
