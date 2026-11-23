package ghthreads

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// otherRepo is a second repository of the fixtures' owner.
const otherRepo = "other-repo"

// old is two days before start: past every age but a quarter's.
var old = start.Add(-48 * time.Hour) //nolint:gochecknoglobals // a fixed test time

// noBulkDone is the bulk mark's status when there is none to show.
func noBulkDone() BulkDoneStatus {
	return BulkDoneStatus{
		Age: BulkAgeAny, Repos: []string{}, Pause: BulkPauseNone, PerMinute: budget.BulkPerMinute,
		PerHour: budget.BulkPerHour,
	}
}

// repoThread is a thread about issue number in the fixtures' owner's repository name,
// unread, updated at.
func repoThread(id int64, name string, number int, at time.Time) fakeThread {
	return fakeThread{
		id: id, unread: true, reason: "subscribed", updated: at, title: "Flaky test " + strconv.Itoa(number),
		repo: owner + "/" + name, subject: "issues/" + strconv.Itoa(number), typ: "Issue",
	}
}

// repoThreads returns n issue threads in the repository name, IDs from first, the newest
// updated at newest and each a minute older than the last.
func repoThreads(first int64, name string, n int, newest time.Time) []fakeThread {
	out := make([]fakeThread, 0, n)

	for i := range n {
		out = append(out, repoThread(first+int64(i), name, i+1, newest.Add(-time.Duration(i)*time.Minute)))
	}

	return out
}

// idsOfThreads returns the threads' IDs, in order.
func idsOfThreads(threads []fakeThread) []int64 {
	out := make([]int64, 0, len(threads))

	for _, t := range threads {
		out = append(out, t.id)
	}

	return out
}

// bulkRig returns a rig whose first pass read threads.
func bulkRig(t *testing.T, threads ...fakeThread) *rig {
	t.Helper()

	r := newRig(t)
	r.gh.set(threads)
	r.mustPass()

	return r
}

// examples is a request for the fixtures' repository alone, at age.
func examples(age BulkAge) BulkDoneRequest {
	return BulkDoneRequest{Age: age, Repos: []string{owner + "/" + exampleRepo}}
}

// startBulk starts a bulk mark of req, which must start, and returns the Snapshot.
func (r *rig) startBulk(req BulkDoneRequest) Snapshot {
	r.t.Helper()

	snap, err := r.svc.StartBulkDone(r.t.Context(), req)
	require.NoError(r.t, err)

	return snap
}

// bulk runs a bulk run, as the Syncer's timer does, and returns the requests it sent.
func (r *rig) bulk() []listedRequest {
	r.t.Helper()

	require.NoError(r.t, r.svc.runPass(r.t.Context(), passRequest{bulk: true}))

	return r.gh.requests()
}

// bulkRunning reports whether the bulk mark runs.
func (r *rig) bulkRunning() bool {
	r.t.Helper()

	b := r.bulkState()

	return b.running()
}

// bulkState reads the bulk mark.
func (r *rig) bulkState() bulkState {
	r.t.Helper()

	b, err := r.svc.loadBulk(r.t.Context())
	require.NoError(r.t, err)

	return b
}

// untilBulk returns how long until the next bulk run is due; ok is false for none.
func (r *rig) untilBulk() (time.Duration, bool) {
	return r.svc.untilBulk()
}

// paceAfterEachMark has the bulk lane refuse the mark after each one GitHub answers, so
// each bulk run marks one.
func (r *rig) paceAfterEachMark() {
	g := r.gate()

	r.gh.configure(func(f *fakeGitHub) {
		f.onWrite = func(*http.Request) { g.configure(func(g *fakeGate) { g.bulkPaced = 1 }) }
	})
}

// bulkEvents returns the progress events emitted, in order.
func (r *rig) bulkEvents() []event.GHThreadsBulk {
	var out []event.GHThreadsBulk

	for _, e := range r.events.All() {
		if b, ok := e.(event.GHThreadsBulk); ok {
			out = append(out, b)
		}
	}

	return out
}

// eventNames returns the names of the events emitted after the first skip, in order.
func (r *rig) eventNames(skip int) []string {
	all := r.events.All()
	out := make([]string, 0, len(all))

	for _, e := range all[skip:] {
		out = append(out, e.Name())
	}

	return out
}

// beforesOf returns the cutoffs the listings among got asked for.
func beforesOf(got []listedRequest) []time.Time {
	listed := listings(got)
	out := make([]time.Time, 0, len(listed))

	for _, r := range listed {
		out = append(out, r.before)
	}

	return out
}

func TestStartBulkDoneRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setUp func(r *rig)
		req   BulkDoneRequest
		says  string
	}{
		{
			name: "reading off", setUp: func(r *rig) { r.rules.off.Store(true) }, req: examples(BulkAgeAny),
			says: brand.Name + " isn't reading your GitHub notifications. Turn reading on first.",
		},
		{
			name: "no account read yet", setUp: func(r *rig) { r.restartFresh() }, req: examples(BulkAgeAny),
			says: "Check your GitHub notifications first, so " + brand.Name + " knows whose to mark.",
		},
		{
			name: "GitHub refusing the listing",
			setUp: func(r *rig) {
				r.gh.configure(func(f *fakeGitHub) { f.listStatus, f.listMessage = http.StatusForbidden, "Forbidden" })
				r.clock.Advance(time.Minute)
				_, _ = r.pass()
			},
			req: examples(BulkAgeAny), says: refusedMessage,
		},
		{
			name: "an age it doesn't know", req: BulkDoneRequest{Age: "fortnight", All: true},
			says: "Choose how old the notifications to mark done are.",
		},
		{
			name: "no repository", req: BulkDoneRequest{Age: BulkAgeAny, Repos: []string{}},
			says: "Tick at least one repository to mark its notifications done.",
		},
		{
			name: "a name that isn't a repository's", req: BulkDoneRequest{Age: BulkAgeAny, Repos: []string{"no slash"}},
			says: `"no slash" isn't a GitHub repository.`,
		},
		{
			name: "one already running", setUp: func(r *rig) { r.startBulk(examples(BulkAgeAny)) },
			req:  BulkDoneRequest{Age: BulkAgeAny, All: true},
			says: "Notifications are being marked done already. Stop that first to start again.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old))
			if tt.setUp != nil {
				tt.setUp(r)
			}

			was := r.bulkState()
			r.gh.requests()

			_, err := r.svc.StartBulkDone(t.Context(), tt.req)
			require.ErrorIs(t, err, errBulkStart)
			assert.Equal(t, tt.says, uierr.Message(err))
			assert.Equal(t, was, r.bulkState(), "nothing is stored")
			assertNoRequests(t, r.gh.requests(), "nothing is sent")
		})
	}
}

// restartFresh empties the database's state of the passes and builds the Service again, as
// before the first pass.
func (r *rig) restartFresh() {
	r.t.Helper()

	_, err := r.db.ExecContext(r.t.Context(), `DELETE FROM prefs WHERE key = ?`, KeySync)
	require.NoError(r.t, err)
	r.restart()
}

func TestBulkDoneMarksOnlyThreadsInScopeAndBeforeTheCutoff(t *testing.T) {
	t.Parallel()

	inScope := []fakeThread{
		repoThread(firstID, exampleRepo, 1, old),
		repoThread(firstID+1, exampleRepo, 2, old.Add(-time.Hour)),
		repoThread(firstID+2, exampleRepo, 3, old.Add(-2*time.Hour)),
	}
	fresh := repoThread(firstID+3, exampleRepo, 4, start.Add(-time.Hour))
	elsewhere := []fakeThread{
		repoThread(firstID+4, otherRepo, 1, old.Add(-30*time.Minute)),
		repoThread(firstID+5, otherRepo, 2, old.Add(-90*time.Minute)),
	}

	r := bulkRig(t, append(append(slices.Clone(inScope), fresh), elsewhere...)...)

	// The repository's name in another case names it all the same.
	snap := r.startBulk(BulkDoneRequest{Age: BulkAgeDay, Repos: []string{"ACME-ORG/Example-Repo", owner + "/" + exampleRepo}})
	cutoff := start.Add(-24 * time.Hour)

	assert.Equal(t, BulkDoneStatus{
		Running: true, Age: BulkAgeDay, Before: clock.Format(cutoff), Repos: []string{"ACME-ORG/Example-Repo"},
		Expected: 3, Left: 3, Pause: BulkPauseNone, StartedAt: clock.Format(start),
		PerMinute: budget.BulkPerMinute, PerHour: budget.BulkPerHour,
	}, snap.BulkDone)

	wait, due := r.untilBulk()
	assert.True(t, due, "a bulk run is due")
	assert.LessOrEqual(t, wait, time.Duration(0), "at once")

	got := r.bulk()

	assert.Equal(t, idsOfThreads(inScope), idsOf(writes(got)), "only the repository's, updated before the cutoff")
	assert.Len(t, writes(got), len(ofKind(got, kindDone)), "each marked done")
	assert.Equal(t, []time.Time{cutoff, cutoff}, beforesOf(got), "read before the cutoff, then again once marked")

	for _, th := range inScope {
		_, kept := r.row(viewerLogin, th.id)
		assert.False(t, kept, "marked done, it leaves the mirror")
	}

	assert.Equal(t, 3, r.rows(viewerLogin), "the fresh one and the other repository's stay")

	b := r.bulkState()
	assert.Equal(t, 3, b.Done)
	assert.Equal(t, start, b.EndedAt, "a short page with none left ends it")
	assert.False(t, b.Stopped)

	_, due = r.untilBulk()
	assert.False(t, due, "no bulk run is due once it ended")

	done := r.snapshot().BulkDone
	assert.False(t, done.Running)
	assert.Equal(t, 3, done.Done)
	assert.Equal(t, clock.Format(start), done.EndedAt)
	assert.Zero(t, done.Left, "an ended mark counts nothing left")

	assert.Equal(t, []event.GHThreadsBulk{{Done: 1, Left: 2}, {Done: 2, Left: 1}, {Done: 3}}, r.bulkEvents(),
		"a progress event after each mark")
}

func TestBulkDoneReachesPastTheMirror(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old))

	// GitHub holds one the mirror never read.
	unseen := repoThread(firstID+1, exampleRepo, 2, old.Add(-time.Hour))
	r.gh.set([]fakeThread{repoThread(firstID, exampleRepo, 1, old), unseen})

	r.startBulk(BulkDoneRequest{Age: BulkAgeAny, All: true})
	assert.Equal(t, 1, r.bulkState().Expected, "the mirror counted one")

	got := r.bulk()
	assert.Equal(t, []int64{firstID, unseen.id}, idsOf(writes(got)))
	assert.Equal(t, 2, r.bulkState().Done)
}

func TestBulkDoneMovesItsCursorPastUntickedRepositories(t *testing.T) {
	t.Parallel()

	others := repoThreads(firstID, otherRepo, 60, old)
	ours := repoThreads(firstID+100, exampleRepo, 5, old.Add(-2*time.Hour))

	r := bulkRig(t, append(slices.Clone(others), ours...)...)
	r.startBulk(examples(BulkAgeAny))

	got := r.bulk()
	assert.Equal(t, idsOfThreads(ours), idsOf(writes(got)))

	// The first page is the other repository's newest 50: the next listing starts a second
	// after its oldest, which lists that one again with any updated in the same second.
	past := others[49].updated.Add(time.Second)
	assert.Equal(t, []time.Time{start, past, past}, beforesOf(got))
	assert.Equal(t, past, r.bulkState().Cursor)
	assert.False(t, r.bulkRunning())
}

func TestBulkDoneStepsBackPastAPageUpdatedInOneSecond(t *testing.T) {
	t.Parallel()

	// Fifty of the other repository's updated in the same second fill the first page.
	others := make([]fakeThread, 0, github.ThreadsPerPage)
	for i := range github.ThreadsPerPage {
		others = append(others, repoThread(firstID+int64(i), otherRepo, i+1, old))
	}

	ours := repoThread(firstID+100, exampleRepo, 1, old.Add(-time.Hour))

	r := bulkRig(t, append(others, ours)...)
	r.startBulk(examples(BulkAgeAny))

	got := r.bulk()
	assert.Equal(t, []int64{ours.id}, idsOf(writes(got)))
	assert.Equal(t, []time.Time{start, old.Add(time.Second), old, old}, beforesOf(got),
		"a cursor that doesn't move steps back a second")
}

func TestBulkDoneCountsNotFoundAsDone(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old))

	// Done elsewhere just before: GitHub has no such thread any more.
	r.gh.configure(func(f *fakeGitHub) {
		f.onWrite = func(*http.Request) { f.threads = nil }
	})

	r.startBulk(examples(BulkAgeAny))
	r.bulk()

	b := r.bulkState()
	assert.Equal(t, 1, b.Done, "a thread GitHub no longer has counts as done")
	assert.Zero(t, b.Failed)
	assert.False(t, b.running())
	assert.Zero(t, r.rows(viewerLogin))
}

func TestBulkDoneWaitsAsTheLaneSays(t *testing.T) {
	t.Parallel()

	held := budget.NewTracker(testutil.NewClock(start), nil)
	held.HoldWrites(40 * time.Second)

	reserved := budget.NewTracker(testutil.NewClock(start), nil)
	reserved.ObserveRate(github.RateReading{
		Resource: github.ResourceCore, Limit: 5000, Remaining: 20, Used: 4980, ResetAt: start.Add(20 * time.Minute),
	}, 0)

	tests := []struct {
		name  string
		err   error
		pause BulkPause
		wait  time.Duration
	}{
		{name: "GitHub asked for a pause", err: held.AllowBulkChore(github.ResourceCore), pause: BulkPauseHeld, wait: 40 * time.Second},
		{
			name: "the budget down to its reserve", err: reserved.AllowBulkChore(github.ResourceCore), pause: BulkPauseReserve,
			wait: 20 * time.Minute,
		},
		{name: "the lane's own pace", err: budget.ErrWritePace, pause: BulkPauseNone, wait: time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old), repoThread(firstID+1, exampleRepo, 2, old))
			r.gate().configure(func(g *fakeGate) { g.bulkRefuse = tt.err })

			// GitHub holds more than the mirror.
			st := r.state()
			st.Truncated = true
			require.NoError(t, saveState(t.Context(), r.svc.prefs, st))

			r.startBulk(examples(BulkAgeAny))

			got := r.bulk()
			assert.Empty(t, writes(got), "nothing is sent")

			wait, due := r.untilBulk()
			assert.True(t, due)
			assert.Equal(t, tt.wait, wait, "the next run waits as long as the lane says")

			status := r.snapshot().BulkDone
			assert.True(t, status.Running)
			assert.Equal(t, tt.pause, status.Pause)
			assert.Equal(t, 2, status.Left)
			assert.True(t, status.More, "the mirror holds only the newest")

			if tt.pause == BulkPauseNone {
				assert.Empty(t, status.NextAt, "the pace is no pause")
			} else {
				assert.Equal(t, clock.Format(start.Add(tt.wait)), status.NextAt)
			}

			// Once the lane lets them go, they go, and the pause is over.
			r.gate().configure(func(g *fakeGate) { g.bulkRefuse = nil })
			r.clock.Advance(tt.wait)

			assert.Len(t, writes(r.bulk()), 2)

			b := r.bulkState()
			assert.Equal(t, 2, b.Done)
			assert.Equal(t, BulkPauseNone, b.Pause)
		})
	}
}

func TestBulkDoneWaitsForTheAccountsToken(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old))
	r.startBulk(examples(BulkAgeAny))

	// gh auth switch.
	r.tokens.Swap("another-test-token-not-real")
	r.gh.configure(func(f *fakeGitHub) { f.viewer = otherLogin })

	assert.Equal(t, []listedRequest{{kind: "viewer"}}, r.bulk(), "the new token's account is learned, and nothing is sent")
	assert.Equal(t, BulkPauseOtherAccount, r.bulkState().Pause)
	assert.Equal(t, BulkPauseOtherAccount, r.snapshot().BulkDone.Pause)

	_, due := r.untilBulk()
	assert.False(t, due, "it waits for the next pass, not a timer")

	// Switched back: the next pass has it go on.
	r.tokens.Swap("test-token-not-real")
	r.gh.configure(func(f *fakeGitHub) { f.viewer = viewerLogin })
	r.clock.Advance(time.Minute)
	r.mustPass()

	wait, due := r.untilBulk()
	require.True(t, due, "a pass has the waiting job go on")
	assert.LessOrEqual(t, wait, time.Duration(0))

	assert.Equal(t, []int64{firstID}, idsOf(writes(r.bulk())))
	assert.Equal(t, BulkPauseNone, r.bulkState().Pause)
}

func TestBulkDoneWaitsWhileReadingIsOff(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old))
	r.startBulk(examples(BulkAgeAny))

	r.rules.off.Store(true)
	assertNoRequests(t, r.bulk(), "nothing is sent while reading is off")
	assert.Equal(t, BulkPauseReadingOff, r.bulkState().Pause)
	assert.True(t, r.bulkRunning(), "the job waits")

	// Reading back on: the next pass has it go on.
	r.rules.off.Store(false)
	r.clock.Advance(time.Minute)
	r.mustPass()

	assert.Equal(t, []int64{firstID}, idsOf(writes(r.bulk())))
	assert.Equal(t, BulkPauseNone, r.bulkState().Pause)
}

func TestBulkDoneGivesUpOnAThreadAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old))
	r.gh.configure(func(f *fakeGitHub) { f.writeStatus = http.StatusBadGateway })
	r.startBulk(examples(BulkAgeAny))

	for attempt := 1; attempt < MaxAttempts; attempt++ {
		assert.Equal(t, []int64{firstID}, idsOf(writes(r.bulk())), "attempt %d", attempt)

		b := r.bulkState()
		assert.Equal(t, BulkPauseFailing, b.Pause)
		assert.Equal(t, bulkAttempt{Thread: firstID, Count: attempt}, b.Attempt)
		assert.Contains(t, b.LastError, "GitHub didn't mark it done:")

		wait, due := r.untilBulk()
		require.True(t, due)
		assert.Equal(t, FailedRetry, wait, "tried again a minute on")

		r.clock.Advance(FailedRetry)
	}

	assert.Equal(t, []int64{firstID}, idsOf(writes(r.bulk())), "the last attempt")

	b := r.bulkState()
	assert.Equal(t, 1, b.Failed)
	assert.Zero(t, b.Done)
	assert.Equal(t, []int64{firstID}, b.Skipped)
	assert.False(t, b.running(), "nothing else is left, so it ended")

	_, kept := r.row(viewerLogin, firstID)
	assert.True(t, kept, "a thread given up on stays in the mirror")

	status := r.snapshot().BulkDone
	assert.Equal(t, 1, status.Failed)
	assert.Contains(t, status.Error, "GitHub didn't mark it done:")
}

func TestBulkDoneLeavesAThreadWithNewActivity(t *testing.T) {
	t.Parallel()

	first := repoThread(firstID, exampleRepo, 1, old)
	second := repoThread(firstID+1, exampleRepo, 2, old.Add(-time.Hour))

	r := bulkRig(t, first, second)
	r.paceAfterEachMark()
	r.startBulk(examples(BulkAgeDay))

	assert.Equal(t, []int64{first.id}, idsOf(writes(r.bulk())), "one a run")

	// The second gets new activity, which a pass reads, before the next run.
	r.gh.update(second.id, start)
	r.mustPass()
	r.clock.Advance(budget.BulkGap)

	got := r.bulk()
	assert.Empty(t, writes(got), "a thread updated since the cutoff is left alone")

	_, kept := r.row(viewerLogin, second.id)
	assert.True(t, kept)

	b := r.bulkState()
	assert.Equal(t, 1, b.Done)
	assert.False(t, b.running(), "nothing older is left")
}

func TestStopAndDismissBulkDone(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThread(firstID, exampleRepo, 1, old))
	r.startBulk(examples(BulkAgeAny))

	snap, err := r.svc.DismissBulkDone(t.Context())
	require.NoError(t, err)
	assert.True(t, snap.BulkDone.Running, "one still running isn't dismissed")

	r.clock.Advance(time.Second)

	snap, err = r.svc.StopBulkDone(t.Context())
	require.NoError(t, err)
	assert.False(t, snap.BulkDone.Running)
	assert.True(t, snap.BulkDone.Stopped)
	assert.Equal(t, clock.Format(start.Add(time.Second)), snap.BulkDone.EndedAt)

	_, due := r.untilBulk()
	assert.False(t, due, "a stopped job runs no more")
	assertNoRequests(t, r.bulk(), "a run finds it stopped, and sends nothing")

	// Stopping again changes nothing.
	again, err := r.svc.StopBulkDone(t.Context())
	require.NoError(t, err)
	assert.Equal(t, snap.BulkDone, again.BulkDone)

	snap, err = r.svc.DismissBulkDone(t.Context())
	require.NoError(t, err)
	assert.Equal(t, noBulkDone(), snap.BulkDone, "dismissed, there's nothing to show")

	r.restart()
	assert.Equal(t, noBulkDone(), r.snapshot().BulkDone, "the dismissal holds across a restart")

	// A new one starts.
	assert.True(t, r.startBulk(examples(BulkAgeAny)).BulkDone.Running)
}

func TestBulkDoneGoesOnAfterARestart(t *testing.T) {
	t.Parallel()

	threads := repoThreads(firstID, exampleRepo, 3, old)

	r := bulkRig(t, threads...)
	r.paceAfterEachMark()
	r.startBulk(examples(BulkAgeAny))

	assert.Equal(t, []int64{firstID}, idsOf(writes(r.bulk())))

	// The app quits, and starts again.
	r.gh.configure(func(f *fakeGitHub) { f.onWrite = nil })
	r.restart()

	_, due := r.untilBulk()
	assert.False(t, due, "nothing is due before the first pass")

	r.clock.Advance(time.Minute)
	r.mustPass()

	_, due = r.untilBulk()
	require.True(t, due, "the first pass finds the job unfinished")

	got := r.bulk()
	assert.Equal(t, []int64{firstID + 1, firstID + 2}, idsOf(writes(got)), "it goes on where it left off")

	b := r.bulkState()
	assert.Equal(t, 3, b.Done)
	assert.False(t, b.running())
}

func TestBulkDoneTellsTheWindowSeldom(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThreads(firstID, exampleRepo, 4, old)...)
	r.paceAfterEachMark()

	told := r.events.Count()
	r.startBulk(examples(BulkAgeAny))

	r.bulk()
	r.clock.Advance(budget.BulkGap)
	r.bulk()
	assert.Equal(t, []string{event.NameGHThreadsUpdated, event.NameGHThreadsBulk, event.NameGHThreadsBulk},
		r.eventNames(told), "the start is told, then each mark's progress, and the mirror not yet")

	r.clock.Advance(bulkTellEvery)
	r.bulk()
	assert.Equal(t, []string{event.NameGHThreadsBulk, event.NameGHThreadsUpdated}, r.eventNames(told+3),
		"every bulkTellEvery, the window reads the mirror again")

	// The last mark finds nothing older left, and ends the job.
	r.clock.Advance(budget.BulkGap)
	r.bulk()
	assert.Equal(t, []string{event.NameGHThreadsBulk, event.NameGHThreadsUpdated}, r.eventNames(told+5),
		"the end is told at once")

	assert.Equal(t, []event.GHThreadsBulk{{Done: 1, Left: 3}, {Done: 2, Left: 2}, {Done: 3, Left: 1}, {Done: 4}},
		r.bulkEvents())
	assert.False(t, r.bulkRunning())
}

func TestBulkDoneAndClearsShareNoLane(t *testing.T) {
	t.Parallel()

	// A pull request the user reviews now, and an old one whose clear is queued too.
	reviewed := prThread(firstID, 1, start)
	queued := prThread(firstID+1, 2, old)

	r := clearingRig(t, reviewed, queued, repoThread(firstID+2, exampleRepo, 9, old))
	r.settle(outcome(OutcomeReviewed, item.KindPR, 1, start), outcome(OutcomeReviewed, item.KindPR, 2, start))
	require.Len(t, r.ledger(), 2)

	r.startBulk(BulkDoneRequest{Age: BulkAgeDay, All: true})

	got := r.bulk()
	assert.Equal(t, []int64{queued.id, firstID + 2}, idsOf(writes(got)), "the old ones are marked in bulk")

	ledger := r.ledger()
	assert.Equal(t, "skipped", ledger[1].state, "the clear of one marked in bulk is settled")
	assert.Equal(t, string(SkipNotListed), ledger[1].reason)

	// The reviewed one's clear goes out through the chore lane, once its hold ends.
	r.clock.Advance(MinHold)
	assert.Equal(t, []int64{reviewed.id}, idsOf(writes(r.send())))

	g := r.gate()
	g.mu.Lock()
	defer g.mu.Unlock()

	assert.Equal(t, 1, g.allowed, "the clear went through the chore lane")
	assert.Equal(t, 2, g.bulkAllowed, "the bulk marks through the bulk lane")
}

func TestBulkDoneEndsWhenNothingIsInScope(t *testing.T) {
	t.Parallel()

	r := bulkRig(t, repoThread(firstID, otherRepo, 1, old))

	snap := r.startBulk(examples(BulkAgeAny))
	assert.Zero(t, snap.BulkDone.Expected)

	got := r.bulk()
	assert.Empty(t, writes(got))
	assert.Len(t, listings(got), 1, "one short page with none in scope ends it")
	assert.False(t, r.bulkRunning())
}

func TestBulkStatusIsNeverNull(t *testing.T) {
	t.Parallel()

	var b bulkState

	status := b.status(nil, false)
	assert.Equal(t, noBulkDone(), status)
	assert.NotNil(t, status.Repos)
}

func TestComparedLeavesOutTheBulkCounts(t *testing.T) {
	t.Parallel()

	a := Snapshot{BulkDone: BulkDoneStatus{Running: true, Pause: BulkPauseNone, Done: 1, Left: 4, NextAt: "soon"}}
	b := Snapshot{BulkDone: BulkDoneStatus{Running: true, Pause: BulkPauseNone, Done: 3, Left: 2, More: true}}
	assert.Equal(t, compared(a), compared(b), "progress alone tells nothing")

	b.BulkDone.Pause, b.BulkDone.NextAt = BulkPauseHeld, "later"
	assert.NotEqual(t, compared(a), compared(b), "a pause does")
}

func TestBulkAgeSpans(t *testing.T) {
	t.Parallel()

	for age, want := range map[BulkAge]time.Duration{
		BulkAgeAny: 0, BulkAgeDay: 24 * time.Hour, BulkAgeWeek: 7 * 24 * time.Hour, BulkAgeMonth: 30 * 24 * time.Hour,
		BulkAgeQuarter: 90 * 24 * time.Hour,
	} {
		got, ok := age.span()
		assert.True(t, ok, age)
		assert.Equal(t, want, got, age)
	}

	_, ok := BulkAge("").span()
	assert.False(t, ok)
}

func TestSyncerRunsBulkRunsAsTheyComeDue(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		p := newFakePasser()

		stop := runSyncer(t, newSyncer(p, nil, nil))
		defer stop()

		synctest.Wait()
		require.Equal(t, 1, p.count(), "the first pass")

		p.bulkBy(time.Now(), budget.BulkGap)
		time.Sleep(10*budget.BulkGap - time.Millisecond)
		synctest.Wait()

		var bulk []time.Time

		for _, run := range p.ran() {
			if run.req.bulk {
				bulk = append(bulk, run.at)
			}
		}

		assert.Len(t, bulk, 10, "one at once, then one each time the lane lets the next go")

		for i := 1; i < len(bulk); i++ {
			assert.Equal(t, budget.BulkGap, bulk[i].Sub(bulk[i-1]))
		}

		assert.Equal(t, 1, p.count(), "bulk runs are no timed passes")
	})
}
