package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRolloutsThreads(t *testing.T) {
	t.Parallel()

	const (
		thread = "01a1bebd-6f52-7942-9e77-e69115ff8ee8"
		dir    = "/srv/wt/example-repo/pr-12"
	)

	home := t.TempDir()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.Local) //nolint:gosmopolitan // Codex's folders are local days
	from := now.Add(-time.Hour)

	day := filepath.Join(home, "sessions", "2026", "11", "09")
	require.NoError(t, os.MkdirAll(day, 0o700))

	name := "rollout-2026-11-09T00-17-28-" + thread + ".jsonl"
	data, err := os.ReadFile(filepath.Join("testdata", "rollout", name))
	require.NoError(t, err)

	write := func(file string, content []byte, at time.Time) {
		path := filepath.Join(day, file)
		require.NoError(t, os.WriteFile(path, content, 0o600)) //nolint:gosec // the test's own temp dir
		require.NoError(t, os.Chtimes(path, at, at))
	}

	meta := func(id, extra string) []byte {
		return []byte(`{"type":"session_meta","payload":{"id":"` + id + `","cwd":"/srv/x"` + extra + `}}` + "\n")
	}

	write(name, data, now)
	write("rollout-old.jsonl", data, now.Add(-2*time.Hour))
	write("rollout-broken.jsonl", []byte("not json\n"), now)
	write("notes.txt", data, now)
	write("rollout-2026-11-09T11-30-00-long.jsonl", meta("long", `,"pad":"`+strings.Repeat("x", 200<<10)+`"`), now)
	write("rollout-2026-11-09T11-31-00-huge.jsonl", meta("huge", `,"pad":"`+strings.Repeat("x", maxMetaBytes)+`"`), now)
	write("rollout-unstamped.jsonl", meta("unstamped", ""), now)
	write("rollout-2026-11-09T11-32-00-sub.jsonl", meta("sub", `,"source":{"subAgent":"review"}`), now)

	got := map[string]Thread{}
	for _, th := range newRollouts(home).threads([]time.Time{from}, now) {
		got[th.ID] = th
	}

	require.Len(t, got, 2, "the thread of the fixture, and the long first line's")
	assert.Equal(t, dir, got[thread].Cwd)
	assert.Equal(t, time.Date(2026, 11, 9, 5, 17, 28, 0, time.UTC).Unix(), got[thread].CreatedAt,
		"started when its first line says, whenever it was last written")
	assert.Equal(t, now.Unix(), got[thread].UpdatedAt)
	assert.Equal(t, time.Date(2026, 11, 9, 11, 30, 0, 0, time.Local).Unix(), got["long"].CreatedAt, //nolint:gosmopolitan // as Codex names them
		"without a time in its first line, started when its name says")
}

func TestRolloutsLookInEachStartsOwnDays(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local) //nolint:gosmopolitan // Codex's folders are local days

	day := filepath.Join(home, "sessions", "2026", "11", "20")
	require.NoError(t, os.MkdirAll(day, 0o700))

	path := filepath.Join(day, "rollout-2026-11-20T11-59-00-today.jsonl")
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"type":"session_meta","payload":{"id":"today","cwd":"/srv/x","timestamp":"2026-11-20T11:59:00Z"}}`+"\n"), 0o600))
	require.NoError(t, os.Chtimes(path, now, now))

	starts := []time.Time{now.AddDate(0, 0, -10), now.Add(-time.Minute)}

	got := newRollouts(home).threads(starts, now)
	require.Len(t, got, 1, "a start long ago does not hide the days of a later one")
	assert.Equal(t, "today", got[0].ID)
}
