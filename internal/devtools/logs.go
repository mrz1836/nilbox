package devtools

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// LogEntry is one record of the app's log.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"msg"`
	Attrs   []Attr    `json:"attrs,omitempty"`
}

// Attr is one attribute of a log record, its key dotted with its groups' names.
type Attr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LogHandler is a slog.Handler that keeps every record from Debug up in a feed, whatever
// level the app's other handlers log at, so the inspector shows the debug records a run
// without --debug never writes to its log file.
type LogHandler struct {
	feed   *Feed[LogEntry]
	attrs  []Attr
	prefix string
}

// NewLogHandler returns a handler that keeps records in feed.
func NewLogHandler(feed *Feed[LogEntry]) *LogHandler {
	return &LogHandler{feed: feed}
}

// Enabled reports whether a record at level is kept: Debug and up are.
func (h *LogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug
}

// Handle keeps r in the feed, with the attributes the handler was given before its own.
func (h *LogHandler) Handle(_ context.Context, r slog.Record) error {
	entry := LogEntry{Time: r.Time, Level: r.Level.String(), Message: r.Message, Attrs: append([]Attr{}, h.attrs...)}

	r.Attrs(func(a slog.Attr) bool {
		entry.Attrs = appendAttr(entry.Attrs, h.prefix, a)

		return true
	})

	h.feed.Add(entry)

	return nil
}

// WithAttrs returns a handler that adds attrs to every record it keeps.
func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append([]Attr{}, h.attrs...)

	for _, a := range attrs {
		next.attrs = appendAttr(next.attrs, h.prefix, a)
	}

	return &next
}

// WithGroup returns a handler whose records' attributes are keyed under name.
func (h *LogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	next := *h
	next.prefix = h.prefix + name + "."

	return &next
}

// appendAttr appends a to attrs, its key under prefix, a group's attributes one by one
// under its name, and returns the result. An empty attribute is left out.
func appendAttr(attrs []Attr, prefix string, a slog.Attr) []Attr {
	a.Value = a.Value.Resolve()

	if a.Value.Kind() == slog.KindGroup {
		inner := prefix
		if a.Key != "" {
			inner += a.Key + "."
		}

		for _, member := range a.Value.Group() {
			attrs = appendAttr(attrs, inner, member)
		}

		return attrs
	}

	if a.Equal(slog.Attr{}) {
		return attrs
	}

	return append(attrs, Attr{Key: prefix + a.Key, Value: a.Value.String()})
}

// levelAtLeast reports whether level, as a LogEntry or a ConsoleEntry names it, is at or
// above floor, named the same way ("debug", "info", "warn", "error"); an empty floor lets
// every level through.
func levelAtLeast(level, floor string) bool {
	if floor == "" {
		return true
	}

	return levelRank(level) >= levelRank(floor)
}

// levelRank orders the levels' names, a console's "log" as info.
func levelRank(level string) int {
	switch strings.ToLower(level) {
	case "debug", "trace":
		return 0
	case "warn", "warning":
		return 2
	case "error":
		return 3
	default:
		return 1
	}
}
