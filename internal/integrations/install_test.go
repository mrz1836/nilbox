package integrations

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/toolinstall"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errNotHere stands for a download URL the fake release doesn't serve, errReadOnly a folder
// that can't be written, and errPlain an error that joins nothing.
var (
	errNotHere  = errors.New("no such download")
	errReadOnly = errors.New("read-only file system")
	errPlain    = errors.New("plain")
)

// files serves a release's files by URL, as DownloadAsset does.
type files map[string][]byte

func (f files) DownloadAsset(_ context.Context, rawURL string, w io.Writer, _ int64) (int64, error) {
	data, ok := f[rawURL]
	if !ok {
		return 0, errNotHere
	}

	n, err := w.Write(data)

	return int64(n), err
}

// hexSHA returns data's SHA-256 in hex.
func hexSHA(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// magexRelease builds mage-x's release at newerMagex for darwin/arm64, whose magex prints
// that version, and returns its files and the release GitHub would list.
func magexRelease(t *testing.T) (files, Release) {
	t.Helper()

	program := []byte(magexSays(newerMagex).stdout)

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "magex", Typeflag: tar.TypeReg, Size: int64(len(program)), Mode: 0o755}))
	_, err := tw.Write(program)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	archive := buf.Bytes()
	archiveName := MageX().Asset(newerMagex, "darwin", "arm64")
	sums := []byte(hexSHA(archive) + "  " + archiveName + "\n")
	base := "https://github.com/mrz1836/mage-x/releases/download/v" + newerMagex + "/"

	served := files{base + archiveName: archive, base + "sums": sums}

	return served, Release{
		Tag: "v" + newerMagex, URL: "https://github.com/mrz1836/mage-x/releases/tag/v" + newerMagex,
		Assets: []Asset{
			{Name: archiveName, URL: base + archiveName, Size: int64(len(archive)), Digest: "sha256:" + hexSHA(archive)},
			{Name: MageX().Checksums(newerMagex), URL: base + "sums", Size: int64(len(sums))},
		},
	}
}

// installHarness is a harness whose service can install mage-x's release.
type installHarness struct {
	*harness

	served   files
	commands *testutil.Runner
	busy     bool
}

func newInstallHarness(t *testing.T) *installHarness {
	t.Helper()

	h := &installHarness{harness: newHarness(t), commands: &testutil.Runner{}}

	served, release := magexRelease(t)
	h.served = served
	h.releases.got = map[string]Release{"mrz1836/mage-x": release}

	h.svc.queries = nil
	h.svc.installer = &toolinstall.Installer{
		Downloads: filepath.Join(h.m.root, "downloads"), Download: h.served, Slug: brand.Slug,
	}
	h.svc.commands = h.commands
	h.svc.goos, h.svc.goarch = "darwin", "arm64"
	h.svc.inUse = func(integrationprefs.ID) bool { return h.busy }

	return h
}

// withRecords keeps the installs in a database, so copies read as installed by the app.
func (h *installHarness) withRecords(t *testing.T) *installHarness {
	t.Helper()

	svc := NewService(Config{DB: testutil.DB(t)})
	h.svc.queries = svc.queries

	return h
}

// ready detects and checks for updates, as the worker does at start.
func (h *installHarness) ready(t *testing.T) map[integrationprefs.ID]Tool {
	t.Helper()

	require.NoError(t, h.svc.detect(t.Context()))
	require.NoError(t, h.svc.checkUpdates(t.Context(), 0))

	_, tools := h.list(t)

	return tools
}

func TestInstallPlacesTheLatestRelease(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t).withRecords(t)

	tools := h.ready(t)
	assert.Equal(t, InstallPlan{
		Action: ActionInstall, Version: newerMagex, Archive: "mage-x_1.30.2_darwin_arm64.tar.gz",
		TargetDir: "~/.local/bin", Target: "~/.local/bin/magex",
	}, tools["mage-x"].Install, "a missing tool goes in ~/.local/bin")

	got, err := h.svc.Install(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)
	assert.Equal(t, "Installed mage-x "+newerMagex+" in ~/.local/bin", got.Message)

	var magex Tool

	for _, tl := range got.Tools.Tools {
		if tl.ID == integrationprefs.MageX {
			magex = tl
		}
	}

	assert.Equal(t, StateFound, magex.State)
	assert.Equal(t, newerMagex, magex.Copy.Version.Raw)
	assert.Equal(t, SourceApp, magex.Copy.Source, "the copy reads as installed by the app")
	assert.Equal(t, "mage-x_1.30.2_darwin_arm64.tar.gz", magex.Copy.Archive)
	assert.NotEmpty(t, magex.Copy.InstalledAt)
	assert.Empty(t, magex.Progress.Step, "no install runs any more")
	assert.GreaterOrEqual(t, h.events.Count(), 6, "the window heard each step")
}

func TestUpdateReplacesTheCopyInPlace(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	old := h.m.put(h.m.pathB, magexSays(recordedMagex))

	tools := h.ready(t)
	plan := tools["mage-x"].Install
	assert.Equal(t, ActionUpdate, plan.Action)
	assert.Equal(t, recordedMagex, plan.Replaces)
	assert.True(t, plan.Foreign, "a copy the app didn't install: the window asks first")
	assert.Equal(t, "~/.local/bin/magex", plan.Target)

	got, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)
	assert.Equal(t, "Updated magex to "+newerMagex, got.Message)

	data, err := os.ReadFile(old) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, magexSays(newerMagex).stdout, string(data), "replaced in place")
}

func TestInstallLeavesHomebrewAndLinksAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(m *machine) string
		blocked string
	}{
		{
			name: "Homebrew",
			setup: func(m *machine) string {
				target := m.put(filepath.Join(m.root, "opt", "homebrew", "Cellar", "magex", recordedMagex, "bin"), magexSays(recordedMagex))
				m.link(filepath.Join(m.brew, "magex"), target)

				return target
			},
			blocked: "Homebrew manages this copy. Update it with brew upgrade magex.",
		},
		{
			name: "a link",
			setup: func(m *machine) string {
				target := m.put(filepath.Join(m.home, "projects", "mage-x", "bin"), magexSays(recordedMagex))
				m.link(filepath.Join(m.pathB, "magex"), target)

				return target
			},
			blocked: "~/.local/bin/magex is a link to ~/projects/mage-x/bin/magex, so " + brand.Name + " won't replace it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newInstallHarness(t)
			target := tt.setup(h.m)

			tools := h.ready(t)
			assert.Equal(t, tt.blocked, tools["mage-x"].Install.Blocked)

			_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
			require.ErrorIs(t, err, ErrInstallRefused)
			assert.Equal(t, tt.blocked, uierr.Message(err))

			data, err := os.ReadFile(target) //nolint:gosec // the test's own file
			require.NoError(t, err)
			assert.Empty(t, data, "never touched")
		})
	}
}

func TestInstallRefusesWhileARunUsesTheTool(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.busy = true

	tools := h.ready(t)
	assert.Equal(t, "A run is using magex. Update it when the run ends.", tools["mage-x"].Install.Blocked)

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, ErrInstallRefused)
}

func TestAGoInstallCopyGetsANewCopyInLocalBin(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.goBin, magexSays(recordedMagex))

	plan := h.ready(t)["mage-x"].Install
	assert.Equal(t, "~/.local/bin/magex", plan.Target)
	assert.Empty(t, plan.Replaces, "go install's copy stays where it is")
	assert.False(t, plan.Foreign)

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(h.m.pathB, "magex"))
	assert.FileExists(t, filepath.Join(h.m.goBin, "magex"))
}

func TestInstallSaysToSignInWhenSignedOut(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.releases.err = ErrSignedOut

	require.NoError(t, h.svc.detect(t.Context()))
	require.ErrorIs(t, h.svc.checkUpdates(t.Context(), 0), ErrSignedOut)

	_, tools := h.list(t)
	assert.Equal(t, "Sign in to GitHub first, so "+brand.Name+" can find the latest release.", tools["mage-x"].Install.Blocked)

	_, err := h.svc.Install(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, ErrInstallRefused)
}

func TestInstallFailuresAreWorded(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.ready(t)

	for url, data := range h.served {
		if !strings.HasSuffix(url, "sums") {
			h.served[url] = append(bytes.Clone(data), 0)
		}
	}

	_, err := h.svc.Install(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, toolinstall.ErrShortDownload)
	assert.Equal(t, "The download was cut short, so "+brand.Name+" didn't install it. Nothing on your Mac changed.",
		uierr.Message(err))
	assert.NoFileExists(t, filepath.Join(h.m.pathB, "magex"))
}

func TestTrashFallsBackToRevealWhenAutomationIsDenied(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathA, magexSays(newerMagex))
	shadow := h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.ready(t)

	h.commands.Stderr = "execution error: Not authorized to send Apple events to Finder. (-1743)"
	h.commands.Err = errExit

	_, err := h.svc.Trash(t.Context(), integrationprefs.MageX, shadow)
	require.ErrorIs(t, err, ErrAutomationDenied)
	assert.Equal(t, brand.Name+" isn't allowed to control Finder, so it can't move this to the Trash.", uierr.Message(err))

	calls := h.commands.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "/usr/bin/osascript", calls[0][0])
	assert.Equal(t, shadow, calls[0][len(calls[0])-1], "the path is an argument")

	for _, arg := range calls[0][:len(calls[0])-1] {
		assert.NotContains(t, arg, shadow, "never written into the script")
	}

	h.commands.Stderr, h.commands.Err = "", nil

	require.NoError(t, h.svc.Reveal(t.Context(), integrationprefs.MageX, shadow))
	assert.Equal(t, []string{"/usr/bin/open", "-R", shadow}, h.commands.Calls()[1])
}

func TestTrashMovesOnlyACopyItFound(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	inUse := h.m.put(h.m.pathA, magexSays(recordedMagex))
	h.ready(t)

	for _, path := range []string{inUse, filepath.Join(h.m.root, "elsewhere", "magex"), ""} {
		_, err := h.svc.Trash(t.Context(), integrationprefs.MageX, path)
		require.ErrorIs(t, err, ErrNotACopy, "%q: the copy in use, which the app didn't install, or no copy at all", path)
	}

	assert.Empty(t, h.commands.Calls(), "Finder is never asked")
}

func TestSmokeTellsAStopFromMacOSRefusingIt(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := h.svc.smoke(ctx, MageX(), newerMagex, filepath.Join(h.m.root, "anything"))
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, errRefusedToRun, "a stopped install is said to have stopped")
}

func TestAWriteFailureKeepsItsReason(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %w", toolinstall.ErrWrite, errReadOnly)

	assert.Equal(t, "read-only file system", joinedCause(err, toolinstall.ErrWrite))
	assert.Equal(t, "plain", joinedCause(errPlain, toolinstall.ErrWrite))
}

func TestAnInstallIntoLocalBinNamesTheCopyThere(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	goCopy := h.m.put(h.m.goBin, magexSays(recordedMagex))
	h.m.put(h.m.pathB, magexSays("1.28.0"))
	require.NoError(t, h.prefs.Set(t.Context(), integrationprefs.KeyPath(integrationprefs.MageX), goCopy))

	plan := h.ready(t)["mage-x"].Install
	assert.Equal(t, "~/.local/bin/magex", plan.Target, "go install's copy stays; the new one goes in ~/.local/bin")
	assert.Equal(t, "1.28.0", plan.Replaces, "and replaces what is there")
	assert.Equal(t, "~/.local/bin/magex", plan.ReplacesPath)
	assert.True(t, plan.Foreign, "which the app didn't install, so the window asks first")
}

func TestAnInstallIntoLocalBinRefusesALinkThere(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	goCopy := h.m.put(h.m.goBin, magexSays(recordedMagex))
	require.NoError(t, os.Symlink(goCopy, filepath.Join(h.m.pathB, "magex")))
	require.NoError(t, h.prefs.Set(t.Context(), integrationprefs.KeyPath(integrationprefs.MageX), goCopy))

	plan := h.ready(t)["mage-x"].Install
	assert.Equal(t, "~/.local/bin/magex is a link, so "+brand.Name+" won't replace it.", plan.Blocked)
}

func TestReinstallingOverABrokenCopySaysSo(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathB, answer{stderr: "dyld: Library not loaded\n", err: errExit})

	plan := h.ready(t)["mage-x"].Install
	require.Equal(t, ActionReinstall, plan.Action)
	assert.Empty(t, plan.Replaces, "a copy that won't run has no version")

	got, err := h.svc.Reinstall(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)
	assert.Equal(t, "Reinstalled mage-x "+newerMagex, got.Message)
}

func TestTrashLeavesALinkAlone(t *testing.T) {
	t.Parallel()

	h := newInstallHarness(t)
	h.m.put(h.m.pathA, magexSays(newerMagex))
	target := h.m.put(filepath.Join(h.m.home, "projects", "mage-x", "bin"), magexSays(recordedMagex))
	link := filepath.Join(h.m.pathB, "magex")
	h.m.link(link, target)
	h.ready(t)

	_, err := h.svc.Trash(t.Context(), integrationprefs.MageX, link)
	require.ErrorIs(t, err, ErrNotACopy, "Finder would trash what the link points to")
	assert.Empty(t, h.commands.Calls())
	assert.FileExists(t, target)
}
