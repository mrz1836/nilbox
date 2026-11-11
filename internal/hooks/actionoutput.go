package hooks

import (
	"regexp"
	"slices"
	"strings"
)

// actionOutput is what go-pre-commit's install or uninstall said, as an actionReader read
// it. What it did is read from the hook files, which say it better than its lines.
type actionOutput struct {
	notes

	// errors are its error lines, without their Error: or ✗.
	errors []string
}

// lastError returns the last error line it printed, or "".
func (o actionOutput) lastError() string {
	if len(o.errors) == 0 {
		return ""
	}

	return o.errors[len(o.errors)-1]
}

// The lines install and uninstall print about what they did, which the hook files tell:
// the hooks installed or removed, those not found, another hook in the way, and the
// hints install prints after it.
var (
	actDone     = regexp.MustCompile(`^✓ Successfully (?:un)?installed hooks: \[.*\]$`)
	actNotFound = regexp.MustCompile(`^ℹ Hooks not found or not managed by Go pre-commit: \[.*\]$`)
	actExists   = regexp.MustCompile(`^⚠ Hook already exists: \S+ \(use --force to overwrite\)$`)
	actNone     = regexp.MustCompile(`^⚠ No hooks were installed$`)
	actTryHint  = regexp.MustCompile(`^ℹ Run 'git commit' to test the pre-commit hook$`)
	actRunHint  = regexp.MustCompile(`^ℹ Run 'go-pre-commit run --help' to see available checks$`)
)

// actionKnown returns those lines' patterns.
func actionKnown() []*regexp.Regexp {
	return []*regexp.Regexp{actDone, actNotFound, actExists, actNone, actTryHint, actRunHint}
}

// actionPrefix is how a line that isn't about the hooks starts: an error, or a warning.
type actionPrefix struct {
	prefix string
	err    bool
}

// actionPrefixes are the starts of go-pre-commit's errors and warnings: its last word on a
// failure, as 1.14's "Error: hooks not installed: …", and a backup it couldn't restore.
func actionPrefixes() []actionPrefix {
	return []actionPrefix{{"Error: ", true}, {"✗ ", true}, {"⚠ ", false}, {"Warning: ", false}}
}

// actionReader reads go-pre-commit install's and uninstall's output as it arrives (see
// lineReader). It knows 1.13.1's lines, recorded in testdata/go-pre-commit-1.13.1.
type actionReader struct {
	lineReader

	out actionOutput
}

// newActionReader returns a reader that has read nothing.
func newActionReader() *actionReader {
	r := &actionReader{}
	r.read = r.readLine

	return r
}

// snapshot returns what the reader has read so far, sharing nothing with it.
func (r *actionReader) snapshot() actionOutput {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := r.out
	out.errors = slices.Clone(out.errors)
	out.warnings = slices.Clone(out.warnings)
	out.unrecognized = slices.Clone(out.unrecognized)

	return out
}

// readLine reads one line.
func (r *actionReader) readLine(line string) {
	if strings.TrimSpace(line) == "" || slices.ContainsFunc(actionKnown(), func(re *regexp.Regexp) bool {
		return re.MatchString(line)
	}) {
		return
	}

	for _, p := range actionPrefixes() {
		rest, ok := strings.CutPrefix(line, p.prefix)

		switch {
		case !ok:
			continue
		case p.err:
			r.out.errors = keepNote(r.out.errors, rest)
		default:
			r.out.warn(rest)
		}

		return
	}

	r.out.unknown(line)
}
