package hooks

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"slices"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	// errNotKept is a run from before the app restarted whose record doesn't keep what its
	// Before read, so what it changed can't be told.
	errNotKept = errors.New("what the run's Before read wasn't kept")

	// errNoStatus is a checks run from before the app restarted whose Before couldn't read
	// git's status of the clone, as its record keeps.
	errNoStatus = errors.New("git's status of the clone couldn't be read before the run")
)

// Verifier settles the hooks actions and the checks runs from before the app restarted
// (scriptrun.Verifier): one orphaned, still running, or interrupted gets an output reader,
// a Finish, a Recorded, and a Released again, built from its record, so it is read and
// judged as one the app started is. An action's Finish reads the hook files against the
// hooks its Before read, and a checks run's reads git's status of the clone against the
// snapshot its Before took, both kept in the run's intent. Core registers it with the
// runner for scriptrun.KindHooks and scriptrun.KindChecks. It is its own type, not a method
// of the Service, which the window binds.
type Verifier struct {
	s *Service
}

// NewVerifier returns the Verifier of s's runs.
func NewVerifier(s *Service) Verifier {
	return Verifier{s: s}
}

// Verify gives a hooks action or a checks run from before the app restarted what its Spec
// gave it; its Finish first waits for the login shell's environment (Config.Ready). A
// record whose intent can't be read gets none: the runner's own outcome stands.
func (v Verifier) Verify(run scriptrun.Record) scriptrun.Verification {
	switch run.Kind {
	case scriptrun.KindHooks:
		return v.s.verifyAction(run)
	case scriptrun.KindChecks:
		return v.s.verifyChecks(run)
	case scriptrun.KindRelease, scriptrun.KindScript, scriptrun.KindUpdate:
	}

	return scriptrun.Verification{}
}

// verifyAction is Verify for a hooks action: its Finish judges it from the hooks its Before
// kept, and its Released has the page scan the clone again, as an action's Spec does.
func (s *Service) verifyAction(rec scriptrun.Record) scriptrun.Verification {
	var in ActionIntent
	if err := decodeIntent(rec.Result.Intent, &in); err != nil || !in.Action.Valid() {
		s.logger.Warn("a hooks action from before the app restarted can't be checked", "run", rec.ID)

		return scriptrun.Verification{}
	}

	run := &actionRun{
		action: in.Action, clone: clone{dir: rec.Dir, name: rec.Repo}, confirm: in.Confirm, where: rec.RunsIn, intent: &in,
	}
	reader := newActionReader()

	return scriptrun.Verification{
		Output: reader,
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			if !s.awaitShell(ctx) {
				return scriptrun.Outcome{Unsettled: true}
			}

			run.result = s.finishKept(ctx, run, reader, end)

			return settled(ctx, actionOutcome(run.result, reader.snapshot(), engine))
		},
		Released: func(int64) { s.markStale(run.clone.dir) },
	}
}

// finishKept reads what an action from before the app restarted did, as its own Finish
// reads it, against the hooks its Before kept, judged by the copy of go-pre-commit that ran
// when it is still the one in use. One whose record keeps no hooks can't be told, and
// says so.
func (s *Service) finishKept(ctx context.Context, run *actionRun, reader *actionReader, end scriptrun.Ending,
) ActionResult {
	kept := run.intent.Before
	if kept == nil {
		reader.Flush()

		res := ActionResult{
			Action: run.action, Outcome: OutcomeUnverified, Repo: repoText(run.clone.name),
			Name: display(run.clone.name, run.clone.dir), Dir: paths.Tilde(run.clone.dir, s.home), Removed: []string{},
			Error: uierr.Message(uierr.New(brand.Name+" restarted while it ran, without a record of the hooks from before it.",
				errNotKept)),
		}
		res.Message = actionMessage(res)

		return res
	}

	run.before = kept.hooks()

	if tool := s.tool(ctx); tool.key() == kept.Tool {
		run.tool = tool
	}

	return s.finishAction(ctx, run, reader, end)
}

// verifyChecks is Verify for a checks run: its Finish compares the clone now with the
// snapshot its Before kept, its Recorded notes a checks run ended, and its Released tells the
// window, as a checks run's Spec does. A record that ran go-pre-commit with --json is read as
// JSON.
func (s *Service) verifyChecks(rec scriptrun.Record) scriptrun.Verification {
	var in ChecksIntent
	if err := decodeIntent(rec.Result.Intent, &in); err != nil || checkIntent(in) != nil {
		s.logger.Warn("a checks run from before the app restarted can't be checked", "run", rec.ID)

		return scriptrun.Verification{}
	}

	run := &checksRun{intent: &in, clone: clone{dir: rec.Dir, name: rec.Repo}, where: rec.RunsIn}
	reader := newChecksReader(slices.Contains(rec.Argv, jsonFlag))

	return scriptrun.Verification{
		Output: reader,
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			if !s.awaitShell(ctx) {
				return scriptrun.Outcome{Unsettled: true}
			}

			run.config, run.before = readConfig(run.clone.dir), in.Before.snapshot()

			return settled(ctx, s.finishChecks(ctx, run, reader, end, engine))
		},
		Recorded: func(ctx context.Context, _ int64, _ scriptrun.Outcome) { s.checksRecorded(ctx) },
		Released: func(int64) { s.events.Emit(event.HooksUpdated{}) },
	}
}

// awaitShell waits, until ctx ends, for the login shell's environment to have been read
// once, and reports whether it was: the runs from before a restart are picked up as the app
// starts, alongside that read, and git and go-pre-commit run with the environment it finds.
func (s *Service) awaitShell(ctx context.Context) bool {
	if s.ready == nil {
		return true
	}

	select {
	case <-s.ready():
		return true
	case <-ctx.Done():
		return false
	}
}

// settled returns out, unless ctx ended first, as when the app quit before a Finish could
// read all it needed: the run is then left Unsettled, recorded as going, and the next start
// judges it again through the Verifier.
func settled(ctx context.Context, out scriptrun.Outcome) scriptrun.Outcome {
	if ctx.Err() != nil {
		return scriptrun.Outcome{Unsettled: true}
	}

	return out
}

// keptHooks is a clone's hooks as a hooks action's Before read them, under the clone's
// lock, kept in its intent: where git ran the hooks from, the pre-commit hook, the hook
// types that carried go-pre-commit's marker, and the hooks it had set aside, with the copy
// of go-pre-commit that ran, by its path and version.
type keptHooks struct {
	Tool string `json:"tool"`

	Toplevel  string `json:"toplevel"`
	Hooks     string `json:"hooks"`
	CommonDir string `json:"commonDir"`

	// Present, Ours, SHA, and OtherTool are the pre-commit hook's: there, go-pre-commit's,
	// its contents' SHA-256, and the other tool whose hook it is.
	Present   bool   `json:"present"`
	Ours      bool   `json:"ours"`
	SHA       string `json:"sha"`
	OtherTool string `json:"otherTool"`

	Marked  []string `json:"marked"`
	Backups []string `json:"backups"`
}

// keepHooks keeps the hooks l, which tool is about to change.
func keepHooks(tool ToolFacts, l clonesHooks) *keptHooks {
	return &keptHooks{
		Tool: tool.key(), Toplevel: l.paths.Toplevel, Hooks: l.paths.Hooks, CommonDir: l.paths.CommonDir,
		Present: l.hook.present, Ours: l.hook.ours, SHA: l.hook.sha, OtherTool: l.hook.tool,
		Marked: textutil.NonNil(l.marked), Backups: textutil.NonNil(l.backups),
	}
}

// hooks returns the hooks k keeps, as the action's Before read them.
func (k *keptHooks) hooks() clonesHooks {
	hp := gitread.HookPaths{Toplevel: k.Toplevel, Hooks: k.Hooks, CommonDir: k.CommonDir}

	return clonesHooks{
		paths: hp, shared: isShared(hp),
		hook: hookFile{
			path: filepath.Join(k.Hooks, hookPreCommit), present: k.Present, ours: k.Ours, sha: k.SHA, tool: k.OtherTool,
		},
		marked: textutil.NonNil(k.Marked), backups: textutil.NonNil(k.Backups),
	}
}

// keptSnapshot is a clone's snapshot as a checks run's Before took it, kept in its intent:
// each dirty file's status, what the index holds for it, and its contents' SHA-256, or why
// git's status couldn't be read. Only a snapshot that hashed its files' contents is kept,
// so it holds at most maxHashed of them.
type keptSnapshot struct {
	Error string      `json:"error,omitempty"`
	Files []keptEntry `json:"files"`
}

// keptEntry is one dirty file in a keptSnapshot.
type keptEntry struct {
	Path  string `json:"path"`
	Code  string `json:"code"`
	Index string `json:"index,omitempty"`
	Sum   string `json:"sum"`
}

// keepSnapshot keeps snap, or returns nil for one with too many files to have hashed them.
func keepSnapshot(snap snapshot) *keptSnapshot {
	switch {
	case snap.err != nil:
		return &keptSnapshot{Error: uierr.Message(snap.err), Files: []keptEntry{}}
	case !snap.hashed:
		return nil
	}

	files := make([]keptEntry, 0, len(snap.entries))

	for _, p := range slices.Sorted(maps.Keys(snap.entries)) {
		e := snap.entries[p]
		files = append(files, keptEntry{Path: p, Code: e.Code, Index: e.Index, Sum: snap.sums[p]})
	}

	return &keptSnapshot{Files: files}
}

// snapshot returns the snapshot k keeps, or, with none kept, one that says so.
func (k *keptSnapshot) snapshot() snapshot {
	switch {
	case k == nil:
		return snapshot{err: uierr.New(brand.Name+" restarted while they ran, without a record of the clone from before them.",
			errNotKept)}
	case k.Error != "":
		return snapshot{err: uierr.New(k.Error, errNoStatus)}
	}

	snap := snapshot{
		entries: make(map[string]gitread.Entry, len(k.Files)), sums: make(map[string]string, len(k.Files)), hashed: true,
	}

	for _, f := range k.Files {
		snap.entries[f.Path] = gitread.Entry{Code: f.Code, Index: f.Index, Path: f.Path}
		snap.sums[f.Path] = f.Sum
	}

	return snap
}
