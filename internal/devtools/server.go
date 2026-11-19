package devtools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"os"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/store"
)

const (
	// socketPerm keeps the socket to the user alone.
	socketPerm = 0o600

	// maxRequestBytes is the longest request a client may send: room for a long script.
	maxRequestBytes = 4 << 20

	// writeTimeout is how long one line of an answer may take to send before the
	// inspector gives up on a client that stopped reading.
	writeTimeout = 10 * time.Second
)

// ErrInUse is returned for a socket another running copy of the app answers on.
var ErrInUse = errors.New("another running copy of the app is inspected there")

// Database is the app's database as the inspector reads it: read-only.
type Database interface {
	// Query runs one statement that reads and returns at most maxRows of its rows.
	Query(ctx context.Context, query string, maxRows int) (store.QueryResult, error)

	// Tables lists the database's tables, with their row counts.
	Tables(ctx context.Context) ([]store.TableInfo, error)
}

// Config is what the inspector reads and runs.
type Config struct {
	Logs     *Feed[LogEntry]
	Events   *Feed[EventEntry]
	Page     *Page
	Database Database

	// Shot takes a PNG of the window as its web view draws it.
	Shot func(ctx context.Context) ([]byte, error)

	// About describes the running app.
	About func() About

	// Now is the time: time.Now unless a test sets it.
	Now func() time.Time
}

// Server answers the inspector's clients, one request per connection.
type Server struct {
	cfg      Config
	commands map[string]command
}

// NewServer returns a server over cfg.
func NewServer(cfg Config) *Server {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	s := &Server{cfg: cfg}
	s.commands = s.table()

	return s
}

// Serve answers the clients that connect to ln until ctx ends, then closes ln and
// returns once every answer has stopped.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	stop := context.AfterFunc(ctx, func() { _ = ln.Close() })
	defer stop()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return fmt.Errorf("accept a client: %w", err)
		}

		wg.Go(func() { s.serveConn(ctx, conn) })
	}
}

// serveConn reads the one request conn carries and answers it. The client closing its
// end stops a command that follows a feed.
func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	out := &writer{conn: conn}

	// The request may be maxRequestBytes long; after it, the client sends nothing more,
	// and reading on only notices it close.
	limited := &io.LimitedReader{R: conn, N: maxRequestBytes}
	reader := bufio.NewReader(limited)

	req, err := readRequest(reader)
	if err != nil {
		out.fail(err)

		return
	}

	limited.N = math.MaxInt64

	go func() {
		_, _ = io.Copy(io.Discard, reader)

		cancel()
	}()

	if err = s.answer(ctx, out, req); err != nil {
		out.fail(err)

		return
	}

	_ = out.send(LineEnd, nil)
}

// answer runs the command req names.
func (s *Server) answer(ctx context.Context, out *writer, req Request) error {
	cmd, ok := s.commands[req.Command]
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCommand, req.Command)
	}

	return cmd.run(ctx, out, args(req.Args))
}

// readRequest reads the request line a connection starts with; a request with no line
// break after it is read to its end.
func readRequest(reader *bufio.Reader) (Request, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil && (!errors.Is(err, io.EOF) || len(line) == 0) {
		return Request{}, fmt.Errorf("read the request: %w", err)
	}

	var req Request
	if err = json.Unmarshal(line, &req); err != nil {
		return Request{}, fmt.Errorf("read the request: %w", err)
	}

	return req, nil
}

// Listen listens on the Unix socket at path for the inspector's clients, readable and
// writable by the user alone. A socket a run that ended left behind is removed first;
// one that another running copy of the app answers on is left alone, ErrInUse.
func Listen(ctx context.Context, path string) (net.Listener, error) {
	var dialer net.Dialer
	if conn, err := dialer.DialContext(ctx, "unix", path); err == nil {
		_ = conn.Close()

		return nil, fmt.Errorf("%w: %s", ErrInUse, path)
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("remove the socket a run left behind: %w", err)
	}

	var config net.ListenConfig

	ln, err := config.Listen(ctx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}

	if err = os.Chmod(path, socketPerm); err != nil {
		return nil, errors.Join(fmt.Errorf("keep the socket to the user: %w", err), ln.Close())
	}

	return ln, nil
}

// writer sends an answer's lines over one connection, each within writeTimeout. It is
// safe for concurrent use; after one send fails, every later one fails the same way.
type writer struct {
	mu   sync.Mutex
	conn net.Conn
	err  error
}

// send sends one line of type typ holding v.
func (w *writer) send(typ string, v any) error {
	var value json.RawMessage

	if v != nil {
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("encode the answer: %w", err)
		}

		value = data
	}

	data, err := json.Marshal(Line{Type: typ, Value: value})
	if err != nil {
		return fmt.Errorf("encode the answer: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.err != nil {
		return w.err
	}

	_ = w.conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	if _, err = w.conn.Write(append(data, '\n')); err != nil {
		w.err = fmt.Errorf("send the answer: %w", err)
	}

	return w.err
}

// fail ends the answer with err.
func (w *writer) fail(err error) {
	_ = w.send(LineError, err.Error())
}
