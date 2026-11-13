package avatars

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// octoUser is the login most tests ask about.
const octoUser = "octo-user"

// errDown is a download that failed.
var errDown = errors.New("the avatar host is down")

// fakeFetcher hands out a picture per login, its bytes the login and how many times it was
// downloaded, and fails for the logins in fail.
type fakeFetcher struct {
	mu    sync.Mutex
	asked map[string]int
	fail  map[string]bool
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{asked: map[string]int{}, fail: map[string]bool{}}
}

func (f *fakeFetcher) FetchLoginAvatar(_ context.Context, login string) (*github.Avatar, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked[login]++

	if f.fail[login] {
		return nil, errDown
	}

	return &github.Avatar{ContentType: "image/png", Data: []byte(login + "#" + strconv.Itoa(f.asked[login]))}, nil
}

func (f *fakeFetcher) times(login string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.asked[login]
}

func (f *fakeFetcher) failing(login string, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fail[login] = fail
}

// rig is a Service over a fresh database, its fetcher, and its clock.
type rig struct {
	svc     *Service
	db      *sql.DB
	fetcher *fakeFetcher
	clock   *testutil.Clock
}

func newRig(t *testing.T) *rig {
	t.Helper()

	db := testutil.DB(t)
	fetcher := newFakeFetcher()
	clk := testutil.NewClock(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))

	return &rig{svc: NewService(Config{Pool: db, Fetcher: fetcher, Clock: clk}), db: db, fetcher: fetcher, clock: clk}
}

// dataURL is the data: URL of the fake picture of login's nth download.
func dataURL(login string, n int) string {
	return (&github.Avatar{ContentType: "image/png", Data: []byte(login + "#" + strconv.Itoa(n))}).DataURL()
}

func keptRows(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM avatars").Scan(&n))

	return n
}

func TestPicturesDownloadsEachLoginOnceAndKeepsIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	got, err := r.svc.Pictures(t.Context(), []string{"Octo-User", octoUser, "octo-other", ""})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"Octo-User": dataURL(octoUser, 1), octoUser: dataURL(octoUser, 1), "octo-other": dataURL("octo-other", 1),
	}, got, "each login as given, compared ignoring case")
	assert.Equal(t, 1, r.fetcher.times(octoUser))
	assert.Equal(t, 1, r.fetcher.times("octo-other"))
	assert.Equal(t, 2, keptRows(t, r.db))

	// A later look, within the week, is served from what was kept.
	r.clock.Advance(refreshAfter - time.Minute)

	got, err = r.svc.Pictures(t.Context(), []string{"OCTO-USER"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"OCTO-USER": dataURL(octoUser, 1)}, got)
	assert.Equal(t, 1, r.fetcher.times(octoUser), "nothing downloaded again")
}

func TestPicturesDownloadsAPictureAWeekOldAgain(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)

	r.clock.Advance(refreshAfter)

	got, err := r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{octoUser: dataURL(octoUser, 2)}, got, "the new picture")

	// A refresh that fails keeps the old picture, and isn't tried again at once.
	r.fetcher.failing(octoUser, true)
	r.clock.Advance(refreshAfter)

	got, err = r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{octoUser: dataURL(octoUser, 2)}, got, "the picture kept")

	_, err = r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Equal(t, 3, r.fetcher.times(octoUser))
}

func TestPicturesLeavesOutAFailedLoginAndTriesItAgainLater(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.fetcher.failing(octoUser, true)

	got, err := r.svc.Pictures(t.Context(), []string{octoUser, "octo-other"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"octo-other": dataURL("octo-other", 1)}, got)

	r.fetcher.failing(octoUser, false)
	r.clock.Advance(retryAfter - time.Second)

	got, err = r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Empty(t, got, "not tried again yet")
	assert.Equal(t, 1, r.fetcher.times(octoUser))

	r.clock.Advance(time.Second)

	got, err = r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{octoUser: dataURL(octoUser, 2)}, got)
}

func TestPicturesLooksAtTheFirstLoginsOnly(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	logins := make([]string, 0, MaxLogins+5)
	for i := range MaxLogins + 5 {
		logins = append(logins, "user-"+strconv.Itoa(i))
	}

	got, err := r.svc.Pictures(t.Context(), logins)
	require.NoError(t, err)
	assert.Len(t, got, MaxLogins)
	assert.NotContains(t, got, "user-"+strconv.Itoa(MaxLogins))
}

func TestPicturesAsksNothingForNoLogins(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	got, err := r.svc.Pictures(t.Context(), nil)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestPicturesFailsWhenTheKeptOnesCantBeRead(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{Pool: testutil.ClosedDB(t), Fetcher: newFakeFetcher()})

	_, err := svc.Pictures(t.Context(), []string{octoUser})
	require.ErrorContains(t, err, "read the kept profile pictures")
}

func TestPicturesServesWhatItDownloadedWhenKeepingFails(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.db.ExecContext(t.Context(), `CREATE TRIGGER no_keep BEFORE INSERT ON avatars
		BEGIN SELECT RAISE(ABORT, 'no room'); END`)
	require.NoError(t, err)

	got, err := r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{octoUser: dataURL(octoUser, 1)}, got)
	assert.Equal(t, 0, keptRows(t, r.db))
}

func TestPicturesStopsWithTheContext(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.svc.Pictures(ctx, []string{octoUser})
	require.ErrorIs(t, err, context.Canceled)

	// A download the window canceled is no failure: the next look tries it.
	got, err := r.svc.Pictures(t.Context(), []string{octoUser})
	require.NoError(t, err)
	assert.Contains(t, got, octoUser)
}

func TestPicturesPrunesWhatNoSheetShows(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.svc.Pictures(t.Context(), []string{"octo-old", "octo-shown"})
	require.NoError(t, err)

	// octo-shown is shown again after a day, which stamps it; octo-old never is.
	r.clock.Advance(touchAfter)
	_, err = r.svc.Pictures(t.Context(), []string{"octo-shown"})
	require.NoError(t, err)

	r.clock.Advance(unusedFor - touchAfter + time.Minute)
	_, err = r.svc.Pictures(t.Context(), []string{"octo-new"})
	require.NoError(t, err)

	var logins []string

	rows, err := r.db.QueryContext(t.Context(), "SELECT login FROM avatars ORDER BY login")
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var login string
		require.NoError(t, rows.Scan(&login))
		logins = append(logins, login)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"octo-new", "octo-shown"}, logins)
}

func TestPruneKeepsWithinTheCaps(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	now := r.clock.Now()
	big := make([]byte, maxKeptBytes/2)

	for i, login := range []string{"oldest", "older", "newest"} {
		_, err := r.db.ExecContext(t.Context(),
			"INSERT INTO avatars (login, mime, image, bytes, fetched_at, used_at) VALUES (?, 'image/png', ?, ?, ?, ?)",
			login, big, len(big), now, now.Add(time.Duration(i)*time.Minute))
		require.NoError(t, err)
	}

	require.NoError(t, r.svc.keep(t.Context(), nil, []string{"newest"}, now.Add(time.Hour)))
	assert.Equal(t, 2, keptRows(t, r.db), "two halves fill the cap")

	var oldest int
	require.NoError(t, r.db.QueryRowContext(t.Context(), "SELECT count(*) FROM avatars WHERE login = 'oldest'").Scan(&oldest))
	assert.Zero(t, oldest, "the least recently shown is dropped")
}
