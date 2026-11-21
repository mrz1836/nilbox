package inbox

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// The follow of a merge. A pull request merged into a branch changes whether every other
// open pull request on that branch merges cleanly, and GitHub works that out again only
// once it is asked, answering UNKNOWN for a few seconds after the branch moved. The
// inbox's whole refresh, every five minutes, would leave their cards saying what was true
// before the merge, so a merge the inbox learns of has the repository's other open pull
// requests on that branch read again, a few seconds later, and again while GitHub is
// still working one out. It costs about a point per followBatch of them a round, and
// nothing while no merge happened.
const (
	// mergeSettle is how long after a merge is noted its follow reads the other pull
	// requests: long enough for GitHub to have started working them out again.
	mergeSettle = 10 * time.Second

	// mergeRetry is how long after a round that left GitHub still working one out, or
	// that failed, the next reads again, and mergeRounds the most rounds one follow takes.
	mergeRetry  = 30 * time.Second
	mergeRounds = 3

	// mergeRecent is how long a merged pull request is remembered, so a read that sees it
	// merged again, as the review sheet's after the user's own merge or a whole refresh's
	// while GitHub's search index trails it, starts no other follow.
	mergeRecent = time.Hour

	// mergeJob names the follow's rounds in log entries.
	mergeJob = "follow a merge"

	// mergeableUnknown is GitHub's mergeable while it is still working it out.
	mergeableUnknown = "UNKNOWN"
)

// mergeKey names a repository of one account a merge went into, its name lower-cased, as
// GitHub matches names.
type mergeKey struct {
	accountID int64
	repo      string
}

// mergeKeyOf returns the key of the repository name of the account with accountID.
func mergeKeyOf(accountID int64, name repo.Name) mergeKey {
	return mergeKey{accountID: accountID, repo: strings.ToLower(name.String())}
}

// mergeNote is the follow a repository's merges are owed.
type mergeNote struct {
	// repo is the repository, as first noted.
	repo repo.Name

	// due is when the next round reads, and since when the latest merge's first round
	// was due: a pull request read before it is read again.
	due, since time.Time

	// bases are the branches merged into, and anyBase is set when a merge's wasn't known.
	bases   map[string]bool
	anyBase bool

	// rounds counts the rounds read since the latest merge, and gen the merges noted, so a
	// round that ends never undoes a merge noted while it ran.
	rounds int
	gen    int
}

// mergeFollow is one round a mergeNote is due, as the round reads it.
type mergeFollow struct {
	key     mergeKey
	gen     int
	repo    repo.Name
	since   time.Time
	bases   []string
	anyBase bool
}

// mergeQueue holds the follows owed, which noteMerged adds to and the MergeFollower takes
// as they come due. It is safe for concurrent use.
type mergeQueue struct {
	// wake tells the MergeFollower a follow was noted.
	wake chan struct{}

	mu     sync.Mutex
	notes  map[mergeKey]*mergeNote
	recent map[string]time.Time
}

// newMergeQueue returns an empty mergeQueue.
func newMergeQueue() *mergeQueue {
	return &mergeQueue{wake: make(chan struct{}, 1), notes: map[mergeKey]*mergeNote{}, recent: map[string]time.Time{}}
}

// add owes the repository name of the account with accountID a follow of the merge of
// the pull request with nodeID into base ("" when not known, which reads every base),
// noted at now, and reports whether it did. A pull request noted within mergeRecent is
// noted once. A merge into a repository already owed a follow joins it, its first round
// due mergeSettle after the latest merge, so two merges in a row share their rounds. It
// never waits.
func (q *mergeQueue) add(nodeID string, accountID int64, name repo.Name, base string, now time.Time) bool {
	if !name.Valid() || !q.remember(nodeID, now) {
		return false
	}

	q.mu.Lock()

	key := mergeKeyOf(accountID, name)

	n := q.notes[key]
	if n == nil {
		n = &mergeNote{repo: name, bases: map[string]bool{}}
		q.notes[key] = n
	}

	n.due = now.Add(mergeSettle)
	n.since = n.due
	n.rounds = 0
	n.gen++

	if base == "" {
		n.anyBase = true
	} else {
		n.bases[base] = true
	}

	q.mu.Unlock()

	select {
	case q.wake <- struct{}{}:
	default:
	}

	return true
}

// remember records the merge of the pull request with nodeID, seen at now, and reports
// whether it is new: not seen within mergeRecent. A merge with no node ID is always new.
func (q *mergeQueue) remember(nodeID string, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	for id, at := range q.recent {
		if now.Sub(at) >= mergeRecent || at.After(now) {
			delete(q.recent, id)
		}
	}

	if nodeID == "" {
		return true
	}

	if _, seen := q.recent[nodeID]; seen {
		return false
	}

	q.recent[nodeID] = now

	return true
}

// due returns the follows due at now, in the order of their keys.
func (q *mergeQueue) due(now time.Time) []mergeFollow {
	q.mu.Lock()
	defer q.mu.Unlock()

	var out []mergeFollow

	for _, key := range slices.SortedFunc(maps.Keys(q.notes), compareKeys) {
		n := q.notes[key]
		if n.due.After(now) {
			continue
		}

		out = append(out, mergeFollow{
			key: key, gen: n.gen, repo: n.repo, since: n.since, bases: slices.Sorted(maps.Keys(n.bases)),
			anyBase: n.anyBase,
		})
	}

	return out
}

// compareKeys orders merge keys by account, then repository.
func compareKeys(a, b mergeKey) int {
	return cmp.Or(cmp.Compare(a.accountID, b.accountID), strings.Compare(a.repo, b.repo))
}

// done records that a round of the follow f ended at now: another round is due mergeRetry
// later when again says so and the follow has rounds left, else the follow is over. A
// merge noted while the round ran keeps its own round.
func (q *mergeQueue) done(f mergeFollow, again bool, now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()

	n := q.notes[f.key]
	if n == nil || n.gen != f.gen {
		return
	}

	n.rounds++

	if again && n.rounds < mergeRounds {
		n.due = now.Add(mergeRetry)

		return
	}

	delete(q.notes, f.key)
}

// next says how long after now the next follow comes due, and false when none is owed.
func (q *mergeQueue) next(now time.Time) (time.Duration, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	var (
		soonest time.Time
		found   bool
	)

	for _, n := range q.notes {
		if !found || n.due.Before(soonest) {
			soonest, found = n.due, true
		}
	}

	if !found {
		return 0, false
	}

	return max(soonest.Sub(now), 0), true
}

// noteMerged owes the merge of the pull request pull, with nodeID, into base ("" when not
// known), for the account with accountID, a follow of the other pull requests in its
// repository (see mergeQueue.add) and of the issues it closes (see closeQueue.add), each
// merge once. It never waits.
func (s *Service) noteMerged(nodeID string, accountID int64, pull item.Ref, base string) {
	now := s.clock.Now().UTC()

	if s.merges.add(nodeID, accountID, pull.Repo, base, now) {
		s.closes.add(accountID, pull, now)
	}
}

// noteMergedRead notes the merge a stored read of the item ref shows, as w wrote it.
func (s *Service) noteMergedRead(w itemWrite, ref item.Ref) {
	if w.update == nil || w.update.update == nil {
		return
	}

	u := w.update.update
	if !u.Found || !u.Item.IsPR || u.Item.State != github.StateMerged {
		return
	}

	s.noteMerged(u.Item.NodeID, w.update.accountID, ref, u.Details.BaseRef)
}

// noteMergedClosed notes the merges among a whole refresh's items that GitHub's search
// still listed though they merged, for the account with accountID, each into its base as
// the refresh's details read it, or any base.
func (s *Service) noteMergedClosed(accountID int64, got refreshed) {
	for _, it := range got.closed {
		if !it.IsPR || it.State != github.StateMerged {
			continue
		}

		var base string
		if got.details != nil {
			base = got.details.Items[it.NodeID].BaseRef
		}

		s.noteMerged(it.NodeID, accountID, it.Ref(), base)
	}
}

// followMerge reads again the open pull requests the merges f follows may have changed,
// as the checks follower reads its own (followRows), and reports whether an answer left
// GitHub still working one out. Once it has let go of them, it tells the outcome sink of
// each merge or close an answer it stored shows, and the Changed seam the items it stored.
func (s *Service) followMerge(ctx context.Context, f mergeFollow) (bool, error) {
	ctx = github.WithPurpose(ctx, github.PurposeFollower)

	res, err := s.mergeRound(ctx, f)
	s.settle(ctx, res.told)
	s.tellChanged(res.wrote)

	return res.unknown, err
}

// mergeRound is followMerge's read and store: the repository's open pull requests on a
// branch merged into (any branch when one wasn't known) not read since the merge, or that
// GitHub was still working out, the listed ones neither dismissed, snoozed, nor muted and
// those a routine's search follows, the first followMax of them.
func (s *Service) mergeRound(ctx context.Context, f mergeFollow) (followResult, error) {
	stored, err := s.prefs.GetMany(ctx, append([]string{prefViewer}, reporules.Keys()...)...)
	if err != nil {
		return followResult{}, fmt.Errorf("read the pull requests a merge into %s changed: %w", f.repo, err)
	}

	login := stored[prefViewer]
	if login == "" {
		return followResult{}, nil
	}

	bases := f.bases
	if len(bases) == 0 {
		bases = []string{""} // never an empty list, which sqlc can't expand
	}

	now := s.clock.Now().UTC()

	rows, err := s.q.ListOpenPullsInRepo(ctx, queries.ListOpenPullsInRepoParams{
		Watched: holdsWatches(s.watchSource), Login: login, AccountID: f.key.accountID, Owner: f.repo.Owner,
		Name: f.repo.Repo, ReadBefore: f.since, Now: &now, AnyBase: f.anyBase, Bases: bases,
	})
	if err != nil {
		return followResult{}, fmt.Errorf("read the pull requests a merge into %s changed: %w", f.repo, err)
	}

	res, err := s.followRows(ctx, login, followable(followRowsOf(rows), reporules.Decode(stored)))
	if err != nil {
		err = fmt.Errorf("follow the merge into %s: %w", f.repo, err)
	}

	return res, err
}

// followRowsOf converts the rows of a query that lists the pull requests a follow reads.
func followRowsOf[R queries.ListRunningChecksRow | queries.ListOpenPullsInRepoRow](rows []R) []followRow {
	out := make([]followRow, 0, len(rows))

	for _, r := range rows {
		out = append(out, followRow(r))
	}

	return out
}

// MergeFollower follows the merges the inbox learns of, as a desktop worker: its own
// merges and Autopilot's through the Actor, and those a read of the inbox finds. It reads
// the issues each merge closes (see followClose) and the other pull requests in its
// repository (see followMerge). It isn't bound, and neither is what it calls. Each
// follow's rounds run one at a time, the issues' first; a round that fails or panics is
// logged, and a failed one is tried again within the follow's rounds, so it keeps going
// until its context is canceled.
type MergeFollower struct {
	queue      *mergeQueue
	closes     *closeQueue
	round      func(ctx context.Context, f mergeFollow) (bool, error)
	closeRound func(ctx context.Context, f closeFollow) (bool, error)
	now        func() time.Time
	logger     *slog.Logger
}

// NewMergeFollower returns the MergeFollower of s, logging failures to logger; a nil
// logger discards them.
func NewMergeFollower(s *Service, logger *slog.Logger) *MergeFollower {
	return &MergeFollower{
		queue: s.merges, closes: s.closes, round: s.followMerge, closeRound: s.followClose,
		now: func() time.Time { return s.clock.Now().UTC() }, logger: logging.Or(logger),
	}
}

// Run reads the follows as they come due until ctx is canceled, and returns once the
// round in flight has.
func (f *MergeFollower) Run(ctx context.Context) {
	for ctx.Err() == nil {
		f.pass(ctx)

		wait, timed := f.next()
		if !f.sleep(ctx, wait, timed) {
			return
		}
	}
}

// next says how long until the next follow of either kind comes due, and false when none
// is owed.
func (f *MergeFollower) next() (time.Duration, bool) {
	now := f.now()
	wait, timed := f.queue.next(now)

	if soon, ok := f.closes.next(now); ok && (!timed || soon < wait) {
		wait, timed = soon, true
	}

	return wait, timed
}

// pass runs a round of each follow due now: the issues merges close first, which their
// cards wait on, then the pull requests merges changed.
func (f *MergeFollower) pass(ctx context.Context) {
	for _, c := range f.closes.due(f.now()) {
		if ctx.Err() != nil {
			return
		}

		again := runRound(ctx, f.logger, closeJob, c, f.closeRound, "merged", c.pull.String())
		f.closes.done(c, again, f.now())
	}

	for _, m := range f.queue.due(f.now()) {
		if ctx.Err() != nil {
			return
		}

		again := runRound(ctx, f.logger, mergeJob, m, f.round, "repo", m.repo.String())
		f.queue.done(m, again, f.now())
	}
}

// runRound runs one round of the follow it through round, logging a failure or a panic as
// job's, with attrs, and reports whether the follow reads again: round said so (GitHub was
// still working out a pull request, or an issue was still open), or the round failed.
func runRound[F any](
	ctx context.Context, logger *slog.Logger, job string, it F, round func(context.Context, F) (bool, error),
	attrs ...any,
) (again bool) {
	defer func() {
		if p := recover(); p != nil {
			logger.ErrorContext(ctx, job+" panicked", append(attrs, "panic", p)...)

			again = false
		}
	}()

	more, err := round(ctx, it)

	switch {
	case err == nil:
		return more
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		return false
	default:
		logger.WarnContext(ctx, job+" failed", append(attrs, "error", err)...)

		return true
	}
}

// sleep waits for wait, when timed, or for a follow noted, and reports false once ctx is
// canceled.
func (f *MergeFollower) sleep(ctx context.Context, wait time.Duration, timed bool) bool {
	var due <-chan time.Time

	if timed {
		timer := time.NewTimer(wait)
		defer timer.Stop()

		due = timer.C
	}

	select {
	case <-ctx.Done():
		return false
	case <-due:
	case <-f.queue.wake:
	}

	return true
}
