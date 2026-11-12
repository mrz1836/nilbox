package release

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The pages a followed release's line links to, and what the user is told.
const (
	sdkReleaseURL    = "https://github.com/acme-org/sdk/releases/tag/v1.4.3"
	sdkReleasesURL   = "https://github.com/acme-org/sdk/releases"
	sdkActionsURL    = "https://github.com/acme-org/sdk/actions?query=branch%3Av1.4.3"
	sdkFailedRun     = "https://github.com/acme-org/sdk/actions/runs/1000000005"
	followedRunID    = int64(7)
	publishedTitle   = "acme-org/sdk v1.4.3 is published"
	goreleaserFailed = "The goreleaser workflow failed for acme-org/sdk v1.4.3"
	goreleaser       = "goreleaser"
)

// errNoRecord is a record that can't be written.
var errNoRecord = errors.New("database is locked")

// answer is one poll's answer.
type answer struct {
	w   Workflow
	err error
}

// pending is a poll's answer while the tag's release workflow goes on.
func pending() answer {
	return answer{w: Workflow{Runs: []TagRun{{Workflow: goreleaser, URL: sdkFailedRun}}}}
}

// runsEnded is a poll's answer once the tag's runs ended, failed as failed says, with no
// release published.
func runsEnded(failed ...bool) answer {
	runs := make([]TagRun, 0, len(failed))
	for _, f := range failed {
		runs = append(runs, TagRun{Workflow: goreleaser, URL: sdkFailedRun, Done: true, Failed: f})
	}

	return answer{w: Workflow{Runs: runs}}
}

// published is a poll's answer once the release is published.
func published() answer {
	return answer{w: Workflow{Published: true, ReleaseURL: sdkReleaseURL}}
}

// fakeWorkflows answers each poll in turn, the last answer again once they run out, and
// keeps when each poll came and what it asked.
type fakeWorkflows struct {
	mu      sync.Mutex
	answers []answer
	at      []time.Time
	asked   []string
}

func (f *fakeWorkflows) ReleaseWorkflow(_ context.Context, name repo.Name, tag, commit string) (Workflow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.at = append(f.at, time.Now())
	f.asked = append(f.asked, name.String()+" "+tag+" "+commit)

	if len(f.answers) == 0 {
		return Workflow{}, nil
	}

	a := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}

	return a.w, a.err
}

// polls returns how long after start each poll came.
func (f *fakeWorkflows) polls(start time.Time) []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]time.Duration, 0, len(f.at))
	for _, at := range f.at {
		out = append(out, at.Sub(start))
	}

	return out
}

// fakeRecords holds the releases' records in memory, as the runner's history would: an
// outcome is kept as plain JSON values. Amend fails with amendErr, or for the next
// failNext calls, and calls afterAmend once it wrote.
type fakeRecords struct {
	mu         sync.Mutex
	recs       map[int64]scriptrun.Record
	amends     int
	tries      int
	amendErr   error
	failNext   int
	afterAmend func()
	endedErr   error
}

func newFakeRecords() *fakeRecords {
	return &fakeRecords{recs: map[int64]scriptrun.Record{}}
}

func (f *fakeRecords) Ended(_ context.Context, kind scriptrun.Kind) ([]scriptrun.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.endedErr != nil {
		return nil, f.endedErr
	}

	out := []scriptrun.Record{}

	for _, rec := range f.recs {
		if rec.Kind == kind {
			out = append(out, rec)
		}
	}

	return out, nil
}

func (f *fakeRecords) Amend(
	_ context.Context, id int64, change func(scriptrun.Record) (scriptrun.Amendment, bool),
) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tries++

	if f.failNext > 0 {
		f.failNext--

		return false, errNoRecord
	}

	if f.amendErr != nil {
		return false, f.amendErr
	}

	rec, ok := f.recs[id]
	if !ok {
		return false, scriptrun.ErrNotFound
	}

	a, ok := change(rec)
	if !ok {
		return false, nil
	}

	raw, err := json.Marshal(a.Outcome)
	if err != nil {
		return false, err
	}

	var outcome any
	if err = json.Unmarshal(raw, &outcome); err != nil {
		return false, err
	}

	rec.Summary, rec.Result.Outcome = a.Summary, outcome
	f.recs[id] = rec
	f.amends++

	if f.afterAmend != nil {
		f.afterAmend()
	}

	return true, nil
}

// add records a release that ended with res, as its summary says.
func (f *fakeRecords) add(t *testing.T, id int64, res Result) {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	f.recs[id] = scriptrun.Record{
		ID: id, Kind: scriptrun.KindRelease, Repo: name("sdk"), Status: scriptrun.StatusSucceeded,
		Summary: followedSummary(res), Result: scriptrun.Result{Outcome: plain(t, res)},
	}
}

// failAmends makes every Amend fail with err, or succeed again for nil.
func (f *fakeRecords) failAmends(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.amendErr = err
}

// failAmendsOnce makes the next n Amends fail.
func (f *fakeRecords) failAmendsOnce(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failNext = n
}

// counts returns how many Amends were tried and how many wrote.
func (f *fakeRecords) counts() (tries, amends int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tries, f.amends
}

// result returns the release id's result and summary as recorded.
func (f *fakeRecords) result(t *testing.T, id int64) (Result, string) {
	t.Helper()

	f.mu.Lock()
	rec := f.recs[id]
	f.mu.Unlock()

	res, err := decodeResult(rec.Result.Outcome)
	require.NoError(t, err)

	return res, rec.Summary
}

// forget drops the release id's record, as the history does past its limits.
func (f *fakeRecords) forget(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.recs, id)
}

// plain returns v as the plain JSON values a record holds.
func plain(t *testing.T, v any) any {
	t.Helper()

	raw, err := json.Marshal(v)
	require.NoError(t, err)

	var out any
	require.NoError(t, json.Unmarshal(raw, &out))

	return out
}

// sdkReleased is acme-org/sdk v1.4.3 on GitHub, at newestOID, not yet followed.
func sdkReleased() Result {
	return releasedResult(name("sdk"),
		Result{Version: recVersion, Tag: recVersion, Head: newestOID, Commands: []string{}}, newestOID)
}

// followingSince is sdkReleased, followed since since, after polls polls.
func followingSince(since time.Time, polls int) Result {
	res := sdkReleased()
	res.Follow = &Follow{
		State: FollowFollowing, Since: clock.Format(since), Until: clock.Format(since.Add(FollowFor)), Polls: polls,
	}

	return res
}

// followRig is a Follower over fakes: GitHub's answers, the runs' records, and the user's
// notices. Build it inside a synctest bubble, whose clock it follows.
type followRig struct {
	t       *testing.T
	gh      *fakeWorkflows
	recs    *fakeRecords
	notices *fakeNotifier
	logs    *testutil.Logs
	svc     *Service
	f       *Follower
	start   time.Time
}

func newFollowRig(t *testing.T, answers ...answer) *followRig {
	t.Helper()

	r := &followRig{
		t: t, gh: &fakeWorkflows{answers: answers}, recs: newFakeRecords(), notices: &fakeNotifier{},
		logs: &testutil.Logs{}, start: time.Now(),
	}
	r.svc = NewService(Config{Workflows: r.gh, Records: r.recs, Notifier: r.notices, Logger: r.logs.Logger()})
	r.f = NewFollower(r.svc)

	return r
}

// released records a release that just reached GitHub and hands it to the Follower, as
// its run's Recorded does.
func (r *followRig) released() {
	r.t.Helper()

	res := sdkReleased()
	res.Follow = r.svc.startFollow(name("sdk"), res)
	require.NotNil(r.t, res.Follow)

	r.recs.add(r.t, followedRunID, res)
	r.svc.announcer(name("sdk"), &Intent{Version: recVersion, Branch: branchMaster})(
		r.t.Context(), followedRunID, scriptrun.Outcome{Status: scriptrun.StatusSucceeded, Detail: res},
	)
}

// run runs the Follower until stop is called, which waits for it to return.
func (r *followRig) run() (stop func()) {
	return r.runIn(r.t.Context())
}

// runIn runs the Follower under ctx until stop is called, which ends it and waits for it.
func (r *followRig) runIn(parent context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})

	go func() {
		defer close(done)

		r.f.Run(ctx)
	}()

	synctest.Wait()

	return func() {
		cancel()
		<-done
	}
}

// followedFor runs the Follower over a release that just reached GitHub for an hour of
// the bubble's time, and returns the release's result and summary as recorded then.
func (r *followRig) followedFor() (Result, string) {
	r.t.Helper()

	r.released()

	stop := r.run()
	defer stop()

	time.Sleep(time.Hour)
	synctest.Wait()

	return r.recs.result(r.t, followedRunID)
}

// minutes returns each of ms as minutes.
func minutes(ms ...int) []time.Duration {
	out := make([]time.Duration, 0, len(ms))
	for _, m := range ms {
		out = append(out, time.Duration(m)*time.Minute)
	}

	return out
}

func TestTheFollowerPollsWithinItsLimits(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, pending())
		r.released()

		stop := r.run()
		defer stop()

		time.Sleep(59 * time.Second)
		synctest.Wait()
		assert.Empty(t, r.gh.polls(r.start), "nothing is asked in the first minute")

		time.Sleep(time.Hour)
		synctest.Wait()

		assert.Equal(t, minutes(1, 4, 7, 10, 13, 16, 19, 22, 25, 28, 31, 34, 37, 40, 43, 45), r.gh.polls(r.start),
			"a minute in, then every three minutes, and a last time as 45 minutes end")
		assert.Equal(t, []string{"acme-org/sdk v1.4.3 " + newestOID}, r.gh.asked[:1], "the tag's own commit")

		res, summary := r.recs.result(t, followedRunID)
		require.NotNil(t, res.Follow)
		assert.Equal(t, FollowTimedOut, res.Follow.State)
		assert.Equal(t, 16, res.Follow.Polls)
		assert.Equal(t, 45, res.Follow.Minutes)
		assert.Equal(t, clock.Format(r.start.Add(FollowFor)), res.Follow.Ended)
		assert.Equal(t, sdkActionsURL, res.Follow.URL, "its line links to the tag's runs")
		assert.Equal(t, "v1.4.3 is on GitHub.", summary, "nothing is assumed")

		_, amends := r.recs.counts()
		assert.Equal(t, 16, amends, "each poll is written down")

		published, failed := r.notices.followed()
		assert.Empty(t, published, "the limit tells nothing: the result says so")
		assert.Empty(t, failed)
	})
}

func TestTheFollowerStopsAtFortyFiveMinutesByTheClock(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, pending())
		r.f.every = 10 * time.Minute

		res, _ := r.followedFor()

		assert.Equal(t, minutes(1, 11, 21, 31, 41, 45), r.gh.polls(r.start),
			"the last poll comes as 45 minutes end, long before 20")
		assert.Equal(t, FollowTimedOut, res.Follow.State)
		assert.Equal(t, 6, res.Follow.Polls)
		assert.Equal(t, clock.Format(r.start.Add(FollowFor)), res.Follow.Ended)
		assert.Equal(t, 45, res.Follow.Minutes)
	})
}

func TestTheFollowerStopsWhenTheReleaseIsPublished(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, pending(), pending(), published())

		res, summary := r.followedFor()

		assert.Equal(t, minutes(1, 4, 7), r.gh.polls(r.start), "nothing is asked once it is published")
		assert.Equal(t, Follow{
			State: FollowPublished, Since: clock.Format(r.start), Until: clock.Format(r.start.Add(FollowFor)),
			Ended: clock.Format(r.start.Add(7 * time.Minute)), Minutes: 7, Polls: 3, URL: sdkReleaseURL,
		}, *res.Follow)
		assert.Equal(t, "v1.4.3 is published.", summary, "the run's result says so")
		assert.Equal(t, ResultReleased, res.Kind, "the rest of the result stays")
		assert.Equal(t, newestOID, res.Commit)

		published, failed := r.notices.followed()
		assert.Equal(t, []Notice{{Repo: name("sdk"), Title: publishedTitle, URL: sdkReleaseURL}}, published)
		assert.Empty(t, failed)
		assert.Len(t, r.notices.all(), 1, "the release on GitHub was told once, as it ended")
	})
}

func TestTheFollowerStopsWhenAWorkflowFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		run      TagRun
		url      string
		title    string
		summary  string
		workflow string
	}{
		{
			name: "it names the workflow and links to its run", run: TagRun{Workflow: goreleaser, URL: sdkFailedRun},
			url: sdkFailedRun, title: goreleaserFailed, summary: "v1.4.3 is on GitHub, but its goreleaser workflow failed.",
			workflow: goreleaser,
		},
		{
			name: "without its run, it links to the tag's runs", run: TagRun{Workflow: goreleaser},
			url: sdkActionsURL, title: goreleaserFailed, summary: "v1.4.3 is on GitHub, but its goreleaser workflow failed.",
			workflow: goreleaser,
		},
		{
			name: "without its name, it is the release workflow", run: TagRun{URL: sdkFailedRun},
			url: sdkFailedRun, title: "The release workflow failed for acme-org/sdk v1.4.3",
			summary: "v1.4.3 is on GitHub, but its release workflow failed.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				run := tt.run
				run.Done, run.Failed = true, true

				r := newFollowRig(t, pending(), answer{w: Workflow{Runs: []TagRun{run}}})

				res, summary := r.followedFor()

				assert.Equal(t, minutes(1, 4), r.gh.polls(r.start))
				assert.Equal(t, FollowFailed, res.Follow.State)
				assert.Equal(t, tt.url, res.Follow.URL)
				assert.Equal(t, tt.workflow, res.Follow.Workflow)
				assert.Equal(t, 2, res.Follow.Polls)
				assert.Equal(t, tt.summary, summary)

				published, failed := r.notices.followed()
				assert.Empty(t, published)
				assert.Equal(t, []Notice{{Repo: name("sdk"), Title: tt.title, URL: tt.url}}, failed)
			})
		})
	}
}

func TestAFailureCountsOnlyOnceEveryTagRunEnded(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		failedOne := TagRun{Workflow: goreleaser, URL: sdkFailedRun, Done: true, Failed: true}
		goingOne := TagRun{Workflow: "CI", URL: "https://github.com/acme-org/sdk/actions/runs/1000000004"}
		ciDone := goingOne
		ciDone.Done = true

		r := newFollowRig(t,
			answer{w: Workflow{Runs: []TagRun{failedOne, goingOne}}},
			answer{w: Workflow{Runs: []TagRun{failedOne, ciDone}}})

		res, _ := r.followedFor()

		assert.Equal(t, minutes(1, 4), r.gh.polls(r.start), "the first poll found another run still going")
		assert.Equal(t, FollowFailed, res.Follow.State)
		assert.Equal(t, goreleaser, res.Follow.Workflow)
	})
}

func TestAPublishedReleaseWinsOverAFailedRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		failed := runsEnded(true)
		failed.w.Published = true

		r := newFollowRig(t, failed)

		res, _ := r.followedFor()

		assert.Equal(t, FollowPublished, res.Follow.State)
		assert.Equal(t, sdkReleaseURL, res.Follow.URL, "without the release's own page, the tag's")
	})
}

func TestTheFollowerStopsWhenNoWorkflowRanForTheTag(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, answer{})

		res, summary := r.followedFor()

		assert.Equal(t, minutes(1, 4, 7), r.gh.polls(r.start), "it asks no more after the third poll")
		assert.Equal(t, FollowNoWorkflow, res.Follow.State)
		assert.Equal(t, sdkActionsURL, res.Follow.URL)
		assert.Equal(t, "v1.4.3 is on GitHub.", summary)

		published, failed := r.notices.followed()
		assert.Empty(t, published, "a result line, and no banner")
		assert.Empty(t, failed)
	})
}

func TestTheFollowerStopsWhenTheWorkflowsEndedWithNoRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		answers []answer
		want    []time.Duration
	}{
		{name: "two polls in a row", answers: []answer{runsEnded(false)}, want: minutes(1, 4)},
		{
			name:    "a run started again in between",
			answers: []answer{runsEnded(false), pending(), runsEnded(false), runsEnded(false)},
			want:    minutes(1, 4, 7, 10),
		},
		{
			name:    "a poll that failed in between",
			answers: []answer{runsEnded(false), {err: errGitHubDown}, runsEnded(false)},
			want:    minutes(1, 4, 7),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newFollowRig(t, tt.answers...)

				res, summary := r.followedFor()

				assert.Equal(t, tt.want, r.gh.polls(r.start))
				assert.Equal(t, FollowUnpublished, res.Follow.State)
				assert.Equal(t, sdkReleasesURL, res.Follow.URL, "where a draft would show")
				assert.Equal(t, "v1.4.3 is on GitHub.", summary)

				published, failed := r.notices.followed()
				assert.Empty(t, published, "a result line, and no banner")
				assert.Empty(t, failed)
			})
		})
	}
}

func TestAPollThatFailsCountsAndTheFollowGoesOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, answer{err: errGitHubDown}, published())

		res, _ := r.followedFor()

		assert.Equal(t, FollowPublished, res.Follow.State)
		assert.Equal(t, 2, res.Follow.Polls, "the poll that failed counts against the budget")
		assert.Contains(t, r.logs.String(), "couldn't read a release's workflow on GitHub")
	})
}

func TestAFollowResumesAfterARestartWithinItsLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ago     time.Duration
		polls   int
		want    []time.Duration
		minutes int
		count   int
	}{
		{
			name: "ten minutes in, after four polls", ago: 10 * time.Minute, polls: 4,
			want: minutes(1, 4, 7, 10, 13, 16, 19, 22, 25, 28, 31, 34, 35), minutes: 45, count: 17,
		},
		{
			name: "at its last two polls", ago: 5 * time.Minute, polls: 18, want: minutes(1, 4), minutes: 9, count: 20,
		},
		{name: "past its 45 minutes", ago: 2 * time.Hour, polls: 9, minutes: 45, count: 9},
		{name: "after its last poll", ago: 30 * time.Minute, polls: FollowPolls, minutes: 30, count: FollowPolls},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newFollowRig(t, pending())
				r.recs.add(t, followedRunID, followingSince(r.start.Add(-tt.ago), tt.polls))

				// Another release, not followed: never resumed.
				r.recs.add(t, followedRunID+1, sdkReleased())

				stop := r.run()
				defer stop()

				time.Sleep(time.Hour)
				synctest.Wait()

				if tt.want == nil {
					tt.want = []time.Duration{}
				}

				assert.Equal(t, tt.want, r.gh.polls(r.start), "a minute after the start, then every three")

				res, summary := r.recs.result(t, followedRunID)
				assert.Equal(t, FollowTimedOut, res.Follow.State)
				assert.Equal(t, tt.count, res.Follow.Polls, "the polls before the restart count")
				assert.Equal(t, tt.minutes, res.Follow.Minutes)
				assert.Equal(t, "v1.4.3 is on GitHub.", summary)

				other, _ := r.recs.result(t, followedRunID+1)
				assert.Nil(t, other.Follow)
			})
		})
	}
}

func TestAReleaseHandedOverTwiceIsFollowedOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, pending(), published())

		// Recorded before the Follower ran, so it is both queued and resumed.
		r.released()

		stop := r.run()
		defer stop()

		res, _ := r.recs.result(t, followedRunID)
		r.svc.queueFollow(followed{id: followedRunID, name: name("sdk"), res: res})

		time.Sleep(time.Hour)
		synctest.Wait()

		assert.Equal(t, minutes(1, 4), r.gh.polls(r.start), "one follow")

		published, _ := r.notices.followed()
		assert.Len(t, published, 1, "told once")
	})
}

func TestAReleaseThatEndsWhileTheFollowerRunsIsFollowed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, published())

		stop := r.run()
		defer stop()

		time.Sleep(10 * time.Minute)

		r.start = time.Now()
		r.released()

		time.Sleep(time.Hour)
		synctest.Wait()

		assert.Equal(t, minutes(1), r.gh.polls(r.start))

		res, _ := r.recs.result(t, followedRunID)
		assert.Equal(t, FollowPublished, res.Follow.State)
	})
}

func TestAQuitLeavesTheFollowForTheNextStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, pending())
		r.released()

		stop := r.run()

		time.Sleep(5 * time.Minute)
		synctest.Wait()
		stop()

		assert.Equal(t, minutes(1, 4), r.gh.polls(r.start))

		res, summary := r.recs.result(t, followedRunID)
		assert.Equal(t, FollowFollowing, res.Follow.State, "the next start resumes it")
		assert.Equal(t, 2, res.Follow.Polls)
		assert.Empty(t, res.Follow.Ended)
		assert.Equal(t, "v1.4.3 is on GitHub.", summary)
	})
}

func TestAQuitDuringAPollCountsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		gh := &cancelingWorkflows{cancel: cancel}
		recs := newFakeRecords()
		svc := NewService(Config{Workflows: gh, Records: recs})
		recs.add(t, followedRunID, followingSince(time.Now(), 2))

		NewFollower(svc).Run(ctx)

		res, _ := recs.result(t, followedRunID)
		assert.Equal(t, 2, res.Follow.Polls, "the next start asks again")
		assert.Equal(t, FollowFollowing, res.Follow.State)
	})
}

// cancelingWorkflows ends the app's run as GitHub is asked, as a quit does mid-poll.
type cancelingWorkflows struct {
	cancel context.CancelFunc
}

func (c *cancelingWorkflows) ReleaseWorkflow(ctx context.Context, _ repo.Name, _, _ string) (Workflow, error) {
	c.cancel()

	return Workflow{}, ctx.Err()
}

func TestTheNoticeOutlivesAQuitThatLandsAsItIsRecorded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		answer answer
	}{
		{name: "published", answer: published()},
		{name: "a workflow failed", answer: runsEnded(true)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newFollowRig(t, tt.answer)
				r.released()

				ctx, quit := context.WithCancel(t.Context())
				r.recs.afterAmend = quit

				stop := r.runIn(ctx)
				defer stop()

				time.Sleep(time.Hour)
				synctest.Wait()

				published, failed := r.notices.followed()
				assert.Len(t, append(published, failed...), 1, "told, though the app quit just as it was recorded")
				assert.Equal(t, []error{nil}, r.notices.endedContexts(), "told with a context the quit doesn't end")
			})
		})
	}
}

func TestAFollowStopsWhenItsRecordDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(r *followRig)
	}{
		{name: "the history forgot the run", change: func(r *followRig) { r.recs.forget(followedRunID) }},
		{
			name: "another follow of it ended it",
			change: func(r *followRig) {
				res := sdkReleased()
				res.Follow = &Follow{State: FollowPublished, Since: clock.Format(r.start)}
				r.recs.add(r.t, followedRunID, res)
			},
		},
		{
			name: "the record holds another follow",
			change: func(r *followRig) {
				r.recs.add(r.t, followedRunID, followingSince(r.start.Add(time.Second), 0))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newFollowRig(t, published())
				r.released()

				stop := r.run()
				defer stop()

				// The record changes once the follow has started.
				tt.change(r)

				time.Sleep(time.Hour)
				synctest.Wait()

				assert.Equal(t, minutes(1), r.gh.polls(r.start), "one poll, then it stops")

				published, _ := r.notices.followed()
				assert.Empty(t, published, "nothing is told of what the record doesn't hold")
			})
		})
	}
}

func TestAPollWhoseRecordFailsStillCounts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, pending(), published())
		r.recs.failAmendsOnce(1)

		res, _ := r.followedFor()

		assert.Equal(t, minutes(1, 4), r.gh.polls(r.start))
		assert.Equal(t, FollowPublished, res.Follow.State)
		assert.Equal(t, 2, res.Follow.Polls, "the poll whose record failed is counted all the same")
		assert.Contains(t, r.logs.String(), "couldn't record how following a release goes")
	})
}

func TestADecidedFollowIsWrittenAgainWithoutAskingGitHubAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t, published())
		r.recs.failAmendsOnce(2)

		res, _ := r.followedFor()

		assert.Equal(t, minutes(1), r.gh.polls(r.start), "it asks GitHub once")

		tries, amends := r.recs.counts()
		assert.Equal(t, 3, tries, "written again, twice, a moment apart")
		assert.Equal(t, 1, amends)

		assert.Equal(t, FollowPublished, res.Follow.State, "published, never timed out")
		assert.Equal(t, clock.Format(r.start.Add(time.Minute)), res.Follow.Ended, "as the poll decided")

		published, _ := r.notices.followed()
		assert.Len(t, published, 1, "told once, as soon as the record holds it")
	})
}

func TestAFollowWhoseEndCantBeRecordedIsLeftToTheNextStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		answer answer
		ago    time.Duration
		polls  []time.Duration
	}{
		{name: "published", answer: published(), polls: minutes(1)},
		{name: "past its limits", ago: time.Hour, polls: []time.Duration{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newFollowRig(t, tt.answer)
				r.recs.add(t, followedRunID, followingSince(r.start.Add(-tt.ago), 3))
				r.recs.failAmends(errNoRecord)

				stop := r.run()
				defer stop()

				time.Sleep(time.Hour)
				synctest.Wait()

				assert.Equal(t, tt.polls, r.gh.polls(r.start))

				tries, _ := r.recs.counts()
				assert.Equal(t, recordTries, tries, "a few tries, then it gives up")
				assert.Contains(t, r.logs.String(), "couldn't record how following a release ended")

				res, _ := r.recs.result(t, followedRunID)
				assert.Equal(t, FollowFollowing, res.Follow.State, "the next start settles it")

				published, _ := r.notices.followed()
				assert.Empty(t, published, "nothing is told the record doesn't hold")

				r.f.mu.Lock()
				assert.Empty(t, r.f.seen, "let go of, for a resume to take up again")
				r.f.mu.Unlock()

				// The next start finds the record writable again.
				r.recs.failAmends(nil)
				r.f.resume(t.Context())

				time.Sleep(time.Hour)
				synctest.Wait()

				res, _ = r.recs.result(t, followedRunID)
				assert.NotEqual(t, FollowFollowing, res.Follow.State, "taken up again and settled")
			})
		})
	}
}

func TestAFollowWithALimitThatCantBeReadStops(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t)
		res := sdkReleased()
		res.Follow = &Follow{State: FollowFollowing, Since: "a while ago", Until: "soon"}
		r.recs.add(t, followedRunID, res)

		stop := r.run()
		defer stop()

		synctest.Wait()

		got, _ := r.recs.result(t, followedRunID)
		assert.Equal(t, FollowTimedOut, got.Follow.State)
		assert.Zero(t, got.Follow.Minutes)
		assert.Empty(t, r.gh.polls(r.start))
	})
}

func TestTheFollowerWithoutWhatItNeedsFollowsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		recs := newFakeRecords()
		recs.add(t, followedRunID, followingSince(time.Now(), 0))

		for _, cfg := range []Config{{Records: recs}, {Workflows: &fakeWorkflows{}}} {
			s := NewService(cfg)
			assert.Nil(t, s.startFollow(name("sdk"), sdkReleased()), "nothing starts")

			NewFollower(s).Run(t.Context())
		}

		res, _ := recs.result(t, followedRunID)
		assert.Equal(t, 0, res.Follow.Polls)
	})
}

func TestTheFollowerLogsARecordItCantRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newFollowRig(t)
		r.recs.endedErr = errNoRecord

		stop := r.run()
		stop()

		assert.Contains(t, r.logs.String(), "couldn't read the releases to follow on GitHub")
	})
}

func TestStartFollow(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := NewService(Config{Workflows: &fakeWorkflows{}, Records: newFakeRecords()})
		now := time.Now()

		assert.Equal(t, &Follow{
			State: FollowFollowing, Since: clock.Format(now), Until: clock.Format(now.Add(45 * time.Minute)),
		}, s.startFollow(name("sdk"), sdkReleased()), "45 minutes from now, on the clock its waits use")

		notPushed := sdkReleased()
		notPushed.Kind = ResultNotPushed

		noCommit := sdkReleased()
		noCommit.Commit = ""

		noTag := sdkReleased()
		noTag.Tag = ""

		assert.Nil(t, s.startFollow(name("sdk"), notPushed), "a release not on GitHub")
		assert.Nil(t, s.startFollow(name("sdk"), noCommit))
		assert.Nil(t, s.startFollow(name("sdk"), noTag))
		assert.Nil(t, s.startFollow(repo.Name{}, sdkReleased()), "no repository to ask about")
	})
}

func TestFollowedSummary(t *testing.T) {
	t.Parallel()

	res := sdkReleased()
	assert.Equal(t, "v1.4.3 is on GitHub.", followedSummary(res))

	for state, want := range map[FollowState]string{
		FollowFollowing:   "v1.4.3 is on GitHub.",
		FollowPublished:   "v1.4.3 is published.",
		FollowFailed:      "v1.4.3 is on GitHub, but its release workflow failed.",
		FollowNoWorkflow:  "v1.4.3 is on GitHub.",
		FollowUnpublished: "v1.4.3 is on GitHub.",
		FollowTimedOut:    "v1.4.3 is on GitHub.",
	} {
		res.Follow = &Follow{State: state}
		assert.Equal(t, want, followedSummary(res), state)
	}
}

// fastFollower is a Follower of s that first polls a millisecond in, for a release the real
// runner records, and not again for an hour.
func fastFollower(s *Service) *Follower {
	f := NewFollower(s)
	f.first, f.every = time.Millisecond, time.Hour

	return f
}

// runFollower runs f until the returned stop is called, which waits for it to return.
func runFollower(t *testing.T, f *Follower) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		f.Run(ctx)
	}()

	return func() {
		cancel()
		<-done
	}
}

func TestAReleaseIsFollowedInItsRunsRecordAcrossARestart(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)
	r.magex("1.30.2", r.replay("patch-feature-behind"))
	r.after.set(r.tagged(recVersion, r.second, true))

	gh := &fakeWorkflows{answers: []answer{pending()}}
	r.svc.workflows, r.svc.records = gh, r.engine

	run := r.ended(r.release())
	assert.Equal(t, "v1.4.3 is on GitHub.", run.Summary)

	res := resultOf(t, run)
	require.NotNil(t, res.Follow, "a release on GitHub is followed")
	assert.Equal(t, FollowFollowing, res.Follow.State)
	assert.Zero(t, res.Follow.Polls)

	// The first start polls, then quits.
	stop := runFollower(t, fastFollower(r.svc))

	require.Eventually(t, func() bool {
		got, err := r.runs.Run(t.Context(), run.ID)

		return err == nil && resultOf(t, got).Follow.Polls > 0
	}, runWait, runPoll, "the poll is written into the run's record")

	gh.mu.Lock()
	gh.answers = []answer{published()}
	gh.mu.Unlock()

	stop()

	// The next start resumes it, and GitHub has published it.
	next := NewService(Config{Workflows: gh, Records: r.engine, Notifier: r.notices, Logger: r.logs.Logger()})

	stop = runFollower(t, fastFollower(next))
	defer stop()

	require.Eventually(t, func() bool {
		got, err := r.runs.Run(t.Context(), run.ID)
		if err != nil {
			return false
		}

		run = got

		return resultOf(t, got).Follow.State == FollowPublished
	}, runWait, runPoll, "the next start finds it published")

	assert.Equal(t, "v1.4.3 is published.", run.Summary, "the run's result says so")
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, sdkReleaseURL, resultOf(t, run).Follow.URL)

	shown, err := r.svc.ReleaseResult(t.Context(), run.ID)
	require.NoError(t, err)
	assert.Equal(t, FollowPublished, shown.Follow.State, "the window reads it typed")

	require.Eventually(t, func() bool {
		published, _ := r.notices.followed()

		return len(published) == 1
	}, runWait, runPoll)

	published, failed := r.notices.followed()
	assert.Equal(t, []Notice{{Repo: name("sdk"), Title: publishedTitle, URL: sdkReleaseURL}}, published)
	assert.Empty(t, failed)
}

func TestAReleaseNotOnGitHubIsntFollowed(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.29.5", r.replay("remote-tag-other-commit"))
	r.after.set(r.tagged(recVersion, r.second, false))
	r.svc.workflows, r.svc.records = &fakeWorkflows{}, r.engine

	run := r.ended(r.release())

	assert.NotEqual(t, scriptrun.StatusSucceeded, run.Status)
	assert.Nil(t, resultOf(t, run).Follow)
	assert.Empty(t, r.svc.takeFollows(), "nothing is handed to the Follower")
}
