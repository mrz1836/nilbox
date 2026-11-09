package codex

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6455 names SHA-1 for the handshake; it secures nothing
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// The WebSocket protocol (RFC 6455), as far as a client of the daemon needs it: one
// connection over a byte stream, whole text messages each way, and the control frames.
// The daemon is local and the stream a pipe, so there is no TLS, no extension, and no
// subprotocol.
const (
	// wsGUID is the key the handshake's accept value is derived with.
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	finBit  = 0x80
	maskBit = 0x80

	// maxControl is the most a control frame may carry.
	maxControl = 125

	// maxWrite is the most one frame the client sends may carry. The client sends only
	// short requests, so a frame anywhere near it is a mistake, and the cap keeps a frame's
	// size from overflowing.
	maxWrite = 1 << 20

	// maxHeader is the most a client frame's header takes: two bytes, an eight-byte
	// length, and the four-byte mask.
	maxHeader = 14

	// maxLine is the longest line the handshake's answer may have, and the size of the
	// connection's read buffer, which a line must fit in.
	maxLine = 4 << 10

	// maxHeaderLines is the most header lines the handshake's answer may have.
	maxHeaderLines = 64
)

var (
	// errHandshake is returned when the daemon does not accept the WebSocket.
	errHandshake = errors.New("the app server did not accept the connection")

	// errProtocol is returned for a frame that breaks the protocol.
	errProtocol = errors.New("the app server broke the WebSocket protocol")

	// errTooLarge is returned for a message over the read limit.
	errTooLarge = errors.New("the app server sent a message too large")

	// errTooLargeToSend is returned for a frame over maxWrite.
	errTooLargeToSend = errors.New("a message too large to send to the app server")

	// errBroken is returned for a frame whose write failed, or was cut short when its
	// context ended. Either closes the connection, as the stream may hold part of a frame.
	errBroken = errors.New("the connection to the app server broke")
)

// wsConn is a client WebSocket over a stream. One goroutine reads; writes may come from
// any, one frame at a time.
type wsConn struct {
	stream io.ReadWriteCloser
	r      *bufio.Reader
	limit  uint64

	// turn holds the right to write: a writer holds it for one frame, and waits for it
	// only as long as its context allows.
	turn chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// dialWS opens a WebSocket over stream, failing when ctx ends first.
func dialWS(ctx context.Context, stream io.ReadWriteCloser, limit uint64) (*wsConn, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("start the WebSocket handshake: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(key)
	c := &wsConn{stream: stream, r: bufio.NewReaderSize(stream, maxLine), limit: limit, turn: make(chan struct{}, 1)}

	done := make(chan error, 1)

	go func() { done <- c.handshake(encoded) }()

	select {
	case err := <-done:
		if err != nil {
			return nil, err
		}

		return c, nil
	case <-ctx.Done():
		_ = c.close()

		return nil, fmt.Errorf("WebSocket handshake: %w", errors.Join(ctx.Err(), <-done))
	}
}

// handshake asks to upgrade the stream to a WebSocket with key, and checks the answer.
func (c *wsConn) handshake(key string) error {
	request := "GET / HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"

	if _, err := io.WriteString(c.stream, request); err != nil {
		return fmt.Errorf("%w: %w", errHandshake, err)
	}

	// The answer is read from the connection's own buffer, which keeps any frame the
	// daemon sends right after it.
	status, err := c.line()
	if err != nil {
		return err
	}

	if fields := strings.Fields(status); len(fields) < 2 || fields[1] != "101" {
		return fmt.Errorf("%w: %q", errHandshake, status)
	}

	header, err := c.header()
	if err != nil {
		return err
	}

	sum := sha1.Sum([]byte(key + wsGUID)) //nolint:gosec // the handshake's own derivation
	if header["sec-websocket-accept"] != base64.StdEncoding.EncodeToString(sum[:]) ||
		!strings.EqualFold(header["upgrade"], "websocket") {
		return fmt.Errorf("%w: unexpected upgrade headers", errHandshake)
	}

	return nil
}

// line reads one line of the handshake's answer, without its line ending. A line that
// does not fit the read buffer is refused, so the answer can never grow without bound.
func (c *wsConn) line() (string, error) {
	line, err := c.r.ReadSlice('\n')
	if err != nil {
		return "", fmt.Errorf("%w: %w", errHandshake, err)
	}

	return strings.TrimRight(string(line), "\r\n"), nil
}

// header reads the handshake answer's header lines up to the blank line that ends them,
// by lower-case name, refusing more than maxHeaderLines.
func (c *wsConn) header() (map[string]string, error) {
	header := map[string]string{}

	for range maxHeaderLines {
		line, err := c.line()
		if err != nil {
			return nil, err
		}

		if line == "" {
			return header, nil
		}

		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%w: a malformed header %q", errHandshake, line)
		}

		header[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}

	return nil, fmt.Errorf("%w: more than %d header lines", errHandshake, maxHeaderLines)
}

// readMessage returns the next whole message, answering pings on the way, or io.EOF once
// the daemon closes the connection.
func (c *wsConn) readMessage() ([]byte, error) {
	var m assembly

	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}

		done, err := c.take(&m, fin, op, payload)
		if err != nil {
			return nil, err
		}

		if done {
			return m.data, nil
		}
	}
}

// assembly is a message read so far, from its frames.
type assembly struct {
	data    []byte
	started bool
}

// take adds one frame to the message being read, acting on a control frame instead, and
// reports whether the message is whole.
func (c *wsConn) take(m *assembly, fin bool, op byte, payload []byte) (bool, error) {
	if op >= opClose {
		return false, c.control(op, payload)
	}

	if err := continues(op, m.started); err != nil {
		return false, err
	}

	m.started = true

	if uint64(len(m.data))+uint64(len(payload)) > c.limit {
		return false, errTooLarge
	}

	m.data = append(m.data, payload...)

	return fin, nil
}

// control acts on a control frame: a ping is answered, a pong ignored, and a close
// answered and returned, which ends the connection with io.EOF. The answers never hold
// the reader up.
func (c *wsConn) control(op byte, payload []byte) error {
	switch op {
	case opPing:
		c.tryWrite(opPong, payload)

		return nil
	case opPong:
		return nil
	case opClose:
		c.tryWrite(opClose, nil)

		return io.EOF
	default:
		return fmt.Errorf("%w: opcode %d", errProtocol, op)
	}
}

// continues checks that a data frame of op may come next: a new message only after the
// last one ended, and a continuation only inside one.
func continues(op byte, started bool) error {
	switch {
	case (op == opText || op == opBinary) && started:
		return fmt.Errorf("%w: a new message inside a message", errProtocol)
	case op == opText || op == opBinary:
		return nil
	case op == opContinuation && !started:
		return fmt.Errorf("%w: a continuation with no message", errProtocol)
	case op == opContinuation:
		return nil
	default:
		return fmt.Errorf("%w: opcode %d", errProtocol, op)
	}
}

// readFrame reads one frame.
func (c *wsConn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(c.r, head[:]); err != nil {
		return false, 0, nil, err //nolint:wrapcheck // the stream's own end
	}

	fin, op = head[0]&finBit != 0, head[0]&0x0F

	if head[0]&0x70 != 0 {
		return false, 0, nil, fmt.Errorf("%w: reserved bits set", errProtocol)
	}

	if head[1]&maskBit != 0 {
		return false, 0, nil, fmt.Errorf("%w: a masked frame from the server", errProtocol)
	}

	size, err := c.payloadSize(head[1] & 0x7F)
	if err != nil {
		return false, 0, nil, err
	}

	if op >= opClose && (size > maxControl || !fin) {
		return false, 0, nil, fmt.Errorf("%w: a control frame too large or split", errProtocol)
	}

	if size > c.limit {
		return false, 0, nil, errTooLarge
	}

	payload = make([]byte, size)
	if _, err = io.ReadFull(c.r, payload); err != nil {
		return false, 0, nil, err //nolint:wrapcheck // the stream's own end
	}

	return fin, op, payload, nil
}

// payloadSize reads a frame's payload length, given its 7-bit length field.
func (c *wsConn) payloadSize(short byte) (uint64, error) {
	switch short {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return 0, err //nolint:wrapcheck // the stream's own end
		}

		return uint64(binary.BigEndian.Uint16(ext[:])), nil
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.r, ext[:]); err != nil {
			return 0, err //nolint:wrapcheck // the stream's own end
		}

		return binary.BigEndian.Uint64(ext[:]), nil
	default:
		return uint64(short), nil
	}
}

// writeText sends one whole text message, as writeFrame does.
func (c *wsConn) writeText(ctx context.Context, data []byte) error {
	return c.writeFrame(ctx, opText, data)
}

// writeFrame sends one final frame. It waits for its turn to write only as long as ctx
// allows, and then returns ctx's error with nothing sent. Once the frame is going out, an
// end of ctx closes the connection, which unblocks the write, and so does a write that
// fails; either returns errBroken. It refuses a payload over maxWrite with
// errTooLargeToSend, sending nothing.
func (c *wsConn) writeFrame(ctx context.Context, op byte, payload []byte) error {
	frame, err := encodeFrame(op, payload)
	if err != nil {
		return err
	}

	if err = ctx.Err(); err != nil {
		return err //nolint:wrapcheck // the caller's own context
	}

	select {
	case c.turn <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller's own context
	}

	defer func() { <-c.turn }()

	stop := context.AfterFunc(ctx, func() { _ = c.close() })

	_, err = c.stream.Write(frame)

	switch {
	case !stop():
		return fmt.Errorf("%w: %w", errBroken, ctx.Err())
	case err != nil:
		_ = c.close()

		return fmt.Errorf("%w: %w", errBroken, err)
	default:
		return nil
	}
}

// tryWrite sends a control frame the reader owes, which must never wait on a write: the
// frame goes out in the background when no other frame is going out, and is dropped
// otherwise.
func (c *wsConn) tryWrite(op byte, payload []byte) {
	frame, err := encodeFrame(op, payload)
	if err != nil {
		return
	}

	select {
	case c.turn <- struct{}{}:
	default:
		return
	}

	go func() {
		defer func() { <-c.turn }()

		_, _ = c.stream.Write(frame)
	}()
}

// encodeFrame returns one final frame of op carrying payload, masked as a client must. It
// refuses a payload over maxWrite with errTooLargeToSend.
func encodeFrame(op byte, payload []byte) ([]byte, error) {
	// The check compares len(payload) itself, the form a static analyzer reads as bounding
	// the payload, so the size below is known not to overflow.
	if len(payload) > maxWrite {
		return nil, fmt.Errorf("%w: %d bytes", errTooLargeToSend, len(payload))
	}

	n := len(payload)

	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return nil, fmt.Errorf("mask a WebSocket frame: %w", err)
	}

	frame := make([]byte, 0, n+maxHeader)
	frame = append(frame, finBit|op)

	switch {
	case n <= maxControl:
		frame = append(frame, maskBit|byte(n))
	case n <= 0xFFFF:
		frame = append(frame, maskBit|126)
		frame = binary.BigEndian.AppendUint16(frame, uint16(n))
	default:
		frame = append(frame, maskBit|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(n))
	}

	frame = append(frame, mask[:]...)

	for i, b := range payload {
		frame = append(frame, b^mask[i%4])
	}

	return frame, nil
}

// close closes the stream, once, which ends the connection and any write still going
// out. It sends no close frame first: one could wait on a stream the other end no longer
// reads, and the proxy ends its own connection to the daemon once its input closes.
func (c *wsConn) close() error {
	c.closeOnce.Do(func() { c.closeErr = c.stream.Close() })

	return c.closeErr
}
