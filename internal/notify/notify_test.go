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
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
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
	db       *gorm.DB
	clock    *testutil.Clock
	poster   *fakePoster
	notifier *testutil.Notifier
	items    []int64
}

// newFixture opens a fresh database holding n pull requests in acme-org/example-repo,
// numbered from 1.
func newFixture(t *testing.T, n int) *fixture {
	t.Helper()

	pool, db := testutil.BridgedDB(t)

	var accountID, repoID int64

	require.NoError(t, db.Raw(`INSERT INTO accounts (login, created_at, updated_at) VALUES ('octo-user', ?, ?)
RETURNING id`, testNow, testNow).Scan(&accountID).Error)
	require.NoError(t, db.Raw(`INSERT INTO repos (owner, name, created_at, updated_at)
VALUES ('acme-org', 'example-repo', ?, ?) RETURNING id`, testNow, testNow).Scan(&repoID).Error)

	f := &fixture{
		pool: pool, db: db, clock: testutil.NewClock(testNow), poster: &fakePoster{status: StatusOn},
		notifier: &testutil.Notifier{},
	}

	for i := 1; i <= n; i++ {
		var id int64

		require.NoError(t, db.Raw(`INSERT INTO items (account_id, repo_id, kind, number, item_created_at,
item_updated_at, fetched_at) VALUES (?, ?, 'pr', ?, ?, ?, ?) RETURNING id`,
			accountID, repoID, i, testNow, testNow, testNow).Scan(&id).Error)

		f.items = append(f.items, id)
	}

	return f
}

func (f *fixture) config() Config {
	return Config{Pool: f.pool, DB: f.db, Poster: f.poster, Notifier: f.notifier, Clock: f.clock}
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
	s := NewStore(f.db)

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
}

func TestRecordingNothingStoresNothing(t *testing.T) {
	f := newFixture(t, 0)

	rows, err := NewStore(f.db).Record(t.Context(), testNow, nil)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestTheLogKeepsOnlyTheNewest(t *testing.T) {
	f := newFixture(t, 1)
	s := NewStore(f.db)

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
	s := NewStore(f.db)

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
	s := NewStore(f.db)

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.NoError(t, err)
	require.NoError(t, f.db.Exec("DELETE FROM items").Error)

	log, err := s.Log(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1)
	assert.Zero(t, log.Items[0].ItemID)
	assert.Equal(t, "https://github.com/acme-org/example-repo/pull/1", log.Items[0].URL, "the copy still reads")
}

func TestTheStoreFailsOnAClosedDatabase(t *testing.T) {
	f := newFixture(t, 1)
	s := NewStore(f.db)
	require.NoError(t, store.Close(f.db))

	_, err := s.Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	require.Error(t, err)

	_, err = s.Log(t.Context())
	require.Error(t, err)
	require.Error(t, s.MarkAllRead(t.Context(), testNow))
	require.Error(t, s.Clear(t.Context()))
}

func TestTheDispatcherRecordsAndShowsBanners(t *testing.T) {
	f := newFixture(t, 2)
	d := NewDispatcher(f.config())

	d.Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested), f.arrival(1, KindAssigned)})

	log, err := NewStore(f.db).Log(t.Context())
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

	log, err := NewStore(f.db).Log(t.Context())
	require.NoError(t, err)
	assert.Len(t, log.Items, 2, "the log keeps every kind")
}

func TestTheDispatcherKeepsTheLogWithoutAPoster(t *testing.T) {
	f := newFixture(t, 1)
	cfg := f.config()
	cfg.Poster = nil

	NewDispatcher(cfg).Arrived(t.Context(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})

	log, err := NewStore(f.db).Log(t.Context())
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

	require.NoError(t, store.Close(f.db))
	d.Arrived(context.Background(), []inbox.Arrival{f.arrival(0, KindReviewRequested)})
	assert.Equal(t, 1, f.notifier.Calls(), "nothing was recorded")
}

func TestTheServiceServesTheLog(t *testing.T) {
	f := newFixture(t, 1)
	svc := NewService(f.config())

	_, err := NewStore(f.db).Record(t.Context(), testNow, []inbox.Arrival{f.arrival(0, KindReviewRequested)})
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

	require.NoError(t, store.Close(f.db))
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
	svc := NewService(Config{DB: f.db})

	require.NotNil(t, svc.clock)
	assert.NotNil(t, svc.logger)
	assert.WithinDuration(t, time.Now(), svc.clock.Now(), time.Minute)
	svc.notify() // no notifier: nothing to tell
}
