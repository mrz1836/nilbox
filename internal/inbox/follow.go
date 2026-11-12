package inbox

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/periodic"
)

// The follow of the pull requests whose checks are running: GitHub's own page updates
// as the checks end, and the inbox's five-minute refresh would leave the card behind.
const (
	// FollowInterval is how often, unless the settings say otherwise, the follow reads
	// again the pull requests whose checks were still running. A round asks GitHub one request for all of them, about a point,
	// and nothing while none runs: at most some 80 points an hour, a percent or two of
	// the GraphQL budget, and only while checks run.
	FollowInterval = 45 * time.Second

	// followFresh is how recently a pull request's details must have been read for a
	// round to leave it alone, as after a whole refresh or the round before: a little less
	// than the shortest interval the follow can be set to, so the round after reads it
	// again.
	followFresh = 25 * time.Second

	// followMax is the most pull requests one round reads, the longest unread first.
	followMax = 50

	// followJob names the follow's rounds in log entries.
	followJob = "follow running checks"
)

// refreshRunning reads again, in one request, the listed pull requests whose checks were
// still running when the inbox last read them, neither dismissed nor snoozed, and stores
// each as RefreshItem would, so a card shows its checks end about as soon as GitHub's own
// page does. A pull request whose checks end, pass or fail, is left alone from then on. It
// asks GitHub nothing while none runs, and leaves alone one read in the last little while.
// A pull request GitHub doesn't answer for is left as it was, for the next whole refresh.
func (s *Service) refreshRunning(ctx context.Context) error {
	viewer, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil {
		return fmt.Errorf("read the pull requests whose checks run: %w", err)
	}

	now := s.clock.Now().UTC()

	running, err := s.q.ListRunningChecks(ctx, queries.ListRunningChecksParams{
		Login: viewer, ReadBefore: now.Add(-followFresh), Now: &now, RowLimit: followMax,
	})
	if err != nil {
		return fmt.Errorf("read the pull requests whose checks run: %w", err)
	}

	if len(running) == 0 {
		return nil
	}

	// Each pull request waits for other work on it, as RefreshItem does, in one order, so
	// two rounds never hold each other up.
	slices.SortFunc(running, func(a, b queries.ListRunningChecksRow) int { return cmp.Compare(a.ID, b.ID) })

	unlock, err := s.lockAll(ctx, running)
	if err != nil {
		return fmt.Errorf("follow the running checks: %w", err)
	}

	defer unlock()

	wrote, err := s.followRunning(ctx, running)
	if err != nil {
		return fmt.Errorf("follow the running checks: %w", err)
	}

	if wrote {
		_, err = s.changed(ctx)
	}

	return err
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

// followRunning asks GitHub about running, and stores each pull request it answered for.
// It reports whether it stored any.
func (s *Service) followRunning(ctx context.Context, running []queries.ListRunningChecksRow) (bool, error) {
	last, err := s.projectAccess(ctx)
	if err != nil {
		return false, err
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
	if err != nil {
		return false, err
	}

	wrote := false

	for _, r := range running {
		update, ok := got.updates.Items[r.NodeID]
		if !ok {
			continue
		}

		w := itemWrite{id: r.ID, seq: seq, update: &itemUpdate{
			accountID: r.AccountID, update: &update, files: got.files[r.ID],
		}}

		stored, storeErr := s.storeItemWrite(ctx, w)
		if storeErr != nil {
			return wrote, fmt.Errorf("save the item: %w", storeErr)
		}

		wrote = wrote || stored
	}

	return wrote, nil
}

// fetchRunning reads running from GitHub with tok, and checks the files of each one whose
// head moved since its files were last checked.
func (s *Service) fetchRunning(
	ctx context.Context, tok github.Token, running []queries.ListRunningChecksRow, ids []string,
	opts github.DetailOptions,
) (followed, error) {
	updates, err := s.client.FetchItems(ctx, tok, ids, opts)
	if err != nil {
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

	return out, nil
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
