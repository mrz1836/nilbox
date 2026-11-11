package hooks

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode"
)

const (
	// envDir is the modular config's folder, and envBase and envCustom the legacy config's
	// files, all inside the clone.
	envDir    = ".github/env"
	envBase   = ".github/.env.base"
	envCustom = ".github/.env.custom"

	// envExt ends every file of the modular config.
	envExt = ".env"

	// configLimit caps how much of one config file is read.
	configLimit = 1 << 20

	// The config keys read: the switch that turns go-pre-commit off in the repository,
	// and the version its CI pins.
	keyEnabled   = "ENABLE_GO_PRE_COMMIT"
	keyCIVersion = "GO_PRE_COMMIT_VERSION"

	// checkKeyPrefix starts each check's switch, as in GO_PRE_COMMIT_ENABLE_MOD_TIDY.
	checkKeyPrefix = "GO_PRE_COMMIT_ENABLE_"
)

// errNotAFile is a config file or a hook that is there but isn't a regular file.
var errNotAFile = errors.New("not a regular file")

// repoConfig is a clone's go-pre-commit config, read as go-pre-commit reads it: the
// modular folder's .env files in sorted order, the last one setting a key winning, or
// else the legacy .env.base, then .env.custom over it. The process's own environment,
// which go-pre-commit also reads, isn't the clone's, so it plays no part.
type repoConfig struct {
	// present reports a config: a .env file in the modular folder, or .env.base.
	present bool

	// values are the keys the files set.
	values map[string]string
}

// readConfig reads the go-pre-commit config inside the clone at dir. A file that can't be
// read sets nothing.
func readConfig(dir string) repoConfig {
	_, files := configSource(dir)
	if len(files) == 0 {
		return repoConfig{values: map[string]string{}}
	}

	cfg := repoConfig{present: true, values: map[string]string{}}

	for _, f := range files {
		data, err := readSmall(f, configLimit)
		if err != nil {
			continue
		}

		for k, v := range parseEnv(data) {
			cfg.values[k] = v
		}
	}

	return cfg
}

// configSource is where the clone at dir keeps its go-pre-commit config, by the one rule the
// scan and Open the config share: what shows it (the modular folder when it holds a .env
// file, else the legacy .env.base) and the files to read, in order; both empty for none.
func configSource(dir string) (shown string, files []string) {
	folder := filepath.Join(dir, filepath.FromSlash(envDir))
	if files = modularFiles(folder); len(files) > 0 {
		return folder, files
	}

	base := filepath.Join(dir, filepath.FromSlash(envBase))
	if !isRegular(base) {
		return "", nil
	}

	return base, []string{base, filepath.Join(dir, filepath.FromSlash(envCustom))}
}

// modularFiles returns the .env files in folder, sorted, or none when folder isn't one.
// Only a regular file counts, or a link to one: a folder, a pipe, or a device named so is
// left out, so it is never read, nor counted as a config.
func modularFiles(folder string) []string {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil
	}

	var files []string

	for _, e := range entries {
		path := filepath.Join(folder, e.Name())
		if strings.HasSuffix(e.Name(), envExt) && regularEntry(e, path) {
			files = append(files, path)
		}
	}

	slices.Sort(files)

	return files
}

// regularEntry reports a folder's entry e, at path, that is a regular file, or a link to
// one.
func regularEntry(e fs.DirEntry, path string) bool {
	if e.Type()&fs.ModeSymlink != 0 {
		return isRegular(path)
	}

	return e.Type().IsRegular()
}

// enabled reports ENABLE_GO_PRE_COMMIT, which is on unless a file sets it to a false
// value; a value that isn't a boolean is on, as go-pre-commit reads it.
func (c repoConfig) enabled() bool {
	return c.flag(keyEnabled, true)
}

// checkOn reports whether check is on in the repository: gitleaks is off unless turned
// on, every other check on unless turned off.
func (c repoConfig) checkOn(check Check) bool {
	return c.flag(checkKey(check), check != CheckGitleaks)
}

// checkKey is check's switch in the config, as in GO_PRE_COMMIT_ENABLE_MOD_TIDY.
func checkKey(check Check) string {
	return checkKeyPrefix + strings.ToUpper(strings.ReplaceAll(string(check), "-", "_"))
}

// ciVersion is GO_PRE_COMMIT_VERSION, the go-pre-commit the repository's CI runs, without
// a leading v, or "".
func (c repoConfig) ciVersion() string {
	return strings.TrimPrefix(c.values[keyCIVersion], "v")
}

// flag reads a boolean key as strconv.ParseBool does, with def for one that is unset or
// not a boolean.
func (c repoConfig) flag(key string, def bool) bool {
	v, ok := c.values[key]
	if !ok || v == "" {
		return def
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}

	return b
}

// settings returns each check and whether the repository turns it on.
func (c repoConfig) settings() []CheckSetting {
	out := make([]CheckSetting, 0, len(Checks()))
	for _, check := range Checks() {
		out = append(out, CheckSetting{Name: check, On: c.checkOn(check)})
	}

	return out
}

// parseEnv reads an .env file as go-pre-commit's envfile package does: KEY=value lines,
// blank lines and # comments skipped, a value in matching quotes taken as it is, and an
// unquoted value cut at " #" or "\t#" and trimmed. A line without = is skipped.
func parseEnv(data []byte) map[string]string {
	out := map[string]string{}

	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if key = strings.TrimSpace(key); !ok || key == "" {
			continue
		}

		out[key] = envValue(value)
	}

	return out
}

// envValue reads one .env value.
func envValue(value string) string {
	value = strings.TrimLeftFunc(value, unicode.IsSpace)

	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' || first == '\'') && last == first {
			return value[1 : len(value)-1]
		}
	}

	if i := strings.Index(value, " #"); i >= 0 {
		value = value[:i]
	} else if i = strings.Index(value, "\t#"); i >= 0 {
		value = value[:i]
	}

	return strings.TrimRightFunc(value, unicode.IsSpace)
}

// isRegular reports a regular file at path, following a link.
func isRegular(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

// readSmall reads up to limit bytes of the regular file at path, following a link. A
// path that is a folder, a pipe, or a device isn't read, so a clone can't make a read
// wait on one.
func readSmall(path string, limit int64) ([]byte, error) {
	f, err := openRegular(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	var buf bytes.Buffer
	if _, err = io.Copy(&buf, io.LimitReader(f, limit)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// openRegular opens the regular file at path, following a link, and refuses anything else:
// a path that isn't one is never opened, and one swapped for a pipe or a device since it
// was looked at is refused by openChecked.
func openRegular(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, errNotAFile
	}

	return openChecked(path)
}

// openChecked opens path to read without waiting, as opening a pipe that nothing writes to
// otherwise would, and keeps it open only when what it opened is a regular file.
func openChecked(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // a file in a clone the user listed, only read
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errNotAFile
	}

	if err != nil {
		_ = f.Close()

		return nil, err
	}

	return f, nil
}
