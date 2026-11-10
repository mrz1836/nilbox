package shellenv

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCappedKeepsTheFirstBytesAndTakesEverything(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	c := &capped{buf: &buf, max: 5}

	for _, chunk := range []string{"abc", "defg", "hij"} {
		n, err := c.Write([]byte(chunk))
		require.NoError(t, err)
		assert.Equal(t, len(chunk), n, "every write is taken, so the writer never blocks")
	}

	assert.Equal(t, "abcde", buf.String())
}
