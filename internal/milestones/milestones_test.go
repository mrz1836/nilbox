package milestones

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata" // the daylight-saving tests need a real zone on any machine

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// zone is where the tests' user is, five hours behind UTC, so a local day and a UTC day
// differ.
func zone() *time.Location {
	return time.FixedZone("UTC-5", -5*60*60)
}

// monday is a Monday afternoon where the user is, which earns no badge of its own.
func monday() time.Time {
	return time.Date(2026, 6, 15, 14, 0, 0, 0, zone())
}

// The reaches the tests make: an inbox emptied while the user watched, loud and quiet, and
// one found empty.
const cleared = 3

func earned() Reach { return Reach{Earned: true, Cleared: cleared, Loud: true} }
func quiet() Reach  { return Reach{Earned: true, Cleared: cleared} }
func found() Reach  { return Reach{Loud: true} }

// rig is a service over a fresh database, on a clock the test moves.
type rig struct {
	db    *sql.DB
	clock *testutil.Clock
	svc   *Service
	loc   *time.Location
}

// newRig returns a rig whose clock reads at, counting days in at's zone.
func newRig(t *testing.T, at time.Time) *rig {
	t.Helper()

	return rigOver(t, testutil.DB(t), at)
}

// rigOver returns a rig over db.
func rigOver(t *testing.T, db *sql.DB, at time.Time) *rig {
	t.Helper()

	r := &rig{db: db, clock: testutil.NewClock(at), loc: at.Location()}
	r.svc = NewService(db, WithClock(r.clock), WithLocation(r.loc))

	return r
}

// reach records reach at the clock's time.
func (r *rig) reach(t *testing.T, reach Reach) Moment {
	t.Helper()

	got, err := r.svc.Reached(t.Context(), reach)
	require.NoError(t, err)
	require.NotNil(t, got.Unlocked, "the window gets a list, never null")

	return got
}

// at moves the clock to at and records reach there.
func (r *rig) at(t *testing.T, at time.Time, reach Reach) Moment {
	t.Helper()

	r.clock.Set(at)

	return r.reach(t, reach)
}

// stored returns the log as stored.
func (r *rig) stored(t *testing.T) string {
	t.Helper()

	got, ok, err := prefs.New(r.db, nil).Get(t.Context(), KeyLog)
	require.NoError(t, err)
	require.True(t, ok, "the log is stored")

	return got
}

// plant stores value as the log.
func (r *rig) plant(t *testing.T, value string) {
	t.Helper()

	require.NoError(t, prefs.New(r.db, nil).Set(t.Context(), KeyLog, value))
}

// days returns day, n days after start, at the same time of day.
func days(start time.Time, n int) time.Time {
	return start.AddDate(0, 0, n)
}

func TestTheFirstNilTheUserEarnsUnlocksFirstAndThrowsConfetti(t *testing.T) {
	t.Parallel()

	r := newRig(t, monday())

	got := r.reach(t, earned())
	assert.Equal(t, Moment{
		Earned: true, Cleared: 3, Streak: 1, Best: 1, Count: 1,
		Unlocked: []Achievement{{Kind: KindFirst, N: 1}}, Confetti: true,
	}, got)

	assert.JSONEq(t,
		`{"count":1,"streak":1,"best":1,"lastDay":"2026-06-15","confettiDay":"2026-06-15","unlocked":["first:1"]}`,
		r.stored(t))
}

func TestAStreakRunsDayToDay(t *testing.T) {
	t.Parallel()

	start := monday()
	r := newRig(t, start)

	steps := []struct {
		at           time.Time
		streak, best int
		why          string
	}{
		{start, 1, 1, "the first day starts a streak"},
		{start.Add(3 * time.Hour), 1, 1, "the same day leaves it"},
		{days(start, 1), 2, 2, "the next day carries it on"},
		{days(start, 2), 3, 3, "and the next"},
		{days(start, 2).Add(time.Hour), 3, 3, "the same day again leaves it"},
		{days(start, 4), 1, 3, "a day missed starts it again, and the best stands"},
		{days(start, 5), 2, 3, "the new one carries on"},
	}

	for _, step := range steps {
		got := r.at(t, step.at, found())
		assert.Equal(t, step.streak, got.Streak, step.why)
		assert.Equal(t, step.best, got.Best, step.why)
	}
}

func TestADayEndsAtMidnightWhereTheUserIs(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	r := newRig(t, time.Date(2026, 11, 19, 23, 30, 0, 0, newYork))
	r.reach(t, found())

	// An hour later it is the next day in New York, though the same day in UTC.
	got := r.at(t, time.Date(2026, 11, 20, 0, 30, 0, 0, newYork), found())
	assert.Equal(t, 2, got.Streak, "past midnight where the user is, the next day")

	// The UTC day turns at 19:00 in New York, which ends no day of theirs.
	got = r.at(t, time.Date(2026, 11, 20, 19, 30, 0, 0, newYork), found())
	assert.Equal(t, 2, got.Streak, "before midnight where the user is, the same day")
}

func TestADayAClockChangeMakesLongerOrShorterStillFollowsTheOneBefore(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	for _, change := range []struct {
		name string
		day  time.Time
	}{
		{"spring forward", time.Date(2026, 3, 8, 0, 30, 0, 0, newYork)},
		{"fall back", time.Date(2026, 11, 1, 23, 30, 0, 0, newYork)},
	} {
		t.Run(change.name, func(t *testing.T) {
			t.Parallel()

			before := time.Date(change.day.Year(), change.day.Month(), change.day.Day()-1, 23, 59, 0, 0, newYork)
			r := newRig(t, before)
			r.reach(t, found())

			assert.Equal(t, 2, r.at(t, change.day, found()).Streak)
		})
	}
}

func TestAClockMovedBackLeavesTheStreak(t *testing.T) {
	t.Parallel()

	start := monday()
	r := newRig(t, start)

	r.reach(t, found())
	r.at(t, days(start, 1), found())

	got := r.at(t, days(start, -3), found())
	assert.Equal(t, 2, got.Streak, "a day before the last one is not a day missed")

	got = r.at(t, days(start, 2), found())
	assert.Equal(t, 3, got.Streak, "the streak carries on from the last day")
}

func TestAFoundNilNeverCountsOrUnlocks(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 19, 9, 0, 0, 0, zone()) // a Friday morning
	r := newRig(t, start)

	for day := range 3 {
		got := r.at(t, days(start, day), Reach{Cleared: 40, Loud: true})
		assert.Equal(t, 0, got.Count, "day %d", day)
		assert.Empty(t, got.Unlocked, "day %d", day)
		assert.False(t, got.Confetti, "day %d", day)
	}

	assert.JSONEq(t,
		`{"count":0,"streak":3,"best":3,"lastDay":"2026-06-21","confettiDay":"","unlocked":[]}`,
		r.stored(t))
}

func TestAStreakTierReachedOnAFoundDayUnlocksWithTheNextEarnedNil(t *testing.T) {
	t.Parallel()

	start := monday()
	r := newRig(t, start)

	r.reach(t, found())
	r.at(t, days(start, 1), found())
	got := r.at(t, days(start, 2), found())
	assert.Equal(t, 3, got.Streak)
	assert.Empty(t, got.Unlocked)

	got = r.at(t, days(start, 2).Add(time.Hour), earned())
	assert.Equal(t, []Achievement{{KindStreak, 3}, {KindFirst, 1}}, got.Unlocked)
	assert.True(t, got.Confetti)
}

func TestUnlocksComeMostSignificantFirst(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 17, 9, 0, 0, 0, zone()) // a Wednesday morning
	r := newRig(t, start)

	r.reach(t, found())
	r.at(t, days(start, 1), found())

	// Friday morning, the third day running, the first nil earned, after a sweep.
	got := r.at(t, days(start, 2), Reach{Earned: true, Cleared: 14, Loud: true})
	assert.Equal(t, []Achievement{
		{KindStreak, 3}, {KindFirst, 1}, {KindSweep, SweepAt}, {KindEarly, 1}, {KindFriday, 1},
	}, got.Unlocked)
	assert.Equal(t, 14, got.Cleared)
}

func TestTheBadgesFollowTheTimeWhereTheUserIs(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		at   time.Time
		want []Achievement
	}{
		{"monday afternoon", monday(), nil},
		{"morning", time.Date(2026, 6, 15, 5, 0, 0, 0, zone()), []Achievement{{KindEarly, 1}}},
		{"just before noon", time.Date(2026, 6, 15, 11, 59, 0, 0, zone()), []Achievement{{KindEarly, 1}}},
		{"late", time.Date(2026, 6, 15, 22, 0, 0, 0, zone()), []Achievement{{KindOwl, 1}}},
		{"before dawn", time.Date(2026, 6, 16, 4, 59, 0, 0, zone()), []Achievement{{KindOwl, 1}}},
		{"friday", time.Date(2026, 6, 19, 15, 0, 0, 0, zone()), []Achievement{{KindFriday, 1}}},
		{"saturday", time.Date(2026, 6, 20, 15, 0, 0, 0, zone()), []Achievement{{KindWeekend, 1}}},
		{"sunday night", time.Date(2026, 6, 21, 23, 0, 0, 0, zone()), []Achievement{{KindWeekend, 1}, {KindOwl, 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := newRig(t, tc.at).reach(t, earned())
			assert.Equal(t, append([]Achievement{{KindFirst, 1}}, tc.want...), got.Unlocked)
		})
	}
}

func TestEachAchievementUnlocksOnce(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 6, 19, 9, 0, 0, 0, zone()) // a Friday morning
	r := newRig(t, start)

	assert.NotEmpty(t, r.reach(t, Reach{Earned: true, Cleared: 20, Loud: true}).Unlocked)

	// A week on: Friday morning again, another sweep.
	got := r.at(t, days(start, 7), Reach{Earned: true, Cleared: 20, Loud: true})
	assert.Empty(t, got.Unlocked)
	assert.False(t, got.Confetti, "nothing unlocked throws nothing")
	assert.Equal(t, 2, got.Count)
}

func TestTheStreakTiersNeverRunOut(t *testing.T) {
	t.Parallel()

	start := monday()
	r := newRig(t, start)

	for day := range 60 {
		r.at(t, days(start, day), found())
	}

	// Every tier the streak passed on found days unlocks with the first earned nil.
	got := r.at(t, days(start, 59).Add(time.Hour), earned())
	assert.Equal(t, 60, got.Streak)
	assert.Equal(t, []Achievement{
		{KindStreak, 60}, {KindStreak, 30}, {KindStreak, 14}, {KindStreak, 7}, {KindStreak, 3}, {KindFirst, 1},
	}, got.Unlocked)

	r.plant(t, `{"count":5,"streak":89,"best":89,"lastDay":"2026-09-11",`+
		`"unlocked":["streak:3","streak:7","streak:14","streak:30","streak:60","first:1"]}`)

	got = r.at(t, time.Date(2026, 9, 12, 14, 0, 0, 0, zone()), earned())
	assert.Equal(t, 90, got.Streak)
	assert.Equal(t, []Achievement{{KindStreak, 90}, {KindWeekend, 1}}, got.Unlocked, "every 30 days after 30")
}

func TestTheCountTiersNeverRunOut(t *testing.T) {
	t.Parallel()

	r := newRig(t, monday())

	for n := 1; n <= 10; n++ {
		got := r.reach(t, earned())
		assert.Equal(t, n, got.Count)

		if n == 10 {
			assert.Equal(t, []Achievement{{KindCount, 10}}, got.Unlocked)
		}
	}

	r.plant(t, `{"count":199,"streak":1,"best":1,"lastDay":"2026-06-15","unlocked":["count:10","count:25","count:50","count:100","first:1"]}`)

	got := r.reach(t, earned())
	assert.Equal(t, 200, got.Count)
	assert.Equal(t, []Achievement{{KindCount, 200}}, got.Unlocked, "every 100th after the 100th")

	got = r.reach(t, earned())
	assert.Equal(t, 201, got.Count)
	assert.Empty(t, got.Unlocked)
}

func TestConfettiFliesOnceADayAcrossRestarts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	start := time.Date(2026, 6, 19, 9, 0, 0, 0, zone()) // a Friday morning

	r := rigOver(t, testutil.DBAt(t, path), start)
	assert.True(t, r.reach(t, Reach{Earned: true, Loud: true}).Confetti, "the first nil")
	require.NoError(t, r.db.Close())

	// The app starts again the same day, and a sweep unlocks something new.
	r = rigOver(t, testutil.DBAt(t, path), start.Add(2*time.Hour))
	got := r.reach(t, Reach{Earned: true, Cleared: 12, Loud: true})
	assert.Equal(t, []Achievement{{KindSweep, SweepAt}}, got.Unlocked, "unlocked all the same")
	assert.False(t, got.Confetti, "confetti already flew today")

	// The next day, a weekend.
	got = r.at(t, days(start, 1), Reach{Earned: true, Loud: true})
	assert.Equal(t, []Achievement{{KindWeekend, 1}}, got.Unlocked)
	assert.True(t, got.Confetti, "a new day")
}

func TestAQuietNilUnlocksWithoutConfetti(t *testing.T) {
	t.Parallel()

	r := newRig(t, monday())

	got := r.reach(t, quiet())
	assert.Equal(t, []Achievement{{KindFirst, 1}}, got.Unlocked)
	assert.False(t, got.Confetti)

	// Nothing flew, so a loud unlock later that day still throws it.
	r.plant(t, `{"count":9,"streak":1,"best":1,"lastDay":"2026-06-15","unlocked":["first:1"]}`)
	assert.True(t, r.reach(t, earned()).Confetti)
}

func TestAnUnreadableLogStartsAfresh(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{
		"{broken",
		"[]",
		`"a string"`,
	} {
		t.Run(stored, func(t *testing.T) {
			t.Parallel()

			r := newRig(t, monday())
			r.plant(t, stored)

			got := r.reach(t, earned())
			assert.Equal(t, Moment{
				Earned: true, Cleared: 3, Streak: 1, Best: 1, Count: 1,
				Unlocked: []Achievement{{KindFirst, 1}}, Confetti: true,
			}, got)
		})
	}
}

func TestALogNoBuildWritesReadsAsNeverHavingHappened(t *testing.T) {
	t.Parallel()

	r := newRig(t, monday())
	r.plant(t, `{"count":-4,"streak":12,"best":-1,"lastDay":"someday","confettiDay":"2026-06-15T00:00:00Z","unlocked":null}`)

	got := r.reach(t, Reach{Earned: true, Cleared: -2, Loud: true})
	assert.Equal(t, Moment{
		Earned: true, Cleared: 0, Streak: 1, Best: 1, Count: 1,
		Unlocked: []Achievement{{KindFirst, 1}}, Confetti: true,
	}, got)
}

func TestAClosedDatabaseRecordsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t, monday())
	require.NoError(t, r.db.Close())

	got, err := r.svc.Reached(t.Context(), earned())
	require.ErrorContains(t, err, "record the inbox at nil")
	assert.Equal(t, Moment{Unlocked: []Achievement{}}, got)
}

func TestAServiceWithoutOptionsCountsDaysWhereTheMacIs(t *testing.T) {
	t.Parallel()

	svc := NewService(testutil.DB(t))
	assert.Equal(t, time.Local, svc.loc) //nolint:gosmopolitan // the default is where the Mac is

	got, err := svc.Reached(t.Context(), found())
	require.NoError(t, err)
	assert.Equal(t, 1, got.Streak)
}
