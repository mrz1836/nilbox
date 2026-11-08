// Package timing measures how long the app takes to show its first frame.
//
// The measurement runs from the moment the operating system started the process to the
// moment the frontend reports its first paint. On systems where the process start time
// cannot be read, it falls back to the moment the Go runtime initialized this package,
// which is slightly later; every measurement records which source it used.
package timing

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/logging"
)

// Sources of the process start time.
const (
	// SourceOS is the start time the operating system recorded for the process.
	SourceOS = "os"

	// SourceRuntime is the time the Go runtime initialized this package.
	SourceRuntime = "runtime"
)

// LogFileName is the file under the logs directory that collects timing lines.
const LogFileName = "timing.log"

// FirstPaintKey is the key every first-paint record carries, followed by the
// milliseconds from process start.
const FirstPaintKey = "startup.first_paint_ms"

// runtimeStart is the fallback start time: the earliest moment package code runs.
//
//nolint:gochecknoglobals // process start fallback, captured once at package init
var runtimeStart = time.Now()

// processStart returns when the current process started and which source that time
// came from: the operating system's record when available, else the runtime fallback.
func processStart() (time.Time, string) {
	if start, err := osProcessStart(os.Getpid()); err == nil && !start.IsZero() {
		return start, SourceOS
	}

	return runtimeStart, SourceRuntime
}

// Measurement is one first-paint reading.
type Measurement struct {
	// FirstPaintMS is the milliseconds from process start to the first paint.
	FirstPaintMS int64

	// Source is where the start time came from: SourceOS or SourceRuntime.
	Source string
}

// String formats the measurement as a timing line, e.g.
// "startup.first_paint_ms=412 source=os".
func (m Measurement) String() string {
	return fmt.Sprintf("%s=%d source=%s", FirstPaintKey, m.FirstPaintMS, m.Source)
}

// Config holds a Recorder's settings. Every field is optional.
type Config struct {
	// Logger receives the timing record. Without it the record is not logged.
	Logger *slog.Logger

	// LogsDir is the directory whose timing.log gets the timing line. Empty skips the
	// file.
	LogsDir string

	// Start is the process start time; zero uses processStart.
	Start time.Time

	// Source names where Start came from; it is ignored when Start is zero.
	Source string

	// Now tells the time. It defaults to time.Now.
	Now func() time.Time
}

// Recorder records the first paint exactly once.
type Recorder struct {
	logger  *slog.Logger
	logsDir string
	start   time.Time
	source  string
	now     func() time.Time

	once   sync.Once
	result Measurement
	err    error
}

// NewRecorder returns a Recorder for the current process.
func NewRecorder(cfg Config) *Recorder {
	r := &Recorder{
		logger: logging.Or(cfg.Logger), logsDir: cfg.LogsDir, start: cfg.Start, source: cfg.Source, now: cfg.Now,
	}

	if r.start.IsZero() {
		r.start, r.source = processStart()
	}

	if r.now == nil {
		r.now = time.Now
	}

	return r
}

// FirstPaint records the time from process start to now as the first paint. The first
// call logs it and appends it to the timing log, and returns true; later calls return
// the first measurement and false without recording again. An error means the timing
// log could not be written; the measurement is still valid and was still logged.
func (r *Recorder) FirstPaint() (Measurement, bool, error) {
	first := false

	r.once.Do(func() {
		first = true
		r.result, r.err = r.record()
	})

	if !first {
		return r.result, false, nil
	}

	return r.result, true, r.err
}

// record measures, logs, and appends the timing line.
func (r *Recorder) record() (Measurement, error) {
	ms := max(r.now().Sub(r.start).Milliseconds(), 0)

	m := Measurement{FirstPaintMS: ms, Source: r.source}

	r.logger.Info("startup timing", slog.Int64(FirstPaintKey, m.FirstPaintMS), slog.String("source", m.Source))

	if r.logsDir == "" {
		return m, nil
	}

	return m, appendLine(filepath.Join(r.logsDir, LogFileName), clock.Format(r.now())+" "+m.String())
}

// appendLine appends one line to the file at path, creating it user-only if needed.
func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, appdata.PrivateFilePerm) //nolint:gosec // path is the app's own log file
	if err != nil {
		return fmt.Errorf("open the timing log: %w", err)
	}

	if _, err = f.WriteString(line + "\n"); err != nil {
		_ = f.Close()

		return fmt.Errorf("write the timing log: %w", err)
	}

	if err = f.Close(); err != nil {
		return fmt.Errorf("close the timing log: %w", err)
	}

	return nil
}
