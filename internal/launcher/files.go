package launcher

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
)

const (
	// launchDirPerm keeps the launch directory private to the user.
	launchDirPerm = 0o700

	// promptFilePerm keeps prompt files private to the user.
	promptFilePerm = 0o600

	// scriptFilePerm makes launch scripts runnable by the user only.
	scriptFilePerm = 0o700

	// promptFileSuffix ends every prompt file name.
	promptFileSuffix = ".prompt.md"

	// stampLayout is the UTC timestamp in launch file names.
	stampLayout = "20060102T150405Z"

	// suffixBytes is how many random bytes end a launch file's base name, so two launches
	// of the same item in the same second never collide.
	suffixBytes = 4

	// launchFileMaxAge is how long launch files are kept before they are pruned.
	launchFileMaxAge = 7 * 24 * time.Hour
)

// ErrEmptyPrompt is returned when a launch has no prompt text.
var ErrEmptyPrompt = errors.New("prompt is empty")

// launchFilePattern matches the names of files WriteLaunchFiles creates with a script
// extension of ext, capturing the timestamp. Pruning only ever touches files that match.
func launchFilePattern(ext string) *regexp.Regexp {
	return regexp.MustCompile(`^(?:pr|issue)-[0-9]+-([0-9]{8}T[0-9]{6}Z)-[0-9a-f]{8}(?:` +
		regexp.QuoteMeta(promptFileSuffix) + `|` + regexp.QuoteMeta(ext) + `)$`)
}

// LaunchFiles are the files written for one launch.
type LaunchFiles struct {
	// Script is the script a terminal runs to start the session.
	Script string

	// Prompt is the file the script reads the prompt from.
	Prompt string
}

// WriteLaunchFiles writes a launch's prompt file and script into paths.Launch, which is
// created (readable only by the user) if needed. The files are named
// <kind>-<number>-<UTC stamp>-<random>.prompt.md (mode 0600) and the matching script
// (mode 0700, with the renderer's extension). The script reads the prompt from the
// prompt file, whatever plan.PromptFile said.
//
// Afterwards it prunes launch files older than seven days, judged by the timestamp in
// their names. Pruning never touches other files, and a pruning failure is logged
// rather than failing the launch.
func WriteLaunchFiles(paths appdata.Paths, plan Plan, prompt string, now time.Time) (LaunchFiles, error) {
	return writeLaunchFiles(paths, plan, prompt, now, DefaultRenderer(), rand.Reader)
}

// writeLaunchFiles is WriteLaunchFiles with the renderer and randomness injected.
func writeLaunchFiles(
	paths appdata.Paths, plan Plan, prompt string, now time.Time, renderer ScriptRenderer, random io.Reader,
) (LaunchFiles, error) {
	if err := validateLaunchFiles(plan, prompt); err != nil {
		return LaunchFiles{}, err
	}

	if err := ensurePrivateDir(paths.Launch); err != nil {
		return LaunchFiles{}, err
	}

	suffix := make([]byte, suffixBytes)
	if _, err := io.ReadFull(random, suffix); err != nil {
		return LaunchFiles{}, fmt.Errorf("name launch files: %w", err)
	}

	base := filepath.Join(paths.Launch, plan.Kind+"-"+strconv.Itoa(plan.Number)+"-"+
		now.UTC().Format(stampLayout)+"-"+hex.EncodeToString(suffix))
	files := LaunchFiles{Prompt: base + promptFileSuffix, Script: base + renderer.Extension()}

	if err := writeNewFile(files.Prompt, []byte(prompt), promptFilePerm); err != nil {
		return LaunchFiles{}, err
	}

	plan.PromptFile = files.Prompt

	if err := writeNewFile(files.Script, renderer.Render(plan), scriptFilePerm); err != nil {
		_ = os.Remove(files.Prompt)

		return LaunchFiles{}, err
	}

	pruneLaunchFiles(paths.Launch, now, launchFilePattern(renderer.Extension()))

	return files, nil
}

// validateLaunchFiles checks what the file names and contents are built from.
func validateLaunchFiles(plan Plan, prompt string) error {
	if plan.Kind != KindPR && plan.Kind != KindIssue {
		return fmt.Errorf("%w: %q", ErrInvalidKind, plan.Kind)
	}

	if plan.Number <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidNumber, plan.Number)
	}

	if prompt == "" {
		return ErrEmptyPrompt
	}

	return nil
}

// ensurePrivateDir creates dir if needed and makes it readable only by the user.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, launchDirPerm); err != nil {
		return fmt.Errorf("create launch directory: %w", err)
	}

	if err := os.Chmod(dir, launchDirPerm); err != nil {
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

// pruneLaunchFiles removes the regular files in dir that match pattern and whose name
// stamp is more than launchFileMaxAge before now. The files just written carry now's
// stamp, so they are always kept.
func pruneLaunchFiles(dir string, now time.Time, pattern *regexp.Regexp) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("could not list old launch files", "error", err)

		return
	}

	cutoff := now.Add(-launchFileMaxAge)

	for _, entry := range entries {
		if !entry.Type().IsRegular() || !stampBefore(pattern, entry.Name(), cutoff) {
			continue
		}

		if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			slog.Warn("could not remove an old launch file", "file", entry.Name(), "error", err)
		}
	}
}

// stampBefore reports whether name is a launch file whose stamp is before cutoff.
func stampBefore(pattern *regexp.Regexp, name string, cutoff time.Time) bool {
	m := pattern.FindStringSubmatch(name)
	if m == nil {
		return false
	}

	stamp, err := time.Parse(stampLayout, m[1])

	return err == nil && stamp.Before(cutoff)
}
