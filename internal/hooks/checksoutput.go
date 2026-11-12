package hooks

import (
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// CheckState is how one check came out of a checks run.
type CheckState string

// The states of a check.
const (
	// CheckPassed and CheckFailed are a check that ran.
	CheckPassed CheckState = "passed"
	CheckFailed CheckState = "failed"

	// CheckSkipped is a check go-pre-commit couldn't run and skipped.
	CheckSkipped CheckState = "skipped"

	// CheckOff is a check the repository turns off.
	CheckOff CheckState = "off"

	// CheckNotRun is a check that is on but this run didn't run: another was asked for,
	// there were no files, or the run stopped first.
	CheckNotRun CheckState = "notRun"
)

// CheckResult is one check in a checks run's result.
type CheckResult struct {
	Name  Check      `json:"name"`
	State CheckState `json:"state"`

	// DurationMs is how long it took, as go-pre-commit said.
	DurationMs int64 `json:"durationMs"`

	// Error and Suggestion are what go-pre-commit said went wrong and how to fix it, and
	// Detail the lines it showed from the check's own output, for a check that failed or
	// was skipped.
	Error      string   `json:"error"`
	Suggestion string   `json:"suggestion"`
	Detail     []string `json:"detail"`

	// ChangedFiles are the files go-pre-commit said the check changed (run --json only);
	// [] when it doesn't say.
	ChangedFiles []string `json:"changedFiles"`
}

// totals is a checks run's summary line, or its JSON totals.
type totals struct {
	known                   bool
	passed, failed, skipped int
	files                   int
	durationMs              int64
}

// checksOutput is what a checks run's output said, as a checksReader read it.
type checksOutput struct {
	scriptrun.Notes

	// checks are the checks it named, in the order it first named them.
	checks []CheckResult

	totals totals

	// noFiles reports "No files to check", and disabled ENABLE_GO_PRE_COMMIT=false.
	noFiles  bool
	disabled bool

	// installs are the tools it said it installs, as in "gofumpt@v0.12.0".
	installs []string

	// errors are its own error lines, apart from the one that counts failed checks.
	errors []string

	// fromJSON reports that the run's JSON report was read.
	fromJSON bool
}

// failedChecks returns the names of the checks that failed.
func (o checksOutput) failedChecks() []Check {
	var out []Check

	for _, c := range o.checks {
		if c.State == CheckFailed {
			out = append(out, c.Name)
		}
	}

	return out
}

// The sections of a checks run's text output.
type section int

const (
	sectionRun section = iota
	sectionResults
	sectionSummary
	sectionErrors
)

var (
	// The lines of the run itself: a check starting, passing, or failing, and a tool
	// installed for one.
	chkRunning    = regexp.MustCompile(`^⏳ Running (\S+) check\.\.\.$`)
	chkPassed     = regexp.MustCompile(`^✓ (\S+) check passed \((\S+)\)$`)
	chkFailed     = regexp.MustCompile(`^✗ (\S+) check failed \((\S+)\)$`)
	chkInstalling = regexp.MustCompile(`^→ Installing (\S+?)\.\.\.$`)
	chkInstalled  = regexp.MustCompile(`^✓ Successfully installed \S+$`)

	// The lines of the Check Results block: a check that passed, failed, or was skipped,
	// its error, and its suggestion. A suggestion in the errors block starts "To fix:".
	chkCompleted  = regexp.MustCompile(`^✓ (\S+) completed successfully \((\S+)\)$`)
	chkFailedLine = regexp.MustCompile(`^✗ (\S+) failed \((\S+)\)$`)
	chkSkipped    = regexp.MustCompile(`^⚠ (\S+) - (.+)$`)
	chkError      = regexp.MustCompile(`^  Error: (.*)$`)
	chkSuggestion = regexp.MustCompile(`^💡 (?:To fix: )?(.*)$`)

	// chkTotals is the summary line, with or without "All checks passed!": "5 passed, 1
	// failed on 6 file(s) in 357ms", "1 failed on 6 file(s) in 6ms".
	chkTotals = regexp.MustCompile(`^[✓✗⚠] (?:All checks passed! )?(\d+ (?:passed|failed|skipped)` +
		`(?:, \d+ (?:passed|failed|skipped))*)(?: on (\d+) file\(s\))? in (\S+)$`)

	// The lines of the errors block: its count, and each failed check's header.
	chkErrorsCount = regexp.MustCompile(`^✗ \d+ check\(s\) failed with the following errors:$`)
	chkErrorHeader = regexp.MustCompile(`^✗ ━ (\S+) ━$`)

	// chkFailedCount is the last line of a run with failed checks, on stderr.
	chkFailedCount = regexp.MustCompile(`^Error: checks failed: \d+$`)

	// chkRule underlines a block's title.
	chkRule = regexp.MustCompile(`^─+$`)
)

// checksPlain are lines of a checks run that say nothing the result needs.
func checksPlain() []string {
	return []string{
		"Check Results", "Summary:", "ERRORS FOUND", "ℹ Run with --verbose flag to see full error details",
		"ℹ Fix the errors above and run the checks again",
	}
}

const (
	// noFilesLine is a run with nothing to check, and disabledLine one the config turns off.
	noFilesLine  = "ℹ No files to check"
	disabledLine = "⚠ Pre-commit system is disabled in configuration (ENABLE_GO_PRE_COMMIT=false)"

	// maxReport caps the JSON report a run with --json prints, which holds every failed
	// check's whole output.
	maxReport = 8 << 20

	// maxDetail caps the lines a check's detail keeps.
	maxDetail = 40
)

// checksReader reads go-pre-commit run's output as it arrives (see scriptrun.LineWriter).
// It knows 1.13.1's lines, recorded in testdata/go-pre-commit-1.13.1, and, for a run with
// --json (1.14.1 and later), the JSON report it prints last on stdout, which then gives
// the checks and the totals.
//
// The app's log holds stdout and stderr in the order the run wrote them, so a failed
// check's error lines follow its "✗ <check> failed" line. The recordings, taken through two
// pipes, hold them out of that order: an error or a detail the reader can't place goes to
// the one failed check that has none, or else to the lines it didn't recognize.
type checksReader struct {
	*scriptrun.LineWriter

	out checksOutput

	// json reports a run with --json; inReport and report hold the report's lines while
	// it is read.
	json     bool
	inReport bool
	report   []string
	size     int

	// sect is the block being read, and current the check whose error and detail lines
	// follow, or -1; currentHasDetail reports a check whose detail the Check Results
	// block gave, which the errors block then repeats.
	sect             section
	current          int
	currentHasDetail bool

	// loose are errors, suggestions, and detail lines that came with no check to place
	// them under.
	looseErrors      []string
	looseSuggestions []string
	looseDetail      []string
}

// newChecksReader returns a reader that has read nothing; withJSON reads a run with
// --json.
func newChecksReader(withJSON bool) *checksReader {
	r := &checksReader{json: withJSON, current: -1}
	r.LineWriter = scriptrun.NewLineWriter(r.readLine)

	return r
}

// readLine reads one line.
func (r *checksReader) readLine(line string) {
	if r.json && r.readReport(line) {
		return
	}

	if strings.TrimSpace(line) == "" || chkRule.MatchString(line) || slices.Contains(checksPlain(), line) {
		r.plain(line)

		return
	}

	for _, read := range []func(string) bool{r.readRun, r.readResults, r.readSummary, r.readOwn} {
		if read(line) {
			return
		}
	}

	r.readDetail(line)
}

// plain notes the block a title line starts.
func (r *checksReader) plain(line string) {
	switch line {
	case "Check Results":
		r.sect, r.current = sectionResults, -1
	case "Summary:":
		r.sect, r.current = sectionSummary, -1
	case "ERRORS FOUND":
		r.sect, r.current = sectionErrors, -1
	case "ℹ Run with --verbose flag to see full error details", "ℹ Fix the errors above and run the checks again":
		r.current = -1
	}
}

// readRun reads a line of the run itself.
func (r *checksReader) readRun(line string) bool {
	if m := chkRunning.FindStringSubmatch(line); m != nil {
		r.check(m[1])

		return true
	}

	if m := chkPassed.FindStringSubmatch(line); m != nil {
		r.settle(r.check(m[1]), CheckPassed, m[2])

		return true
	}

	if m := chkFailed.FindStringSubmatch(line); m != nil {
		r.settle(r.check(m[1]), CheckFailed, m[2])

		return true
	}

	if m := chkInstalling.FindStringSubmatch(line); m != nil {
		r.out.installs = scriptrun.KeepNote(r.out.installs, m[1])

		return true
	}

	return chkInstalled.MatchString(line)
}

// readResults reads a line of the Check Results block, or of the errors block.
func (r *checksReader) readResults(line string) bool {
	if m := chkCompleted.FindStringSubmatch(line); m != nil {
		r.settle(r.check(m[1]), CheckPassed, m[2])
		r.current = -1

		return true
	}

	if m := chkFailedLine.FindStringSubmatch(line); m != nil {
		i := r.check(m[1])
		r.settle(i, CheckFailed, m[2])
		r.current, r.currentHasDetail = i, false

		return true
	}

	if m := chkSkipped.FindStringSubmatch(line); m != nil && Check(m[1]).Valid() {
		i := r.check(m[1])
		r.settle(i, CheckSkipped, "")
		r.out.checks[i].Error = m[2]
		r.current, r.currentHasDetail = i, false

		return true
	}

	if m := chkErrorHeader.FindStringSubmatch(line); m != nil {
		i := r.check(m[1])
		if r.out.checks[i].State == "" {
			r.out.checks[i].State = CheckFailed
		}

		r.current, r.currentHasDetail = i, len(r.out.checks[i].Detail) > 0

		return true
	}

	if m := chkError.FindStringSubmatch(line); m != nil {
		r.placeError(m[1])

		return true
	}

	if m := chkSuggestion.FindStringSubmatch(line); m != nil {
		r.placeSuggestion(m[1])

		return true
	}

	return false
}

// readSummary reads the summary line and the errors block's count.
func (r *checksReader) readSummary(line string) bool {
	if m := chkTotals.FindStringSubmatch(line); m != nil {
		r.out.totals = parseTotals(m[1], m[2], m[3])
		r.current = -1

		return true
	}

	if chkErrorsCount.MatchString(line) || chkFailedCount.MatchString(line) {
		r.current = -1

		return true
	}

	return false
}

// readOwn reads go-pre-commit's own lines: no files, turned off, its errors, and its
// warnings.
func (r *checksReader) readOwn(line string) bool {
	switch {
	case line == noFilesLine:
		r.out.noFiles = true
	case line == disabledLine:
		r.out.disabled = true
	case strings.HasPrefix(line, "Error: "):
		r.out.errors = scriptrun.KeepNote(r.out.errors, strings.TrimPrefix(line, "Error: "))
	case strings.HasPrefix(line, "✗ "):
		r.out.errors = scriptrun.KeepNote(r.out.errors, strings.TrimPrefix(line, "✗ "))
	case strings.HasPrefix(line, "⚠ "):
		r.out.Warn(strings.TrimPrefix(line, "⚠ "))
	default:
		return false
	}

	r.current = -1

	return true
}

// readDetail reads a line that follows a failed check's error: the lines go-pre-commit
// shows from the check's own output.
func (r *checksReader) readDetail(line string) {
	text := strings.TrimSpace(line)

	switch {
	case r.current >= 0 && r.currentHasDetail:
		// the errors block repeats what the Check Results block showed
	case r.current >= 0:
		c := &r.out.checks[r.current]
		if len(c.Detail) < maxDetail {
			c.Detail = append(c.Detail, text)
		}
	case r.sect == sectionResults || r.sect == sectionErrors:
		r.looseDetail = scriptrun.KeepNote(r.looseDetail, text)
	default:
		r.out.Unknown(line)
	}
}

// placeError sets the error of the check whose lines are being read, or keeps it loose.
func (r *checksReader) placeError(text string) {
	if r.current >= 0 && r.out.checks[r.current].Error == "" {
		r.out.checks[r.current].Error = text

		return
	}

	if r.current < 0 {
		r.looseErrors = scriptrun.KeepNote(r.looseErrors, text)
	}
}

// placeSuggestion sets the suggestion of the check whose lines are being read, or keeps
// it loose.
func (r *checksReader) placeSuggestion(text string) {
	if r.current >= 0 && r.out.checks[r.current].Suggestion == "" {
		r.out.checks[r.current].Suggestion = text

		return
	}

	if r.current < 0 {
		r.looseSuggestions = scriptrun.KeepNote(r.looseSuggestions, text)
	}
}

// check returns the index of the check called name, adding it when it's new.
func (r *checksReader) check(name string) int {
	i := slices.IndexFunc(r.out.checks, func(c CheckResult) bool { return c.Name == Check(name) })
	if i >= 0 {
		return i
	}

	r.out.checks = append(r.out.checks, CheckResult{Name: Check(name), Detail: []string{}, ChangedFiles: []string{}})

	return len(r.out.checks) - 1
}

// settle records how the check at i came out, unless a failure is already recorded, and
// how long it took.
func (r *checksReader) settle(i int, state CheckState, duration string) {
	c := &r.out.checks[i]

	if c.State != CheckFailed {
		c.State = state
	}

	if ms, ok := durationMs(duration); ok {
		c.DurationMs = ms
	}
}

// snapshot returns what the reader has read so far, sharing nothing with it: the loose
// lines placed where they can be, and the JSON report's checks and totals when it was
// read.
func (r *checksReader) snapshot() checksOutput {
	r.Lock()
	defer r.Unlock()

	out := r.out
	out.checks = make([]CheckResult, 0, len(r.out.checks))

	for _, c := range r.out.checks {
		c.Detail = slices.Clone(c.Detail)
		c.ChangedFiles = slices.Clone(c.ChangedFiles)
		out.checks = append(out.checks, c)
	}

	out.installs = slices.Clone(out.installs)
	out.errors = slices.Clone(out.errors)
	out.Warnings = slices.Clone(out.Warnings)
	out.Unrecognized = slices.Clone(out.Unrecognized)

	if !out.fromJSON {
		r.placeLoose(&out)
	}

	return out
}

// placeLoose gives the loose lines to the one failed check, when only one failed, or else
// keeps them as lines the reader didn't recognize. The Check Results block and the errors
// block say the same error, suggestion, and detail, so lines its own already say are
// dropped.
func (r *checksReader) placeLoose(out *checksOutput) {
	keep := func(lines []string) {
		for _, l := range lines {
			out.Unknown(l)
		}
	}

	if len(out.failedChecks()) != 1 {
		keep(r.looseErrors)
		keep(r.looseSuggestions)
		keep(r.looseDetail)

		return
	}

	c := &out.checks[slices.IndexFunc(out.checks, func(c CheckResult) bool { return c.State == CheckFailed })]

	if !fill(&c.Error, r.looseErrors) {
		keep(r.looseErrors)
	}

	if !fill(&c.Suggestion, r.looseSuggestions) {
		keep(r.looseSuggestions)
	}

	if len(c.Detail) == 0 {
		c.Detail = append(c.Detail, r.looseDetail[:min(len(r.looseDetail), maxDetail)]...)
	}
}

// fill sets field to the first of lines when it is empty, and reports whether every line
// says what it says.
func fill(field *string, lines []string) bool {
	if len(lines) == 0 {
		return true
	}

	if *field == "" {
		*field = lines[0]
	}

	return !slices.ContainsFunc(lines, func(l string) bool { return l != *field })
}

// readReport reads a line of the JSON report a run with --json prints last, and reports
// whether the line was the report's: from a line that is just "{" to the first line that
// is just "}" after it, at which the whole parses. A report that doesn't parse is kept as
// lines the reader didn't recognize.
func (r *checksReader) readReport(line string) bool {
	if !r.inReport {
		if line != "{" {
			return false
		}

		r.inReport, r.report, r.size = true, []string{line}, len(line)

		return true
	}

	r.report = append(r.report, line)
	r.size += len(line) + 1

	if line != "}" && r.size < maxReport {
		return true
	}

	r.inReport = false

	if !r.applyReport(strings.Join(r.report, "\n")) {
		for _, l := range r.report {
			r.out.Unknown(l)
		}
	}

	r.report = nil

	return true
}

// runReport is the document `go-pre-commit run --json` prints (1.14.1 and later), or its
// error document.
type runReport struct {
	Enabled *bool  `json:"enabled"`
	Result  string `json:"result"`
	Checks  []struct {
		Name         string   `json:"name"`
		Result       string   `json:"result"`
		DurationMs   int64    `json:"duration_ms"`
		ChangedFiles []string `json:"changed_files"`
		Error        string   `json:"error"`
		Suggestion   string   `json:"suggestion"`
		Output       string   `json:"output"`
	} `json:"checks"`
	Totals struct {
		Checks     int   `json:"checks"`
		Passed     int   `json:"passed"`
		Failed     int   `json:"failed"`
		Skipped    int   `json:"skipped"`
		Files      int   `json:"files"`
		DurationMs int64 `json:"duration_ms"`
	} `json:"totals"`
	Warnings []string `json:"warnings"`
	Error    *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// applyReport reads the JSON report into the output, and reports whether it was one.
func (r *checksReader) applyReport(text string) bool {
	var rep runReport
	if json.Unmarshal([]byte(text), &rep) != nil {
		return false
	}

	if rep.Error != nil {
		r.out.errors = scriptrun.KeepNote(r.out.errors, rep.Error.Message)

		return true
	}

	if rep.Enabled == nil {
		return false
	}

	o := &r.out
	o.fromJSON, o.disabled = true, !*rep.Enabled
	o.noFiles = *rep.Enabled && rep.Result == "skipped" && rep.Totals.Checks == 0
	o.totals = totals{
		known: true, passed: rep.Totals.Passed, failed: rep.Totals.Failed, skipped: rep.Totals.Skipped,
		files: rep.Totals.Files, durationMs: rep.Totals.DurationMs,
	}
	o.checks = make([]CheckResult, 0, len(rep.Checks))

	for _, c := range rep.Checks {
		o.checks = append(o.checks, CheckResult{
			Name: Check(c.Name), State: reportState(c.Result), DurationMs: c.DurationMs, Error: c.Error,
			Suggestion: c.Suggestion, Detail: detailLines(c.Output), ChangedFiles: slices.Clone(c.ChangedFiles),
		})
	}

	for _, w := range rep.Warnings {
		if !slices.Contains(o.Warnings, w) {
			o.Warn(w)
		}
	}

	return true
}

// reportState reads a check's result in the JSON report.
func reportState(result string) CheckState {
	switch result {
	case "passed":
		return CheckPassed
	case "failed":
		return CheckFailed
	default:
		return CheckSkipped
	}
}

// detailLines returns a check's output as its first maxDetail lines that aren't blank.
func detailLines(output string) []string {
	out := []string{}

	for line := range strings.Lines(output) {
		if line = strings.TrimSpace(line); line != "" && len(out) < maxDetail {
			out = append(out, line)
		}
	}

	return out
}

// parseTotals reads the summary line's counts, "5 passed, 1 failed", its files, and its
// duration.
func parseTotals(counts, files, duration string) totals {
	t := totals{known: true}
	t.files, _ = strconv.Atoi(files)
	t.durationMs, _ = durationMs(duration)

	for part := range strings.SplitSeq(counts, ", ") {
		n, word, _ := strings.Cut(part, " ")
		count, _ := strconv.Atoi(n)

		switch word {
		case "passed":
			t.passed = count
		case "failed":
			t.failed = count
		case "skipped":
			t.skipped = count
		}
	}

	return t
}

// durationMs reads a duration as go-pre-commit prints it, 141μs, 17ms, 2.6s, or 1.2m, in
// whole milliseconds.
func durationMs(text string) (int64, bool) {
	d, err := time.ParseDuration(text)
	if err != nil {
		return 0, false
	}

	return d.Milliseconds(), true
}
