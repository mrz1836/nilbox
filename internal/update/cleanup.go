package update

import (
	"bytes"
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
	"github.com/mrz1836/nilbox/internal/paths"
)

// stagingPrefix starts the folders the desktop runtime's updater stages an update in,
// beside the bundle, and its helper's files in the temporary folder.
const stagingPrefix = "wails-update-"

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
//     <prefix><pid>-<n>.ready, which leak on every update: those of the process pid (the
//     app that last restarted to update), and those of another process whose log names
//     this bundle as its target, once HelperWindow old. Other apps' are never touched.
//
// A neighbor with a like name (<prefix>notes, <bundle>.bak2) is never touched.
func cleanUp(bundle, tempDir string, pid int, now time.Time) []error {
	errs := removeStaging(bundle)

	if bundle != "" {
		if err := removeIfExists(bundle + ".bak"); err != nil {
			errs = append(errs, err)
		}
	}

	return append(errs, removeHelperFiles(tempDir, bundle, pid, now)...)
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

// removeHelperFiles removes this app's helper logs and readiness files in tempDir: the
// process pid's (the one the last install's record names), and those of any other process
// whose log names this app's bundle as its target, once HelperWindow old. Another app's
// files, which share the folder and the names, are left alone.
func removeHelperFiles(tempDir, bundle string, pid int, now time.Time) []error {
	listed, err := os.ReadDir(tempDir)
	if err != nil {
		return []error{fmt.Errorf("look for an update's leftovers in the temporary folder: %w", err)}
	}

	var errs []error

	for _, name := range helperFiles(tempDir, listed, bundle, pid, now) {
		if err := removeIfExists(filepath.Join(tempDir, name)); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// helperFiles names the files of listed, in tempDir, that removeHelperFiles removes: this
// app's helper logs, then the readiness files of the processes they, or pid, are named for.
func helperFiles(tempDir string, listed []fs.DirEntry, bundle string, pid int, now time.Time) []string {
	ours := map[int]bool{}
	if pid > 0 {
		ours[pid] = true
	}

	var names []string

	for _, item := range listed {
		if owner, ok := ourHelperLog(tempDir, item, bundle, pid, now); ok {
			ours[owner] = true

			names = append(names, item.Name())
		}
	}

	for _, item := range listed {
		if owner, ok := fileOwner(readyPattern, item); ok && ours[owner] {
			names = append(names, item.Name())
		}
	}

	return names
}

// ourHelperLog reports whether item, in tempDir, is a helper log of this app's to remove,
// and the process it is named for: pid's, or another's naming bundle once HelperWindow old.
func ourHelperLog(tempDir string, item fs.DirEntry, bundle string, pid int, now time.Time) (int, bool) {
	owner, ok := fileOwner(helperLogPattern, item)
	if !ok {
		return 0, false
	}

	if owner == pid {
		return owner, true
	}

	info, err := item.Info()
	if err != nil || now.Sub(info.ModTime()) < HelperWindow {
		return 0, false
	}

	return owner, namesBundle(filepath.Join(tempDir, item.Name()), bundle)
}

// fileOwner returns the process the regular file item is named for under pattern, and
// whether it is one.
func fileOwner(pattern *regexp.Regexp, item fs.DirEntry) (int, bool) {
	m := pattern.FindStringSubmatch(item.Name())
	if m == nil || !item.Type().IsRegular() {
		return 0, false
	}

	owner, err := strconv.Atoi(m[1])

	return owner, err == nil
}

// helperStart starts the first line of the runtime's helper log, before the target's path.
const helperStart = "helper start: target="

// namesBundle reports whether the helper log at path is of an install into bundle: its
// first line, "<time>: helper start: target=<path> new=<path> pid=<pid>", names it.
func namesBundle(path, bundle string) bool {
	if bundle == "" {
		return false
	}

	head, err := paths.ReadHead(path, 4096)
	if err != nil {
		return false
	}

	line, _, _ := bytes.Cut(head, []byte("\n"))

	_, rest, ok := bytes.Cut(line, []byte(helperStart))
	if !ok {
		return false
	}

	target, _, ok := bytes.Cut(rest, []byte(" new="))

	return ok && sameBundle(string(target), bundle)
}

// sameBundle reports whether a and b are the same bundle: the same path, or, when both
// exist, the same folder.
func sameBundle(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}

	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)

	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

// removeIfExists removes path and what it holds; a missing one is no error.
func removeIfExists(path string) error {
	if err := os.RemoveAll(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove an update's leftover %s: %w", filepath.Base(path), err)
	}

	return nil
}
