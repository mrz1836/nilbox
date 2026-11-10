// Package shellenv reads the environment the user's login shell sets up, as editors do,
// so the tools the app runs see the PATH, the ssh agent, and the settings the user's
// terminal has. An app started from the Finder or the Dock gets only launchd's few
// variables and its /usr/bin:/bin:/usr/sbin:/sbin.
//
// A read runs the shell named by $SHELL (or /bin/zsh when that is unset or not listed in
// /etc/shells) as an interactive login shell, in its own process group, with stdin from
// /dev/null and a time limit that kills the whole group: [$SHELL -i -l -c "printf
// marker; /usr/bin/env -0; printf marker"]. The marker is random each time, so whatever
// the shell's startup files print before or after it is ignored, and the values are split
// on NUL, so a value holding a newline survives.
//
// It never logs, stores, or returns in an error a variable's value or its name: names can
// carry private project names. The log gets how long a read took, how many variables it
// found, and why one failed.
package shellenv

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
)

const (
	// DefaultTimeout bounds one read.
	DefaultTimeout = 5 * time.Second

	// fallbackShell runs when $SHELL is unset or not a listed login shell.
	fallbackShell = "/bin/zsh"

	// shellsFile lists the system's valid login shells.
	shellsFile = "/etc/shells"

	// maxOutput caps what a read keeps of the shell's output.
	maxOutput = 4 << 20

	// readGrace bounds how long a read waits, once the shell has exited, for a program
	// its startup files started to let go of the shell's output.
	readGrace = 500 * time.Millisecond
)

var (
	// ErrNoMarkers is returned when the shell's output doesn't hold the environment
	// between two markers: the shell failed early, or printed nothing.
	ErrNoMarkers = errors.New("the shell's output had no environment between its markers")

	// ErrNoVariables is returned when the shell's environment was empty.
	ErrNoVariables = errors.New("the shell reported no variables")
)

// Starter runs a shell's argv with env, stdin from /dev/null, and returns what it wrote,
// stopping it when ctx ends. RunShell is the real one; tests hand in their own, so no test
// runs the developer's own shell.
type Starter func(ctx context.Context, argv, env []string) ([]byte, error)

// Config holds a Reader's settings. Every field is optional.
type Config struct {
	// Start runs the shell. It defaults to RunShell.
	Start Starter

	// Environ returns the app's own environment: what the shell starts from, and what
	// runs get when the shell's can't be read. It defaults to os.Environ.
	Environ func() []string

	// Home is the user's home folder, for the well-known tool folders a fallback PATH
	// puts first (execpath.FallbackPath).
	Home string

	// Shells is the file of valid login shells. It defaults to /etc/shells.
	Shells string

	// Timeout bounds one read. It defaults to DefaultTimeout.
	Timeout time.Duration

	// Logger gets one record per read. Without it nothing is logged.
	Logger *slog.Logger
}

// Reader reads the login shell's environment and keeps the latest one it read. It is safe
// for concurrent use.
type Reader struct {
	start   Starter
	environ func() []string
	home    string
	shells  string
	timeout time.Duration
	logger  *slog.Logger

	ready     chan struct{}
	readyOnce sync.Once

	mu  sync.Mutex
	env []string
	ok  bool
}

// New returns a Reader over cfg that hasn't read yet.
func New(cfg Config) *Reader {
	r := &Reader{
		start: cfg.Start, environ: cfg.Environ, home: cfg.Home, shells: cfg.Shells, timeout: cfg.Timeout,
		logger: logging.Or(cfg.Logger), ready: make(chan struct{}),
	}

	if r.start == nil {
		r.start = RunShell
	}

	if r.environ == nil {
		r.environ = os.Environ
	}

	if r.shells == "" {
		r.shells = shellsFile
	}

	if r.timeout <= 0 {
		r.timeout = DefaultTimeout
	}

	return r
}

// Read reads the login shell's environment, keeps it, and returns nil; or it returns why
// it couldn't, keeping what an earlier read found. Ready closes once the first read ends,
// either way.
func (r *Reader) Read(ctx context.Context) error {
	defer r.readyOnce.Do(func() { close(r.ready) })

	start := time.Now()
	env, err := r.read(ctx)
	took := time.Since(start).Round(time.Millisecond)

	if err != nil {
		r.logger.WarnContext(ctx, "couldn't read the login shell's environment; runs use the app's own, with the usual tool folders first on PATH",
			"duration", took, "reason", err)

		return err
	}

	r.mu.Lock()
	r.env, r.ok = env, true
	r.mu.Unlock()

	r.logger.InfoContext(ctx, "read the login shell's environment", "duration", took, "variables", len(env))

	return nil
}

// Ready is closed once the first Read has ended, whether it read the environment or not.
// Nothing reads before the window's first paint, and a startup measurement never reads,
// so a caller waits on it together with its own context, never on it alone.
func (r *Reader) Ready() <-chan struct{} {
	return r.ready
}

// Env returns the environment the latest successful read found, and true; until a read
// has succeeded, it returns the app's own environment with execpath.FallbackPath as PATH,
// and false. The slice is the caller's to change.
func (r *Reader) Env() ([]string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ok {
		return slices.Clone(r.env), true
	}

	return r.fallback(), false
}

// Path returns PATH from Env: the shell's, or the fallback.
func (r *Reader) Path() string {
	env, _ := r.Env()

	return Lookup(env, "PATH")
}

// Lookup returns key's value in env, the last one when it repeats, or "".
func Lookup(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if k, v, ok := strings.Cut(env[i], "="); ok && k == key {
			return v
		}
	}

	return ""
}

// read runs the shell once and parses what it printed.
func (r *Reader) read(ctx context.Context) ([]string, error) {
	marker, err := newMarker()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	argv := []string{r.shell(), "-i", "-l", "-c", "printf '%s' " + marker + "; /usr/bin/env -0; printf '%s' " + marker}

	out, err := r.start(ctx, argv, r.environ())
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("run %s: %w", filepath.Base(argv[0]), ctxErr)
		}

		return nil, fmt.Errorf("run %s: %w", filepath.Base(argv[0]), err)
	}

	return parse(out, marker)
}

// shell returns $SHELL from the app's environment when it is a listed login shell, else
// /bin/zsh.
func (r *Reader) shell() string {
	sh := Lookup(r.environ(), "SHELL")
	if sh == "" || !filepath.IsAbs(sh) {
		return fallbackShell
	}

	listed, err := os.ReadFile(r.shells)
	if err != nil {
		return fallbackShell
	}

	for line := range strings.Lines(string(listed)) {
		if strings.TrimSpace(line) == sh {
			return sh
		}
	}

	return fallbackShell
}

// fallback is the app's own environment with the fallback PATH.
func (r *Reader) fallback() []string {
	own := r.environ()
	env := make([]string, 0, len(own)+1)

	for _, kv := range own {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}

	return append(env, "PATH="+execpath.FallbackPath(r.home, Lookup(own, "PATH")))
}

// newMarker returns a marker no startup file prints by chance.
func newMarker() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("make a marker: %w", err)
	}

	return "ENV" + hex.EncodeToString(b), nil
}

// parse returns the NUL-separated variables between the first and the last marker.
func parse(out []byte, marker string) ([]string, error) {
	first := bytes.Index(out, []byte(marker))
	last := bytes.LastIndex(out, []byte(marker))

	if first < 0 || last <= first {
		return nil, ErrNoMarkers
	}

	var env []string

	for kv := range bytes.SplitSeq(out[first+len(marker):last], []byte{0}) {
		if key, _, ok := bytes.Cut(kv, []byte("=")); ok && len(key) > 0 && !bytes.ContainsAny(key, "\n\x00") {
			env = append(env, string(kv))
		}
	}

	if len(env) == 0 {
		return nil, ErrNoVariables
	}

	return env, nil
}

// RunShell runs argv in its own process group (execpath.Start) with env, and returns
// what it wrote to stdout and stderr: the first 4 MiB, with the rest read and dropped. Once the shell exits it waits a moment
// more for a program the shell started to let go of the output, then stops reading.
func RunShell(ctx context.Context, argv, env []string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, ErrNoMarkers
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("make a pipe: %w", err)
	}

	defer func() { _ = pr.Close() }()

	proc, err := execpath.Start(ctx, execpath.Cmd{Path: argv[0], Args: argv[1:], Env: env, Output: pw})
	_ = pw.Close() // the shell holds its own copy

	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	copied := make(chan struct{})

	go func() {
		// Keep the first maxOutput bytes and drain the rest, so a shell that prints more
		// never blocks on a full pipe.
		_, _ = io.Copy(&capped{buf: &buf, max: maxOutput}, pr)

		close(copied)
	}()

	_, waitErr := proc.Wait()

	select {
	case <-copied:
	case <-time.After(readGrace):
		_ = pr.SetReadDeadline(time.Now())

		<-copied
	}

	if waitErr != nil {
		return nil, waitErr
	}

	return buf.Bytes(), nil
}

// capped keeps what is written to it in buf up to max bytes and drops the rest, always
// reporting the whole write as taken.
type capped struct {
	buf *bytes.Buffer
	max int
}

// Write keeps what fits and drops the rest.
func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		c.buf.Write(p[:min(room, len(p))])
	}

	return len(p), nil
}
