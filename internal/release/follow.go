package release

import (
	"cmp"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// The limits of following a release's workflow on GitHub (§3.4.9): the first poll a
// minute after the follow starts, or resumes after a restart, then one every three
// minutes, at most FollowPolls of them over FollowFor, whichever comes first. A release's
// follow so spends at most 20 points of the API budget.
const (
	FollowFor   = 45 * time.Minute
	FollowPolls = 20
	FollowFirst = time.Minute
	FollowEvery = 3 * time.Minute
)

// FollowState is where following a release's workflow on GitHub is.
type FollowState string

// The states of a follow.
const (
	// FollowFollowing is a follow going on, within its limits.
	FollowFollowing FollowState = "following"

	// FollowPublished is a release its workflow published: GitHub has it, published, and
	// not a draft.
	FollowPublished FollowState = "published"

	// FollowFailed is a release whose workflow failed: the tag's own push run.
	FollowFailed FollowState = "failed"

	// FollowTimedOut is a follow that reached its limits with neither: nothing is assumed.
	FollowTimedOut FollowState = "timedOut"
)

// Follow is how following a release's workflow on GitHub goes. It is kept in the release's
// result (Result.Follow), in its run's record, so a follow resumes after a restart within
// the same limits.
type Follow struct {
	// State is where it is.
	State FollowState `json:"state"`

	// Since is when it started, Until when it stops at the latest, and Ended when it
	// stopped, or "" while it goes on, each RFC 3339; Minutes is how long it went on, in
	// whole minutes, once it stopped.
	Since   string `json:"since"`
	Until   string `json:"until"`
	Ended   string `json:"ended"`
	Minutes int    `json:"minutes"`

	// Polls counts the queries it made.
	Polls int `json:"polls"`

	// URL is the page its line links to: the release's once published; the failed run's,
	// or else the tag's Actions runs, once failed; the tag's Actions runs once it stopped
	// at its limits; and "" while it goes on.
	URL string `json:"url"`
}

// Workflow is how a release's workflow on GitHub goes, as one poll read it.
type Workflow struct {
	// Published reports the release published, and ReleaseURL is its page.
	Published  bool
	ReleaseURL string

	// Failed reports the tag's own push run failed, and RunURL is that run's page, or "".
	Failed bool
	RunURL string
}

// Workflows reads how a release's workflow goes on GitHub with one query, which the API
// budget counts: core adapts the GitHub client's FetchReleaseProgress. commit is the
// commit the release's tag points at on origin.
type Workflows interface {
	ReleaseWorkflow(ctx context.Context, name repo.Name, tag, commit string) (Workflow, error)
}

// RunRecords reads the releases that ended, and rewrites how one came out as its follow
// goes: the scriptrun engine.
type RunRecords interface {
	Ended(ctx context.Context, kind scriptrun.Kind) ([]scriptrun.Record, error)
	Amend(ctx context.Context, id int64, change func(scriptrun.Record) (scriptrun.Amendment, bool)) (bool, error)
}

// followed is a release followed on GitHub: its run, its repository, and its result, the
// follow in it as last recorded.
type followed struct {
	id   int64
	name repo.Name
	res  Result
}

// startFollow returns the follow of res that starts now, or nil for a result that can't
// be followed: one that isn't on GitHub, with no repository, tag, or commit to ask about,
// or a Service that can't follow at all.
func (s *Service) startFollow(name repo.Name, res Result) *Follow {
	if s.workflows == nil || s.records == nil || res.Kind != ResultReleased || !name.Valid() || res.Tag == "" ||
		res.Commit == "" {
		return nil
	}

	now := s.clock.Now()

	return &Follow{State: FollowFollowing, Since: clock.Format(now), Until: clock.Format(now.Add(FollowFor))}
}

// queueFollow hands a release that reached GitHub, its outcome recorded, to the Follower.
// A release handed over before the Follower runs is followed once it does.
func (s *Service) queueFollow(f followed) {
	s.followMu.Lock()
	s.toFollow[f.id] = f
	s.followMu.Unlock()

	select {
	case s.wakeFollow <- struct{}{}:
	default:
	}
}

// takeFollows returns the releases handed to the Follower since it last looked.
func (s *Service) takeFollows() []followed {
	s.followMu.Lock()
	defer s.followMu.Unlock()

	out := make([]followed, 0, len(s.toFollow))
	for _, f := range s.toFollow {
		out = append(out, f)
	}

	s.toFollow = map[int64]followed{}

	return out
}

// Follower follows each release that reached GitHub until its workflow publishes it, the
// workflow fails, or the follow reaches its limits (§3.4.9): it polls GitHub a minute after
// the follow starts, then every three minutes, at most FollowPolls times over FollowFor.
// Each poll's count, and how the follow ended, are written into the release's record, so
// the run's result and Recent say so, and a follow cut off by a quit resumes on the next
// start within the same limits. A release published, or whose workflow failed, is told to
// the user (release_published, release_failed); one that reached the limits only says so
// in its result. The desktop shell runs it as deferred work, from the first paint until
// quit; it is not bound.
type Follower struct {
	s *Service

	// first is how long after a follow starts, or resumes, it first polls, and every how
	// long after it polls again: FollowFirst and FollowEvery, which tests shorten.
	first time.Duration
	every time.Duration

	// mu guards seen: the follows started in this run of the app, so one handed over twice,
	// resumed and recorded meanwhile, is followed once.
	mu   sync.Mutex
	seen map[followKey]bool

	// wg tracks the follows going.
	wg sync.WaitGroup
}

// NewFollower returns the worker that follows s's releases on GitHub.
func NewFollower(s *Service) *Follower {
	return &Follower{s: s, first: FollowFirst, every: FollowEvery, seen: map[followKey]bool{}}
}

// followKey names a follow: its release's run, and when the follow started, since a run's
// ID can be handed out again once the history forgets the run.
type followKey struct {
	id    int64
	since string
}

// Run resumes the follows the releases' records say go on, then follows each release
// handed to it as it ends, until ctx ends, and returns once every follow has stopped. A
// follow the quit cut off stays recorded as going on, for the next start. Without GitHub's
// workflows or the runs' records, it follows nothing.
func (f *Follower) Run(ctx context.Context) {
	if f.s.workflows == nil || f.s.records == nil {
		return
	}

	defer f.wg.Wait()

	f.resume(ctx)

	for {
		for _, fl := range f.s.takeFollows() {
			f.start(ctx, fl)
		}

		select {
		case <-ctx.Done():
			return
		case <-f.s.wakeFollow:
		}
	}
}

// resume follows again each release whose record says its follow goes on.
func (f *Follower) resume(ctx context.Context) {
	recs, err := f.s.records.Ended(ctx, scriptrun.KindRelease)
	if err != nil {
		if ctx.Err() == nil {
			f.s.logger.WarnContext(ctx, "couldn't read the releases to follow on GitHub", "error", err)
		}

		return
	}

	for _, rec := range recs {
		res, err := decodeResult(rec.Result.Outcome)
		if err != nil || res.Follow == nil || res.Follow.State != FollowFollowing {
			continue
		}

		f.start(ctx, followed{id: rec.ID, name: rec.Repo, res: res})
	}
}

// start follows fl, unless it is followed already.
func (f *Follower) start(ctx context.Context, fl followed) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := followKey{id: fl.id, since: fl.res.Follow.Since}
	if f.seen[key] {
		return
	}

	f.seen[key] = true
	f.wg.Add(1)

	go func() {
		defer f.wg.Done()

		f.follow(ctx, fl)
	}()
}

// follow polls GitHub for fl's workflow until it is published, it failed, or the follow
// reaches its limits, writing each poll into the release's record and telling the user of
// the end. It returns early when ctx ends, the record is gone, or the record no longer
// says this follow goes on.
func (f *Follower) follow(ctx context.Context, fl followed) {
	fw := *fl.res.Follow

	until, err := time.Parse(time.RFC3339, fw.Until)
	if err != nil {
		until = time.Time{} // a limit that can't be read is reached
	}

	for wait := f.first; ; wait = f.every {
		now := f.s.clock.Now()
		if fw.Polls >= FollowPolls || !now.Before(until) {
			f.stop(ctx, fl, fw.ended(FollowTimedOut, fl.res.WorkflowURL, earlier(now, until)))

			return
		}

		if !sleep(ctx, min(wait, until.Sub(now))) {
			return
		}

		var goOn bool
		if fw, goOn = f.poll(ctx, fl, fw, until); !goOn {
			return
		}
	}
}

// poll asks GitHub once how fl's workflow goes, and writes the answer into its record. It
// returns the follow as it stands then, and whether it goes on: not once it ended, the quit
// cut the poll off (the next start polls again), or the record says otherwise. A record that
// couldn't be written leaves the follow going, the poll counted, for the next poll to decide
// again and write.
func (f *Follower) poll(ctx context.Context, fl followed, fw Follow, until time.Time) (Follow, bool) {
	s := f.s

	got, err := s.workflows.ReleaseWorkflow(ctx, fl.name, fl.res.Tag, fl.res.Commit)
	if ctx.Err() != nil {
		return fw, false
	}

	if err != nil {
		s.logger.WarnContext(ctx, "couldn't read a release's workflow on GitHub", "repo", fl.name.String(),
			"tag", fl.res.Tag, "error", err)
	}

	next := fw.polled(got, err, fl.res, s.clock.Now(), until)
	written, goOn := f.record(ctx, fl, next)

	switch {
	case !goOn:
		return fw, false
	case !written:
		fw.Polls = next.Polls

		return fw, true
	case next.State != FollowFollowing:
		f.ended(ctx, fl, next)

		return next, false
	default:
		return next, true
	}
}

// stop records a follow that stopped without a poll, and tells of it.
func (f *Follower) stop(ctx context.Context, fl followed, fw Follow) {
	if written, _ := f.record(ctx, fl, fw); written {
		f.ended(ctx, fl, fw)
	}
}

// record writes fw into fl's record, as long as the record still says this follow goes
// on. It reports whether it wrote it, and whether the follow goes on: not once the history
// no longer holds the run, nor once its record says otherwise, as after the user cleared
// it or another follow of it ended. A record that couldn't be written is logged, and the
// follow goes on.
func (f *Follower) record(ctx context.Context, fl followed, fw Follow) (written, goOn bool) {
	changed, err := f.s.records.Amend(ctx, fl.id, func(rec scriptrun.Record) (scriptrun.Amendment, bool) {
		res, err := decodeResult(rec.Result.Outcome)
		if err != nil || res.Follow == nil || res.Follow.State != FollowFollowing || res.Follow.Since != fw.Since {
			return scriptrun.Amendment{}, false
		}

		res.Follow = &fw

		return scriptrun.Amendment{Summary: followedSummary(res), Outcome: res}, true
	})

	switch {
	case errors.Is(err, scriptrun.ErrNotFound):
		return false, false
	case err != nil:
		if ctx.Err() == nil {
			f.s.logger.WarnContext(ctx, "couldn't record how following a release goes", "run", fl.id, "error", err)
		}

		return false, ctx.Err() == nil
	default:
		return changed, changed
	}
}

// ended logs a follow that stopped, and tells the user of a release published or whose
// workflow failed, once the record holds it: a follow whose record failed is followed
// again, and told then, so nothing is told twice. A follow that reached its limits is
// told nothing: its result says so.
func (f *Follower) ended(ctx context.Context, fl followed, fw Follow) {
	s := f.s
	what := fl.name.String() + " " + fl.res.Tag

	s.logger.InfoContext(ctx, "stopped following a release's workflow", "repo", fl.name.String(), "tag", fl.res.Tag,
		"state", string(fw.State), "polls", fw.Polls)

	if s.notifier == nil {
		return
	}

	switch fw.State {
	case FollowPublished:
		s.notifier.Published(ctx, Notice{Repo: fl.name, Title: what + " is published", URL: fw.URL})
	case FollowFailed:
		s.notifier.WorkflowFailed(ctx, Notice{Repo: fl.name, Title: "The release workflow failed for " + what, URL: fw.URL})
	case FollowFollowing, FollowTimedOut:
	}
}

// polled works out the follow after one poll's answer, at now: published or failed when
// the answer says so, stopped at its limits when this was the last poll, and going on
// otherwise. A poll that failed counts all the same.
func (fw Follow) polled(w Workflow, err error, res Result, now, until time.Time) Follow {
	fw.Polls++

	switch {
	case err == nil && w.Published:
		return fw.ended(FollowPublished, cmp.Or(w.ReleaseURL, res.TagURL), now)
	case err == nil && w.Failed:
		return fw.ended(FollowFailed, cmp.Or(w.RunURL, res.WorkflowURL), now)
	case fw.Polls >= FollowPolls || !now.Before(until):
		return fw.ended(FollowTimedOut, res.WorkflowURL, now)
	default:
		return fw
	}
}

// ended returns the follow stopped in state at at, its line linking to url.
func (fw Follow) ended(state FollowState, url string, at time.Time) Follow {
	fw.State, fw.URL, fw.Ended = state, url, clock.Format(at)

	if since, err := time.Parse(time.RFC3339, fw.Since); err == nil && at.After(since) {
		fw.Minutes = int(at.Sub(since).Round(time.Minute) / time.Minute)
	}

	return fw
}

// followedSummary says, in a sentence, how a release on GitHub came out as its follow
// stands: published, its workflow failed, or else on GitHub.
func followedSummary(res Result) string {
	if res.Follow != nil {
		switch res.Follow.State {
		case FollowPublished:
			return res.Tag + " is published."
		case FollowFailed:
			return res.Tag + " is on GitHub, but its release workflow failed."
		case FollowFollowing, FollowTimedOut:
		}
	}

	return summaryOf(res, scriptrun.Outcome{})
}

// earlier returns the earlier of a and b, or a when b is the zero time.
func earlier(a, b time.Time) time.Time {
	if !b.IsZero() && b.Before(a) {
		return b
	}

	return a
}

// sleep waits d, and reports whether it did: not when ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
