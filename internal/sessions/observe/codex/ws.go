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
	"net/textproto"
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
)

var (
	// errHandshake is returned when the daemon does not accept the WebSocket.
	errHandshake = errors.New("the app server did not accept the connection")

	// errProtocol is returned for a frame that breaks the protocol.
	errProtocol = errors.New("the app server broke the WebSocket protocol")

	// errTooLarge is returned for a message over the read limit.
	errTooLarge = errors.New("the app server sent a message too large")
)

// wsConn is a client WebSocket over a stream. One goroutine reads; writes may come from
// any.
type wsConn struct {
	stream io.ReadWriteCloser
	r      *bufio.Reader
	limit  uint64

	wmu sync.Mutex
}

// dialWS opens a WebSocket over stream, failing when ctx ends first.
func dialWS(ctx context.Context, stream io.ReadWriteCloser, limit uint64) (*wsConn, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("start the WebSocket handshake: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(key)
	c := &wsConn{stream: stream, r: bufio.NewReader(stream), limit: limit}

	done := make(chan error, 1)

	go func() { done <- c.handshake(encoded) }()

	select {
	case err := <-done:
		if err != nil {
			return nil, err
		}

		return c, nil
	case <-ctx.Done():
		_ = stream.Close()

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
	reader := textproto.NewReader(c.r)

	status, err := reader.ReadLine()
	if err != nil {
		return fmt.Errorf("%w: %w", errHandshake, err)
	}

	if fields := strings.Fields(status); len(fields) < 2 || fields[1] != "101" {
		return fmt.Errorf("%w: %q", errHandshake, status)
	}

	header, err := reader.ReadMIMEHeader()
	if err != nil {
		return fmt.Errorf("%w: %w", errHandshake, err)
	}

	sum := sha1.Sum([]byte(key + wsGUID)) //nolint:gosec // the handshake's own derivation
	if header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) ||
		!strings.EqualFold(header.Get("Upgrade"), "websocket") {
		return fmt.Errorf("%w: unexpected upgrade headers", errHandshake)
	}

	return nil
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
// returned, which ends the connection with io.EOF.
func (c *wsConn) control(op byte, payload []byte) error {
	switch op {
	case opPing:
		return c.writeFrame(opPong, payload)
	case opPong:
		return nil
	case opClose:
		_ = c.writeFrame(opClose, nil)

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

// writeText sends one whole text message.
func (c *wsConn) writeText(data []byte) error {
	return c.writeFrame(opText, data)
}

// writeFrame sends one final frame, masked as a client must.
func (c *wsConn) writeFrame(op byte, payload []byte) error {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return fmt.Errorf("mask a WebSocket frame: %w", err)
	}

	frame := make([]byte, 0, len(payload)+14)
	frame = append(frame, finBit|op)

	switch n := len(payload); {
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

	c.wmu.Lock()
	defer c.wmu.Unlock()

	if _, err := c.stream.Write(frame); err != nil {
		return err //nolint:wrapcheck // the stream's own end
	}

	return nil
}

// close says goodbye, when it still can, and closes the stream.
func (c *wsConn) close() error {
	_ = c.writeFrame(opClose, nil)

	return c.stream.Close() //nolint:wrapcheck // the stream's own error
}
