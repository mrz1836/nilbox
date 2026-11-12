package release

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// The limits of following a release's workflow on GitHub (§3.4.9): the first poll a
// minute after the follow starts, or resumes after a restart, then one every three
// minutes, at most FollowPolls of them over FollowFor, whichever comes first. Each poll
// is one GraphQL query of one point and, until the release is published, one REST request,
// so a release's follow spends at most 20 of each.
const (
	FollowFor   = 45 * time.Minute
	FollowPolls = 20
	FollowFirst = time.Minute
	FollowEvery = 3 * time.Minute
)

const (
	// followNoRunsPolls is the poll by which a tag with no workflow run of its own, and no
	// release published, stops the follow: the third, about seven minutes in.
	followNoRunsPolls = 3

	// recordTries is how many times a follow that ended tries to write that into its
	// release's record, recordRetry apart and doubling, before it leaves the follow to the
	// next start.
	recordTries = 4
	recordRetry = time.Second
)

// FollowState is where following a release's workflow on GitHub is.
type FollowState string

// The states of a follow.
const (
	// FollowFollowing is a follow going on, within its limits.
	FollowFollowing FollowState = "following"

	// FollowPublished is a release GitHub published: it has it, published, and not a draft.
	FollowPublished FollowState = "published"

	// FollowFailed is a release one of whose tag's workflow runs failed, once all of them
	// ended without it being published.
	FollowFailed FollowState = "failed"

	// FollowNoWorkflow is a tag no workflow ran for by the third poll, with no release
	// published: there's nothing to follow.
	FollowNoWorkflow FollowState = "noWorkflow"

	// FollowUnpublished is a tag whose workflow runs all ended well, with no release
	// published a poll later.
	FollowUnpublished FollowState = "unpublished"

	// FollowTimedOut is a follow that reached its limits with none of those: nothing is
	// assumed.
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

	// Polls counts the polls it made.
	Polls int `json:"polls"`

	// RunsDone reports the last poll found every one of the tag's runs ended well, and no
	// release published: the next poll that finds no release published either stops the
	// follow (FollowUnpublished).
	RunsDone bool `json:"runsDone"`

	// Workflow is the name of the tag's workflow that failed, for FollowFailed, or "".
	Workflow string `json:"workflow"`

	// URL is the page its line links to: the release's once published; the failed run's,
	// or else the tag's Actions runs, once failed; the repository's releases once the tag's
	// runs ended with none published; the tag's Actions runs once no workflow ran, or it
	// stopped at its limits; and "" while it goes on.
	URL string `json:"url"`
}

// Workflow is how a release's workflow on GitHub goes, as one poll read it.
type Workflow struct {
	// Published reports the release published, and ReleaseURL is its page.
	Published  bool
	ReleaseURL string

	// Runs are the tag's own workflow runs: those a push of the tag started at its commit.
	// They are left unread once the release is published.
	Runs []TagRun
}

// TagRun is one of the tag's own workflow runs.
type TagRun struct {
	// Workflow is its workflow's name, as goreleaser, and URL its page.
	Workflow string
	URL      string

	// Done reports it ended, and Failed that it failed, timed out, or never started.
	Done   bool
	Failed bool
}

// Workflows reads how a release's workflow goes on GitHub: core adapts the GitHub client's
// FetchReleaseProgress, which the API budget counts. commit is the commit the release's tag
// points at on origin.
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

// followLinks are the pages a follow's line links to: the tag's, its Actions runs, and the
// repository's releases, where a draft shows.
type followLinks struct {
	tag      string
	runs     string
	releases string
}

// links returns the pages fl's line links to.
func (fl followed) links() followLinks {
	return followLinks{
		tag: fl.res.TagURL, runs: fl.res.WorkflowURL, releases: "https://github.com/" + fl.name.String() + "/releases",
	}
}

// startFollow returns the follow of res that starts now, or nil for a result that can't
// be followed: one that isn't on GitHub, with no repository, tag, or commit to ask about,
// or a Service that can't follow at all. A follow reads the real clock, for its limits and
// its waits alike (clock.Sleep), since a Clock makes no timers.
func (s *Service) startFollow(name repo.Name, res Result) *Follow {
	if s.workflows == nil || s.records == nil || res.Kind != ResultReleased || !name.Valid() || res.Tag == "" ||
		res.Commit == "" {
		return nil
	}

	now := time.Now()

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

// Follower follows each release that reached GitHub until GitHub publishes it, one of the
// tag's workflows fails, the answer is otherwise settled, or the follow reaches its limits
// (§3.4.9): it polls GitHub a minute after the follow starts, then every three minutes, at
// most FollowPolls times over FollowFor. Each poll's count, and how the follow ended, are
// written into the release's record, so the run's result and Recent say so, and a follow
// cut off by a quit resumes on the next start within the same limits. A release published,
// or whose workflow failed, is told to the user (release_published, release_failed); any
// other end only says so in the result. It reads the real clock throughout; tests run it
// in a synctest bubble. The desktop shell runs it as deferred work, from the first paint
// until quit; it is not bound.
type Follower struct {
	s *Service

	// first is how long after a follow starts, or resumes, it first polls, and every how
	// long after it polls again: FollowFirst and FollowEvery, which tests change.
	first time.Duration
	every time.Duration

	// mu guards seen: the follows going, or ended, in this run of the app, so one handed
	// over twice, resumed and recorded meanwhile, is followed once.
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

// keyOf names fl's follow.
func keyOf(fl followed) followKey {
	return followKey{id: fl.id, since: fl.res.Follow.Since}
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

	key := keyOf(fl)
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

// forget lets go of fl's follow, so the next resume takes it up again.
func (f *Follower) forget(fl followed) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.seen, keyOf(fl))
}

// follow polls GitHub for fl's workflow until the follow ends, writing each poll into the
// release's record, then settles how it ended. It returns early when ctx ends, the record
// is gone, or the record no longer says this follow goes on.
func (f *Follower) follow(ctx context.Context, fl followed) {
	fw, until := *fl.res.Follow, limitOf(fl.res.Follow.Until)

	for wait := f.first; ; wait = f.every {
		now := time.Now()
		if fw.Polls >= FollowPolls || !now.Before(until) {
			f.settle(ctx, fl, fw.ended(FollowTimedOut, fl.links().runs, earlier(now, until)))

			return
		}

		if clock.Sleep(ctx, min(wait, until.Sub(now))) != nil {
			return
		}

		var goOn bool
		if fw, goOn = f.poll(ctx, fl, fw, until); !goOn {
			return
		}

		if fw.State != FollowFollowing {
			f.settle(ctx, fl, fw)

			return
		}
	}
}

// limitOf reads a follow's limit, until; one that can't be read is the zero time, which is
// reached already.
func limitOf(until string) time.Time {
	t, err := time.Parse(time.RFC3339, until)
	if err != nil {
		return time.Time{}
	}

	return t
}

// poll asks GitHub once how fl's workflow goes, and returns the follow after it, and
// whether to go on: not once the quit cut the poll off (the next start polls again), nor
// once the record is gone or says otherwise. A poll that leaves the follow going is written
// into the record; one that couldn't be is logged and counted all the same. One that ends
// the follow is settled by the caller.
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

	next := fw.polled(got, err, fl.links(), time.Now(), until)
	if next.State != FollowFollowing {
		return next, true
	}

	if _, goOn := f.record(ctx, fl, next); !goOn {
		return fw, false
	}

	return next, true
}

// settle writes how fl's follow ended into its record, trying again a few times a moment
// apart without asking GitHub again, and tells of it once the record holds it. A follow
// whose record still couldn't be written is logged and let go of, so the next start, which
// finds the record still following, takes it up again; one the quit cut off is left to
// the next start too.
func (f *Follower) settle(ctx context.Context, fl followed, fw Follow) {
	wait := recordRetry

	for try := 1; ; try++ {
		written, goOn := f.record(ctx, fl, fw)

		switch {
		case written:
			f.ended(ctx, fl, fw)

			return
		case !goOn:
			return
		case try == recordTries:
			f.s.logger.ErrorContext(ctx, "couldn't record how following a release ended; the next start follows it again",
				"run", fl.id, "state", string(fw.State))
			f.forget(fl)

			return
		}

		if clock.Sleep(ctx, wait) != nil {
			return
		}

		wait *= 2
	}
}

// record writes fw into fl's record, as long as the record still says this follow goes
// on. It reports whether it wrote it, and whether the follow goes on: not once the history
// no longer holds the run, nor once its record says otherwise, as after the user cleared
// it or another follow of it ended, nor once ctx ended. A record that couldn't be written
// is logged, and the follow goes on.
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
// again, and told then, so nothing is told twice. The record already holds the outcome, so
// a quit landing now doesn't keep the user from hearing it. Any other end is told nothing:
// its result says so.
func (f *Follower) ended(ctx context.Context, fl followed, fw Follow) {
	s := f.s
	what := fl.name.String() + " " + fl.res.Tag

	s.logger.InfoContext(ctx, "stopped following a release's workflow", "repo", fl.name.String(), "tag", fl.res.Tag,
		"state", string(fw.State), "polls", fw.Polls)

	if s.notifier == nil {
		return
	}

	tell := context.WithoutCancel(ctx)

	switch fw.State {
	case FollowPublished:
		s.notifier.Published(tell, Notice{Repo: fl.name, Title: what + " is published", URL: fw.URL})
	case FollowFailed:
		s.notifier.WorkflowFailed(tell, Notice{Repo: fl.name, Title: failedTitle(fw.Workflow, what), URL: fw.URL})
	case FollowFollowing, FollowNoWorkflow, FollowUnpublished, FollowTimedOut:
	}
}

// failedTitle says that workflow, by name, failed for what, the repository and the tag.
func failedTitle(workflow, what string) string {
	return "The " + cmp.Or(workflow, "release") + " workflow failed for " + what
}

// polled works out the follow after one poll's answer, at now: ended when the answer
// settles it (read), stopped at its limits when this was the last poll, and going on
// otherwise. A poll that failed settles nothing, and counts all the same.
func (fw Follow) polled(w Workflow, err error, links followLinks, now, until time.Time) Follow {
	fw.Polls++

	if err == nil {
		fw = fw.read(w, links, now)
		if fw.State != FollowFollowing {
			return fw
		}
	}

	if fw.Polls >= FollowPolls || !now.Before(until) {
		return fw.ended(FollowTimedOut, links.runs, now)
	}

	return fw
}

// read works out what one answer says, at now: published wins; while one of the tag's
// runs goes on, the follow goes on; once they all ended, one that failed ends it as
// failed, and all of them ending well twice in a row, with no release published, ends it
// as unpublished; and a tag with no run of its own by the third poll ends it as having
// nothing to follow.
func (fw Follow) read(w Workflow, links followLinks, now time.Time) Follow {
	switch {
	case w.Published:
		return fw.ended(FollowPublished, cmp.Or(w.ReleaseURL, links.tag), now)
	case len(w.Runs) == 0:
		fw.RunsDone = false
		if fw.Polls >= followNoRunsPolls {
			return fw.ended(FollowNoWorkflow, links.runs, now)
		}

		return fw
	case slices.ContainsFunc(w.Runs, func(r TagRun) bool { return !r.Done }):
		fw.RunsDone = false

		return fw
	}

	if i := slices.IndexFunc(w.Runs, func(r TagRun) bool { return r.Failed }); i >= 0 {
		fw.Workflow = w.Runs[i].Workflow

		return fw.ended(FollowFailed, cmp.Or(w.Runs[i].URL, links.runs), now)
	}

	if fw.RunsDone {
		return fw.ended(FollowUnpublished, links.releases, now)
	}

	fw.RunsDone = true

	return fw
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
// stands: published, one of its workflows failed, or else on GitHub.
func followedSummary(res Result) string {
	if res.Follow != nil {
		switch res.Follow.State {
		case FollowPublished:
			return res.Tag + " is published."
		case FollowFailed:
			return res.Tag + " is on GitHub, but its " + cmp.Or(res.Follow.Workflow, "release") + " workflow failed."
		case FollowFollowing, FollowNoWorkflow, FollowUnpublished, FollowTimedOut:
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
