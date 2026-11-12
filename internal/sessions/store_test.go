package sessions_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// launched is when the test sessions were launched.
func launched() time.Time {
	return time.Date(2026, 11, 9, 5, 11, 0, 0, time.UTC)
}

// record is a Claude session on pull request number in acme-org/example-repo.
func record(id string, number int) sessions.Record {
	return sessions.Record{
		ID: id, AgentSessionID: id, Agent: agent.Claude, Profile: "default", Prompt: "Review", PromptHead: "Review it",
		Terminal: "iterm2", ItemID: int64(number), Owner: "acme-org", Name: "example-repo", Kind: item.KindPR,
		Number: number, Title: "Fix the widget", URL: "https://github.com/acme-org/example-repo/pull/12",
		Dir: "/srv/wt/example-repo/pr-12", ClonePath: "/srv/projects/example-repo", Worktree: "pr-12",
		WorktreeAction: sessions.WorktreeCreate, Permission: "full access", LaunchedAt: launched(),
	}
}

func TestInsertAndRead(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))

	r := record("s1", 12)
	require.NoError(t, store.Insert(t.Context(), r))

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)

	want := r
	want.State, want.StateAt, want.LinkedAt = sessions.StateStarting, launched(), launched()
	assert.Equal(t, want, got, "a session the agent already named is linked at once")

	codex := record("s2", 13)
	codex.Agent, codex.AgentSessionID = agent.Codex, ""
	require.NoError(t, store.Insert(t.Context(), codex))

	got, err = store.Get(t.Context(), "s2")
	require.NoError(t, err)
	assert.True(t, got.LinkedAt.IsZero(), "not linked until its thread is found")

	stale := record("s3", 14)
	stale.WorktreeAction = "example-action"
	stale.Notice = "Couldn't fetch origin/main, so the worktree starts from the last fetched copy."
	require.NoError(t, store.Insert(t.Context(), stale))

	got, err = store.Get(t.Context(), "s3")
	require.NoError(t, err)
	assert.Equal(t, sessions.WorktreeNone, got.WorktreeAction, "an action this build doesn't know reads as none")
	assert.Equal(t, stale.Notice, got.Notice)

	_, err = store.Get(t.Context(), "missing")
	require.ErrorIs(t, err, sessions.ErrNotFound)

	for name, bad := range map[string]func(*sessions.Record){
		"no id":         func(r *sessions.Record) { r.ID = "" },
		"unknown agent": func(r *sessions.Record) { r.Agent = "example-agent" },
		"no number":     func(r *sessions.Record) { r.Number = 0 },
		"relative dir":  func(r *sessions.Record) { r.Dir = "pr-12" },
		"no time":       func(r *sessions.Record) { r.LaunchedAt = time.Time{} },
	} {
		r := record("bad", 14)
		bad(&r)
		require.ErrorIs(t, store.Insert(t.Context(), r), sessions.ErrInvalidRecord, name)
	}
}

func TestApplyStoresWhatChanged(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	r := record("s1", 12)
	r.AgentSessionID = ""
	require.NoError(t, store.Insert(t.Context(), r))

	at := launched().Add(time.Minute)

	changes, err := store.Apply(t.Context(), at, []sessions.Observation{{
		ID: "s1", AgentSessionID: "thread-1", State: sessions.StateWorking, PID: 4100,
		LastMessage: "  " + strings.Repeat("a", sessions.MaxMessageRunes+10) + "  ",
	}, {ID: "gone", State: sessions.StateIdle}})
	require.NoError(t, err)
	require.Len(t, changes, 1, "an observation of a session no longer held is dropped")

	c := changes[0]
	assert.True(t, c.StateChanged())
	assert.Equal(t, sessions.StateStarting, c.Before.State)
	assert.Equal(t, sessions.StateWorking, c.After.State)
	assert.Equal(t, "thread-1", c.After.AgentSessionID)
	assert.Equal(t, at, c.After.LinkedAt)
	assert.Equal(t, at, c.After.StateAt)
	assert.Equal(t, 4100, c.After.PID)
	assert.Len(t, []rune(c.After.LastMessage), sessions.MaxMessageRunes)

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, c.After, got)

	later := at.Add(time.Minute)

	changes, err = store.Apply(t.Context(), later, []sessions.Observation{{ID: "s1", State: sessions.StateWorking}})
	require.NoError(t, err)
	assert.Empty(t, changes, "nothing changed")

	changes, err = store.Apply(t.Context(), later, []sessions.Observation{{ID: "s1", AgentSessionID: "thread-2"}})
	require.NoError(t, err)
	assert.Empty(t, changes, "a session stays linked to its first thread")

	changes, err = store.Apply(t.Context(), later, []sessions.Observation{{ID: "s1", LastMessage: "Done."}})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.False(t, changes[0].StateChanged())
	assert.Equal(t, at, changes[0].After.StateAt, "a new message is not a new state")

	end := later.Add(time.Minute)

	changes, err = store.Apply(t.Context(), end, []sessions.Observation{{ID: "s1", State: sessions.StateEnded}})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, end, changes[0].After.EndedAt)
	assert.Equal(t, "Done.", changes[0].After.LastMessage)

	changes, err = store.Apply(t.Context(), end.Add(time.Minute), []sessions.Observation{{ID: "s1", State: sessions.StateIdle}})
	require.NoError(t, err)
	assert.Empty(t, changes, "an ended session stays ended")
}

func TestLiveAndRecent(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))

	older := record("old", 1)
	older.LaunchedAt = launched().Add(-10 * 24 * time.Hour)
	running := record("running", 2)
	running.LaunchedAt = launched().Add(-20 * 24 * time.Hour)
	recent := record("recent", 3)

	for _, r := range []sessions.Record{older, running, recent} {
		require.NoError(t, store.Insert(t.Context(), r))
	}

	_, err := store.Apply(t.Context(), launched(), []sessions.Observation{
		{ID: "old", State: sessions.StateEnded}, {ID: "recent", State: sessions.StateEnded},
	})
	require.NoError(t, err)

	live, err := store.Live(t.Context())
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, "running", live[0].ID)

	got, err := store.Recent(t.Context(), launched().Add(-sessions.RecentWindow))
	require.NoError(t, err)
	require.Len(t, got, 2, "every running session, and those launched in the window")
	assert.Equal(t, "recent", got[0].ID, "newest first")
	assert.Equal(t, "running", got[1].ID)
}

func TestPrune(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	now := launched().Add(40 * 24 * time.Hour)

	ancient := record("ancient", 1)
	require.NoError(t, store.Insert(t.Context(), ancient))
	_, err := store.Apply(t.Context(), launched(), []sessions.Observation{{ID: "ancient", State: sessions.StateEnded}})
	require.NoError(t, err)

	for i := range sessions.KeepPerItem + 3 {
		r := record("busy-"+string(rune('a'+i)), 2)
		r.LaunchedAt = now.Add(-time.Duration(sessions.KeepPerItem+3-i) * time.Minute)
		require.NoError(t, store.Insert(t.Context(), r))

		if i != 0 {
			_, err = store.Apply(t.Context(), now, []sessions.Observation{{ID: r.ID, State: sessions.StateEnded}})
			require.NoError(t, err)
		}
	}

	forgot, err := store.Prune(t.Context(), now)
	require.NoError(t, err)
	assert.Equal(t, int64(1+2), forgot, "the session ended long ago, and the item's oldest ended past its newest 20")

	_, err = store.Get(t.Context(), "busy-a")
	require.NoError(t, err, "a running session is always kept, however old")

	recent, err := store.Recent(t.Context(), now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Len(t, recent, sessions.KeepPerItem+1)
}

func TestRecordSession(t *testing.T) {
	t.Parallel()

	r := record("s1", 12)
	r.Dir = "/srv/home/projects/.wt/pr-12"
	r.State, r.PID, r.StateAt = sessions.StateIdle, 4100, launched().Add(time.Minute)

	s := r.Session("/srv/home")
	assert.Equal(t, "~/projects/.wt/pr-12", s.Dir)
	assert.Equal(t, sessions.WorktreeCreate, s.WorktreeAction)
	assert.Equal(t, "full access", s.Permission)
	assert.Empty(t, s.Notice)
	assert.Equal(t, "acme-org/example-repo", s.Repo)
	assert.Equal(t, "2026-11-09T05:12:00Z", s.StateAt)
	assert.Equal(t, "2026-11-09T05:11:00Z", s.LaunchedAt)
	assert.Empty(t, s.EndedAt)
	assert.True(t, s.CanFocus)
	assert.False(t, s.CanResume)

	r.State, r.EndedAt = sessions.StateEnded, launched().Add(time.Hour)
	s = r.Session("/srv/home")
	assert.False(t, s.CanFocus)
	assert.True(t, s.CanResume)

	r.AgentSessionID = ""
	assert.False(t, r.Session("").CanResume, "nothing to resume without the agent's ID")
}

func TestStates(t *testing.T) {
	t.Parallel()

	for _, s := range sessions.States() {
		assert.True(t, s.Valid())
		assert.NotEmpty(t, s.Phrase())
		assert.Equal(t, s != sessions.StateEnded, s.Live(), s)
		assert.Equal(t, s == sessions.StateNeedsApproval || s == sessions.StateNeedsInput, s.NeedsYou(), s)
	}

	assert.False(t, sessions.State("later").Valid())
	assert.Equal(t, "needs approval", sessions.StateNeedsApproval.Phrase())
}

func TestEndedSinceIsMeasuredFromTheEnd(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	old, recent, unlinked := record("old", 12), record("recent", 12), record("unlinked", 12)
	unlinked.AgentSessionID = ""

	for _, r := range []sessions.Record{old, recent, unlinked} {
		require.NoError(t, store.Insert(t.Context(), r))
	}

	_, err := store.Apply(t.Context(), launched().Add(time.Hour), []sessions.Observation{{ID: "old", State: sessions.StateEnded}})
	require.NoError(t, err)

	_, err = store.Apply(t.Context(), launched().Add(48*time.Hour), []sessions.Observation{
		{ID: "recent", State: sessions.StateEnded}, {ID: "unlinked", State: sessions.StateEnded},
	})
	require.NoError(t, err)

	got, err := store.EndedSince(t.Context(), launched().Add(47*time.Hour))
	require.NoError(t, err)
	require.Len(t, got, 1, "launched two days ago, ended an hour ago; the one the agent never named is left out")
	assert.Equal(t, "recent", got[0].ID)
}

func TestInsertUnlessLive(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("launched", 12)))

	live, err := store.ConversationLive(t.Context(), "launched")
	require.NoError(t, err)
	assert.True(t, live)

	live, err = store.ConversationLive(t.Context(), "")
	require.NoError(t, err)
	assert.False(t, live, "no conversation has no ID")

	revived := record("revived", 12)
	revived.AgentSessionID = "launched"

	inserted, err := store.InsertUnlessLive(t.Context(), revived)
	require.NoError(t, err)
	assert.False(t, inserted, "a running session already carries on the conversation")

	_, err = store.Get(t.Context(), "revived")
	require.ErrorIs(t, err, sessions.ErrNotFound)

	_, err = store.Apply(t.Context(), launched(), []sessions.Observation{{ID: "launched", State: sessions.StateEnded}})
	require.NoError(t, err)

	inserted, err = store.InsertUnlessLive(t.Context(), revived)
	require.NoError(t, err)
	assert.True(t, inserted, "once that one ended")
}

func TestApplyRelinksOnlyWhenTheProcessMovedOn(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	_, err := store.Apply(t.Context(), launched(), []sessions.Observation{{ID: "s1", AgentSessionID: "other"}})
	require.NoError(t, err)

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "s1", got.AgentSessionID, "a linked session keeps its conversation")

	_, err = store.Apply(t.Context(), launched().Add(time.Minute), []sessions.Observation{
		{ID: "s1", AgentSessionID: "cleared", Relinked: true, State: sessions.StateWorking},
	})
	require.NoError(t, err)

	got, err = store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, "cleared", got.AgentSessionID, "its process moved on to another conversation")
	assert.Equal(t, launched().Add(time.Minute), got.LinkedAt)
}

func TestApplyTakesOnlyAProcessIDThatCanBeOne(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	_, err := store.Apply(t.Context(), launched(), []sessions.Observation{{ID: "s1", PID: 1<<32 - 1}})
	require.NoError(t, err)

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Zero(t, got.PID, "a PID the kernel would cut to -1 is not taken")
}
