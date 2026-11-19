package inbox

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/github/githubtest"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/trust"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// watchCount returns how many watch requests the fake client was sent.
func (h *watchHarness) watchCount() int {
	h.client.mu.Lock()
	defer h.client.mu.Unlock()

	return len(h.client.watchCalls)
}

func TestItemStatesReadWhatIsStoredOfEachItem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h, ids := routinesHarness(t)
		defer h.stop()

		listedAt := h.clock.Now().UTC()

		// A routine's test stores #9, which neither a list nor a search holds.
		h.clock.Advance(time.Minute)
		h.client.watches[otherQuery] = answer(watched(dependabot(9), headA, "SUCCESS"))
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "SUCCESS")

		tested, _, err := NewRoutines(h.svc).SearchNow(t.Context(), []string{otherQuery})
		require.NoError(t, err)
		require.Len(t, tested[otherQuery].IDs, 1)

		ids[9] = tested[otherQuery].IDs[0]
		testedAt := h.clock.Now().UTC()

		// Another service reads #7 merged.
		h.clock.Advance(time.Minute)

		ref := dependabot(7).Ref()
		require.NoError(t, h.notes.NoteStates(t.Context(), h.notes.StartRead(), []item.Ref{ref},
			map[string]github.ItemState{ref.Key(): {State: github.StateMerged, ClosedAt: h.clock.Now()}}))

		got, err := h.store().ItemStates(t.Context(), []int64{ids[1], ids[7], ids[9], 999})
		require.NoError(t, err)
		require.Len(t, got, 3, "an id with no stored item is left out")

		assert.Equal(t, StoredState{Ref: pr(1, "OWNER", false).Ref(), FetchedAt: listedAt, Listed: true, Watched: true},
			got[ids[1]], "a listed item a search also returns; no read has said its state")
		assert.Equal(t, StoredState{Ref: ref, State: github.StateMerged, FetchedAt: h.clock.Now().UTC(), Watched: true},
			got[ids[7]], "the state the newest read said, stamped then")
		assert.Equal(t, StoredState{Ref: dependabot(9).Ref(), State: github.StateOpen, FetchedAt: testedAt},
			got[ids[9]], "a test's result is in no list, and watched by no search")

		none, err := h.store().ItemStates(t.Context(), nil)
		require.NoError(t, err)
		assert.NotNil(t, none)
		assert.Empty(t, none)
	})
}

func TestReadStatusSaysWhenTheInboxLastReadWhatTheRoutinesWatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		before, err := h.store().ReadStatus(t.Context())
		require.NoError(t, err)
		assert.Equal(t, ReadStatus{Searches: map[string]SearchRead{}}, before, "nothing read yet")

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		read := h.clock.Now().UTC()

		got, err := h.store().ReadStatus(t.Context())
		require.NoError(t, err)
		assert.Equal(t, ReadStatus{
			Viewer: octoUser, LastRefresh: read, WatchedAt: read,
			Searches: map[string]SearchRead{depQuery: {Answered: true, At: read}},
		}, got)

		// The search fails, and a new one is truncated.
		h.next()
		h.source.set(Watch{Key: depQuery, Query: depQuery, Follow: true}, Watch{Key: otherQuery, Query: otherQuery})
		delete(h.client.watches, depQuery)
		h.client.watches[otherQuery] = github.WatchResult{
			IssueCount: 150, Items: []github.WatchItem{watched(dependabot(9), headA, "SUCCESS")},
		}
		h.client.details[nodeID(9)] = watchedPRDetails(headA, "SUCCESS")
		h.refresh(t)
		synctest.Wait()

		again := h.clock.Now().UTC()

		got, err = h.store().ReadStatus(t.Context())
		require.NoError(t, err)
		assert.Equal(t, again, got.LastRefresh)
		assert.Equal(t, again, got.WatchedAt)
		assert.Equal(t, SearchRead{Answered: true, At: read, Error: uierr.Message(github.ErrSearchFailed)},
			got.Searches[depQuery], "a search that failed keeps its last answer's time, and says why")
		assert.Equal(t, SearchRead{Answered: true, At: again, Truncated: true}, got.Searches[otherQuery])

		// Under a quarter of the budget, the refresh sends no search, and says why.
		h.next()

		low := fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
		low.RateLimit.Remaining, low.RateLimit.Used = 1000, 4000
		h.client.inbox = low
		h.refresh(t)
		synctest.Wait()

		got, err = h.store().ReadStatus(t.Context())
		require.NoError(t, err)
		assert.Equal(t, PausedLowBudget, got.Paused)
		assert.Equal(t, h.clock.Now().UTC(), got.WatchedAt, "the paused pass stores its state all the same")
		assert.Equal(t, SearchRead{Answered: true, At: again, Truncated: true}, got.Searches[otherQuery],
			"and keeps each search's last answer")
	})
}

func TestReadStatusSaysWhenTheLastRefreshFoundNoOneSignedIn(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name      string
		err       error
		signedOut bool
	}{
		{"GitHub refused the token", githubtest.BadCredentials(), true},
		{"the GitHub CLI is signed out", github.ErrNotLoggedIn, true},
		{"the GitHub CLI isn't there", uierr.New("Install the GitHub CLI.", github.ErrGHNotFound), true},
		{"the network is down", errFakeNetwork, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

			h.clock.Advance(DefaultInterval)
			h.tokens.Fail(c.err)

			_, err := h.svc.Refresh(t.Context())
			require.Error(t, err)

			got, err := h.store().ReadStatus(t.Context())
			require.NoError(t, err)
			assert.Equal(t, c.signedOut, got.SignedOut)
			assert.Equal(t, octoUser, got.Viewer, "the account last signed in is kept")

			h.tokens.Fail(nil)
			h.clock.Advance(DefaultInterval)
			h.refresh(t)

			got, err = h.store().ReadStatus(t.Context())
			require.NoError(t, err)
			assert.False(t, got.SignedOut, "the next refresh that stores clears it")
		})
	}
}

func TestWatchNowReadsANewSearchAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		routines := NewRoutines(h.svc)

		h.source.set(Watch{Key: depQuery, Query: depQuery, Follow: true})
		h.client.details[nodeID(7)] = watchedPRDetails(headA, "SUCCESS")
		h.client.watches = map[string]github.WatchResult{depQuery: answer(watched(dependabot(7), headA, "SUCCESS"))}

		require.NoError(t, routines.WatchNow(t.Context()))
		synctest.Wait()
		assert.Zero(t, h.watchCount(), "nothing is sent before an account signed in")

		_, ran := routines.LastWatchPass()
		assert.False(t, ran, "and no pass ran")

		// The first refresh went out before the account was stored, so it sent no search.
		h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
		synctest.Wait()
		require.Zero(t, h.watchCount())

		h.clock.Advance(time.Minute)
		require.NoError(t, routines.WatchNow(t.Context()))

		assert.Equal(t, 1, h.watchCount(), "the routines' searches go out at once")
		assert.Equal(t, []int{7}, h.watchesOf(t), "and what they watch is stored when it returns")

		last, ran := routines.LastWatchPass()
		require.True(t, ran)
		assert.Equal(t, h.clock.Now().UTC(), last.At)
		assert.Equal(t, 1, last.Searches)
		assert.Equal(t, 1, last.Changed)

		status, err := h.store().ReadStatus(t.Context())
		require.NoError(t, err)
		assert.True(t, status.Searches[depQuery].Answered)
	})
}

func TestWatchNowSendsNothingWhileAPassRuns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newWatchHarness(t)
		defer h.stop()

		routines := NewRoutines(h.svc)

		h.startWatching(t)
		h.refresh(t)
		synctest.Wait()

		sent := h.watchCount()

		h.next()
		h.client.mu.Lock()
		h.client.watchGate = newGate(t)
		h.client.mu.Unlock()

		since := h.clock.Now()
		waiting := make(chan error, 1)

		go func() { waiting <- routines.WatchNow(t.Context()) }()

		h.client.watchGate.waitArrived(t)

		require.NoError(t, routines.WatchNow(t.Context()), "a pass is running, so the second returns at once")

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		_, err := routines.AwaitPass(ctx, since)
		require.ErrorIs(t, err, context.DeadlineExceeded, "what waits for the pass gives up with its context")

		h.client.watchGate.release()
		require.NoError(t, <-waiting, "the first returns once its pass stored")
		synctest.Wait()
		assert.Equal(t, sent+1, h.watchCount(), "and the second sent nothing of its own")

		last, ran := routines.LastWatchPass()
		require.True(t, ran)
		assert.Equal(t, since.UTC(), last.At)
	})
}

func TestBotPullRequestsCountTheListedPullRequestsBotsOpened(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	since := time.Date(2025, 12, 1, 0, 0, 0, 0, time.UTC)

	none, err := h.store().BotPullRequests(t.Context(), since)
	require.NoError(t, err)
	assert.Equal(t, BotCounts{}, none, "with no one signed in, none")

	bot := func(it github.Item, login string) github.Item {
		it.AuthorLogin, it.AuthorType = login, "Bot"

		return it
	}

	old := dependabot(4)
	old.CreatedAt = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	// #10 is a Dependabot pull request that left the lists.
	h.seed(t, []github.Item{dependabot(10)}, nil, nil)
	h.clock.Advance(DefaultInterval)

	h.seed(t,
		[]github.Item{dependabot(2), old, pr(5, "MEMBER", false)},
		[]github.Item{bot(dependabot(8), "dependabot[bot]")},
		[]github.Item{bot(pr(3, "NONE", false), "renovate"), bot(issue("example-repo", 6, "NONE"), "renovate")},
	)

	got, err := h.store().BotPullRequests(t.Context(), since)
	require.NoError(t, err)
	assert.Equal(t, BotCounts{Dependabot: 2, Bots: 2}, got,
		"Dependabot's #2 and #8 in any list; the bots' #2 and #3 that ask for the user's review or are assigned")

	got, err = h.store().BotPullRequests(t.Context(), dependabot(3).CreatedAt)
	require.NoError(t, err)
	assert.Equal(t, BotCounts{Dependabot: 1, Bots: 1}, got, "only those opened since")
}

func TestFactsEnvReadsTheAccountTrustAndSensitiveSettings(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	now := h.clock.Now().Add(time.Hour)

	isBot := func(env facts.Env, login string) bool {
		return env.Trust.Classify(trust.Author{Login: login, Type: "Bot"}, true, false) == trust.ClassBot
	}

	env, err := h.store().FactsEnv(t.Context(), now)
	require.NoError(t, err)
	assert.Equal(t, now, env.Now)
	assert.Empty(t, env.Viewer, "no one signed in yet")
	assert.True(t, isBot(env, "dependabot"), "the default bots are trusted while the setting is unset")
	assert.Equal(t, sensitive.DefaultConfig(), env.Sensitive)

	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.setPrefs(t, map[string]string{
		trust.PrefBots:         trust.EncodeBots([]string{"acme-bot"}),
		sensitive.PrefScope:    string(sensitive.ScopeOutside),
		sensitive.PrefPatterns: `["deploy/**"]`,
	})

	env, err = h.store().FactsEnv(t.Context(), now)
	require.NoError(t, err)
	assert.Equal(t, octoUser, env.Viewer)
	assert.True(t, isBot(env, "acme-bot"), "the user's trusted bots")
	assert.False(t, isBot(env, "dependabot"), "and only them")
	assert.Equal(t, sensitive.ScopeOutside, env.Sensitive.Scope)
	assert.Equal(t, []string{"deploy/**"}, env.Sensitive.Patterns)

	h.setPrefs(t, map[string]string{trust.PrefBots: trust.EncodeBots(nil)})

	env, err = h.store().FactsEnv(t.Context(), now)
	require.NoError(t, err)
	assert.False(t, isBot(env, "dependabot"), "an empty list trusts no bot")
}

func TestTriageActionValidKnowsTheFourSteps(t *testing.T) {
	t.Parallel()

	for _, a := range []TriageAction{TriagePin, TriageSnooze, TriageDismiss, TriageRestore} {
		assert.True(t, a.Valid(), a)
	}

	for _, a := range []TriageAction{"", "archive", "Pin", " pin"} {
		assert.False(t, a.Valid(), "%q", a)
	}
}

func TestTheAutopilotReadsReportAnUnreadableDatabase(t *testing.T) {
	t.Parallel()

	s := NewStore(testutil.ClosedDB(t), nil)
	ctx := t.Context()

	_, err := s.ItemStates(ctx, []int64{1})
	require.ErrorContains(t, err, "read the items' states")

	_, err = s.ReadStatus(ctx)
	require.ErrorContains(t, err, "read the inbox status")

	_, err = s.BotPullRequests(ctx, time.Time{})
	require.ErrorContains(t, err, "read the signed-in account")

	_, err = s.FactsEnv(ctx, time.Time{})
	require.ErrorContains(t, err, "read the inbox status")
}

func TestBotPullRequestsReportACountThatFails(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{dependabot(2)}, nil, nil)
	h.exec(t, "DROP TABLE items")

	_, err := h.store().BotPullRequests(t.Context(), time.Time{})
	require.ErrorContains(t, err, "count the bots' pull requests")
}
