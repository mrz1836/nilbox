package testutil

import (
	"bytes"
	"log/slog"
	"sync"
)

// Logs collects a logger's records, or any other text written to it, in memory for a test
// to read. It is safe for concurrent use.
type Logs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Logger returns a logger that writes every record, Debug and up, into l.
func (l *Logs) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// Write appends p.
func (l *Logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

// String returns every record written so far.
func (l *Logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.String()
}
