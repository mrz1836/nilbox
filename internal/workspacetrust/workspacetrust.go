// Package workspacetrust marks the folder a session starts in trusted in the coding
// agent's own config, so a session launched there starts without the agent's workspace
// trust question.
//
// Claude Code asks once per repository whether to trust it and keeps the answer in its
// config file, under projects[<repository root>].hasTrustDialogAccepted. A session that
// starts in a worktree the app made cannot be answered for in advance otherwise; the agent
// has no flag that skips the question, and setting that key is how its documentation says
// to trust a folder by hand (see Grant and Claude).
//
// Every running Claude session shares the file and rewrites it often, so a grant saves the
// way the agent does: it takes the agent's lock (a "<file>.lock" directory), reads the file
// again under it, changes the one project entry, and replaces the file with a single
// rename. Neither side loses the other's changes.
//
// Codex keeps each project's trust in its config.toml instead; Codex appends the entry
// there, keeping the rest of the file as the user wrote it.
package workspacetrust

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// Config file names and keys.
const (
	// configName is the agent's config file: in a profile's config directory, or in the
	// home directory for the default profile.
	configName = ".claude.json"

	// legacyConfigName is the config file older agent versions kept in the config
	// directory. The agent still uses it instead when it holds anything.
	legacyConfigName = ".config.json"

	// defaultConfigDir is the default profile's config directory under home.
	defaultConfigDir = ".claude"

	// lockSuffix names the agent's lock on its config file.
	lockSuffix = ".lock"

	// keyProjects holds each project's entry, keyed by its root; keyTrusted is the entry's
	// trust answer.
	keyProjects = "projects"
	keyTrusted  = "hasTrustDialogAccepted"
)

// Lock timing, matching the agent's lock library.
const (
	// lockStale is how long a lock may go untouched before its holder counts as gone. A
	// live holder refreshes it twice as often.
	lockStale = 10 * time.Second

	// lockWait is how long a grant waits for a live holder to let go.
	lockWait = 3 * time.Second

	// lockPoll is how often a waiting grant tries the lock again.
	lockPoll = 20 * time.Millisecond
)

// File permissions.
const (
	// configPerm is a new config file's mode, the one the agent uses.
	configPerm = appdata.PrivateFilePerm

	// lockPerm is the lock directory's mode.
	lockPerm = appdata.PrivateDirPerm
)

var (
	// ErrRelativePath is returned for a config file or clone path that is not absolute.
	ErrRelativePath = errors.New("path must be absolute")

	// ErrNotObject is returned when the config file, or the part of it a grant changes,
	// is not a JSON object.
	ErrNotObject = errors.New("not a JSON object")

	// ErrBusy is returned when another process held the agent's config lock for longer
	// than a grant waits.
	ErrBusy = errors.New("the agent's config file is locked by another process; try again")
)

// ConfigFile returns the agent config file a session with configDir reads:
// <configDir>/.claude.json, or <home>/.claude.json for the default profile (configDir "").
// A .config.json that holds anything in the config directory (~/.claude for the default
// profile) is used instead, as the agent does for older installs. It returns "" for the
// default profile when home is "".
func ConfigFile(configDir, home string) string {
	dir, base := configDir, configDir

	if configDir == "" {
		if home == "" {
			return ""
		}

		dir, base = filepath.Join(home, defaultConfigDir), home
	}

	legacy := filepath.Join(dir, legacyConfigName)
	if info, err := os.Stat(legacy); err == nil && info.Mode().IsRegular() && info.Size() > 0 {
		return legacy
	}

	return filepath.Join(base, configName)
}

// Grant marks dir trusted in the agent config file at file, under the key the agent
// looks up: dir's real path, symlinks resolved, in Unicode NFC form. It reports whether
// it changed the file. Nothing is written when dir is already trusted, or when file's
// directory does not exist (a profile the agent has never run with, which it sets up on
// its own). A missing file is created.
func Grant(ctx context.Context, file, dir string) (bool, error) {
	if !filepath.IsAbs(file) || !filepath.IsAbs(dir) {
		return false, fmt.Errorf("%w: config file %q, clone %q", ErrRelativePath, file, dir)
	}

	key, err := projectKey(dir)
	if err != nil {
		return false, err
	}

	if _, err = os.Stat(filepath.Dir(file)); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}

	// Most launches find the clone trusted already, so look before taking the lock.
	if data, readErr := os.ReadFile(file); readErr == nil { //nolint:gosec // the agent's own config file
		if _, changed, grantErr := grant(data, key); grantErr == nil && !changed {
			return false, nil
		}
	}

	unlock, err := lock(ctx, file+lockSuffix, lockWait)
	if err != nil {
		return false, err
	}
	defer unlock()

	return grantLocked(file, key)
}

// grantLocked rereads the config file under the lock, trusts key, and replaces the file.
// A symlinked config file is followed, so the link itself stays in place.
func grantLocked(file, key string) (bool, error) {
	target, err := filepath.EvalSymlinks(file)
	if errors.Is(err, fs.ErrNotExist) {
		target, err = file, nil
	}

	if err != nil {
		return false, fmt.Errorf("resolve %s: %w", file, err)
	}

	data, mode, err := readConfig(target)
	if err != nil {
		return false, err
	}

	out, changed, err := grant(data, key)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", file, err)
	}

	if !changed {
		return false, nil
	}

	if err = replaceFile(target, out, mode); err != nil {
		return false, fmt.Errorf("write %s: %w", file, err)
	}

	return true, nil
}

// readConfig returns the config file's contents and mode: nothing and configPerm for a
// file that does not exist yet.
func readConfig(path string) ([]byte, fs.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, configPerm, nil
	}

	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", path, err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // the agent's own config file
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", path, err)
	}

	return data, info.Mode().Perm(), nil
}

// grant returns the config with key's project entry trusted, and whether that changed
// anything. Everything else is kept as it was, values byte for byte; only the key order
// becomes sorted, which the agent restores on its next save. Empty input is an empty
// config.
func grant(data []byte, key string) ([]byte, bool, error) {
	config, err := object(data)
	if err != nil {
		return nil, false, err
	}

	projects, err := object(config[keyProjects])
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", keyProjects, err)
	}

	entry, err := object(projects[key])
	if err != nil {
		return nil, false, fmt.Errorf("%s[%q]: %w", keyProjects, key, err)
	}

	if string(bytes.TrimSpace(entry[keyTrusted])) == "true" {
		return data, false, nil
	}

	entry[keyTrusted] = json.RawMessage("true")

	if projects[key], err = encode(entry, false); err != nil {
		return nil, false, err
	}

	if config[keyProjects], err = encode(projects, false); err != nil {
		return nil, false, err
	}

	out, err := encode(config, true)
	if err != nil {
		return nil, false, err
	}

	return out, true, nil
}

// object decodes a JSON object, keeping each value's bytes. Missing, empty, and null
// decode to an empty object; any other non-object is ErrNotObject.
func object(raw []byte) (map[string]json.RawMessage, error) {
	obj := map[string]json.RawMessage{}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return obj, nil
	}

	if trimmed[0] != '{' {
		return nil, ErrNotObject
	}

	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return obj, nil
}

// encode writes v as JSON without escaping <, >, and &, indented by two spaces as the
// agent writes its config when indent is set, and with no trailing newline.
func encode(v any, indent bool) ([]byte, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if indent {
		enc.SetIndent("", "  ")
	}

	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}

	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// replaceFile writes data to a temporary file beside path and renames it over path, so a
// reader sees the old file or the new one, never a partial one.
func replaceFile(path string, data []byte, mode fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}

	name := tmp.Name()

	err = errors.Join(writeAll(tmp, data, mode), tmp.Close())
	if err == nil {
		err = os.Rename(name, path)
	}

	if err != nil {
		_ = os.Remove(name)
	}

	return err
}

// writeAll writes data to f, sets its mode, and flushes it to disk.
func writeAll(f *os.File, data []byte, mode fs.FileMode) error {
	if _, err := f.Write(data); err != nil {
		return err
	}

	if err := f.Chmod(mode); err != nil {
		return err
	}

	return f.Sync()
}

// projectKey returns the key the agent keeps dir's trust under.
func projectKey(dir string) (string, error) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", dir, err)
	}

	return norm.NFC.String(filepath.ToSlash(resolved)), nil
}

// lock takes the agent's lock on its config file: the directory at path, as the agent's
// lock library makes it. A lock untouched for lockStale was left by a process that is
// gone and is cleared, as the agent clears it; a live holder is waited for up to wait.
// The returned func releases the lock.
func lock(ctx context.Context, path string, wait time.Duration) (func(), error) {
	deadline := time.Now().Add(wait)

	for {
		taken, err := tryLock(path)
		if err != nil {
			return nil, err
		}

		if taken {
			return func() { _ = os.Remove(path) }, nil
		}

		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("%w (%s)", ErrBusy, path)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockPoll):
		}
	}
}

// tryLock makes the lock directory once, clearing a stale one first. It reports whether
// it took the lock; false means another process holds it.
func tryLock(path string) (bool, error) {
	err := os.Mkdir(path, lockPerm)
	if err == nil {
		return true, nil
	}

	if !errors.Is(err, fs.ErrExist) {
		return false, fmt.Errorf("lock the agent's config file: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) <= lockStale || os.Remove(path) != nil {
		return false, nil //nolint:nilerr // the holder let go or is live; the caller tries again
	}

	return os.Mkdir(path, lockPerm) == nil, nil
}
