package sessions_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// prRef names pull request number in acme-org/example-repo, as record's sessions do.
func prRef(number int) item.Ref {
	return item.Ref{Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: number, Kind: item.KindPR}
}

// noteOn is event at commit on pull request number, at.
func noteOn(number int, ev sessions.GitHubEvent, commit string, at time.Time) sessions.GitHubNote {
	return sessions.GitHubNote{Ref: prRef(number), Event: ev, Commit: commit, At: at}
}

// endAt ends the session with id at.
func endAt(t *testing.T, store *sessions.Store, id string, at time.Time) {
	t.Helper()

	_, err := store.Apply(t.Context(), at, []sessions.Observation{{ID: id, State: sessions.StateEnded}})
	require.NoError(t, err)
}

func TestNoteGitHubRecordsOnTheSessionsRunningThen(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	approvedAt := launched().Add(30 * time.Minute)

	// The session that ran when the agent approved, one that had ended an hour before,
	// one launched after, and one on another pull request.
	require.NoError(t, store.Insert(t.Context(), record("running", 12)))

	ended := record("ended", 12)
	ended.LaunchedAt = launched().Add(-2 * time.Hour)
	require.NoError(t, store.Insert(t.Context(), ended))
	endAt(t, store, "ended", launched().Add(-time.Hour))

	later := record("later", 12)
	later.LaunchedAt = approvedAt.Add(time.Minute)
	require.NoError(t, store.Insert(t.Context(), later))

	require.NoError(t, store.Insert(t.Context(), record("other", 13)))

	changed, err := store.NoteGitHub(t.Context(), noteOn(12, sessions.GitHubApproved, "abc1234", approvedAt))
	require.NoError(t, err)
	assert.True(t, changed)

	got, err := store.Get(t.Context(), "running")
	require.NoError(t, err)
	assert.Equal(t, sessions.GitHubApproved, got.GitHubEvent)
	assert.Equal(t, "abc1234", got.GitHubEventCommit)
	assert.Equal(t, approvedAt, got.GitHubEventAt)

	view := got.Session("/srv")
	assert.Equal(t, sessions.GitHubApproved, view.GitHubEvent)
	assert.Equal(t, "abc1234", view.GitHubEventCommit)
	assert.Equal(t, "2026-11-09T05:41:00Z", view.GitHubEventAt)

	for _, id := range []string{"ended", "later", "other"} {
		got, err = store.Get(t.Context(), id)
		require.NoError(t, err)
		assert.Equal(t, sessions.GitHubNone, got.GitHubEvent, id)
		assert.Empty(t, got.Session("/srv").GitHubEventAt, id)
	}
}

func TestNoteGitHubCountsASessionThatEndedJustAfter(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	// The agent merged, and quit a moment later, by the Mac's clock a little ahead of
	// GitHub's.
	mergedAt := launched().Add(10 * time.Minute)
	endAt(t, store, "s1", mergedAt.Add(-5*time.Second))

	changed, err := store.NoteGitHub(t.Context(), noteOn(12, sessions.GitHubMerged, "", mergedAt))
	require.NoError(t, err)
	assert.True(t, changed)

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.GitHubMerged, got.GitHubEvent)
}

func TestNoteGitHubKeepsTheNewest(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	approvedAt := launched().Add(10 * time.Minute)
	mergedAt := approvedAt.Add(time.Minute)

	_, err := store.NoteGitHub(t.Context(), noteOn(12, sessions.GitHubMerged, "", mergedAt))
	require.NoError(t, err)

	// The approval is learned after the merge, from a read that came in later.
	changed, err := store.NoteGitHub(t.Context(), noteOn(12, sessions.GitHubApproved, "abc1234", approvedAt))
	require.NoError(t, err)
	assert.False(t, changed, "an older event never replaces a newer one")

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.GitHubMerged, got.GitHubEvent)
	assert.Equal(t, mergedAt, got.GitHubEventAt)
}

func TestNoteGitHubIgnoresCase(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	n := noteOn(12, sessions.GitHubClosed, "", launched().Add(time.Minute))
	n.Ref.Repo = repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}

	changed, err := store.NoteGitHub(t.Context(), n)
	require.NoError(t, err)
	assert.True(t, changed, "GitHub's names are the same in any case")
}

func TestNoteGitHubSkipsANoteWithNothingToSay(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	for name, n := range map[string]sessions.GitHubNote{
		"no event":      noteOn(12, sessions.GitHubNone, "", launched().Add(time.Minute)),
		"unknown event": noteOn(12, "example-event", "", launched().Add(time.Minute)),
		"no time":       noteOn(12, sessions.GitHubApproved, "abc1234", time.Time{}),
	} {
		changed, err := store.NoteGitHub(t.Context(), n)
		require.NoError(t, err, name)
		assert.False(t, changed, name)
	}
}

func TestLedgerNoteGitHubTellsTheWindowOnlyWhenItChanged(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	events := &testutil.Events{}
	ledger := sessions.NewLedger(sessions.Config{Store: store, Events: events})
	require.NoError(t, store.Insert(t.Context(), record("s1", 12)))

	// A pull request no session worked on.
	require.NoError(t, ledger.NoteGitHub(t.Context(), []sessions.GitHubNote{
		noteOn(99, sessions.GitHubMerged, "", launched().Add(time.Minute)),
	}))
	assert.Empty(t, events.All())

	require.NoError(t, ledger.NoteGitHub(t.Context(), []sessions.GitHubNote{
		noteOn(12, sessions.GitHubApproved, "abc1234", launched().Add(time.Minute)),
		noteOn(12, sessions.GitHubMerged, "", launched().Add(2*time.Minute)),
	}))
	assert.Equal(t, []event.Event{event.SessionsUpdated{}}, events.All(), "told once")

	got, err := store.Get(t.Context(), "s1")
	require.NoError(t, err)
	assert.Equal(t, sessions.GitHubMerged, got.GitHubEvent)
}
