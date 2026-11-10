package launcher

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
)

var (
	// ErrEmptyPrompt is returned when a launch has no prompt text.
	ErrEmptyPrompt = errors.New("prompt is empty")

	// ErrResumePrompt is returned when a launch that resumes a session is given a prompt,
	// which the session would never see.
	ErrResumePrompt = errors.New("a resumed session takes no prompt")
)

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
// (nil discards it) rather than failing the launch. terminal.ScriptWriter does the
// writing and the pruning.
func WriteLaunchFiles(
	paths appdata.Paths, plan Plan, prompt string, now time.Time, log *slog.Logger,
) (LaunchFiles, error) {
	return writeLaunchFiles(paths, plan, prompt, now, terminal.ScriptWriter{Log: log})
}

// writeLaunchFiles is WriteLaunchFiles with the writer, which holds the script renderer,
// the randomness that names the files, and the logger pruning failures go to, injected.
func writeLaunchFiles(
	paths appdata.Paths, plan Plan, prompt string, now time.Time, w terminal.ScriptWriter,
) (LaunchFiles, error) {
	if err := validateLaunchFiles(plan, prompt); err != nil {
		return LaunchFiles{}, err
	}

	prefix := string(plan.Kind) + "-" + strconv.Itoa(plan.Number)

	files, err := w.WriteWithPrompt(paths.Launch, prefix, plan.script(), prompt, now)
	if err != nil {
		return LaunchFiles{}, err
	}

	return LaunchFiles{Script: files.Script, Prompt: files.Prompt}, nil
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
