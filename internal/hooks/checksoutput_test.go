package hooks

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readChecks feeds a new checks reader text in uneven pieces, as the log arrives, and
// flushes it.
func readChecks(text string, withJSON bool) checksOutput {
	r := newChecksReader(withJSON)

	for len(text) > 0 {
		n := min(len(text), 7)

		if _, err := r.Write([]byte(text[:n])); err != nil {
			panic(err)
		}

		text = text[n:]
	}

	r.Flush()

	return r.snapshot()
}

// states returns each check's state, by name.
func states(out checksOutput) map[Check]CheckState {
	got := map[Check]CheckState{}
	for _, c := range out.checks {
		got[c.Name] = c.State
	}

	return got
}

// checkNamed returns the check called name.
func checkNamed(t *testing.T, out checksOutput, name Check) CheckResult {
	t.Helper()

	for _, c := range out.checks {
		if c.Name == name {
			return c
		}
	}

	require.Failf(t, "no such check", "%s", name)

	return CheckResult{}
}

// allPassed is every check passing, as the synced config runs them, gitleaks included.
func allPassed() map[Check]CheckState {
	out := map[Check]CheckState{}
	for _, c := range Checks() {
		out[c] = CheckPassed
	}

	return out
}

// withFailed is allPassed with failed failing.
func withFailed(failed ...Check) map[Check]CheckState {
	out := allPassed()
	for _, c := range failed {
		out[c] = CheckFailed
	}

	return out
}

func TestChecksReaderReadsEveryRecording(t *testing.T) {
	t.Parallel()

	want := map[string]struct {
		states  map[Check]CheckState
		totals  totals
		noFiles bool
		off     bool
	}{
		"run-pass":                          {allPassed(), totals{known: true, passed: 6, files: 6, durationMs: 2600}, false, false},
		"run-fail-eof":                      {withFailed(CheckEOF), totals{known: true, passed: 5, failed: 1, files: 6, durationMs: 357}, false, false},
		"run-fail-fumpt":                    {allPassed(), totals{known: true, passed: 6, files: 6, durationMs: 353}, false, false},
		"run-fail-gitleaks":                 {allPassed(), totals{known: true, passed: 6, files: 7, durationMs: 2600}, false, false},
		"run-fail-lint":                     {withFailed(CheckLint), totals{known: true, passed: 5, failed: 1, files: 6, durationMs: 353}, false, false},
		"run-fail-modtidy":                  {withFailed(CheckModTidy), totals{known: true, passed: 5, failed: 1, files: 6, durationMs: 728}, false, false},
		"run-fail-whitespace":               {withFailed(CheckWhitespace), totals{known: true, passed: 5, failed: 1, files: 6, durationMs: 355}, false, false},
		"run-gitleaks-staged":               {withFailed(CheckGitleaks), totals{known: true, passed: 5, failed: 1, files: 1, durationMs: 330}, false, false},
		"run-whitespace-dirty-file":         {withFailed(CheckWhitespace), totals{known: true, passed: 5, failed: 1, files: 6, durationMs: 353}, false, false},
		"run-staged":                        {withFailed(CheckWhitespace), totals{known: true, passed: 5, failed: 1, files: 1, durationMs: 336}, false, false},
		"run-one-check":                     {map[Check]CheckState{CheckWhitespace: CheckFailed}, totals{known: true, failed: 1, files: 6, durationMs: 6}, false, false},
		"run-only":                          {map[Check]CheckState{CheckWhitespace: CheckFailed}, totals{known: true, failed: 1, files: 6, durationMs: 6}, false, false},
		"run-no-files":                      {map[Check]CheckState{}, totals{}, true, false},
		"run-disabled":                      {map[Check]CheckState{}, totals{}, false, true},
		"run-installs-tools":                {withFailed(CheckFumpt, CheckGitleaks, CheckLint), totals{known: true, passed: 3, failed: 3, files: 6, durationMs: 16500}, false, false},
		"run-installs-tools-gopath-on-path": {withFailed(CheckLint), totals{known: true, passed: 5, failed: 1, files: 6, durationMs: 10400}, false, false},
	}

	for _, name := range recordingNames(t, "run-") {
		expected, ok := want[name]
		require.True(t, ok, "%s has no expectation", name)

		got := readChecks(loadRecording(t, name).Log("\n"), false)

		assert.Equal(t, expected.states, states(got), name)
		assert.Equal(t, expected.totals, got.totals, name)
		assert.Equal(t, expected.noFiles, got.noFiles, name)
		assert.Equal(t, expected.off, got.disabled, name)
		assert.Empty(t, got.errors, name)
		assert.False(t, got.fromJSON, name)

		if name != "run-installs-tools" {
			assert.Empty(t, got.Unrecognized, "%s: only three checks failing at once leave lines it can't place", name)
		}
	}
}

func TestChecksReaderPlacesEachFailedChecksWords(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, error, suggestion string
		check                   Check
		detail                  string
	}{
		{"run-fail-eof", "EOF issues found", "", CheckEOF, "EOF issues found"},
		{"run-fail-whitespace", "whitespace issues found", "", CheckWhitespace, "whitespace issues found"},
		{
			"run-fail-modtidy", "command 'go mod tidy (in one or more modules)' failed",
			"Fix the mod tidy issues shown above. Run 'go mod tidy' in each module directory.", CheckModTidy,
			"Module . needs tidying:",
		},
		{
			"run-fail-lint", "Found 1 linting issue(s):",
			"Fix the linting issues shown above. Run 'golangci-lint run .' to see full details.", CheckLint,
			"main.go:6:11: Error return value of `os.Remove` is not checked (errcheck)",
		},
		{
			"run-gitleaks-staged", "Found 1 secret(s):",
			"Remove secrets from code or add exceptions to .gitleaks.toml allowlist", CheckGitleaks,
			"Secret:      <redacted-token>",
		},
		{
			"run-installs-tools-gopath-on-path", "golangci-lint not found",
			"Install golangci-lint manually: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest",
			CheckLint, "",
		},
	} {
		got := checkNamed(t, readChecks(loadRecording(t, c.name).Log("\n"), false), c.check)

		assert.Equal(t, c.error, got.Error, c.name)
		assert.Equal(t, c.suggestion, got.Suggestion, c.name)

		if c.detail != "" {
			assert.Contains(t, got.Detail, c.detail, c.name)
		}
	}
}

func TestChecksReaderReadsTheToolsItInstalls(t *testing.T) {
	t.Parallel()

	got := readChecks(loadRecording(t, "run-installs-tools").Log("\n"), false)
	assert.Equal(t, []string{"gofumpt@v0.12.0", "gitleaks@v8.30.1", "golangci-lint@v2.15.0"}, got.installs)

	assert.InDelta(t, 4700, checkNamed(t, got, CheckFumpt).DurationMs, 1)
}

// programOrder is run-installs-tools as the app's log holds it: stdout and stderr in the
// order go-pre-commit wrote them, which two pipes didn't keep.
const programOrder = `⏳ Running eof check...
⏳ Running fumpt check...
→ Installing gofumpt@v0.12.0...
✗ fumpt check failed (4.7s)
✓ eof check passed (135μs)
✗ lint check failed (11.8s)

Check Results
─────────────
✓ eof completed successfully (135μs)
✗ fumpt failed (4.7s)
  Error: command 'gofumpt' failed
    tool installed but not found in PATH: gofumpt
💡 Failed to install gofumpt. Verify network access, then retry.
✗ lint failed (11.8s)
  Error: golangci-lint not found
💡 Install golangci-lint manually.
⚠ gitleaks - gitleaks is not installed
💡 Install gitleaks.

Summary:
✗ 1 passed, 2 failed, 1 skipped on 6 file(s) in 1.2m

ERRORS FOUND
────────────
✗ 2 check(s) failed with the following errors:

✗ ━ fumpt ━
    tool installed but not found in PATH: gofumpt
💡 To fix: Failed to install gofumpt. Verify network access, then retry.

✗ ━ lint ━
    golangci-lint not found
💡 To fix: Install golangci-lint manually.

ℹ Run with --verbose flag to see full error details
ℹ Fix the errors above and run the checks again
Error: checks failed: 2
`

func TestChecksReaderInProgramOrder(t *testing.T) {
	t.Parallel()

	got := readChecks(programOrder, false)

	assert.Equal(t, map[Check]CheckState{
		CheckEOF: CheckPassed, CheckFumpt: CheckFailed, CheckLint: CheckFailed, CheckGitleaks: CheckSkipped,
	}, states(got))
	assert.Equal(t, totals{known: true, passed: 1, failed: 2, skipped: 1, files: 6, durationMs: 72000}, got.totals)
	assert.Empty(t, got.Unrecognized)

	fumpt := checkNamed(t, got, CheckFumpt)
	assert.Equal(t, "command 'gofumpt' failed", fumpt.Error)
	assert.Equal(t, "Failed to install gofumpt. Verify network access, then retry.", fumpt.Suggestion)
	assert.Equal(t, []string{"tool installed but not found in PATH: gofumpt"}, fumpt.Detail, "the errors block isn't kept twice")

	lint := checkNamed(t, got, CheckLint)
	assert.Equal(t, "golangci-lint not found", lint.Error)
	assert.Equal(t, "Install golangci-lint manually.", lint.Suggestion)
	assert.Equal(t, []string{"golangci-lint not found"}, lint.Detail, "the errors block shows what Check Results didn't")

	gitleaks := checkNamed(t, got, CheckGitleaks)
	assert.Equal(t, "gitleaks is not installed", gitleaks.Error)
	assert.Equal(t, "Install gitleaks.", gitleaks.Suggestion)

	assert.Equal(t, []Check{CheckFumpt, CheckLint}, got.failedChecks())
}

func TestChecksReaderKeepsWhatItCantPlace(t *testing.T) {
	t.Parallel()

	got := readChecks(strings.Join([]string{
		"Check Results",
		"  Error: one",
		"💡 fix one",
		"    a loose detail",
		"✗ eof check failed (1ms)",
		"✗ lint check failed (1ms)",
		"Error: failed to load configuration",
		"⚠ a warning of its own",
		"a line it never saw",
	}, "\n")+"\n", false)

	assert.Equal(t, []string{"failed to load configuration"}, got.errors)
	assert.Equal(t, []string{"a warning of its own"}, got.Warnings)
	assert.Equal(t, []string{"one", "fix one", "a loose detail", "a line it never saw"}, got.Unrecognized,
		"with two failed checks, the loose lines can't be placed")

	// With one failed check whose own words differ, the loose ones are kept too.
	got = readChecks(strings.Join([]string{
		"✗ eof failed (1ms)",
		"  Error: its own",
		"💡 its own fix",
		"Summary:",
		"Check Results",
		"  Error: another",
		"💡 another fix",
	}, "\n")+"\n", false)

	eof := checkNamed(t, got, CheckEOF)
	assert.Equal(t, "its own", eof.Error)
	assert.Equal(t, "its own fix", eof.Suggestion)
	assert.Equal(t, []string{"another", "another fix"}, got.Unrecognized)
}

func TestChecksReaderSnapshotSharesNothing(t *testing.T) {
	t.Parallel()

	r := newChecksReader(false)
	_, err := r.Write([]byte("✗ eof failed (1ms)\n  Error: x\n    detail\n"))
	require.NoError(t, err)

	first := r.snapshot()
	first.checks[0].Detail[0] = "changed"

	assert.Equal(t, "detail", r.snapshot().checks[0].Detail[0])
}

// report is a run --json report as go-pre-commit 1.14.1 prints it, with the human output
// before it on stderr.
const report = `⏳ Running whitespace check...
✗ whitespace check failed (6ms)
⚠ golangci-lint config not found, using defaults
{
  "version": "1.14.1",
  "enabled": true,
  "result": "failed",
  "files": ["README.md", "main.go"],
  "checks": [
    {"name": "eof", "result": "passed", "duration_ms": 1, "files": ["README.md"], "changed_files": []},
    {"name": "lint", "result": "skipped", "duration_ms": 0, "files": [], "changed_files": null, "error": "golangci-lint not found", "suggestion": "Install it"},
    {
      "name": "whitespace",
      "result": "failed",
      "duration_ms": 6,
      "files": ["README.md"],
      "changed_files": ["README.md"],
      "error": "whitespace issues found",
      "suggestion": "Review the changes",
      "output": "README.md: trailing whitespace\n\n  line 3\n"
    }
  ],
  "totals": {"checks": 3, "passed": 1, "failed": 1, "skipped": 1, "files": 2, "duration_ms": 9},
  "warnings": ["lint skipped: golangci-lint not found", "golangci-lint config not found, using defaults"]
}
`

func TestChecksReaderReadsTheJSONReport(t *testing.T) {
	t.Parallel()

	got := readChecks(report, true)

	require.True(t, got.fromJSON)
	assert.Equal(t, totals{known: true, passed: 1, failed: 1, skipped: 1, files: 2, durationMs: 9}, got.totals)
	assert.Equal(t, map[Check]CheckState{CheckEOF: CheckPassed, CheckLint: CheckSkipped, CheckWhitespace: CheckFailed}, states(got))
	assert.Empty(t, got.Unrecognized)
	assert.Equal(t, []string{"golangci-lint config not found, using defaults", "lint skipped: golangci-lint not found"},
		got.Warnings, "a warning both said is kept once")

	ws := checkNamed(t, got, CheckWhitespace)
	assert.Equal(t, CheckResult{
		Name: CheckWhitespace, State: CheckFailed, DurationMs: 6, Error: "whitespace issues found",
		Suggestion: "Review the changes", Detail: []string{"README.md: trailing whitespace", "line 3"},
		ChangedFiles: []string{"README.md"},
	}, ws)
	assert.Empty(t, checkNamed(t, got, CheckLint).ChangedFiles)
}

func TestChecksReaderReadsJSONEdges(t *testing.T) {
	t.Parallel()

	off := readChecks("{\n  \"version\": \"1.14.1\", \"enabled\": false, \"result\": \"skipped\", \"checks\": []\n}\n", true)
	assert.True(t, off.disabled)
	assert.False(t, off.noFiles)

	none := readChecks("{\n  \"version\": \"1.14.1\", \"enabled\": true, \"result\": \"skipped\", \"checks\": []\n}\n", true)
	assert.True(t, none.noFiles)

	failed := readChecks("{\n  \"version\": \"1.14.1\", \"error\": {\"code\": \"config_not_found\", \"message\": \"no config\"}\n}\n", true)
	assert.Equal(t, []string{"no config"}, failed.errors)

	broken := readChecks("{\n  \"version\": \"1.14.1\",\n  \"result\": \n}\n", true)
	assert.False(t, broken.fromJSON)
	assert.Equal(t, []string{"{", `  "version": "1.14.1",`, `  "result":`, "}"}, broken.Unrecognized)

	notReport := readChecks("{\n  \"other\": 1\n}\n", true)
	assert.False(t, notReport.fromJSON)
	assert.Len(t, notReport.Unrecognized, 3)

	// A brace on its own in a run without --json is just a line.
	text := readChecks("{\n}\n", false)
	assert.Equal(t, []string{"{", "}"}, text.Unrecognized)
}

func TestDurationsAsGoPreCommitPrintsThem(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]int64{"141μs": 0, "17ms": 17, "2.6s": 2600, "1.2m": 72000, "1500µs": 1} {
		got, ok := durationMs(text)
		assert.True(t, ok, text)
		assert.Equal(t, want, got, text)
	}

	_, ok := durationMs("soon")
	assert.False(t, ok)
}
