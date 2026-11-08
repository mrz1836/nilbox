package inbox_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errNotAsked = errors.New("the race test never asks for this")

// raceTokens hands out a placeholder token that matches no real token format.
type raceTokens struct{}

func (raceTokens) Token(context.Context) (github.Token, error) {
	return github.NewToken("test-token-not-real"), nil
}

func (raceTokens) Forget(github.Token) {}

// raceClient answers every refresh with the same items, except that #4 asks for a review
// on every other refresh only, so refreshes keep recording arrivals.
type raceClient struct {
	mu        sync.Mutex
	refreshes int
}

// raceItem is a pull request someone other than the signed-in user wrote.
func raceItem(number int) github.Item {
	updated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(number) * time.Hour)

	return github.Item{
		IsPR: true, NodeID: fmt.Sprintf("NODE_%d", number), Number: number, Title: fmt.Sprintf("Item %d", number),
		URL:       fmt.Sprintf("https://github.com/acme-org/example-repo/pull/%d", number),
		CreatedAt: updated.Add(-time.Hour), UpdatedAt: updated, AuthorLogin: "octo-other",
		AuthorAssociation: "MEMBER", RepoOwner: "acme-org", RepoName: "example-repo",
	}
}

// reviewRequested lists the numbers of the items asking for a review on the nth refresh.
func reviewRequested(n int) []int {
	if n%2 == 0 {
		return []int{1, 2, 4}
	}

	return []int{1, 2}
}

func (c *raceClient) FetchInbox(context.Context, github.Token) (*github.Inbox, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.refreshes++

	numbers := reviewRequested(c.refreshes)
	review := make([]github.Item, 0, len(numbers))

	for _, n := range numbers {
		review = append(review, raceItem(n))
	}

	return &github.Inbox{
		Viewer:          github.Viewer{Login: "octo-user"},
		ReviewRequested: github.SearchResult{IssueCount: len(review), Items: review},
		Assigned:        github.SearchResult{IssueCount: 1, Items: []github.Item{raceItem(3)}},
		RateLimit:       github.QueryRate{Cost: 1, Limit: 5000, Remaining: 4990, Used: 10},
		OAuthScopes:     "repo, read:org",
	}, nil
}

func (c *raceClient) FetchDetails(
	_ context.Context, _ github.Token, ids []string, _ github.DetailOptions,
) (*github.Details, error) {
	out := &github.Details{Items: map[string]github.ItemDetails{}, Requests: 1}
	for _, id := range ids {
		out.Items[id] = github.ItemDetails{HeadOID: "1111111111111111111111111111111111111111", Commits: 2}
	}

	return out, nil
}

func (c *raceClient) FetchRateLimits(context.Context, github.Token) (*github.RateLimits, error) {
	return &github.RateLimits{Core: github.Rate{Limit: 5000}, Search: github.Rate{Limit: 30}}, nil
}

// FetchItem answers that the item with the node ID is open and unchanged.
func (c *raceClient) FetchItem(
	_ context.Context, _ github.Token, id string, _ github.DetailOptions,
) (*github.ItemUpdate, error) {
	var number int
	if _, err := fmt.Sscanf(id, "NODE_%d", &number); err != nil {
		return nil, err
	}

	return &github.ItemUpdate{
		Found: true, Item: raceItem(number),
		Details:   github.ItemDetails{HeadOID: "1111111111111111111111111111111111111111", Commits: 2},
		RateLimit: github.QueryRate{Cost: 1, Limit: 5000, Remaining: 4980, Used: 20},
	}, nil
}

func (c *raceClient) FetchPullHead(context.Context, github.Token, item.Ref) (*github.PullHead, error) {
	return nil, errNotAsked
}

func (c *raceClient) FetchChangedFiles(context.Context, github.Token, item.Ref, string) (*github.ChangedFiles, error) {
	return nil, errNotAsked
}

func (c *raceClient) FetchAvatar(context.Context, string) (*github.Avatar, error) {
	return nil, errNotAsked
}

func (c *raceClient) FetchViewer(context.Context, github.Token) (string, error) {
	return "", errNotAsked
}

func (c *raceClient) RemoveAssignee(context.Context, github.Token, item.Ref, string) (*github.Edited, error) {
	return nil, errNotAsked
}

func (c *raceClient) RemoveReviewRequest(context.Context, github.Token, item.Ref, string) (*github.Edited, error) {
	return nil, errNotAsked
}

// lockedBuffer collects log output written from many goroutines.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// numbered returns the snapshot item with a number, from any list.
func numbered(t *testing.T, snap inbox.Snapshot, number int) inbox.Item {
	t.Helper()

	for _, l := range snap.Lists() {
		for _, it := range l.Items {
			if it.Number == number {
				return it
			}
		}
	}

	require.Failf(t, "item not listed", "#%d", number)

	return inbox.Item{}
}

// listNumbers lists a list's item numbers in order.
func listNumbers(l inbox.List) []int {
	out := make([]int, 0, len(l.Items))
	for _, it := range l.Items {
		out = append(out, it.Number)
	}

	return out
}

// raceRounds is how many times each of the race test's jobs repeats its writes.
const raceRounds = 12

// repeat returns a job that runs fn raceRounds times, stopping at its first error.
func repeat(fn func(round int) error) func() error {
	return func() error {
		for round := range raceRounds {
			if err := fn(round); err != nil {
				return err
			}
		}

		return nil
	}
}

// errOnly returns a call's error, dropping its result.
func errOnly[T any](_ T, err error) error {
	return err
}

// triageStep is a triage action on one item.
type triageStep func(ctx context.Context, id int64) (inbox.Snapshot, error)

// toggle returns a job that runs on and off on item id in turn, raceRounds times, then on
// once more, so what on sets is what holds at the end.
func toggle(ctx context.Context, id int64, on, off triageStep) func() error {
	return func() error {
		err := repeat(func(int) error {
			if err := errOnly(on(ctx, id)); err != nil {
				return err
			}

			return errOnly(off(ctx, id))
		})()
		if err != nil {
			return err
		}

		return errOnly(on(ctx, id))
	}
}

// terminals are the terminals the settings job alternates between, by round.
func terminals() []terminal.ID {
	return []terminal.ID{settings.TerminalApp, settings.TerminalITerm2}
}

// writeSettings returns a job that writes settings the inbox reads in a refresh (the
// trusted bots and the wake rules) and one it does not, a different value each round.
func writeSettings(ctx context.Context, s *settings.Service) func() error {
	return repeat(func(round int) error {
		if err := errOnly(s.SetTerminal(ctx, terminals()[round%2])); err != nil {
			return err
		}

		if err := errOnly(s.SetTrustedBots(ctx, []string{"dependabot", fmt.Sprintf("helper-%d", round)})); err != nil {
			return err
		}

		return errOnly(s.SetSnoozeWake(ctx, snooze.Rules{Commits: 1 + round%3, Reviews: snooze.ReviewsAny}))
	})
}

// rememberClones returns a job that records a clone for the repository with repoID, a
// different path each round, as launches do while refreshes upsert the same repository.
func rememberClones(ctx context.Context, items *inbox.Store, repoID int64) func() error {
	return repeat(func(round int) error {
		return items.RememberClone(ctx, repoID, clonePath(round))
	})
}

// clonePath is the clone the rememberClones job records in a round.
func clonePath(round int) string {
	return fmt.Sprintf("~/projects/example-repo-%d", round)
}

// readNotifications returns a job that marks the notification history read and reads it,
// as the window does when it is opened.
func readNotifications(ctx context.Context, s *notify.Service) func() error {
	return repeat(func(int) error {
		if err := s.MarkAllRead(ctx); err != nil {
			return err
		}

		_, err := s.List(ctx)

		return err
	})
}

func TestRefreshTriageAndSettingsWritesShareOnePool(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	pool := testutil.DB(t)
	clk := testutil.NewClock(time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC))

	var logs lockedBuffer

	notices := notify.Config{Pool: pool, Clock: clk, Logger: slog.New(slog.NewTextHandler(&logs, nil))}
	svc := inbox.NewService(inbox.Config{
		Pool: pool, Tokens: raceTokens{}, Client: &raceClient{}, Clock: clk, Arrivals: notify.NewDispatcher(notices),
	})
	notifications := notify.NewService(notices)
	userSettings := settings.NewService(pool, profiles.NewStore(pool, clk), t.TempDir(), settings.WithClock(clk))

	first, err := svc.Refresh(ctx)
	require.NoError(t, err)

	items := inbox.NewStore(pool, clk)
	pinned := numbered(t, first, 1).ID

	launched, err := items.LaunchItems(ctx, []int64{pinned})
	require.NoError(t, err)
	require.Len(t, launched, 1)

	snoozeFor3Days := func(ctx context.Context, id int64) (inbox.Snapshot, error) { return svc.Snooze(ctx, id, 3) }

	// Every job runs at once on the one pool: refreshes, which store the inbox and its
	// status in one transaction each and record the notifications of what arrived; an
	// item's own refreshes, which rewrite it in a transaction of their own; clones
	// remembered for the repository the refreshes upsert; triage of three items, each
	// ending in a known state; settings, some of which the refresh reads; the
	// notification history being read and marked read; and snapshots.
	var g errgroup.Group

	g.Go(repeat(func(int) error { return errOnly(svc.Refresh(ctx)) }))
	g.Go(repeat(func(int) error { return errOnly(svc.RefreshItem(ctx, pinned)) }))
	g.Go(rememberClones(ctx, items, launched[0].RepoID))
	g.Go(toggle(ctx, pinned, svc.Pin, svc.Unpin))
	g.Go(toggle(ctx, numbered(t, first, 2).ID, svc.Dismiss, svc.Restore))
	g.Go(toggle(ctx, numbered(t, first, 3).ID, snoozeFor3Days, svc.Restore))
	g.Go(writeSettings(ctx, userSettings))
	g.Go(readNotifications(ctx, notifications))
	g.Go(repeat(func(int) error { return errOnly(svc.Snapshot(ctx)) }))

	require.NoError(t, g.Wait(), "no write fails, and none waits out the busy timeout")
	assert.Empty(t, logs.String(), "no arrival went unrecorded")

	snap, err := svc.Snapshot(ctx)
	require.NoError(t, err)

	refreshes := 1 + raceRounds

	// The last refresh is an odd one, which does not ask for #4's review.
	assert.Equal(t, []int{1, 2}, listNumbers(snap.ReviewRequested), "the last refresh's list, the pinned item first")
	assert.Equal(t, []int{3}, listNumbers(snap.Assigned))
	assert.Equal(t, "Item 1", numbered(t, snap, 1).Title)

	now := clk.Now()

	assert.NotEmpty(t, numbered(t, snap, 1).PinnedAt, "the last pin holds")
	assert.True(t, numbered(t, snap, 1).NeedsYou(now))
	assert.True(t, numbered(t, snap, 2).Dismissed(), "the last dismissal holds")
	assert.True(t, numbered(t, snap, 3).Snoozed(now), "the last snooze holds")
	assert.Equal(t, "2026-01-10T12:00:00Z", snap.LastRefreshedAt)
	assert.Empty(t, snap.LastError)

	readings := map[string]int{}

	rows, err := pool.QueryContext(ctx, "SELECT resource, COUNT(*) FROM rate_snapshots GROUP BY resource")
	require.NoError(t, err)

	for rows.Next() {
		var (
			resource string
			n        int
		)

		require.NoError(t, rows.Scan(&resource, &n))
		readings[resource] = n
	}

	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())

	// An item's own refresh stores nothing when a whole refresh that started after it
	// stored the item first, so how many of their GraphQL readings were stored depends on
	// how the jobs interleaved.
	assert.Equal(t, refreshes, readings["core"], "every refresh stored its readings")
	assert.Equal(t, refreshes, readings["search"])
	assert.GreaterOrEqual(t, readings["graphql"], refreshes)
	assert.LessOrEqual(t, readings["graphql"], refreshes+raceRounds)

	launched, err = items.LaunchItems(ctx, []int64{pinned})
	require.NoError(t, err)
	require.Len(t, launched, 1)
	assert.Equal(t, clonePath(raceRounds-1), launched[0].LocalPath, "the last clone remembered holds")

	got, err := userSettings.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, terminals()[(raceRounds-1)%2], got.Terminal)
	assert.Equal(t, []string{"dependabot", fmt.Sprintf("helper-%d", raceRounds-1)}, got.TrustedBots)

	// #4 newly asks for a review on every even refresh after the first.
	require.NoError(t, notifications.MarkAllRead(ctx))

	history, err := notifications.List(ctx)
	require.NoError(t, err)
	assert.Len(t, history.Items, refreshes/2, "every arrival was recorded")
	assert.Zero(t, history.Unread)
}
