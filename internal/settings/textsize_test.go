package settings

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTextSizeIsTheDefaultOnAFreshDatabase(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.TextSize(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TextSizeDefault, got)
	assert.Equal(t, TextSizeDefault, DefaultTextSize)

	all, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TextSizeDefault, all.TextSize)
}

func TestTextSizePersists(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	for _, size := range []TextSize{TextSizeLarge, TextSizeLarger, TextSizeDefault, TextSizeLarger} {
		out, err := svc.SetTextSize(ctx, size)
		require.NoError(t, err)
		assert.Equal(t, size, out.TextSize, "the setter returns the settings with it")

		got, err := svc.TextSize(ctx)
		require.NoError(t, err)
		assert.Equal(t, size, got)

		all, err := svc.Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, size, all.TextSize, "the size alone reads as the settings do")
	}
}

func TestTextSizeSurvivesARestart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "app.db")

	db := testutil.DBAt(t, path)
	require.NoError(t, testutil.ErrOf(serviceOver(t, db, home).SetTextSize(ctx, TextSizeLarge)))
	require.NoError(t, db.Close())

	got, err := serviceOver(t, testutil.DBAt(t, path), home).TextSize(ctx)
	require.NoError(t, err)
	assert.Equal(t, TextSizeLarge, got)
}

func TestSetTextSizeRejectsAnUnknownSize(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	require.NoError(t, testutil.ErrOf(svc.SetTextSize(ctx, TextSizeLarge)))

	for _, size := range []TextSize{"", "Large", " large", "largest", "small", "115", "ui.textSize"} {
		require.ErrorIs(t, testutil.ErrOf(svc.SetTextSize(ctx, size)), ErrInvalidTextSize, string(size))
	}

	_, err := svc.SetTextSize(ctx, "huge")
	require.ErrorContains(t, err, `"huge": choose one of default, large, larger`)

	got, err := svc.TextSize(ctx)
	require.NoError(t, err)
	assert.Equal(t, TextSizeLarge, got, "a rejected size must not be stored")
}

func TestTextSizeIgnoresABadStoredValue(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	for _, stored := range []string{"", "largest", "LARGE", "large ", "130"} {
		setPref(t, db, KeyTextSize, stored)

		got, err := svc.TextSize(t.Context())
		require.NoError(t, err)
		assert.Equal(t, DefaultTextSize, got, "%q falls back to the default", stored)

		all, err := svc.Get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, DefaultTextSize, all.TextSize, "%q falls back to the default", stored)
	}
}

func TestTextSizeReportsAClosedDatabase(t *testing.T) {
	t.Parallel()

	svc := serviceOver(t, closedDB(t), t.TempDir())

	got, err := svc.TextSize(t.Context())
	require.ErrorContains(t, err, "read the text size")
	assert.Equal(t, DefaultTextSize, got, "a size that cannot be read is the default")

	require.ErrorContains(t, testutil.ErrOf(svc.SetTextSize(t.Context(), TextSizeLarge)), "begin transaction")
}

func TestTextSizesAreTheChoicesInOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []TextSize{"default", "large", "larger"}, textSizes())

	for _, size := range textSizes() {
		assert.True(t, validTextSize(size), string(size))
		assert.Equal(t, size, decodeTextSize(string(size)))
	}

	assert.False(t, validTextSize("largest"))
}

func TestTextSizePercent(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 100, TextSizeDefault.Percent())
	assert.Equal(t, 115, TextSizeLarge.Percent())
	assert.Equal(t, 130, TextSizeLarger.Percent())
	assert.Equal(t, DefaultTextSize.Percent(), TextSize("huge").Percent(), "an unknown size counts as the default")
}

func TestSetTextSizeEmitsOnceStored(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))
	ctx := t.Context()

	require.NoError(t, testutil.ErrOf(svc.SetTextSize(ctx, TextSizeLarge)))
	require.ErrorIs(t, testutil.ErrOf(svc.SetTextSize(ctx, "huge")), ErrInvalidTextSize)
	require.NoError(t, testutil.ErrOf(svc.SetTextSize(ctx, TextSizeDefault)))

	assert.Equal(t, []event.Event{
		event.TextSizeChanged{Size: string(TextSizeLarge)},
		event.TextSizeChanged{Size: string(TextSizeDefault)},
	}, events.All(), "every stored choice, in order, and no refused one")
}

func TestSetTextSizeDoesNotEmitWhatItCouldNotStore(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, closedDB(t), t.TempDir(), WithEvents(events))

	require.Error(t, testutil.ErrOf(svc.SetTextSize(t.Context(), TextSizeLarge)))
	assert.Empty(t, events.All())
}
