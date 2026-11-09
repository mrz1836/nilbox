package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// rolloutsDir is the folder in a Codex home that holds its rollout files, one per
	// thread, under YYYY/MM/DD folders of the local day each started.
	rolloutsDir = "sessions"

	// maxMetaBytes caps the first line of a rollout file, which describes its thread.
	maxMetaBytes = 1 << 20

	// sessionMeta is the type of a rollout file's first line.
	sessionMeta = "session_meta"
)

// rolloutMeta is the first line of a rollout file.
type rolloutMeta struct {
	Type    string `json:"type"`
	Payload struct {
		ID  string `json:"id"`
		Cwd string `json:"cwd"`
	} `json:"payload"`
}

// RolloutThreads lists the threads whose rollout files a Codex home holds that were
// written since from, by the first line of each: its ID and folder. It reads the
// folders of every local day from from's to now's. It is how sessions are linked when
// the daemon cannot be reached.
func RolloutThreads(codexHome string, from, now time.Time) []Thread {
	dirs := dayDirs(filepath.Join(codexHome, rolloutsDir), from, now)
	out := make([]Thread, 0, len(dirs))

	for _, dir := range dirs {
		out = append(out, rolloutsIn(dir, from)...)
	}

	return out
}

// rolloutsIn lists the threads of the rollout files in one day's folder written since
// from.
func rolloutsIn(dir string, from time.Time) []Thread {
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

		if t, ok := readMeta(filepath.Join(dir, e.Name())); ok {
			t.CreatedAt, t.UpdatedAt = info.ModTime().Unix(), info.ModTime().Unix()
			out = append(out, t)
		}
	}

	return out
}

// isRollout reports whether a folder entry is a rollout file.
func isRollout(e os.DirEntry) bool {
	return e.Type().IsRegular() && strings.HasPrefix(e.Name(), "rollout-") && filepath.Ext(e.Name()) == ".jsonl"
}

// dayDirs returns the folders of the local days from from's to now's, at most a week of
// them, oldest first. Codex names them by the day on the user's own clock.
func dayDirs(root string, from, now time.Time) []string {
	from, now = from.Local(), now.Local() //nolint:gosmopolitan // Codex names its folders by the local day

	var out []string

	day := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.Local) //nolint:gosmopolitan // as above
	for i := 0; i < 8 && !day.After(now); i++ {
		out = append(out, filepath.Join(root, day.Format("2006"), day.Format("01"), day.Format("02")))
		day = day.AddDate(0, 0, 1)
	}

	return slices.Compact(out)
}

// readMeta reads a rollout file's first line, and returns its thread.
func readMeta(path string) (Thread, bool) {
	f, err := os.Open(path) //nolint:gosec // a rollout file under the Codex home
	if err != nil {
		return Thread{}, false
	}

	defer func() { _ = f.Close() }()

	line, err := bufio.NewReaderSize(io.LimitReader(f, maxMetaBytes), 64<<10).ReadSlice('\n')
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return Thread{}, false
	}

	if errors.Is(err, bufio.ErrBufferFull) {
		line, err = readLine(path)
		if err != nil {
			return Thread{}, false
		}
	}

	var meta rolloutMeta
	if json.Unmarshal(line, &meta) != nil || meta.Type != sessionMeta || meta.Payload.ID == "" {
		return Thread{}, false
	}

	return Thread{ID: meta.Payload.ID, Cwd: meta.Payload.Cwd}, true
}

// readLine reads a first line longer than the usual buffer, up to maxMetaBytes.
func readLine(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // a rollout file under the Codex home
	if err != nil {
		return nil, err //nolint:wrapcheck // only whether it can be read matters
	}

	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(io.LimitReader(f, maxMetaBytes))
	scanner.Buffer(make([]byte, 0, 64<<10), maxMetaBytes)

	if !scanner.Scan() {
		return nil, io.ErrUnexpectedEOF
	}

	return scanner.Bytes(), nil
}
