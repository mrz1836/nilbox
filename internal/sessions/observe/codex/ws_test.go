package codex

import (
	"bufio"
	"context"
	"crypto/sha1" //nolint:gosec // the handshake's own derivation
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientFrame is one frame the client sent, unmasked.
type clientFrame struct {
	op      byte
	payload []byte
	masked  bool
}

// rawServer is the server end of a pipe that speaks WebSocket frames by hand, so a test
// can send what a library would not: fragments, pings, and broken frames. Once the
// handshake is answered, it reads every frame the client sends into frames, so the
// client never waits on it, as it never waits on a real proxy's pipe.
type rawServer struct {
	conn   net.Conn
	r      *bufio.Reader
	frames chan clientFrame
}

// answer reads the client's handshake and answers it with status, and a correct accept
// value unless badAccept is set, then reads every frame the client sends. It reports what
// was wrong with the handshake, or "".
func (s *rawServer) answer(status string, badAccept bool) string {
	if wrong := s.accept(status, badAccept, ""); wrong != "" {
		return wrong
	}

	go s.pump()

	return ""
}

// accept reads the client's handshake and answers it with status and the header lines in
// extra, and a correct accept value unless badAccept is set. It reads nothing more. It
// reports what was wrong with the handshake, or "".
func (s *rawServer) accept(status string, badAccept bool, extra string) string {
	reader := textproto.NewReader(s.r)

	line, err := reader.ReadLine()
	if err != nil || line != "GET / HTTP/1.1" {
		return "request line " + line
	}

	header, err := reader.ReadMIMEHeader()
	if err != nil || header.Get("Upgrade") != "websocket" || header.Get("Sec-WebSocket-Version") != "13" {
		return "headers"
	}

	sum := sha1.Sum([]byte(header.Get("Sec-WebSocket-Key") + wsGUID)) //nolint:gosec // the handshake's own derivation

	accept := base64.StdEncoding.EncodeToString(sum[:])
	if badAccept {
		accept = "nope"
	}

	_, _ = io.WriteString(s.conn, "HTTP/1.1 "+status+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		extra+"Sec-WebSocket-Accept: "+accept+"\r\n\r\n")

	return ""
}

// pump reads the client's frames until the pipe closes.
func (s *rawServer) pump() {
	defer close(s.frames)

	for {
		f, err := s.readFrame()
		if err != nil {
			return
		}

		s.frames <- f
	}
}

// next returns the next frame the client sent.
func (s *rawServer) next(t *testing.T) clientFrame {
	t.Helper()

	select {
	case f, ok := <-s.frames:
		require.True(t, ok, "the client closed the pipe")

		return f
	case <-time.After(5 * time.Second):
		t.Fatal("the client sent nothing")

		return clientFrame{}
	}
}

// frame sends one unmasked frame. A failed write, to a client that stopped reading, is
// ignored: the test checks what the client did.
func (s *rawServer) frame(fin bool, op byte, payload []byte) {
	head := op
	if fin {
		head |= finBit
	}

	out := []byte{head}

	switch n := len(payload); {
	case n <= maxControl:
		out = append(out, byte(n))
	case n <= 0xFFFF:
		out = append(out, 126)
		out = binary.BigEndian.AppendUint16(out, uint16(n))
	default:
		out = append(out, 127)
		out = binary.BigEndian.AppendUint64(out, uint64(n))
	}

	_, _ = s.conn.Write(append(out, payload...))
}

// readFrame reads one frame the client sent, unmasking it.
func (s *rawServer) readFrame() (clientFrame, error) {
	var head [2]byte
	if _, err := io.ReadFull(s.r, head[:]); err != nil {
		return clientFrame{}, err
	}

	size := uint64(head[1] & 0x7F)

	switch size {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(s.r, ext[:]); err != nil {
			return clientFrame{}, err
		}

		size = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(s.r, ext[:]); err != nil {
			return clientFrame{}, err
		}

		size = binary.BigEndian.Uint64(ext[:])
	}

	var mask [4]byte
	if _, err := io.ReadFull(s.r, mask[:]); err != nil {
		return clientFrame{}, err
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(s.r, payload); err != nil {
		return clientFrame{}, err
	}

	for i := range payload {
		payload[i] ^= mask[i%4]
	}

	return clientFrame{op: head[0] & 0x0F, payload: payload, masked: head[1]&maskBit != 0}, nil
}

// pipeServer returns a client end and a rawServer on the other end of a pipe.
func pipeServer(t *testing.T) (net.Conn, *rawServer) {
	t.Helper()

	client, server := net.Pipe()

	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})

	return client, &rawServer{conn: server, r: bufio.NewReader(server), frames: make(chan clientFrame, 16)}
}

// dialRaw dials a client with limit over a pipe to a rawServer, which reads every frame
// the client sends.
func dialRaw(t *testing.T, limit uint64) (*wsConn, *rawServer) {
	t.Helper()

	client, s := pipeServer(t)

	wrong := make(chan string, 1)

	go func() { wrong <- s.answer("101 Switching Protocols", false) }()

	c, err := dialWS(t.Context(), client, limit)
	require.NoError(t, err)
	require.Empty(t, <-wrong, "the client's handshake")

	return c, s
}

// dialQuiet dials a client over a pipe to a rawServer that reads nothing after the
// handshake unless the test does, as a proxy that stopped reading would.
func dialQuiet(t *testing.T) (*wsConn, *rawServer) {
	t.Helper()

	client, s := pipeServer(t)

	wrong := make(chan string, 1)

	go func() { wrong <- s.accept("101 Switching Protocols", false, "") }()

	c, err := dialWS(t.Context(), client, 1<<20)
	require.NoError(t, err)
	require.Empty(t, <-wrong, "the client's handshake")

	return c, s
}

func TestWSReadsOnWhileAPongCannotGoOut(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, s := dialQuiet(t)

		go func() {
			s.frame(true, opPing, []byte("are you there"))
			s.frame(true, opText, []byte("after the ping"))
		}()

		got, err := c.readMessage()
		require.NoError(t, err, "the reader never waits on the pong it owes")
		assert.Equal(t, "after the ping", string(got))
	})
}

func TestWSHandshakeRefusesAnOversizedAnswer(t *testing.T) {
	t.Parallel()

	for name, answer := range map[string]struct {
		status string
		extra  string
	}{
		"a status line over the cap": {status: "101 " + strings.Repeat("x", 8<<10)},
		"a header line over the cap": {
			status: "101 Switching Protocols", extra: "X-Pad: " + strings.Repeat("y", 8<<10) + "\r\n",
		},
		"too many header lines": {status: "101 Switching Protocols", extra: strings.Repeat("X-Pad: y\r\n", 100)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, s := pipeServer(t)

			go s.accept(answer.status, false, answer.extra)

			_, err := dialWS(t.Context(), client, 1<<20)
			require.ErrorIs(t, err, errHandshake)
		})
	}
}

func TestWSReadsWholeMessages(t *testing.T) {
	t.Parallel()

	c, s := dialRaw(t, 1<<20)

	go func() {
		s.frame(false, opText, []byte(`{"id":1,`))
		s.frame(true, opPing, []byte("are you there"))
		s.frame(true, opContinuation, []byte(`"result":{}}`))
	}()

	got, err := c.readMessage()
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":1,"result":{}}`, string(got), "fragments are joined, around a ping")

	pong := s.next(t)
	assert.Equal(t, byte(opPong), pong.op)
	assert.Equal(t, "are you there", string(pong.payload), "a ping is answered with its own payload")

	for _, n := range []int{126, 70_000} {
		big := strings.Repeat("x", n)

		go s.frame(true, opText, []byte(big))

		got, err = c.readMessage()
		require.NoError(t, err)
		assert.Len(t, got, n)
	}
}

func TestWSWritesMaskedFrames(t *testing.T) {
	t.Parallel()

	c, s := dialRaw(t, 1<<20)

	for _, n := range []int{5, 300, 70_000} {
		msg := strings.Repeat("y", n)
		require.NoError(t, c.writeText(t.Context(), []byte(msg)))

		f := s.next(t)
		assert.True(t, f.masked, "a client's frames are masked")
		assert.Equal(t, byte(opText), f.op)
		assert.Equal(t, msg, string(f.payload))
	}

	err := c.writeText(t.Context(), make([]byte, maxWrite+1))
	require.ErrorIs(t, err, errTooLargeToSend, "a frame over the cap is refused")

	require.NoError(t, c.writeText(t.Context(), []byte("after")))
	assert.Equal(t, "after", string(s.next(t).payload), "the refused frame sent nothing")
}

func TestWSEndsOnClose(t *testing.T) {
	t.Parallel()

	c, s := dialRaw(t, 1<<20)

	go s.frame(true, opClose, nil)

	_, err := c.readMessage()
	require.ErrorIs(t, err, io.EOF)
	assert.Equal(t, byte(opClose), s.next(t).op, "the close is returned")

	require.NoError(t, c.close())
}

func TestWSRefusesWhatBreaksTheProtocol(t *testing.T) {
	t.Parallel()

	tests := map[string]func(s *rawServer){
		"a masked frame": func(s *rawServer) {
			_, _ = s.conn.Write([]byte{finBit | opText, maskBit | 1, 0, 0, 0, 0, 'x'})
		},
		"reserved bits": func(s *rawServer) {
			_, _ = s.conn.Write([]byte{finBit | 0x40 | opText, 0})
		},
		"a continuation with no message": func(s *rawServer) { s.frame(true, opContinuation, []byte("x")) },
		"a new message inside a message": func(s *rawServer) {
			s.frame(false, opText, []byte("x"))
			s.frame(true, opText, []byte("y"))
		},
		"a split control frame": func(s *rawServer) { s.frame(false, opPing, []byte("x")) },
		"an unknown opcode":     func(s *rawServer) { s.frame(true, 0x3, []byte("x")) },
	}

	for name, send := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c, s := dialRaw(t, 1<<20)

			go send(s)

			_, err := c.readMessage()
			require.ErrorIs(t, err, errProtocol)
		})
	}

	t.Run("a message over the limit", func(t *testing.T) {
		t.Parallel()

		c, s := dialRaw(t, 10)

		go s.frame(true, opText, []byte(strings.Repeat("z", 11)))

		_, err := c.readMessage()
		require.ErrorIs(t, err, errTooLarge)
	})
}

func TestWSHandshakeRefused(t *testing.T) {
	t.Parallel()

	for name, answer := range map[string]struct {
		status    string
		badAccept bool
	}{
		"not upgraded":     {status: "400 Bad Request"},
		"a wrong accept":   {status: "101 Switching Protocols", badAccept: true},
		"a garbled status": {status: ""},
	} {
		client, s := pipeServer(t)

		go s.answer(answer.status, answer.badAccept)

		_, err := dialWS(t.Context(), client, 1<<20)
		require.ErrorIs(t, err, errHandshake, name)
	}
}

func TestWSHandshakeEndsWithItsContext(t *testing.T) {
	t.Parallel()

	client, s := pipeServer(t)

	go func() { _, _ = io.Copy(io.Discard, s.conn) }()

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	_, err := dialWS(ctx, client, 1<<20)
	require.ErrorIs(t, err, context.DeadlineExceeded, "a server that never answers")
}
