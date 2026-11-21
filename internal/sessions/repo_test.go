package sessions_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// repoWorktree is the worktree a repository's launch made for the test's sessions.
const repoWorktree = "deps-20261109"

// repoRecord is a Claude session a repository's launch started on acme-org/example-repo,
// in the worktree it made: no item, kind, or number, its title its prompt's name and its
// URL the repository's page.
func repoRecord(id string) sessions.Record {
	r := record(id, 1)
	r.ItemID, r.Kind, r.Number, r.Worktree, r.Prompt = 0, "", 0, repoWorktree, "Upgrade deps"
	r.Title, r.URL = "Upgrade dependencies", "https://github.com/acme-org/example-repo"
	r.Dir = "/srv/projects/.wt/example-repo/" + repoWorktree

	return r
}

func TestARepositorysSessionIsRecordedWithNoItem(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))

	r := repoRecord("r1")
	require.NoError(t, store.Insert(t.Context(), r))

	got, err := store.Get(t.Context(), "r1")
	require.NoError(t, err)

	want := r
	want.State, want.StateAt, want.LinkedAt = sessions.StateStarting, launched(), launched()
	assert.Equal(t, want, got)
	assert.True(t, got.IsRepo())

	kept, err := store.KeptItems(t.Context())
	require.NoError(t, err)
	assert.Empty(t, kept, "no inbox item for the inbox to keep")

	for name, bad := range map[string]func(*sessions.Record){
		"a number with no kind": func(r *sessions.Record) { r.Number = 12 },
		"an item":               func(r *sessions.Record) { r.ItemID = 7 },
		"no worktree":           func(r *sessions.Record) { r.Worktree = "" },
		"a kind with no number": func(r *sessions.Record) { r.Kind = item.KindIssue },
	} {
		r := repoRecord("bad")
		bad(&r)
		require.ErrorIs(t, store.Insert(t.Context(), r), sessions.ErrInvalidRecord, name)
	}
}

func TestARepositorysSessionOffersNoResume(t *testing.T) {
	t.Parallel()

	r := repoRecord("r1")
	r.State, r.EndedAt = sessions.StateEnded, launched().Add(time.Hour)

	s := r.Session("/srv")
	assert.Equal(t, "~/projects/.wt/example-repo/"+repoWorktree, s.Dir)
	assert.Equal(t, repoWorktree, s.Worktree)
	assert.Zero(t, s.ItemID)
	assert.False(t, s.CanResume, "the launcher resumes a session on its item, and this one has none")
	assert.True(t, s.IsRepo())

	item := record("s1", 12)
	item.State, item.EndedAt = sessions.StateEnded, launched().Add(time.Hour)
	assert.True(t, item.Session("").CanResume, "an item's still does")
}

func TestSessionLabels(t *testing.T) {
	t.Parallel()

	r := repoRecord("r1").Session("")
	assert.Equal(t, "example-repo · Upgrade dependencies", r.Label())
	assert.Equal(t, "acme-org/example-repo · Upgrade dependencies", r.FullLabel())

	r.Title = "  Upgrade\tdependencies  "
	assert.Equal(t, "example-repo · Upgrade dependencies", r.Label(), "its spaces made one")

	r.Title = ""
	assert.Equal(t, "example-repo", r.Label(), "the repository alone with no title")

	s := record("s1", 7).Session("")
	assert.False(t, s.IsRepo())
	assert.Equal(t, "example-repo #7", s.Label())
	assert.Equal(t, "acme-org/example-repo #7", s.FullLabel())

	bare := sessions.Session{Repo: "no-owner", Number: 3}
	assert.Equal(t, "no-owner #3", bare.Label(), "an item's even with no kind given")
}

func TestARepositorysSessionsAreLeftOutOfWhatCameOfItems(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	require.NoError(t, store.Insert(t.Context(), repoRecord("r1")))

	at := launched().Add(time.Minute)
	name := repo.Name{Owner: "acme-org", Repo: "example-repo"}

	for _, ref := range []item.Ref{{Repo: name}, {Repo: name, Number: 12, Kind: item.KindPR}} {
		changed, err := store.NoteGitHub(t.Context(), sessions.GitHubNote{Ref: ref, Event: sessions.GitHubMerged, At: at})
		require.NoError(t, err)
		assert.False(t, changed, "%+v", ref)
	}

	got, err := store.Get(t.Context(), "r1")
	require.NoError(t, err)
	assert.Equal(t, sessions.GitHubNone, got.GitHubEvent)
}

func TestPruneKeepsEachRepositorysNewestSessions(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	now := launched().Add(time.Hour)

	for i := range sessions.KeepPerItem + 2 {
		r := repoRecord("r-" + string(rune('a'+i)))
		r.LaunchedAt = now.Add(-time.Duration(sessions.KeepPerItem+2-i) * time.Minute)
		require.NoError(t, store.Insert(t.Context(), r))
		endAt(t, store, r.ID, now)
	}

	// An item's session in the same repository is counted apart.
	require.NoError(t, store.Insert(t.Context(), record("item", 12)))
	endAt(t, store, "item", now)

	forgot, err := store.Prune(t.Context(), now)
	require.NoError(t, err)
	assert.Equal(t, int64(2), forgot, "the repository's oldest past its newest 20")

	for _, id := range []string{"r-a", "r-b"} {
		_, err = store.Get(t.Context(), id)
		require.ErrorIs(t, err, sessions.ErrNotFound, id)
	}

	_, err = store.Get(t.Context(), "item")
	require.NoError(t, err)
}

func TestTheLedgerFindsARepositorysSessionInItsWorktree(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	store := sessions.NewStore(db)
	require.NoError(t, store.Insert(t.Context(), repoRecord("r1")))

	running, err := sessions.NewLedger(sessions.Config{Store: store}).Running(t.Context(),
		"/srv/projects/.wt/example-repo/"+repoWorktree)
	require.NoError(t, err)
	require.Len(t, running, 1, "so a launch there waits for it")
	assert.Equal(t, "r1", running[0].ID)
	assert.Zero(t, running[0].Number)
}

func TestWatcherRereadsNothingForARepositorysSession(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		w, reread := rereading(t)
		w.run(t)

		r := repoRecord("s1")
		r.LaunchedAt = time.Now()

		w.claude.set("s1", sessions.StateWorking)
		require.NoError(t, w.ledger.Start(t.Context(), r))
		synctest.Wait()

		w.act()
		w.report(sessions.StateIdle)
		assert.Equal(t, sessions.StateIdle, w.state(t, "s1"), "it is followed as any session")
		assert.Empty(t, reread.heard(), "no item to read again, whatever its agent did on GitHub")
	})
}
