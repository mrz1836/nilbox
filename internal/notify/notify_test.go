package notify

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errPostFailed = errors.New("the notification center said no")

// testNow is the fixed time the tests record at.
var testNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test time

// fakePoster records the banners posted, and fails when err is set. denied and
// authErr are the answer to Authorize.
type fakePoster struct {
	mu      sync.Mutex
	status  BannerStatus
	err     error
	denied  bool
	authErr error
	asked   int
	banners []Banner
}

func (p *fakePoster) Authorize() (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.asked++

	return !p.denied, p.authErr
}

func (p *fakePoster) Post(b Banner) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.err != nil {
		return p.err
	}

	p.banners = append(p.banners, b)

	return nil
}

func (p *fakePoster) Status() BannerStatus { return p.status }

func (p *fakePoster) posted() []Banner {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]Banner(nil), p.banners...)
}

// fixture is a database with stored items to notify about.
type fixture struct {
	pool   *sql.DB
	clock  *testutil.Clock
	poster *fakePoster
	events *testutil.Events
	items  []int64
}

// newFixture opens a fresh database holding n pull requests in acme-org/example-repo,
// numbered from 1.
func newFixture(t *testing.T, n int) *fixture {
	t.Helper()

	pool := testutil.DB(t)
	ctx := t.Context()

	var accountID, repoID int64

	require.NoError(t, pool.QueryRowContext(ctx, `INSERT INTO accounts (login, created_at, updated_at)
VALUES ('octo-user', ?, ?) RETURNING id`, testNow, testNow).Scan(&accountID))
	require.NoError(t, pool.QueryRowContext(ctx, `INSERT INTO repos (owner, name, created_at, updated_at)
VALUES ('acme-org', 'example-repo', ?, ?) RETURNING id`, testNow, testNow).Scan(&repoID))

	f := &fixture{
		pool: pool, clock: testutil.NewClock(testNow), poster: &fakePoster{status: StatusOn},
		events: &testutil.Events{},
	}

	for i := 1; i <= n; i++ {
		var id int64

		require.NoError(t, pool.QueryRowContext(ctx, `INSERT INTO items (account_id, repo_id, kind, number,
item_created_at, item_updated_at, fetched_at) VALUES (?, ?, 'pr', ?, ?, ?, ?) RETURNING id`,
			accountID, repoID, i, testNow, testNow, testNow).Scan(&id))

		f.items = append(f.items, id)
	}

	return f
}

func (f *fixture) config() Config {
	return Config{Pool: f.pool, Poster: f.poster, Events: f.events, Clock: f.clock}
}

// arrival is the arrival of the fixture's item i (from 0) as kind.
func (f *fixture) arrival(i int, kind notifyprefs.Kind) inbox.Arrival {
	return inbox.Arrival{
		Kind: string(kind), ItemID: f.items[i], ItemKind: "pr", RepoOwner: "acme-org", RepoName: "example-repo",
		Number: i + 1, Title: fmt.Sprintf("Item %d", i+1),
		URL: fmt.Sprintf("https://github.com/acme-org/example-repo/pull/%d", i+1), Author: "octo-other",
	}
}

func TestKindsAreTheInboxArrivals(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []notifyprefs.Kind{
		KindReviewRequested, KindAssigned, KindWoke, KindSessionNeedsYou, KindSessionFinished, KindReleaseDone,
		KindRunFailed,
	}, notifyprefs.Kinds(),
		"the kinds the settings list are the inbox's kinds of arrival, in order, then the sessions', a release's, and a failed run's")
}

// releaseEntry is the entry for acme-org/sdk's v1.4.3 reaching GitHub.
func releaseEntry() Entry {
	return Entry{
		Kind: KindReleaseDone, Title: "v1.4.3 is on GitHub", Body: "Tagged a1b2c3d on master.",
		URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3", RepoOwner: "acme-org", RepoName: "sdk",
	}
}

func TestRecordEventStoresAnEntryAboutNoItem(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)

	got, err := s.RecordEvent(t.Context(), testNow.Add(time.Minute), releaseEntry())
	require.NoError(t, err)

	want := Notification{
		ID: got.ID, Kind: KindReleaseDone, CreatedAt: "2026-03-04T12:01:00Z", Repo: "acme-org/sdk",
		Title: "v1.4.3 is on GitHub", URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
		Detail: "Tagged a1b2c3d on master.",
	}
	assert.Equal(t, want, got, "no item: no id, kind, number, or author")

	var itemID sql.NullInt64
	require.NoError(t, f.pool.QueryRowContext(t.Context(), "SELECT item_id FROM notifications WHERE id = ?", got.ID).
		Scan(&itemID))
	assert.False(t, itemID.Valid, "its item is NULL")

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 2, "the log lists entries about no item beside the items'")
	assert.Equal(t, want, log.Items[0], "newest first, as RecordEvent returned it")
	assert.Equal(t, KindReviewRequested, log.Items[1].Kind)
	assert.Equal(t, 2, log.Unread)

	_, err = s.RecordEvent(t.Context(), testNow, Entry{Kind: "mentions", Title: "Hello"})
	require.ErrorIs(t, err, notifyprefs.ErrUnknownKind)
	assert.Equal(t, 2, countNotifications(t, f.pool), "an unknown kind stores nothing")

	repoless := releaseEntry()
	repoless.RepoOwner, repoless.RepoName = "", ""

	got, err = s.RecordEvent(t.Context(), testNow, repoless)
	require.NoError(t, err)
	assert.Empty(t, got.Repo, "no repository reads as none")
}

// runEntry is the entry for run 42, checks that failed in acme-org/sdk.
func runEntry() Entry {
	return Entry{
		Kind: KindRunFailed, Title: "Checks failed in acme-org/sdk: Checks · all files",
		Body: "4 passed, 1 failed: whitespace.", RepoOwner: "acme-org", RepoName: "sdk", RunID: 42,
	}
}

func TestRecordEventKeepsTheRunAnEntryIsAbout(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	got, err := s.RecordEvent(t.Context(), testNow, runEntry())
	require.NoError(t, err)

	want := Notification{
		ID: got.ID, Kind: KindRunFailed, CreatedAt: "2026-03-04T12:00:00Z", Repo: "acme-org/sdk",
		Title: "Checks failed in acme-org/sdk: Checks · all files", Detail: "4 passed, 1 failed: whitespace.", RunID: 42,
	}
	assert.Equal(t, want, got, "no item and no page: the run is what a click opens")

	_, err = s.Record(t.Context(), testNow.Add(time.Minute), []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)

	release, err := s.RecordEvent(t.Context(), testNow.Add(2*time.Minute), releaseEntry())
	require.NoError(t, err)
	assert.Zero(t, release.RunID, "an entry about no run names none")

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 3)
	assert.Equal(t, want, log.Items[2], "read back as stored")
	assert.Zero(t, log.Items[1].RunID, "an arrival names no run")

	negative := runEntry()
	negative.RunID = -1

	_, err = s.RecordEvent(t.Context(), testNow, negative)
	require.Error(t, err, "a run's ID is never negative")
	assert.Equal(t, 3, countNotifications(t, f.pool), "and nothing is stored")
}

func TestRecordEventKeepsOnlyTheNewest(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)
	s.keep = 3

	for i := range 5 {
		_, err := s.RecordEvent(t.Context(), testNow.Add(time.Duration(i)*time.Minute), releaseEntry())
		require.NoError(t, err)
	}

	assert.Equal(t, 3, countNotifications(t, f.pool), "trimmed to Keep like the rest")

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 3)
	assert.Equal(t, "2026-03-04T12:04:00Z", log.Items[0].CreatedAt)
	assert.Equal(t, "2026-03-04T12:02:00Z", log.Items[2].CreatedAt, "the oldest were dropped")

	require.NoError(t, f.pool.Close())

	_, err = s.RecordEvent(t.Context(), testNow, releaseEntry())
	require.ErrorContains(t, err, "record the notification")
}

func TestTheLogKeepsWhatWasRecorded(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 2)
	s := NewStore(f.pool)

	woke := f.arrival(1, KindWoke)
	woke.Woke = snooze.Reasons{Review: snooze.ReviewApproved, Commits: 2}

	rows, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested), woke})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.NotZero(t, rows[0].ID)

	f.clock.Set(testNow.Add(time.Minute))
	_, err = s.Record(t.Context(), f.clock.Now(), []inbox.Arrival{f.arrival(0, KindAssigned)})
	require.NoError(t, err)

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 3)
	assert.Equal(t, 3, log.Unread)

	newest := log.Items[0]
	assert.Equal(t, KindAssigned, newest.Kind, "newest first")
	assert.Equal(t, "2026-03-04T12:01:00Z", newest.CreatedAt)

	got := log.Items[1]
	assert.Equal(t, Notification{
		ID: got.ID, Kind: KindWoke, CreatedAt: "2026-03-04T12:00:00Z", ItemID: f.items[1], ItemKind: "pr",
		Repo: "acme-org/example-repo", Number: 2, Title: "Item 2",
		URL: "https://github.com/acme-org/example-repo/pull/2", Author: "octo-other",
		Detail: "approved, 2 new commits",
	}, got)
	assert.Equal(t, rows[1], got, "Record returns what the log reads back")
}

func TestTheLogLeavesOutKindsItDoesNotKnow(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindAssigned)})
	require.NoError(t, err)

	// A kind of notification a different build wrote.
	_, err = f.pool.ExecContext(t.Context(),
		"INSERT INTO notifications (kind, created_at, item_kind) VALUES ('mentions', ?, 'pr')", testNow)
	require.NoError(t, err)

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1)
	assert.Equal(t, KindAssigned, log.Items[0].Kind)
	assert.Equal(t, 1, log.Unread, "only what the log shows counts as unread")
}

func TestRecordingNothingStoresNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 0)

	rows, err := NewStore(f.pool).Record(t.Context(), testNow, nil)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestTheLogKeepsOnlyTheNewest(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	arrivals := make([]inbox.Arrival, Keep+5)
	for i := range arrivals {
		arrivals[i] = f.arrival(0, KindReviewRequested)
	}

	_, err := s.Record(t.Context(), testNow, arrivals[:5])
	require.NoError(t, err)
	_, err = s.Record(t.Context(), testNow.Add(time.Hour), arrivals[5:])
	require.NoError(t, err)

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	assert.Len(t, log.Items, Keep)

	for _, n := range log.Items {
		assert.Equal(t, "2026-03-04T13:00:00Z", n.CreatedAt, "the oldest were dropped")
	}
}

func TestMarkAllReadAndClear(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)

	require.NoError(t, s.MarkAllRead(t.Context(), testNow))

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	assert.Zero(t, log.Unread)
	assert.True(t, log.Items[0].Read)

	require.NoError(t, s.Clear(t.Context()))

	log, err = s.Log(t.Context())
	require.NoError(t, err)
	assert.Empty(t, log.Items)
}

func TestAnEntryOutlivesItsItem(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)
	_, err = f.pool.ExecContext(t.Context(), "DELETE FROM items")
	require.NoError(t, err)

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1)
	assert.Zero(t, log.Items[0].ItemID)
	assert.Equal(t, "https://github.com/acme-org/example-repo/pull/1", log.Items[0].URL, "the copy still reads")
}

func TestAnEntryWithoutARepository(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	a := f.arrival(0, KindAssigned)
	a.RepoOwner, a.RepoName = "", ""

	rows, err := s.Record(t.Context(), testNow, []inbox.Arrival{a})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].Repo, "no repository reads as none, not as a lone slash")
}

func TestTheStoreFailsOnAClosedDatabase(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)
	require.NoError(t, f.pool.Close())

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.Error(t, err)

	_, err = s.Log(t.Context())
	require.Error(t, err)
	require.Error(t, s.MarkAllRead(t.Context(), testNow))
	require.Error(t, s.Clear(t.Context()))
}

// stamp returns the stored text of a notification's DATETIME column, or "" when it is
// NULL.
func stamp(t *testing.T, db *sql.DB, column string, id int64) string {
	t.Helper()

	var text sql.NullString
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT CAST("+column+" AS TEXT) FROM notifications WHERE id = ?", id).Scan(&text))

	return text.String
}

// countNotifications returns how many notifications are stored.
func countNotifications(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM notifications").Scan(&n))

	return n
}

func TestTheLogKeepsTheLastRecordedOfTheSameTime(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	arrivals := make([]inbox.Arrival, Keep+3)
	for i := range arrivals {
		arrivals[i] = f.arrival(0, KindReviewRequested)
	}

	rows, err := s.Record(t.Context(), testNow, arrivals)
	require.NoError(t, err)
	require.Len(t, rows, Keep+3, "Record returns every one it stored")
	assert.Equal(t, Keep, countNotifications(t, f.pool), "exactly Keep are kept")

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, Keep)
	assert.Equal(t, rows[len(rows)-1].ID, log.Items[0].ID, "the last recorded comes first")
	assert.Equal(t, rows[3].ID, log.Items[Keep-1].ID, "the first three recorded are dropped")
}

func TestRecordIsAllOrNothing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)

	unknown := f.arrival(0, KindAssigned)
	unknown.ItemID = f.items[0] + 100

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested), unknown})
	require.ErrorContains(t, err, "record the notifications")
	assert.Zero(t, countNotifications(t, f.pool), "an arrival for an item that is not stored stores none")

	// A trim that fails takes the notifications it followed with it.
	_, err = f.pool.ExecContext(t.Context(), `CREATE TRIGGER refuse_trim BEFORE DELETE ON notifications
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	arrivals := make([]inbox.Arrival, Keep+1)
	for i := range arrivals {
		arrivals[i] = f.arrival(0, KindReviewRequested)
	}

	_, err = s.Record(t.Context(), testNow, arrivals)
	require.ErrorContains(t, err, "refused by the test")
	assert.Zero(t, countNotifications(t, f.pool))
}

func TestRecordConcurrently(t *testing.T) {
	t.Parallel()

	// The cap is lowered so the writers run past it in few transactions: SQLite takes
	// one writer at a time, and hundreds of back-to-back transactions on a loaded machine
	// can leave one writer waiting longer than the busy timeout.
	const (
		writers   = 8
		perWriter = 5
		keep      = 20
	)

	f := newFixture(t, 1)
	s := NewStore(f.pool)
	s.keep = keep

	var g errgroup.Group

	for w := range writers {
		g.Go(func() error {
			for i := range perWriter {
				// Every notification has its own time, so which are newest does not
				// depend on the order the writers ran in.
				at := testNow.Add(time.Duration(w*perWriter+i) * time.Second)
				if _, err := s.Record(t.Context(), at, []inbox.Arrival{f.arrival(0, KindAssigned)}); err != nil {
					return err
				}
			}

			return nil
		})
	}

	require.NoError(t, g.Wait())
	assert.Equal(t, keep, countNotifications(t, f.pool))

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, keep)

	for i, n := range log.Items {
		want := testNow.Add(time.Duration(writers*perWriter-1-i) * time.Second)
		assert.Equal(t, want.Format(time.RFC3339), n.CreatedAt, "exactly the newest are kept, newest first")
	}
}

func TestMarkAllReadStampsOnlyTheUnread(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)
	svc := NewService(f.config())

	first, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)
	assert.Empty(t, stamp(t, f.pool, "read_at", first[0].ID), "unread")

	f.clock.Set(testNow.Add(time.Minute))
	require.NoError(t, svc.MarkAllRead(t.Context()))
	assert.Equal(t, "2026-03-04 12:01:00+00:00", stamp(t, f.pool, "read_at", first[0].ID), "read at the clock's time")

	f.clock.Set(testNow.Add(2 * time.Minute))
	second, err := s.Record(t.Context(), f.clock.Now(), []inbox.Arrival{f.arrival(0, KindAssigned)})
	require.NoError(t, err)
	assert.Equal(t, "2026-03-04 12:02:00+00:00", stamp(t, f.pool, "created_at", second[0].ID))

	f.clock.Set(testNow.Add(3 * time.Minute))
	require.NoError(t, svc.MarkAllRead(t.Context()))
	assert.Equal(t, "2026-03-04 12:01:00+00:00", stamp(t, f.pool, "read_at", first[0].ID), "already read")
	assert.Equal(t, "2026-03-04 12:03:00+00:00", stamp(t, f.pool, "read_at", second[0].ID))
}

func TestTimesAreStoredInUTC(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	s := NewStore(f.pool)
	zone := time.FixedZone("UTC-5", -5*60*60)

	rows, err := s.Record(t.Context(), testNow.In(zone), []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)
	assert.Equal(t, "2026-03-04T12:00:00Z", rows[0].CreatedAt)
	assert.Equal(t, "2026-03-04 12:00:00+00:00", stamp(t, f.pool, "created_at", rows[0].ID))

	require.NoError(t, s.MarkAllRead(t.Context(), testNow.Add(time.Hour).In(zone)))
	assert.Equal(t, "2026-03-04 13:00:00+00:00", stamp(t, f.pool, "read_at", rows[0].ID))
}

func TestTheDispatcherRecordsAndShowsBanners(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 2)
	d := NewDispatcher(f.config())

	d.Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested), f.arrival(1, KindAssigned)})

	log, err := NewStore(f.pool).Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 2)
	assert.Equal(t, []event.Event{event.NotificationsUpdated{}}, f.events.All())

	banners := f.poster.posted()
	require.Len(t, banners, 2)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-" + strconv.FormatInt(log.Items[1].ID, 10), Title: "Review requested",
		Subtitle: "acme-org/example-repo #1", Body: "Item 1", Thread: string(KindReviewRequested), ItemID: f.items[0],
		URL: "https://github.com/acme-org/example-repo/pull/1",
	}, banners[0])
	assert.Equal(t, "Assigned to you", banners[1].Title)
}

func TestTheDispatcherKeepsTheLogWhateverHappensToTheBanners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string

		// setup changes the fixture or the dispatcher's config before the arrivals come.
		setup func(t *testing.T, f *fixture, cfg *Config)

		banners []string
		why     string
	}{
		{
			name: "a kind whose banners are off",
			setup: func(t *testing.T, f *fixture, _ *Config) {
				t.Helper()

				require.NoError(t, prefs.New(f.pool, f.clock).Set(t.Context(), notifyprefs.KeyBannersOff, `["review_requested"]`))
			},
			banners: []string{"Assigned to you"},
			why:     "only the banners the user left on are shown",
		},
		{
			name: "no poster",
			setup: func(_ *testing.T, _ *fixture, cfg *Config) {
				cfg.Poster = nil
			},
			why: "without a poster, nothing can be shown",
		},
		{
			name: "banners the user has not allowed",
			setup: func(_ *testing.T, f *fixture, _ *Config) {
				f.poster.status = StatusOff
			},
			why: "the system's setting is respected",
		},
		{
			name: "settings that cannot be read",
			setup: func(t *testing.T, f *fixture, _ *Config) {
				t.Helper()

				_, err := f.pool.ExecContext(t.Context(), "DROP TABLE prefs")
				require.NoError(t, err)
			},
			why: "without the settings, no banner the user may have turned off is shown",
		},
		{
			name: "banners that fail to post",
			setup: func(_ *testing.T, f *fixture, _ *Config) {
				f.poster.err = errPostFailed
			},
			why: "a failed banner is not shown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, 2)
			cfg := f.config()
			tt.setup(t, f, &cfg)

			NewDispatcher(cfg).Arrived(t.Context(),
				[]inbox.Arrival{f.arrival(0, KindReviewRequested), f.arrival(1, KindAssigned)})

			posted := f.poster.posted()
			titles := make([]string, 0, len(posted))

			for _, b := range posted {
				titles = append(titles, b.Title)
			}

			assert.ElementsMatch(t, tt.banners, titles, tt.why)
			assert.Equal(t, 1, f.events.Count(), "the log changed, and the window is told once")

			log, err := NewStore(f.pool).Log(t.Context())
			require.NoError(t, err)
			assert.Len(t, log.Items, 2, "the log keeps every arrival")
		})
	}
}

func TestTheDispatcherNotifiesOfAnEntryAboutNoItem(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 0)
	logs := &testutil.Logs{}
	cfg := f.config()
	cfg.Logger = logs.Logger()
	d := NewDispatcher(cfg)

	d.Notify(t.Context(), releaseEntry())

	log, err := NewStore(f.pool).Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1)
	assert.Equal(t, []event.Event{event.NotificationsUpdated{}}, f.events.All())
	assert.Equal(t, []Banner{{
		ID: brand.Slug + "-" + strconv.FormatInt(log.Items[0].ID, 10), Title: "Released", Subtitle: "acme-org/sdk",
		Body: "v1.4.3 is on GitHub\nTagged a1b2c3d on master.", Thread: string(KindReleaseDone),
		URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
	}}, f.poster.posted(), "a click opens the entry's page: there's no item to show")

	require.NoError(t, prefs.New(f.pool, f.clock).Set(t.Context(), notifyprefs.KeyBannersOff, `["release_done"]`))
	d.Notify(t.Context(), releaseEntry())
	assert.Len(t, f.poster.posted(), 1, "its kind's banners are off")
	assert.Equal(t, 2, f.events.Count(), "the log still keeps it")

	d.Notify(t.Context(), Entry{Kind: "mentions"})
	assert.Equal(t, 2, f.events.Count(), "an entry that can't be recorded tells nothing")
	assert.Contains(t, logs.String(), "could not record the notification")
}

func TestTheDispatcherNotifiesOfAFailedRun(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 0)
	d := NewDispatcher(f.config())

	d.Notify(t.Context(), runEntry())

	log, err := NewStore(f.pool).Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1)
	assert.Equal(t, []Banner{{
		ID: brand.Slug + "-" + strconv.FormatInt(log.Items[0].ID, 10), Title: "Run failed", Subtitle: "acme-org/sdk",
		Body:   "Checks failed in acme-org/sdk: Checks · all files\n4 passed, 1 failed: whitespace.",
		Thread: string(KindRunFailed), RunID: 42,
	}}, f.poster.posted(), "a click opens the run")

	require.NoError(t, prefs.New(f.pool, f.clock).Set(t.Context(), notifyprefs.KeyBannersOff, `["run_failed"]`))
	d.Notify(t.Context(), runEntry())
	assert.Len(t, f.poster.posted(), 1, "its kind's banners are off")
	assert.Equal(t, 2, f.events.Count(), "the log still keeps it")

	f.poster.status = StatusOff

	require.NoError(t, prefs.New(f.pool, f.clock).Set(t.Context(), notifyprefs.KeyBannersOff, `[]`))
	d.Notify(t.Context(), runEntry())
	assert.Len(t, f.poster.posted(), 1, "no banner while the user doesn't allow them")
	assert.Equal(t, 3, f.events.Count(), "the log still keeps it")
}

func TestTheDispatcherSurvivesFailures(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	f.poster.err = errPostFailed

	d := NewDispatcher(f.config())
	d.Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	assert.Equal(t, 1, f.events.Count(), "the entry was still recorded")

	d.Arrived(t.Context(), nil)
	assert.Equal(t, 1, f.events.Count(), "nothing new, nothing told")

	require.NoError(t, f.pool.Close())
	d.Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	assert.Equal(t, 1, f.events.Count(), "nothing was recorded")
}

func TestTheServiceServesTheLog(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	svc := NewService(f.config())

	_, err := NewStore(f.pool).Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)

	log, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, log.Unread)

	require.NoError(t, svc.MarkAllRead(t.Context()))
	assert.Equal(t, 1, f.events.Count())

	log, err = svc.List(t.Context())
	require.NoError(t, err)
	assert.Zero(t, log.Unread)

	require.NoError(t, svc.Clear(t.Context()))
	assert.Equal(t, 2, f.events.Count())

	log, err = svc.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, log.Items)

	require.NoError(t, f.pool.Close())
	require.Error(t, svc.MarkAllRead(t.Context()))
	require.Error(t, svc.Clear(t.Context()))
	assert.Equal(t, 2, f.events.Count(), "failures are not told")
}

func TestBannerStatusAndTest(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 0)

	svc := NewService(f.config())
	assert.Equal(t, StatusOn, svc.BannerStatus())
	require.NoError(t, svc.SendTest())
	assert.Equal(t, []Banner{TestBanner()}, f.poster.posted())
	assert.Equal(t, 1, f.poster.asked, "the user is asked before the first banner")

	f.poster.err = errPostFailed

	require.ErrorIs(t, svc.SendTest(), errPostFailed)

	f.poster.denied = true

	require.ErrorIs(t, svc.SendTest(), ErrBannersOff)

	f.poster.authErr = errPostFailed

	require.ErrorIs(t, svc.SendTest(), errPostFailed)

	f.poster.status = StatusUnavailable

	require.ErrorIs(t, svc.SendTest(), ErrBannersUnavailable)

	cfg := f.config()
	cfg.Poster = nil
	none := NewService(cfg)
	assert.Equal(t, StatusUnavailable, none.BannerStatus())
	require.ErrorIs(t, none.SendTest(), ErrBannersUnavailable)
}

func TestTheDefaultsFillIn(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 0)
	svc := NewService(Config{Pool: f.pool})

	require.NotNil(t, svc.clock)
	assert.NotNil(t, svc.logger)
	assert.WithinDuration(t, time.Now(), svc.clock.Now(), time.Minute)
	assert.Equal(t, event.Discard{}, svc.events, "without a sink, events are dropped")
	svc.notify()
}
