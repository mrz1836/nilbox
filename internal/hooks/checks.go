package hooks

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ChecksRequest is a checks run the window asks for in one clone.
type ChecksRequest struct {
	// Dir is the clone, as the page lists it (Repo.Dir), with ~.
	Dir string `json:"dir"`

	// Scope is which files: every tracked file, the staged ones, or one check on every
	// tracked file, Check.
	Scope Scope `json:"scope"`
	Check Check `json:"check"`

	// RunsIn is where it runs, or "" for the setting.
	RunsIn integrationprefs.RunsIn `json:"runsIn"`
}

// ChecksIntent is what a checks run sets out to do, written into its run's result as it
// starts, so Run again does the same.
type ChecksIntent struct {
	Scope Scope `json:"scope"`
	Check Check `json:"check"`
}

// ChecksResult is a checks run's result for the window: its run's outcome detail
// (scriptrun.Result.Outcome), from what go-pre-commit said and what git says of the clone
// before and after.
type ChecksResult struct {
	// Scope and Check are what was asked; Name and Dir are the clone, as in ActionResult.
	Scope Scope  `json:"scope"`
	Check Check  `json:"check"`
	Name  string `json:"name"`
	Dir   string `json:"dir"`

	// Message sums the run up in a sentence.
	Message string `json:"message"`

	// Checks are every check: each that ran, then each the repository turns off, then
	// each that didn't run.
	Checks []CheckResult `json:"checks"`

	// Passed, Failed, and Skipped count the checks that ran, Files the files they
	// checked, and DurationMs how long they took, as go-pre-commit said.
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	Skipped    int   `json:"skipped"`
	Files      int   `json:"files"`
	DurationMs int64 `json:"durationMs"`

	// NoFiles reports nothing to check, as with nothing staged, and Disabled a config
	// that turns go-pre-commit off.
	NoFiles  bool `json:"noFiles"`
	Disabled bool `json:"disabled"`

	// Changed are the files the run changed or staged, a file already dirty before it
	// included. ChangedKnown reports that git's status was read before and after, so
	// Changed is the whole list; ChangedError says why it wasn't.
	Changed      []ChangedFile `json:"changed"`
	ChangedKnown bool          `json:"changedKnown"`
	ChangedError string        `json:"changedError"`

	// Installs are the tools go-pre-commit installed for the checks, as in
	// "gofumpt@v0.12.0", and Errors its own errors, apart from the failed checks.
	Installs []string `json:"installs"`
	Errors   []string `json:"errors"`
}

// RunChecks runs go-pre-commit's checks in the clone req names, and returns the run's ID.
// The run's Before takes git's status of the clone under its lock, and its Finish again
// once the checks end, so the result names the files they changed.
func (s *Service) RunChecks(ctx context.Context, req ChecksRequest) (int64, error) {
	if s.runs == nil {
		return 0, ErrNotSetUp
	}

	if req.RunsIn != "" && !req.RunsIn.Valid() {
		return 0, fmt.Errorf("%w: the place %q", ErrInvalidRequest, req.RunsIn)
	}

	c, err := s.cloneAt(ctx, req.Dir)
	if err != nil {
		return 0, err
	}

	where, err := s.where(ctx, req.RunsIn)
	if err != nil {
		return 0, err
	}

	spec, err := s.checksSpec(ctx, c, ChecksIntent{Scope: req.Scope, Check: req.Check}, where)
	if err != nil {
		return 0, err
	}

	id, err := s.runs.Start(ctx, spec)
	if err != nil {
		return id, err
	}

	s.logger.InfoContext(ctx, "a checks run started", "run", id, "scope", string(req.Scope), "repo", c.label(s.home),
		"in", string(where))

	return id, nil
}

// checksRun is one checks run as it goes: what it was asked, and what its Before read.
type checksRun struct {
	intent ChecksIntent
	clone  clone
	where  integrationprefs.RunsIn

	// config and before are the clone's config and its snapshot, under the lock, and kept
	// what the run keeps of them beside its record (scriptrun.Spec.Keep).
	config repoConfig
	before snapshot
	kept   *keptChecks

	// cut reports a step of the Finish cut short by the app's quit (cutShort).
	cut bool
}

// checksSpec is the checks run in c that intent asks for. Its Before keeps the config's
// switches and the clone's snapshot beside the run's record (Keep), for the Verifier.
func (s *Service) checksSpec(ctx context.Context, c clone, intent ChecksIntent, where integrationprefs.RunsIn,
) (scriptrun.Spec, error) {
	if err := checkIntent(intent); err != nil {
		return scriptrun.Spec{}, err
	}

	tool := s.tool(ctx)
	if !tool.Usable() {
		return scriptrun.Spec{}, noToolError(tool)
	}

	run := &checksRun{intent: intent, clone: c, where: where, kept: &keptChecks{}}
	reader := newChecksReader(tool.Has(integrations.FeatureRunJSON))
	base, _ := s.env()

	return scriptrun.Spec{
		Kind: scriptrun.KindChecks, Title: checksTitle(intent), Source: string(intent.Scope), Repo: c.name, Dir: c.dir,
		Env: runEnv(base, where), RunsIn: where, Tool: string(toolID), Intent: intent, Keep: run.kept,
		Output: reader,
		Before: func(ctx context.Context) ([]string, error) { return s.beforeChecks(ctx, run) },
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			return settled(run.cut, s.finishChecks(ctx, run, reader, end, engine))
		},
		Recorded: func(ctx context.Context, _ int64, _ scriptrun.Outcome) { s.checksRecorded(ctx) },
		// The window reads the page again once the run lets go of the clone, its record
		// kept: the row then shows the clone free and this run as its newest.
		Released: func(int64) { s.events.Emit(event.HooksUpdated{}) },
	}, nil
}

// checkIntent checks a checks run's scope and check.
func checkIntent(intent ChecksIntent) error {
	switch {
	case !intent.Scope.Valid():
		return fmt.Errorf("%w: the scope %q", ErrInvalidRequest, intent.Scope)
	case intent.Scope == ScopeOne && !intent.Check.Valid():
		return fmt.Errorf("%w: the check %q", ErrInvalidRequest, intent.Check)
	case intent.Scope != ScopeOne && intent.Check != "":
		return fmt.Errorf("%w: a check with the scope %q", ErrInvalidRequest, intent.Scope)
	default:
		return nil
	}
}

// beforeChecks is a checks run's Before: under the clone's lock, it reads go-pre-commit and
// the clone's config again, takes the clone's snapshot, keeps both beside the run's record,
// and returns go-pre-commit's argv.
func (s *Service) beforeChecks(ctx context.Context, run *checksRun) ([]string, error) {
	tool := s.tool(ctx)
	if !tool.Usable() {
		return nil, noToolError(tool)
	}

	run.config = readConfig(run.clone.dir)
	if !run.config.present {
		return nil, notSetUpError(run.clone, s.home)
	}

	run.before = s.take(ctx, run.clone.dir)
	*run.kept = keptChecks{Checks: run.config.settings(), Snapshot: keepSnapshot(run.before)}

	return checksArgv(tool, run.intent, run.where), nil
}

// checksArgv is go-pre-commit's command for a checks run: every tracked file, the staged
// files, or one check, with --json for a copy that has FeatureRunJSON, and colors off for
// a run in the app.
func checksArgv(tool ToolFacts, intent ChecksIntent, where integrationprefs.RunsIn) []string {
	argv := []string{tool.Path, "run"}

	if intent.Scope == ScopeOne {
		argv = append(argv, string(intent.Check))
	}

	argv = withColor(argv, where)

	if intent.Scope != ScopeStaged {
		argv = append(argv, "--all-files")
	}

	if tool.Has(integrations.FeatureRunJSON) {
		argv = append(argv, jsonFlag)
	}

	return argv
}

// finishChecks works out a checks run's result once go-pre-commit ended: the checks as it
// said, and the files git says changed since Before.
func (s *Service) finishChecks(ctx context.Context, run *checksRun, reader *checksReader, end scriptrun.Ending,
	engine scriptrun.Outcome,
) scriptrun.Outcome {
	reader.Flush()
	said := reader.snapshot()

	res := ChecksResult{
		Scope: run.intent.Scope, Check: run.intent.Check, Name: display(run.clone.name, run.clone.dir),
		Dir: paths.Tilde(run.clone.dir, s.home), Changed: []ChangedFile{}, Installs: textutil.NonNil(said.installs),
		Errors: textutil.NonNil(said.errors), NoFiles: said.noFiles, Disabled: said.disabled,
	}

	res.Checks = allChecks(said.checks, run.config, run.intent)
	res.Passed, res.Failed, res.Skipped = counts(res.Checks)

	if said.totals.known {
		res.Files, res.DurationMs = said.totals.files, said.totals.durationMs
	}

	s.changes(ctx, run, said, &res)

	ok := res.Failed == 0 && len(res.Errors) == 0 && (end.ExitCode == nil || *end.ExitCode == 0)
	res.Message = checksMessage(res, ok)

	status, summary := checksStatus(ok, res.Message, end, engine)

	return scriptrun.Outcome{
		Status: status, Summary: summary, Warnings: said.warnings, Unrecognized: said.unrecognized,
		UnrecognizedCount: said.unrecognizedCount, Detail: res,
	}
}

// checksStatus works out how a checks run ended: succeeded when every check that ran
// passed and go-pre-commit exited 0, stopped or interrupted as the engine saw it, and
// otherwise failed, in the engine's words for a run past its limit.
func checksStatus(ok bool, message string, end scriptrun.Ending, engine scriptrun.Outcome) (scriptrun.Status, string) {
	switch {
	case engine.Status == scriptrun.StatusStopped || engine.Status == scriptrun.StatusInterrupted:
		return engine.Status, engine.Summary
	case engine.Status == scriptrun.StatusFailed && end.ExitCode == nil:
		return scriptrun.StatusFailed, engine.Summary
	case ok && engine.Status == scriptrun.StatusSucceeded:
		return scriptrun.StatusSucceeded, message
	default:
		return scriptrun.StatusFailed, message
	}
}

// changes takes the clone's snapshot again and lists the files the run changed, with the
// checks go-pre-commit's JSON report says changed each. A read of git's status the app's
// quit cut short cuts the Finish short.
func (s *Service) changes(ctx context.Context, run *checksRun, said checksOutput, res *ChecksResult) {
	if run.before.err != nil {
		res.ChangedError = uierr.Message(run.before.err)

		return
	}

	after := s.takeAfter(ctx, run.clone.dir, run.before)
	if after.err != nil {
		run.cut = cutShort(ctx, after.err)
		res.ChangedError = uierr.Message(after.err)

		return
	}

	res.Changed, res.ChangedKnown = changedFiles(run.before, after), true

	for i := range res.Changed {
		for _, c := range said.checks {
			if slices.Contains(c.ChangedFiles, res.Changed[i].Path) {
				res.Changed[i].By = append(res.Changed[i].By, c.Name)
			}
		}
	}
}

// checksRecorded notes, once a checks run's end is recorded, that one has ended, which
// folds the page's note about the tools the checks install.
func (s *Service) checksRecorded(ctx context.Context) {
	if s.memory == nil {
		return
	}

	if err := s.memory.Set(ctx, checksRanKey, "true"); err != nil {
		s.logger.WarnContext(ctx, "couldn't note that a checks run ended", "error", err)
	}
}

// allChecks lists every check for the result: those the run named, in go-pre-commit's
// order, then each it didn't, off when the repository turns it off, else not run.
func allChecks(ran []CheckResult, cfg repoConfig, intent ChecksIntent) []CheckResult {
	out := make([]CheckResult, 0, len(Checks()))

	for _, c := range ran {
		if c.State == "" {
			c.State = CheckNotRun // started, and never said how it came out
		}

		c.Detail, c.ChangedFiles = textutil.NonNil(c.Detail), textutil.NonNil(c.ChangedFiles)
		out = append(out, c)
	}

	slices.SortStableFunc(out, func(a, b CheckResult) int { return checkRank(a.Name) - checkRank(b.Name) })

	for _, name := range Checks() {
		if slices.ContainsFunc(out, func(c CheckResult) bool { return c.Name == name }) {
			continue
		}

		state := CheckNotRun
		if !cfg.checkOn(name) && (intent.Scope != ScopeOne || intent.Check == name) {
			state = CheckOff
		}

		out = append(out, CheckResult{Name: name, State: state, Detail: []string{}, ChangedFiles: []string{}})
	}

	return out
}

// checkRank orders checks as go-pre-commit lists them, one it doesn't know last.
func checkRank(c Check) int {
	if i := slices.Index(Checks(), c); i >= 0 {
		return i
	}

	return len(Checks())
}

// counts counts the checks that passed, failed, and were skipped.
func counts(checks []CheckResult) (passed, failed, skipped int) {
	for _, c := range checks {
		switch c.State {
		case CheckPassed:
			passed++
		case CheckFailed:
			failed++
		case CheckSkipped:
			skipped++
		case CheckOff, CheckNotRun:
		}
	}

	return passed, failed, skipped
}

// checksTitle is what the Runs list calls a checks run.
func checksTitle(intent ChecksIntent) string {
	switch intent.Scope {
	case ScopeStaged:
		return "Checks · staged files"
	case ScopeOne:
		return "Check " + string(intent.Check)
	case ScopeAll:
	}

	return "Checks · all files"
}

// checksMessage sums a checks run up: "All 5 checks passed on 41 files in 38 s.", "4
// passed, 1 failed: whitespace.", and so on.
func checksMessage(res ChecksResult, ok bool) string {
	switch {
	case res.Disabled:
		return "go-pre-commit is turned off in this repository (ENABLE_GO_PRE_COMMIT=false)."
	case res.NoFiles:
		return "No files to check."
	case len(res.Errors) > 0 && res.Passed+res.Failed+res.Skipped == 0:
		return textutil.WithReason("go-pre-commit couldn't run the checks", res.Errors[len(res.Errors)-1])
	case res.Failed > 0:
		return failedChecksMessage(res)
	case ok && res.Passed > 0:
		return passedChecksMessage(res)
	default:
		return "The checks ended without saying how they came out."
	}
}

// failedChecksMessage names the checks that failed.
func failedChecksMessage(res ChecksResult) string {
	var failed []string

	for _, c := range res.Checks {
		if c.State == CheckFailed {
			failed = append(failed, string(c.Name))
		}
	}

	if res.Passed == 0 {
		return fmt.Sprintf("%d failed: %s.", res.Failed, strings.Join(failed, ", "))
	}

	return fmt.Sprintf("%d passed, %d failed: %s.", res.Passed, res.Failed, strings.Join(failed, ", "))
}

// passedChecksMessage says every check that ran passed, on how many files, how fast.
func passedChecksMessage(res ChecksResult) string {
	text := "All " + plural(res.Passed, "check") + " passed"
	if res.Passed == 1 {
		text = "The check passed"
	}

	if res.Files > 0 {
		text += " on " + plural(res.Files, "file")
	}

	if res.DurationMs > 0 {
		text += " in " + secondsText(res.DurationMs)
	}

	return text + "."
}

// plural writes n with its noun, in the plural unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

// secondsText writes a duration the way the result says it: "350 ms", "2.6 s", "38 s".
func secondsText(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%d ms", ms)
	case ms < 10000:
		return fmt.Sprintf("%.1f s", float64(ms)/1000)
	default:
		return fmt.Sprintf("%d s", (ms+500)/1000)
	}
}
