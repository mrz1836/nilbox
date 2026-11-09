package claude

import (
	"context"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/sessions"
)

// Words reads a Claude Code session's last message in full from its transcript, for
// sessions.Service.LastWords.
type Words struct {
	// Home is the user's home directory, whose .claude is the default config directory.
	Home string
}

// LastWords returns the last message the session r wrote, read from the end of its
// transcript under its config directory. It returns sessions.ErrNoWords when the
// transcript can't be found or holds no message of the session's own.
func (w Words) LastWords(_ context.Context, r sessions.Record) (string, error) {
	dir := r.ConfigDir
	if dir == "" {
		dir = filepath.Join(w.Home, agentInfo().ConfigDir)
	}

	path := findTranscript(dir, r.AgentSessionID)
	if path == "" {
		return "", sessions.ErrNoWords
	}

	text, err := lastMessageIn(path, wordsTailBytes)
	if err != nil {
		return "", err
	}

	if text == "" {
		return "", sessions.ErrNoWords
	}

	return text, nil
}
