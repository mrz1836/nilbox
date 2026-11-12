package prefs_test

import (
	"database/sql"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errExample = errors.New("example failure")

// Keys the tests store values under, and values they store.
const (
	keyA = "example.a"
	keyB = "example.b"

	valueOne = "one"
	valueTwo = "two"
)

// start is the time the tests' clocks begin at.
var start = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test time

// refuseKey makes every write of keyB fail, the way a full disk would, so a test can reach
// the error a write reports.
const refuseKey = `CREATE TRIGGER refuse_example_b BEFORE INSERT ON prefs WHEN NEW.key = 'example.b'
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`

// newStore returns a Store over a fresh database, and the database.
func newStore(t *testing.T) (*prefs.Store, *sql.DB) {
	t.Helper()

	db := testutil.DB(t)

	return prefs.New(db, testutil.NewClock(start)), db
}

// closedStore returns a Store over a database that is already closed.
func closedStore(t *testing.T) *prefs.Store {
	t.Helper()

	return prefs.New(testutil.ClosedDB(t), nil)
}

// stored returns every row in the prefs table, by key.
func stored(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), "SELECT key, value FROM prefs")
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	out := map[string]string{}

	for rows.Next() {
		var key, value string
		require.NoError(t, rows.Scan(&key, &value))

		out[key] = value
	}

	require.NoError(t, rows.Err())

	return out
}

// updatedAt returns the stored updated_at of key, as the text in the database.
func updatedAt(t *testing.T, db *sql.DB, key string) string {
	t.Helper()

	// The cast keeps the driver from parsing the DATETIME column back into a time.Time.
	var text string
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT CAST(updated_at AS TEXT) FROM prefs WHERE key = ?", key).Scan(&text))

	return text
}

func TestGet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		set       map[string]string
		wantValue string
		wantOK    bool
	}{
		{name: "nothing stored", wantValue: "", wantOK: false},
		{name: "a value", set: map[string]string{keyA: valueOne}, wantValue: valueOne, wantOK: true},
		{name: "an empty value is stored", set: map[string]string{keyA: ""}, wantValue: "", wantOK: true},
		{name: "another key's value", set: map[string]string{keyB: valueTwo}, wantValue: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newStore(t)
			require.NoError(t, s.SetMany(t.Context(), tt.set))

			value, ok, err := s.Get(t.Context(), keyA)
			require.NoError(t, err)
			assert.Equal(t, tt.wantValue, value)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestSetReplacesTheValue(t *testing.T) {
	t.Parallel()

	s, db := newStore(t)

	require.NoError(t, s.Set(t.Context(), keyA, valueOne))
	require.NoError(t, s.Set(t.Context(), keyA, valueTwo))
	require.NoError(t, s.Set(t.Context(), keyB, "three"))

	assert.Equal(t, map[string]string{keyA: valueTwo, keyB: "three"}, stored(t, db))
}

func TestGetMany(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		keys []string
		want map[string]string
	}{
		{name: "no keys", keys: nil, want: map[string]string{}},
		{name: "one key", keys: []string{keyA}, want: map[string]string{keyA: valueOne}},
		{
			name: "keys with no value are left out", keys: []string{keyA, "example.missing", "example.c"},
			want: map[string]string{keyA: valueOne, "example.c": ""},
		},
		{name: "only keys with no value", keys: []string{"example.missing"}, want: map[string]string{}},
		{name: "a key twice", keys: []string{keyB, keyB}, want: map[string]string{keyB: valueTwo}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newStore(t)
			require.NoError(t, s.SetMany(t.Context(), map[string]string{
				keyA: valueOne, keyB: valueTwo, "example.c": "",
			}))

			got, err := s.GetMany(t.Context(), tt.keys...)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSetManyStoresAllOrNothing(t *testing.T) {
	t.Parallel()

	s, db := newStore(t)

	require.NoError(t, s.SetMany(t.Context(), nil), "nothing to store is not an error")
	assert.Empty(t, stored(t, db))

	require.NoError(t, s.SetMany(t.Context(), map[string]string{keyA: valueOne, "example.c": "three"}))

	_, err := db.ExecContext(t.Context(), refuseKey)
	require.NoError(t, err)

	err = s.SetMany(t.Context(), map[string]string{keyA: "changed", keyB: valueTwo, "example.c": "changed"})
	require.ErrorContains(t, err, "save setting "+keyB)
	assert.Equal(t, map[string]string{keyA: valueOne, "example.c": "three"}, stored(t, db),
		"the values written before the failure are rolled back")
}

func TestWritesStampUpdatedAtFromTheClock(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	clk := testutil.NewClock(start.In(time.FixedZone("UTC+2", 2*60*60)))
	s := prefs.New(db, clk)

	require.NoError(t, s.Set(t.Context(), keyA, valueOne))
	assert.Equal(t, "2026-03-04 12:00:00+00:00", updatedAt(t, db, keyA), "stamped in UTC")

	clk.Advance(time.Minute)
	require.NoError(t, s.SetMany(t.Context(), map[string]string{keyA: valueTwo, keyB: valueTwo}))
	assert.Equal(t, "2026-03-04 12:01:00+00:00", updatedAt(t, db, keyA), "a write moves the stamp on")
	assert.Equal(t, "2026-03-04 12:01:00+00:00", updatedAt(t, db, keyB))

	clk.Advance(time.Minute)
	require.NoError(t, s.Update(t.Context(), keyB, func(string, bool) (string, error) { return "three", nil }))
	assert.Equal(t, "2026-03-04 12:02:00+00:00", updatedAt(t, db, keyB))

	clk.Advance(time.Minute)
	require.NoError(t, s.SetList(t.Context(), "example.c", []string{"x"}))
	assert.Equal(t, "2026-03-04 12:03:00+00:00", updatedAt(t, db, "example.c"))
}

func TestANilClockIsTheSystemClock(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	before := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, prefs.New(db, nil).Set(t.Context(), keyA, valueOne))

	var at time.Time
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT updated_at FROM prefs WHERE key = 'example.a'").Scan(&at))
	assert.WithinRange(t, at, before, time.Now().UTC().Add(time.Second))
}

func TestDelete(t *testing.T) {
	t.Parallel()

	s, db := newStore(t)
	require.NoError(t, s.SetMany(t.Context(), map[string]string{keyA: valueOne, keyB: valueTwo}))

	require.NoError(t, s.Delete(t.Context(), keyA))
	require.NoError(t, s.Delete(t.Context(), "example.missing"), "a key with no value is not an error")

	_, ok, err := s.Get(t.Context(), keyA)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, map[string]string{keyB: valueTwo}, stored(t, db))
}

func TestGetList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		value    *string
		wantList []string
		wantOK   bool
		wantErr  string
	}{
		{name: "nothing stored"},
		{name: "an empty value", value: new(""), wantList: nil, wantOK: false},
		{name: "a list", value: new(`["b","a"]`), wantList: []string{"b", "a"}, wantOK: true},
		{name: "an empty list", value: new(`[]`), wantList: []string{}, wantOK: true},
		{name: "not JSON", value: new("not json"), wantErr: "read setting example.list"},
		{name: "not a list of strings", value: new(`[1, 2]`), wantErr: "read setting example.list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newStore(t)

			if tt.value != nil {
				require.NoError(t, s.Set(t.Context(), "example.list", *tt.value))
			}

			list, ok, err := s.GetList(t.Context(), "example.list")

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Nil(t, list)
				assert.False(t, ok)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantList, list)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestSetList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		list []string
		want string
	}{
		{name: "a list", list: []string{"b", "a"}, want: `["b","a"]`},
		{name: "an empty list", list: []string{}, want: `[]`},
		{name: "a nil list is stored empty", list: nil, want: `[]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := newStore(t)
			require.NoError(t, s.SetList(t.Context(), "example.list", tt.list))
			assert.Equal(t, tt.want, stored(t, db)["example.list"])

			list, ok, err := s.GetList(t.Context(), "example.list")
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, append([]string{}, tt.list...), list)
		})
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		before    *string
		fn        func(value string, ok bool) (string, error)
		wantErr   error
		wantStore map[string]string
	}{
		{
			name: "a key with no value",
			fn: func(value string, ok bool) (string, error) {
				return value + "|" + strconv.FormatBool(ok), nil
			},
			wantStore: map[string]string{keyA: "|false"},
		},
		{
			name: "a stored value", before: new(valueOne),
			fn: func(value string, ok bool) (string, error) {
				return value + "|" + strconv.FormatBool(ok), nil
			},
			wantStore: map[string]string{keyA: "one|true"},
		},
		{
			name: "an empty stored value", before: new(""),
			fn: func(value string, ok bool) (string, error) {
				return value + "|" + strconv.FormatBool(ok), nil
			},
			wantStore: map[string]string{keyA: "|true"},
		},
		{
			name: "fn refuses", before: new(valueOne),
			fn:        func(string, bool) (string, error) { return "never", errExample },
			wantErr:   errExample,
			wantStore: map[string]string{keyA: valueOne},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := newStore(t)

			if tt.before != nil {
				require.NoError(t, s.Set(t.Context(), keyA, *tt.before))
			}

			err := s.Update(t.Context(), keyA, tt.fn)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.EqualError(t, err, tt.wantErr.Error(), "fn's error is returned as it is")
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantStore, stored(t, db))
		})
	}
}

func TestConcurrentUpdatesLoseNoChange(t *testing.T) {
	t.Parallel()

	const writers = 24

	s, _ := newStore(t)

	increment := func(value string, _ bool) (string, error) {
		n, _ := strconv.Atoi(value) // no value counts from zero

		return strconv.Itoa(n + 1), nil
	}

	var g errgroup.Group

	for range writers {
		g.Go(func() error { return s.Update(t.Context(), "example.count", increment) })
	}

	require.NoError(t, g.Wait())

	got, _, err := s.Get(t.Context(), "example.count")
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(writers), got, "every update read the one before it")
}

func TestWithTxJoinsTheTransaction(t *testing.T) {
	t.Parallel()

	s, db := newStore(t)
	require.NoError(t, s.Set(t.Context(), keyA, "before"))

	err := store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		in := s.WithTx(tx)

		require.NoError(t, in.Set(t.Context(), keyA, "changed"))
		require.NoError(t, in.SetMany(t.Context(), map[string]string{keyB: valueTwo}))
		require.NoError(t, in.SetList(t.Context(), "example.list", []string{"x"}))
		require.NoError(t, in.Update(t.Context(), keyB, func(value string, _ bool) (string, error) {
			return value + "+", nil
		}))
		require.NoError(t, in.Delete(t.Context(), "example.missing"))

		got, err := in.GetMany(t.Context(), keyA, keyB)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{keyA: "changed", keyB: "two+"}, got,
			"reads in the transaction see its writes")

		return errExample
	})
	require.ErrorIs(t, err, errExample)

	assert.Equal(t, map[string]string{keyA: "before"}, stored(t, db),
		"every write in a rolled back transaction is gone")

	require.NoError(t, store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		in := s.WithTx(tx)

		if err := in.Delete(t.Context(), keyA); err != nil {
			return err
		}

		return in.Set(t.Context(), keyB, "kept")
	}))

	assert.Equal(t, map[string]string{keyB: "kept"}, stored(t, db), "a committed transaction keeps them")
}

func TestUpdateInAJoinedTransactionRollsBackWithIt(t *testing.T) {
	t.Parallel()

	s, db := newStore(t)

	err := store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		if err := s.WithTx(tx).Update(t.Context(), keyA, func(string, bool) (string, error) {
			return "written", nil
		}); err != nil {
			return err
		}

		return errExample
	})
	require.ErrorIs(t, err, errExample)
	assert.Empty(t, stored(t, db))
}

func TestErrorsNameTheKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(t *testing.T, s *prefs.Store) error
		want string
	}{
		{
			name: "get", want: "read setting " + keyA,
			run: func(t *testing.T, s *prefs.Store) error { _, _, err := s.Get(t.Context(), keyA); return err },
		},
		{
			name: "get many", want: "read settings",
			run: func(t *testing.T, s *prefs.Store) error {
				_, err := s.GetMany(t.Context(), keyA, keyB)
				return err
			},
		},
		{
			name: "get a list", want: "read setting " + keyA,
			run: func(t *testing.T, s *prefs.Store) error { _, _, err := s.GetList(t.Context(), keyA); return err },
		},
		{
			name: "set", want: "save setting " + keyA,
			run: func(t *testing.T, s *prefs.Store) error { return s.Set(t.Context(), keyA, valueOne) },
		},
		{
			name: "set a list", want: "save setting " + keyA,
			run: func(t *testing.T, s *prefs.Store) error { return s.SetList(t.Context(), keyA, nil) },
		},
		{
			name: "delete", want: "delete setting " + keyA,
			run: func(t *testing.T, s *prefs.Store) error { return s.Delete(t.Context(), keyA) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorContains(t, tt.run(t, closedStore(t)), tt.want)
		})
	}
}

func TestTransactionsReportADatabaseTheyCannotUse(t *testing.T) {
	t.Parallel()

	s := closedStore(t)

	require.ErrorContains(t, s.SetMany(t.Context(), map[string]string{keyA: valueOne}), "begin transaction")
	require.ErrorContains(t, s.Update(t.Context(), keyA, func(string, bool) (string, error) {
		return valueOne, nil
	}), "begin transaction")

	got, err := s.GetMany(t.Context())
	require.NoError(t, err, "no keys read nothing, so the database is not asked")
	assert.Empty(t, got)
}

func TestUpdateReportsAReadOrWriteItCannotMake(t *testing.T) {
	t.Parallel()

	s, db := newStore(t)

	_, err := db.ExecContext(t.Context(), refuseKey)
	require.NoError(t, err)

	err = s.Update(t.Context(), keyB, func(string, bool) (string, error) { return valueTwo, nil })
	require.ErrorContains(t, err, "save setting "+keyB)

	_, err = db.ExecContext(t.Context(), "DROP TABLE prefs")
	require.NoError(t, err)

	called := false
	err = s.Update(t.Context(), keyA, func(string, bool) (string, error) {
		called = true

		return valueOne, nil
	})
	require.ErrorContains(t, err, "read setting "+keyA)
	assert.False(t, called, "fn never sees a value that could not be read")
}
