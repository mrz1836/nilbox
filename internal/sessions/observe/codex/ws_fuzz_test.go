package codex

import (
	"bufio"
	"bytes"
	"crypto/sha1" //nolint:gosec // the handshake's own derivation
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fuzzKey returns the handshake key the fuzz tests send: RFC 6455's sample nonce, encoded
// as a client sends it.
func fuzzKey() string {
	return base64.StdEncoding.EncodeToString([]byte("the sample nonce"))
}

// replay is a stream that reads what the daemon sent and drops what the client writes.
type replay struct{ io.Reader }

func (replay) Write(p []byte) (int, error) { return len(p), nil }

func (replay) Close() error { return nil }

// replayConn returns a connection, past its handshake, that reads sent.
func replayConn(sent []byte, limit uint64) *wsConn {
	stream := replay{bytes.NewReader(sent)}

	return &wsConn{stream: stream, r: bufio.NewReaderSize(stream, maxLine), limit: limit, turn: make(chan struct{}, 1)}
}

// serverFrame is one frame as the daemon sends it: unmasked, with the shortest length.
func serverFrame(fin bool, op byte, payload []byte) []byte {
	head := op
	if fin {
		head |= finBit
	}

	out := []byte{head}

	switch n := len(payload); {
	case n <= maxControl:
		out = append(out, byte(n))
	case n <= 0xFFFF:
		out = binary.BigEndian.AppendUint16(append(out, 126), uint16(n))
	default:
		out = binary.BigEndian.AppendUint64(append(out, 127), uint64(n))
	}

	return append(out, payload...)
}

// FuzzWSReadMessage checks that any bytes the daemon sends read as whole messages no
// larger than the limit, ending with the stream's end, a broken protocol, or a message
// too large, and that a message the daemon sends in two fragments with a ping between
// them reads back as that message.
func FuzzWSReadMessage(f *testing.F) {
	for _, seed := range []struct {
		sent    []byte
		limit   uint16
		message string
		split   uint8
	}{
		{nil, 0, "", 0},
		{serverFrame(true, opText, []byte(`{"id":1}`)), 64, `{"result":{}}`, 3},
		{append(serverFrame(false, opText, []byte("ab")), serverFrame(true, opContinuation, []byte("cd"))...), 3, "x", 9},
		{serverFrame(true, opPing, []byte("hi")), 8, strings.Repeat("y", 200), 130},
		{[]byte{finBit | opText, 127, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, 1024, "z", 1},
		{[]byte{finBit | opText, maskBit | 1, 0, 0, 0, 0, 'a'}, 16, "", 0},
		{[]byte{0x70 | opText, 0}, 16, "a", 0},
		{serverFrame(false, opPing, nil), 16, "a", 1},
		{serverFrame(true, opContinuation, []byte("x")), 16, "a", 1},
		{serverFrame(true, opClose, nil), 16, "a", 1},
	} {
		f.Add(seed.sent, seed.limit, seed.message, seed.split)
	}

	f.Fuzz(func(t *testing.T, sent []byte, limit uint16, message string, split uint8) {
		c := replayConn(sent, uint64(limit))

		for {
			m, err := c.readMessage()
			if err != nil {
				assert.True(t, errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
					errors.Is(err, errProtocol) || errors.Is(err, errTooLarge), "%v", err)

				break
			}

			assert.LessOrEqual(t, uint64(len(m)), uint64(limit))
		}

		cut := min(int(split), len(message))
		fragments := bytes.Join([][]byte{
			serverFrame(false, opText, []byte(message[:cut])),
			serverFrame(true, opPing, []byte("ping")),
			serverFrame(true, opContinuation, []byte(message[cut:])),
		}, nil)

		got, err := replayConn(fragments, uint64(len(message))+maxControl).readMessage()
		require.NoError(t, err)
		assert.Equal(t, message, string(got))
	})
}

// FuzzWSHandshake checks that the handshake takes an answer only when it holds the status
// 101 and the accept value derived from the key, refusing any other as one the daemon
// didn't accept, and that an accepting answer, with any header line before the ones that
// accept, is taken, leaving what follows it to read as frames.
func FuzzWSHandshake(f *testing.F) {
	key := fuzzKey()
	sum := sha1.Sum([]byte(key + wsGUID)) //nolint:gosec // the handshake's own derivation
	accept := base64.StdEncoding.EncodeToString(sum[:])

	for _, seed := range []struct {
		answer, name, value string
	}{
		{"", "Server", "codex"},
		{"HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n", "X", ""},
		{"HTTP/1.1 101\r\nUpgrade: WebSocket\r\nSec-WebSocket-Accept: x\r\n\r\n", "Sec-WebSocket-Accept", "x"},
		{"HTTP/1.1 400 Bad Request\r\n\r\n", "", ":"},
		{"HTTP/1.1 101\r\nno colon here\r\n\r\n", "Upgrade", "not-websocket"},
		{"HTTP/1.1 101\n" + strings.Repeat("A: b\n", maxHeaderLines+1), "a", strings.Repeat("v", maxLine)},
	} {
		f.Add([]byte(seed.answer), seed.name, seed.value)
	}

	f.Fuzz(func(t *testing.T, answer []byte, name, value string) {
		if err := replayConn(answer, maxLine).handshake(key); err != nil {
			require.ErrorIs(t, err, errHandshake)
		} else {
			status, _, _ := strings.Cut(string(answer), "\n")
			assert.Equal(t, "101", strings.Fields(status)[1])
			assert.Contains(t, string(answer), accept)
		}

		// The header lines that accept come after the extra one, so they are the ones read.
		extra := name + ":" + value
		if strings.ContainsAny(extra, "\r\n") || len(extra)+2 > maxLine {
			return
		}

		accepting := "HTTP/1.1 101 Switching Protocols\r\n" + extra + "\r\nUpgrade: websocket\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"

		c := replayConn(append([]byte(accepting), serverFrame(true, opText, []byte("after"))...), maxLine)
		require.NoError(t, c.handshake(key))

		got, err := c.readMessage()
		require.NoError(t, err)
		assert.Equal(t, "after", string(got))
	})
}
