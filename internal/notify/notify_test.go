package notify

import (
	"context"
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
	status  string
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

func (p *fakePoster) Status() string { return p.status }

func (p *fakePoster) posted() []Banner {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]Banner(nil), p.banners...)
}

// fixture is a database with stored items to notify about.
type fixture struct {
	pool     *sql.DB
	clock    *testutil.Clock
	poster   *fakePoster
	notifier *testutil.Notifier
	items    []int64
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
		notifier: &testutil.Notifier{},
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
	return Config{Pool: f.pool, Poster: f.poster, Notifier: f.notifier, Clock: f.clock}
}

// arrival is the arrival of the fixture's item i (from 0) as kind.
func (f *fixture) arrival(i int, kind string) inbox.Arrival {
	return inbox.Arrival{
		Kind: kind, ItemID: f.items[i], ItemKind: "pr", RepoOwner: "acme-org", RepoName: "example-repo",
		Number: i + 1, Title: fmt.Sprintf("Item %d", i+1),
		URL: fmt.Sprintf("https://github.com/acme-org/example-repo/pull/%d", i+1), Author: "octo-other",
	}
}

func TestKindsAreTheInboxArrivals(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{KindReviewRequested, KindAssigned, KindWoke}, notifyprefs.Kinds(),
		"the kinds the settings list are the inbox's kinds of arrival, in order")
}

func TestTheLogKeepsWhatWasRecorded(t *testing.T) {
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

func TestRecordingNothingStoresNothing(t *testing.T) {
	f := newFixture(t, 0)

	rows, err := NewStore(f.pool).Record(t.Context(), testNow, nil)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestTheLogKeepsOnlyTheNewest(t *testing.T) {
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

	const (
		writers   = 8
		perWriter = 30
	)

	f := newFixture(t, 1)
	s := NewStore(f.pool)

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
	assert.Equal(t, Keep, countNotifications(t, f.pool))

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, Keep)

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
	f := newFixture(t, 2)
	d := NewDispatcher(f.config())

	d.Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested), f.arrival(1, KindAssigned)})

	log, err := NewStore(f.pool).Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 2)
	assert.Equal(t, 1, f.notifier.Calls())

	banners := f.poster.posted()
	require.Len(t, banners, 2)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-" + strconv.FormatInt(log.Items[1].ID, 10), Title: "Review requested",
		Subtitle: "acme-org/example-repo #1", Body: "Item 1", Thread: KindReviewRequested, ItemID: f.items[0],
		URL: "https://github.com/acme-org/example-repo/pull/1",
	}, banners[0])
	assert.Equal(t, "Assigned to you", banners[1].Title)
}

func TestTheDispatcherLeavesOutBannersThatAreOff(t *testing.T) {
	f := newFixture(t, 2)
	require.NoError(t, prefs.New(f.pool, f.clock).Set(t.Context(), notifyprefs.KeyBannersOff, `["review_requested"]`))

	NewDispatcher(f.config()).Arrived(t.Context(),
		[]inbox.Arrival{f.arrival(0, KindReviewRequested), f.arrival(1, KindAssigned)})

	banners := f.poster.posted()
	require.Len(t, banners, 1)
	assert.Equal(t, "Assigned to you", banners[0].Title)

	log, err := NewStore(f.pool).Log(t.Context())
	require.NoError(t, err)
	assert.Len(t, log.Items, 2, "the log keeps every kind")
}

func TestTheDispatcherKeepsTheLogWithoutAPoster(t *testing.T) {
	f := newFixture(t, 1)
	cfg := f.config()
	cfg.Poster = nil

	NewDispatcher(cfg).Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})

	log, err := NewStore(f.pool).Log(t.Context())
	require.NoError(t, err)
	assert.Len(t, log.Items, 1)
	assert.Equal(t, 1, f.notifier.Calls())
}

func TestTheDispatcherShowsNoBannersTheUserHasNotAllowed(t *testing.T) {
	f := newFixture(t, 1)
	f.poster.status = StatusOff

	NewDispatcher(f.config()).Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})

	assert.Empty(t, f.poster.posted())
	assert.Equal(t, 1, f.notifier.Calls(), "the log still has it")
}

func TestTheDispatcherSurvivesFailures(t *testing.T) {
	f := newFixture(t, 1)
	f.poster.err = errPostFailed

	d := NewDispatcher(f.config())
	d.Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	assert.Equal(t, 1, f.notifier.Calls(), "the entry was still recorded")

	d.Arrived(t.Context(), nil)
	assert.Equal(t, 1, f.notifier.Calls(), "nothing new, nothing told")

	require.NoError(t, f.pool.Close())
	d.Arrived(context.Background(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	assert.Equal(t, 1, f.notifier.Calls(), "nothing was recorded")
}

func TestTheDispatcherShowsNoBannersWhenTheSettingsCannotBeRead(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 1)
	_, err := f.pool.ExecContext(t.Context(), "DROP TABLE prefs")
	require.NoError(t, err)

	NewDispatcher(f.config()).Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})

	assert.Empty(t, f.poster.posted(), "without the settings, no banner the user may have turned off is shown")
	assert.Equal(t, 1, f.notifier.Calls(), "the log still has it")
}

func TestTheServiceServesTheLog(t *testing.T) {
	f := newFixture(t, 1)
	svc := NewService(f.config())

	_, err := NewStore(f.pool).Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)

	log, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, log.Unread)

	require.NoError(t, svc.MarkAllRead(t.Context()))
	assert.Equal(t, 1, f.notifier.Calls())

	log, err = svc.List(t.Context())
	require.NoError(t, err)
	assert.Zero(t, log.Unread)

	require.NoError(t, svc.Clear(t.Context()))
	assert.Equal(t, 2, f.notifier.Calls())

	log, err = svc.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, log.Items)

	require.NoError(t, f.pool.Close())
	require.Error(t, svc.MarkAllRead(t.Context()))
	require.Error(t, svc.Clear(t.Context()))
	assert.Equal(t, 2, f.notifier.Calls(), "failures are not told")
}

func TestBannerStatusAndTest(t *testing.T) {
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
	f := newFixture(t, 0)
	svc := NewService(Config{Pool: f.pool})

	require.NotNil(t, svc.clock)
	assert.NotNil(t, svc.logger)
	assert.WithinDuration(t, time.Now(), svc.clock.Now(), time.Minute)
	svc.notify() // no notifier: nothing to tell
}
