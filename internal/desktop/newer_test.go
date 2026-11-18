package desktop_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

var errFakeDownload = errors.New("fake: the connection dropped")

// The commands the tests' dialogs offer, and the label of their Quit.
const (
	fakeInstall = "the install command"
	fakeBuild   = "git pull && magex app:run"
	quitLabel   = "Quit"
)

// testCommands are the commands the tests' dialogs offer.
func testCommands() desktop.Commands {
	return desktop.Commands{Install: fakeInstall, Build: fakeBuild}
}

// buttonLabels lists f's buttons' labels, left to right.
func buttonLabels(f desktop.Failure) []string {
	buttons := f.Buttons()
	out := make([]string, 0, len(buttons))

	for _, b := range buttons {
		out = append(out, b.Label)
	}

	return out
}

// fakeStartupUI clicks the startup dialogs' buttons by their labels, in order, and Quit
// once they run out. Updating, shown while an update runs, waits until stop is closed,
// then stops it.
type fakeStartupUI struct {
	t        *testing.T
	stop     chan struct{}
	stopOnce sync.Once
	failed   bool

	mu      sync.Mutex
	clicks  []string
	asked   []desktop.Failure
	waited  int
	copied  []string
	quits   int
	reopens int
}

func newStartupUI(t *testing.T, clicks ...string) *fakeStartupUI {
	t.Helper()

	ui := &fakeStartupUI{t: t, stop: make(chan struct{}), clicks: clicks}
	t.Cleanup(ui.clickStop)

	return ui
}

func (u *fakeStartupUI) Ask(f desktop.Failure) desktop.Button {
	if f.Waiting {
		u.mu.Lock()
		u.waited++
		u.mu.Unlock()

		<-u.stop

		return f.Buttons()[0]
	}

	u.mu.Lock()
	u.asked = append(u.asked, f)

	label := quitLabel
	if len(u.clicks) > 0 {
		label, u.clicks = u.clicks[0], u.clicks[1:]
	}
	u.mu.Unlock()

	for _, b := range f.Buttons() {
		if b.Label == label {
			return b
		}
	}

	u.t.Errorf("%q has no %q: %q", f.Title, label, buttonLabels(f))

	return desktop.Button{Label: quitLabel, Choice: desktop.ChoiceQuit}
}

func (u *fakeStartupUI) Copy(text string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.copied = append(u.copied, text)

	return !u.failed
}

func (u *fakeStartupUI) Quit() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.quits++
}

func (u *fakeStartupUI) Reopen() {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.reopens++
}

// clickStop clicks Updating's Stop and Quit, once.
func (u *fakeStartupUI) clickStop() {
	u.stopOnce.Do(func() { close(u.stop) })
}

// seenSoFar is what a fakeStartupUI saw: the dialogs asked, Updating aside, the commands
// copied, and the quits and reopens.
type seenSoFar struct {
	asked   []desktop.Failure
	copied  []string
	quits   int
	reopens int
}

// seen returns what u saw so far.
func (u *fakeStartupUI) seen() seenSoFar {
	u.mu.Lock()
	defer u.mu.Unlock()

	return seenSoFar{
		asked: append([]desktop.Failure{}, u.asked...), copied: append([]string{}, u.copied...), quits: u.quits,
		reopens: u.reopens,
	}
}

// waits counts the times Updating was shown.
func (u *fakeStartupUI) waits() int {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.waited
}

// shownOnce waits for Updating, which shows on a goroutine of its own, to show, and fails
// the test unless it showed exactly once.
func (u *fakeStartupUI) shownOnce(t *testing.T, why string) {
	t.Helper()

	require.Eventually(t, func() bool { return u.waits() > 0 }, 5*time.Second, time.Millisecond, why)
	assert.Equal(t, 1, u.waits(), why)
}

// fakeNewerUpdate finds a newer release, or none, and installs it, or fails.
type fakeNewerUpdate struct {
	latest    update.Manifest
	found     bool
	latestErr error

	// updateErrs are what each Update returns, in turn; nil once they run out.
	updateErrs []error

	// hold, when set, keeps every Update until its context ends.
	hold bool

	mu      sync.Mutex
	checks  int
	updates []update.Manifest
	via     []update.Installer
}

func (f *fakeNewerUpdate) Latest(context.Context) (update.Manifest, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.checks++

	return f.latest, f.found, f.latestErr
}

func (f *fakeNewerUpdate) Update(ctx context.Context, m update.Manifest, installer update.Installer) error {
	f.mu.Lock()
	f.updates = append(f.updates, m)
	f.via = append(f.via, installer)

	var err error
	if len(f.updateErrs) > 0 {
		err, f.updateErrs = f.updateErrs[0], f.updateErrs[1:]
	}

	hold := f.hold
	f.mu.Unlock()

	if hold {
		<-ctx.Done()

		return ctx.Err()
	}

	return err
}

func (f *fakeNewerUpdate) calls() (checks int, updates []update.Manifest) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.checks, append([]update.Manifest{}, f.updates...)
}

// noInstaller stands in for the runtime's installer, which the fake update never calls.
type noInstaller struct{}

func (noInstaller) Install(context.Context, string, string, int64, string) error { return nil }

func (noInstaller) Restart(context.Context) error { return nil }

// startup runs the dialog of data a newer build saved, in a build that goes way, until the
// app quits.
func startup(t *testing.T, ui *fakeStartupUI, u desktop.NewerUpdate, way update.Way) {
	t.Helper()

	f := desktop.StartupFailure(errNewerData, desktop.Newer{Way: way, Commands: testCommands()})

	desktop.Startup{UI: ui, Update: u, Installer: noInstaller{}, Commands: testCommands()}.Run(t.Context(), f)
}

func TestTheNewerDataDialogOffersEachBuildItsWayForward(t *testing.T) {
	t.Parallel()

	tests := []struct {
		way     update.Way
		buttons []desktop.Button
		says    string
	}{
		{
			way: update.WayUpdate,
			buttons: []desktop.Button{
				{Label: "Update and Reopen", Choice: desktop.ChoiceUpdate, Default: true},
				{Label: quitLabel, Choice: desktop.ChoiceQuit, Cancel: true},
			},
			says: "Update and Reopen downloads the latest release from GitHub, checks it, installs it in place of " +
				"this copy, and opens it.",
		},
		{
			way: update.WayInstall,
			buttons: []desktop.Button{
				{Label: "Copy Install Command", Choice: desktop.ChoiceCopy, Copies: fakeInstall, Default: true},
				{Label: quitLabel, Choice: desktop.ChoiceQuit, Cancel: true},
			},
			says: "This copy can't update itself where it runs",
		},
		{
			way: update.WaySource,
			buttons: []desktop.Button{
				{Label: "Copy Command", Choice: desktop.ChoiceCopy, Copies: fakeBuild, Default: true},
				{Label: quitLabel, Choice: desktop.ChoiceQuit, Cancel: true},
			},
			says: "A build from source never downloads an update: pull the latest master and build it again, in its " +
				"clone:\n\n    git pull && magex app:run\n\n",
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.way), func(t *testing.T) {
			t.Parallel()

			got := desktop.StartupFailure(errNewerData, desktop.Newer{Way: tt.way, Commands: testCommands()})

			assert.Equal(t, brand.Name+" needs an update", got.Title)
			assert.Equal(t, tt.buttons, got.Buttons())
			assert.Contains(t, got.Message, tt.says)
			assert.Contains(t, got.Message, "Your data is safe and untouched")
		})
	}
}

func TestUpdateAndReopenInstallsANewerReleaseAndQuits(t *testing.T) {
	t.Parallel()

	newer := update.Manifest{Version: "0.3.0"}
	u := &fakeNewerUpdate{latest: newer, found: true}
	ui := newStartupUI(t, "Update and Reopen")

	startup(t, ui, u, update.WayUpdate)

	checks, updates := u.calls()
	assert.Equal(t, 1, checks)
	assert.Equal(t, []update.Manifest{newer}, updates, "the release found is the one installed")
	assert.Equal(t, []update.Installer{noInstaller{}}, u.via)

	seen := ui.seen()
	asked, copied, quits := seen.asked, seen.copied, seen.quits
	require.Len(t, asked, 1, "nothing more to say: the app restarts into the new version")
	ui.shownOnce(t, "Updating shows while it runs")
	assert.Empty(t, copied)
	assert.Equal(t, 1, quits, "a quit the restart's own didn't carry out is asked again")

	waiting := desktop.Updating()
	assert.Equal(t, []desktop.Button{{Label: "Stop and Quit", Choice: desktop.ChoiceQuit, Cancel: true}},
		waiting.Buttons(), "Escape stops it; a Return pressed again never does")
	assert.Contains(t, waiting.Message, "Your data is safe and untouched.")
}

func TestUpdateAndReopenSaysPlainlyWhenNoReleaseIsNewer(t *testing.T) {
	t.Parallel()

	u := &fakeNewerUpdate{latest: update.Manifest{Version: "0.2.0"}}
	ui := newStartupUI(t, "Update and Reopen", "Copy Build Command", "Copy Install Command", quitLabel)

	startup(t, ui, u, update.WayUpdate)

	_, updates := u.calls()
	assert.Empty(t, updates, "nothing is installed")

	seen := ui.seen()
	asked, copied, quits := seen.asked, seen.copied, seen.quits
	require.Len(t, asked, 4)

	none := asked[1]
	assert.Equal(t, "No release can open this data yet", none.Title)
	assert.True(t, strings.HasPrefix(none.Message, "No published release can open this data yet; it was saved by a "+
		"newer build, likely one built from source. Your data is safe and untouched."), none.Message)
	assert.Contains(t, none.Message, "\n\n    git pull && magex app:run\n\n")
	assert.Equal(t, []desktop.Button{
		{Label: "Copy Build Command", Choice: desktop.ChoiceCopy, Copies: fakeBuild, Default: true},
		{Label: "Copy Install Command", Choice: desktop.ChoiceCopy, Copies: fakeInstall},
		{Label: quitLabel, Choice: desktop.ChoiceQuit, Cancel: true},
	}, none.Buttons())

	assert.Equal(t, []string{fakeBuild, fakeInstall}, copied)
	assert.Equal(t, none.Message+"\n\nCopied. Quit "+brand.Name+", then paste it into Terminal.", asked[2].Message,
		"the dialog comes back, saying so")
	assert.Equal(t, asked[2].Message, asked[3].Message, "said once, however many copies")
	assert.Equal(t, 1, quits)
}

func TestABuildFromSourceNeverDownloads(t *testing.T) {
	t.Parallel()

	u := &fakeNewerUpdate{latest: update.Manifest{Version: "0.3.0"}, found: true}
	ui := newStartupUI(t, "Copy Command", quitLabel)

	startup(t, ui, u, update.WaySource)

	checks, updates := u.calls()
	assert.Zero(t, checks, "a build from source never checks")
	assert.Empty(t, updates)

	seen := ui.seen()
	asked, copied, quits := seen.asked, seen.copied, seen.quits
	require.Len(t, asked, 2)
	assert.Equal(t, []string{fakeBuild}, copied)
	assert.Zero(t, ui.waits())
	assert.Equal(t, 1, quits)
}

func TestAFailedUpdateSaysWhyAndCanBeTriedAgain(t *testing.T) {
	t.Parallel()

	newer := update.Manifest{Version: "0.3.0"}
	failure := uierr.New("Couldn't download 0.3.0: check the connection, then try again.", errFakeDownload)
	u := &fakeNewerUpdate{latest: newer, found: true, updateErrs: []error{failure}}
	ui := newStartupUI(t, "Update and Reopen", "Try Again")

	startup(t, ui, u, update.WayUpdate)

	_, updates := u.calls()
	assert.Equal(t, []update.Manifest{newer, newer}, updates)

	seen := ui.seen()
	asked, quits := seen.asked, seen.quits
	require.Len(t, asked, 2)
	assert.Equal(t, brand.Name+" couldn't update", asked[1].Title)
	assert.True(t, strings.HasPrefix(asked[1].Message, "Couldn't download 0.3.0: check the connection, then try "+
		"again.\n\nNothing was installed, and your data is safe and untouched."), asked[1].Message)
	assert.Equal(t, []string{"Try Again", "Copy Install Command", quitLabel}, buttonLabels(asked[1]))
	ui.shownOnce(t, "Updating stays up through the try again")
	assert.Equal(t, 1, quits)
}

func TestACheckThatFailsSaysWhy(t *testing.T) {
	t.Parallel()

	reach := uierr.New("Couldn't reach the releases: check the connection, then try again.", errFakeDownload)
	u := &fakeNewerUpdate{latestErr: reach}
	ui := newStartupUI(t, "Update and Reopen", quitLabel)

	startup(t, ui, u, update.WayUpdate)

	seen := ui.seen()
	asked, quits := seen.asked, seen.quits
	require.Len(t, asked, 2)
	assert.True(t, strings.HasPrefix(asked[1].Message, "Couldn't reach the releases:"), asked[1].Message)
	assert.Equal(t, 1, quits)
}

func TestStopAndQuitStopsTheUpdate(t *testing.T) {
	t.Parallel()

	u := &fakeNewerUpdate{latest: update.Manifest{Version: "0.3.0"}, found: true, hold: true}
	ui := newStartupUI(t, "Update and Reopen")

	done := make(chan struct{})

	go func() {
		defer close(done)

		startup(t, ui, u, update.WayUpdate)
	}()

	require.Eventually(t, func() bool {
		_, updates := u.calls()

		return len(updates) == 1 && ui.waits() == 1
	}, 5*time.Second, time.Millisecond)

	ui.clickStop()
	<-done

	seen := ui.seen()
	asked, quits := seen.asked, seen.quits
	assert.Len(t, asked, 1, "a stopped update says nothing more")
	assert.Equal(t, 1, quits, "Stop and Quit quits")
}

func TestACopyThatFailsSaysWhereTheCommandIs(t *testing.T) {
	t.Parallel()

	ui := newStartupUI(t, "Copy Install Command", quitLabel)
	ui.failed = true

	startup(t, ui, nil, update.WayInstall)

	asked := ui.seen().asked
	require.Len(t, asked, 2)
	assert.True(t, strings.HasSuffix(asked[1].Message, "\n\nThe command couldn't be copied. The README holds it: "+
		brand.Repository+"#install"), asked[1].Message)
}

func TestTheBackupStaysOfferedAfterAnUpdateThatDidntHelp(t *testing.T) {
	t.Parallel()

	r := desktop.Restore{Backup: "/data/app.db.backup-x", Button: "Use my data from Nov 14, 3:02 PM", Detail: "Or go back."}
	u := &fakeNewerUpdate{latest: update.Manifest{Version: "0.2.0"}}
	ui := newStartupUI(t, "Update and Reopen", quitLabel)

	f := desktop.StartupFailure(errNewerData, desktop.Newer{Way: update.WayUpdate, Commands: testCommands()}).Offering(r)
	desktop.Startup{UI: ui, Update: u, Installer: noInstaller{}, Commands: testCommands()}.Run(t.Context(), f)

	asked := ui.seen().asked
	require.Len(t, asked, 2)
	assert.Equal(t, []string{"Use my data from Nov 14, 3:02 PM", "Copy Build Command", "Copy Install Command", quitLabel},
		buttonLabels(asked[1]))
	assert.True(t, strings.HasSuffix(asked[1].Message, "\n\nOr go back."), asked[1].Message)
}

func TestTheBackupsButtonPutsItBackAndOpensTheAppAgain(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), brand.Slug+".db")
	db := testutil.DBAt(t, path)

	_, err := store.Backup(t.Context(), db, path)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	newerBuildSaved(t, path)

	r, ok, err := desktop.FindRestore(errNewerData, path, time.Now())
	require.NoError(t, err)
	require.True(t, ok)

	u := &fakeNewerUpdate{}
	ui := newStartupUI(t, r.Button)
	f := desktop.StartupFailure(errNewerData, desktop.Newer{Way: update.WayUpdate, Commands: testCommands()}).Offering(r)

	desktop.Startup{UI: ui, Update: u, Installer: noInstaller{}, Commands: testCommands(), DB: path}.Run(t.Context(), f)

	seen := ui.seen()
	quits, reopens := seen.quits, seen.reopens
	assert.Equal(t, 1, reopens)
	assert.Zero(t, quits)

	checks, _ := u.calls()
	assert.Zero(t, checks)

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err, "the data put back opens")
	require.NoError(t, opened.DB.Close())
}

// bareRelease is a rehearsal build of 0.2.0 in an Applications folder of its own, whose
// update reads a folder holding 0.3.0's release, signed with the key it trusts, beside a
// data folder holding a database a newer build saved. zip is the zip the folder holds.
type bareRelease struct {
	bare   *update.Bare
	bundle string
	cache  string
	db     string
}

func newBareRelease(t *testing.T, zip []byte) bareRelease {
	t.Helper()

	root := t.TempDir()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	body := []byte("the zipped bundle")
	sum := sha512.Sum512(body)

	m, err := update.Sign(update.Manifest{
		Version: "0.3.0", Platform: update.Platform, File: update.FileName(update.Platform, "0.3.0"),
		Size: int64(len(body)), SHA512: hex.EncodeToString(sum[:]),
	}, private)
	require.NoError(t, err)

	encoded, err := update.Encode(m)
	require.NoError(t, err)

	feed := filepath.Join(root, "feed")
	testutil.WriteFile(t, filepath.Join(feed, update.ManifestName), string(encoded))

	if zip != nil {
		testutil.WriteFile(t, filepath.Join(feed, m.File), string(zip))
	}

	apps := filepath.Join(root, "Applications")
	bundle := filepath.Join(apps, brand.Name+".app")
	require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o700))

	data := filepath.Join(root, "data")
	db := filepath.Join(data, brand.Slug+".db")
	require.NoError(t, os.MkdirAll(data, 0o700))
	newerBuildSaved(t, db)

	b := update.NewBare(update.BareConfig{
		Flavor: update.FlavorRehearsal, Current: "0.2.0", Feed: update.FolderFeed{Dir: feed},
		Key: update.EncodeKey(update.PublicKeyOf(private)), Cache: filepath.Join(data, "updates"), Bundle: bundle,
		Applications: apps, Home: filepath.Join(root, "home"), Writable: func(string) bool { return true },
	})

	return bareRelease{bare: b, bundle: bundle, cache: filepath.Join(data, "updates"), db: db}
}

// recordingInstaller records what it stages, and returns from Restart as the runtime's
// does when something held the quit.
type recordingInstaller struct {
	mu       sync.Mutex
	versions []string
	restarts int
}

func (r *recordingInstaller) Install(_ context.Context, zip, version string, _ int64, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := os.Stat(zip); err != nil {
		return err
	}

	r.versions = append(r.versions, version)

	return nil
}

func (r *recordingInstaller) Restart(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.restarts++

	return nil
}

// fileSums reads the SHA-256 of every file under dir, by its path.
func fileSums(t *testing.T, dir string) map[string]string {
	t.Helper()

	out := map[string]string{}

	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		body, readErr := os.ReadFile(path) //nolint:gosec // the test's own files
		if readErr != nil {
			return readErr
		}

		sum := sha512.Sum512(body)
		out[path] = hex.EncodeToString(sum[:])

		return nil
	}))

	return out
}

func TestTheDialogUpdatesFromTheReleasesWithoutTheDatabase(t *testing.T) {
	t.Parallel()

	rel := newBareRelease(t, []byte("the zipped bundle"))
	data := fileSums(t, filepath.Dir(rel.db))
	installer := &recordingInstaller{}
	ui := newStartupUI(t, "Update and Reopen")

	f := desktop.StartupFailure(errNewerData, desktop.Newer{Way: rel.bare.Way(), Commands: testCommands()})
	desktop.Startup{UI: ui, Update: rel.bare, Installer: installer, Commands: testCommands(), DB: rel.db}.Run(t.Context(), f)

	assert.Equal(t, []string{"0.3.0"}, installer.versions, "downloaded, checked, and staged")
	assert.Equal(t, 1, installer.restarts)

	quits := ui.seen().quits
	assert.Equal(t, 1, quits)

	after := fileSums(t, filepath.Dir(rel.db))
	for path, sum := range data {
		assert.Equal(t, sum, after[path], "%s is as it was", filepath.Base(path))
	}

	for path := range after {
		if _, was := data[path]; !was {
			assert.True(t, strings.HasPrefix(path, rel.cache+string(filepath.Separator)),
				"only the update is new in the data folder: %s", path)
		}
	}

	_, err := store.Open(t.Context(), rel.db)
	require.ErrorIs(t, err, store.ErrNewerSchema, "the newer data is untouched, for the newer build to open")
}

func TestAFailedDownloadLeavesTheAppAsItWas(t *testing.T) {
	t.Parallel()

	rel := newBareRelease(t, nil)
	apps := fileSums(t, filepath.Dir(rel.bundle))
	data := fileSums(t, filepath.Dir(rel.db))
	installer := &recordingInstaller{}
	ui := newStartupUI(t, "Update and Reopen", quitLabel)

	f := desktop.StartupFailure(errNewerData, desktop.Newer{Way: rel.bare.Way(), Commands: testCommands()})
	desktop.Startup{UI: ui, Update: rel.bare, Installer: installer, Commands: testCommands(), DB: rel.db}.Run(t.Context(), f)

	seen := ui.seen()
	asked, quits := seen.asked, seen.quits
	require.Len(t, asked, 2)
	assert.Equal(t, brand.Name+" couldn't update", asked[1].Title)
	assert.True(t, strings.HasPrefix(asked[1].Message, "Couldn't download 0.3.0: "), asked[1].Message)
	assert.NotContains(t, asked[1].Message, rel.cache, "no path in the words")
	assert.Equal(t, 1, quits)

	assert.Empty(t, installer.versions, "nothing was staged")
	assert.Equal(t, apps, fileSums(t, filepath.Dir(rel.bundle)), "the app and what is beside it are as they were")
	assert.Equal(t, data, fileSums(t, filepath.Dir(rel.db)), "nothing of the download is kept, and the data is as it was")
}

func TestTheInstallCommandsAreTheREADMEs(t *testing.T) {
	t.Parallel()

	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	require.NoError(t, err)

	assert.Contains(t, string(readme), "```bash\n"+desktop.InstallCommand(false)+"\n```",
		"the README's install command")

	indented := "  " + strings.ReplaceAll(desktop.InstallCommand(true), "\n", "\n  ")
	assert.Contains(t, string(readme), "  ```bash\n"+indented+"\n  ```",
		"the README's install command for ~/Applications")
}

func TestTheCommandsFollowWhereTheAppRuns(t *testing.T) {
	t.Parallel()

	system := desktop.InstallCommand(false)
	user := desktop.InstallCommand(true)

	tests := []struct {
		name string
		loc  update.Location
		want string
	}{
		{name: "in /Applications", loc: update.Location{Kind: update.LocationApplications}, want: system},
		{name: "in ~/Applications", loc: update.Location{Kind: update.LocationUserApplications}, want: user},
		{
			name: "elsewhere, moving to /Applications",
			loc:  update.Location{Kind: update.LocationDiskImage, Target: "/Applications/" + brand.Name + ".app"},
			want: system,
		},
		{
			name: "elsewhere, moving to ~/Applications",
			loc:  update.Location{Kind: update.LocationElsewhere, Target: "~/Applications/" + brand.Name + ".app"},
			want: user,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, desktop.NewerCommands("", tt.loc).Install)
		})
	}

	assert.True(t, strings.HasPrefix(user, "mkdir -p ~/Applications\n"), user)
	assert.Contains(t, user, "ditto mnt/"+brand.Name+".app ~/Applications/"+brand.Name+".app")
	assert.Contains(t, system, "rm -rf /Applications/"+brand.Name+".app &&")
}

func TestABuildFromSourceIsBuiltAgainInItsClone(t *testing.T) {
	t.Parallel()

	clone := filepath.Join(t.TempDir(), "my clone")
	bundle := filepath.Join(clone, "bin", brand.DevName+".app")
	require.NoError(t, os.MkdirAll(bundle, 0o700))
	testutil.WriteFile(t, filepath.Join(clone, ".git"), "gitdir: elsewhere")

	got := desktop.NewerCommands(bundle, update.Location{})
	assert.Equal(t, "cd '"+clone+"' && git pull && magex app:run", got.Build, "quoted, for the space")

	elsewhere := filepath.Join(t.TempDir(), "bin", brand.DevName+".app")
	require.NoError(t, os.MkdirAll(elsewhere, 0o700))

	for _, b := range []string{"", elsewhere, filepath.Join(clone, brand.DevName+".app")} {
		assert.Equal(t, "git pull && magex app:run", desktop.NewerCommands(b, update.Location{}).Build,
			"no clone known for %q", b)
	}
}
