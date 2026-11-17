package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/sessions"
)

// usageTailBytes is how much of a rollout file's end is read for its last token count.
const usageTailBytes = 4 << 20

// Usage reads a Codex session's token counts from its rollout file, for
// sessions.Service.Usage: the thread's running total, which each token count Codex records
// there carries.
type Usage struct {
	// Home is the user's home directory, whose .codex is the default Codex home.
	Home string
}

// Usage returns the token counts of the session r's thread, from the last token count in
// the end of its rollout file. It returns sessions.ErrNoUsage when the file can't be found
// or its end holds no token count.
func (u Usage) Usage(_ context.Context, r sessions.Record) (sessions.Usage, error) {
	home := r.ConfigDir
	if home == "" {
		home = filepath.Join(u.Home, codexInfo().ConfigDir)
	}

	path := findRollout(filepath.Join(home, rolloutsDir), r.AgentSessionID)
	if path == "" {
		return sessions.Usage{}, sessions.ErrNoUsage
	}

	got, ok, err := lastTokenCount(path, usageTailBytes)

	switch {
	case err != nil:
		return sessions.Usage{}, err
	case !ok:
		return sessions.Usage{}, sessions.ErrNoUsage
	}

	return got, nil
}

// tokenCountLine is one line of a rollout file, as far as a token count needs it.
type tokenCountLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type string `json:"type"`
		Info *struct {
			Total *totalUsage `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// totalUsage is a thread's running total, as a token count names it. Codex counts the
// input read from the cache, and that written to it, within the input.
type totalUsage struct {
	Input      int64 `json:"input_tokens"`
	CacheRead  int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
}

// usage returns the total as the session panel counts it: the input not read from or
// written to the cache apart from what was, so the parts add up to Codex's own total.
func (t totalUsage) usage() (sessions.Usage, bool) {
	if t.Input < 0 || t.CacheRead < 0 || t.CacheWrite < 0 || t.Output < 0 {
		return sessions.Usage{}, false
	}

	fresh := max(t.Input-t.CacheRead-t.CacheWrite, 0)

	return sessions.Usage{Input: fresh, Output: t.Output, CacheRead: t.CacheRead, CacheWrite: t.CacheWrite}.Sum(), true
}

// lastTokenCount returns the last token count in the rollout file at path, read from its
// last n bytes, and whether they hold one. Only a regular file, or a link to one, is read
// (paths.ReadTail), so a pipe never blocks the read.
func lastTokenCount(path string, n int64) (sessions.Usage, bool, error) {
	data, cut, err := paths.ReadTail(path, n)
	if err != nil {
		return sessions.Usage{}, false, fmt.Errorf("read the rollout file: %w", err)
	}

	lines := bytes.Split(data, []byte("\n"))
	if cut && len(lines) > 0 {
		lines = lines[1:]
	}

	for _, line := range slices.Backward(lines) {
		if got, ok := tokenCount(line); ok {
			return got, true, nil
		}
	}

	return sessions.Usage{}, false, nil
}

// tokenCount returns the running total a rollout line's token count carries, and whether
// it is one that carries it.
func tokenCount(line []byte) (sessions.Usage, bool) {
	if !bytes.Contains(line, []byte(`"token_count"`)) {
		return sessions.Usage{}, false
	}

	var l tokenCountLine
	if json.Unmarshal(bytes.TrimSpace(line), &l) != nil || l.Type != "event_msg" || l.Payload.Type != "token_count" ||
		l.Payload.Info == nil || l.Payload.Info.Total == nil {
		return sessions.Usage{}, false
	}

	return l.Payload.Info.Total.usage()
}
