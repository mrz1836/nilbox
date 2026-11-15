package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// stagingPrefix starts the folders the desktop runtime's updater stages an update in,
	// beside the bundle.
	stagingPrefix = "wails-update-"

	// strayAge is how old a helper's log or readiness file of another process must be
	// before the start's cleanup removes it, so a file another app's update is using
	// right now is left alone.
	strayAge = time.Hour
)

var (
	// helperLogPattern matches the runtime's helper log in the temporary folder:
	// <prefix><pid>.log.
	helperLogPattern = regexp.MustCompile(`^` + stagingPrefix + `([0-9]+)\.log$`)

	// readyPattern matches the runtime's readiness file in the temporary folder:
	// <prefix><pid>-<nanoseconds>.ready.
	readyPattern = regexp.MustCompile(`^` + stagingPrefix + `([0-9]+)-[0-9]+\.ready$`)
)

// helperLog is the runtime's helper log of the app whose process was pid.
func helperLog(tempDir string, pid int) string {
	return filepath.Join(tempDir, stagingPrefix+strconv.Itoa(pid)+".log")
}

// cleanUp removes what an update leaves behind, by exact name only, and returns what it
// couldn't remove:
//
//   - beside the running bundle, <bundle>.bak, the copy of the old app the runtime's helper
//     keeps while it swaps, and the runtime's staging folders, <prefix>*, that hold this
//     app's update (its bundle or its zip) or nothing;
//   - in the temporary folder, the helper's log, <prefix><pid>.log, and readiness file,
//     <prefix><pid>-<n>.ready, which leak on every update: the files of the process pid
//     (the app that last restarted to update) at once, and any other's once strayAge old.
//
// A neighbor with a like name (<prefix>notes, <bundle>.bak2) is never touched.
func cleanUp(bundle, tempDir string, pid int, now time.Time) []error {
	errs := removeStaging(bundle)

	if bundle != "" {
		if err := removeIfExists(bundle + ".bak"); err != nil {
			errs = append(errs, err)
		}
	}

	return append(errs, removeHelperFiles(tempDir, pid, now)...)
}

// removeStaging removes the runtime's staging folders beside bundle that hold this app's
// update, or nothing.
func removeStaging(bundle string) []error {
	if bundle == "" {
		return nil
	}

	dir := filepath.Dir(bundle)

	listed, err := os.ReadDir(dir)
	if err != nil {
		return []error{fmt.Errorf("look beside the app for an update's leftovers: %w", err)}
	}

	var errs []error

	for _, item := range listed {
		if path := filepath.Join(dir, item.Name()); ourStaging(item, path) {
			if err = removeIfExists(path); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

// ourStaging reports whether item, at path, is a staging folder of the runtime's that holds
// this app's update, or nothing.
func ourStaging(item fs.DirEntry, path string) bool {
	name := item.Name()
	if !item.IsDir() || !strings.HasPrefix(name, stagingPrefix) || len(name) == len(stagingPrefix) {
		return false
	}

	ours, err := holdsOurUpdate(path)

	return err == nil && ours
}

// holdsOurUpdate reports whether a staging folder holds this app's update, its bundle or
// its zip, or nothing at all; another app's is left alone.
func holdsOurUpdate(dir string) (bool, error) {
	listed, err := os.ReadDir(dir)
	if err != nil {
		return false, fmt.Errorf("read an update's staging folder: %w", err)
	}

	for _, item := range listed {
		name := item.Name()
		if name == brand.Name+".app" || (strings.HasPrefix(name, brand.Name+"_") && strings.HasSuffix(name, ".zip")) {
			return true, nil
		}
	}

	return len(listed) == 0, nil
}

// removeHelperFiles removes the helper's logs and readiness files in tempDir: pid's at
// once, any other's once strayAge old.
func removeHelperFiles(tempDir string, pid int, now time.Time) []error {
	listed, err := os.ReadDir(tempDir)
	if err != nil {
		return []error{fmt.Errorf("look for an update's leftovers in the temporary folder: %w", err)}
	}

	var errs []error

	for _, item := range listed {
		if leftHelperFile(item, pid, now) {
			if err = removeIfExists(filepath.Join(tempDir, item.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errs
}

// leftHelperFile reports whether item is a helper's log or readiness file to remove: pid's,
// or another's once strayAge old.
func leftHelperFile(item fs.DirEntry, pid int, now time.Time) bool {
	owner, ok := helperFileOwner(item.Name())
	if !ok || !item.Type().IsRegular() {
		return false
	}

	if owner == pid {
		return true
	}

	info, err := item.Info()

	return err == nil && now.Sub(info.ModTime()) >= strayAge
}

// helperFileOwner returns the process a helper's log or readiness file is named for, and
// whether name is one.
func helperFileOwner(name string) (int, bool) {
	m := helperLogPattern.FindStringSubmatch(name)
	if m == nil {
		m = readyPattern.FindStringSubmatch(name)
	}

	if m == nil {
		return 0, false
	}

	pid, err := strconv.Atoi(m[1])

	return pid, err == nil
}

// removeIfExists removes path and what it holds; a missing one is no error.
func removeIfExists(path string) error {
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove an update's leftover %s: %w", filepath.Base(path), err)
	}

	return nil
}
