package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestWindowSizePersists(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	for _, size := range []WindowSize{
		{Width: 1680, Height: 1050},
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
	t.Parallel()

	got, err := ReadWindowSize(t.Context(), prefs.New(testutil.DB(t), nil))
	require.NoError(t, err)
	assert.Equal(t, DefaultWindowSize(), got)
}

func TestReadWindowSizeReportsAClosedDatabase(t *testing.T) {
	t.Parallel()

	got, err := ReadWindowSize(t.Context(), prefs.New(testutil.ClosedDB(t), nil))
	require.ErrorContains(t, err, "read the window size")
	assert.Equal(t, DefaultWindowSize(), got, "the default still comes back")
}

func TestSetWindowSizeRejectsOutOfBounds(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	assert.Equal(t, "1440x900", DefaultWindowSize().encode())
}

func TestAtLeast(t *testing.T) {
	t.Parallel()

	least := WindowSize{Width: 1040, Height: 676}

	assert.Equal(t, WindowSize{Width: 1440, Height: 900}, WindowSize{Width: 1440, Height: 900}.AtLeast(least))
	assert.Equal(t, least, WindowSize{Width: 800, Height: 520}.AtLeast(least))
	assert.Equal(t, WindowSize{Width: 1100, Height: 676}, WindowSize{Width: 1100, Height: 600}.AtLeast(least))
}

func TestMinWindowSizeGrowsWithTheTextSize(t *testing.T) {
	t.Parallel()

	assert.Equal(t, WindowSize{Width: MinWindowWidth, Height: MinWindowHeight}, MinWindowSizeFor(TextSizeDefault))
	assert.Equal(t, WindowSize{Width: 920, Height: 598}, MinWindowSizeFor(TextSizeLarge))
	assert.Equal(t, WindowSize{Width: 1040, Height: 676}, MinWindowSizeFor(TextSizeLarger))
	assert.Equal(t, MinWindowSizeFor(DefaultTextSize), MinWindowSizeFor("unknown"))

	for _, size := range textSizes().values {
		assert.True(t, MinWindowSizeFor(size).valid(), "the minimum at %s is a size the window can be", size)
	}
}

func TestEveryWindowPresetClearsTheLargestMinimum(t *testing.T) {
	t.Parallel()

	largest := MinWindowSizeFor(TextSizeLarger)

	// The window size presets the settings offer (frontend/src/lib/windowSize.ts), so
	// choosing one never shrinks a window below the minimum for any text size.
	for _, preset := range []WindowSize{{1100, 720}, {1280, 800}, {1440, 900}, {1680, 1050}} {
		assert.Equal(t, preset, preset.AtLeast(largest), "%v", preset)
	}
}

func TestWindowBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		stored        WindowSize
		textSize      TextSize
		width, height int
		size, least   WindowSize
	}{
		{
			name: "the default size at the default text size", stored: DefaultWindowSize(), textSize: TextSizeDefault,
			width: 2560, height: 1415,
			size: DefaultWindowSize(), least: WindowSize{Width: 800, Height: 520},
		},
		{
			name: "a small window grows to a larger text size's minimum", stored: WindowSize{Width: 800, Height: 520},
			textSize: TextSizeLarger, width: 2560, height: 1415,
			size: WindowSize{Width: 1040, Height: 676}, least: WindowSize{Width: 1040, Height: 676},
		},
		{
			name: "a window already large enough keeps its size", stored: WindowSize{Width: 1100, Height: 720},
			textSize: TextSizeLarge, width: 2560, height: 1415,
			size: WindowSize{Width: 1100, Height: 720}, least: WindowSize{Width: 920, Height: 598},
		},
		{
			name: "a screen smaller than the minimum caps both", stored: WindowSize{Width: 1440, Height: 900},
			textSize: TextSizeLarger, width: 1024, height: 640,
			size: WindowSize{Width: 1024, Height: 640}, least: WindowSize{Width: 1024, Height: 640},
		},
		{
			name: "never below the default minimum", stored: WindowSize{Width: 1440, Height: 900},
			textSize: TextSizeLarger, width: 640, height: 400,
			size: WindowSize{Width: 800, Height: 520}, least: WindowSize{Width: 800, Height: 520},
		},
		{
			name: "an unknown screen area fits anything", stored: WindowSize{Width: 800, Height: 520},
			textSize: TextSizeLarge, width: 0, height: -1,
			size: WindowSize{Width: 920, Height: 598}, least: WindowSize{Width: 920, Height: 598},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			size, least := WindowBounds(tt.stored, tt.textSize, tt.width, tt.height)
			assert.Equal(t, tt.size, size, "the size it opens at")
			assert.Equal(t, tt.least, least, "the smallest it can be")
		})
	}
}
