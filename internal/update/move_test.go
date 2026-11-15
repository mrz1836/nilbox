package update_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

var (
	errFakeDitto    = errors.New("fake: ditto failed")
	errFakeCodesign = errors.New("fake: codesign failed")
)

// app is the bundle's name, as a move names its copy.
const app = brand.Name + ".app"

// infoPlist is an app's Info.plist, as brandgen writes the app's own, naming id and version.
func infoPlist(id, version string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDisplayName</key>
	<string>` + brand.Name + `</string>
	<key>LSArchitecturePriority</key>
	<array>
		<string>arm64</string>
	</array>
	<key>CFBundleIdentifier</key>
	<string>` + id + `</string>
	<key>CFBundleShortVersionString</key>
	<string>` + version + `</string>
	<key>NSHighResolutionCapable</key>
	<true/>
</dict>
</plist>
`
}

// writeBundle makes an app bundle at bundle whose Info.plist names id and version, with its
// program beside it.
func writeBundle(t *testing.T, bundle, id, version string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(infoPlist(id, version)), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "Contents", "MacOS", brand.Slug), []byte("the program of "+version),
		0o600))
}

// stageName matches the hidden folder a move copies the app into beside its target.
var stageName = regexp.MustCompile(`^\.` + regexp.QuoteMeta(app) + `\.moving-[0-9]+$`)

// stageless names a path inside a move's hidden folder as the tests write it: the folder's
// random name as "(stage)".
func stageless(path string) string {
	dir := filepath.Dir(path)
	if stageName.MatchString(filepath.Base(dir)) {
		return filepath.Join(filepath.Dir(dir), "(stage)", filepath.Base(path))
	}

	return path
}

// inStage is where a move into folder copies the app, as stageless writes it.
func inStage(folder string) string {
	return filepath.Join(folder, "(stage)", app)
}

// fakeMover records what a move asks of it, in order, copying the bundle's Info.plist as
// ditto would copy the bundle, then failing as set. A path inside a move's hidden folder is
// recorded as stageless writes it.
type fakeMover struct {
	mu        sync.Mutex
	calls     []string
	copyErr   error
	verifyErr error

	// marked and markErr answer Translocates, and markAsked records whom it asked about.
	marked    bool
	markErr   error
	markAsked []string

	// likes records the bundle each Verify compared with: the app that runs.
	likes []string

	// holding counts the quit holds taken and not let go of, holds every one taken, and
	// heldOnCopy the holding count as each copy was made.
	holding, holds int
	heldOnCopy     []int

	// emptyOnCopy records whether the folder a copy went into was empty, as the move made it.
	emptyOnCopy []bool

	// onCopy, when set, runs once the copy is written, before it returns, and onVerify as
	// each Verify begins.
	onCopy   func(to string)
	onVerify func(bundle string)
}

func (m *fakeMover) HoldQuit() func() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.holding++
	m.holds++

	var once sync.Once

	return func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()

			m.holding--
		})
	}
}

func (m *fakeMover) Copy(_ context.Context, from, to string) error {
	listed, err := os.ReadDir(to)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.calls = append(m.calls, "copy "+from+" "+stageless(to))
	m.emptyOnCopy = append(m.emptyOnCopy, len(listed) == 0)
	m.heldOnCopy = append(m.heldOnCopy, m.holding)
	copyErr, onCopy := m.copyErr, m.onCopy
	m.mu.Unlock()

	plist, err := os.ReadFile(filepath.Join(from, "Contents", "Info.plist")) //nolint:gosec // the test's own bundle
	if err != nil {
		return err
	}

	if err = os.MkdirAll(filepath.Join(to, "Contents"), 0o700); err != nil {
		return err
	}

	if err = os.WriteFile(filepath.Join(to, "Contents", "Info.plist"), plist, 0o600); err != nil { //nolint:gosec // the test's own copy
		return err
	}

	if onCopy != nil {
		onCopy(to)
	}

	return copyErr
}

func (m *fakeMover) Verify(_ context.Context, bundle, like string) error {
	m.mu.Lock()
	m.calls = append(m.calls, "verify "+stageless(bundle))
	m.likes = append(m.likes, like)
	onVerify := m.onVerify
	m.mu.Unlock()

	if onVerify != nil {
		onVerify(bundle)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.verifyErr
}

func (m *fakeMover) Translocates(bundle string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.markAsked = append(m.markAsked, bundle)

	return m.marked, m.markErr
}

func (m *fakeMover) OpenAfterQuit(bundle string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, "open "+bundle)
}

func (m *fakeMover) all() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.calls)
}

// held reports the quit holds a move took, and how many it hasn't let go of.
func (m *fakeMover) held() (holds, holding int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.holds, m.holding
}

// stages lists the hidden folders a move made in folder and left there.
func stages(t *testing.T, folder string) []string {
	t.Helper()

	listed, err := os.ReadDir(folder)
	require.NoError(t, err)

	var found []string

	for _, item := range listed {
		if strings.HasPrefix(item.Name(), "."+app+".moving-") {
			found = append(found, item.Name())
		}
	}

	return found
}

// moveRig is a rig whose app runs from a Downloads folder of its own, a copy of the running
// version, with an empty Applications folder beside it and a home folder with none, and a
// fake mover attached.
func newMoveRig(t *testing.T, change ...func(*update.Config)) (*rig, *fakeMover) {
	t.Helper()

	inDownloads := func(c *update.Config) {
		c.Bundle = filepath.Join(filepath.Dir(c.Applications), "Downloads", app)
	}

	r := newRig(t, append([]func(*update.Config){inDownloads}, change...)...)

	// The rig's own bundle, in its Applications folder, is not the one a move is about.
	require.NoError(t, os.RemoveAll(filepath.Join(r.cfg.Applications, app)))

	if r.cfg.Home != "" {
		require.NoError(t, os.MkdirAll(r.cfg.Home, 0o700))
	}

	if r.cfg.Bundle != "" {
		writeBundle(t, r.cfg.Bundle, brand.BundleID, running)
	}

	m := &fakeMover{}
	r.engine.AttachMover(m)

	return r, m
}

func TestMovingPutsTheAppInTheSharedApplicationsFolderWhenItCan(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	target := filepath.Join(r.cfg.Applications, app)

	got, err := r.service.MoveToApplications(t.Context())
	require.NoError(t, err)

	assert.Equal(t, update.MoveResult{Outcome: update.MoveMoved, Target: target}, got)
	assert.Equal(t, []string{
		"copy " + r.cfg.Bundle + " " + inStage(r.cfg.Applications), "verify " + inStage(r.cfg.Applications),
		"open " + target,
	}, m.all(), "copied and checked beside the target, then opened from there once the app quits")
	assert.Equal(t, []bool{true}, m.emptyOnCopy, "the copy goes into a folder the move made, empty")
	assert.Equal(t, []string{r.cfg.Bundle}, m.likes, "checked against the app that runs")
	assert.FileExists(t, filepath.Join(target, "Contents", "Info.plist"), "the copy is put in place")
	assert.Empty(t, stages(t, r.cfg.Applications), "and its hidden folder removed")
	assert.NoDirExists(t, filepath.Join(r.cfg.Home, "Applications"), "home's Applications folder is never made")

	id, err := update.BundleID(r.cfg.Bundle)
	require.NoError(t, err)
	assert.Equal(t, brand.BundleID, id, "the bundle that runs is never touched")
	assert.FileExists(t, filepath.Join(r.cfg.Bundle, "Contents", "MacOS", brand.Slug))
}

func TestMovingPutsTheAppInHomesApplicationsFolderWhenTheSharedOneIsReadOnly(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t, func(c *update.Config) {
		apps := c.Applications
		c.Writable = func(dir string) bool { return dir != apps }
	})
	homeApps := filepath.Join(r.cfg.Home, "Applications")
	target := filepath.Join(homeApps, app)

	assert.Equal(t, "~/Applications/"+app, r.status(t).Location.Target, "the status says where it would go")

	got, err := r.service.MoveToApplications(t.Context())
	require.NoError(t, err)

	assert.Equal(t, update.MoveResult{Outcome: update.MoveMoved, Target: "~/Applications/" + app}, got)
	assert.Equal(t, []string{
		"copy " + r.cfg.Bundle + " " + inStage(homeApps), "verify " + inStage(homeApps), "open " + target,
	}, m.all())
	assert.DirExists(t, homeApps, "made, since it was missing")
	assert.FileExists(t, filepath.Join(target, "Contents", "Info.plist"))
	assert.NoDirExists(t, filepath.Join(r.cfg.Applications, app), "nothing goes in the shared folder")
}

func TestMovingIntoHomesApplicationsFolderKeepsTheOneThere(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t, func(c *update.Config) { c.Writable = func(string) bool { return false } })
	homeApps := filepath.Join(r.cfg.Home, "Applications")
	writeBundle(t, filepath.Join(homeApps, "Other.app"), "com.example.other", "1.0.0")

	_, err := r.service.MoveToApplications(t.Context())
	require.NoError(t, err)

	assert.Len(t, m.all(), 3)
	assert.FileExists(t, filepath.Join(homeApps, "Other.app", "Contents", "Info.plist"), "what was there stays")
}

// A real read-only Applications folder, as a standard user's is, sends the move home: the
// engine asks the system when nothing stands in for it.
func TestMovingAskTheSystemWhetherApplicationsIsWritable(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	r, m := newMoveRig(t, func(c *update.Config) { c.Writable = nil })
	require.NoError(t, os.Chmod(r.cfg.Applications, 0o500)) //nolint:gosec // the test's own folder, made read-only

	t.Cleanup(func() { _ = os.Chmod(r.cfg.Applications, 0o700) }) //nolint:gosec // put back for t.TempDir's removal

	// The engine was built before the folder went read-only; a move works the target out
	// again.
	got, err := r.service.MoveToApplications(t.Context())
	require.NoError(t, err)

	target := filepath.Join(r.cfg.Home, "Applications", app)
	assert.Equal(t, "~/Applications/"+app, got.Target)
	assert.Equal(t, "open "+target, m.all()[2])
}

func TestACopyAlreadyInApplicationsIsOfferedNeverReplaced(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"0.1.0", running, "9.9.9"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			r, m := newMoveRig(t)
			target := filepath.Join(r.cfg.Applications, app)
			writeBundle(t, target, brand.BundleID, version)

			got, err := r.service.MoveToApplications(t.Context())
			require.NoError(t, err)

			assert.Equal(t, update.MoveResult{Outcome: update.MoveExists, Target: target}, got)
			assert.Empty(t, m.all(), "nothing copied, checked, or opened")

			program, err := os.ReadFile(filepath.Join(target, "Contents", "MacOS", brand.Slug)) //nolint:gosec // the test's own copy
			require.NoError(t, err)
			assert.Equal(t, "the program of "+version, string(program), "the copy there is left as it was")
		})
	}
}

func TestAMoveRefusesAnythingElseWhereTheAppWouldGo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		place func(t *testing.T, target, elsewhere string)
		what  string
	}{
		{
			name: "a link to a copy of the app",
			place: func(t *testing.T, target, elsewhere string) {
				t.Helper()
				writeBundle(t, elsewhere, brand.BundleID, running)
				require.NoError(t, os.Symlink(elsewhere, target))
			},
			what: "a link",
		},
		{
			name: "a file",
			place: func(t *testing.T, target, _ string) {
				t.Helper()
				require.NoError(t, os.WriteFile(target, []byte("not an app"), 0o600))
			},
			what: "a file",
		},
		{
			name: "another app",
			place: func(t *testing.T, target, _ string) {
				t.Helper()
				writeBundle(t, target, "com.example.other", "1.0.0")
			},
			what: "another app",
		},
		{
			name: "a folder that isn't an app",
			place: func(t *testing.T, target, _ string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(target, "notes"), 0o700))
			},
			what: "a folder",
		},
		{
			name: "an app whose Info.plist is binary",
			place: func(t *testing.T, target, _ string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(target, "Contents"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(target, "Contents", "Info.plist"), []byte("bplist00\x01\x02"), 0o600))
			},
			what: "a folder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, m := newMoveRig(t)
			target := filepath.Join(r.cfg.Applications, app)
			tt.place(t, target, filepath.Join(filepath.Dir(r.cfg.Applications), "Elsewhere", app))
			before, err := os.Lstat(target)
			require.NoError(t, err)

			for _, call := range []func(context.Context) (update.MoveResult, error){
				r.service.MoveToApplications, r.service.OpenInApplications,
			} {
				_, err = call(t.Context())
				require.ErrorIs(t, err, update.ErrTargetTaken)
				assert.Equal(t, r.cfg.Applications+" already has "+tt.what+" named "+app+
					", so nothing was moved. Rename or remove it, then try again.", uierr.Message(err))
			}

			assert.Empty(t, m.all(), "nothing copied, checked, or opened")

			after, err := os.Lstat(target)
			require.NoError(t, err)
			assert.Equal(t, before.Mode(), after.Mode(), "what is there is left as it was")
			assert.Equal(t, before.ModTime(), after.ModTime())
		})
	}
}

func TestAFailedCopyIsRemovedAndNothingElse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		copyErr   error
		verifyErr error
		words     string
		calls     func(from, folder string) []string
	}{
		{
			name:    "the copy fails",
			copyErr: uierr.New("ditto: No space left on device", errFakeDitto),
			words:   "Couldn't copy " + brand.Name + " into %s: ditto: No space left on device.",
			calls:   func(from, folder string) []string { return []string{"copy " + from + " " + inStage(folder)} },
		},
		{
			name:      "the copy doesn't pass its check",
			verifyErr: uierr.New(app+": a sealed resource is missing or invalid", errFakeCodesign),
			words: "The copy in %s didn't pass its signature check, so nothing was moved: " + app +
				": a sealed resource is missing or invalid.",
			calls: func(from, folder string) []string {
				return []string{"copy " + from + " " + inStage(folder), "verify " + inStage(folder)}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, m := newMoveRig(t)
			m.copyErr, m.verifyErr = tt.copyErr, tt.verifyErr
			target := filepath.Join(r.cfg.Applications, app)
			sibling := filepath.Join(r.cfg.Applications, "Other.app")
			writeBundle(t, sibling, "com.example.other", "1.0.0")

			_, err := r.service.MoveToApplications(t.Context())
			require.ErrorIs(t, err, update.ErrMoveFailed)
			assert.Equal(t, fmt.Sprintf(tt.words, r.cfg.Applications), uierr.Message(err))

			assert.Equal(t, tt.calls(r.cfg.Bundle, r.cfg.Applications), m.all(), "nothing opens")
			assert.NoFileExists(t, target, "nothing is put where the app goes")
			assert.NoDirExists(t, target)
			assert.Empty(t, stages(t, r.cfg.Applications), "the copy the move made is removed, with its folder")
			assert.FileExists(t, filepath.Join(sibling, "Contents", "Info.plist"), "the folder's other apps stay")
			assert.FileExists(t, filepath.Join(r.cfg.Bundle, "Contents", "MacOS", brand.Slug), "the app that runs stays")
			assert.DirExists(t, r.cfg.Applications)

			// The move can be tried again at once.
			m.copyErr, m.verifyErr = nil, nil
			got, err := r.service.MoveToApplications(t.Context())
			require.NoError(t, err)
			assert.Equal(t, update.MoveMoved, got.Outcome)
		})
	}
}

// The programs' words name the bundle that ran by its name alone, a translocated one's path
// being a folder macOS hides, and home as ~.
func TestAFailedMoveWordsThePathsPlainly(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t, func(c *update.Config) { c.Writable = func(string) bool { return false } })
	homeApps := filepath.Join(r.cfg.Home, "Applications")
	m.copyErr = uierr.New("ditto: "+r.cfg.Bundle+"/Contents/MacOS/"+brand.Slug+": Permission denied", errFakeDitto)

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "Couldn't copy "+brand.Name+" into ~/Applications: ditto: "+app+"/Contents/MacOS/"+brand.Slug+
		": Permission denied.", uierr.Message(err))

	// codesign names the copy where it checks it, which is by the target's name to the user.
	m.copyErr = nil
	m.onVerify = func(bundle string) { m.verifyErr = uierr.New(bundle+": invalid signature", errFakeCodesign) }

	_, err = r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "The copy in ~/Applications didn't pass its signature check, so nothing was moved: ~/Applications/"+
		app+": invalid signature.", uierr.Message(err))

	writeBundle(t, filepath.Join(homeApps, app), brand.BundleID, running)

	_, err = r.service.OpenInApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "The copy in ~/Applications didn't pass its signature check, so it wasn't opened: ~/Applications/"+
		app+": invalid signature. Move it to the Trash, then move this one there.", uierr.Message(err))
}

// A copy that can't be removed is named, for the user to remove: nothing else is touched.
func TestAFailedCopyThatCantBeRemovedIsNamed(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root can remove anything")
	}

	r, m := newMoveRig(t, func(c *update.Config) { c.Writable = func(string) bool { return false } })
	homeApps := filepath.Join(r.cfg.Home, "Applications")
	m.verifyErr = uierr.New("invalid signature", errFakeCodesign)

	var stage string

	m.onCopy = func(to string) {
		stage = filepath.Dir(to)
		require.NoError(t, os.Chmod(stage, 0o500)) //nolint:gosec // the move's own folder, made read-only
	}

	t.Cleanup(func() { _ = os.Chmod(stage, 0o700) }) //nolint:gosec // put back for t.TempDir's removal

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "The copy in ~/Applications didn't pass its signature check, so nothing was moved: invalid "+
		"signature. Move ~/Applications/"+filepath.Base(stage)+" to the Trash.", uierr.Message(err))
	assert.Regexp(t, stageName, filepath.Base(stage), "the hidden folder the copy was made in")
	assert.NoDirExists(t, filepath.Join(homeApps, app), "nothing is where the app goes")
}

func TestAMoveIsRefusedWhereThereIsNothingToMove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(c *update.Config)
		want   error
		words  string
	}{
		{
			name:   "a source build",
			change: func(c *update.Config) { c.Flavor = update.FlavorSource },
			want:   update.ErrOff,
			words:  "Updates are off in builds made from source.",
		},
		{
			name:   "in Applications already",
			change: func(c *update.Config) { c.Bundle = filepath.Join(c.Applications, "Tools", app) },
			want:   update.ErrCantMove,
			words:  brand.Name + " already runs from Applications.",
		},
		{
			name: "in an Applications folder the user can't change",
			change: func(c *update.Config) {
				c.Bundle = filepath.Join(c.Applications, "Tools", app)
				c.Writable = func(string) bool { return false }
			},
			want:  update.ErrCantMove,
			words: brand.Name + " is in an Applications folder already, which your account can't change.",
		},
		{
			name:   "outside a bundle",
			change: func(c *update.Config) { c.Bundle = "" },
			want:   update.ErrCantMove,
			words:  "This copy of " + brand.Name + " doesn't run from an app bundle, so there's nothing to move.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, m := newMoveRig(t, tt.change)
			assert.False(t, r.status(t).Location.Movable)

			for _, call := range []func(context.Context) (update.MoveResult, error){
				r.service.MoveToApplications, r.service.OpenInApplications,
			} {
				_, err := call(t.Context())
				require.ErrorIs(t, err, tt.want)
				assert.Equal(t, tt.words, uierr.Message(err))
			}

			assert.Empty(t, m.all())
		})
	}
}

// A restart into an update goes first: a move waits for it, in words.
func TestAMoveWaitsForARestartIntoAnUpdate(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	r.engine.MarkInstalling()

	for _, call := range []func(context.Context) (update.MoveResult, error){
		r.service.MoveToApplications, r.service.OpenInApplications,
	} {
		_, err := call(t.Context())
		require.ErrorIs(t, err, update.ErrUpdating)
		assert.Equal(t, brand.Name+" is restarting into an update. Move it once the update has opened.", uierr.Message(err))
	}

	assert.Empty(t, m.all())
	assert.NoDirExists(t, filepath.Join(r.cfg.Applications, app))
}

// A restart whose quit was held finishes first: the app is quitting on its own so the
// update's helper installs it, and a move waits for it, in words, copying nothing.
func TestAMoveWaitsForARestartThatIsFinishing(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	r.engine.MarkFinishing()

	for _, call := range []func(context.Context) (update.MoveResult, error){
		r.service.MoveToApplications, r.service.OpenInApplications,
	} {
		_, err := call(t.Context())
		require.ErrorIs(t, err, update.ErrUpdating)
		require.ErrorIs(t, err, update.ErrFinishing)
		assert.Equal(t, brand.Name+" is quitting to finish an update. Move it once the update has opened.",
			uierr.Message(err))
	}

	assert.Empty(t, m.all())
	assert.NoDirExists(t, filepath.Join(r.cfg.Applications, app))
	assert.NoDirExists(t, filepath.Join(r.cfg.Home, "Applications"))
}

func TestAMoveNeedsAMover(t *testing.T) {
	t.Parallel()

	r, _ := newMoveRig(t)
	r.engine.AttachMover(nil)

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrNoMover)
	assert.Equal(t, "This build can't move itself to Applications.", uierr.Message(err))
	assert.NoDirExists(t, filepath.Join(r.cfg.Applications, app))
}

func TestOneMoveAtATime(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	copying, release := make(chan struct{}), make(chan struct{})
	m.onCopy = func(string) {
		close(copying)
		<-release
	}

	done := make(chan error, 1)

	go func() {
		_, err := r.service.MoveToApplications(context.WithoutCancel(t.Context()))
		done <- err
	}()

	<-copying

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoving)
	assert.Equal(t, brand.Name+" is already moving to Applications.", uierr.Message(err))

	_, err = r.service.OpenInApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoving)

	close(release)
	require.NoError(t, <-done)

	assert.Len(t, m.all(), 3, "the first move ran alone")
}

// A move the window gives up on still ends, or cleans up, as it would have: its copy
// never stops halfway.
func TestAMoveOutlivesTheWindowsCall(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	m.onCopy = func(string) { cancel() }

	got, err := r.service.MoveToApplications(ctx)
	require.NoError(t, err)
	assert.Equal(t, update.MoveMoved, got.Outcome)
	assert.Len(t, m.all(), 3)
}

func TestMoveTellsWhenItCantLookWhereTheAppWouldGo(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root can look anywhere")
	}

	r, m := newMoveRig(t)
	require.NoError(t, os.Chmod(r.cfg.Applications, 0o000))

	t.Cleanup(func() { _ = os.Chmod(r.cfg.Applications, 0o700) }) //nolint:gosec // put back for t.TempDir's removal

	for _, call := range []func(context.Context) (update.MoveResult, error){
		r.service.MoveToApplications, r.service.OpenInApplications,
	} {
		_, err := call(t.Context())
		require.ErrorIs(t, err, update.ErrMoveFailed)
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Equal(t, brand.Name+" can't look in "+r.cfg.Applications+".", uierr.Message(err))
	}

	assert.Empty(t, m.all())
}

func TestAMoveIntoAFolderItCantWriteSaysSo(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	r, m := newMoveRig(t)
	require.NoError(t, os.Chmod(r.cfg.Applications, 0o500)) //nolint:gosec // the test's own folder, made read-only

	t.Cleanup(func() { _ = os.Chmod(r.cfg.Applications, 0o700) }) //nolint:gosec // put back for t.TempDir's removal

	// The engine is told the folder is writable, as it was when it looked.
	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "Couldn't copy "+brand.Name+" into "+r.cfg.Applications+", so nothing was moved.", uierr.Message(err))
	assert.Empty(t, m.all())
}

func TestAMoveIntoAMissingHomeSaysSo(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}

	r, m := newMoveRig(t, func(c *update.Config) { c.Writable = func(string) bool { return false } })
	require.NoError(t, os.Chmod(r.cfg.Home, 0o500)) //nolint:gosec // the test's own folder, made read-only

	t.Cleanup(func() { _ = os.Chmod(r.cfg.Home, 0o700) }) //nolint:gosec // put back for t.TempDir's removal

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "Couldn't make ~/Applications, so nothing was moved.", uierr.Message(err))
	assert.Empty(t, m.all())
}

func TestAMoveWithNoHomeToGoToSaysSo(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t, func(c *update.Config) {
		c.Home = ""
		c.Writable = func(string) bool { return false }
	})

	for _, call := range []func(context.Context) (update.MoveResult, error){
		r.service.MoveToApplications, r.service.OpenInApplications,
	} {
		_, err := call(t.Context())
		require.ErrorIs(t, err, update.ErrCantMove)
		assert.Equal(t, brand.Name+" can't tell where your Applications folder is.", uierr.Message(err))
	}

	assert.Empty(t, m.all())
}

// The app reached by another path than an Applications folder's, a link to it, is still
// the copy there: it is never offered as a copy of its own to open.
func TestTheRunningAppIsNeverItsOwnCopy(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t, func(c *update.Config) {
		c.Bundle = filepath.Join(filepath.Dir(c.Applications), "Linked", app)
	})
	writeBundle(t, filepath.Join(r.cfg.Applications, app), brand.BundleID, running)
	require.NoError(t, os.RemoveAll(filepath.Dir(r.cfg.Bundle)))
	require.NoError(t, os.Symlink(r.cfg.Applications, filepath.Dir(r.cfg.Bundle)))

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrCantMove)
	assert.Equal(t, brand.Name+" already runs from Applications.", uierr.Message(err))
	assert.Empty(t, m.all())
}

func TestOpenItOpensTheCopyInApplicationsOnceItChecksOut(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	target := filepath.Join(r.cfg.Applications, app)
	writeBundle(t, target, brand.BundleID, "0.1.0")

	got, err := r.service.OpenInApplications(t.Context())
	require.NoError(t, err)

	assert.Equal(t, update.MoveResult{Outcome: update.MoveOpening, Target: target}, got)
	assert.Equal(t, []string{"verify " + target, "open " + target}, m.all(), "checked, then opened once the app quits")
	assert.Equal(t, []string{target}, m.markAsked, "asked first whether macOS would open it where it is")
	assert.Equal(t, []string{r.cfg.Bundle}, m.likes, "checked against the app that runs")
	assert.FileExists(t, filepath.Join(target, "Contents", "MacOS", brand.Slug), "left as it was")
}

// A copy still marked as downloaded, put in Applications other than by Finder (cp or mv in
// Terminal), would open from a hidden folder, where it can't update itself: Open it refuses
// it, saying what to do, and leaves it as it was, mark and all.
func TestOpenItRefusesACopyMacOSWouldOpenFromAHiddenFolder(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	target := filepath.Join(r.cfg.Applications, app)
	writeBundle(t, target, brand.BundleID, "0.1.0")

	m.marked = true

	got, err := r.service.MoveToApplications(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.MoveExists, got.Outcome, "a move still finds it")

	_, err = r.service.OpenInApplications(t.Context())
	require.ErrorIs(t, err, update.ErrQuarantined)
	assert.Equal(t, "The copy in "+r.cfg.Applications+" is still marked as downloaded, so macOS would open it from "+
		"a hidden folder, where it can't update itself. Move it to the Trash, then move this one there.",
		uierr.Message(err))
	assert.Empty(t, m.all(), "nothing checked or opened")
	assert.FileExists(t, filepath.Join(target, "Contents", "MacOS", brand.Slug), "left as it was")
}

// A mark that can't be read stops nothing: the signature is still checked.
func TestOpenItWhoseMarkCantBeReadChecksTheSignature(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	r, m := newMoveRig(t, func(c *update.Config) { c.Logger = logs.Logger() })
	target := filepath.Join(r.cfg.Applications, app)
	writeBundle(t, target, brand.BundleID, "0.1.0")

	m.markErr = fs.ErrPermission

	got, err := r.service.OpenInApplications(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.MoveOpening, got.Outcome)
	assert.Equal(t, []string{"verify " + target, "open " + target}, m.all())
	assert.Contains(t, logs.String(), "could not read whether the copy in Applications is marked as downloaded")
}

// The copy is made and checked in a hidden folder beside the target, and takes the target's
// place only once it checks out, so a copy cut off partway, or never checked, is never where
// the app goes.
func TestACopyTakesTheTargetsPlaceOnlyOnceItChecksOut(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	target := filepath.Join(r.cfg.Applications, app)

	var checked string

	m.onVerify = func(bundle string) {
		checked = bundle

		assert.NoDirExists(t, target, "nothing is where the app goes while the copy is checked")
	}

	_, err := r.service.MoveToApplications(t.Context())
	require.NoError(t, err)

	stage := filepath.Dir(checked)
	assert.Equal(t, r.cfg.Applications, filepath.Dir(stage), "beside the target, in the same folder")
	assert.Regexp(t, stageName, filepath.Base(stage), "hidden, named as a move's own")
	assert.Equal(t, app, filepath.Base(checked), "the copy named as the app is")
	assert.NoDirExists(t, stage, "removed once the copy is in place")
	assert.FileExists(t, filepath.Join(target, "Contents", "Info.plist"))
}

// Something that appears where the app goes while the copy is made is never replaced, even
// an empty folder, which a plain rename would replace.
func TestSomethingThatAppearsWhileTheCopyIsMadeIsNeverReplaced(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	target := filepath.Join(r.cfg.Applications, app)
	m.onVerify = func(string) { require.NoError(t, os.Mkdir(target, 0o700)) }

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrTargetTaken)
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "Something appeared at "+target+" just now, so nothing was moved. Try again.", uierr.Message(err))

	listed, err := os.ReadDir(target)
	require.NoError(t, err)
	assert.Empty(t, listed, "what appeared is left as it was")
	assert.Empty(t, stages(t, r.cfg.Applications), "and the copy removed")
	assert.Len(t, m.all(), 2, "nothing opens")
}

// A move holds the app's quit from its start to its end, however it ends, so its copy is
// never cut off partway, and lets go of it once.
func TestAMoveHoldsTheQuitFromItsStartToItsEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, r *rig, m *fakeMover)
		call  func(s *update.Service) func(context.Context) (update.MoveResult, error)
		copy  bool
	}{
		{name: "a move that goes ahead", copy: true},
		{
			name:  "a copy that fails",
			setup: func(_ *testing.T, _ *rig, m *fakeMover) { m.copyErr = errFakeDitto },
			copy:  true,
		},
		{
			name: "a copy there already",
			setup: func(t *testing.T, r *rig, _ *fakeMover) {
				t.Helper()
				writeBundle(t, filepath.Join(r.cfg.Applications, app), brand.BundleID, running)
			},
		},
		{
			name: "something else there",
			setup: func(t *testing.T, r *rig, _ *fakeMover) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(r.cfg.Applications, app), []byte("x"), 0o600))
			},
		},
		{
			name: "Open it",
			setup: func(t *testing.T, r *rig, _ *fakeMover) {
				t.Helper()
				writeBundle(t, filepath.Join(r.cfg.Applications, app), brand.BundleID, running)
			},
			call: func(s *update.Service) func(context.Context) (update.MoveResult, error) { return s.OpenInApplications },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, m := newMoveRig(t)
			if tt.setup != nil {
				tt.setup(t, r, m)
			}

			call := r.service.MoveToApplications
			if tt.call != nil {
				call = tt.call(r.service)
			}

			_, _ = call(t.Context())

			holds, holding := m.held()
			assert.Equal(t, 1, holds, "one hold")
			assert.Zero(t, holding, "let go of as the move ends")

			if tt.copy {
				assert.Equal(t, []int{1}, m.heldOnCopy, "held while the copy is made")
			}
		})
	}

	// A move refused before it starts holds nothing.
	r, m := newMoveRig(t)
	r.engine.MarkInstalling()

	_, err := r.service.MoveToApplications(t.Context())
	require.ErrorIs(t, err, update.ErrUpdating)

	holds, _ := m.held()
	assert.Zero(t, holds)
}

// A move cut off partway (a crash, a power cut) leaves its hidden folder beside the target;
// a later move removes it, but only a folder named as a move's own, old enough that no move
// still runs in it, holding nothing, or nothing but a copy of the app. Anything else is
// left.
func TestALaterMoveRemovesWhatACutOffMoveLeft(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	apps := r.cfg.Applications
	old := time.Now().Add(-update.MoveLimit - 2*time.Minute)

	// leave makes a leftover named name, with what holds puts in it, last changed at.
	leave := func(name string, at time.Time, holds func(stage string)) string {
		stage := filepath.Join(apps, name)
		require.NoError(t, os.Mkdir(stage, 0o700))

		if holds != nil {
			holds(stage)
		}

		require.NoError(t, os.Chtimes(stage, at, at))

		return stage
	}
	ours := func(stage string) { writeBundle(t, filepath.Join(stage, app), brand.BundleID, running) }

	removed := []string{
		leave("."+app+".moving-1", old, ours),
		leave("."+app+".moving-2", old, nil),
	}
	kept := []string{
		leave("."+app+".moving-3", time.Now(), ours),
		leave("."+app+".moving-4", old, func(stage string) {
			writeBundle(t, filepath.Join(stage, app), "com.example.other", "1.0.0")
		}),
		leave("."+app+".moving-5", old, func(stage string) {
			ours(stage)
			require.NoError(t, os.WriteFile(filepath.Join(stage, "notes.txt"), []byte("mine"), 0o600))
		}),
		leave("."+app+".moving-6", old, func(stage string) {
			require.NoError(t, os.MkdirAll(filepath.Join(stage, app, "Contents"), 0o700))
		}),
		leave("."+app+".other", old, ours),
	}

	elsewhere := filepath.Join(filepath.Dir(apps), "Elsewhere")
	writeBundle(t, filepath.Join(elsewhere, app), brand.BundleID, running)

	link := filepath.Join(apps, "."+app+".moving-7")
	require.NoError(t, os.Symlink(elsewhere, link))

	file := filepath.Join(apps, "."+app+".moving-8")
	require.NoError(t, os.WriteFile(file, []byte("a file"), 0o600))

	got, err := r.service.MoveToApplications(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.MoveMoved, got.Outcome)
	assert.Len(t, m.all(), 3)

	for _, stage := range removed {
		assert.NoDirExists(t, stage)
	}

	for _, stage := range kept {
		assert.DirExists(t, stage)
	}

	assert.FileExists(t, file)
	assert.FileExists(t, filepath.Join(elsewhere, app, "Contents", "Info.plist"), "a link is never followed")

	_, err = os.Lstat(link)
	require.NoError(t, err, "nor removed")
}

func TestOpenItRefusesACopyThatDoesntCheckOut(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)
	target := filepath.Join(r.cfg.Applications, app)
	writeBundle(t, target, brand.BundleID, "0.1.0")
	m.verifyErr = uierr.New(target+": code object is not signed at all", errFakeCodesign)

	_, err := r.service.OpenInApplications(t.Context())
	require.ErrorIs(t, err, update.ErrMoveFailed)
	assert.Equal(t, "The copy in "+r.cfg.Applications+" didn't pass its signature check, so it wasn't opened: "+target+
		": code object is not signed at all. Move it to the Trash, then move this one there.", uierr.Message(err))
	assert.Equal(t, []string{"verify " + target}, m.all(), "nothing opens")
	assert.DirExists(t, target, "and nothing is removed")
}

func TestOpenItWithNoCopyInApplicationsSaysSo(t *testing.T) {
	t.Parallel()

	r, m := newMoveRig(t)

	_, err := r.service.OpenInApplications(t.Context())
	require.ErrorIs(t, err, update.ErrNoCopy)
	assert.Equal(t, brand.Name+" isn't in "+r.cfg.Applications+" any more. Move it there instead.", uierr.Message(err))
	assert.Empty(t, m.all())
}

func TestMoveToApplicationsIsOfferedWhereTheAppCantUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bundle func(apps, home string) string
		flavor update.Flavor
		kind   update.LocationKind
		want   bool
	}{
		{
			name: "in Downloads", bundle: func(_, home string) string { return filepath.Join(home, "Downloads", app) },
			kind: update.LocationElsewhere, want: true,
		},
		{
			name: "on a disk image", bundle: func(string, string) string { return "/Volumes/" + brand.Name + "/" + app },
			kind: update.LocationDiskImage, want: true,
		},
		{name: "translocated", bundle: func(string, string) string {
			return "/private/var/folders/xy/abc/T/AppTranslocation/0A1B/d/" + app
		}, kind: update.LocationTranslocated, want: true},
		{
			name: "in Applications", bundle: func(apps, _ string) string { return filepath.Join(apps, app) },
			kind: update.LocationApplications,
		},
		{
			name: "in home's Applications", bundle: func(_, home string) string { return filepath.Join(home, "Applications", app) },
			kind: update.LocationUserApplications,
		},
		{name: "outside a bundle", bundle: func(string, string) string { return "" }, kind: update.LocationElsewhere},
		{
			name: "a source build in Downloads", flavor: update.FlavorSource,
			bundle: func(_, home string) string { return filepath.Join(home, "Downloads", app) }, kind: update.LocationElsewhere,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t, func(c *update.Config) {
				c.Bundle = tt.bundle(c.Applications, c.Home)
				if tt.flavor != "" {
					c.Flavor = tt.flavor
				}
			})

			got := r.status(t).Location
			assert.Equal(t, tt.kind, got.Kind)
			assert.Equal(t, tt.want, got.Movable)
		})
	}
}

func TestShowAppInFinderShowsTheCopyThatRuns(t *testing.T) {
	t.Parallel()

	r, _ := newMoveRig(t)

	require.NoError(t, r.service.ShowAppInFinder(t.Context()))
	assert.Equal(t, []string{r.cfg.Bundle}, r.reveal.shown)
}

func TestShowAppInFinderShowsNothingItCantShow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(c *update.Config)
		words  string
	}{
		{
			name: "translocated",
			change: func(c *update.Config) {
				c.Bundle = "/private/var/folders/xy/abc/T/AppTranslocation/0A1B/d/" + app
			},
			words: "macOS opened this copy from a hidden folder, so Finder can't show where it is.",
		},
		{
			name:   "outside a bundle",
			change: func(c *update.Config) { c.Bundle = "" },
			words:  "This copy of " + brand.Name + " doesn't run from an app bundle.",
		},
		{
			name:   "with no Finder",
			change: func(c *update.Config) { c.Reveal = nil },
			words:  "This build can't show files in Finder.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t, tt.change)

			err := r.service.ShowAppInFinder(t.Context())
			require.ErrorIs(t, err, update.ErrCantMove)
			assert.Equal(t, tt.words, uierr.Message(err))
			assert.Empty(t, r.reveal.shown)
		})
	}
}

func TestBundleIDReadsTheInfoPlist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	write := func(name, plist string) string {
		bundle := filepath.Join(dir, name+".app")
		require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist), 0o600)) //nolint:gosec // the test's own bundle

		return bundle
	}

	id, err := update.BundleID(write("ours", infoPlist(brand.BundleID, running)))
	require.NoError(t, err)
	assert.Equal(t, brand.BundleID, id)

	generated, err := os.ReadFile(filepath.Join("..", "..", "build", "darwin", "Info.plist"))
	require.NoError(t, err)
	id, err = update.BundleID(write("generated", string(generated)))
	require.NoError(t, err)
	assert.Equal(t, brand.BundleID, id, "the Info.plist every build has")

	for name, plist := range map[string]string{
		"none":      `<plist version="1.0"><dict><key>CFBundleName</key><string>X</string></dict></plist>`,
		"last key":  `<plist version="1.0"><dict><key>CFBundleIdentifier</key></dict></plist>`,
		"not a str": `<plist version="1.0"><dict><key>CFBundleIdentifier</key><true/></dict></plist>`,
		"binary":    "bplist00\x01\x02\x03",
		"garbage":   "not a property list",
	} {
		_, err = update.BundleID(write(name, plist))
		require.Error(t, err, name)
	}

	_, err = update.BundleID(filepath.Join(dir, "missing.app"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}
