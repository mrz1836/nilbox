package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/logging"
)

const (
	// PromptSuffix ends the name of the prompt file written beside a launch script.
	PromptSuffix = ".prompt.md"

	// scriptFilePerm makes scripts runnable by the user only.
	scriptFilePerm fs.FileMode = 0o700

	// nameRandomBytes is how many random bytes end a script's base name, so two scripts
	// with the same prefix written in the same second never collide.
	nameRandomBytes = 4

	// scriptMaxAge is how long scripts and their prompt files are kept before they are
	// pruned.
	scriptMaxAge = 7 * 24 * time.Hour
)

// ErrInvalidScriptPrefix is returned for a script name prefix other than pr-<number>,
// issue-<number>, or run-<number>, which pruning would never remove.
var ErrInvalidScriptPrefix = errors.New("invalid script name prefix")

// scriptPrefix is what a script name's prefix must look like.
var scriptPrefix = regexp.MustCompile(`^(?:pr|issue|run)-[0-9]+$`)

// scriptFileName matches the names of the files a ScriptWriter writes, capturing the
// timestamp and the suffix from the first dot on, which is a script's extension or
// PromptSuffix. Pruning only ever touches files that match, with a known suffix.
var scriptFileName = regexp.MustCompile(`^(?:pr|issue|run)-[0-9]+-([0-9]{8}T[0-9]{6}Z)-[0-9a-f]{8}(\..*)$`)

// ScriptFiles are the files written for one script.
type ScriptFiles struct {
	// Script is the script a terminal runs.
	Script string

	// Prompt is the prompt file the script reads, or "" for a script that reads none.
	Prompt string
}

// ScriptWriter writes scripts into a folder only the user can read, each under a name of
// its own, and prunes the old ones. Its zero value renders with DefaultRenderer, names
// files from crypto/rand, and logs nothing.
type ScriptWriter struct {
	// Renderer renders the scripts; nil means DefaultRenderer.
	Renderer ScriptRenderer

	// Random supplies the random part of each name; nil means crypto/rand.
	Random io.Reader

	// Log receives pruning failures, which never fail a write; nil discards them.
	Log *slog.Logger
}

// WriteScript writes s, rendered with DefaultRenderer, as a new script in dir and returns
// its path, as ScriptWriter.Write does; random names it (nil means crypto/rand), and log
// receives pruning failures (nil discards them).
func WriteScript(dir, prefix string, s Script, now time.Time, random io.Reader, log *slog.Logger) (string, error) {
	return ScriptWriter{Random: random, Log: log}.Write(dir, prefix, s, now)
}

// Write writes s as a new script in dir, which is created (readable only by the user) if
// needed, and returns its path. The script is named <prefix>-<UTC stamp>-<8 random hex
// digits> with the renderer's extension, has mode 0700, and never replaces a file. prefix
// is pr-<number>, issue-<number>, or run-<number>; anything else is
// ErrInvalidScriptPrefix, and nothing is written.
//
// Afterwards it prunes the scripts in dir, and the prompt files beside them, older than
// seven days, judged by the stamp in their names. Pruning touches no other file, and a
// pruning failure is logged rather than failing the write.
func (w ScriptWriter) Write(dir, prefix string, s Script, now time.Time) (string, error) {
	files, err := w.write(dir, prefix, s, nil, now)

	return files.Script, err
}

// WriteWithPrompt is Write for a launch script that reads its prompt from a file: unless
// s.NoPrompt, it first writes prompt beside the script, under the same base name with
// PromptSuffix (mode 0600), and points s.PromptFile at it, whatever s.PromptFile said. A
// script that cannot be written takes its prompt file with it.
func (w ScriptWriter) WriteWithPrompt(dir, prefix string, s Script, prompt string, now time.Time) (ScriptFiles, error) {
	if s.NoPrompt {
		return w.write(dir, prefix, s, nil, now)
	}

	return w.write(dir, prefix, s, &prompt, now)
}

// write writes the script, and before it the prompt file when prompt is not nil, then
// prunes dir.
func (w ScriptWriter) write(dir, prefix string, s Script, prompt *string, now time.Time) (ScriptFiles, error) {
	if !scriptPrefix.MatchString(prefix) {
		return ScriptFiles{}, fmt.Errorf("%w: %q", ErrInvalidScriptPrefix, prefix)
	}

	renderer := w.Renderer
	if renderer == nil {
		renderer = DefaultRenderer()
	}

	if err := ensurePrivateDir(dir); err != nil {
		return ScriptFiles{}, err
	}

	base, err := w.base(dir, prefix, now)
	if err != nil {
		return ScriptFiles{}, err
	}

	files := ScriptFiles{Script: base + renderer.Extension()}

	if prompt != nil {
		files.Prompt = base + PromptSuffix

		if err = writeNewFile(files.Prompt, []byte(*prompt), appdata.PrivateFilePerm); err != nil {
			return ScriptFiles{}, err
		}

		s.PromptFile = files.Prompt
	}

	if err = writeNewFile(files.Script, renderer.Render(s), scriptFilePerm); err != nil {
		if files.Prompt != "" {
			_ = os.Remove(files.Prompt)
		}

		return ScriptFiles{}, err
	}

	pruneScripts(dir, now, renderer.Extension(), logging.Or(w.Log))

	return files, nil
}

// base returns the path in dir that a new script's files are named from, before their
// suffix: <prefix>-<UTC stamp>-<8 random hex digits>.
func (w ScriptWriter) base(dir, prefix string, now time.Time) (string, error) {
	random := w.Random
	if random == nil {
		random = rand.Reader
	}

	suffix := make([]byte, nameRandomBytes)
	if _, err := io.ReadFull(random, suffix); err != nil {
		return "", fmt.Errorf("name script files: %w", err)
	}

	return filepath.Join(dir, prefix+"-"+clock.Stamp(now)+"-"+hex.EncodeToString(suffix)), nil
}

// CheckScriptFile checks that path is a script a ScriptWriter with the default renderer
// wrote into dir: a regular file directly in dir, named as Write names scripts. Anything
// else, a prompt file, a link, or a file elsewhere among them, is ErrInvalidScriptPath, so
// a terminal is only ever handed a script the app wrote.
func CheckScriptFile(dir, path string) error {
	if err := validateScriptPath(path); err != nil {
		return err
	}

	name := filepath.Base(path)

	m := scriptFileName.FindStringSubmatch(name)
	if m == nil || m[2] != DefaultRenderer().Extension() || filepath.Join(dir, name) != path {
		return fmt.Errorf("%w: %q is not a script in %q", ErrInvalidScriptPath, path, dir)
	}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidScriptPath, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %q is not a regular file", ErrInvalidScriptPath, path)
	}

	return nil
}

// ensurePrivateDir creates dir if needed and makes it readable only by the user.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("create script directory: %w", err)
	}

	if err := os.Chmod(dir, appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("restrict script directory: %w", err)
	}

	return nil
}

// writeNewFile creates path, failing if it already exists, and writes data with the
// exact permissions perm (independent of the umask).
func writeNewFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // path is built under the app's own script directory
	if err != nil {
		return fmt.Errorf("create script file: %w", err)
	}

	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(perm)
	}

	if closeErr := f.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		_ = os.Remove(path)

		return fmt.Errorf("write script file: %w", err)
	}

	return nil
}

// pruneScripts removes the regular files in dir that are scripts with the extension ext,
// or the prompt files beside them, and whose name stamp is more than scriptMaxAge before
// now. The files just written carry now's stamp, so they are always kept.
func pruneScripts(dir string, now time.Time, ext string, log *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn("could not list old script files", "error", err)

		return
	}

	cutoff := now.Add(-scriptMaxAge)

	for _, entry := range entries {
		if !entry.Type().IsRegular() || !stampBefore(entry.Name(), ext, cutoff) {
			continue
		}

		if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			log.Warn("could not remove an old script file", "file", entry.Name(), "error", err)
		}
	}
}

// stampBefore reports whether name is a script with the extension ext, or a prompt file,
// whose stamp is before cutoff.
func stampBefore(name, ext string, cutoff time.Time) bool {
	m := scriptFileName.FindStringSubmatch(name)
	if m == nil || (m[2] != PromptSuffix && m[2] != ext) {
		return false
	}

	stamp, err := time.Parse(clock.StampLayout, m[1])

	return err == nil && stamp.Before(cutoff)
}
