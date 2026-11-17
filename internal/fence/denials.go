package fence

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The fence hook's own log of what it refused, beside the policy in the run's folder: an
// agent that keeps a refused call out of its stream (Codex's --json shows none) leaves the
// run no other trace of it, so the runner reads the log as the run ends.
const (
	// DenialsFile is the log, one JSON Denial a line.
	DenialsFile = "denials.jsonl"

	// maxDenialsBytes caps the log: past it, no more refusals are written, though each is
	// still refused.
	maxDenialsBytes = 1 << 20

	// maxDenialRunes caps what a refusal keeps of the call and of its reason.
	maxDenialRunes = 300

	// KeepDenials is how many of the last refusals ReadDenials returns.
	KeepDenials = 20
)

// Denial is one call the fence hook refused: its tool, what it would have run or written,
// and why it was refused.
type Denial struct {
	Tool   string `json:"tool"`
	What   string `json:"what"`
	Reason string `json:"reason"`
}

// LogDenial appends a refusal of the call c to the log in the run's folder runDir, a
// folder the app made: it never makes one, never writes through a link, and stops once the
// log is full. A refusal it can't write is still refused; the log is best effort.
func LogDenial(runDir string, c Call, reason string) {
	if !filepath.IsAbs(runDir) {
		return
	}

	path := filepath.Join(runDir, DenialsFile)

	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Size() >= maxDenialsBytes) {
		return
	}

	what := c.Command
	if what == "" {
		what = c.Path
	}

	line, err := json.Marshal(Denial{
		Tool: c.Tool, What: textutil.Clip(what, maxDenialRunes), Reason: textutil.Clip(reason, maxDenialRunes),
	})
	if err != nil {
		return
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600) //nolint:gosec // the run's own folder
	if err != nil {
		return
	}

	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
}

// ReadDenials reads the log in the run's folder: how many calls the hook refused, and the
// last KeepDenials of them, oldest first. A folder with no log refused none; a line it
// can't read still counts.
func ReadDenials(runDir string) (int, []Denial) {
	last := []Denial{}

	if !filepath.IsAbs(runDir) {
		return 0, last
	}

	data, err := paths.ReadHead(filepath.Join(runDir, DenialsFile), maxDenialsBytes+maxDenialRunes*8)
	if err != nil {
		return 0, last
	}

	count := 0
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 0, 4096), maxDenialsBytes)

	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 {
			continue
		}

		count++

		var d Denial
		if json.Unmarshal(line, &d) != nil {
			continue
		}

		last = append(last, d)
		if len(last) > KeepDenials {
			last = last[1:]
		}
	}

	return count, last
}
