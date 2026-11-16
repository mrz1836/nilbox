package inbox

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/periodic"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// The follow of the pull requests whose checks are running: GitHub's own page updates
// as the checks end, and the inbox's five-minute refresh would leave the card behind.
const (
	// FollowInterval is how often, unless the settings say otherwise, the follow reads
	// again the pull requests whose checks were still running. A round asks GitHub one
	// request per followBatch of them, about a point each, and nothing while none runs: at
	// most some 160 points an hour, three percent of the GraphQL budget, and only while
	// checks run.
	FollowInterval = 45 * time.Second

	// followFresh is how recently a pull request's details must have been read for a
	// round to leave it alone, as after a whole refresh or the round before: a little less
	// than the shortest interval the follow can be set to, so the round after reads it
	// again.
	followFresh = 25 * time.Second

	// followMax is the most pull requests one round reads: the listed ones first, then
	// those a routine's search follows, each the longest unread first.
	followMax = 50

	// followBatch is the most pull requests one of a round's requests reads, side by side.
	// GitHub works out each one's merge state as it answers, and four requests of 25 pull
	// requests it hadn't read lately took up to 6 seconds of its 10 (measured in November
	// 2026); the follow's were read a round ago, so a batch of 25 answers well within it.
	followBatch = 25

	// followJob names the follow's rounds in log entries.
	followJob = "follow running checks"
)

// refreshRunning reads again, in requests of followBatch sent side by side, the listed pull
// requests whose checks were still running when the inbox last read them, neither
// dismissed nor snoozed, nor of a repository or org the user muted, and the ones a
// routine's search follows, muted or not, in the copy that reads the routines' searches
// alone (so in none while Autopilot is off), and stores each as RefreshItem would, so a card
// shows its checks end about as soon as GitHub's own page does, a routine sees them settle
// within about a round, and one GitHub no longer asks the user to review leaves Review
// requested, telling no outcome. A pull request whose checks end, pass or fail, is left
// alone from then on. It asks GitHub nothing while none runs, and leaves alone one read in
// the last little while. A pull request GitHub doesn't answer for, or whose request
// failed, is left as it was, for the next round or whole refresh. Once it has let go of
// every pull request, it tells the outcome sink of each merge or close an answer it stored
// shows, and the Changed seam the items it stored.
func (s *Service) refreshRunning(ctx context.Context) error {
	ctx = github.WithPurpose(ctx, github.PurposeFollower)

	told, stored, err := s.followRound(ctx)
	s.settle(ctx, told)
	s.tellChanged(stored)

	return err
}

// followRound is refreshRunning under the pull requests' locks. It returns what to tell
// once they are let go, and the items it stored, the stores made before a failure
// included.
func (s *Service) followRound(ctx context.Context) ([]Outcome, []int64, error) {
	stored, err := s.prefs.GetMany(ctx, append([]string{prefViewer}, reporules.Keys()...)...)
	if err != nil {
		return nil, nil, fmt.Errorf("read the pull requests whose checks run: %w", err)
	}

	now := s.clock.Now().UTC()
	login := stored[prefViewer]

	// The pull requests a routine's search follows only while this copy reads the
	// routines' searches: none does while Autopilot is off, and what is stored of them then
	// is stale.
	listed, err := s.q.ListRunningChecks(ctx, queries.ListRunningChecksParams{
		Watched: holdsWatches(s.watchSource), Login: login, ReadBefore: now.Add(-followFresh), Now: &now,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("read the pull requests whose checks run: %w", err)
	}

	running := followable(listed, reporules.Decode(stored))

	if len(running) == 0 {
		return nil, nil, nil
	}

	// Each pull request waits for other work on it, as RefreshItem does, in one order, so
	// two rounds never hold each other up.
	slices.SortFunc(running, func(a, b queries.ListRunningChecksRow) int { return cmp.Compare(a.ID, b.ID) })

	unlock, err := s.lockAll(ctx, running)
	if err != nil {
		return nil, nil, fmt.Errorf("follow the running checks: %w", err)
	}

	defer unlock()

	told, wrote, err := s.followRunning(ctx, login, running)
	if err != nil {
		err = fmt.Errorf("follow the running checks: %w", err)
	}

	// What a round stored, a part of it when a request failed, reaches the window.
	if len(wrote) > 0 {
		if _, changedErr := s.changed(ctx); changedErr != nil {
			err = errors.Join(err, changedErr)
		}
	}

	return told, wrote, err
}

// followable returns the first followMax of rows, the listed pull requests first and then
// those only a routine's search follows, each in rows' order, leaving out the listed pull
// requests of the repositories and orgs rules mutes. A pull request a routine's search
// follows is kept muted or not, since a routine watches a muted repository on purpose, but
// it comes after the inbox's own cards.
func followable(rows []queries.ListRunningChecksRow, rules reporules.Rules) []queries.ListRunningChecksRow {
	listed := make([]queries.ListRunningChecksRow, 0, len(rows))
	followed := make([]queries.ListRunningChecksRow, 0, len(rows))

	for _, r := range rows {
		muted := rules.IsMuted(repo.Name{Owner: r.RepoOwner, Repo: r.RepoName})

		switch {
		case r.Listed && !muted:
			listed = append(listed, r)
		case r.Followed:
			followed = append(followed, r)
		}
	}

	running := slices.Concat(listed, followed)

	return running[:min(len(running), followMax)]
}

// lockAll takes each pull request's lock, in running's order, and returns what lets go of
// them all; when one can't be taken, it lets go of those it took.
func (s *Service) lockAll(ctx context.Context, running []queries.ListRunningChecksRow) (func(), error) {
	unlocks := make([]func(), 0, len(running))
	release := func() {
		for _, unlock := range slices.Backward(unlocks) {
			unlock()
		}
	}

	for _, r := range running {
		unlock, err := s.items.lock(ctx, r.ID)
		if err != nil {
			release()

			return nil, err
		}

		unlocks = append(unlocks, unlock)
	}

	return release, nil
}

// followed is what one round read: GitHub's answer, and each pull request's file check.
type followed struct {
	updates *github.ItemUpdates
	files   map[int64]map[string]fileCheck
}

// followRunning asks GitHub about running, the pull requests of the account login, and
// stores each pull request it answered for. It returns the ones it stored, and what the
// answers it stored tell (see seenClosed), those stored before a failure included.
func (s *Service) followRunning(
	ctx context.Context, login string, running []queries.ListRunningChecksRow,
) ([]Outcome, []int64, error) {
	last, err := s.projectAccess(ctx)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]string, 0, len(running))
	for _, r := range running {
		ids = append(ids, r.NodeID)
	}

	var seq uint64

	got, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (followed, error) {
		seq = s.startFetch() // each try numbers its own fetch

		return s.fetchRunning(ctx, tok, running, ids, readOptions(last))
	})
	if got.updates == nil {
		return nil, nil, err
	}

	// The requests that were answered are stored, whatever became of the others.
	told, wrote, storeErr := s.storeFollowed(ctx, login, running, got, seq)

	return told, wrote, errors.Join(err, storeErr)
}

// storeFollowed stores what a round read, fetched as seq, for each pull request of running
// GitHub answered for, as followRunning says.
func (s *Service) storeFollowed(
	ctx context.Context, login string, running []queries.ListRunningChecksRow, got followed, seq uint64,
) ([]Outcome, []int64, error) {
	var (
		told  []Outcome
		wrote []int64
	)

	for _, r := range running {
		update, ok := got.updates.Items[r.NodeID]
		if !ok {
			continue
		}

		w := itemWrite{id: r.ID, seq: seq, update: &itemUpdate{
			accountID: r.AccountID, login: login, update: &update, files: got.files[r.ID],
		}}

		stored, err := s.storeItemWrite(ctx, w)
		if err != nil {
			return told, wrote, fmt.Errorf("save the item: %w", err)
		}

		if stored {
			ref := refOf(r.RepoOwner, r.RepoName, r.Number, item.KindPR)
			told = append(told, seenClosed(w, login, ref, s.now())...)
			wrote = append(wrote, r.ID)
		}
	}

	return told, wrote, nil
}

// fetchRunning reads running from GitHub with tok, followBatch at a time side by side, and
// checks the files of each one whose head moved since its files were last checked. A
// request that fails leaves its pull requests out, and its error is returned beside what
// the others read; a token GitHub refused fails the whole, to be borrowed again.
func (s *Service) fetchRunning(
	ctx context.Context, tok github.Token, running []queries.ListRunningChecksRow, ids []string,
	opts github.DetailOptions,
) (followed, error) {
	updates, err := s.fetchFollowed(ctx, tok, ids, opts)
	if updates == nil {
		return followed{}, err
	}

	out := followed{updates: updates, files: make(map[int64]map[string]fileCheck, len(running))}

	for _, r := range running {
		update, ok := updates.Items[r.NodeID]
		if !ok {
			continue
		}

		files, filesErr := s.checkUpdatedFiles(ctx, tok, r.ID, &update)
		if filesErr != nil {
			return followed{}, filesErr
		}

		out.files[r.ID] = files
	}

	return out, err
}

// fetchFollowed sends ids in requests of followBatch, side by side, and merges what they
// read: each pull request answered, whether a request read without the projects, and the
// latest budget reading. It returns nil with the error when no request was answered, or
// one was refused for its token, and what the answered ones read beside the others'
// errors otherwise.
func (s *Service) fetchFollowed(
	ctx context.Context, tok github.Token, ids []string, opts github.DetailOptions,
) (*github.ItemUpdates, error) {
	batches := slices.Collect(slices.Chunk(ids, followBatch))
	answers := make([]*github.ItemUpdates, len(batches))
	errs := make([]error, len(batches))

	var wg sync.WaitGroup

	for i, batch := range batches {
		wg.Go(func() {
			answers[i], errs[i] = s.client.FetchItems(ctx, tok, batch, opts)
		})
	}

	wg.Wait()

	var out *github.ItemUpdates

	for i, a := range answers {
		if github.IsBadCredentials(errs[i]) {
			return nil, errs[i]
		}

		if errs[i] == nil && a != nil {
			out = mergeUpdates(out, a)
		}
	}

	return out, errors.Join(errs...)
}

// mergeUpdates adds one request's answer to what the round read so far (nil before any):
// its pull requests, whether it read without the projects, and its budget reading when
// it is the latest.
func mergeUpdates(out, a *github.ItemUpdates) *github.ItemUpdates {
	if out == nil {
		out = &github.ItemUpdates{Items: make(map[string]github.ItemUpdate, len(a.Items)), RateLimit: a.RateLimit}
	} else if a.RateLimit.Later(out.RateLimit) {
		out.RateLimit = a.RateLimit
	}

	maps.Copy(out.Items, a.Items)
	out.ProjectsRefused = out.ProjectsRefused || a.ProjectsRefused

	return out
}

// ChecksFollower reads again, as often as its every says (the user's setting,
// FollowInterval when nil; off at zero), the pull requests whose checks are running
// (refreshRunning), as a desktop worker. Wake makes it read the setting again at once. It
// isn't bound, and neither is what it calls: only it follows the checks. A round that arrives while one is still
// running is skipped; failures are logged, and a panicking round is recovered, so it keeps
// going until its context is canceled.
type ChecksFollower struct {
	follow func(ctx context.Context) error
	every  periodic.Every
	wake   chan struct{}
	logger *slog.Logger
}

// NewChecksFollower returns a ChecksFollower over s, as often as every says, logging
// failures to logger. A nil every follows every FollowInterval, and a nil logger discards
// the failures.
func NewChecksFollower(s *Service, every periodic.Every, logger *slog.Logger) *ChecksFollower {
	return newChecksFollower(s.refreshRunning, every, logger)
}

// newChecksFollower returns a ChecksFollower that follows through follow.
func newChecksFollower(follow func(ctx context.Context) error, every periodic.Every, logger *slog.Logger,
) *ChecksFollower {
	if every == nil {
		every = func(context.Context) time.Duration { return FollowInterval }
	}

	return &ChecksFollower{follow: follow, every: every, wake: make(chan struct{}, 1), logger: logger}
}

// Run follows now and then as often as every says until ctx is canceled, then waits for
// a round in flight to return. Its first round finds nothing to read when a whole refresh
// read every pull request a moment before.
func (f *ChecksFollower) Run(ctx context.Context) {
	periodic.RunEvery(ctx, f.every, f.wake, followJob, f.follow, f.logger)
}

// Wake has the follower read how often to follow again, as when the user changed it.
func (f *ChecksFollower) Wake() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}
