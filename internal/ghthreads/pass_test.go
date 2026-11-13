package ghthreads

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// newThreadsFrom returns n pull request threads from the ID id on, each about its own pull
// request, the newest updated at newest and each a minute older than the last.
func newThreadsFrom(id int64, n int, newest time.Time) []fakeThread {
	out := make([]fakeThread, 0, n)

	for i := range n {
		out = append(out, prThread(id+int64(i), int(id-firstID)+i+1, newest.Add(-time.Duration(i)*time.Minute)))
	}

	return out
}

// pagesUpTo returns 1 to n.
func pagesUpTo(n int) []int {
	out := make([]int, 0, n)

	for i := 1; i <= n; i++ {
		out = append(out, i)
	}

	return out
}

// otherAccountRow stores a thread of otherLogin's, last seen at seen.
func otherAccountRow(t *testing.T, r *rig, id int64, seen time.Time) {
	t.Helper()

	testutil.Exec(t, r.db, `INSERT INTO gh_threads (account_login, thread_id, reason, unread, updated_at, subject_type,
		subject_title, repo_owner, repo_name, item_kind, number, seen_at) VALUES (?, ?, 'mention', 1, ?, 'Issue',
		'Another account''s issue', 'acme-org', 'example-repo', 'issue', ?, ?)`, otherLogin, id, seen, id-firstID+900, seen)
}

func TestPassNotModifiedReadsNothingMore(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(60, start))

	got := r.mustPass()
	assert.Equal(t, []int{1, 2}, pagesOf(got), "the first pass walks every page")
	assert.Empty(t, listings(got)[0].ifNoneMatch, "with no ETag")

	before, ok := r.row(viewerLogin, firstID)
	require.True(t, ok)

	etag := r.state().ETag
	require.NotEmpty(t, etag)

	r.clock.Advance(5 * time.Minute)

	got = r.mustPass()
	assert.Equal(t, []listedRequest{{kind: "list", page: 1, ifNoneMatch: etag}}, got,
		"page 1 sent with its ETag, answered 304, and nothing more read or checked")

	after, ok := r.row(viewerLogin, firstID)
	require.True(t, ok)
	assert.True(t, before.seenAt.Equal(after.seenAt), "no row written")
	assert.True(t, r.state().LastPass.Equal(r.clock.Now()), "only when the pass went through")
	assert.Equal(t, etag, r.state().ETag)
}

func TestPassStopsAtHighWater(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(120, start))

	assert.Equal(t, []int{1, 2, 3}, pagesOf(r.mustPass()))
	assert.True(t, r.state().HighWater.Equal(start), "the newest update read")

	// Two threads further down get new activity, and move up to page 1, whose oldest is
	// still older than the mark: nothing below it changed.
	r.clock.Advance(5 * time.Minute)
	r.gh.update(firstID+70, start.Add(time.Minute))
	r.gh.update(firstID+110, start.Add(2*time.Minute))

	got := r.mustPass()
	assert.Equal(t, []int{1}, pagesOf(got), "page 1 reached the mark")
	assert.NotEmpty(t, listings(got)[0].ifNoneMatch)
	assert.True(t, r.state().HighWater.Equal(start.Add(2*time.Minute)))

	moved, ok := r.row(viewerLogin, firstID+110)
	require.True(t, ok)
	assert.True(t, moved.seenAt.Equal(r.clock.Now()), "the moved thread is stored")

	// Sixty new threads: page 1 is all newer than the mark, and page 2 reaches it.
	r.clock.Advance(5 * time.Minute)

	current := make([]fakeThread, 0, 180)
	current = append(current, newThreadsFrom(firstID+500, 60, start.Add(2*time.Hour))...)
	current = append(current, prThreads(120, start)...)
	r.gh.set(current)
	r.gh.update(firstID+70, start.Add(time.Minute))
	r.gh.update(firstID+110, start.Add(2*time.Minute))

	got = r.mustPass()
	assert.Equal(t, []int{1, 2}, pagesOf(got), "page 2's oldest is at or before the mark; page 3 isn't read")
	assert.Equal(t, 180, r.rows(viewerLogin))
	assert.True(t, r.state().HighWater.Equal(start.Add(2*time.Hour)))
}

func TestFullWalkDeletesUnseenRows(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(60, start))
	r.mustPass()

	// Another account's threads: one no pass listed for longer than they are kept, one
	// listed yesterday.
	otherAccountRow(t, r, firstID+1, start.Add(-OtherAccountsKept-time.Hour))
	otherAccountRow(t, r, firstID+2, start.Add(-24*time.Hour))

	// Marked done on GitHub, on page 2: page 1 is as it was, so a 304 can't tell.
	r.gh.remove(firstID + 55)
	r.clock.Advance(5 * time.Minute)

	assert.Equal(t, []int{1}, pagesOf(r.mustPass()))

	_, ok := r.row(viewerLogin, firstID+55)
	assert.True(t, ok, "page 1 unchanged says nothing of page 2")

	r.clock.Advance(FullWalkEvery)

	got := r.mustPass()
	assert.Equal(t, []int{1, 2}, pagesOf(got), "six hours on, every page is read")
	assert.Empty(t, listings(got)[0].ifNoneMatch, "a walk of every page sends no ETag")

	_, ok = r.row(viewerLogin, firstID+55)
	assert.False(t, ok, "done on GitHub, so dropped")
	assert.Equal(t, 59, r.rows(viewerLogin))
	assert.Equal(t, 1, r.rows(otherLogin), "another account's thread unseen for 30 days is dropped, a recent one kept")

	_, ok = r.row(otherLogin, firstID+2)
	assert.True(t, ok)
	assert.True(t, r.state().LastFullWalk.Equal(r.clock.Now()))
}

func TestPassDropsThreadDoneElsewhere(t *testing.T) {
	t.Parallel()

	// Thread 51 is updated at the same moment as thread 50, so once the newest is gone, the
	// oldest on page 1 (50) and the first on page 2 (51) tie.
	threads := prThreads(120, start)
	threads[51].updated = threads[50].updated

	r := newRig(t)
	r.gh.set(threads)
	r.mustPass()

	// The newest marked done on github.com, and one on page 3 too: page 1 changed.
	r.gh.remove(firstID)
	r.gh.remove(firstID + 100)
	r.clock.Advance(5 * time.Minute)

	got := r.mustPass()
	assert.Equal(t, []int{1}, pagesOf(got), "page 1, a 200, reaches the mark")
	assert.NotEmpty(t, listings(got)[0].ifNoneMatch, "an ordinary pass, not a walk")

	_, ok := r.row(viewerLogin, firstID)
	assert.False(t, ok, "newer than the oldest thread on page 1 and not listed: done, so dropped")

	_, ok = r.row(viewerLogin, firstID+51)
	assert.True(t, ok, "updated exactly at the oldest on page 1, it may be on page 2: kept")

	_, ok = r.row(viewerLogin, firstID+100)
	assert.True(t, ok, "older than the span read: kept until a walk can tell")
	assert.Equal(t, 119, r.rows(viewerLogin))
	assert.Equal(t, 119, r.snapshot().Status.Count)

	// A 304 covers nothing, and drops nothing.
	r.clock.Advance(5 * time.Minute)

	assert.Equal(t, []int{1}, pagesOf(r.mustPass()))
	assert.Equal(t, 119, r.rows(viewerLogin))
}

func TestAPassThatReadsTheLastPageDeletesToo(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(3, start))
	r.mustPass()

	// Marked done on page 1, the only page: page 1 changed, and the pass reads to its end.
	r.gh.remove(firstID + 1)
	r.clock.Advance(5 * time.Minute)

	got := r.mustPass()
	assert.Equal(t, []int{1}, pagesOf(got))
	assert.NotEmpty(t, listings(got)[0].ifNoneMatch, "an ordinary pass")

	_, ok := r.row(viewerLogin, firstID+1)
	assert.False(t, ok, "it read every page there is")
	assert.Equal(t, 2, r.rows(viewerLogin))
	assert.True(t, r.state().LastFullWalk.Equal(r.clock.Now()), "and counts as a walk of every page")
}

// checkThreads returns n threads about a check suite, which names no item, so no pass
// checks a state: the newest updated at newest and each a minute older than the last.
func checkThreads(n int, newest time.Time) []fakeThread {
	out := prThreads(n, newest)

	for i := range out {
		out[i].subject, out[i].typ, out[i].reason = "", "CheckSuite", "ci_activity"
	}

	return out
}

func TestFullWalkStopsAtCeiling(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(checkThreads(MaxThreads+50, start))

	got := r.mustPass()
	assert.Equal(t, pagesUpTo(20), pagesOf(got), "never past the last page the app reads")

	snap := r.snapshot()
	assert.True(t, snap.Status.Truncated, "the mirror holds the newest 1,000 only")
	assert.Equal(t, MaxThreads, snap.Status.Count)
	assert.Equal(t, 1000, MaxThreads)
	assert.True(t, r.state().LastFullWalk.Equal(start), "a walk all the same")
}

func TestTruncatedOnlyWhenGitHubListsMore(t *testing.T) {
	t.Parallel()

	full := make([]github.Thread, github.ThreadsPerPage)
	for i := range full {
		full[i] = github.Thread{ID: firstID + int64(i), UpdatedAt: start.Add(-time.Duration(i) * time.Minute)}
	}

	last := &listing{seen: map[int64]bool{}}
	assert.Zero(t, last.add(github.MaxThreadPages, &github.ThreadPage{Threads: full}, time.Time{}))
	assert.True(t, last.walkedAll)
	assert.False(t, last.state.Truncated, "a full last page GitHub lists nothing after leaves nothing out")

	capped := &listing{seen: map[int64]bool{}}
	assert.Zero(t, capped.add(github.MaxThreadPages, &github.ThreadPage{Threads: full, Capped: true}, time.Time{}))
	assert.True(t, capped.walkedAll, "the last page the app reads")
	assert.True(t, capped.state.Truncated, "GitHub lists another: the mirror holds the newest only")
}

func TestPassThatFailsPartwayStoresNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(60, start))
	r.gh.configure(func(f *fakeGitHub) { f.failPage = 2 })

	_, err := r.pass()
	require.Error(t, err)

	st := r.state()
	assert.Empty(t, st.ETag, "no ETag stored")
	assert.True(t, st.HighWater.IsZero(), "no high-water mark")
	assert.Zero(t, r.rows(viewerLogin), "no rows")
	assert.NotEmpty(t, st.LastError, "but the failure, which the window shows")
	assert.Equal(t, st.LastError, r.snapshot().Status.LastError)

	r.gh.configure(func(f *fakeGitHub) { f.failPage = 0 })
	r.clock.Advance(5 * time.Minute)

	got := r.mustPass()
	assert.Empty(t, listings(got)[0].ifNoneMatch, "page 1's ETag from the failed pass was never kept")
	assert.Equal(t, []int{1, 2}, pagesOf(got))
	assert.Empty(t, r.state().LastError)

	etag, mark := r.state().ETag, r.state().HighWater

	// Sixty new threads, and page 2 fails again: the last pass's ETag and mark stand.
	current := make([]fakeThread, 0, 120)
	current = append(current, newThreadsFrom(firstID+500, 60, start.Add(time.Hour))...)
	current = append(current, prThreads(60, start)...)
	r.gh.set(current)
	r.gh.configure(func(f *fakeGitHub) { f.failPage = 2 })
	r.clock.Advance(5 * time.Minute)

	_, err = r.pass()
	require.Error(t, err)
	assert.Equal(t, etag, r.state().ETag)
	assert.True(t, r.state().HighWater.Equal(mark))
	assert.Equal(t, 60, r.rows(viewerLogin), "page 1's threads aren't stored either")

	r.gh.configure(func(f *fakeGitHub) { f.failPage = 0 })
	r.clock.Advance(5 * time.Minute)

	got = r.mustPass()
	assert.Equal(t, etag, listings(got)[0].ifNoneMatch, "the last ETag stored, never the failed pass's")
	assert.Equal(t, []int{1, 2}, pagesOf(got))
	assert.Equal(t, 120, r.rows(viewerLogin))
}

func TestRefusedListingBacksOff(t *testing.T) {
	t.Parallel()

	for status, message := range map[int]string{
		http.StatusForbidden: "Resource not accessible by personal access token",
		http.StatusNotFound:  "Not Found",
	} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			r.gh.set(prThreads(3, start))
			r.gh.configure(func(f *fakeGitHub) { f.listStatus, f.listMessage = status, message })

			_, err := r.pass()
			require.Error(t, err)
			assert.Equal(t, "Your GitHub token can't read notifications.", uierr.Message(err))

			snap := r.snapshot()
			assert.Equal(t, AccessRefused, snap.Status.Access)
			assert.Equal(t, "Your GitHub token can't read notifications.", snap.Status.LastError)
			assert.Equal(t, clock.Format(start.Add(RefusedFor)), snap.Status.AskAgainAt)
			assert.Equal(t, viewerLogin, snap.Status.Account)

			r.gh.configure(func(f *fakeGitHub) { f.listStatus = 0 })
			r.clock.Advance(RefusedFor - time.Minute)

			assertNoRequests(t, r.mustPass(), "nothing is sent for six hours")
			assert.Equal(t, AccessRefused, r.snapshot().Status.Access)

			r.clock.Advance(time.Minute)

			got := r.mustPass()
			assert.Equal(t, []int{1}, pagesOf(got), "then the listing is sent again")
			assert.Empty(t, listings(got)[0].ifNoneMatch, "walking every page, since the mirror went stale")

			snap = r.snapshot()
			assert.Equal(t, AccessOK, snap.Status.Access)
			assert.Empty(t, snap.Status.LastError)
			assert.Empty(t, snap.Status.AskAgainAt)
			assert.Equal(t, 3, snap.Status.Count)
		})
	}
}

func TestARateLimitIsNotARefusal(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(3, start))
	r.gh.configure(func(f *fakeGitHub) {
		f.listStatus, f.listMessage = http.StatusForbidden, "API rate limit exceeded for user ID 1."
	})

	_, err := r.pass()
	require.Error(t, err)
	assert.Equal(t, AccessUnchecked, r.snapshot().Status.Access, "a rate limit says to wait, not that the token can't")
	assert.NotEmpty(t, r.snapshot().Status.LastError)

	r.gh.configure(func(f *fakeGitHub) { f.listStatus = 0 })
	r.clock.Advance(time.Minute)

	assert.Equal(t, []int{1}, pagesOf(r.mustPass()), "the next pass tries again")
	assert.Equal(t, AccessOK, r.snapshot().Status.Access)
}

func TestCheckNowLiftsBackOff(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(3, start))
	r.gh.configure(func(f *fakeGitHub) { f.listStatus = http.StatusNotFound })

	_, err := r.pass()
	require.Error(t, err)

	// Still refused: CheckNow says so in the snapshot, not as an error.
	r.clock.Advance(time.Hour)

	snap, err := r.svc.CheckNow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int{1}, pagesOf(r.gh.requests()), "sent within the six hours")
	assert.Equal(t, AccessRefused, snap.Status.Access)
	assert.Equal(t, clock.Format(r.clock.Now().Add(RefusedFor)), snap.Status.AskAgainAt, "and asked again six hours on")

	// The user fixed the token, and asks again.
	r.gh.configure(func(f *fakeGitHub) { f.listStatus = 0 })
	r.clock.Advance(time.Minute)

	snap, err = r.svc.CheckNow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int{1}, pagesOf(r.gh.requests()))
	assert.Equal(t, AccessOK, snap.Status.Access)
	assert.Equal(t, 3, snap.Status.Count)
	assert.Empty(t, snap.Status.LastError)
}

func TestReadingOffSendsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(3, start))
	r.mustPass()

	loans, told := r.tokens.loans(), r.events.Count()

	r.rules.off.Store(true)
	r.clock.Advance(5 * time.Minute)

	assertNoRequests(t, r.mustPass(), "reading off: no request")
	assert.Equal(t, loans, r.tokens.loans(), "not even a token borrowed")
	assert.Equal(t, told+1, r.events.Count(), "the window hears the notifications are hidden")

	snap := r.snapshot()
	assert.False(t, snap.Status.Reading)
	assert.Zero(t, snap.Status.Count)
	assert.Zero(t, snap.Status.Unread)
	assert.NotNil(t, snap.ByItem)
	assert.Empty(t, snap.ByItem)
	assert.NotNil(t, snap.Unread)
	assert.Empty(t, snap.Unread)

	assertNoRequests(t, r.mustPass(), "nor on the next pass")
	assert.Equal(t, told+1, r.events.Count(), "which changed nothing")

	snap, err := r.svc.CheckNow(t.Context())
	require.NoError(t, err)
	assertNoRequests(t, r.gh.requests(), "nor when the user asks")
	assert.Equal(t, loans, r.tokens.loans())
	assert.False(t, snap.Status.Reading)
}

func TestReadingBackOnWalksFully(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(60, start))
	r.mustPass()
	r.clock.Advance(5 * time.Minute)

	assert.NotEmpty(t, listings(r.mustPass())[0].ifNoneMatch, "an ordinary pass sends the ETag")

	r.rules.off.Store(true)
	r.mustPass()
	assert.True(t, r.state().WalkNext)

	r.rules.off.Store(false)
	r.clock.Advance(5 * time.Minute)

	got := r.mustPass()
	assert.Empty(t, listings(got)[0].ifNoneMatch, "back on, the mirror went stale: every page is read")
	assert.Equal(t, []int{1, 2}, pagesOf(got))
	assert.False(t, r.state().WalkNext)
}

func TestFirstPassAfterStartWalksFully(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(60, start))
	r.mustPass()
	r.clock.Advance(5 * time.Minute)
	assert.NotEmpty(t, listings(r.mustPass())[0].ifNoneMatch)

	r.restart()
	r.clock.Advance(5 * time.Minute)

	got := r.mustPass()
	assert.Empty(t, listings(got)[0].ifNoneMatch, "the app's first pass walks every page")
	assert.Equal(t, []int{1, 2}, pagesOf(got))
}

func TestAccountSwitchWalksFully(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(60, start))
	r.mustPass()
	r.clock.Advance(5 * time.Minute)
	r.mustPass()

	// gh auth switch: another token, another account, its own notifications.
	r.tokens.swap("another-test-token-not-real")
	r.gh.configure(func(f *fakeGitHub) { f.viewer = otherLogin })
	r.gh.set(newThreadsFrom(firstID+700, 3, start))
	r.clock.Advance(5 * time.Minute)

	got := r.mustPass()
	require.NotEmpty(t, got)
	assert.Equal(t, "viewer", got[0].kind, "a new token's account is asked first")
	assert.Empty(t, listings(got)[0].ifNoneMatch, "the other account's ETag is never sent")
	assert.Equal(t, []int{1}, pagesOf(got))

	snap := r.snapshot()
	assert.Equal(t, otherLogin, snap.Status.Account)
	assert.Equal(t, 3, snap.Status.Count, "only this account's threads are shown")
	assert.Len(t, snap.ByItem, 3)
	assert.Equal(t, 60, r.rows(viewerLogin), "the other account's are kept a while")

	r.clock.Advance(OtherAccountsKept)
	r.mustPass()
	assert.Zero(t, r.rows(viewerLogin), "and dropped once unseen for 30 days")
}

// givingUpClient is the fake GitHub, whose listing first ends the caller's context, as the
// app quitting or the window giving up on CheckNow does.
type givingUpClient struct {
	Client

	cancel context.CancelFunc
}

func (c givingUpClient) ListThreads(ctx context.Context, tok github.Token, q github.ThreadQuery) (*github.ThreadPage, error) {
	c.cancel()

	return c.Client.ListThreads(ctx, tok, q)
}

func TestNothingToldOnceTheCallerGaveUp(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(3, start))
	r.mustPass()

	shown := r.svc.shown
	require.NotNil(t, shown)

	told := r.events.Count()

	// The window gives up on CheckNow while it lists, after a thread changed.
	ctx, cancel := context.WithCancel(t.Context())
	r.svc.client = givingUpClient{Client: r.gh.client(), cancel: cancel}
	r.gh.update(firstID+2, start.Add(time.Minute))
	r.clock.Advance(5 * time.Minute)

	_, err := r.svc.CheckNow(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, told, r.events.Count(), "nothing told to a window that stopped waiting")
	assert.Same(t, shown, r.svc.shown, "what was shown stands")
	assert.Empty(t, r.state().LastError, "nor is giving up a failure")

	// The next pass finds the change, against what was shown.
	r.svc.client = r.gh.client()
	r.clock.Advance(5 * time.Minute)
	r.mustPass()
	assert.Equal(t, told+1, r.events.Count())
}

func TestUpdatedEmittedOnlyOnChange(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.gh.set(prThreads(3, start))

	r.mustPass()
	assert.Equal(t, 1, r.events.Count(), "the first pass found threads")

	r.clock.Advance(5 * time.Minute)
	r.mustPass()
	assert.Equal(t, 1, r.events.Count(), "a 304 that changed nothing tells nothing")

	r.gh.update(firstID+2, start.Add(time.Minute))
	r.clock.Advance(5 * time.Minute)
	r.mustPass()
	assert.Equal(t, 2, r.events.Count(), "a thread changed")

	r.gh.configure(func(f *fakeGitHub) { f.failPage = 1 })
	r.clock.Advance(5 * time.Minute)

	_, err := r.pass()
	require.Error(t, err)
	assert.Equal(t, 3, r.events.Count(), "the pass failed")

	r.clock.Advance(5 * time.Minute)

	_, err = r.pass()
	require.Error(t, err)
	assert.Equal(t, 3, r.events.Count(), "failing the same way again changes nothing shown")

	for _, e := range r.events.All() {
		assert.Equal(t, "ghthreads:updated", e.Name())
	}
}
