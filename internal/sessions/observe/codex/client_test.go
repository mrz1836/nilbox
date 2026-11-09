package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rawConnect connects a Client over a pipe to a rawServer that answers the handshake and
// the initialize call, then reads nothing more unless the test does.
func rawConnect(t *testing.T) (*Client, *rawServer) {
	t.Helper()

	client, s := pipeServer(t)

	go func() {
		if s.accept("101 Switching Protocols", false, "") != "" {
			return
		}

		f, err := s.readFrame()
		if err != nil {
			return
		}

		var call struct {
			ID int64 `json:"id"`
		}

		if json.Unmarshal(f.payload, &call) != nil {
			return
		}

		s.frame(true, opText, fmt.Appendf(nil, `{"id":%d,"result":{}}`, call.ID))

		_, _ = s.readFrame() // initialized
	}()

	c, err := Connect(t.Context(), client, ClientInfo{Name: "test", Version: "0"})
	require.NoError(t, err)

	return c, s
}

func TestClientCallEndsWithItsContextWhileAFrameIsStuck(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, s := rawConnect(t)

		// The daemon pings, then reads nothing more, so the pong owed it cannot go out.
		s.frame(true, opPing, []byte("are you there"))
		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		start := time.Now()
		err := c.Call(ctx, "thread/loaded/list", map[string]any{}, nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Second, time.Since(start), "the call waited for its turn to write only as long as it could")

		require.NoError(t, c.Close(), "closing never waits on the stuck frame")
	})
}

func TestClientDropsAConnectionWhoseWriteOutlivesItsCall(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c, _ := rawConnect(t) // the daemon reads nothing more, so the call's own frame sticks

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		err := c.Call(ctx, "thread/loaded/list", map[string]any{}, nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorIs(t, err, ErrClosed, "a frame cut short leaves the connection broken")

		synctest.Wait()

		select {
		case <-c.Done():
		default:
			assert.Fail(t, "the connection stayed open, so the observer would not connect again")
		}

		require.NoError(t, c.Close())
	})
}

func TestClientDropsASecondAnswerToOneCall(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		wait := make(chan reply, 1)
		c := &Client{pending: map[int64]chan reply{7: wait}, changed: make(chan struct{}, 1)}

		done := make(chan struct{})

		go func() {
			defer close(done)

			c.handle([]byte(`{"id":7,"result":{"n":1}}`))
			c.handle([]byte(`{"id":7,"result":{"n":2}}`))
		}()

		synctest.Wait()

		select {
		case <-done:
		default:
			require.Fail(t, "the reader waited on a call that already has its answer")
		}

		assert.JSONEq(t, `{"n":1}`, string((<-wait).result), "the first answer is the call's")
	})
}
