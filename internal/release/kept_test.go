package release

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errDiskFull is a keeper that can't write.
var errDiskFull = errors.New("disk full")

// memoryKeeper keeps values in memory, as the prefs store keeps them on disk.
type memoryKeeper struct {
	mu     sync.Mutex
	values map[string]string
	setErr error
}

func (k *memoryKeeper) Get(_ context.Context, key string) (string, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	value, ok := k.values[key]

	return value, ok, nil
}

func (k *memoryKeeper) Set(_ context.Context, key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.setErr != nil {
		return k.setErr
	}

	if k.values == nil {
		k.values = map[string]string{}
	}

	k.values[key] = value

	return nil
}

// newKeptRig returns a start of the app over the rig's fakes that keeps its list with
// keeper, logging into the rig's logs.
func newKeptRig(keeper Keeper) (*rig, *testutil.Logs) {
	r, logs := newRig(), &testutil.Logs{}
	r.svc = NewService(Config{
		GitHub: r.github, Clones: r.clones, Checker: r.checker, Home: "/Users/example", Clock: r.clock,
		Events: r.events, Keeper: keeper, Logger: logs.Logger(),
	})

	return r, logs
}

// startsOver returns the app's first start over db's prefs, with its list read, and the
// next start, two hours on, with its Refresher running and web released meanwhile.
func startsOver(t *testing.T, db *sql.DB) (first Candidates, next *rig) {
	t.Helper()

	before, _ := newKeptRig(prefs.New(db, nil))

	first, err := before.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	require.False(t, first.Kept, "a list read now isn't a kept one")

	next, _ = newKeptRig(prefs.New(db, nil))
	next.clock.Advance(2 * time.Hour)
	next.github.compares[name("web").Key()] = Compare{}
	next.refresherOn()

	return first, next
}

// refresherOn marks the Refresher as running, as the desktop shell runs it.
func (r *rig) refresherOn() {
	r.svc.mu.Lock()
	defer r.svc.mu.Unlock()

	r.svc.background = true
}

// readBehind does the read the page asked the Refresher for, as the Refresher would, and
// fails the test when none was asked.
func (r *rig) readBehind(ctx context.Context, t *testing.T) {
	t.Helper()

	select {
	case <-r.svc.wake:
	default:
		require.FailNow(t, "the page asked for no read behind it")
	}

	r.svc.refreshBehind(ctx)
}

// askedBehind reports whether the page asked the Refresher for a read, taking the ask.
func (r *rig) askedBehind() bool {
	select {
	case <-r.svc.wake:
		return true
	default:
		return false
	}
}

func TestAKeptListIsShownFirstThenReplaced(t *testing.T) {
	t.Parallel()

	first, next := startsOver(t, testutil.DB(t))

	kept, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, kept.Kept, "the list from the last start is shown at once")
	assert.True(t, kept.Refreshing, "while GitHub is read again behind it")
	assert.Equal(t, groups(first), groups(kept))
	assert.Equal(t, first.CheckedAt, kept.CheckedAt, "with the time it was read, so its age shows")
	assert.Equal(t, first.WithoutGitHub, kept.WithoutGitHub)
	assert.Equal(t, first.Repos[1].Commits, kept.Repos[1].Commits)
	assert.Equal(t, first.Repos[1].Clone, kept.Repos[1].Clone)
	assert.Empty(t, kept.Error)
	assert.Zero(t, next.github.calls(), "nothing asked GitHub before it was shown")

	again, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, again.Refreshing, "shown again while the read runs")
	assert.Zero(t, next.github.calls())

	next.readBehind(t.Context(), t)
	assert.False(t, next.askedBehind(), "the page asked for one read, however often it was shown")
	assert.Equal(t, 1, next.github.calls())
	assert.Equal(t, 1, updates(next.events), "the page hears the new list landed")

	fresh, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, fresh.Kept)
	assert.False(t, fresh.Refreshing)
	assert.Contains(t, groups(fresh), "acme-org/web upToDate", "the new list replaces the kept one")
	assert.Equal(t, next.clock.Now().UTC().Format(time.RFC3339), fresh.CheckedAt)
	assert.Equal(t, 1, next.github.calls(), "and is fresh for five minutes")
}

func TestAKeptListSurvivesARestart(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	_, next := startsOver(t, db)

	_, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	assert.Equal(t, "master", next.svc.listedDefault(name("sdk")), "the kept list knows each repository")

	dir, ok := next.svc.listedClone("/Users/example/projects", func(dir string, origin repo.Name) bool {
		return origin.Key() == name("web").Key() && dir == "/Users/example/projects/web"
	})
	assert.True(t, ok, "and each clone's folder")
	assert.Equal(t, "/Users/example/projects/web", dir)

	next.readBehind(t.Context(), t)

	third, _ := newKeptRig(prefs.New(db, nil))
	third.refresherOn()

	kept, err := third.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, kept.Kept)
	assert.Contains(t, groups(kept), "acme-org/web upToDate", "the next start shows the list that landed last")
	assert.Equal(t, next.clock.Now().UTC().Format(time.RFC3339), kept.CheckedAt)
}

func TestAnotherRootsKeptListIsntShown(t *testing.T) {
	t.Parallel()

	_, next := startsOver(t, testutil.DB(t))
	next.clones.moveTo("/Users/example/work")
	next.github.fail(errGitHubDown)

	_, err := next.svc.Candidates(t.Context(), false)
	require.ErrorIs(t, err, errGitHubDown, "a list from another clone root is neither shown nor kept as the last")
	assert.Equal(t, 1, next.github.calls(), "the page waited for GitHub")
	assert.False(t, next.askedBehind())

	next.github.fail(nil)

	got, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.Kept)
	assert.False(t, got.Refreshing)
}

func TestAFailedReadBehindKeepsTheKeptList(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	first, next := startsOver(t, db)
	next.github.fail(errGitHubDown)

	_, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	next.readBehind(t.Context(), t)
	assert.Equal(t, 1, updates(next.events), "a read that failed tells the page too")

	got, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, got.Kept, "the kept list stays")
	assert.False(t, got.Refreshing)
	assert.Equal(t, "Couldn't read your repositories from GitHub.", got.Error, "with why it couldn't be read again")
	assert.Equal(t, first.CheckedAt, got.CheckedAt)
	assert.Equal(t, groups(first), groups(got))
	assert.False(t, next.askedBehind(), "and a read soon after asks nothing")

	third, _ := newKeptRig(prefs.New(db, nil))
	third.refresherOn()

	stillKept, err := third.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, first.CheckedAt, stillKept.CheckedAt, "a failed read keeps nothing for the next start")

	next.clock.Advance(FreshFor)
	next.github.fail(nil)

	got, err = next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, got.Refreshing, "five minutes on, it is read again behind the page")

	next.readBehind(t.Context(), t)

	healed, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, healed.Kept)
	assert.Empty(t, healed.Error)
}

func TestAReadBehindCutShortKeepsNothing(t *testing.T) {
	t.Parallel()

	_, next := startsOver(t, testutil.DB(t))

	_, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	next.readBehind(ctx, t)
	assert.Zero(t, updates(next.events), "nothing landed")
	assert.Zero(t, next.github.calls())

	got, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, got.Kept)
	assert.True(t, got.Refreshing, "the next showing asks again")

	next.readBehind(t.Context(), t)
	assert.Equal(t, 1, updates(next.events))
}

func TestAReadThePageAsksForReplacesTheKeptList(t *testing.T) {
	t.Parallel()

	_, next := startsOver(t, testutil.DB(t))

	_, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	got, err := next.svc.Candidates(t.Context(), true)
	require.NoError(t, err)
	assert.False(t, got.Kept, "Read GitHub again waits for GitHub")
	assert.Equal(t, 1, next.github.calls())

	next.readBehind(t.Context(), t)
	assert.Equal(t, 1, next.github.calls(), "the read asked for behind the page isn't needed any more")
}

func TestWithoutTheRefresherTheKeptListWaitsForGitHub(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	_, _ = startsOver(t, db)

	next, _ := newKeptRig(prefs.New(db, nil))

	got, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.Kept, "nothing would read GitHub behind the page")
	assert.Equal(t, 1, next.github.calls())
}

func TestAKeptListThatCantBeReadIsLeft(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"not a list", `{"version":99,"root":"/Users/example/projects","repos":[]}`} {
		keeper := &memoryKeeper{values: map[string]string{keptKey: raw}}
		r, logs := newKeptRig(keeper)
		r.refresherOn()

		got, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.False(t, got.Kept, "%s: the list is read from GitHub", raw)
		assert.Equal(t, 1, r.github.calls())

		if raw == "not a list" {
			assert.Contains(t, logs.String(), "couldn't read the Release page's kept list")
		} else {
			assert.Empty(t, logs.String(), "a list kept by another build is passed over")
		}
	}
}

func TestAListThatCantBeKeptIsStillShown(t *testing.T) {
	t.Parallel()

	r, logs := newKeptRig(&memoryKeeper{setErr: errDiskFull})

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.NotEmpty(t, got.Repos)
	assert.Contains(t, logs.String(), "couldn't keep the Release page's list for the next start")
	assert.Contains(t, logs.String(), errDiskFull.Error())
}

func TestTheRefresherReadsBehindThePage(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		keeper := &memoryKeeper{}
		before, _ := newKeptRig(keeper)

		_, err := before.svc.Candidates(t.Context(), false)
		require.NoError(t, err)

		next, _ := newKeptRig(keeper)
		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			NewRefresher(next.svc).Run(ctx)
		}()

		synctest.Wait()

		kept, err := next.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.True(t, kept.Kept)

		synctest.Wait()
		assert.Equal(t, 1, next.github.calls(), "the Refresher read GitHub")
		assert.Equal(t, 1, updates(next.events))

		fresh, err := next.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.False(t, fresh.Kept)

		stop()
		<-done

		next.svc.mu.Lock()
		running := next.svc.background
		next.svc.mu.Unlock()
		assert.False(t, running, "once it stops, a kept list waits for GitHub again")
	})
}
