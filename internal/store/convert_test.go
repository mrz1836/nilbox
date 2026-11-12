package store_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
)

// errUnreadableRow is the error the test's converter refuses a row with.
var errUnreadableRow = errors.New("unreadable row")

// rowNumber converts a row of text to its number, refusing one that isn't.
func rowNumber(row string) (int, error) {
	n, err := strconv.Atoi(row)
	if err != nil {
		return 0, errUnreadableRow
	}

	return n, nil
}

func TestConvertRowsKeepsTheRowsOrder(t *testing.T) {
	t.Parallel()

	got, err := store.ConvertRows([]string{"3", "1", "2"}, rowNumber)
	require.NoError(t, err)
	assert.Equal(t, []int{3, 1, 2}, got)
}

func TestConvertRowsOfNoRowsIsAnEmptyList(t *testing.T) {
	t.Parallel()

	for _, rows := range [][]string{nil, {}} {
		got, err := store.ConvertRows(rows, rowNumber)
		require.NoError(t, err)
		assert.NotNil(t, got, "an empty list, not nil, so the window receives []")
		assert.Empty(t, got)
	}
}

func TestConvertRowsStopsAtTheFirstRowItCannotConvert(t *testing.T) {
	t.Parallel()

	var seen []string

	got, err := store.ConvertRows([]string{"1", "two", "3"}, func(row string) (int, error) {
		seen = append(seen, row)

		return rowNumber(row)
	})
	require.ErrorIs(t, err, errUnreadableRow)
	assert.Equal(t, errUnreadableRow, err, "the converter's error, as it is")
	assert.Nil(t, got)
	assert.Equal(t, []string{"1", "two"}, seen, "no row after it is read")
}

func TestDeref(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(7), store.Deref(new(int64(7))))
	assert.Equal(t, "head", store.Deref(new("head")))
	assert.Zero(t, store.Deref[int64](nil), "a NULL column reads as zero")
	assert.Empty(t, store.Deref[string](nil))
	assert.False(t, store.Deref[bool](nil))
}
