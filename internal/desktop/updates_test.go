package desktop_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// nextVersion is the version the update tests offer, newer than the 0.2.0 running.
const nextVersion = "0.3.0"

// errFakeReleases stands in for a check that couldn't reach the releases.
var errFakeReleases = errors.New("fake: GitHub didn't answer")

// fakeUpdates is the update as the menu bar sees it: a status, and a record of what was
// asked of it.
type fakeUpdates struct {
	log *journal

	mu       sync.Mutex
	st       update.Status
	checked  update.Status // what a check leaves
	checkErr error
}

func (f *fakeUpdates) Status(context.Context) (update.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.st, nil
}

func (f *fakeUpdates) CheckNow(context.Context) (update.Status, error) {
	f.log.add("update checked")

	f.mu.Lock()
	defer f.mu.Unlock()

	f.st = f.checked

	return f.st, f.checkErr
}

func (f *fakeUpdates) set(st update.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.st = st
}

// updating is the status of a build that updates, running 0.2.0, as change adjusts it.
func updating(change func(st *update.Status)) update.Status {
	st := update.Status{
		Flavor: update.FlavorRelease, Enabled: true, Current: "0.2.0", Mode: updateprefs.ModeDownload,
		State: update.StateIdle,
	}

	if change != nil {
		change(&st)
	}

	return st
}

// ready is 0.3.0, downloaded and waiting for a restart.
func ready(st *update.Status) { st.State, st.Latest = update.StateReady, nextVersion }

// checkRow is the menu's Check for Updates…, enabled.
func checkRow() menubar.UpdateRow {
	return menubar.UpdateRow{Label: "Check for Updates…", Action: menubar.UpdateCheck}
}

// updateRows returns the update's rows of the menu as last drawn.
func updateRows(t *testing.T, r *rig) []menubar.UpdateRow {
	t.Helper()

	draws := r.tray.drawn()
	require.NotEmpty(t, draws, "the menu was drawn")

	return draws[len(draws)-1].Updates.Rows
}

// withUpdates is a shell change that gives it u.
func withUpdates(u desktop.Updates) func(*desktop.Config) {
	return func(cfg *desktop.Config) { cfg.Updates = u }
}

func TestTheMenuOffersTheUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		st   update.Status
		want []menubar.UpdateRow
	}{
		{name: "a build that doesn't update offers nothing", st: update.Status{State: update.StateOff}},
		{name: "up to date", st: updating(nil), want: []menubar.UpdateRow{checkRow()}},
		{name: "a version ready", st: updating(ready), want: []menubar.UpdateRow{
			checkRow(), {Label: "Restart to Update (0.3.0)", Action: menubar.UpdateRestart},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newRig()
				s := r.shell(withUpdates(&fakeUpdates{log: r.log, st: tt.st}))

				s.Start(t.Context())
				s.Launched()
				synctest.Wait()

				assert.Equal(t, tt.want, updateRows(t, r))

				require.NoError(t, s.Stop())
			})
		})
	}

	t.Run("a shell with no update offers nothing", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			r := newRig()
			s := r.shell()

			s.Start(t.Context())
			s.Launched()
			synctest.Wait()

			assert.Empty(t, updateRows(t, r))
			s.UpdateAction(menubar.UpdateCheck)
			synctest.Wait()

			require.NoError(t, s.Stop())
		})
	})
}

func TestAnUpdateChangeRedrawsOnlyWhatTheMenuShows(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		u := &fakeUpdates{log: r.log, st: updating(nil)}
		s := r.shell(withUpdates(u))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()
		require.Len(t, r.tray.drawn(), 1)

		s.Emit(event.UpdatesUpdated{})
		synctest.Wait()
		assert.Len(t, r.tray.drawn(), 1, "nothing the menu shows changed")

		u.set(updating(func(st *update.Status) { st.State, st.Latest, st.Progress = update.StateDownloading, nextVersion, 10 }))
		s.Emit(event.UpdatesUpdated{})
		synctest.Wait()
		assert.Len(t, r.tray.drawn(), 2)
		assert.Equal(t, []menubar.UpdateRow{checkRow(), {Label: "Downloading 0.3.0…"}}, updateRows(t, r))

		u.set(updating(func(st *update.Status) { st.State, st.Latest, st.Progress = update.StateDownloading, nextVersion, 60 }))
		s.Emit(event.UpdatesUpdated{})
		synctest.Wait()
		assert.Len(t, r.tray.drawn(), 2, "the download getting further changes no row")

		u.set(updating(ready))
		s.Emit(event.UpdatesUpdated{})
		synctest.Wait()
		assert.Len(t, r.tray.drawn(), 3)

		assert.Len(t, r.page.events(), 4, "the window hears every change")

		require.NoError(t, s.Stop())
	})
}

func TestTheMenusCheckShowsItsResultForAMinute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		checked update.Status
		err     error
		want    menubar.UpdateRow
	}{
		{
			name:    "up to date",
			checked: updating(nil),
			want:    menubar.UpdateRow{Label: "Up to date (0.2.0)", Tooltip: "Checked just now."},
		},
		{
			name:    "a check that failed",
			checked: updating(func(st *update.Status) { st.LastError = "Couldn't reach the releases." }),
			want:    menubar.UpdateRow{Label: "Check failed", Tooltip: "Couldn't reach the releases."},
		},
		{
			name:    "a check refused",
			checked: updating(nil),
			err:     uierr.New("Updates are off in this build.", errFakeReleases),
			want:    menubar.UpdateRow{Label: "Check failed", Tooltip: "Updates are off in this build."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newRig()
				u := &fakeUpdates{log: r.log, st: updating(nil), checked: tt.checked, checkErr: tt.err}
				s := r.shell(withUpdates(u))

				s.Start(t.Context())
				s.Launched()
				synctest.Wait()

				s.UpdateAction(menubar.UpdateCheck)
				synctest.Wait()

				assert.Equal(t, []string{"update checked"}, r.lines("update"))
				assert.Equal(t, []menubar.UpdateRow{checkRow(), tt.want}, updateRows(t, r))

				time.Sleep(menubar.NoteFor)
				synctest.Wait()
				assert.Equal(t, []menubar.UpdateRow{checkRow(), tt.want}, updateRows(t, r), "not before the minute is up")

				time.Sleep(time.Second)
				synctest.Wait()
				assert.Equal(t, []menubar.UpdateRow{checkRow()}, updateRows(t, r), "then the line is gone")

				require.NoError(t, s.Stop())
			})
		})
	}
}

func TestTheMenusRestartAndUpdateAskTheWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		action menubar.UpdateAction
		st     update.Status
		row    menubar.UpdateRow
	}{
		{
			action: menubar.UpdateRestart, st: updating(ready),
			row: menubar.UpdateRow{Label: "Restart to Update (0.3.0)", Action: menubar.UpdateRestart},
		},
		{
			action: menubar.UpdateDownload,
			st:     updating(func(st *update.Status) { st.State, st.Latest = update.StateAvailable, nextVersion }),
			row:    menubar.UpdateRow{Label: "Update to 0.3.0…", Action: menubar.UpdateDownload},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newRig()
				u := &fakeUpdates{log: r.log, st: tt.st, checked: tt.st}
				s := r.shell(withUpdates(u))

				s.Start(t.Context())
				s.Launched()
				synctest.Wait()

				// A check's line shows until the next thing the menu asks.
				s.UpdateAction(menubar.UpdateCheck)
				synctest.Wait()
				require.Len(t, updateRows(t, r), 2, "the check found the version it offered")

				shownBefore := r.window.shown()

				s.UpdateAction(tt.action)
				synctest.Wait()

				assert.Equal(t, shownBefore+1, r.window.shown(), "the window comes forward")
				assert.Contains(t, r.page.events(), event.WindowEvent(event.UpdateRestartRequested{}),
					"and is asked to restart, as its banner does")
				assert.Equal(t, []string{"update checked"}, r.lines("update"),
					"nothing downloads or restarts but in the window")
				assert.Equal(t, []menubar.UpdateRow{checkRow(), tt.row}, updateRows(t, r), "the check's line is gone")

				require.NoError(t, s.Stop())

				s.UpdateAction(tt.action)
				synctest.Wait()
				assert.Equal(t, shownBefore+1, r.window.shown(), "a stopped shell asks nothing")
			})
		})
	}
}

func TestTheMenusTryAgainAsksTheWindow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		u := &fakeUpdates{log: r.log, st: updating(func(st *update.Status) {
			ready(st)
			st.InstallFailed = update.InstallFailure{
				Version: nextVersion, Reason: "The app didn't quit in time.", Retryable: true,
			}
		})}
		s := r.shell(withUpdates(u))

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		again := menubar.UpdateRow{Label: "Try Updating to 0.3.0 Again", Action: menubar.UpdateRestart}
		assert.Equal(t, []menubar.UpdateRow{
			checkRow(),
			{Label: "0.3.0 didn't install", Tooltip: "The app didn't quit in time."},
			again,
		}, updateRows(t, r))

		shownBefore := r.window.shown()

		s.UpdateAction(again.Action)
		synctest.Wait()

		assert.Equal(t, shownBefore+1, r.window.shown(), "the window comes forward")
		assert.Contains(t, r.page.events(), event.WindowEvent(event.UpdateRestartRequested{}),
			"and runs its one restart, as its banner's Try again does")
		assert.Empty(t, r.lines("update"), "nothing downloads or restarts but in the window")

		require.NoError(t, s.Stop())
	})
}

// memory is the update's state, the update.* preferences, in a map.
type memory struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memory) GetMany(_ context.Context, keys ...string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := map[string]string{}

	for _, k := range keys {
		if v, ok := m.values[k]; ok {
			out[k] = v
		}
	}

	return out, nil
}

func (m *memory) SetMany(_ context.Context, values map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	maps.Copy(m.values, values)

	return nil
}

func (m *memory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.values, key)

	return nil
}

// runs are the runs going in the app, as the update asks about them.
type runs struct {
	going []update.Run
}

func (r runs) Active() bool { return len(r.going) > 0 }

func (r runs) Going(context.Context) ([]update.Run, error) { return r.going, nil }

// installer records what the update's installer was asked to do.
type installer struct {
	log *journal

	// held has Restart return with the app running, as when something held the quit;
	// otherwise it waits, as the app would exit inside it, until ctx ends.
	held bool

	// returnsAfter is how long a held Restart takes to come back once it started the
	// helper, as a quit slow to return.
	returnsAfter time.Duration
}

func (i installer) Install(_ context.Context, _, version string, _ int64, _ string) error {
	i.log.add("installer staged %s", version)

	return nil
}

func (i installer) Restart(ctx context.Context) error {
	i.log.add("installer restarted")

	if i.held {
		time.Sleep(i.returnsAfter)

		return nil
	}

	<-ctx.Done()

	return ctx.Err()
}

// dataBackup backs the database up, as far as the update can tell.
type dataBackup struct {
	log *journal
}

func (b dataBackup) BackUp(context.Context) (string, error) {
	b.log.add("data backed up")

	return "/data/app.db.backup-x", nil
}

// quitGuard reports runs going in the app, whatever the update was told.
type quitGuard struct{}

func (quitGuard) Active() bool { return true }

// publish writes 0.3.0's zip and its manifest, signed with private, into a folder the
// update reads its releases from, and returns the folder.
func publish(t *testing.T, private ed25519.PrivateKey) string {
	t.Helper()

	dir := t.TempDir()
	body := []byte("the zip of 0.3.0")

	size, sum, err := update.Digest(bytes.NewReader(body))
	require.NoError(t, err)

	m, err := update.Sign(update.Manifest{
		Version: nextVersion, Platform: update.Platform, File: update.FileName(update.Platform, nextVersion), Size: size, SHA512: sum,
	}, private)
	require.NoError(t, err)

	manifest, err := update.Encode(m)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, update.ManifestName), manifest, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, m.File), body, 0o600))

	return dir
}

// realOpts is how realUpdate sets the app's update up.
type realOpts struct {
	// elsewhere puts the app in a Downloads folder, where it can't replace itself, in place
	// of an Applications folder of its own.
	elsewhere bool

	// held has the installer's restart return with the app running, as when something held
	// the quit, returnsAfter after it started the helper.
	held         bool
	returnsAfter time.Duration

	// going are the runs going in the app.
	going []update.Run
}

// realUpdate is the app's real update over a folder holding 0.3.0, as a rehearsal reads
// it, set up as opts says, its installer attached; events reach the sink relay passes them
// to.
func realUpdate(t *testing.T, log *journal, relay *event.Relay, opts realOpts) *update.Service {
	t.Helper()

	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	root := t.TempDir()
	applications := filepath.Join(root, "Applications")

	bundle := filepath.Join(applications, brand.Name+".app")
	if opts.elsewhere {
		bundle = filepath.Join(root, "Downloads", brand.Name+".app")
	}

	require.NoError(t, os.MkdirAll(bundle, 0o700))

	engine := update.NewEngine(update.Config{
		Flavor: update.FlavorRehearsal, Current: "0.2.0", Feed: update.FolderFeed{Dir: publish(t, private)},
		Key: update.EncodeKey(update.PublicKeyOf(private)), Memory: &memory{values: map[string]string{}},
		Cache: filepath.Join(root, "updates"), Logs: filepath.Join(root, "logs"), TempDir: root, PID: 4242,
		Bundle: bundle, Home: filepath.Join(root, "home"), Applications: applications,
		Writable: func(string) bool { return true },
		Backup:   dataBackup{log: log}, Runs: runs{going: opts.going}, Events: relay,
	})
	engine.Attach(installer{log: log, held: opts.held, returnsAfter: opts.returnsAfter})

	return update.NewService(engine)
}

func TestTheMenuOffersTheUpdateWhereTheAppCantReplaceItself(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		relay := &event.Relay{}
		s := r.shell(withUpdates(realUpdate(t, r.log, relay, realOpts{elsewhere: true})))
		relay.Connect(s)

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		s.UpdateAction(menubar.UpdateCheck)
		synctest.Wait()
		assert.Equal(t, []menubar.UpdateRow{checkRow(), {Label: "Update to 0.3.0…", Action: menubar.UpdateDownload}},
			updateRows(t, r), "it checks and tells where it is; the window says to move it")
		assert.Empty(t, r.lines("installer"), "nothing was staged")

		require.NoError(t, s.Stop())
	})
}

// restartInTheWindow restarts as the window does once the user confirmed, in the
// background, since the app quits inside a restart that goes ahead; it returns what ends it
// and what it returned, once it has.
func restartInTheWindow(t *testing.T, u *update.Service) (context.CancelFunc, <-chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() {
		_, err := u.Restart(ctx, true)
		done <- err
	}()

	return cancel, done
}

func TestTheWindowsRestartLetsTheQuitThrough(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		relay := &event.Relay{}
		u := realUpdate(t, r.log, relay, realOpts{})
		s := r.shell(withUpdates(u), func(cfg *desktop.Config) { cfg.QuitGuard = quitGuard{} })
		relay.Connect(s)

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		_, err := u.CheckNow(t.Context())
		require.NoError(t, err)

		_, err = u.Download(t.Context())
		require.NoError(t, err)

		require.False(t, s.ShouldQuit(), "the guard holds a quit while runs go")

		cancel, done := restartInTheWindow(t, u)

		synctest.Wait()

		assert.Equal(t, []string{"data backed up", "installer staged 0.3.0", "installer restarted"},
			append(r.lines("data"), r.lines("installer")...))
		assert.True(t, s.ShouldQuit(), "the restart let the quit through, which its installer makes")
		assert.Zero(t, r.app.quitCount(), "the shell quits nothing itself")

		cancel()
		require.Error(t, <-done, "the app would have quit inside the restart; ended here, it returns")

		require.NoError(t, s.Stop())
	})
}

// exits records the statuses the shell exited with.
type exits struct {
	mu    sync.Mutex
	codes []int
}

func (e *exits) exit(code int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.codes = append(e.codes, code)
}

func (e *exits) all() []int {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]int(nil), e.codes...)
}

func TestARestartWhoseQuitIsHeldSeesTheAppOut(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		relay := &event.Relay{}
		u := realUpdate(t, r.log, relay, realOpts{held: true})
		exited := &exits{}
		s := r.shell(withUpdates(u), func(cfg *desktop.Config) {
			cfg.QuitGuard, cfg.Exit = quitGuard{}, exited.exit
		})
		relay.Connect(s)

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		_, err := u.CheckNow(t.Context())
		require.NoError(t, err)

		_, err = u.Download(t.Context())
		require.NoError(t, err)

		cancel, done := restartInTheWindow(t, u)
		defer cancel()

		require.NoError(t, <-done, "the restart goes on: the app quits on its own")
		synctest.Wait()

		assert.Equal(t, []string{"installer staged 0.3.0", "installer restarted"}, r.lines("installer"))
		assert.True(t, s.ShouldQuit(), "every quit stays let through")
		assert.Equal(t, []menubar.UpdateRow{{Label: "Check for Updates…"}, {
			Label:   "Finishing the update…",
			Tooltip: brand.Name + " is quitting to install 0.3.0. If it doesn't, quit it and open it again.",
		}}, updateRows(t, r), "the restart row is never offered again")

		time.Sleep(desktop.FinishQuitEvery * desktop.FinishQuitTries)
		synctest.Wait()
		assert.Equal(t, desktop.FinishQuitTries, r.app.quitCount(), "the quit is asked for again")
		assert.Empty(t, exited.all(), "not yet")
		assert.Empty(t, r.lines(dataClosed))

		time.Sleep(desktop.FinishWithin - desktop.FinishQuitEvery*desktop.FinishQuitTries)
		synctest.Wait()
		assert.Equal(t, []string{dataClosed}, r.lines(dataClosed), "the shell shut down in order")
		assert.Equal(t, []int{0}, exited.all(), "then exited, so the helper installs the update")
		assert.Equal(t, desktop.FinishQuitTries, r.app.quitCount(), "no more asks once the tries are spent")
		assert.Contains(t, r.logs.String(), "the app still runs; shutting down in order to finish the update")
	})
}

func TestAHeldQuitIsTimedFromTheHelpersStartNotFromWhenTheQuitCameBack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		returns  time.Duration // how long the held quit took to come back
		asks     int           // the asks to quit before the exit
		lastWord string        // what the log says as the shell leaves
	}{
		{
			name: "the quit came back 4 seconds on", returns: 4 * time.Second, asks: desktop.FinishQuitTries,
			lastWord: "the app still runs; shutting down in order to finish the update",
		},
		{
			name: "the quit came back after the helper's start plus FinishWithin", returns: 27 * time.Second,
			lastWord: "the update's helper is nearly done waiting; shutting down in order to finish the update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newRig()
				relay := &event.Relay{}
				u := realUpdate(t, r.log, relay, realOpts{held: true, returnsAfter: tt.returns})
				exited := &exits{}
				s := r.shell(withUpdates(u), func(cfg *desktop.Config) {
					cfg.QuitGuard, cfg.Exit = quitGuard{}, exited.exit
				})
				relay.Connect(s)

				s.Start(t.Context())
				s.Launched()
				synctest.Wait()

				_, err := u.CheckNow(t.Context())
				require.NoError(t, err)

				_, err = u.Download(t.Context())
				require.NoError(t, err)

				// The helper's 30 seconds start as the restart starts it, here.
				helperStarted := time.Now()

				s.UpdateAction(menubar.UpdateRestart)
				synctest.Wait()

				exitBy := max(desktop.FinishWithin, tt.returns)

				time.Sleep(exitBy - time.Second)
				synctest.Wait()
				assert.Empty(t, exited.all(), "not yet")

				time.Sleep(time.Second)
				synctest.Wait()
				assert.Equal(t, []int{0}, exited.all(), "exited %s after the helper started", exitBy)
				assert.Less(t, time.Since(helperStarted), 30*time.Second, "within the helper's 30 seconds")
				assert.Equal(t, tt.asks, r.app.quitCount())
				assert.Equal(t, []string{dataClosed}, r.lines(dataClosed), "the shell shut down in order")
				assert.Contains(t, r.logs.String(), tt.lastWord)
			})
		})
	}
}

func TestAShutdownThatHangsStillExitsBeforeTheHelperGivesUp(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		relay := &event.Relay{}
		u := realUpdate(t, r.log, relay, realOpts{held: true})
		exited := &exits{}
		release := make(chan struct{})
		s := r.shell(withUpdates(u), func(cfg *desktop.Config) {
			cfg.QuitGuard, cfg.Exit = quitGuard{}, exited.exit
			cfg.StopTimeout = time.Minute
			cfg.Workers = []desktop.Worker{&fakeWorker{name: "stuck", log: r.log, release: release}}
		})
		relay.Connect(s)

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		_, err := u.CheckNow(t.Context())
		require.NoError(t, err)

		_, err = u.Download(t.Context())
		require.NoError(t, err)

		held := time.Now()

		cancel, done := restartInTheWindow(t, u)
		defer cancel()

		require.NoError(t, <-done)
		synctest.Wait()

		time.Sleep(desktop.FinishWithin)
		synctest.Wait()
		assert.Empty(t, exited.all(), "the shutdown waits for the stuck work")

		time.Sleep(desktop.FinishStopWithin)
		synctest.Wait()
		assert.Equal(t, []int{0}, exited.all(), "but no longer than the helper waits")
		assert.Less(t, time.Since(held), 30*time.Second, "within the helper's 30 seconds")
		assert.Contains(t, r.logs.String(), "the shutdown is taking too long")

		close(release)
		synctest.Wait()
	})
}

// browser records the pages it opens.
type browser struct {
	mu     sync.Mutex
	opened []string
}

func (b *browser) OpenURL(url string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.opened = append(b.opened, url)

	return nil
}

func (b *browser) pages() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return append([]string(nil), b.opened...)
}

func TestTheMenusManualDownloadOpensTheReleasePage(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		page := "https://example.com/releases/tag/v0.3.0"
		pages := &browser{}
		u := &fakeUpdates{log: r.log, st: updating(func(st *update.Status) {
			st.State, st.Latest, st.ReleaseURL = update.StateAvailable, nextVersion, page
			st.InstallFailed = update.InstallFailure{Version: nextVersion, Reason: "It failed its signature check."}
		})}
		s := r.shell(withUpdates(u), func(cfg *desktop.Config) { cfg.Browser = pages })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, []menubar.UpdateRow{
			checkRow(),
			{Label: "0.3.0 didn't install", Tooltip: "It failed its signature check."},
			{Label: "Download 0.3.0 Manually…", Tooltip: page, Action: menubar.UpdateManual},
		}, updateRows(t, r))

		s.UpdateAction(menubar.UpdateManual)
		synctest.Wait()

		assert.Equal(t, []string{page}, pages.pages())
		assert.Empty(t, r.lines("update"), "nothing is downloaded or installed")

		require.NoError(t, s.Stop())
	})
}

func TestTheMenusManualDownloadWithNoVersionFoundOpensTheFailedVersionsPage(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		pages := &browser{}
		u := &fakeUpdates{log: r.log, st: updating(func(st *update.Status) {
			st.Latest, st.ReleaseURL = "", update.ReleaseURL("0.2.0")
			st.InstallFailed = update.InstallFailure{Version: nextVersion, Reason: "It failed its signature check."}
		})}
		s := r.shell(withUpdates(u), func(cfg *desktop.Config) { cfg.Browser = pages })

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		failedPage := update.ReleaseURL(nextVersion)
		assert.Equal(t, []menubar.UpdateRow{
			checkRow(),
			{Label: "0.3.0 didn't install", Tooltip: "It failed its signature check."},
			{Label: "Download 0.3.0 Manually…", Tooltip: failedPage, Action: menubar.UpdateManual},
		}, updateRows(t, r))

		s.UpdateAction(menubar.UpdateManual)
		synctest.Wait()

		assert.Equal(t, []string{failedPage}, pages.pages(), "the failed version's page, not the running one's")
		assert.Empty(t, r.lines("update"), "nothing is downloaded or installed")

		require.NoError(t, s.Stop())
	})
}

// Compile-time check: the update's bound face is what the menu bar offers.
var _ desktop.Updates = (*update.Service)(nil)
