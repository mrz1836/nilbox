package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	// rolloutsDir is the folder in a Codex home that holds its rollout files, one per
	// thread, under YYYY/MM/DD folders of the local day each started.
	rolloutsDir = "sessions"

	// rolloutPrefix starts a rollout file's name, followed by the local time its thread
	// started, in nameStamp's layout.
	rolloutPrefix = "rollout-"
	nameStamp     = "2006-01-02T15-04-05"

	// maxMetaBytes caps the first line of a rollout file, which describes its thread.
	maxMetaBytes = 1 << 20

	// maxDays caps the local days' folders looked in for one launch's thread.
	maxDays = 8

	// sessionMeta is the type of a rollout file's first line.
	sessionMeta = "session_meta"
)

// rolloutMeta is the first line of a rollout file.
type rolloutMeta struct {
	Type    string `json:"type"`
	Payload struct {
		ID  string `json:"id"`
		Cwd string `json:"cwd"`

		// Timestamp is when the thread started, as RFC 3339.
		Timestamp string `json:"timestamp"`

		// Source is where the thread came from: a plain kind such as "cli", or an object
		// for a subagent's thread, which names its parent.
		Source json.RawMessage `json:"source"`
	} `json:"payload"`
}

// rollouts reads the threads of one Codex home's rollout files. It is how sessions are
// linked when the daemon cannot be reached. It keeps what each file's first line said, by
// path: a whole line never changes, so each poll reads only the files new to it.
type rollouts struct {
	home  string
	metas map[string]rolloutEntry
}

// rolloutEntry is what a rollout file's first line said: its thread, or ok false for a
// line that names no thread a launch could have started.
type rolloutEntry struct {
	thread Thread
	ok     bool
}

// newRollouts returns the rollouts of a Codex home.
func newRollouts(codexHome string) *rollouts {
	return &rollouts{home: codexHome, metas: map[string]rolloutEntry{}}
}

// threads lists the threads of the rollout files written since any of starts, looking for
// each start's in the folders of the local days from its own to now's.
func (rs *rollouts) threads(starts []time.Time, now time.Time) []Thread {
	from := map[string]time.Time{}

	for _, start := range starts {
		for _, dir := range dayDirs(filepath.Join(rs.home, rolloutsDir), start, now) {
			if at, ok := from[dir]; !ok || start.Before(at) {
				from[dir] = start
			}
		}
	}

	seen := make(map[string]rolloutEntry, len(rs.metas))
	out := make([]Thread, 0, len(rs.metas))

	for _, dir := range sortedKeys(from) {
		out = append(out, rs.in(dir, from[dir], seen)...)
	}

	rs.metas = seen

	return out
}

// in lists the threads of the rollout files in one day's folder written since from,
// keeping each whole first line it has in seen.
func (rs *rollouts) in(dir string, from time.Time, seen map[string]rolloutEntry) []Thread {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var out []Thread

	for _, e := range entries {
		if !isRollout(e) {
			continue
		}

		info, err := e.Info()
		if err != nil || info.ModTime().Before(from) {
			continue
		}

		if t, ok := rs.thread(filepath.Join(dir, e.Name()), seen); ok {
			t.UpdatedAt = info.ModTime().Unix()
			out = append(out, t)
		}
	}

	return out
}

// thread returns the thread of the rollout file at path, from what its first line said
// when it was read before, or else reading it, and keeps a whole line's in seen.
func (rs *rollouts) thread(path string, seen map[string]rolloutEntry) (Thread, bool) {
	entry, known := rs.metas[path]
	if !known {
		if entry, known = readMeta(path); !known {
			return Thread{}, false // a first line still being written, read again next time
		}
	}

	seen[path] = entry

	return entry.thread, entry.ok
}

// isRollout reports whether a folder entry is a rollout file.
func isRollout(e os.DirEntry) bool {
	return e.Type().IsRegular() && strings.HasPrefix(e.Name(), rolloutPrefix) && filepath.Ext(e.Name()) == ".jsonl"
}

// dayDirs returns the folders of the local days from from's to now's, at most maxDays of
// them, oldest first. Codex names them by the day on the user's own clock.
func dayDirs(root string, from, now time.Time) []string {
	from, now = from.Local(), now.Local() //nolint:gosmopolitan // Codex names its folders by the local day

	var out []string

	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local) //nolint:gosmopolitan // as above
	for i := 0; i < maxDays && !day.After(now); i++ {
		out = append(out, filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02")))
		day = day.AddDate(0, 0, 1)
	}

	return slices.Compact(out)
}

// readMeta reads a rollout file's first line, and returns what it said, and false when
// the line is not whole yet, so it may still change. It never waits on a file that is not
// a regular one, such as a FIFO put where a rollout was listed.
func readMeta(path string) (rolloutEntry, bool) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // a rollout file under the Codex home
	if err != nil {
		return rolloutEntry{}, false
	}

	defer func() { _ = f.Close() }()

	if info, statErr := f.Stat(); statErr != nil || !info.Mode().IsRegular() {
		return rolloutEntry{}, false
	}

	line, whole := firstLine(f)
	if line == nil {
		return rolloutEntry{}, whole
	}

	var meta rolloutMeta
	if json.Unmarshal(line, &meta) != nil {
		return rolloutEntry{}, whole
	}

	t, ok := meta.thread(filepath.Base(path))

	return rolloutEntry{thread: t, ok: ok}, true
}

// firstLine reads r's first line, up to maxMetaBytes, and reports whether it is whole: it
// ended, or will never fit. It returns nil for a line that cannot be read or is too long.
func firstLine(r io.Reader) ([]byte, bool) {
	line, err := bufio.NewReader(io.LimitReader(r, maxMetaBytes+1)).ReadBytes('\n')

	switch {
	case len(line) > maxMetaBytes:
		return nil, true
	case err == nil:
		return line, true
	case errors.Is(err, io.EOF):
		return line, false
	default:
		return nil, false
	}
}

// thread returns the thread a rollout file's first line describes, started when its
// payload says, or else when the file's name says, and false for a line that names no
// thread, or a thread no launch started: a subagent's, whose source is an object.
func (m rolloutMeta) thread(name string) (Thread, bool) {
	if m.Type != sessionMeta || m.Payload.ID == "" || bytes.HasPrefix(bytes.TrimSpace(m.Payload.Source), []byte("{")) {
		return Thread{}, false
	}

	started, ok := startedAt(m.Payload.Timestamp, name)
	if !ok {
		return Thread{}, false
	}

	return Thread{ID: m.Payload.ID, Cwd: m.Payload.Cwd, CreatedAt: started.Unix()}, true
}

// startedAt returns when a rollout's thread started: the time its first line records, or
// else the local time its file's name starts with.
func startedAt(stamp, name string) (time.Time, bool) {
	if at, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
		return at, true
	}

	rest, ok := strings.CutPrefix(name, rolloutPrefix)
	if !ok || len(rest) < len(nameStamp) {
		return time.Time{}, false
	}

	at, err := time.ParseInLocation(nameStamp, rest[:len(nameStamp)], time.Local) //nolint:gosmopolitan // Codex names its files by the local time

	return at, err == nil
}
