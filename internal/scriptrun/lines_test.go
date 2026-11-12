package scriptrun

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// lineKeeper is a reader of a run's output that keeps every line it reads.
type lineKeeper struct {
	*LineWriter

	lines []string
}

func newLineKeeper() *lineKeeper {
	k := &lineKeeper{}
	k.LineWriter = NewLineWriter(func(line string) { k.lines = append(k.lines, line) })

	return k
}

// read returns the lines read so far, under the writer's lock.
func (k *lineKeeper) read() []string {
	k.Lock()
	defer k.Unlock()

	return append([]string{}, k.lines...)
}

func TestLineWriterReadsWholeLines(t *testing.T) {
	t.Parallel()

	k := newLineKeeper()

	for _, part := range []string{"fir", "st\r\nsec", "ond\n\x1b[31mred\x1b[0m\nprogress 1\rprogress 2\n", "last"} {
		n, err := k.Write([]byte(part))
		require.NoError(t, err)
		assert.Equal(t, len(part), n)
	}

	assert.Equal(t, []string{"first", "second", "red", "progress 2"}, k.read(), "a line still missing its newline waits")

	k.Flush()
	assert.Equal(t, []string{"first", "second", "red", "progress 2", "last"}, k.read())

	k.Flush()
	assert.Len(t, k.read(), 5, "nothing is left to flush")

	_, err := k.Write([]byte(strings.Repeat("x", textutil.MaxLine+10) + "\nnext\n"))
	require.NoError(t, err)

	got := k.read()
	require.Len(t, got, 7)
	assert.Len(t, got[5], textutil.MaxLine, "a long line keeps its first MaxLine bytes")
	assert.Equal(t, "next", got[6])
}

func TestLineWriterReadsUnderItsLock(t *testing.T) {
	t.Parallel()

	k := newLineKeeper()

	var wg sync.WaitGroup

	for i := range 8 {
		wg.Go(func() {
			for j := range 50 {
				_, _ = k.Write([]byte(strconv.Itoa(i*100+j) + "\n"))
				_ = k.read()
			}
		})
	}

	wg.Wait()
	assert.Len(t, k.read(), 400)
}

func TestNotes(t *testing.T) {
	t.Parallel()

	var n Notes

	for i := range MaxNotes + 10 {
		n.Warn("warning " + strconv.Itoa(i))
		n.Unknown("line " + strconv.Itoa(i))
	}

	require.Len(t, n.Warnings, MaxNotes)
	assert.Equal(t, "warning 0", n.Warnings[0], "the first ones")
	assert.Equal(t, "warning 199", n.Warnings[MaxNotes-1])
	require.Len(t, n.Unrecognized, MaxUnrecognized)
	assert.Equal(t, "line 199", n.Unrecognized[MaxUnrecognized-1])
	assert.Equal(t, MaxNotes+10, n.UnrecognizedCount, "every line is counted")

	var list []string
	for range MaxNotes + 1 {
		list = KeepNote(list, "x")
	}

	assert.Len(t, list, MaxNotes)
	assert.Equal(t, []string{"x"}, KeepNote(nil, "x"))
}
