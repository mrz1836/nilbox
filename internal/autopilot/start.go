package autopilot

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// What holds an agent's start back beyond the engine's own limits, and what the user is
// told while a match waits for them:
//
//   - this Mac's readiness for the routine (ready.go), read from the last checks in the
//     cycle, never waiting on one: a need not checked yet waits for the check, and one
//     that isn't met waits as blocked: not_ready;
//   - an outside contributor's item, which an agent runs on only where its org or its
//     repository allows it (OutsideRules), in plan mode even then;
//   - right before the start, on a worker: the step's prompt, which must still be in the
//     library and fit the item (promptHold), then what the app's other parts say (Holds:
//     the item's own agent and profile, the agent's provider, a clone, the repository's
//     tools), and only then the read of the item from GitHub, so a start held back for
//     something on this Mac spends no point;
//   - at the start, the launcher's own holds, which the runner reports back (Result.Wait):
//     a file hold or a dirty worktree, which only the user's Run confirms (Hold), no clone,
//     and a session working where it would start.
//
// A match that comes to wait for the user is told once, as a Needs your OK notice.

// OutsideRules says where the user lets Autopilot run agents on outside contributors'
// items: the launch defaults' choice, per org or repository. Core adapts them.
type OutsideRules interface {
	Outside(ctx context.Context) (OutsideIndex, error)
}

// OutsideIndex answers, for a repository "owner/name", whether Autopilot may run agents,
// in plan mode, on its outside contributors' items, and where that was chosen ("repo",
// "org", or "" when nothing chose, which is off).
type OutsideIndex interface {
	Allowed(repo string) (bool, string)
}

// noOutside allows outside contributors' items nowhere.
type noOutside struct{}

func (noOutside) Allowed(string) (bool, string) { return false, "" }

// outsideIndex reads where agents may run on outside contributors' items, once a cycle; one
// that can't be read allows them nowhere, logged.
func (e *Engine) outsideIndex(ctx context.Context) OutsideIndex {
	if e.outside == nil {
		return noOutside{}
	}

	idx, err := e.outside.Outside(ctx)
	if err != nil || idx == nil {
		if err != nil {
			e.logger.WarnContext(ctx, "Autopilot could not read where agents may run on outside contributors' items",
				"error", err)
		}

		return noOutside{}
	}

	return idx
}

// readyFor checks this Mac's readiness for r's agent step, from the last checks: it waits
// for a check none has made yet, and as not ready for a need that isn't met, naming it.
func (p *pass) readyFor(r *routineRun) verdict {
	if !p.ready.on {
		return verdict{}
	}

	rr, checked := p.ready.routineReady(r.id(), r.row.Name, r.spec, true)

	switch {
	case !checked:
		p.e.svc.ready.markDue()

		return wait(BlockStarting, "Checking what this Mac has first.")
	case !rr.Ready:
		return wait(BlockNotReady, "Not ready on this Mac: "+rr.Problems[0].Words())
	}

	return verdict{}
}

// checkReady runs the readiness checks on a worker when they are stale, for the enabled
// routines this cycle runs, and wakes the loop once they are done.
func (e *Engine) checkReady(ctx context.Context, p *pass) {
	if !e.svc.ready.stale(p.now) || !e.svc.ready.begin() {
		return
	}

	specs := make([]spec.Spec, 0, len(p.routines))
	for _, r := range p.routines {
		specs = append(specs, r.spec)
	}

	now := p.now

	e.goWork(ctx, func(ctx context.Context) {
		if err := e.svc.ready.run(ctx, specs, false, now); err != nil && ctx.Err() == nil {
			e.logger.InfoContext(ctx, "Autopilot could not check what this Mac has", "error", err)
		}

		e.mail.post(func(context.Context) { e.Wake() })
	})
}

// readinessProblems returns a problem for each need of an enabled routine that this Mac
// doesn't meet, as the last checks found it, keyed by the routine and the need.
func (p *pass) readinessProblems() []Problem {
	found := []Problem{}

	if !p.ready.on {
		return found
	}

	for _, r := range p.routines {
		rr, checked := p.ready.routineReady(r.id(), r.row.Name, r.spec, true)
		if !checked {
			continue
		}

		for _, c := range rr.Problems {
			found = append(found, Problem{
				Key: problemKey(r.id(), ProblemNotReady, c.ID), RoutineID: r.id(), Kind: ProblemNotReady,
				Detail: "Not ready on this Mac: " + c.Words(), Since: p.now.UTC(),
			})
		}
	}

	return found
}

// promptHold checks, right before a start, that the prompt job's agent step names, or each
// reply its GitHub step comments, is still in the library and fits what the routine acts
// on, as Save checked it then: a prompt deleted, made a reply, or narrowed to the other
// kind since keeps the run from starting, and a reply deleted or made an approval's since
// keeps the action from going, and says so. A library that can't be read holds nothing;
// the launch itself still finds no prompt that isn't there, and the send refuses a reply
// that isn't a comment's.
func (e *Engine) promptHold(ctx context.Context, sp spec.Spec, st spec.Step) (Blocker, string) {
	agent := st.Agent != nil && !st.Agent.Prompt.ItemDefault
	if e.svc.library == nil || (!agent && !repliesIn(st)) {
		return "", ""
	}

	library, err := e.svc.library.Prompts(ctx)
	if err != nil {
		e.logger.InfoContext(ctx, "Autopilot could not read the prompt library before a start", "error", err)

		return "", ""
	}

	problems := stepPromptProblems(library, st, actsOn(sp), "")

	switch {
	case len(problems) == 0:
		return "", ""
	case agent:
		return BlockNotReady, textutil.Sentence("Its prompt can't run: " + problems[0].Message)
	default:
		return BlockNotReady, textutil.Sentence("Its reply can't be sent: " + problems[0].Message)
	}
}

// repliesIn reports a GitHub step that comments a reply.
func repliesIn(st spec.Step) bool {
	for _, a := range st.GitHub {
		if a.Reply != nil {
			return true
		}
	}

	return false
}

// noteWait remembers m when its change makes it wait for the user, so the cycle's end
// tells them (tellWaits). A Dry run never waits.
func (e *Engine) noteWait(m *match) {
	if e.needsYou == nil || m.DryRun || m.state() != StateWaiting {
		return
	}

	if m.orig.State == m.State && m.orig.BlockedBy == m.BlockedBy {
		return
	}

	e.waits[m.ID] = true
}

// tellWaits tells, on a worker, of each match that came to wait for the user since the
// last cycle ended and still does, once per match, wait, and try, whatever happens to the
// engine: the notice's key names them. Several at once are gathered into one banner past
// three.
func (e *Engine) tellWaits(ctx context.Context) {
	if len(e.waits) == 0 {
		return
	}

	ids := slices.Sorted(maps.Keys(e.waits))
	clear(e.waits)

	e.goWork(ctx, func(ctx context.Context) {
		notices := make([]Notice, 0, len(ids))

		for _, id := range ids {
			if n, ok := e.waitNotice(ctx, id); ok {
				notices = append(notices, n)
			}
		}

		for _, n := range notices {
			n.Burst = len(notices) > 1
			e.tellWait(ctx, n)
		}
	})
}

// tellWait tells one Needs your OK notice, logging one that couldn't be told.
func (e *Engine) tellWait(ctx context.Context, n Notice) {
	if err := e.needsYou.Notice(ctx, n); err != nil && ctx.Err() == nil {
		e.logger.InfoContext(ctx, "Autopilot could not tell that a match waits for you", "match", n.MatchID,
			"error", err)
	}
}

// waitNotice is the Needs your OK notice of the match id, while it still waits for the
// user, headed by its item and naming its routine.
func (e *Engine) waitNotice(ctx context.Context, id int64) (Notice, bool) {
	m, err := e.ledger.get(ctx, id)
	if err != nil || m.state() != StateWaiting || m.DryRun {
		return Notice{}, false
	}

	n := Notice{
		Kind: NoticeNeedsYou, RoutineID: m.RoutineID, MatchID: m.ID, CreatedAt: m.CreatedAt, Item: new(matchItem(m)),
		Title: m.Title,
		Key: "needs:" + strconv.FormatInt(m.ID, 10) + ":" + strconv.FormatInt(m.CreatedAt.Unix(), 10) + ":" +
			m.BlockedBy + ":" + strconv.FormatInt(m.Attempt, 10),
	}

	name := "A routine"

	if row, getErr := e.svc.store.get(ctx, e.svc.store.q, m.RoutineID); getErr == nil {
		name, n.Routine = row.Name, row.Name
		if l := load(row.Spec); l.broken == nil {
			n.Banner = problemBanner(l.spec.Notify)
		}
	}

	n.Body = textutil.OneLine(name + ": " + firstOf(m.Detail, "Waiting for your OK."))

	return n, true
}

// problemBanner is a routine's own choice of banner for what needs the user, over Settings
// → Notifications': its Needs your OK switch, nil to follow the settings.
func problemBanner(n *spec.Notices) *bool {
	if n == nil {
		return nil
	}

	return n.NeedsYou
}

// matchItem is m's item, as the ledger keeps a copy of it.
func matchItem(m *match) ItemRef {
	return ItemRef{
		ID: store.Deref(m.ItemID), Kind: item.Kind(m.Kind), Repo: m.RepoOwner + "/" + m.RepoName,
		Number: int(m.Number), Title: m.Title, URL: m.URL,
	}
}

// outsideWords says where to let Autopilot run agents on an outside contributor's items,
// for the match such an item closed: in the item's org's launch defaults.
func outsideWords(repo string) string {
	owner, _, _ := strings.Cut(repo, "/")

	return fmt.Sprintf("Skipped: an outside contributor's item. Autopilot can act on these in %s's launch defaults.",
		firstOf(owner, "its org"))
}

// TriggerWords names a routine's trigger as the builder does, "Checks fail", for an
// agent's prompt (.Trigger).
func TriggerWords(e catalog.Event) string {
	if t, ok := catalog.TriggerByEvent(e); ok {
		return t.Label
	}

	return string(e)
}

// heldAt puts m back as its runner reported its launch held: the step not started, its
// run taken back, and the match waiting on what held it, or back in the queue to be
// decided again. A hold only the user confirms takes back an OK given before it, so
// only their Run on exactly what it shows lets it through.
func (e *Engine) heldAt(m *match, sr *stepRun, res Result) {
	sr.Status, sr.RunID, sr.Started, sr.Hold = stepPending, 0, nil, res.Hold

	m.unstarted()

	if res.Requeue || res.Wait == "" {
		m.set(StateQueued, "", "")

		return
	}

	m.wait(res.Wait, res.WaitWords)

	if m.state() == StateWaiting {
		m.Approved = false
	}
}

// recordLaunch records, through q, what the launch of the run runID started, when it
// started one.
func (e *Engine) recordLaunch(ctx context.Context, q *queries.Queries, runID int64, l *Launched, now time.Time) error {
	if l == nil || runID == 0 {
		return nil
	}

	err := q.RecordRunLaunch(ctx, queries.RecordRunLaunchParams{
		SessionID: l.SessionID, Agent: l.Agent, Profile: l.Profile, Model: l.Model, Effort: l.Effort,
		ClonePath: l.ClonePath, Worktree: l.Worktree, DisplayCommand: l.Command, UpdatedAt: now, ID: runID,
	})
	if err != nil {
		return fmt.Errorf("record what run %d started: %w", runID, err)
	}

	return nil
}
