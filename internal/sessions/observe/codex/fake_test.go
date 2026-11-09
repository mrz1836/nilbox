package codex_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sessions/observe/codex"
)

// fixture reads a file under testdata.
func fixture(t *testing.T, name string) json.RawMessage {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // the test's own testdata
	require.NoError(t, err)

	return data
}

// request is one message the fake daemon received.
type request struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// answerFunc answers a call: its result, or the error the daemon answers with.
type answerFunc func(method string, params json.RawMessage) (json.RawMessage, *codex.RPCError)

// fakeDaemon is an app-server daemon in the test's own process: it speaks WebSocket
// over one end of a pipe, the end a proxy would carry, records every message it gets,
// and answers each call with answer.
type fakeDaemon struct {
	mu       sync.Mutex
	received []request
	answer   answerFunc
	conn     *websocket.Conn
	ready    chan struct{}
}

// newFakeDaemon serves one connection on server, answering with answer.
func newFakeDaemon(t *testing.T, server net.Conn, answer answerFunc) *fakeDaemon {
	t.Helper()

	d := &fakeDaemon{answer: answer, ready: make(chan struct{})}
	srv := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(d.serve)}

	ln := newOneConnListener(server)

	go func() { _ = srv.Serve(ln) }()

	t.Cleanup(func() { _ = srv.Close() })

	return d
}

// serve accepts the WebSocket and answers every call until the connection ends.
func (d *fakeDaemon) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}

	d.mu.Lock()
	d.conn = conn
	d.mu.Unlock()
	close(d.ready)

	ctx := r.Context()

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		if out, ok := d.reply(data); ok {
			_ = conn.Write(ctx, websocket.MessageText, out)
		}
	}
}

// rpcReply is a response the fake daemon sends.
type rpcReply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcFailure     `json:"error,omitempty"`
}

// rpcFailure is an error a response carries.
type rpcFailure struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

// reply records one message, and returns the response to it when it is a call.
func (d *fakeDaemon) reply(data []byte) ([]byte, bool) {
	var req request
	if json.Unmarshal(data, &req) != nil {
		return nil, false
	}

	d.mu.Lock()
	d.received = append(d.received, req)
	answer := d.answer
	d.mu.Unlock()

	if req.ID == nil {
		return nil, false
	}

	out := rpcReply{ID: *req.ID}

	result, rpcErr := answer(req.Method, req.Params)
	if rpcErr != nil {
		out.Error = &rpcFailure{Code: rpcErr.Code, Message: rpcErr.Message}
	} else {
		out.Result = result
	}

	encoded, err := json.Marshal(out)

	return encoded, err == nil
}

// push sends the client a message of the daemon's own, such as a notification.
func (d *fakeDaemon) push(t *testing.T, msg string) {
	t.Helper()

	<-d.ready

	d.mu.Lock()
	conn := d.conn
	d.mu.Unlock()

	require.NoError(t, conn.Write(t.Context(), websocket.MessageText, []byte(msg)))
}

// methods returns the method of every message received, in order.
func (d *fakeDaemon) methods() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := make([]string, 0, len(d.received))
	for _, r := range d.received {
		out = append(out, r.Method)
	}

	return out
}

// requests returns every message received with method.
func (d *fakeDaemon) requests(method string) []request {
	d.mu.Lock()
	defer d.mu.Unlock()

	var out []request

	for _, r := range d.received {
		if r.Method == method {
			out = append(out, r)
		}
	}

	return out
}

// count is how many messages with method were received.
func (d *fakeDaemon) count(method string) int {
	return len(d.requests(method))
}

// fakeDialer connects each Dial to a new fakeDaemon over a pipe, or fails with err.
type fakeDialer struct {
	t      *testing.T
	answer answerFunc

	mu      sync.Mutex
	daemons []*fakeDaemon
	homes   []string
	err     error
}

func (f *fakeDialer) Dial(ctx context.Context, codexHome string) (*codex.Client, error) {
	f.mu.Lock()
	f.homes = append(f.homes, codexHome)
	err := f.err
	f.mu.Unlock()

	if err != nil {
		return nil, err
	}

	client, server := net.Pipe()
	d := newFakeDaemon(f.t, server, f.answer)

	f.mu.Lock()
	f.daemons = append(f.daemons, d)
	f.mu.Unlock()

	return codex.Connect(ctx, client, codex.ClientInfo{Name: "test", Version: "0"})
}

// daemon returns the daemon of the last Dial.
func (f *fakeDialer) daemon() *fakeDaemon {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.daemons[len(f.daemons)-1]
}

// dials returns the Codex home of every Dial.
func (f *fakeDialer) dials() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.homes)
}

// oneConnListener is a net.Listener that accepts one connection, then waits until closed.
type oneConnListener struct {
	conn   net.Conn
	once   sync.Once
	closed chan struct{}
	given  chan struct{}
}

func newOneConnListener(conn net.Conn) *oneConnListener {
	return &oneConnListener{conn: conn, closed: make(chan struct{}), given: make(chan struct{}, 1)}
}

func (l *oneConnListener) Accept() (net.Conn, error) {
	select {
	case l.given <- struct{}{}:
		return l.conn, nil
	default:
	}

	<-l.closed

	return nil, net.ErrClosed
}

func (l *oneConnListener) Close() error {
	l.once.Do(func() { close(l.closed) })

	return nil
}

func (l *oneConnListener) Addr() net.Addr { return pipeAddr{} }

// pipeAddr is a pipe's address.
type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// errDialRefused is what a failing fakeDialer fails with.
var errDialRefused = errors.New("no app server is running")
