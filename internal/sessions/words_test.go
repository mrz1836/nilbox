package sessions_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errWordsUnreadable = errors.New("the transcript can't be read")

// fakeWords answers every session with text, or err.
type fakeWords struct {
	text string
	err  error
}

func (f fakeWords) LastWords(context.Context, sessions.Record) (string, error) {
	return f.text, f.err
}

// wordsService returns a Service over a ledger holding session s1, which last said "The
// start of it", reading Claude's sessions with reader.
func wordsService(t *testing.T, reader sessions.WordsReader) *sessions.Service {
	t.Helper()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	_, err := store.Apply(t.Context(), launched().Add(time.Minute), []sessions.Observation{
		{ID: "s1", State: sessions.StateIdle, LastMessage: "The start of it"},
	})
	require.NoError(t, err)

	return sessions.NewService(sessions.Config{
		Store: store, Words: map[agent.ID]sessions.WordsReader{agent.Claude: reader, agent.Codex: nil},
		Clock: testutil.NewClock(launched().Add(time.Hour)),
	})
}

func TestLastWordsReadsTheWholeMessage(t *testing.T) {
	t.Parallel()

	got, err := wordsService(t, fakeWords{text: "  The whole of it, and more.\n"}).LastWords(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.Words{Text: "The whole of it, and more."}, got)
}

func TestLastWordsCutsAVeryLongMessage(t *testing.T) {
	t.Parallel()

	got, err := wordsService(t, fakeWords{text: strings.Repeat("word ", sessions.MaxWordsRunes)}).LastWords(t.Context(), "s1")
	require.NoError(t, err)
	assert.True(t, got.Partial)
	assert.LessOrEqual(t, len([]rune(got.Text)), sessions.MaxWordsRunes+1)
}

func TestLastWordsFallsBackToWhatTheLedgerKeeps(t *testing.T) {
	t.Parallel()

	for name, reader := range map[string]sessions.WordsReader{
		"nothing to read": fakeWords{err: sessions.ErrNoWords},
		"a failed read":   fakeWords{err: errWordsUnreadable},
		"an empty read":   fakeWords{text: "  "},
		"no reader":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := wordsService(t, reader).LastWords(t.Context(), "s1")
			require.NoError(t, err)
			assert.Equal(t, sessions.Words{Text: "The start of it", Partial: true}, got)
		})
	}
}

func TestLastWordsOfASessionThatIsGone(t *testing.T) {
	t.Parallel()

	_, err := wordsService(t, fakeWords{text: "x"}).LastWords(t.Context(), "nope")
	require.ErrorIs(t, err, sessions.ErrNotFound)
}
