package devtools_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
)

func TestLogHandlerKeepsEveryRecordFromDebugUp(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[devtools.LogEntry](10)
	handler := devtools.NewLogHandler(feed)
	logger := slog.New(handler)

	assert.False(t, handler.Enabled(t.Context(), slog.LevelDebug-1))
	assert.True(t, handler.Enabled(t.Context(), slog.LevelDebug))

	logger.With("app", "x").WithGroup("refresh").WithGroup("").LogAttrs(t.Context(), slog.LevelDebug, "read the inbox",
		slog.Int("items", 3), slog.Group("budget", "left", 4990), slog.Group("", "flat", true), slog.Attr{})
	logger.Warn("slow", slog.Group("", slog.Group("inner", "ms", 900)))

	entries := feed.Last(0)
	require.Len(t, entries, 2)

	assert.Equal(t, "DEBUG", entries[0].Level)
	assert.Equal(t, "read the inbox", entries[0].Message)
	assert.False(t, entries[0].Time.IsZero())
	assert.Equal(t, []devtools.Attr{
		{Key: "app", Value: "x"},
		{Key: "refresh.items", Value: "3"},
		{Key: "refresh.budget.left", Value: "4990"},
		{Key: "refresh.flat", Value: "true"},
	}, entries[0].Attrs, "a group's attributes are keyed under its name; an empty one's stay where they are")

	assert.Equal(t, "WARN", entries[1].Level)
	assert.Equal(t, []devtools.Attr{{Key: "inner.ms", Value: "900"}}, entries[1].Attrs)
}

func TestLogHandlerAttrsDoNotLeakBetweenLoggers(t *testing.T) {
	t.Parallel()

	feed := devtools.NewFeed[devtools.LogEntry](10)
	base := slog.New(devtools.NewLogHandler(feed)).With("a", 1)

	base.With("b", 2).Info("one")
	base.With("c", 3).Info("two")

	entries := feed.Last(0)
	require.Len(t, entries, 2)
	assert.Equal(t, []devtools.Attr{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}, entries[0].Attrs)
	assert.Equal(t, []devtools.Attr{{Key: "a", Value: "1"}, {Key: "c", Value: "3"}}, entries[1].Attrs)
}
