package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"
)

// readLimit caps one message from the daemon; it says it sends whole messages up to
// 16 MiB.
const readLimit = 32 << 20

var (
	// ErrNotAllowed is returned for a method the client may not send: it only reads.
	ErrNotAllowed = errors.New("the method is not one the app may send")

	// ErrClosed is returned once the connection to the daemon is gone.
	ErrClosed = errors.New("the connection to Codex's app server is closed")
)

// Allowed returns the methods the client may call: initialize, which opens a connection,
// and the reads that list threads, read one, list its turns, and list those loaded. It
// never starts, resumes, steers, or answers a thread.
func Allowed() []string {
	return []string{"initialize", "thread/list", "thread/read", "thread/turns/list", "thread/loaded/list"}
}

// allowedNotifications are the notifications the client may send: initialized, which
// ends the handshake.
func allowedNotifications() []string {
	return []string{"initialized"}
}

// optOut are the notifications the client asks the daemon not to send it: the streams of
// a turn's text, which it never reads.
func optOut() []string {
	return []string{
		"item/agentMessage/delta", "item/reasoning/textDelta", "item/reasoning/summaryTextDelta",
		"item/commandExecution/outputDelta", "item/fileChange/outputDelta", "item/plan/delta",
		"thread/realtime/outputAudio/delta", "thread/realtime/transcript/delta", "thread/tokenUsage/updated",
	}
}

// changeMethods are the notifications that say a thread changed, which wake the observer.
func changeMethods() []string {
	return []string{"thread/status/changed", "thread/closed", "turn/started", "turn/completed", "thread/started"}
}

// ClientInfo names the app to the daemon.
type ClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// Client is a read-only JSON-RPC connection to the daemon. It is safe for concurrent use.
type Client struct {
	ws *wsConn

	nextID atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan reply
	err     error

	changed chan struct{}
	done    chan struct{}
	once    sync.Once
}

// reply is a response to one call.
type reply struct {
	result json.RawMessage
	err    error
}

// message is any message from the daemon: a response, a notification, or a request.
type message struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Result json.RawMessage  `json:"result"`
	Error  *struct {
		Code    int64  `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Connect opens a WebSocket over stream, a byte stream to the daemon's control socket
// such as a proxy's standard input and output, and initializes it as info. Closing the
// client closes stream.
func Connect(ctx context.Context, stream io.ReadWriteCloser, info ClientInfo) (*Client, error) {
	ws, err := dialWS(ctx, stream, readLimit)
	if err != nil {
		_ = stream.Close()

		return nil, fmt.Errorf("open the connection to Codex's app server: %w", err)
	}

	c := &Client{ws: ws, pending: map[int64]chan reply{}, changed: make(chan struct{}, 1), done: make(chan struct{})}

	go c.read()

	if err = c.initialize(ctx, info); err != nil {
		_ = c.Close()

		return nil, err
	}

	return c, nil
}

// Call sends method with params and decodes its result into result, unless it is nil.
// It refuses any method not in Allowed.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if !slices.Contains(Allowed(), method) {
		return fmt.Errorf("%w: %s", ErrNotAllowed, method)
	}

	id := c.nextID.Add(1)
	wait := make(chan reply, 1)

	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()

		return err
	}

	c.pending[id] = wait
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	if err := c.write(ctx, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.failure()
	case r := <-wait:
		if r.err != nil || result == nil {
			return r.err
		}

		if err := json.Unmarshal(r.result, result); err != nil {
			return fmt.Errorf("read Codex's answer to %s: %w", method, err)
		}

		return nil
	}
}

// Changed is signaled when the daemon says a thread changed.
func (c *Client) Changed() <-chan struct{} {
	return c.changed
}

// Done is closed once the connection is gone.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// Close closes the connection and the stream under it, and waits for its reads to end. It
// never waits on a write: closing the stream first ends any write still going out.
func (c *Client) Close() error {
	var err error

	c.once.Do(func() {
		c.fail(ErrClosed)
		err = c.ws.close()
	})

	<-c.done

	if errors.Is(err, io.ErrClosedPipe) {
		return nil
	}

	return err
}

// initialize opens the connection as info, asking for none of the streamed text.
func (c *Client) initialize(ctx context.Context, info ClientInfo) error {
	params := map[string]any{
		"clientInfo":   info,
		"capabilities": map[string]any{"optOutNotificationMethods": optOut()},
	}

	if err := c.Call(ctx, "initialize", params, nil); err != nil {
		return fmt.Errorf("initialize the connection to Codex's app server: %w", err)
	}

	return c.notify(ctx, "initialized")
}

// notify sends a notification, which has no answer. It refuses any not allowed.
func (c *Client) notify(ctx context.Context, method string) error {
	if !slices.Contains(allowedNotifications(), method) {
		return fmt.Errorf("%w: %s", ErrNotAllowed, method)
	}

	return c.write(ctx, map[string]any{"method": method})
}

// write sends one message, waiting to write only as long as ctx allows. A message cut
// short, or one whose write failed, leaves the connection closed, so the observer
// connects again.
func (c *Client) write(ctx context.Context, msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("write to Codex's app server: %w", err)
	}

	err = c.ws.writeText(ctx, data)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, errBroken):
		c.fail(err)

		return fmt.Errorf("%w: %w", ErrClosed, err)
	default:
		return err // the caller's own context, or a message never sent
	}
}

// read reads every message until the connection ends, answering the calls waiting for
// them and signaling Changed for the notifications that say a thread changed. A request
// from the daemon is never answered: the client acts on nothing.
func (c *Client) read() {
	defer close(c.done)

	for {
		data, err := c.ws.readMessage()
		if err != nil {
			c.fail(fmt.Errorf("%w: %w", ErrClosed, err))

			return
		}

		c.handle(data)
	}
}

// handle acts on one message: a response goes to its call, and a notification that a
// thread changed signals Changed. Anything else, a request among them, is left alone.
func (c *Client) handle(data []byte) {
	var msg message
	if json.Unmarshal(data, &msg) != nil {
		return
	}

	switch {
	case msg.ID != nil && msg.Method == "":
		c.answer(msg)
	case msg.ID == nil && slices.Contains(changeMethods(), msg.Method):
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}
}

// answer hands a response to the call waiting for it. It never waits: a second answer to
// one call, which nobody reads, is dropped.
func (c *Client) answer(msg message) {
	var id int64
	if json.Unmarshal(*msg.ID, &id) != nil {
		return
	}

	c.mu.Lock()
	wait, ok := c.pending[id]
	c.mu.Unlock()

	if !ok {
		return
	}

	r := reply{result: msg.Result}
	if msg.Error != nil {
		r.err = &RPCError{Code: msg.Error.Code, Message: msg.Error.Message}
	}

	select {
	case wait <- r:
	default:
	}
}

// fail records why the connection is gone, once.
func (c *Client) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err == nil {
		c.err = err
	}
}

// failure is why the connection is gone.
func (c *Client) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err == nil {
		return ErrClosed
	}

	return c.err
}

// RPCError is an error the daemon answered a call with.
type RPCError struct {
	Code    int64
	Message string
}

// Error returns the daemon's message.
func (e *RPCError) Error() string {
	return fmt.Sprintf("Codex's app server: %s (%d)", e.Message, e.Code)
}
