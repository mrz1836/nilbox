package hooks

import (
	"context"
	"slices"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Verifier settles the hooks actions and the checks runs from before the app restarted
// (scriptrun.Verifier): one orphaned, still running, or interrupted gets an output reader,
// a Finish, a Recorded, and a Released again, built from its record and from what its
// Before kept beside it (scriptrun.Spec.Keep, which the runner removes once the run's end
// is recorded), so it is read and judged as one the app started is. An action's Finish
// reads the hook files against the hooks its Before read, judged with the copy of
// go-pre-commit that ran; a checks run's reads git's status of the clone against the
// snapshot its Before took, with the config it ran with. Core registers it with the runner
// for scriptrun.KindHooks and scriptrun.KindChecks. It is its own type, not a method of the
// Service, which the window binds.
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

	run := &actionRun{action: in.Action, clone: clone{dir: rec.Dir, name: rec.Repo}, confirm: in.Confirm, where: rec.RunsIn}
	reader := newActionReader()

	return scriptrun.Verification{
		Output: reader,
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			if !s.awaitShell(ctx) {
				return scriptrun.Outcome{Unsettled: true}
			}

			run.result = s.finishKept(ctx, rec.ID, run, reader, end)

			return settled(run.cut, actionOutcome(run.result, reader.snapshot(), engine))
		},
		Released: func(int64) { s.markStale(run.clone.dir) },
	}
}

// finishKept reads what an action from before the app restarted did, as its own Finish
// reads it, against the hooks its Before kept, judged with the features of the copy of
// go-pre-commit that ran. Whether the hook it left unchanged is current is asked of that
// copy only while it is still the one in use. An action that kept nothing can't be told,
// and says so.
func (s *Service) finishKept(ctx context.Context, id int64, run *actionRun, reader *actionReader,
	end scriptrun.Ending,
) ActionResult {
	var kept keptAction
	if !s.keptOf(ctx, id, &kept) {
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

	run.before, run.tool = kept.Hooks.hooks(), kept.Tool.facts()

	if now := s.tool(ctx); !now.Usable() || now.key() != run.tool.key() {
		run.stale = true
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

	run := &checksRun{intent: in, clone: clone{dir: rec.Dir, name: rec.Repo}, where: rec.RunsIn}
	reader := newChecksReader(slices.Contains(rec.Argv, jsonFlag))

	return scriptrun.Verification{
		Output: reader,
		Finish: func(ctx context.Context, end scriptrun.Ending, engine scriptrun.Outcome) scriptrun.Outcome {
			if !s.awaitShell(ctx) {
				return scriptrun.Outcome{Unsettled: true}
			}

			s.restoreChecks(ctx, rec.ID, run)

			return settled(run.cut, s.finishChecks(ctx, run, reader, end, engine))
		},
		Recorded: func(ctx context.Context, _ int64, _ scriptrun.Outcome) { s.checksRecorded(ctx) },
		Released: func(int64) { s.events.Emit(event.HooksUpdated{}) },
	}
}

// restoreChecks gives a checks run from before the app restarted the config it ran with
// and the snapshot its Before took, as it kept them. One that kept nothing is judged with
// the config as it is now, and says it can't tell which files the checks changed.
func (s *Service) restoreChecks(ctx context.Context, id int64, run *checksRun) {
	var kept keptChecks
	if !s.keptOf(ctx, id, &kept) {
		run.config = readConfig(run.clone.dir)
		run.before = snapshot{err: uierr.New(brand.Name+" restarted while they ran, without a record of the clone "+
			"from before them.", errNotKept)}

		return
	}

	run.config, run.before = configOf(kept.Checks), kept.Snapshot.snapshot()
}

// keptOf reads what the run id kept beside its record into into, and reports whether it
// kept anything readable; one that can't be read is logged.
func (s *Service) keptOf(ctx context.Context, id int64, into any) bool {
	if s.runs == nil {
		return false
	}

	found, err := s.runs.Kept(id, into)
	if err != nil {
		s.logger.WarnContext(ctx, "couldn't read what a run from before the app restarted kept", "run", id, "error", err)
	}

	return found && err == nil
}

// awaitShell waits, until ctx ends, for the login shell's environment to have been read
// once (shellenv.Await), and reports whether it was: the runs from before a restart are
// picked up as the app starts, alongside that read, and git and go-pre-commit run with the
// environment it finds.
func (s *Service) awaitShell(ctx context.Context) bool {
	return s.ready == nil || shellenv.Await(ctx, s.ready())
}

// cutShort reports whether err, from a step of a Finish, came of ctx ending, as when the app
// quit before the step could learn what it needed: the run is then left Unsettled, recorded
// as going, and the next start judges it again through the Verifier. A step that succeeded
// cuts nothing short, whenever ctx ended.
func cutShort(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil
}

// settled returns out, the outcome a Finish worked out, or an Unsettled outcome when cut,
// one of its steps cut short by the app's quit (cutShort): the next start judges the run
// again, from what its Before kept, which stays until the run's end is recorded.
func settled(cut bool, out scriptrun.Outcome) scriptrun.Outcome {
	if cut {
		return scriptrun.Outcome{Unsettled: true}
	}

	return out
}
