package hooks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// readAction feeds a new action reader text, byte by byte, as the log may arrive split
// anywhere, and flushes it.
func readAction(text string) actionOutput {
	r := newActionReader()

	for i := range len(text) {
		n, err := r.Write([]byte{text[i]})
		if err != nil || n != 1 {
			panic("the reader refused a write")
		}
	}

	r.Flush()

	return r.snapshot()
}

func TestActionReaderKnowsEveryRecordedLine(t *testing.T) {
	t.Parallel()

	names := append(recordingNames(t, "install-"), recordingNames(t, "uninstall-")...)
	names = append(names, "global-hookspath-install")

	for _, name := range names {
		got := readAction(loadRecording(t, name).log())

		assert.Empty(t, got.unrecognized, name)
		assert.Zero(t, got.unrecognizedCount, name)
		assert.Empty(t, got.warnings, name)

		if name == "install-no-config" {
			assert.Equal(t, "failed to load configuration: failed to load configuration: failed to find environment "+
				"configuration (.github/env/ directory or .github/.env.base)", got.lastError())

			continue
		}

		assert.Empty(t, got.errors, name)
	}
}

func TestActionReaderKeepsWhatItDoesntKnow(t *testing.T) {
	t.Parallel()

	got := readAction(strings.Join([]string{
		"Error: hooks not installed: pre-commit (use --force to overwrite)",
		"✗ Failed to write the hook",
		"⚠ A warning of its own",
		"Warning: failed to restore backup hook: permission denied",
		"\x1b[32msomething new\x1b[0m",
		"",
		"last line without a newline",
	}, "\n"))

	assert.Equal(t, []string{"hooks not installed: pre-commit (use --force to overwrite)", "Failed to write the hook"}, got.errors)
	assert.Equal(t, "Failed to write the hook", got.lastError())
	assert.Equal(t, []string{"A warning of its own", "failed to restore backup hook: permission denied"}, got.warnings)
	assert.Equal(t, []string{"something new", "last line without a newline"}, got.unrecognized)
	assert.Equal(t, 2, got.unrecognizedCount)

	assert.Empty(t, readAction("").lastError())
}

func TestReadersKeepNotesUpToTheirLimits(t *testing.T) {
	t.Parallel()

	lines := make([]string, 0, 3*(maxNotes+10))
	for range maxNotes + 10 {
		lines = append(lines, "⚠ again", "Error: again", "?? unknown")
	}

	got := readAction(strings.Join(lines, "\n") + "\n")
	assert.Len(t, got.warnings, maxNotes)
	assert.Len(t, got.errors, maxNotes)
	assert.Len(t, got.unrecognized, 200)
	assert.Equal(t, maxNotes+10, got.unrecognizedCount)

	// A line past textutil.MaxLine keeps its start, and the next line reads as its own.
	got = readAction(strings.Repeat("x", textutil.MaxLine+10) + "\n" + "next\n")
	require.Len(t, got.unrecognized, 2)
	assert.Len(t, got.unrecognized[0], textutil.MaxLine)
	assert.Equal(t, "next", got.unrecognized[1])
}
