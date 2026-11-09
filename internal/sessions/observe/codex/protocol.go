// Package codex observes OpenAI Codex sessions through the local app-server daemon's
// JSON-RPC protocol, the one Codex's own IDE and desktop clients use, reached through
// `codex app-server proxy`.
//
// A Codex session started in a terminal runs its thread on the shared daemon unless it
// was started with command-line config overrides, which Codex runs in-process instead.
// The daemon reports a thread it hosts live, with the approval or input it waits for; of
// a thread it does not host it still serves what the thread recorded, which tells a turn
// in progress from one finished, and the last message. When the daemon cannot be reached
// at all, a session is linked through the first line of the rollout file Codex writes,
// and only whether its process runs is known.
//
// This package is the anti-corruption layer for Codex: everything here that knows its
// protocol's shapes turns them into the sessions package's State. The client is
// read-only by construction: it sends only the methods in Allowed, and never starts,
// resumes, steers, or answers a thread.
package codex

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Codex's words, as the app-server protocol writes them.
const (
	statusNotLoaded   = "notLoaded"
	statusIdle        = "idle"
	statusSystemError = "systemError"
	statusActive      = "active"

	flagApproval  = "waitingOnApproval"
	flagUserInput = "waitingOnUserInput"

	turnCompleted   = "completed"
	turnInterrupted = "interrupted"
	turnFailed      = "failed"
	turnInProgress  = "inProgress"

	itemAgentMessage = "agentMessage"
	phaseFinalAnswer = "final_answer"
)

// linkSlack is how much earlier than its launch a thread may say it was created and
// still be the launch's: Codex stamps whole seconds.
const linkSlack = 5 * time.Second

// previewRunes is how much of a thread's first message is compared with the prompt a
// launch sent.
const previewRunes = 80

// Thread is a Codex thread, as far as following a session needs it.
type Thread struct {
	ID string `json:"id"`

	// Cwd is the folder the thread works in.
	Cwd string `json:"cwd"`

	// Preview is usually the thread's first user message.
	Preview string `json:"preview"`

	// CreatedAt and UpdatedAt are Unix seconds.
	CreatedAt int64 `json:"createdAt"`
	UpdatedAt int64 `json:"updatedAt"`

	Status ThreadStatus `json:"status"`

	// ParentThreadID is set for a subagent's thread, and Ephemeral for one never kept.
	ParentThreadID *string `json:"parentThreadId"`
	Ephemeral      bool    `json:"ephemeral"`
}

// ThreadStatus is a thread's runtime status: notLoaded, idle, systemError, or active
// with the flags of what it waits for.
type ThreadStatus struct {
	Type        string   `json:"type"`
	ActiveFlags []string `json:"activeFlags"`
}

// Loaded reports a thread the daemon hosts, whose status is live.
func (s ThreadStatus) Loaded() bool {
	return s.Type != "" && s.Type != statusNotLoaded
}

// Turn is one turn of a thread: the user's message and the agent's work on it.
type Turn struct {
	ID     string `json:"id"`
	Status string `json:"status"`

	// CompletedAt is Unix seconds, or nil for a turn that has not completed.
	CompletedAt *int64 `json:"completedAt"`

	Error *struct {
		Message string `json:"message"`
	} `json:"error"`

	Items []TurnItem `json:"items"`
}

// TurnItem is one item of a turn; only the agent's messages are read.
type TurnItem struct {
	Type  string  `json:"type"`
	Text  string  `json:"text"`
	Phase *string `json:"phase"`
}

// UnknownStatus is the detail of a session whose thread Codex reports in a status this
// build does not know, which says nothing it can trust.
const UnknownStatus = "Codex reported a state this version of the app doesn't know"

// MapStatus turns a loaded thread's status into a State, with a detail where it says
// more, and StateUnknown for a status it does not know. It reports false for a thread the
// daemon does not host, whose status says nothing of it.
func MapStatus(s ThreadStatus) (sessions.State, string, bool) {
	switch s.Type {
	case statusIdle:
		return sessions.StateIdle, "", true
	case statusSystemError:
		return sessions.StateFailed, "Codex reported a system error", true
	case statusActive:
		switch {
		case slices.Contains(s.ActiveFlags, flagApproval):
			return sessions.StateNeedsApproval, "", true
		case slices.Contains(s.ActiveFlags, flagUserInput):
			return sessions.StateNeedsInput, "", true
		default:
			return sessions.StateWorking, "", true
		}
	case statusNotLoaded, "":
		return "", "", false
	default:
		return sessions.StateUnknown, UnknownStatus, true
	}
}

// TurnState says what a thread's latest turn shows of it: working while the turn has not
// completed, and idle once it has, with why when it did not finish, and StateUnknown for
// a status it does not know. A turn of a thread the daemon does not host reads as
// interrupted until it completes, so only its completion counts.
func TurnState(t Turn) (sessions.State, string) {
	switch {
	case t.CompletedAt == nil && (t.Status == turnInProgress || t.Status == turnInterrupted):
		return sessions.StateWorking, ""
	case t.Status == turnCompleted:
		return sessions.StateIdle, ""
	case t.Status == turnFailed:
		if t.Error != nil && strings.TrimSpace(t.Error.Message) != "" {
			return sessions.StateIdle, "The last turn failed: " + strings.TrimSpace(t.Error.Message)
		}

		return sessions.StateIdle, "The last turn failed"
	case t.Status == turnInterrupted:
		return sessions.StateIdle, "The last turn was interrupted"
	default:
		return sessions.StateUnknown, ""
	}
}

// LastAgentMessage returns the text of a turn's last message from the agent, its final
// answer when it gave one, or "".
func LastAgentMessage(t Turn) string {
	var last, final string

	for _, it := range t.Items {
		if it.Type != itemAgentMessage || strings.TrimSpace(it.Text) == "" {
			continue
		}

		last = strings.TrimSpace(it.Text)

		if it.Phase != nil && *it.Phase == phaseFinalAnswer {
			final = last
		}
	}

	if final != "" {
		return final
	}

	return last
}

// Link picks the thread a launch started among threads: one working in the launch's
// folder, created no earlier than the launch, and not a subagent's, preferring one whose
// first message is the prompt the launch sent, then the earliest. taken holds the threads
// already linked to another session.
func Link(threads []Thread, r sessions.Record, taken map[string]bool) (Thread, bool) {
	dir := samePath(r.Dir)
	from := r.LaunchedAt.Add(-linkSlack).Unix()

	var candidates []Thread

	for _, t := range threads {
		if taken[t.ID] || t.ParentThreadID != nil || t.Ephemeral || t.CreatedAt < from || samePath(t.Cwd) != dir {
			continue
		}

		candidates = append(candidates, t)
	}

	slices.SortStableFunc(candidates, func(a, b Thread) int { return cmp.Compare(a.CreatedAt, b.CreatedAt) })

	for _, t := range candidates {
		if samePrompt(t.Preview, r.PromptHead) {
			return t, true
		}
	}

	if len(candidates) == 0 {
		return Thread{}, false
	}

	return candidates[0], true
}

// samePrompt reports whether a thread's first message starts as the prompt a launch sent,
// whitespace aside.
func samePrompt(preview, head string) bool {
	a, b := squash(preview), squash(head)
	if a == "" || b == "" {
		return false
	}

	n := min(len([]rune(a)), len([]rune(b)), previewRunes)

	return textutil.Clip(a, n) == textutil.Clip(b, n)
}

// squash collapses every run of white space in s to one space.
func squash(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// samePath returns path cleaned, with symbolic links resolved where they can be, so the
// folder a launch named and the one Codex recorded compare equal.
func samePath(path string) string {
	if path == "" {
		return ""
	}

	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}

	return filepath.Clean(path)
}
