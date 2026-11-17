package sessions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errUsageUnreadable = errors.New("the transcript can't be read")

// fakeUsage answers every session with got, or err, and records the session it read.
type fakeUsage struct {
	got  sessions.Usage
	err  error
	read *sessions.Record
}

func (f fakeUsage) Usage(_ context.Context, r sessions.Record) (sessions.Usage, error) {
	if f.read != nil {
		*f.read = r
	}

	return f.got, f.err
}

// usageService returns a Service over a ledger holding Claude's session s1 and s2, which
// the agent hasn't named yet, reading Claude's sessions' counts with reader.
func usageService(t *testing.T, reader sessions.UsageReader) *sessions.Service {
	t.Helper()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	unnamed := record("s2", 13)
	unnamed.AgentSessionID = ""
	require.NoError(t, store.Insert(t.Context(), unnamed))

	return sessions.NewService(sessions.Config{
		Store: store, Usage: map[agent.ID]sessions.UsageReader{agent.Claude: reader, agent.Codex: nil},
	})
}

func TestUsageReadsTheAgentsCounts(t *testing.T) {
	t.Parallel()

	var read sessions.Record

	want := sessions.Usage{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 4, Partial: true}.Sum()

	got, err := usageService(t, fakeUsage{got: want, read: &read}).Usage(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, int64(334), got.Total)
	assert.Equal(t, "s1", read.AgentSessionID, "it reads the session's own files")
}

func TestUsageKnowsNothingItCantRead(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		reader sessions.UsageReader
		id     string
	}{
		"nothing to read":     {reader: fakeUsage{err: sessions.ErrNoUsage}, id: "s1"},
		"a failed read":       {reader: fakeUsage{err: errUsageUnreadable}, id: "s1"},
		"a read with nothing": {reader: fakeUsage{}, id: "s1"},
		"no reader":           {reader: nil, id: "s1"},
		"not named yet":       {reader: fakeUsage{got: sessions.Usage{Output: 1}.Sum()}, id: "s2"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := usageService(t, tt.reader).Usage(t.Context(), tt.id)
			require.NoError(t, err)
			assert.Equal(t, sessions.Usage{}, got)
		})
	}
}

func TestUsageOfASessionThatIsGone(t *testing.T) {
	t.Parallel()

	_, err := usageService(t, fakeUsage{}).Usage(t.Context(), "nope")
	require.ErrorIs(t, err, sessions.ErrNotFound)
}

// fakeRunTokens answers the background run 7 with got, ok, and err, and every other run as
// recorded nowhere.
type fakeRunTokens struct {
	got sessions.Usage
	ok  bool
	err error
}

func (f fakeRunTokens) RunTokens(_ context.Context, runID int64) (sessions.Usage, bool, error) {
	if runID != 7 {
		return sessions.Usage{}, false, nil
	}

	return f.got, f.ok, f.err
}

// runService returns a Service over a ledger holding s3, the session of the background
// run 7, reading its run's tokens with tokens, and Claude's files with a reader that would
// report 999 tokens.
func runService(t *testing.T, tokens sessions.RunTokens) *sessions.Service {
	t.Helper()

	store := sessions.NewStore(testutil.DB(t))

	background := record("s3", 14)
	background.RunID = 7
	require.NoError(t, store.Insert(t.Context(), background))

	return sessions.NewService(sessions.Config{
		Store:     store,
		Usage:     map[agent.ID]sessions.UsageReader{agent.Claude: fakeUsage{got: sessions.Usage{Output: 999}.Sum()}},
		RunTokens: tokens,
	})
}

func TestUsageOfABackgroundRunIsWhatItsRunReported(t *testing.T) {
	t.Parallel()

	got, err := runService(t, fakeRunTokens{
		got: sessions.Usage{Input: 100, Output: 20, CacheRead: 3000}, ok: true,
	}).Usage(t.Context(), "s3")
	require.NoError(t, err)
	assert.Equal(t, sessions.Usage{
		Known: true, FromRun: true, Input: 100, Output: 20, CacheRead: 3000, Total: 3120,
	}, got, "its run's record, never the agent's files")
}

func TestUsageOfABackgroundRunThatReportedNothing(t *testing.T) {
	t.Parallel()

	for name, tokens := range map[string]sessions.RunTokens{
		"no reader":        nil,
		"recorded nowhere": fakeRunTokens{},
		"a failed read":    fakeRunTokens{err: errUsageUnreadable},
		"nothing reported": fakeRunTokens{ok: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := runService(t, tokens).Usage(t.Context(), "s3")
			require.NoError(t, err)
			assert.Equal(t, sessions.Usage{}, got, "it never falls back to the agent's files")
		})
	}
}
