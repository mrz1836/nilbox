// Package claude observes Claude Code sessions through `claude agents --json`, which
// Claude Code documents as the supported way to read session state from outside it, and
// reads a session's last message from the transcript it keeps.
//
// This package is the anti-corruption layer for Claude Code: everything here that knows
// Claude Code's own shapes, its statuses, its waitingFor words, and its transcript
// records, turns them into the sessions package's State. Anything it does not recognize
// is read as the closest state rather than refused, and a session whose state cannot be
// read is reported StateUnknown rather than guessed.
package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Claude Code's words, as `claude agents --json` writes them.
const (
	statusBusy    = "busy"
	statusWaiting = "waiting"
	statusIdle    = "idle"

	// stateFailed is a background session's state when it failed.
	stateFailed = "failed"

	waitingPermission = "permission prompt"
	waitingSandbox    = "sandbox request"
)

// Entry is one session `claude agents --json` lists. Only the fields the app reads are
// kept.
type Entry struct {
	// PID is the session's process, or 0 for a background session that has none.
	PID int `json:"pid"`

	// SessionID is the session's ID: the one a launch passed with --session-id.
	SessionID string `json:"sessionId"`

	// Cwd is the folder the session runs in.
	Cwd string `json:"cwd"`

	// Kind is "interactive" or "background".
	Kind string `json:"kind"`

	// Status is busy, waiting, or idle, and WaitingFor what a waiting session waits for,
	// such as "permission prompt" or "input needed".
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`

	// State is a background session's state, such as "working" or "failed".
	State string `json:"state"`
}

// ParseAgents reads what `claude agents --json` printed, by session ID. An entry without
// a session ID is left out.
func ParseAgents(out []byte) (map[string]Entry, error) {
	var list []Entry
	if err := json.Unmarshal(bytes.TrimSpace(out), &list); err != nil {
		return nil, fmt.Errorf("read claude agents --json: %w", err)
	}

	byID := make(map[string]Entry, len(list))

	for _, e := range list {
		if e.SessionID != "" {
			byID[e.SessionID] = e
		}
	}

	return byID, nil
}

// MapState turns a listed session's status into a State, with a detail where the status
// says more. A waiting session waits for approval when a permission or sandbox prompt is
// open, and for the user's input otherwise; a status this build does not know reads as
// working, as Claude Code itself reads one.
func MapState(e Entry) (sessions.State, string) {
	if e.State == stateFailed {
		return sessions.StateFailed, ""
	}

	switch e.Status {
	case statusIdle:
		return sessions.StateIdle, ""
	case statusWaiting:
		detail := textutil.Capitalize(strings.TrimSpace(e.WaitingFor))

		switch e.WaitingFor {
		case waitingPermission, waitingSandbox, "":
			return sessions.StateNeedsApproval, detail
		default:
			return sessions.StateNeedsInput, detail
		}
	default:
		return sessions.StateWorking, ""
	}
}
