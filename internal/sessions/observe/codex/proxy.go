package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/execpath"
)

// Dialer opens a connection to the app-server daemon of one Codex home.
type Dialer interface {
	// Dial connects to the daemon of codexHome, a profile's config directory, or "" for
	// Codex's default.
	Dial(ctx context.Context, codexHome string) (*Client, error)
}

// ProxyDialer connects through `codex app-server proxy`, a child process that carries
// the connection over its standard input and output to the daemon's socket. It never
// starts the daemon: a Codex session started in a terminal does that.
type ProxyDialer struct {
	// Finder finds the codex command.
	Finder execpath.Finder

	// Environ is this process's environment; it defaults to os.Environ.
	Environ func() []string

	// Info names the app to the daemon.
	Info ClientInfo
}

// Dial starts the proxy with Codex's config variable set to codexHome, or unset for the
// default, and connects through it. The proxy runs until the client is closed.
func (d ProxyDialer) Dial(ctx context.Context, codexHome string) (*Client, error) {
	a, err := agent.Lookup(agent.Codex)
	if err != nil {
		return nil, err
	}

	bin, err := d.Finder.Find(a.Command)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", a.Command, err)
	}

	cmd := exec.Command(bin, "app-server", "proxy") //nolint:gosec,noctx // argv only; the proxy outlives the call that starts it, and Close stops it
	cmd.Env = d.env(a.ConfigEnv, codexHome, filepath.Dir(bin))

	stream, err := startStream(cmd)
	if err != nil {
		return nil, err
	}

	return Connect(ctx, stream, d.Info)
}

// env is this process's environment with Codex's config variable set to codexHome, or
// unset, and the command's own folder first on the search path, since Codex installed
// with npm starts through a script that needs node beside it.
func (d ProxyDialer) env(configEnv, codexHome, binDir string) []string {
	environ := d.Environ
	if environ == nil {
		environ = os.Environ
	}

	env := slices.DeleteFunc(slices.Clone(environ()), func(kv string) bool {
		return strings.HasPrefix(kv, configEnv+"=") || strings.HasPrefix(kv, "PATH=")
	})

	env = append(env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if codexHome != "" {
		env = append(env, configEnv+"="+codexHome)
	}

	return env
}

// stopGrace is how long closing a proxy waits for it to stop on SIGTERM before it kills
// it.
const stopGrace = time.Second

// procStream is a child process's standard input and output as one stream. Closing it
// ends the process.
type procStream struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out io.ReadCloser

	// grace is how long Close waits for the process to stop on SIGTERM.
	grace time.Duration

	once sync.Once
	err  error
}

// startStream starts cmd with its standard input and output as a stream.
func startStream(cmd *exec.Cmd) (*procStream, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("start codex app-server proxy: %w", err)
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("start codex app-server proxy: %w", err)
	}

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server proxy: %w", err)
	}

	return &procStream{cmd: cmd, in: in, out: out, grace: stopGrace}, nil
}

func (s *procStream) Read(p []byte) (int, error)  { return s.out.Read(p) } //nolint:wrapcheck // a stream's own errors
func (s *procStream) Write(p []byte) (int, error) { return s.in.Write(p) } //nolint:wrapcheck // a stream's own errors

// Close closes the process's input and stops it, and waits for it.
func (s *procStream) Close() error {
	s.once.Do(func() {
		_ = s.in.Close()

		s.err = s.stop()
	})

	return s.err
}

// stop asks the process to stop with SIGTERM, kills it if it has not stopped within the
// grace, and waits for it. Codex installed with npm runs its proxy under a launcher that
// passes SIGTERM on to it, but SIGKILL alone would stop only the launcher and leave the
// proxy running.
func (s *procStream) stop() error {
	exited := make(chan error, 1)

	go func() { exited <- s.cmd.Wait() }()

	_ = s.cmd.Process.Signal(syscall.SIGTERM)

	timer := time.NewTimer(s.grace)
	defer timer.Stop()

	var err error

	select {
	case err = <-exited:
	case <-timer.C:
		_ = s.cmd.Process.Kill()
		err = <-exited
	}

	if _, ok := errors.AsType[*exec.ExitError](err); err != nil && !ok {
		return fmt.Errorf("stop codex app-server proxy: %w", err)
	}

	return nil
}
