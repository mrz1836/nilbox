package release

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/periodic"
)

// The follow of the Release page's repositories whose newest commit's checks are running:
// the list is read again in full only when the user asks, so a row whose checks were
// running when it was read would otherwise say so until then.
const (
	// FollowChecksInterval is how often, unless the settings say otherwise, the follow reads
	// again the listed repositories whose checks were still running: the inbox's cadence
	// (inbox.FollowInterval). A round asks GitHub one status batch for them all and, for
	// those with a version off their newest commit, one comparison batch, about two points,
	// and nothing while none runs.
	FollowChecksInterval = 45 * time.Second

	// followChecksFresh is how recently a repository must have been read for a round to
	// leave it alone, as after a whole refresh, a patch, or the round before: a little less
	// than the shortest interval the follow can be set to, so the round after reads it again.
	followChecksFresh = 25 * time.Second

	// followChecksMax is the most repositories one round reads, the longest unread first:
	// one status batch, 15 to a query, as the client asks them.
	followChecksMax = 15

	// followChecksFor is how long after its newest commit a repository's checks are
	// followed. A commit older than this whose checks still read running, as when a required
	// check is never reported and the rollup stays EXPECTED, isn't followed, so it can't
	// spend the budget for good; Read GitHub again still reads it.
	followChecksFor = 2 * time.Hour

	// followChecksJob names the follow's rounds in log entries.
	followChecksJob = "follow the release checks"
)

// followChecks reads again the listed repositories whose newest commit's checks were still
// running when the list last read them (runningChecks), and replaces their rows in the
// list, quietly: it tells the window nothing while it reads (no event.ReleasesProgress, and
// the list never reads as refreshing), and only once a row the page shows changed does it
// keep the list for the next start and tell the window (event.ReleasesUpdated). A round
// whose checks still run changes nothing. A repository whose checks end, pass or fail, is
// left alone from then on.
//
// It asks GitHub nothing while none runs, and follows only a list a patch could patch
// (patchable): one read from GitHub in this run, from the repository locations as set
// now, current, and whose last read didn't fail; any other is read again in full next
// anyway. Like a patch, it waits for the read of the list under way (startReading). When
// GitHub can't be read, it returns the failure, which its worker logs, and leaves the list
// as it was, current: the next round tries again.
func (s *Service) followChecks(ctx context.Context) error {
	s.readRules(ctx)

	s.mu.Lock()
	none := len(s.runningChecks(s.clock.Now())) == 0
	s.mu.Unlock()

	if none {
		return nil
	}

	if s.startReading(ctx) != nil {
		return nil // the app quits
	}

	defer s.doneReading()

	roots, err := s.clones.Roots(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}

		return fmt.Errorf("follow the release checks: read the repository locations: %w", err)
	}

	root := roots.Key()

	// Picked again: a refresh that landed meanwhile read them already.
	s.mu.Lock()
	keys := s.runningChecks(s.clock.Now())
	listed := s.patchable(root, keys)
	s.mu.Unlock()

	if len(listed) == 0 {
		return nil
	}

	rows, err := s.readRows(ctx, listed, nil)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}

		return fmt.Errorf("follow the release checks: %w", err)
	}

	s.landFollowed(ctx, root, keys, rows)

	return nil
}

// landFollowed replaces the rows of the repositories keys, read again as rows, in the last
// list, and stamps them read now. Only when a row the page shows changed does it keep the
// list for the next start and tell the window. No refresh or patch ran since the rows were
// picked (startReading), so the list is the one they were taken from.
func (s *Service) landFollowed(ctx context.Context, root string, keys map[string]bool, rows []Candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stampRead(keys)

	before := make([]Candidate, 0, len(rows))

	for _, c := range s.last.Repos {
		if keys[c.origin.Key()] {
			before = append(before, c)
		}
	}

	if sameRows(before, rows) {
		return
	}

	list := s.last
	list.Repos = slices.DeleteFunc(slices.Clone(s.last.Repos), func(c Candidate) bool { return keys[c.origin.Key()] })
	list.Repos = append(list.Repos, rows...)
	sortCandidates(list.Repos)

	s.last = list
	s.keep(context.WithoutCancel(ctx), list, root)
	s.events.Emit(event.ReleasesUpdated{})
}

// runningChecks returns the keys of the repositories, at most followChecksMax, whose rows
// the last list holds with their newest commit's checks running (followsChecks), and
// which were read before followChecksFresh ago: the longest unread first, so with more
// running than one round reads, each is read in turn. The caller holds mu.
func (s *Service) runningChecks(now time.Time) map[string]bool {
	if !s.has {
		return nil
	}

	type running struct {
		key    string
		readAt time.Time
	}

	var found []running

	seen := map[string]bool{}

	for _, c := range s.last.Repos {
		key := c.origin.Key()
		if readAt, ok := s.followable(c, now); ok && !seen[key] {
			seen[key] = true

			found = append(found, running{key: key, readAt: readAt})
		}
	}

	slices.SortStableFunc(found, func(a, b running) int {
		if c := a.readAt.Compare(b.readAt); c != 0 {
			return c
		}

		return cmp.Compare(a.key, b.key)
	})

	keys := make(map[string]bool, min(len(found), followChecksMax))

	for _, r := range found[:min(len(found), followChecksMax)] {
		keys[r.key] = true
	}

	return keys
}

// followable reports whether a round at now reads c's row again, and when its repository
// was last read: its checks are followed (followsChecks), its repository isn't muted now,
// and it was read in this run, but not within followChecksFresh. The caller holds mu.
func (s *Service) followable(c Candidate, now time.Time) (time.Time, bool) {
	if !followsChecks(c, now) || s.rules.IsMuted(c.origin) {
		return time.Time{}, false
	}

	readAt, ok := s.readAt[c.origin.Key()]

	return readAt, ok && now.Sub(readAt) >= followChecksFresh
}

// followsChecks reports whether c's row is one whose checks are followed at now: GitHub
// was asked about it, its newest commit's checks are running (pending, or expected and not
// yet reported), and that commit is no older than followChecksFor.
func followsChecks(c Candidate, now time.Time) bool {
	if c.unread() || (c.Checks != ChecksPending && c.Checks != ChecksExpected) {
		return false
	}

	return !c.headAt.IsZero() && now.Sub(c.headAt) <= followChecksFor
}

// sameRows reports whether a and b would show the same on the page: their rows as the
// window gets them, in order.
func sameRows(a, b []Candidate) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)

	return errA == nil && errB == nil && string(left) == string(right)
}

// ChecksFollower reads again, as often as its every says (the user's setting,
// FollowChecksInterval when nil; off at zero), the Release page's repositories whose newest
// commit's checks are running (followChecks), as a desktop worker. Wake makes it read the
// setting again at once. It isn't bound, and neither is what it calls. A round that
// arrives while one is still running is skipped; failures are logged, and a panicking
// round is recovered, so it keeps going until its context is canceled.
type ChecksFollower struct {
	follow func(ctx context.Context) error
	every  periodic.Every
	wake   chan struct{}
	logger *slog.Logger
}

// NewChecksFollower returns a ChecksFollower over s, as often as every says, logging
// failures to logger. A nil every follows every FollowChecksInterval, and a nil logger
// discards the failures.
func NewChecksFollower(s *Service, every periodic.Every, logger *slog.Logger) *ChecksFollower {
	return newChecksFollower(s.followChecks, every, logger)
}

// newChecksFollower returns a ChecksFollower that follows through follow.
func newChecksFollower(follow func(ctx context.Context) error, every periodic.Every, logger *slog.Logger,
) *ChecksFollower {
	if every == nil {
		every = func(context.Context) time.Duration { return FollowChecksInterval }
	}

	return &ChecksFollower{follow: follow, every: every, wake: make(chan struct{}, 1), logger: logger}
}

// Run follows now and then as often as every says until ctx is canceled, then waits for a
// round in flight to return. Its rounds find nothing to read until the page has read the
// list from GitHub in this run.
func (f *ChecksFollower) Run(ctx context.Context) {
	periodic.RunEvery(ctx, f.every, f.wake, followChecksJob, f.follow, f.logger)
}

// Wake has the follower read how often to follow again, as when the user changed it.
func (f *ChecksFollower) Wake() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}
