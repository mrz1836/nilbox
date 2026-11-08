package logging

import (
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// FileName is the log file's name in the logs folder.
	FileName = brand.Slug + ".log"

	// MaxFileBytes is the size a log file is rotated at.
	MaxFileBytes = 5 << 20

	// KeptFiles is how many log files are kept: FileName, FileName.1, and FileName.2.
	KeptFiles = 3

	// DebugFlag on the command line keeps Debug records.
	DebugFlag = "--debug"
)

// DebugEnv names the environment variable that keeps Debug records when it is set to a
// true value, such as 1.
func DebugEnv() string {
	return strings.ToUpper(brand.Slug) + "_DEBUG"
}

// Level returns the level a run logs at: slog.LevelDebug when args hold DebugFlag or
// getenv reports DebugEnv as true, else slog.LevelInfo.
func Level(args []string, getenv func(key string) string) slog.Level {
	if slices.Contains(args, DebugFlag) {
		return slog.LevelDebug
	}

	if on, err := strconv.ParseBool(getenv(DebugEnv())); err == nil && on {
		return slog.LevelDebug
	}

	return slog.LevelInfo
}

// New returns the app's logger, which writes records at level and above as text to
// stderr and as JSON to FileName in paths.Logs, creating the folder if needed, and the
// Closer that closes the file. Without a logs folder the logger writes to stderr alone;
// when the file cannot be opened it does too, warning once why. The Closer then does
// nothing.
func New(paths appdata.Paths, level slog.Leveler) (*slog.Logger, io.Closer) {
	return newLogger(os.Stderr, paths.Logs, level, MaxFileBytes)
}

// newLogger is New with stderr and the rotation size chosen.
func newLogger(stderr io.Writer, dir string, level slog.Leveler, maxBytes int64) (*slog.Logger, io.Closer) {
	opts := &slog.HandlerOptions{Level: level}
	console := slog.NewTextHandler(stderr, opts)

	if dir == "" {
		return slog.New(console), nopCloser{}
	}

	file, err := openFile(dir, maxBytes)
	if err != nil {
		logger := slog.New(console)
		logger.Warn("logging to stderr only: the log file could not be opened", "error", err)

		return logger, nopCloser{}
	}

	return slog.New(slog.NewMultiHandler(console, slog.NewJSONHandler(file, opts))), file
}

// Or returns logger, or a logger that discards every record when logger is nil, so a
// service built without one logs nowhere rather than through slog's default logger.
func Or(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(slog.DiscardHandler)
	}

	return logger
}

// nopCloser is the Closer of a logger with no file to close.
type nopCloser struct{}

// Close does nothing.
func (nopCloser) Close() error { return nil }
