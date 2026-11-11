package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// The states a hook's line in `go-pre-commit status` says, text or JSON.
const (
	// statusReady and statusOutdated are go-pre-commit's own hook, current or not.
	statusReady    = "ready"
	statusOutdated = "outdated"

	// statusOther is another tool's hook, and statusMissing none.
	statusOther   = "other"
	statusMissing = "missing"

	// statusNotExecutable is go-pre-commit's own hook without its executable bit, in
	// status --json.
	statusNotExecutable = "not_executable"
)

var (
	// errStatusOutput is a status whose output says nothing the reader knows.
	errStatusOutput = errors.New("go-pre-commit status said nothing about the hooks")

	// errStatusFailed is a status that said it failed.
	errStatusFailed = errors.New("go-pre-commit status failed")
)

// statusReport is what `go-pre-commit status` said of a clone's hooks.
type statusReport struct {
	// hooks maps each hook type it named to its state: statusReady, statusOutdated, or
	// statusOther; a type it didn't name has none.
	hooks map[string]string

	// disabled reports ENABLE_GO_PRE_COMMIT=false.
	disabled bool
}

var (
	// statusHookLine is one hook type's line: "✓   ✓ pre-commit: Go pre-commit hook
	// installed and ready", its message ending in " (warning: not executable)" for one
	// git won't run.
	statusHookLine = regexp.MustCompile(`^[✓⚠✗]\s+[✓⚠✗] ([a-z-]+): (.+)$`)

	// statusDisabled is the line saying the config turns go-pre-commit off.
	statusDisabled = regexp.MustCompile(`Pre-commit system is disabled`)
)

// readStatusText reads go-pre-commit 1.13's `status --no-color`, stdout and stderr
// together: one line per hook type that has a file, go-pre-commit's own or another
// tool's. A status that failed, such as one with no config, says why in an Error: line.
func readStatusText(out []byte) (statusReport, error) {
	rep := statusReport{hooks: map[string]string{}}
	known := false

	for line := range strings.Lines(string(out)) {
		line = textutil.TerminalLine(line)

		if m := statusHookLine.FindStringSubmatch(line); m != nil {
			rep.hooks[m[1]] = hookStatus(m[2])
			known = true

			continue
		}

		switch {
		case statusDisabled.MatchString(line):
			rep.disabled = true
		case strings.Contains(line, "Pre-commit system is enabled"), strings.Contains(line, "No hooks are currently installed"):
			known = true
		case strings.HasPrefix(line, "Error: "):
			return statusReport{}, fmt.Errorf("%w: %s", errStatusFailed, strings.TrimPrefix(line, "Error: "))
		}
	}

	if !known && !rep.disabled {
		return statusReport{}, errStatusOutput
	}

	return rep, nil
}

// hookStatus reads a hook line's message: go-pre-commit's own hook installed and ready
// (executable or not: the scan reads the bit itself), or outdated, or another hook.
func hookStatus(message string) string {
	switch {
	case strings.Contains(message, "installed but outdated"):
		return statusOutdated
	case strings.Contains(message, "installed and ready"):
		return statusReady
	case strings.Contains(message, "Different hook"):
		return statusOther
	default:
		return ""
	}
}

// statusJSON is the document `go-pre-commit status --json` prints (1.14.1 and later), or
// its error document.
type statusJSON struct {
	Enabled *bool `json:"enabled"`
	Hooks   []struct {
		Name     string `json:"name"`
		State    string `json:"state"`
		Outdated bool   `json:"outdated"`
	} `json:"hooks"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// readStatusJSON reads `status --json`'s document from stdout: each hook type with its
// state, ready, outdated, not_executable, other, missing, or error. not_executable is
// go-pre-commit's own hook, current unless its outdated flag says otherwise: the scan reads
// the executable bit itself.
func readStatusJSON(out []byte) (statusReport, error) {
	var doc statusJSON
	if err := json.Unmarshal(out, &doc); err != nil {
		return statusReport{}, fmt.Errorf("read go-pre-commit status --json: %w", err)
	}

	if doc.Error != nil {
		return statusReport{}, fmt.Errorf("%w: %s", errStatusFailed, doc.Error.Message)
	}

	if doc.Enabled == nil {
		return statusReport{}, errStatusOutput
	}

	rep := statusReport{hooks: map[string]string{}, disabled: !*doc.Enabled}

	for _, h := range doc.Hooks {
		switch h.State {
		case statusReady, statusOutdated, statusOther:
			rep.hooks[h.Name] = h.State
		case statusNotExecutable:
			rep.hooks[h.Name] = statusReady
			if h.Outdated {
				rep.hooks[h.Name] = statusOutdated
			}
		}
	}

	return rep, nil
}
