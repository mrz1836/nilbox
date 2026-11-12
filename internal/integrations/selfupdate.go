package integrations

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/toolinstall"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// StepUpdating is an install's step while the tool's own updater runs.
const StepUpdating = "updating"

// maxWarnings caps the warning lines a tool's own update keeps in its result.
const maxWarnings = 50

// Runs starts the runs of the tools' own updaters and follows them to their end.
// *scriptrun.Engine implements it.
type Runs interface {
	Start(ctx context.Context, spec scriptrun.Spec) (int64, error)

	// Updating returns the ID of the tool's own update going now, however it started, one
	// from before the app restarted included, or 0.
	Updating(tool string) int64
}

// SelfUpdate is the last run of a tool's own updater since the app started, or one the
// runner follows from before it restarted, as the settings card follows it.
type SelfUpdate struct {
	// Run is the run's ID, or 0 when the tool hasn't updated itself since the app started,
	// and Running reports that it goes now.
	Run     int64 `json:"run"`
	Running bool  `json:"running"`

	// Updated reports that it ended with the copy at a newer version, Failed that it ended
	// any way but updated or already up to date, and Message says what it did, as "Updated
	// magex to 1.30.0" or "magex is already up to date (1.30.0)", or why it failed, once the
	// run ended and the tool was looked for again; until then all three are unset.
	Updated bool   `json:"updated"`
	Failed  bool   `json:"failed"`
	Message string `json:"message"`
}

// updateIntent is what a run of a tool's own updater sets out to do, recorded as it
// starts, so one from before the app restarted is checked the same way.
type updateIntent struct {
	// Tool is the tool, Path the copy it updates, and From the version the copy reported.
	Tool integrationprefs.ID `json:"tool"`
	Path string              `json:"path"`
	From string              `json:"from"`

	// Installed reports a copy the app installed, which still reads as installed by the app
	// once it updated itself in place.
	Installed bool `json:"installed"`
}

// updateResult is what a tool's own updater did, as the app checked it once the run ended:
// the run's result.
type updateResult struct {
	Tool integrationprefs.ID `json:"tool"`

	// From is the version before, and To the version the copy reports now, or "" when it
	// doesn't run any more.
	From string `json:"from"`
	To   string `json:"to"`

	// Updated reports a copy at a newer version than before, and Message says what was done.
	Updated bool   `json:"updated"`
	Message string `json:"message"`
}

// selfRun is one run of a tool's own updater: what it sets out to do, the environment it
// runs with, its output as it arrives, and, under the Service's mu, its ID, whether it
// goes, and what it did.
type selfRun struct {
	def    Definition
	intent updateIntent
	env    []string
	reader *updateReader

	id      int64
	running bool
	settled bool
	failed  bool
	result  updateResult
}

// view is the run as the card follows it.
func (r *selfRun) view() SelfUpdate {
	return SelfUpdate{
		Run: r.id, Running: r.running, Updated: r.result.Updated, Failed: r.failed, Message: r.result.Message,
	}
}

// argv is the tool's own update command.
func (r *selfRun) argv() []string {
	return append([]string{r.intent.Path}, r.def.UpdateArgs...)
}

// settle keeps what the run did, as its outcome says.
func (r *selfRun) settle(res updateResult, out scriptrun.Outcome) {
	r.result, r.failed, r.settled = res, out.Status != scriptrun.StatusSucceeded, true
}

// Verifier settles a tool's own update from before the app restarted as one that just
// ended: once the shell's environment is read, it looks for the tools again, reads the
// copy's version from that look, and keeps a copy the app installed recorded as the app's.
// Core registers it with the runner for scriptrun.KindUpdate.
type Verifier struct {
	s *Service
}

// NewVerifier returns the Verifier of s's tools' own updates.
func NewVerifier(s *Service) Verifier {
	return Verifier{s: s}
}

// Verify gives a tool's own update from before the app restarted its output reader and its
// Finish. A record whose intent can't be read, or names no tool the app knows, gets none:
// the runner's own outcome stands.
func (v Verifier) Verify(run scriptrun.Record) scriptrun.Verification {
	def, in, ok := v.s.intentOf(run.Result.Intent)
	if !ok {
		v.s.logger.Warn("a tool's update from before the app restarted can't be checked", "run", run.ID)

		return scriptrun.Verification{}
	}

	r := &selfRun{def: def, intent: in, reader: &updateReader{}, id: run.ID}

	return scriptrun.Verification{Callbacks: v.s.updateCallbacks(r, true)}
}

// updatesItself reports whether the copy c updates itself, with the tool's own updater,
// rather than through the app's installer: there is a runner to run it; the tool has one,
// and c's version has it (FeatureSelfUpdate); and c runs, came from a GitHub release or the
// app, and can be replaced in place, outside go install's folders bins. Homebrew's copies,
// go install's, links, and development builds never update themselves here. Detection works
// it out once per look (detection.selfUpdates).
func (s *Service) updatesItself(def Definition, c Copy, bins goFolders) bool {
	switch {
	case s.runs == nil || len(def.UpdateArgs) == 0 || !c.Runs || c.Link:
		return false
	case c.Source != SourceApp && c.Source != SourceRelease:
		return false
	default:
		return def.Has(FeatureSelfUpdate, c.Version) && replaceable(c.Path, bins)
	}
}

// replaceable reports whether the file at path can be replaced in place by its own updater,
// as the updaters check before they write: an executable file, no link, that its owner may
// write, in a folder the user may write that is surely none of go install's bins, where
// magex's updater would run go install instead.
func replaceable(path string, bins goFolders) bool {
	info, err := os.Lstat(path)
	if err != nil || !execpath.Executable(info.Mode()) || info.Mode().Perm()&0o200 == 0 {
		return false
	}

	dir := filepath.Dir(path)

	return writable(dir) && !bins.unsure && !bins.has(dir)
}

// claimUpdate works out whether the tool updates itself and, when it does, claims its
// update, so nothing else installs the tool until the run ends; ok is false when the app
// installs the release itself. It refuses an update the plan blocks, and one under way.
func (s *Service) claimUpdate(ctx context.Context, def Definition) (*selfRun, bool, error) {
	if s.runs == nil {
		return nil, false, nil
	}

	s.installMu.Lock()
	defer s.installMu.Unlock()

	if err := s.updatingError(def); err != nil {
		return nil, false, err
	}

	plan, _, d, err := s.planFor(ctx, def)

	switch {
	case err != nil:
		return nil, false, err
	case plan.Updater == "":
		return nil, false, nil
	case plan.Blocked != "":
		return nil, false, uierr.New(plan.Blocked, ErrInstallRefused)
	}

	env, _ := s.env.Env()
	c := d.selected
	r := &selfRun{
		def:    def,
		intent: updateIntent{Tool: def.ID, Path: c.Path, From: c.Version.Raw, Installed: c.Source == SourceApp},
		env:    def.RunEnv(env, integrationprefs.RunsInApp), reader: &updateReader{}, running: true,
	}

	s.mu.Lock()
	if s.updates == nil {
		s.updates = map[integrationprefs.ID]*selfRun{}
	}

	s.updates[def.ID] = r
	s.mu.Unlock()

	return r, true, nil
}

// updatingError refuses to install or update a tool while its own updater runs.
func (s *Service) updatingError(def Definition) error {
	if !s.updating(def.ID) {
		return nil
	}

	return uierr.New(fmt.Sprintf("%s is updating itself. Try again once it ends.", def.Binary), ErrInstallRefused)
}

// updating reports whether the tool's own updater runs now: one this run started, or one
// the runner follows from before the app restarted.
func (s *Service) updating(id integrationprefs.ID) bool {
	s.mu.Lock()
	r := s.updates[id]
	own := r != nil && r.running
	s.mu.Unlock()

	return own || (s.runs != nil && s.runs.Updating(string(id)) != 0)
}

// orphanedUpdate returns the tool's own update the runner follows that this run didn't
// start, as one from before the app restarted, or 0.
func (s *Service) orphanedUpdate(id integrationprefs.ID) int64 {
	if s.runs == nil {
		return 0
	}

	run := s.runs.Updating(string(id))

	s.mu.Lock()
	defer s.mu.Unlock()

	if r := s.updates[id]; r != nil && r.running {
		return 0
	}

	return run
}

// updateItself starts the tool's own updater over its copy, as a run in the app, in the
// copy's folder, and returns the run's ID with the tools as they are now. The run's Before
// checks the copy again under its folder's lock, and its Finish checks what the updater
// did.
func (s *Service) updateItself(ctx context.Context, r *selfRun) (InstallResult, error) {
	id, err := s.runs.Start(ctx, s.updateSpec(r))
	if err != nil {
		s.unclaim(r, id)

		return InstallResult{}, startError(r.def, err)
	}

	s.mu.Lock()
	r.id = id
	s.mu.Unlock()

	s.logger.InfoContext(ctx, "a tool started updating itself", "tool", r.def.Name, "run", id)
	s.events.Emit(event.ToolsUpdated{})

	tools, err := s.List(ctx)
	if err != nil {
		return InstallResult{}, err
	}

	return InstallResult{Run: id, Tools: tools}, nil
}

// updateSpec is the run of a tool's own updater: its command with the shell's environment
// and the tool's quiet variables, in the copy's folder, always in the app, where its output
// shows as it comes; nothing about it needs a terminal.
func (s *Service) updateSpec(r *selfRun) scriptrun.Spec {
	return scriptrun.Spec{
		Kind: scriptrun.KindUpdate, Title: "Update " + r.def.Binary, Source: string(r.def.ID),
		Dir: filepath.Dir(r.intent.Path), Argv: r.argv(), Env: r.env, RunsIn: scriptrun.RunsInApp,
		Tool: string(r.def.ID), Intent: r.intent, Callbacks: s.updateCallbacks(r, false),
		Before: func(context.Context) ([]string, error) { return s.beforeUpdate(r) },
	}
}

// updateCallbacks are the callbacks of r, a tool's own update, built alike for the run
// updateSpec starts and, restarted, for one from before the app restarted, which the
// Verifier picks up: the updater's output read as it arrives, and a Finish that checks
// what the updater did (finishUpdate), first waiting, for a run from before the restart,
// for the shell's environment. Only a run this app started is the card's (claimUpdate), so
// only it gets a Recorded, which keeps what it did for the card, and a Released, which
// tells the window once it has let go of the copy's folder.
func (s *Service) updateCallbacks(r *selfRun, restarted bool) scriptrun.Callbacks {
	cb := scriptrun.Callbacks{
		Output: r.reader,
		Finish: func(ctx context.Context, end scriptrun.Ending, out scriptrun.Outcome) scriptrun.Outcome {
			if restarted && !shellenv.Await(ctx, s.env.Ready()) {
				return scriptrun.Outcome{Unsettled: true}
			}

			return s.finishUpdate(ctx, r, end, out)
		},
	}

	if !restarted {
		cb.Recorded = func(_ context.Context, id int64, out scriptrun.Outcome) { s.recordedUpdate(r, id, out) }
		cb.Released = func(int64) { s.releasedUpdate(r) }
	}

	return cb
}

// unclaim forgets an update that never started. One the runner recorded, which failed to
// start, keeps what its record says.
func (s *Service) unclaim(r *selfRun, id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == 0 && s.updates[r.def.ID] == r {
		delete(s.updates, r.def.ID)
	}
}

// startError words an update that couldn't start: the runner's own words, such as a busy
// folder's, or a sentence saying so.
func startError(def Definition, err error) error {
	if _, worded := errors.AsType[*uierr.Error](err); worded {
		return err
	}

	return uierr.New(fmt.Sprintf("%s couldn't start updating itself.", def.Binary), err)
}

// beforeUpdate checks the copy again under its folder's lock, just before its updater
// starts: no run may be using the tool, and the copy must still be one it can replace,
// outside go install's folders as the last look found them.
func (s *Service) beforeUpdate(r *selfRun) ([]string, error) {
	s.mu.Lock()
	bins := s.goBins
	s.mu.Unlock()

	switch {
	case s.inUse(r.def.ID):
		return nil, uierr.New(inUseText(r.def), ErrInstallRefused)
	case !replaceable(r.intent.Path, bins):
		return nil, uierr.New(fmt.Sprintf("%s changed since %s looked for it. Check again, then update it.",
			paths.Tilde(r.intent.Path, s.home), brand.Name), ErrInstallRefused)
	default:
		return r.argv(), nil
	}
}

// finishUpdate checks what the tool's own updater did once it ended, whatever its exit
// status: it looks for the tools again, reads the copy's version from that look, keeps a
// copy the app installed recorded as the app's when its version moved, and says what was
// done. A copy whose version moved is updated, and any other outcome says why not. A
// context that ends first, as the app quits, leaves the run for the next start to settle.
func (s *Service) finishUpdate(ctx context.Context, r *selfRun, end scriptrun.Ending, engine scriptrun.Outcome,
) scriptrun.Outcome {
	r.reader.Flush()
	said := r.reader.snapshot()

	now := s.lookAgain(ctx, r)
	if ctx.Err() != nil {
		return scriptrun.Outcome{Unsettled: true}
	}

	res := updateResult{Tool: r.def.ID, From: r.intent.From}
	if now.Runs {
		res.To = now.Version.Raw
		res.Updated = r.movedTo(now)
	}

	out := judgeUpdate(r.def, end, engine, now, said, &res)

	s.mu.Lock()
	r.settle(res, out)
	s.mu.Unlock()

	s.logger.InfoContext(ctx, "a tool's own update ended", "tool", r.def.Name, "updated", res.Updated,
		"from", res.From, "to", res.To)

	return out
}

// movedTo reports whether c, the copy as it is now, is newer than the one the update
// started from.
func (r *selfRun) movedTo(c Copy) bool {
	return c.Runs && Version{Raw: r.intent.From}.OlderThan(c.Version.Raw)
}

// lookAgain looks for the tools again and returns the updated copy as that look found it,
// recording it as the app's, for a copy the app installed whose version moved, before the
// look matches its copies with the installs: the copy is run for its version once. A copy
// the look didn't find, as one no longer chosen, or a look that failed, is run on its own.
func (s *Service) lookAgain(ctx context.Context, r *selfRun) Copy {
	var (
		now   Copy
		found bool
	)

	err := s.detectThen(ctx, func(got map[string]detection) {
		if now, found = copyAt(got[string(r.def.ID)], r.intent.Path); found {
			s.keepInstalled(ctx, r, now)
		}
	})
	if err != nil && ctx.Err() == nil {
		s.logger.WarnContext(ctx, "a tool updated itself, but couldn't look for the tools again",
			"tool", r.def.Name, "error", err)
	}

	if !found && ctx.Err() == nil {
		now = s.inspectAt(ctx, r.def, r.intent.Path)
		s.keepInstalled(ctx, r, now)
	}

	return now
}

// copyAt returns the copy at path among those d found.
func copyAt(d detection, path string) (Copy, bool) {
	if !d.has {
		return Copy{}, false
	}

	want := paths.Resolved(path)

	for _, c := range append([]Copy{d.selected}, d.others...) {
		if paths.Resolved(c.Path) == want {
			return c, true
		}
	}

	return Copy{}, false
}

// inspectAt runs the copy at path for its version, as detection does.
func (s *Service) inspectAt(ctx context.Context, def Definition, path string) Copy {
	env, _ := s.env.Env()
	d := detector{def: def, env: env, home: s.home, run: s.run}

	return d.inspect(ctx, candidate{path: path, resolved: paths.Resolved(path)})
}

// keepInstalled records the copy a tool's own updater swapped in where the app had
// installed it, once its version moved, so it still reads as installed by the app: the
// record holds its new version and hash, and the archive its updater picks for this Mac.
func (s *Service) keepInstalled(ctx context.Context, r *selfRun, now Copy) {
	if !r.intent.Installed || !r.movedTo(now) {
		return
	}

	sum, ok := s.hashOf(r.intent.Path)
	if !ok {
		s.logger.WarnContext(ctx, "couldn't read a tool that updated itself", "tool", r.def.Name)

		return
	}

	version := now.Version.Raw

	s.record(ctx, r.def, toolinstall.Placed{
		Path: paths.Resolved(r.intent.Path), SHA256: sum, Version: version,
		Archive: r.def.Asset(version, s.goos, s.goarch),
	}, r.intent.From)
}

// recordedUpdate keeps the run's ID once its end is recorded and, for a run that never got
// as far as its Finish, as one that couldn't start, what its record says.
func (s *Service) recordedUpdate(r *selfRun, id int64, out scriptrun.Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()

	r.id = id

	if !r.settled {
		r.settle(updateResult{Tool: r.def.ID, From: r.intent.From, Message: out.Summary}, out)
	}
}

// releasedUpdate marks the run ended once it has let go of the copy's folder, and tells the
// window, so the card shows what it did.
func (s *Service) releasedUpdate(r *selfRun) {
	s.mu.Lock()
	r.running = false

	if !r.settled {
		r.result.Message = fmt.Sprintf("%s's update ended, but %s couldn't record how.", r.def.Binary, brand.Name)
		r.failed = true
	}
	s.mu.Unlock()

	s.events.Emit(event.ToolsUpdated{})
}

// intentOf reads a run's intent back from its record, where it was decoded as plain JSON
// values, with the tool it names; ok is false for one that can't be read, or names no tool
// the app knows.
func (s *Service) intentOf(recorded any) (Definition, updateIntent, bool) {
	in, err := scriptrun.Decode[updateIntent](recorded)
	def, known := s.definition(in.Tool)

	return def, in, err == nil && known && filepath.IsAbs(in.Path)
}

// judgeUpdate words what a tool's own updater did into res, and returns the run's outcome:
// updated when the copy's version moved, whatever the updater said; up to date when it
// exited 0 saying so; and otherwise why not.
func judgeUpdate(def Definition, end scriptrun.Ending, engine scriptrun.Outcome, now Copy, said updateOutput,
	res *updateResult,
) scriptrun.Outcome {
	status := scriptrun.StatusFailed
	still := fmt.Sprintf("%s is still %s", def.Binary, res.From)

	switch code, exited := exitCode(end); {
	case res.Updated:
		status, res.Message = engine.Status, fmt.Sprintf("Updated %s to %s", def.Binary, res.To)
		if engine.Status != scriptrun.StatusSucceeded {
			res.Message += ". " + engine.Summary
		}
	case !now.Runs:
		res.Message = textutil.WithReason(def.Binary+" doesn't run since its update", now.Error)
	case !exited:
		status, res.Message = engine.Status, engine.Summary+" "+textutil.EndSentence(still)
	case code != 0:
		res.Message = textutil.WithReason(def.Binary+" couldn't update itself",
			cmp.Or(said.err, "it exited with status "+strconv.Itoa(code)))
	case said.upToDate:
		status, res.Message = scriptrun.StatusSucceeded, fmt.Sprintf("%s is already up to date (%s)", def.Binary, res.From)
	default:
		res.Message = textutil.WithReason(still, cmp.Or(said.warning, said.last))
	}

	return scriptrun.Outcome{
		Status: status, Summary: textutil.EndSentence(res.Message), Warnings: said.warnings, Detail: *res,
	}
}

// exitCode returns the updater's exit status, and whether it exited on its own: neither
// stopped, nor past its limit, nor cut off as the app quit, nor ended unseen.
func exitCode(end scriptrun.Ending) (int, bool) {
	if end.ExitCode == nil || end.Stopped || end.TimedOut || end.Quit || end.Interrupted != "" {
		return 0, false
	}

	return *end.ExitCode, true
}

// inUseText says that a run is using the tool, which an update waits for.
func inUseText(def Definition) string {
	return fmt.Sprintf("A run is using %s. Update it when the run ends.", def.Binary)
}

// updateOutput is what a tool's own updater said: its warning lines, its last error,
// warning, and line, each without the level or mark it was printed with, and whether it
// said the copy is already up to date.
type updateOutput struct {
	warnings []string
	err      string
	warning  string
	last     string
	upToDate bool
}

// updateReader reads a tool's own updater's output as it arrives, each line as a terminal
// shows it (see scriptrun.LineWriter). The zero value is ready.
type updateReader struct {
	// lines hands r.line each whole line under its lock, which every method takes. It is
	// made on first use (writer).
	linesOnce sync.Once
	lines     *scriptrun.LineWriter

	said updateOutput
}

// Write reads p, which may end anywhere. It never fails.
func (r *updateReader) Write(p []byte) (int, error) {
	return r.writer().Write(p)
}

// Flush reads a last line that has no newline.
func (r *updateReader) Flush() {
	r.writer().Flush()
}

// writer returns the LineWriter that hands r each line, made on first use.
func (r *updateReader) writer() *scriptrun.LineWriter {
	r.linesOnce.Do(func() { r.lines = scriptrun.NewLineWriter(r.line) })

	return r.lines
}

// snapshot returns what was read so far.
func (r *updateReader) snapshot() updateOutput {
	w := r.writer()

	w.Lock()
	defer w.Unlock()

	out := r.said
	out.warnings = append([]string{}, r.said.warnings...)

	return out
}

// line reads one line.
func (r *updateReader) line(text string) {
	level, msg := lineLevel(text)
	if msg == "" {
		return
	}

	r.said.last = msg
	r.said.upToDate = r.said.upToDate || upToDateLine.MatchString(msg)

	switch level {
	case levelWarning:
		r.said.warning = msg

		if len(r.said.warnings) < maxWarnings {
			r.said.warnings = append(r.said.warnings, msg)
		}
	case levelError:
		r.said.err = msg
	case levelInfo:
	}
}

// level is how a tool marked a line of its output.
type level int

// The levels a line can have, least first.
const (
	levelInfo level = iota
	levelWarning
	levelError
)

// logLine matches magex's log line: "15:04:05 [WARN] message".
var logLine = regexp.MustCompile(`^\d{2}:\d{2}:\d{2} \[(DEBUG|INFO|WARN|ERROR|FATAL)\] ?(.*)$`)

// upToDateLine matches the updaters' word that the copy is current: "magex is already up
// to date (1.30.2)", as go-selfupdate says it.
var upToDateLine = regexp.MustCompile(`(?i)\bis (already )?up to date\b`)

// lineLevel reads a line's level and its message without what it was marked with: magex's
// log level, the marks before the message (❌, ⚠️, ✅, and their like), and the "Error:" or
// "warning:" label that both tools' failures and their updater's warnings start with.
func lineLevel(text string) (level, string) {
	lvl, msg := levelInfo, strings.TrimSpace(text)

	if m := logLine.FindStringSubmatch(msg); m != nil {
		lvl, msg = logLevel(m[1]), m[2]
	}

	marked, msg := cutMarks(msg)
	lvl = max(lvl, marked)

	if rest, ok := cutLabel(msg, "error:"); ok {
		return levelError, rest
	}

	if rest, ok := cutLabel(msg, "warning:"); ok {
		return max(lvl, levelWarning), rest
	}

	return lvl, msg
}

// logLevel returns the level of magex's log level tag.
func logLevel(tag string) level {
	switch tag {
	case "WARN":
		return levelWarning
	case "ERROR", "FATAL":
		return levelError
	default:
		return levelInfo
	}
}

// cutMarks returns msg without the marks it starts with, and the highest level they give.
func cutMarks(msg string) (level, string) {
	lvl := levelInfo

	for {
		msg = strings.TrimSpace(msg)

		r, size := utf8.DecodeRuneInString(msg)

		marked, ok := markLevel(r)
		if !ok {
			return lvl, msg
		}

		lvl, msg = max(lvl, marked), msg[size:]
	}
}

// markLevel returns the level a mark at the start of a line gives it, and whether r is
// one: a cross is an error, a warning sign a warning, and a tick, an information sign, or
// the selector that draws a mark as an emoji gives none.
func markLevel(r rune) (level, bool) {
	switch r {
	case '❌', '✗', '✘', '⛔':
		return levelError, true
	case '⚠':
		return levelWarning, true
	case '✅', '✓', '✔', 'ℹ', '\uFE0F':
		return levelInfo, true
	default:
		return levelInfo, false
	}
}

// cutLabel returns msg after label, in any case, and whether msg starts with it.
func cutLabel(msg, label string) (string, bool) {
	if len(msg) < len(label) || !strings.EqualFold(msg[:len(label)], label) {
		return msg, false
	}

	return strings.TrimSpace(msg[len(label):]), true
}
