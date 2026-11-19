package integrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/toolinstall"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestInstallRefusesWhatItCantCarryOut(t *testing.T) {
	t.Parallel()

	t.Run("a tool it doesn't know", func(t *testing.T) {
		t.Parallel()

		h := newInstallHarness(t)
		h.ready(t)

		_, err := h.svc.Install(t.Context(), "example-tool")
		require.ErrorIs(t, err, integrationprefs.ErrUnknownIntegration)
	})

	t.Run("no installer", func(t *testing.T) {
		t.Parallel()

		h := newInstallHarness(t)
		h.svc.installer = nil
		h.ready(t)

		_, err := h.svc.Install(t.Context(), integrationprefs.MageX)
		require.ErrorIs(t, err, ErrInstallRefused)
		assert.Equal(t, brand.Name+" can't install tools here.", uierr.Message(err))
	})

	t.Run("no latest release", func(t *testing.T) {
		t.Parallel()

		h := newInstallHarness(t)
		h.releases.got = map[string]Release{}
		h.ready(t)

		_, err := h.svc.Install(t.Context(), integrationprefs.MageX)
		require.ErrorIs(t, err, ErrInstallRefused)
		assert.Equal(t, brand.Name+" couldn't find mage-x's latest release on GitHub.", uierr.Message(err))
		assert.NoFileExists(t, filepath.Join(h.m.pathB, "magex"), "nothing was placed")
	})
}

func TestAnInstallItCantRecordIsStillDone(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.svc.queries = NewService(Config{DB: testutil.ClosedDB(t)}).queries
	h.ready(t)

	got, err := h.svc.Install(t.Context(), integrationprefs.MageX)
	require.NoError(t, err, "the program is in place")
	assert.Equal(t, "Installed mage-x "+newerMagex+" in ~/.local/bin", got.Message)
	assert.FileExists(t, filepath.Join(h.m.pathB, "magex"))
	assert.Contains(t, h.logs.String(), "couldn't record an install")
}

func TestSmokeSaysWhatTheDownloadedProgramDid(t *testing.T) {
	t.Parallel()

	killedErr := runEnded(t, testutil.FakeRun{Sleep: time.Minute}, true)
	require.True(t, killed(killedErr))

	tests := []struct {
		name   string
		answer answer
		is     error
		says   string
	}{
		{
			name:   "it failed",
			answer: answer{stderr: "dyld: Library not loaded\nReferenced from: magex\n", err: errExit},
			is:     errExit, says: "dyld: Library not loaded: ",
		},
		{name: "macOS killed it on start", answer: answer{err: killedErr}, is: errRefusedToRun, says: "macOS refused to run it"},
		{
			name: "it printed no version", answer: answer{stdout: "Usage: magex [target]\n"},
			is: ErrNoVersion, says: "it printed no version",
		},
		{
			name: "it printed another version", answer: magexSays(recordedMagex),
			is: errWrongVersion, says: "it says " + recordedMagex + ", not " + newerMagex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newInstallHarness(t)
			path := h.m.put(filepath.Join(h.m.root, "downloads", "fresh"), tt.answer)

			err := h.svc.smoke(t.Context(), MageX(), newerMagex, path)
			require.ErrorIs(t, err, tt.is)
			assert.Contains(t, err.Error(), tt.says)
		})
	}
}

func TestSmokeGivesUpOnAProgramThatNeverAnswers(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	path := h.m.put(filepath.Join(h.m.root, "downloads", "fresh"), answer{hang: true})

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		err := h.svc.smoke(t.Context(), MageX(), newerMagex, path)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, err.Error(), "it didn't answer within "+smokeTimeout.String())
		assert.Equal(t, smokeTimeout, time.Since(start), "it waits its time limit, and no longer")
	})
}

func TestInstallErrorsSayWhatHappened(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	fresh := InstallPlan{Version: newerMagex, TargetDir: "~/.local/bin", Target: "~/.local/bin/magex"}
	over := fresh
	over.Replaces, over.ReplacesPath = recordedMagex, "~/.local/bin/magex"

	tests := []struct {
		name string
		plan InstallPlan
		err  error
		want string
	}{
		{
			name: "a new copy that didn't run", plan: fresh, err: toolinstall.ErrRolledBack,
			want: "The new magex didn't run, so " + brand.Name + " took it away again.",
		},
		{
			name: "a replacement that didn't run", plan: over, err: toolinstall.ErrRolledBack,
			want: "The new magex didn't run, so " + brand.Name + " put magex " + recordedMagex + " back.",
		},
		{
			name: "an old copy that couldn't go back", plan: over,
			err:  fmt.Errorf("%w: %w", toolinstall.ErrRollbackFailed, errReadOnly),
			want: "The new magex didn't run, and " + brand.Name + " couldn't put the old copy back: read-only file system",
		},
		{
			name: "a target that is a link", plan: over, err: toolinstall.ErrLinkTarget,
			want: "~/.local/bin/magex is a link, so " + brand.Name + " won't replace it.",
		},
		{
			name: "a folder it can't write", plan: fresh, err: fmt.Errorf("%w: %w", toolinstall.ErrWrite, errReadOnly),
			want: brand.Name + " can't write to ~/.local/bin: read-only file system. Nothing changed.",
		},
		{
			name: "no build for this Mac", plan: fresh, err: toolinstall.ErrNoBuild,
			want: "mage-x " + newerMagex + " has no build for this Mac (darwin/arm64).",
		},
		{
			name: "a download that failed", plan: fresh, err: errNotHere,
			want: brand.Name + " couldn't download mage-x " + newerMagex + ". Nothing on your Mac changed.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := h.svc.installError(MageX(), tt.plan, tt.err)
			require.ErrorIs(t, err, tt.err, "the cause is kept")
			assert.Equal(t, tt.want, uierr.Message(err))
		})
	}
}

func TestInstallProgressTellsTheWindowAtMostEveryQuarterSecond(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)

	synctest.Test(t, func(*testing.T) {
		progress := h.svc.progressFor(integrationprefs.MageX)
		told := func() InstallProgress {
			h.svc.mu.Lock()
			defer h.svc.mu.Unlock()

			return h.svc.installing[integrationprefs.MageX]
		}

		progress(toolinstall.Progress{Step: toolinstall.StepDownloading, Done: 1, Total: 10})
		assert.Equal(t, InstallProgress{Step: string(toolinstall.StepDownloading), Done: 1, Total: 10}, told(), "a new step")

		progress(toolinstall.Progress{Step: toolinstall.StepDownloading, Done: 2, Total: 10})
		assert.Equal(t, int64(1), told().Done, "the same step within a quarter second isn't told")

		progress(toolinstall.Progress{Step: toolinstall.StepDownloading, Done: 10, Total: 10})
		assert.Equal(t, int64(10), told().Done, "its end always is")

		time.Sleep(progressEvery)
		progress(toolinstall.Progress{Step: toolinstall.StepDownloading, Done: 3, Total: 10})
		assert.Equal(t, int64(3), told().Done, "nor is a step a quarter second on")
	})
}

func TestCleanupRemovesOnlyOldLeftovers(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathA, magexSays(recordedMagex))
	h.ready(t)

	now := h.clock.Now()
	old, recent := now.Add(-2*leftoverAge), now.Add(-time.Hour)

	files := map[string]time.Time{
		filepath.Join(h.m.pathA, "magex."+brand.Slug+"-old"): old,    // beside the copy in use
		filepath.Join(h.m.pathB, "magex."+brand.Slug+"-new"): old,    // in ~/.local/bin
		filepath.Join(h.m.pathA, "magex."+brand.Slug+"-new"): recent, // an install may still be placing it
		filepath.Join(h.m.pathA, "magex.backup"):             old,    // not the app's name
	}

	for path, at := range files {
		testutil.WriteFile(t, path, "x")
		require.NoError(t, os.Chtimes(path, at, at))
	}

	downloads := filepath.Join(h.m.root, "downloads", brand.Slug+"-123")
	require.NoError(t, os.MkdirAll(downloads, 0o700))
	require.NoError(t, os.Chtimes(downloads, old, old))

	h.svc.cleanup()

	assert.NoFileExists(t, filepath.Join(h.m.pathA, "magex."+brand.Slug+"-old"))
	assert.NoFileExists(t, filepath.Join(h.m.pathB, "magex."+brand.Slug+"-new"))
	assert.FileExists(t, filepath.Join(h.m.pathA, "magex."+brand.Slug+"-new"), "a recent leftover stays")
	assert.FileExists(t, filepath.Join(h.m.pathA, "magex.backup"), "a file the app didn't name stays")
	assert.NoDirExists(t, downloads, "an old download folder goes")
	assert.FileExists(t, filepath.Join(h.m.pathA, "magex"), "the copy itself stays")
}

func TestHashOfReadsOnlyAFileItCanRead(t *testing.T) {
	t.Parallel()
	testutil.SkipAsRoot(t)

	h := newInstallHarness(t)

	unreadable := filepath.Join(t.TempDir(), "magex")
	testutil.WriteFile(t, unreadable, "x")
	require.NoError(t, os.Chmod(unreadable, 0o000))

	_, ok := h.svc.hashOf(unreadable)
	assert.False(t, ok, "a file it can't open")

	_, ok = h.svc.hashOf(t.TempDir())
	assert.False(t, ok, "a folder, which reads as nothing")

	readable := filepath.Join(t.TempDir(), "magex")
	testutil.WriteFile(t, readable, "x")

	sum, ok := h.svc.hashOf(readable)
	require.True(t, ok)
	assert.Equal(t, hexSHA([]byte("x")), sum)
}

// finderTrashes answers Finder's delete by removing the file named last, as the Trash
// takes it away, and every other command as done.
func finderTrashes(_ context.Context, argv []string) ([]byte, []byte, error) {
	if argv[0] == osascript {
		return nil, nil, os.Remove(argv[len(argv)-1])
	}

	return nil, nil, nil
}

func TestTrashMovesAnotherCopyAndLooksAgain(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathA, magexSays(newerMagex))
	shadow := h.m.put(h.m.pathB, magexSays(recordedMagex))
	require.Len(t, h.ready(t)["mage-x"].Others, 1)

	h.commands.Script = finderTrashes

	got, err := h.svc.Trash(t.Context(), integrationprefs.MageX, shadow)
	require.NoError(t, err)
	assert.NoFileExists(t, shadow)

	for _, tl := range got.Tools {
		if tl.ID == integrationprefs.MageX {
			assert.Empty(t, tl.Others, "the list it returns was looked for again")
			assert.Equal(t, newerMagex, tl.Copy.Version.Raw, "the copy in use stays")
		}
	}
}

func TestTrashMovesTheCopyTheAppInstalled(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t).withRecords(t)
	h.ready(t)

	_, err := h.svc.Install(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)

	installed := filepath.Join(h.m.pathB, "magex")
	h.commands.Script = finderTrashes

	_, err = h.svc.Trash(t.Context(), integrationprefs.MageX, installed)
	require.NoError(t, err, "the copy in use, which the app installed, may go")
	assert.NoFileExists(t, installed)
}

func TestTrashAndRevealSayWhatFinderSaid(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathA, magexSays(newerMagex))
	shadow := h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.ready(t)

	h.commands.Stderr, h.commands.Err = "execution error: Finder got an error: Handler can't handle objects. (-10000)\n", errExit

	_, err := h.svc.Trash(t.Context(), integrationprefs.MageX, shadow)
	require.ErrorIs(t, err, errExit)
	assert.Equal(t, "Finder couldn't move ~/.local/bin/magex to the Trash: execution error: Finder got an error: "+
		"Handler can't handle objects. (-10000)", uierr.Message(err))

	h.commands.Stderr = "LSOpenURLsWithRole() failed with error -10810.\n"

	err = h.svc.Reveal(t.Context(), integrationprefs.MageX, shadow)
	require.ErrorIs(t, err, errExit)
	assert.Equal(t, "Finder couldn't show ~/.local/bin/magex: LSOpenURLsWithRole() failed with error -10810.",
		uierr.Message(err))

	assert.FileExists(t, shadow)
}

func TestTrashAndRevealNeedFinder(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathA, magexSays(newerMagex))
	shadow := h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.ready(t)
	h.svc.commands = nil

	_, err := h.svc.Trash(t.Context(), integrationprefs.MageX, shadow)
	require.ErrorIs(t, err, ErrNotACopy)
	assert.Equal(t, brand.Name+" can't move files to the Trash here.", uierr.Message(err))

	err = h.svc.Reveal(t.Context(), integrationprefs.MageX, shadow)
	require.ErrorIs(t, err, ErrNotACopy)
	assert.Equal(t, brand.Name+" can't open Finder here.", uierr.Message(err))

	err = h.svc.Reveal(t.Context(), integrationprefs.MageX, filepath.Join(h.m.root, "elsewhere", "magex"))
	require.ErrorIs(t, err, ErrNotACopy, "nor shows anything it didn't find")

	assert.FileExists(t, shadow)
}
