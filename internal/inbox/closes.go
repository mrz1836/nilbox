package inbox

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
)

// The follow of the issues a merge closes. A pull request that closes an issue once it
// merges (ClosingPull) has GitHub close the issue a moment after the merge, and the
// inbox's whole refresh, every five minutes, would leave the issue's card saying a fix is
// on its way to an issue already closed. So a merge the inbox learns of has the listed
// issues that name the merged pull request among their closing pull requests read again a
// few seconds later, and again while GitHub still has one open, closeRounds at most; one
// GitHub closed leaves the lists, as any read that finds an item closed takes it out. The
// inbox's own store says which issues name it, so a merge none names asks GitHub nothing,
// and one some name costs about a point a round.
//
// While a merge's follow is owed, every snapshot marks the listed issues that name the
// merged pull request Closing (closeQueue.mark), so their cards say GitHub is closing them
// from the moment the merge is noted, rather than staying as they were for the seconds
// until a round reads them closed. The mark is run state, never stored: a round that finds
// an issue closed takes it out of the lists, and a follow that ends with one still open,
// once it showed one closing, tells the window again (MergeFollower.ended), so the card
// shows the issue as it is.
const (
	// closeSettle is how long after a merge is noted its issues are first read: long
	// enough for GitHub to have closed them, which it does a second or two after the merge.
	closeSettle = 5 * time.Second

	// closeRetry is how long after a round that found an issue still open, or failed, the
	// next reads again, and closeRounds the most rounds one merge's follow takes. An issue
	// still open after them, as one whose pull request merged into a branch other than the
	// default, which GitHub closes nothing from, waits for the whole refresh.
	closeRetry  = 5 * time.Second
	closeRounds = 3

	// closeJob names the follow's rounds in log entries.
	closeJob = "follow the issues a merge closes"
)

// closeFollow is a merge whose issues a round reads: the account it was noted for, and the
// pull request that merged.
type closeFollow struct {
	accountID int64
	pull      item.Ref
}

// closeKey keys a closeFollow: its account, and its pull request's Key, case-folded as
// GitHub matches names.
type closeKey struct {
	accountID int64
	pull      string
}

// key returns f's closeKey.
func (f closeFollow) key() closeKey {
	return closeKey{accountID: f.accountID, pull: f.pull.Key()}
}

// closeNote is the follow a merge's issues are owed: when its next round is due, how many
// rounds it read, and whether the last snapshot read showed one of its issues closing (see
// mark).
type closeNote struct {
	follow closeFollow
	due    time.Time
	rounds int
	shown  bool
}

// closeQueue holds the follows the merges' issues are owed, which noteMerged adds to and the
// MergeFollower takes as they come due. It wakes the MergeFollower on the channel it shares
// with the merge queue. It is safe for concurrent use.
type closeQueue struct {
	wake chan<- struct{}

	mu    sync.Mutex
	notes map[closeKey]*closeNote
}

// newCloseQueue returns an empty closeQueue that wakes its follower on wake.
func newCloseQueue(wake chan<- struct{}) *closeQueue {
	return &closeQueue{wake: wake, notes: map[closeKey]*closeNote{}}
}

// add owes the merge of the pull request pull, for the account with accountID, noted at
// now, a follow of the issues it closes, its first round closeSettle later. A pull request
// without a repository or a number is owed nothing. It never waits.
func (q *closeQueue) add(accountID int64, pull item.Ref, now time.Time) {
	if !pull.Repo.Valid() || pull.Number <= 0 {
		return
	}

	f := closeFollow{accountID: accountID, pull: pull}

	q.mu.Lock()
	q.notes[f.key()] = &closeNote{follow: f, due: now.Add(closeSettle)}
	q.mu.Unlock()

	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// due returns the follows due at now, in the order of their keys.
func (q *closeQueue) due(now time.Time) []closeFollow {
	q.mu.Lock()
	defer q.mu.Unlock()

	var out []closeFollow

	for _, key := range slices.SortedFunc(maps.Keys(q.notes), compareCloseKeys) {
		if n := q.notes[key]; !n.due.After(now) {
			out = append(out, n.follow)
		}
	}

	return out
}

// compareCloseKeys orders close keys by account, then pull request.
func compareCloseKeys(a, b closeKey) int {
	return cmp.Or(cmp.Compare(a.accountID, b.accountID), strings.Compare(a.pull, b.pull))
}

// done records that a round of the follow f ended at now: another round is due closeRetry
// later when again says so and the follow has rounds left, else the follow is over. It
// reports whether the follow is over while the last snapshot read showed one of its issues
// closing, when the window is to be told, so the card shows the issue as it is. A follow
// whose issues all closed tells nothing more: the round's own update took them out.
func (q *closeQueue) done(f closeFollow, again bool, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	n := q.notes[f.key()]
	if n == nil {
		return false
	}

	n.rounds++

	if again && n.rounds < closeRounds {
		n.due = now.Add(closeRetry)

		return false
	}

	delete(q.notes, f.key())

	return n.shown
}

// mark sets Closing on each issue of lists, a snapshot's of the account with accountID,
// whose closing pull requests, as its details were last read, name a merge a follow is
// still owed for, and records which of the account's follows it showed (see done). It
// holds the queue's lock throughout, so no follow ends between finding it owed and
// recording it shown. A list's items are the kept snapshot's, so one it marks an item of
// is copied first. With no follow owed, as almost always, it costs only the lock.
func (q *closeQueue) mark(accountID int64, lists ...*List) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.notes) == 0 || accountID == 0 {
		return
	}

	for _, n := range q.notes {
		if n.follow.accountID == accountID {
			n.shown = false
		}
	}

	for _, l := range lists {
		q.markList(accountID, l)
	}
}

// markList is mark for one list, copying its items before it marks the first. The caller
// holds q.mu.
func (q *closeQueue) markList(accountID int64, l *List) {
	copied := false

	for i, it := range l.Items {
		if !q.closes(accountID, it) {
			continue
		}

		if !copied {
			l.Items, copied = slices.Clone(l.Items), true
		}

		l.Items[i].Closing = true
	}
}

// closes reports whether it is an issue one of whose closing pull requests is the merged
// pull request of a follow still owed for the account with accountID, its repository
// compared ignoring case, and records each such follow as shown. The caller holds q.mu.
func (q *closeQueue) closes(accountID int64, it Item) bool {
	if it.Kind != item.KindIssue || it.Details == nil {
		return false
	}

	found := false

	for _, p := range it.Details.ClosingPulls {
		name, err := repo.Parse(p.Repo)
		if err != nil {
			continue
		}

		pull := item.Ref{Repo: name, Number: p.Number}
		if n := q.notes[closeKey{accountID: accountID, pull: pull.Key()}]; n != nil {
			n.shown, found = true, true
		}
	}

	return found
}

// next says how long after now the next follow comes due, and false when none is owed.
func (q *closeQueue) next(now time.Time) (time.Duration, bool) {
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

// followClose reads again the open listed issues the merge f follows closes, as the merge's
// own follow reads its pull requests (followRows), and reports whether one GitHub answered
// for is still open. Once it has let go of them, it tells the outcome sink of each close an
// answer it stored shows, and the Changed seam the items it stored.
func (s *Service) followClose(ctx context.Context, f closeFollow) (bool, error) {
	ctx = github.WithPurpose(ctx, github.PurposeFollower)

	res, err := s.closeRound(ctx, f)
	s.settle(ctx, res.told)
	s.tellChanged(res.wrote)

	return res.open, err
}

// closeRound is followClose's read and store: the open listed issues of f's account,
// whatever their triage, since a closed one leaves every tab, whose closing pull requests,
// as their details were last read, name f's pull request.
func (s *Service) closeRound(ctx context.Context, f closeFollow) (followResult, error) {
	login, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil {
		return followResult{}, fmt.Errorf("read the issues %s closes: %w", f.pull, err)
	}

	if login == "" {
		return followResult{}, nil
	}

	rows, err := s.q.ListClosingIssues(ctx, queries.ListClosingIssuesParams{Login: login, AccountID: f.accountID})
	if err != nil {
		return followResult{}, fmt.Errorf("read the issues %s closes: %w", f.pull, err)
	}

	closing, err := closedBy(rows, f.pull)
	if err != nil {
		return followResult{}, fmt.Errorf("read the issues %s closes: %w", f.pull, err)
	}

	res, err := s.followRows(ctx, login, closing)
	if err != nil {
		err = fmt.Errorf("follow the issues %s closes: %w", f.pull, err)
	}

	return res, err
}

// closedBy returns the issues of rows whose closing pull requests name pull, its repository
// compared ignoring case, as a follow reads them.
func closedBy(rows []queries.ListClosingIssuesRow, pull item.Ref) ([]followRow, error) {
	var out []followRow

	for _, r := range rows {
		pulls, err := store.DecodeJSON[[]storedClosingPull](r.ClosingPulls)
		if err != nil {
			return nil, fmt.Errorf("read the pull requests that close issue %d: %w", r.ID, err)
		}

		names := func(p storedClosingPull) bool {
			return p.Number == pull.Number && strings.EqualFold(p.Repo, pull.Repo.String())
		}

		if slices.ContainsFunc(pulls, names) {
			out = append(out, followRow{
				ID: r.ID, AccountID: r.AccountID, NodeID: r.NodeID, Number: r.Number, RepoOwner: r.RepoOwner,
				RepoName: r.RepoName, Listed: r.Listed, Followed: r.Followed, Kind: r.Kind,
			})
		}
	}

	return out, nil
}
