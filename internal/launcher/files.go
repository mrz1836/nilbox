package launcher

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
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/logging"
)

const (
	// scriptFilePerm makes launch scripts runnable by the user only.
	scriptFilePerm fs.FileMode = 0o700

	// promptFileSuffix ends every prompt file name.
	promptFileSuffix = ".prompt.md"

	// suffixBytes is how many random bytes end a launch file's base name, so two launches
	// of the same item in the same second never collide.
	suffixBytes = 4

	// launchFileMaxAge is how long launch files are kept before they are pruned.
	launchFileMaxAge = 7 * 24 * time.Hour
)

var (
	// ErrEmptyPrompt is returned when a launch has no prompt text.
	ErrEmptyPrompt = errors.New("prompt is empty")

	// ErrResumePrompt is returned when a launch that resumes a session is given a prompt,
	// which the session would never see.
	ErrResumePrompt = errors.New("a resumed session takes no prompt")
)

// launchFileName matches the names of files WriteLaunchFiles creates, capturing the
// timestamp and the suffix from the first dot on, which is the prompt file's or a
// script's. Pruning only ever touches files that match, with a known suffix.
var launchFileName = regexp.MustCompile(`^(?:pr|issue)-[0-9]+-([0-9]{8}T[0-9]{6}Z)-[0-9a-f]{8}(\..*)$`)

// LaunchFiles are the files written for one launch.
type LaunchFiles struct {
	// Script is the script a terminal runs to start the session.
	Script string

	// Prompt is the file the script reads the prompt from, or "" for a session resumed,
	// which takes none.
	Prompt string
}

// WriteLaunchFiles writes a launch's prompt file and script into paths.Launch, which is
// created (readable only by the user) if needed. The files are named
// <kind>-<number>-<UTC stamp>-<random>.prompt.md (mode 0600) and the matching script
// (mode 0700, with the renderer's extension). The script reads the prompt from the
// prompt file, whatever plan.PromptFile said. A NoPrompt plan, which resumes a session,
// writes the script alone, and prompt must be "".
//
// Afterwards it prunes launch files older than seven days, judged by the timestamp in
// their names. Pruning never touches other files, and a pruning failure is logged to log
// (nil discards it) rather than failing the launch.
func WriteLaunchFiles(
	paths appdata.Paths, plan Plan, prompt string, now time.Time, log *slog.Logger,
) (LaunchFiles, error) {
	return writeLaunchFiles(paths, plan, prompt, now, launchFileDeps{
		renderer: terminal.DefaultRenderer(), random: rand.Reader, log: logging.Or(log),
	})
}

// launchFileDeps are what writeLaunchFiles uses besides its inputs: the script renderer,
// the randomness that names the files, and the logger pruning failures go to.
type launchFileDeps struct {
	renderer terminal.ScriptRenderer
	random   io.Reader
	log      *slog.Logger
}

// writeLaunchFiles is WriteLaunchFiles with its dependencies injected.
func writeLaunchFiles(
	paths appdata.Paths, plan Plan, prompt string, now time.Time, deps launchFileDeps,
) (LaunchFiles, error) {
	if err := validateLaunchFiles(plan, prompt); err != nil {
		return LaunchFiles{}, err
	}

	if err := ensurePrivateDir(paths.Launch); err != nil {
		return LaunchFiles{}, err
	}

	suffix := make([]byte, suffixBytes)
	if _, err := io.ReadFull(deps.random, suffix); err != nil {
		return LaunchFiles{}, fmt.Errorf("name launch files: %w", err)
	}

	base := filepath.Join(paths.Launch, string(plan.Kind)+"-"+strconv.Itoa(plan.Number)+"-"+
		clock.Stamp(now)+"-"+hex.EncodeToString(suffix))
	files := LaunchFiles{Script: base + deps.renderer.Extension()}

	if !plan.NoPrompt {
		files.Prompt = base + promptFileSuffix

		if err := writeNewFile(files.Prompt, []byte(prompt), appdata.PrivateFilePerm); err != nil {
			return LaunchFiles{}, err
		}
	}

	plan.PromptFile = files.Prompt

	if err := writeNewFile(files.Script, deps.renderer.Render(plan.script()), scriptFilePerm); err != nil {
		if files.Prompt != "" {
			_ = os.Remove(files.Prompt)
		}

		return LaunchFiles{}, err
	}

	pruneLaunchFiles(paths.Launch, now, deps.renderer.Extension(), deps.log)

	return files, nil
}

// validateLaunchFiles checks what the file names and contents are built from.
func validateLaunchFiles(plan Plan, prompt string) error {
	if !plan.Kind.Valid() {
		return fmt.Errorf("%w: %q", item.ErrInvalidKind, plan.Kind)
	}

	if plan.Number <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidNumber, plan.Number)
	}

	switch {
	case plan.NoPrompt && prompt != "":
		return ErrResumePrompt
	case !plan.NoPrompt && prompt == "":
		return ErrEmptyPrompt
	default:
		return nil
	}
}

// ensurePrivateDir creates dir if needed and makes it readable only by the user.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("create launch directory: %w", err)
	}

	if err := os.Chmod(dir, appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("restrict launch directory: %w", err)
	}

	return nil
}

// writeNewFile creates path, failing if it already exists, and writes data with the
// exact permissions perm (independent of the umask).
func writeNewFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // path is built under the app's own launch directory
	if err != nil {
		return fmt.Errorf("create launch file: %w", err)
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

		return fmt.Errorf("write launch file: %w", err)
	}

	return nil
}

// pruneLaunchFiles removes the regular files in dir that are launch files, prompts or
// scripts with the extension ext, and whose name stamp is more than launchFileMaxAge
// before now. The files just written carry now's stamp, so they are always kept.
func pruneLaunchFiles(dir string, now time.Time, ext string, log *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn("could not list old launch files", "error", err)

		return
	}

	cutoff := now.Add(-launchFileMaxAge)

	for _, entry := range entries {
		if !entry.Type().IsRegular() || !stampBefore(entry.Name(), ext, cutoff) {
			continue
		}

		if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			log.Warn("could not remove an old launch file", "file", entry.Name(), "error", err)
		}
	}
}

// stampBefore reports whether name is a launch file, a prompt or a script with the
// extension ext, whose stamp is before cutoff.
func stampBefore(name, ext string, cutoff time.Time) bool {
	m := launchFileName.FindStringSubmatch(name)
	if m == nil || (m[2] != promptFileSuffix && m[2] != ext) {
		return false
	}

	stamp, err := time.Parse(clock.StampLayout, m[1])

	return err == nil && stamp.Before(cutoff)
}
