package measure

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/timing"
)

// StartupFlag makes the desktop app quit right after its first painted frame and print
// the startup timing line on stdout.
const StartupFlag = "--measure-startup"

// startupTimeout bounds one measured launch; the app gives up on its own after a minute.
const startupTimeout = 90 * time.Second

// maxStderrDetail caps how much of a failed launch's error output an error repeats.
const maxStderrDetail = 300

var (
	// ErrNoTimingLine is returned when the app exited without printing its timing line.
	ErrNoTimingLine = errors.New("the app exited without printing its startup timing line")

	// ErrTooFewRuns is returned when a series is too short to have a median after its
	// first launch.
	ErrTooFewRuns = errors.New("measure at least two launches")
)

// Startup is one measured launch.
type Startup struct {
	// FirstPaintMS is the app's own reading: milliseconds from the moment the operating
	// system started the process to the first frame on screen.
	FirstPaintMS int64

	// Source is where the app read its process start time: timing.SourceOS or
	// timing.SourceRuntime.
	Source string

	// Wall is the time from spawning the app to reading its timing line, measured from
	// outside the process.
	Wall time.Duration
}

// ParseFirstPaint reads a startup timing line such as
// "startup.first_paint_ms=412 source=os". Wall is left zero.
func ParseFirstPaint(line string) (Startup, bool) {
	var s Startup

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return Startup{}, false
	}

	value, ok := strings.CutPrefix(fields[0], timing.FirstPaintKey+"=")
	if !ok {
		return Startup{}, false
	}

	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil || ms < 0 {
		return Startup{}, false
	}

	s.FirstPaintMS = ms

	for _, f := range fields[1:] {
		if src, found := strings.CutPrefix(f, "source="); found {
			s.Source = src
		}
	}

	return s, true
}

// LaunchApp starts the desktop binary exe with StartupFlag, waits for it to print its
// timing line and quit, and returns the reading.
func LaunchApp(ctx context.Context, exe string) (Startup, error) {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()

	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, exe, StartupFlag)
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Startup{}, fmt.Errorf("connect to the app's output: %w", err)
	}

	start := time.Now()

	if err = cmd.Start(); err != nil {
		return Startup{}, fmt.Errorf("start the app: %w", err)
	}

	reading, found := scanFirstPaint(stdout, start)

	if err = cmd.Wait(); err != nil {
		return Startup{}, fmt.Errorf("run the app: %w%s", err, detail(stderr.String()))
	}

	if !found {
		return Startup{}, fmt.Errorf("%w%s", ErrNoTimingLine, detail(stderr.String()))
	}

	return reading, nil
}

// scanFirstPaint reads the app's output to the end and returns its first timing line,
// stamped with the wall time since start at which it arrived.
func scanFirstPaint(out io.Reader, start time.Time) (Startup, bool) {
	var (
		reading Startup
		found   bool
	)

	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		if found {
			continue
		}

		if s, ok := ParseFirstPaint(scanner.Text()); ok {
			s.Wall = time.Since(start)
			reading, found = s, true
		}
	}

	return reading, found
}

// detail returns the last non-blank line of a program's error output, ready to append to
// an error message, or "" when there is none.
func detail(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")

	last := strings.TrimSpace(lines[len(lines)-1])
	if last == "" {
		return ""
	}

	if len(last) > maxStderrDetail {
		last = last[:maxStderrDetail]
	}

	return " (" + last + ")"
}

// StartupReport is a series of launches, in order.
type StartupReport struct {
	Runs []Startup
}

// TimeStartups launches the app runs times, one after another, with launch (LaunchApp in
// practice), and stops at the first failure.
func TimeStartups(ctx context.Context, runs int, launch func(context.Context) (Startup, error)) (StartupReport, error) {
	if runs < 2 {
		return StartupReport{}, ErrTooFewRuns
	}

	report := StartupReport{Runs: make([]Startup, 0, runs)}

	for i := range runs {
		s, err := launch(ctx)
		if err != nil {
			return report, fmt.Errorf("launch %d of %d: %w", i+1, runs, err)
		}

		report.Runs = append(report.Runs, s)
	}

	return report, nil
}

// First returns the series' first launch. Right after a build it includes the operating
// system's one-time check of the new binary, so it is reported on its own.
func (r StartupReport) First() Startup {
	if len(r.Runs) == 0 {
		return Startup{}
	}

	return r.Runs[0]
}

// MedianMS returns the median first paint of the launches after the first: the app's
// cold start, each launch a new process.
func (r StartupReport) MedianMS() int64 {
	return Median(rest(r, func(s Startup) int64 { return s.FirstPaintMS }))
}

// MedianWall returns the median wall time, spawn to timing line, of the launches after
// the first.
func (r StartupReport) MedianWall() time.Duration {
	return Median(rest(r, func(s Startup) time.Duration { return s.Wall }))
}

// Lines renders the report as key=value lines: one per launch, then the summary.
func (r StartupReport) Lines() []string {
	lines := make([]string, 0, len(r.Runs)+3)

	for i, s := range r.Runs {
		lines = append(lines, fmt.Sprintf("startup.run=%d %s=%d wall_ms=%s source=%s",
			i+1, timing.FirstPaintKey, s.FirstPaintMS, milliseconds(s.Wall), s.Source))
	}

	return append(lines,
		fmt.Sprintf("startup.first_launch_ms=%d", r.First().FirstPaintMS),
		fmt.Sprintf("startup.median_ms=%d (launches 2-%d)", r.MedianMS(), len(r.Runs)),
		"startup.median_wall_ms="+milliseconds(r.MedianWall()),
	)
}

// rest maps every launch after the first with value.
func rest[T any](r StartupReport, value func(Startup) T) []T {
	if len(r.Runs) < 2 {
		return nil
	}

	out := make([]T, 0, len(r.Runs)-1)
	for _, s := range r.Runs[1:] {
		out = append(out, value(s))
	}

	return out
}
