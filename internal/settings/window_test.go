package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestWindowSizePersists(t *testing.T) {
	svc, db, _ := newTestService(t)

	for _, size := range []WindowSize{
		{Width: 1440, Height: 900},
		{Width: MinWindowWidth, Height: MinWindowHeight},
		{Width: MaxWindowWidth, Height: MaxWindowHeight},
	} {
		require.NoError(t, testutil.ErrOf(svc.SetWindowSize(t.Context(), size)))

		got, err := svc.Get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, size, got.WindowSize)
		assert.Equal(t, DefaultWindowSize(), got.DefaultWindowSize, "the default never changes")

		alone, err := ReadWindowSize(t.Context(), prefs.New(db, nil))
		require.NoError(t, err)
		assert.Equal(t, size, alone, "the size alone reads the same")
	}
}

func TestReadWindowSizeIsTheDefaultOnAFreshDatabase(t *testing.T) {
	got, err := ReadWindowSize(t.Context(), prefs.New(testutil.DB(t), nil))
	require.NoError(t, err)
	assert.Equal(t, DefaultWindowSize(), got)
}

func TestReadWindowSizeReportsAClosedDatabase(t *testing.T) {
	got, err := ReadWindowSize(t.Context(), prefs.New(closedDB(t), nil))
	require.ErrorContains(t, err, "read the window size")
	assert.Equal(t, DefaultWindowSize(), got, "the default still comes back")
}

func TestSetWindowSizeRejectsOutOfBounds(t *testing.T) {
	svc, _, _ := newTestService(t)

	for _, size := range []WindowSize{
		{},
		{Width: MinWindowWidth - 1, Height: 800},
		{Width: 1280, Height: MinWindowHeight - 1},
		{Width: MaxWindowWidth + 1, Height: 800},
		{Width: 1280, Height: MaxWindowHeight + 1},
		{Width: -1280, Height: -800},
	} {
		_, err := svc.SetWindowSize(t.Context(), size)
		require.ErrorIs(t, err, ErrInvalidWindowSize, size)
	}

	_, err := svc.SetWindowSize(t.Context(), WindowSize{Width: 640, Height: 480})
	require.ErrorContains(t, err, "640 x 480; choose 800 x 520 up to 7680 x 4320")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, DefaultWindowSize(), got.WindowSize, "rejected sizes must not be stored")
}

func TestGetFallsBackOnABadStoredWindowSize(t *testing.T) {
	svc, db, _ := newTestService(t)

	for _, stored := range []string{"", "1280", "1280x", "x800", "widexhigh", "1280x800x2", "100x100", "99999x800"} {
		setPref(t, db, KeyWindowSize, stored)

		got, err := svc.Get(t.Context())
		require.NoError(t, err)
		assert.Equalf(t, DefaultWindowSize(), got.WindowSize, "stored %q", stored)

		alone, err := ReadWindowSize(t.Context(), prefs.New(db, nil))
		require.NoError(t, err)
		assert.Equalf(t, DefaultWindowSize(), alone, "stored %q, read alone", stored)
	}
}

func TestWindowSizeFit(t *testing.T) {
	t.Parallel()

	large := WindowSize{Width: 1680, Height: 1050}

	tests := []struct {
		name          string
		width, height int
		want          WindowSize
	}{
		{name: "fits as it is", width: 2560, height: 1415, want: large},
		{name: "exactly fits", width: 1680, height: 1050, want: large},
		{name: "too wide and too tall", width: 1440, height: 875, want: WindowSize{Width: 1440, Height: 875}},
		{name: "too tall only", width: 1920, height: 1000, want: WindowSize{Width: 1680, Height: 1000}},
		{name: "never below the minimum", width: 640, height: 400, want: WindowSize{Width: MinWindowWidth, Height: MinWindowHeight}},
		{name: "an unknown area changes nothing", width: 0, height: -1, want: large},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, large.Fit(tt.width, tt.height))
		})
	}
}

func TestDefaultWindowSizeIsInBounds(t *testing.T) {
	t.Parallel()

	assert.True(t, DefaultWindowSize().valid())
	assert.Equal(t, "1280x800", DefaultWindowSize().encode())
}
