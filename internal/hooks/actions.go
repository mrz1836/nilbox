package hooks

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// installAllSlack is how much longer than a hooks run's own limit Install and update all
// waits for one to end.
const installAllSlack = 30 * time.Second

// ActionRequest is a hooks action the window asks for in one clone.
type ActionRequest struct {
	// Dir is the clone, as the page lists it (Repo.Dir), with ~.
	Dir string `json:"dir"`

	// Confirm is the shared hooks folder the user agreed to change for every repository
	// that uses it, as the page showed it (Repo.HooksDir), or "". An action in a clone
	// whose hooks folder is shared is refused without it.
	Confirm string `json:"confirm"`

	// RunsIn is where it runs, or "" for the setting.
	RunsIn integrationprefs.RunsIn `json:"runsIn"`
}

// ActionOutcome is what a hooks action did, as the hook files say once it ended.
type ActionOutcome string

// The outcomes of a hooks action.
const (
	// OutcomeInstalled is go-pre-commit's hook where there was none.
	OutcomeInstalled ActionOutcome = "installed"

	// OutcomeUpdated is go-pre-commit's hook written over an older one of its own.
	OutcomeUpdated ActionOutcome = "updated"

	// OutcomeCurrent is go-pre-commit's current hook, already there.
	OutcomeCurrent ActionOutcome = "alreadyCurrent"

	// OutcomeReplaced is go-pre-commit's hook in place of another tool's, which it set
	// aside.
	OutcomeReplaced ActionOutcome = "replaced"

	// OutcomeBlocked is nothing installed: another tool's hook is in the way, though
	// go-pre-commit 1.13 exits 0.
	OutcomeBlocked ActionOutcome = "nothingInstalled"

	// OutcomeUninstalled is every hook of go-pre-commit's removed, and the one it
	// replaced put back.
	OutcomeUninstalled ActionOutcome = "uninstalled"

	// OutcomeNoneToRemove is an uninstall that found no hook of go-pre-commit's.
	OutcomeNoneToRemove ActionOutcome = "nothingToUninstall"

	// OutcomeFailed is an action that didn't do what it was asked.
	OutcomeFailed ActionOutcome = "failed"

	// OutcomeUnverified is an action whose result couldn't be read from the hooks.
	OutcomeUnverified ActionOutcome = "unverified"

	// OutcomeSkipped is a clone Install and update all left alone.
	OutcomeSkipped ActionOutcome = "skipped"
)

// succeeded reports an outcome that did what was asked.
func (o ActionOutcome) succeeded() bool {
	return slices.Contains([]ActionOutcome{
		OutcomeInstalled, OutcomeUpdated, OutcomeCurrent, OutcomeReplaced, OutcomeUninstalled, OutcomeNoneToRemove,
	}, o)
}

// ActionResult is a hooks action's result for the window: its run's outcome detail
// (scriptrun.Result.Outcome), read from the hook files once go-pre-commit ended, never
// from its exit status alone.
type ActionResult struct {
	// Action is what was asked, Outcome what the hooks say happened, and Message says so
	// in a sentence.
	Action  Action        `json:"action"`
	Outcome ActionOutcome `json:"outcome"`
	Message string        `json:"message"`

	// Repo is the clone's repository, "owner/name", or ""; Name is what the page calls
	// it; Dir and HooksDir are its folder and its hooks folder, with ~.
	Repo     string `json:"repo"`
	Name     string `json:"name"`
	Dir      string `json:"dir"`
	HooksDir string `json:"hooksDir"`

	// Shared reports a hooks folder other repositories use too, as Repo.Shared does, and
	// SharedWith how many of the clones under the clone root use it, so the window can ask
	// before an action there, as the row does.
	Shared     bool `json:"shared"`
	SharedWith int  `json:"sharedWith"`

	// OtherTool names the other tool whose hook was in the way, replaced, or put back:
	// husky, pre-commit, lefthook, or "" when it can't be told.
	OtherTool string `json:"otherTool"`

	// Backup is the name go-pre-commit gave the hook it replaced, as in
	// pre-commit.backup.64138, and Restored reports an uninstall that put that hook back.
	Backup   string `json:"backup"`
	Restored bool   `json:"restored"`

	// Removed are the hook types an uninstall removed.
	Removed []string `json:"removed"`

	// NotExecutable reports go-pre-commit's hook left without its executable bit.
	NotExecutable bool `json:"notExecutable"`

	// Error is go-pre-commit's last error line, or why the hooks couldn't be read.
	Error string `json:"error"`
}

// ActionIntent is what a hooks action sets out to do, written into its run's result as it
// starts, so Run again does the same.
type ActionIntent struct {
	Action  Action `json:"action"`
	Confirm string `json:"confirm"`
}

// Install installs go-pre-commit's pre-commit hook in the clone, and returns the run's ID.
func (s *Service) Install(ctx context.Context, req ActionRequest) (int64, error) {
	return s.act(ctx, ActionInstall, req)
}

// Update writes go-pre-commit's current hook over an outdated one.
func (s *Service) Update(ctx context.Context, req ActionRequest) (int64, error) {
	return s.act(ctx, ActionUpdate, req)
}

// Reinstall writes go-pre-commit's hook again.
func (s *Service) Reinstall(ctx context.Context, req ActionRequest) (int64, error) {
	return s.act(ctx, ActionReinstall, req)
}

// Replace has go-pre-commit set another tool's hook aside, as pre-commit.backup.<number>,
// and install its own.
func (s *Service) Replace(ctx context.Context, req ActionRequest) (int64, error) {
	return s.act(ctx, ActionReplace, req)
}

// Uninstall has go-pre-commit remove every hook of its own, listing each hook type that
// carries its marker, and put back the hook it replaced.
func (s *Service) Uninstall(ctx context.Context, req ActionRequest) (int64, error) {
	return s.act(ctx, ActionUninstall, req)
}

// act starts action in the clone req names. The runner takes the clone's lock, failing at
// once when the clone is busy, and the run's Before reads the clone's hooks again under
// it: a shared hooks folder the request doesn't confirm, a clone with no config, or a
// go-pre-commit that can't be used stops it there, in words.
func (s *Service) act(ctx context.Context, action Action, req ActionRequest) (int64, error) {
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

	spec, err := s.actionSpec(ctx, action, c, req.Confirm, where, nil)
	if err != nil {
		return 0, err
	}

	id, err := s.runs.Start(ctx, spec)
	if err != nil {
		return id, err
	}

	s.logger.InfoContext(ctx, "a hooks action started", "run", id, "action", string(action), "repo", c.label(s.home),
		"in", string(where))

	return id, nil
}

// actionRun is one hooks action as it goes: what it was asked, and what its Before read.
type actionRun struct {
	action  Action
	clone   clone
	confirm string
	where   integrationprefs.RunsIn

	// kept is what the run keeps beside its record (scriptrun.Spec.Keep), which Before
	// fills in with what it read.
	kept *keptAction

	// tool and before are the copy that runs and the clone's hooks, under the lock, and
	// result what its Finish read from them.
	tool   ToolFacts
	before clonesHooks
	result ActionResult

	// stale reports, for an action from before a restart, that the copy that ran isn't the
	// one in use now, so it can't be asked whether the hook it left is current; cut, that a
	// step of the Finish was cut short by the app's quit (cutShort).
	stale bool
	cut   bool
}

// actionSpec is the run of action in c: go-pre-commit, its output read as it arrives,
// its Before reading the hooks under the clone's lock, and keeping them, with the copy that
// runs, beside the run's record (Keep), and its Finish reading them again once it ended.
// Once the run has let go of the clone, its record kept, the clone is marked to be scanned
// again, which tells the window, and done, when set, hears the result.
func (s *Service) actionSpec(ctx context.Context, action Action, c clone, confirm string,
	where integrationprefs.RunsIn, done func(ActionResult),
) (scriptrun.Spec, error) {
	if !action.Valid() {
		return scriptrun.Spec{}, fmt.Errorf("%w: the action %q", ErrInvalidRequest, action)
	}

	if tool := s.tool(ctx); !tool.Usable() {
		return scriptrun.Spec{}, noToolError(tool)
	}

	run := &actionRun{action: action, clone: c, confirm: confirm, where: where, kept: &keptAction{}}
	base, _ := s.env()

	return scriptrun.Spec{
		Kind: scriptrun.KindHooks, Title: actionTitle(action), Source: string(action), Repo: c.name, Dir: c.dir,
		Env: runEnv(base, where), RunsIn: where, Tool: string(toolID),
		Intent: ActionIntent{Action: action, Confirm: confirm}, Keep: run.kept,
		Callbacks: s.actionCallbacks(run, nil, done),
		Before:    func(ctx context.Context) ([]string, error) { return s.beforeAction(ctx, run) },
	}, nil
}

// actionCallbacks are the callbacks of run, a hooks action, built alike for the run
// actionSpec starts and for one from before the app restarted, whose record, rec, the
// Verifier picks up (nil for a run this app starts): go-pre-commit's output read as it
// arrives; a Finish that reads what the action did from the hook files (finishAction),
// which, for a run from before the restart, first waits for the login shell's environment
// and judges it against the hooks its Before kept (finishKept); and a Released that has
// the page scan the clone again, then tells done, when set, the result.
func (s *Service) actionCallbacks(run *actionRun, rec *scriptrun.Record, done func(ActionResult),
) scriptrun.Callbacks {
	reader := newActionReader()

	return scriptrun.Callbacks{
		Output: reader,
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			switch {
			case rec == nil:
				run.result = s.finishAction(ctx, run, reader, end)
			case !s.awaitShell(ctx):
				return scriptrun.Outcome{Unsettled: true}
			default:
				run.result = s.finishKept(ctx, rec.ID, run, reader, end)
			}

			return settled(run.cut, actionOutcome(run.result, reader.snapshot(), engine))
		},
		Released: func(int64) {
			s.markStale(run.clone.dir)

			if done != nil {
				done(run.result)
			}
		},
	}
}

// beforeAction is a hooks action's Before: under the clone's lock, it reads go-pre-commit
// and the clone's hooks again, refuses a shared hooks folder the request didn't confirm,
// keeps the copy and the hooks beside the run's record, and returns go-pre-commit's argv.
func (s *Service) beforeAction(ctx context.Context, run *actionRun) ([]string, error) {
	tool := s.tool(ctx)
	if !tool.Usable() {
		return nil, noToolError(tool)
	}

	if !readConfig(run.clone.dir).present {
		return nil, notSetUpError(run.clone, s.home)
	}

	l := s.look(ctx, run.clone.dir)
	if l.err != nil {
		return nil, l.err
	}

	if l.shared && !confirmed(run.confirm, l.paths.Hooks, s.home) {
		return nil, s.sharedError(run.action, l.paths.Hooks)
	}

	run.tool, run.before = tool, l
	*run.kept = keptAction{Tool: keepTool(tool), Hooks: keepHooks(l)}

	return actionArgv(run.action, tool.Path, l.marked, run.where), nil
}

// actionArgv is go-pre-commit's command for action: install, with --force to replace
// another tool's hook, or uninstall, naming every hook type that carries its marker. A run
// in the app turns colors off; one in the terminal keeps them.
func actionArgv(action Action, path string, marked []string, where integrationprefs.RunsIn) []string {
	var argv []string

	switch action {
	case ActionReplace:
		argv = []string{path, "install", "--force"}
	case ActionUninstall:
		argv = []string{path, "uninstall"}
	case ActionInstall, ActionUpdate, ActionReinstall:
		argv = []string{path, "install"}
	}

	argv = withColor(argv, where)

	if action == ActionUninstall {
		if len(marked) == 0 {
			marked = []string{hookPreCommit}
		}

		for _, t := range marked {
			argv = append(argv, "--hook-type", t)
		}
	}

	return argv
}

// withColor adds --no-color to argv for a run in the app, which has no terminal to show
// colors in; a run in the terminal keeps them.
func withColor(argv []string, where integrationprefs.RunsIn) []string {
	if where == integrationprefs.RunsInTerminal {
		return argv
	}

	return append(argv, noColor)
}

// finishAction reads, once go-pre-commit ended, what the action did to the hooks.
func (s *Service) finishAction(ctx context.Context, run *actionRun, reader *actionReader, end scriptrun.Ending,
) ActionResult {
	reader.Flush()
	said := reader.snapshot()

	res := ActionResult{
		Action: run.action, Repo: repoText(run.clone.name), Name: display(run.clone.name, run.clone.dir),
		Dir: paths.Tilde(run.clone.dir, s.home), HooksDir: paths.Tilde(run.before.paths.Hooks, s.home),
		Removed: []string{}, Error: said.lastError(),
	}

	if res.Shared = isShared(run.before.paths); res.Shared {
		res.SharedWith = s.sharedCount(paths.Resolved(run.before.paths.Hooks))
	}

	if run.action == ActionUninstall {
		s.judgeUninstall(run, &res)
	} else {
		s.judgeInstall(ctx, run, end, &res)
	}

	res.Message = actionMessage(res)

	return res
}

// judgeInstall works out what an install did from the pre-commit hook before and after:
// installed means go-pre-commit's marker is there and the bytes changed, or the bytes
// stayed and are what this copy writes; another tool's hook left as it was is nothing
// installed, whatever the exit status. With FeatureInstallExitCode, a non-zero exit fails
// it too.
func (s *Service) judgeInstall(ctx context.Context, run *actionRun, end scriptrun.Ending, res *ActionResult) {
	after := readHook(filepath.Join(run.before.paths.Hooks, hookPreCommit))

	s.readInstall(ctx, run, after, res)

	res.NotExecutable = after.ours && !after.executable

	if res.NotExecutable || (res.Outcome.succeeded() && exitFailed(end) && run.tool.Has(integrations.FeatureInstallExitCode)) {
		res.Outcome = OutcomeFailed
	}
}

// readInstall reads what an install did from the pre-commit hook before it and after.
func (s *Service) readInstall(ctx context.Context, run *actionRun, after hookFile, res *ActionResult) {
	before := run.before.hook
	changed := after.present != before.present || after.sha != before.sha

	switch {
	case after.err != nil:
		res.Outcome, res.Error = OutcomeUnverified, after.err.Error()
	case after.ours && changed:
		s.wrote(run, after, res)
	case after.ours:
		var why string

		res.Outcome, why = s.unchanged(ctx, run, after)
		res.Error = cmp.Or(res.Error, why)
	case after.present && !changed:
		res.Outcome, res.OtherTool = OutcomeBlocked, after.tool
	default:
		res.Outcome = OutcomeFailed
	}
}

// exitFailed reports a program that exited with a status other than 0.
func exitFailed(end scriptrun.Ending) bool {
	return end.ExitCode != nil && *end.ExitCode != 0
}

// wrote records what go-pre-commit just wrote, the hook this copy writes, as current, and
// what writing it over the hook before did.
func (s *Service) wrote(run *actionRun, after hookFile, res *ActionResult) {
	before := run.before.hook

	s.judged.set(run.tool.Key(), after.sha, true)

	res.Outcome = installedOutcome(before)

	if res.Outcome == OutcomeReplaced {
		res.OtherTool, res.Backup = before.tool, newBackup(run.before.backups, backups(run.before.paths.Hooks))
	}
}

// unchanged works out an install that left go-pre-commit's hook as it was: already current
// when those bytes are what this copy writes, failed when they aren't, and unverified when
// that can't be told, with why when there's more to say. The copy that ran is asked only
// while it is the one in use; a status the app's quit cut short cuts the Finish short.
func (s *Service) unchanged(ctx context.Context, run *actionRun, after hookFile) (ActionOutcome, string) {
	if run.stale {
		return OutcomeUnverified, "the copy of go-pre-commit that ran isn't the one in use now, so it can't say " +
			"whether the hook it left is current"
	}

	current, known, err := s.judgeBytes(ctx, after, run.tool)
	if cutShort(ctx, err) {
		run.cut = true
	}

	switch {
	case !known:
		return OutcomeUnverified, ""
	case current:
		return OutcomeCurrent, ""
	default:
		return OutcomeFailed, ""
	}
}

// installedOutcome is what writing go-pre-commit's hook over before did.
func installedOutcome(before hookFile) ActionOutcome {
	switch {
	case !before.present:
		return OutcomeInstalled
	case !before.ours:
		return OutcomeReplaced
	default:
		return OutcomeUpdated
	}
}

// newBackup returns the backup that is in after and wasn't in before, the newest when
// there are several, or "".
func newBackup(before, after []string) string {
	added := slices.DeleteFunc(slices.Clone(after), func(b string) bool { return slices.Contains(before, b) })
	if len(added) == 0 {
		return ""
	}

	return added[len(added)-1]
}

// judgeUninstall works out what an uninstall did: every hook type that carried
// go-pre-commit's marker before, and whether another tool's pre-commit hook is back.
func (s *Service) judgeUninstall(run *actionRun, res *ActionResult) {
	if len(run.before.marked) == 0 {
		res.Outcome = OutcomeNoneToRemove

		return
	}

	var left []string

	for _, t := range run.before.marked {
		h := readHook(filepath.Join(run.before.paths.Hooks, t))

		switch {
		case h.err != nil || h.ours:
			left = append(left, t)
		default:
			res.Removed = append(res.Removed, t)

			if t == hookPreCommit && h.present {
				res.Restored, res.OtherTool = true, h.tool
			}
		}
	}

	res.Outcome = OutcomeUninstalled
	if len(left) > 0 {
		res.Outcome = OutcomeFailed
		res.Error = cmp.Or(res.Error, "go-pre-commit left its "+strings.Join(left, " and ")+" hook")
	}
}

// actionOutcome turns a hooks action's result into its run's outcome: succeeded only when
// the hooks show the action done. Anything else failed, unless the user stopped it or its
// end wasn't seen, which the engine's own outcome says.
func actionOutcome(res ActionResult, said actionOutput, engine scriptrun.Outcome) scriptrun.Outcome {
	out := scriptrun.Outcome{
		Status: scriptrun.StatusFailed, Summary: res.Message, Warnings: said.Warnings, Unrecognized: said.Unrecognized,
		UnrecognizedCount: said.UnrecognizedCount, Detail: res,
	}

	switch {
	case res.Outcome.succeeded():
		out.Status = scriptrun.StatusSucceeded
	case engine.Status == scriptrun.StatusStopped || engine.Status == scriptrun.StatusInterrupted:
		out.Status, out.Summary = engine.Status, engine.Summary
	}

	return out
}

// actionTitle is what the Runs list calls a hooks action.
func actionTitle(action Action) string {
	switch action {
	case ActionUpdate:
		return "Update the hook"
	case ActionReinstall:
		return "Reinstall the hook"
	case ActionReplace:
		return "Replace the hook"
	case ActionUninstall:
		return "Uninstall the hook"
	case ActionInstall:
	}

	return "Install the hook"
}

// actionMessage says what a hooks action did, as the page and the run say it.
func actionMessage(res ActionResult) string {
	switch res.Outcome {
	case OutcomeInstalled:
		return "Installed the pre-commit hook in " + res.Name + "."
	case OutcomeUpdated:
		return "Updated the pre-commit hook in " + res.Name + "."
	case OutcomeCurrent:
		return "The pre-commit hook in " + res.Name + " was already current."
	case OutcomeReplaced:
		return replacedMessage(res)
	case OutcomeBlocked:
		return "Nothing installed: " + owner(res.OtherTool) + " hook is in the way."
	case OutcomeUninstalled:
		return uninstalledMessage(res)
	case OutcomeNoneToRemove:
		return "There's no hook of go-pre-commit's to uninstall in " + res.Name + "."
	case OutcomeUnverified:
		return textutil.WithReason("Couldn't read what go-pre-commit did to the hooks in "+res.Name, res.Error)
	case OutcomeFailed, OutcomeSkipped:
	}

	return failedMessage(res)
}

// replacedMessage says another tool's hook was replaced, and where it went.
func replacedMessage(res ActionResult) string {
	text := "Replaced " + owner(res.OtherTool) + " pre-commit hook in " + res.Name + "."
	if res.Backup != "" {
		text += " go-pre-commit kept it as " + res.Backup + ", and puts it back if you uninstall."
	}

	return text
}

// uninstalledMessage says which hooks an uninstall removed, and what it put back.
func uninstalledMessage(res ActionResult) string {
	hooks := " hook"
	if len(res.Removed) > 1 {
		hooks = " hooks"
	}

	text := "Uninstalled the " + strings.Join(res.Removed, " and ") + hooks + " from " + res.Name + "."
	if res.Restored {
		text += " " + textutil.Capitalize(owner(res.OtherTool)) + " pre-commit hook is back."
	}

	return text
}

// failedMessage says why an action failed.
func failedMessage(res ActionResult) string {
	if res.NotExecutable {
		return "go-pre-commit's hook in " + res.Name + " isn't executable, so git won't run it, and go-pre-commit " +
			"can't fix that by writing it again. Uninstall the hook, then install it again."
	}

	verb := map[Action]string{
		ActionInstall: "install", ActionUpdate: "update", ActionReinstall: "reinstall", ActionReplace: "replace",
		ActionUninstall: "uninstall",
	}[res.Action]

	return textutil.WithReason("go-pre-commit couldn't "+verb+" the hook in "+res.Name, res.Error)
}

// owner names whose hook it is: "husky's", or "another tool's".
func owner(tool string) string {
	if tool == "" {
		return "another tool's"
	}

	return tool + "'s"
}

// clone is a clone a request names: its folder, absolute, and its origin's repository on
// GitHub, or the zero Name.
type clone struct {
	dir  string
	name repo.Name
}

// label names the clone for the app's log: its repository, or its folder with ~.
func (c clone) label(home string) string {
	if c.name.Valid() {
		return c.name.String()
	}

	return paths.Tilde(c.dir, home)
}

// folders finds the clone a request names by its folder, as the page lists it, with ~:
// only a clone directly under the clone root is one. Its repository is the one the last
// scan read for it, else its origin's.
func (s *Service) folders() localrepos.ByFolder {
	return localrepos.ByFolder{
		Clones: s.clones, Home: s.home, Invalid: ErrInvalidRequest, NoClone: ErrNoClone, Known: s.scannedName,
	}
}

// cloneAt finds the clone a request names by its folder (see folders).
func (s *Service) cloneAt(ctx context.Context, shown string) (clone, error) {
	l, err := s.folders().List(ctx)
	if err != nil {
		return clone{}, err
	}

	return s.cloneIn(ctx, shown, l)
}

// cloneIn finds the clone shown names among l's (see folders).
func (s *Service) cloneIn(ctx context.Context, shown string, l localrepos.Listing) (clone, error) {
	p, err := s.folders().Pick(ctx, shown, l)
	if err != nil {
		return clone{}, err
	}

	return clone{dir: p.Dir, name: p.Name}, nil
}

// scannedName returns the repository the last scan read for the clone at dir, the zero
// Name for one whose origin isn't on GitHub, and whether the scan listed the clone.
func (s *Service) scannedName(dir string) (repo.Name, bool) {
	key := paths.Resolved(dir)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, r := range s.repos {
		if r.key != key {
			continue
		}

		if r.row.Repo == "" {
			return repo.Name{}, true
		}

		name, err := repo.Parse(r.row.Repo)

		return name, err == nil
	}

	return repo.Name{}, false
}

// where returns where a run goes: the request's choice, or the setting, or the app.
func (s *Service) where(ctx context.Context, chosen integrationprefs.RunsIn) (integrationprefs.RunsIn, error) {
	if chosen != "" {
		return chosen, nil
	}

	if s.places == nil {
		return integrationprefs.RunsInApp, nil
	}

	got, err := s.places.RunsIn(ctx)
	if err != nil {
		return "", fmt.Errorf("read where runs go: %w", err)
	}

	return cmp.Or(got, integrationprefs.RunsInApp), nil
}

// confirmed reports whether the request's confirmation names the hooks folder hooks.
func confirmed(confirm, hooks, home string) bool {
	if confirm == "" {
		return false
	}

	dir, err := paths.ExpandHome(confirm, home)

	return err == nil && paths.Same(dir, hooks)
}

// sharedError refuses action in a shared hooks folder the user didn't confirm, saying how
// many of their clones use it.
func (s *Service) sharedError(action Action, hooks string) error {
	folder := paths.Tilde(hooks, s.home)
	verb := "Installing here installs"

	if action == ActionUninstall {
		verb = "Uninstalling here uninstalls"
	}

	users := "other repositories"
	if n := s.sharedCount(paths.Resolved(hooks)); n > 1 {
		users = fmt.Sprintf("%d of your repositories, and by any other repository that uses it", n)
	}

	return uierr.New(fmt.Sprintf("%s is shared by %s. %s for all of them.", folder, users, verb),
		fmt.Errorf("%w: %s", ErrSharedFolder, hooks))
}

// noToolError says why go-pre-commit can't be used.
func noToolError(tool ToolFacts) error {
	var text string

	switch {
	case tool.Off:
		text = "go-pre-commit is turned off in Settings → Integrations."
	case tool.Path == "" || tool.State == integrations.StateMissing:
		text = "go-pre-commit isn't installed. Install it in Settings → Integrations."
	case tool.State == integrations.StateTooOld:
		text = "This go-pre-commit is too old for " + brand.Name + ". Update it in Settings → Integrations."
	default:
		text = "go-pre-commit won't run. Check it in Settings → Integrations."
	}

	return uierr.New(text, ErrNoTool)
}

// notSetUpError says a clone has no go-pre-commit config.
func notSetUpError(c clone, home string) error {
	return uierr.New(fmt.Sprintf("%s has no go-pre-commit config (.github/env/ or .github/.env.base).", c.label(home)),
		fmt.Errorf("%w: %s", ErrNotSetUp, c.dir))
}

// InstallLine is one clone in Install and update all's result.
type InstallLine struct {
	// Name is what the page calls the clone, and Dir its folder, with ~.
	Name string `json:"name"`
	Dir  string `json:"dir"`

	// Action is what ran, or "" for a clone skipped; RunID is its run, or 0.
	Action Action `json:"action"`
	RunID  int64  `json:"runId"`

	// Outcome is what it did, or OutcomeSkipped, and Message says so, or why it was
	// skipped.
	Outcome ActionOutcome `json:"outcome"`
	Message string        `json:"message"`
}

// InstallAllResult is Install and update all's result: one line per clone, in the order
// asked, and a sentence that sums them up.
type InstallAllResult struct {
	Lines   []InstallLine `json:"lines"`
	Message string        `json:"message"`
}

// InstallAll installs or updates go-pre-commit's hook in each clone of dirs (folders as
// the page lists them, with ~), one at a time, each as its own run in the app, and returns
// a line per clone once all have ended. It never replaces another tool's hook, never
// touches a shared hooks folder, does a folder that two clones resolve to once, and skips
// a clone that is busy, turned off, current already, or without a config.
func (s *Service) InstallAll(ctx context.Context, dirs []string) (InstallAllResult, error) {
	if s.runs == nil {
		return InstallAllResult{}, ErrNotSetUp
	}

	tool := s.tool(ctx)
	if !tool.Usable() {
		return InstallAllResult{}, noToolError(tool)
	}

	out := InstallAllResult{Lines: []InstallLine{}}
	if len(dirs) == 0 {
		out.Message = installAllMessage(out.Lines)

		return out, nil
	}

	// The clone root and the clones under it are read once for every clone asked.
	l, err := s.folders().List(ctx)
	if err != nil {
		return InstallAllResult{}, err
	}

	done := map[string]string{} // hooks folders already done, and the clone that did each
	seen := map[string]bool{}

	for _, shown := range dirs {
		if seen[shown] {
			continue
		}

		seen[shown] = true

		out.Lines = append(out.Lines, s.installOne(ctx, shown, l, tool, done))
	}

	out.Message = installAllMessage(out.Lines)

	return out, nil
}

// installOne installs or updates the hook in the clone shown, unless it is one to skip,
// and waits for the run to end.
func (s *Service) installOne(ctx context.Context, shown string, l localrepos.Listing, tool ToolFacts, done map[string]string,
) InstallLine {
	line := InstallLine{Name: filepath.Base(shown), Dir: shown, Outcome: OutcomeSkipped}

	c, err := s.cloneIn(ctx, shown, l)
	if err != nil {
		line.Message = uierr.Message(err)

		return line
	}

	line.Name, line.Dir = display(c.name, c.dir), paths.Tilde(c.dir, s.home)

	action, hooks, why := s.planInstall(ctx, c, tool, done)
	if action == "" {
		line.Message = why

		return line
	}

	results := make(chan ActionResult, 1)

	spec, err := s.actionSpec(ctx, action, c, "", integrationprefs.RunsInApp, func(r ActionResult) { results <- r })
	if err == nil {
		line.RunID, err = s.runs.Start(ctx, spec)
	}

	switch {
	case errors.Is(err, clonelock.ErrBusy):
		line.Message = uierr.Message(err)

		return line
	case err != nil:
		line.Action, line.Outcome, line.Message = action, OutcomeFailed, uierr.Message(err)

		return line
	}

	line.Action = action
	done[hooks] = line.Name

	select {
	case r := <-results:
		line.Outcome, line.Message = r.Outcome, r.Message
	case <-ctx.Done():
		line.Outcome, line.Message = OutcomeUnverified, "Stopped waiting for it to end."
	case <-time.After(scriptrun.KindHooks.Timeout() + installAllSlack):
		line.Outcome, line.Message = OutcomeUnverified, "It didn't end in time."
	}

	return line
}

// planInstall works out what Install and update all does in c: install, update, or
// reinstall, and in which hooks folder, or why it skips it.
func (s *Service) planInstall(ctx context.Context, c clone, tool ToolFacts, done map[string]string,
) (action Action, hooks, why string) {
	l := s.look(ctx, c.dir)
	if why = s.skipReason(c, l); why != "" {
		return "", "", why
	}

	hooks = paths.Resolved(l.paths.Hooks)
	if by, ok := done[hooks]; ok {
		return "", "", "It shares its hooks folder with " + by + ", done just now."
	}

	switch state, _ := s.judge(ctx, l.hook, tool); state {
	case StateMissing:
		return ActionInstall, hooks, ""
	case StateOutdated:
		return ActionUpdate, hooks, ""
	case StateNotExecutable:
		return ActionReinstall, hooks, ""
	case StateUnknown, StateUnreadable:
		return "", "", "go-pre-commit couldn't say whether its hook is current."
	case StateReady, StateOther:
	}

	return "", "", "Its hook is current already."
}

// skipReason says why Install and update all leaves c, whose hooks are l, as it is: no
// config, turned off, hooks it can't read, a shared hooks folder, or another tool's hook;
// "" when nothing does.
func (s *Service) skipReason(c clone, l clonesHooks) string {
	cfg := readConfig(c.dir)

	switch {
	case !cfg.present:
		return "It has no go-pre-commit config."
	case !cfg.enabled():
		return "go-pre-commit is turned off in this repository."
	case l.err != nil:
		return uierr.Message(l.err)
	case l.shared:
		return "Its hooks folder, " + paths.Tilde(l.paths.Hooks, s.home) + ", is shared, so it's left as it is."
	case l.hook.err != nil:
		return uierr.Message(uierr.New("Couldn't read its pre-commit hook.", l.hook.err))
	case l.hook.present && !l.hook.ours:
		return textutil.Capitalize(owner(l.hook.tool)) + " hook is there, so it's left as it is."
	default:
		return ""
	}
}

// installAllMessage sums up Install and update all.
func installAllMessage(lines []InstallLine) string {
	count := map[ActionOutcome]int{}
	for _, l := range lines {
		count[l.Outcome]++
	}

	var parts []string

	for _, p := range []struct {
		outcome ActionOutcome
		word    string
	}{
		{OutcomeInstalled, "installed"},
		{OutcomeUpdated, "updated"},
		{OutcomeCurrent, "already current"},
		{OutcomeBlocked, "blocked by another tool's hook"},
		{OutcomeFailed, "failed"},
		{OutcomeUnverified, "unverified"},
		{OutcomeSkipped, "skipped"},
	} {
		if n := count[p.outcome]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, p.word))
		}
	}

	if len(parts) == 0 {
		return "Nothing to install or update."
	}

	return textutil.Capitalize(strings.Join(parts, ", ")) + "."
}
