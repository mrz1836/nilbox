//go:build mage

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
)

// freshPrefix starts the name of each sandbox data folder app:fresh and app:dev fresh=true
// make in the temporary folder.
const freshPrefix = brand.Slug + "-fresh-"

// Reset starts the build from source over on fresh data, as Settings → About → Reset does:
// it quits the running build, writes the request to start over beside the app's data, which
// the release shares, and opens the build again, building the bundle first when it is
// missing. The app's start carries the reset out: it sets the database aside as
// <db>.reset-<stamp> for a fresh one and brings back the prompts, the launch defaults, and
// your own scripts, then opens on the first-run setup. Nothing else is touched: your clones,
// worktrees, backups, logs, and GitHub stay as they are.
//
//	magex app:reset
func (App) Reset() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	paths, err := appdata.Resolve()
	if err != nil {
		return fmt.Errorf("find the app's data: %w", err)
	}

	bundle, exe, err := builtBundle()
	if err != nil {
		return err
	}

	return resetApp(paths, bundle, exe, appQuitTimeout)
}

// Fresh opens the app on a fresh data folder of its own, a sandbox for trying the first-run
// setup, or anything else, on clean data, leaving your own data exactly as it is: it builds
// the bundle when it is missing, quits the running app, makes $TMPDIR/<slug>-fresh-*,
// removing the ones earlier runs made, copies your prompt library and launch defaults in,
// and opens the app on it (--data-dir). The window shows a Test data chip while it runs on
// the sandbox; `magex app:run` goes back to your own data.
//
//	magex app:fresh
func (App) Fresh() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	paths, err := appdata.Resolve()
	if err != nil {
		return fmt.Errorf("find the app's data: %w", err)
	}

	bundle, exe, err := builtBundle()
	if err != nil {
		return err
	}

	return openFresh(paths, os.TempDir(), bundle, exe, appQuitTimeout)
}

// builtBundle returns the macOS app bundle built from source and its executable, building
// the bundle first when it is missing.
func builtBundle() (bundle, exe string, err error) {
	bundle, exe, err = appBundle()
	if errors.Is(err, errNoBundle) {
		if err = (App{}).Build(); err != nil {
			return "", "", err
		}

		bundle, exe, err = appBundle()
	}

	return bundle, exe, err
}

// resetApp does App.Reset's work for the data at paths and the app bundle, whose running
// copies get quitTimeout to quit: the request is written only once none runs, so a copy
// that holds its data open never meets it half way.
func resetApp(paths appdata.Paths, bundle, exe string, quitTimeout time.Duration) error {
	quit, err := quitRunningApp(exe, quitTimeout)
	if err != nil {
		return err
	}

	if err = appdata.RequestReset(paths.ResetRequest, appdata.ResetByMagex, time.Now()); err != nil {
		return err
	}

	if err = openBundle(quit, bundle); err != nil {
		return fmt.Errorf("the reset is asked for, and the next start carries it out: %w", err)
	}

	say("Starting " + brand.DevName + " over, on the data it shares with " + brand.Name + ". It keeps your prompts, " +
		"launch defaults, and your own scripts.")
	say("Your clones, worktrees, backups, and logs are left as they are.")
	say("To undo it, quit " + brand.DevName + ", then rename the newest " + filepath.Base(paths.DB) +
		".reset-<stamp> in " + paths.Root + " back to " + filepath.Base(paths.DB) + ".")

	return nil
}

// openFresh does App.Fresh's work: it quits the running copies of the app bundle, whose
// executable is exe, makes a sandbox data folder in tmp from the data at paths, and opens
// the bundle on it.
func openFresh(paths appdata.Paths, tmp, bundle, exe string, quitTimeout time.Duration) error {
	quit, err := quitRunningApp(exe, quitTimeout)
	if err != nil {
		return err
	}

	dir, err := freshDataDir(paths, tmp)
	if err != nil {
		return err
	}

	if err = openBundle(quit, "-n", bundle, "--args", appdata.DataDirFlag+"="+dir); err != nil {
		return err
	}

	say(brand.DevName + " is open on fresh data in " + dir + ".")
	say("Your own data is untouched; `magex app:run` goes back to it.")

	return nil
}

// freshDataDir makes a fresh sandbox data folder in tmp, removing the ones earlier runs
// made, and copies the prompt library and the launch defaults kept at paths into it, when
// there are copies, so the sandbox starts with the user's prompts. It returns the folder.
func freshDataDir(paths appdata.Paths, tmp string) (string, error) {
	earlier, err := filepath.Glob(filepath.Join(tmp, freshPrefix+"*"))
	if err != nil {
		return "", fmt.Errorf("find the earlier sandboxes: %w", err)
	}

	for _, dir := range earlier {
		if err = os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("remove an earlier sandbox: %w", err)
		}
	}

	dir, err := os.MkdirTemp(tmp, freshPrefix)
	if err != nil {
		return "", fmt.Errorf("make the sandbox: %w", err)
	}

	sandbox := appdata.NewPaths(dir)

	for from, to := range map[string]string{paths.Prompts: sandbox.Prompts, paths.LaunchDefaults: sandbox.LaunchDefaults} {
		if err = copyKept(from, to); err != nil {
			return "", err
		}
	}

	return dir, nil
}

// copyKept copies the file at from to a new, user-only file at to; a file that isn't there
// is nothing to copy.
func copyKept(from, to string) error {
	src, err := os.Open(from) //nolint:gosec // the app's own copy of the user's prompts or launch defaults
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("copy %s into the sandbox: %w", filepath.Base(from), err)
	}

	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, appdata.PrivateFilePerm) //nolint:gosec // a new file in the sandbox the target just made
	if err != nil {
		return fmt.Errorf("copy %s into the sandbox: %w", filepath.Base(from), err)
	}

	if _, err = io.Copy(dst, src); err != nil {
		return errors.Join(fmt.Errorf("copy %s into the sandbox: %w", filepath.Base(from), err), dst.Close())
	}

	return dst.Close()
}

// openBundle runs open with args: at once, or, just after a running copy of the app quit,
// trying again for a moment as openAfterQuit does.
func openBundle(quit bool, args ...string) error {
	if quit {
		return openRetrying(appReopenTimeout, args...)
	}

	out, err := exec.CommandContext(context.Background(), "open", args...).CombinedOutput() //nolint:gosec // argv only, no shell
	if err != nil {
		return fmt.Errorf("open %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return nil
}
