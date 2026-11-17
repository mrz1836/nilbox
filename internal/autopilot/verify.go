package autopilot

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Wait for the checks closes the loop on an agent's push. Once the run ends, its match is
// verifying: the engine judges it on every cycle from what the inbox stored of its item,
// as the checks follower, a refresh, or the engine's own read last brought it, and asks
// GitHub nothing more than it must.
//
//   - The run's commit is what it pushed (its report's pushedCommit, which must be the
//     worktree's HEAD), or, when its report says nothing, its worktree's HEAD once the pull
//     request's head is that commit.
//   - While the pull request's head is still the head the run started from, its push
//     hasn't been seen: the item is read again, at most every verifyReadAfter.
//   - Once the head is the run's commit, the checks on it decide. While one runs, the
//     checks follower reads it every round (a routine with this step follows the checks of
//     what its searches return), and the engine reads it itself only when nothing did for
//     verifyReadAfter. Once none runs, the check names are read, and once the last check
//     finished as long ago as the step's settle time, the verdict is in.
//   - A head that moved to another commit is someone else's push: the run's fix wasn't
//     checked (overtaken), and the routine sees a new head, as for any new commit.
//   - A run that pushed nothing says so (no push), unless it made a commit while the
//     branch moved, which it never force-pushes over (push rejected).
//   - After the step's give-up time with no verdict, it is unknown.
//
// The verdict is recorded on the step (its Only if outcome: passed or stillFails) and on
// the agent's run, the match moves on to its next step, and the user is told, as Checks
// after a fix.

// Verdict is what Wait for the checks found of an agent's run.
type Verdict string

// The verdicts.
const (
	// VerdictFixed is every check passing on the run's commit.
	VerdictFixed Verdict = "fixed"

	// VerdictFailing is a check failing on it.
	VerdictFailing Verdict = "still_failing"

	// VerdictUnknown is checks that didn't finish, or never ran, within the step's
	// give-up time, or that wait for a maintainer to approve them.
	VerdictUnknown Verdict = "unknown"

	// VerdictNoPush is a run that pushed nothing.
	VerdictNoPush Verdict = "no_push"

	// VerdictRejected is a run whose push was refused because the branch moved while it
	// worked, which is safe: it never force-pushes.
	VerdictRejected Verdict = "push_rejected"

	// VerdictOvertaken is someone else's push after the run's commit, so its fix wasn't
	// checked.
	VerdictOvertaken Verdict = "overtaken"
)

// Verdicts returns every verdict.
func Verdicts() []Verdict {
	return []Verdict{VerdictFixed, VerdictFailing, VerdictUnknown, VerdictNoPush, VerdictRejected, VerdictOvertaken}
}

// Timing of verify's own reads.
const (
	// verifyReadAfter is how old the item's details may be before verify reads it again
	// itself, while the run's push isn't seen, its checks run where no follower reads
	// them, or no check has appeared yet: about a point every two minutes, only then.
	verifyReadAfter = 2 * time.Minute

	// noChecksAfter is how long after the run's commit was seen verify waits for a check
	// to appear on it before it calls the checks unknown.
	noChecksAfter = 10 * time.Minute

	// verifyNamesAfter is how often verify asks for the check names of a commit whose
	// checks all finished, while the names it has were read before they had.
	verifyNamesAfter = time.Minute
)

// maxNamedChecks is how many failed checks a verdict names before it counts the rest.
const maxNamedChecks = 3

// verifyRun is what the verdict reads of the agent's run.
type verifyRun struct {
	// Commit is what the run pushed, "" for none; HeadBefore the head it started from;
	// and HeadAfter its worktree's commit as it ended.
	Commit     string
	HeadBefore string
	HeadAfter  string

	// Summary is its end in a sentence, whose words say why it didn't push.
	Summary string

	// EndedAt is when it ended.
	EndedAt time.Time
}

// verifyCase is what one judgment of a verifying match reads.
type verifyCase struct {
	run  verifyRun
	wait spec.WaitStep

	// facts are the item's, as the cycle read them; known is false for an item not stored.
	facts facts.Facts
	known bool

	// started is when the match began verifying, and seen when the run's commit was first
	// seen as the pull request's head, nil before.
	started time.Time
	seen    *time.Time

	now time.Time
}

// verifyResult is what one judgment came to: a verdict with its words and its Only if
// outcome, or "" while it waits, with what to read meanwhile.
type verifyResult struct {
	verdict Verdict
	words   string
	outcome catalog.Outcome

	// commit is the run's commit as the verdict counts it, and seen reports it as the
	// pull request's head now.
	commit string
	seen   bool

	// read asks for the item's details again, and names for its head's check names.
	read  bool
	names bool

	// failed names the checks that failed, for a verdict that still fails.
	failed []string
}

// waiting reports a judgment that came to no verdict yet.
func (r verifyResult) waiting() bool { return r.verdict == "" }

// judgeVerify works out a verifying match's verdict, or what it waits on. It is pure.
func judgeVerify(c verifyCase) verifyResult {
	head := c.head()

	commit := c.commit(head)
	if commit == "" {
		return judgeNoPush(c, head)
	}

	switch {
	case head == "":
		return c.orGiveUp(verifyResult{commit: commit, read: true},
			"Unknown: its pull request couldn't be read to see its push of "+short(commit)+".")
	case sameOID(head, commit):
		return judgeChecks(c, commit)
	case sameOID(head, c.run.HeadBefore) && c.seen == nil:
		return c.orGiveUp(verifyResult{commit: commit, read: c.stale()},
			"Unknown: its push of "+short(commit)+" wasn't seen on GitHub within "+hoursWords(c.wait.GiveUp())+".")
	}

	return verifyResult{
		verdict: VerdictOvertaken, commit: commit,
		words: "Pushed " + short(commit) + ", but someone else pushed after it, so its fix wasn't checked.",
	}
}

// head is the pull request's head as the cycle read it, "" when it isn't known.
func (c verifyCase) head() string {
	if c.known && c.facts.Head.Known() {
		return c.facts.Head.Text
	}

	return ""
}

// commit is the run's commit as the verdict counts it, given the pull request's head:
// what it pushed, or, when its report didn't say it pushed, its worktree's commit once
// that is the head; "" for a run that pushed nothing.
func (c verifyCase) commit(head string) string {
	r := c.run
	if r.Commit == "" && r.HeadAfter != "" && !sameOID(r.HeadAfter, r.HeadBefore) && sameOID(head, r.HeadAfter) {
		return r.HeadAfter
	}

	return r.Commit
}

// judgeNoPush judges a run that pushed nothing: a commit it made while the branch moved
// was a push refused, once the item was read after the run ended; else it didn't push.
func judgeNoPush(c verifyCase, head string) verifyResult {
	committed := c.run.HeadAfter != "" && !sameOID(c.run.HeadAfter, c.run.HeadBefore)
	readSince := c.known && !c.facts.DetailsAt.IsZero() && !c.facts.DetailsAt.Before(c.run.EndedAt)

	switch {
	case committed && !readSince:
		return c.orGiveUp(verifyResult{read: true}, noPushWords(c.run.Summary))
	case committed && head != "" && !sameOID(head, c.run.HeadBefore):
		return verifyResult{verdict: VerdictRejected, words: "Its push was rejected: the branch moved while it worked."}
	}

	return verifyResult{verdict: VerdictNoPush, words: noPushWords(c.run.Summary)}
}

// noPushWords says a run pushed nothing, in its own words when it gave some.
func noPushWords(summary string) string {
	why := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(summary), "Finished:"))
	if why == "" || why == "Finished." {
		return "No push: the agent pushed nothing."
	}

	return textutil.Sentence("No push: " + strings.TrimSuffix(why, "."))
}

// judgeChecks judges the checks on the run's commit, the pull request's head.
func judgeChecks(c verifyCase, commit string) verifyResult {
	base := verifyResult{commit: commit, seen: true}

	failed, pending := c.facts.Value(catalog.FieldChecksFailed), c.facts.Value(catalog.FieldChecksPending)
	passed, awaiting := c.facts.Value(catalog.FieldChecksPassed), c.facts.Value(catalog.FieldAwaitingApproval)

	if !failed.Known() || !pending.Known() || !passed.Known() {
		base.read = true

		return c.orGiveUp(base, "Unknown: the checks on "+short(commit)+" couldn't be read.")
	}

	switch {
	case pending.Number > 0:
		base.read = c.stale()

		return c.orGiveUp(base, "Unknown: the checks on "+short(commit)+" hadn't finished after "+
			hoursWords(c.wait.GiveUp())+".")
	case awaiting.Known() && awaiting.Bool:
		return verifyResult{
			verdict: VerdictUnknown, commit: commit, seen: true,
			words: "Unknown: the workflow runs on " + short(commit) + " wait for a maintainer to approve them.",
		}
	case failed.Number == 0 && passed.Number == 0:
		return c.noChecks(base, commit)
	}

	return c.settled(base, commit, failed)
}

// noChecks waits a while for a check to appear on commit, reading the item again, then
// calls the checks unknown.
func (c verifyCase) noChecks(base verifyResult, commit string) verifyResult {
	since := c.started
	if c.seen != nil {
		since = *c.seen
	}

	if c.now.Sub(since) >= noChecksAfter {
		base.verdict, base.words = VerdictUnknown, "Unknown: no checks ran on "+short(commit)+"."

		return base
	}

	base.read = c.stale()

	return base
}

// settled reaches the verdict on a commit whose checks all finished, once their names are
// read and the last finished as long ago as the step settles.
func (c verifyCase) settled(base verifyResult, commit string, failed facts.Value) verifyResult {
	done := c.facts.ChecksDone
	if !done.Known() {
		base.names = done.Need == facts.NeedChecks

		return c.orGiveUp(base, "Unknown: the checks on "+short(commit)+" couldn't be read.")
	}

	if c.now.Sub(done.Since) < c.wait.Settle() {
		return base
	}

	if failed.Number == 0 {
		base.verdict, base.outcome = VerdictFixed, catalog.OutcomePassedChecks
		base.words = "Fixed: checks pass on " + short(commit) + "."

		return base
	}

	base.verdict, base.outcome = VerdictFailing, catalog.OutcomeFailingChecks
	base.failed = c.facts.Value(catalog.FieldFailedNames).List
	base.words = failingWords(base.failed, failed.Number, commit)

	return base
}

// failingWords says which checks still fail on commit: "Still failing: CodeQL.", naming
// at most maxNamedChecks, or counting them when their names aren't known.
func failingWords(names []string, n int, commit string) string {
	if len(names) == 0 {
		return "Still failing: " + textutil.Counted(n, "check", "checks") + " failed on " + short(commit) + "."
	}

	shown := names[:min(len(names), maxNamedChecks)]
	words := strings.Join(shown, ", ")

	if rest := len(names) - len(shown); rest > 0 {
		words += ", and " + strconv.Itoa(rest) + " more"
	}

	return "Still failing: " + words + "."
}

// orGiveUp returns r while the step's give-up time hasn't passed since verifying began,
// else the unknown verdict, saying words.
func (c verifyCase) orGiveUp(r verifyResult, words string) verifyResult {
	if c.now.Sub(c.started) < c.wait.GiveUp() {
		return r
	}

	return verifyResult{verdict: VerdictUnknown, words: words, commit: r.commit, seen: r.seen}
}

// stale reports details old enough for verify to read the item again itself.
func (c verifyCase) stale() bool {
	return !c.known || c.now.Sub(c.facts.DetailsAt) >= verifyReadAfter
}

// sameOID reports two commits the same, ignoring case; "" is no commit.
func sameOID(a, b string) bool {
	return a != "" && strings.EqualFold(a, b)
}

// short writes a commit as its first seven characters.
func short(oid string) string {
	const n = 7
	if len(oid) > n {
		return strings.ToLower(oid[:n])
	}

	return strings.ToLower(oid)
}

// hoursWords writes a give-up time: "2 hours".
func hoursWords(d time.Duration) string {
	return textutil.Counted(int(d/time.Hour), "hour", "hours")
}

// verifyMatch judges the verifying match m from what the cycle read, and reports whether
// it reached its verdict, which moves it on to its next step, queued. r is its routine,
// nil for one no longer running, whose verdict still lands. A match whose run can't be
// read waits for the next cycle.
func (p *pass) verifyMatch(ctx context.Context, r *routineRun, m *match) bool {
	if m.state() != StateVerifying || int(m.Step) >= len(m.steps) {
		return false
	}

	sr := &m.steps[m.Step]
	if sr.Step.WaitForChecks == nil {
		return false
	}

	run, ok := p.agentRunOf(ctx, m)
	if !ok {
		return false
	}

	started := p.now
	if sr.Started != nil {
		started = *sr.Started
	}

	f, known := p.facts[store.Deref(m.ItemID)]
	res := judgeVerify(verifyCase{
		run: run, wait: *sr.Step.WaitForChecks, facts: f, known: known, started: started, seen: sr.Seen, now: p.now,
	})

	if res.seen && sr.Seen == nil {
		sr.Seen = new(p.now)
	}

	if res.waiting() {
		p.askVerifyReads(m, run, res)

		return false
	}

	return p.applyVerdict(ctx, r, m, sr, res)
}

// agentRunOf returns the agent's run m's Wait for the checks follows: the run of the
// latest agent step before it. A match with no such run follows a push that never was.
func (p *pass) agentRunOf(ctx context.Context, m *match) (verifyRun, bool) {
	for i := int(m.Step) - 1; i >= 0; i-- {
		sr := m.steps[i]
		if sr.Step.Agent == nil {
			continue
		}

		if sr.RunID == 0 {
			return verifyRun{}, true
		}

		run, err := p.e.ledger.q.GetRun(ctx, sr.RunID)
		if err != nil {
			p.e.logger.WarnContext(ctx, "Autopilot could not read the run a match verifies", "match", m.ID, "error", err)

			return verifyRun{}, false
		}

		out := verifyRun{Commit: run.RunCommit, HeadBefore: run.HeadBefore, HeadAfter: run.HeadAfter, Summary: run.Summary}
		if run.FinishedAt != nil {
			out.EndedAt = *run.FinishedAt
		}

		p.verifying[m.ID] = run.ID

		return out, true
	}

	return verifyRun{}, true
}

// askVerifyReads asks for what a verifying match waits on: the item again, once its
// details are old enough and its run ended a moment ago, so the read the run's end asks for
// comes first; or its head's check names, at most every verifyNamesAfter.
func (p *pass) askVerifyReads(m *match, run verifyRun, res verifyResult) {
	id := store.Deref(m.ItemID)
	if id == 0 {
		return
	}

	if res.names {
		if at, ok := p.e.checksRead[id]; !ok || time.Since(at) >= verifyNamesAfter {
			delete(p.e.checksRead, id)
			p.checks = append(p.checks, id)
		}
	}

	if !res.read || p.now.Sub(run.EndedAt) < time.Minute {
		return
	}

	if at, ok := p.e.verifyReads[id]; ok && p.now.Sub(at) < verifyReadAfter {
		return
	}

	p.e.verifyReads[id] = p.now
	p.itemReads = append(p.itemReads, id)
}

// applyVerdict records res, m's verdict at its Wait for the checks sr, on the step and on
// the agent's run, in one transaction, and moves m on to its next step; the user is told,
// as Checks after a fix. The last try that still fails says the routine gave up.
func (p *pass) applyVerdict(ctx context.Context, r *routineRun, m *match, sr *stepRun, res verifyResult) bool {
	words := p.verdictWords(r, m, res)

	sr.Status, sr.Outcome, sr.Verdict, sr.Note, sr.Ended = stepDone, string(res.outcome), res.verdict, words, new(p.now)
	m.Step++

	m.set(StateQueued, "", "")

	runID := p.verifying[m.ID]

	if err := p.recordVerdict(ctx, m, runID, res.verdict); err != nil {
		p.e.logger.WarnContext(ctx, "Autopilot could not record a verdict; it judges it again", "match", m.ID,
			"error", err)

		m.AutopilotMatch, m.steps = m.orig, mustSteps(m.origSteps)

		return false
	}

	if res.verdict != VerdictNoPush && res.verdict != VerdictRejected {
		p.e.tellChecks(ctx, m, runID, r, words)
	}

	// Another of the routine's matches on the item may wait for this one.
	delete(p.active, [2]int64{m.RoutineID, store.Deref(m.ItemID)})
	p.e.Wake()

	return true
}

// verdictWords says res, m's verdict: on the routine r's last try on the item, a verdict
// that still fails says it gave up.
func (p *pass) verdictWords(r *routineRun, m *match, res verifyResult) string {
	if res.verdict != VerdictFailing || r == nil || m.Manual {
		return res.words
	}

	if tries := r.spec.Limits.Tries(); p.historyOf(r, store.Deref(m.ItemID)).Tries >= tries {
		return res.words + " " + gaveUpWords(tries)
	}

	return res.words
}

// recordVerdict writes m, moved on by its verdict, and the verdict on the agent's run
// runID, in one transaction.
func (p *pass) recordVerdict(ctx context.Context, m *match, runID int64, v Verdict) error {
	return p.e.ledger.inTx(ctx, func(q *queries.Queries) error {
		if runID != 0 {
			if _, err := q.SetRunVerdict(ctx, queries.SetRunVerdictParams{
				Verdict: string(v), VerifiedAt: new(p.now), UpdatedAt: p.now, ID: runID,
			}); err != nil {
				return err
			}
		}

		return p.e.ledger.update(ctx, q, m)
	})
}

// gaveUpWords says a routine used its last try on an item: "Gave up after 2 tries.",
// which Try again starts over.
func gaveUpWords(tries int) string {
	return "Gave up after " + textutil.Counted(tries, "try", "tries") + "."
}

// tellChecks tells the user, on a worker, the verdict on the checks after a routine's fix,
// as Checks after a fix, once per run.
func (e *Engine) tellChecks(ctx context.Context, m *match, runID int64, r *routineRun, words string) {
	if e.checkNotices == nil {
		return
	}

	n := Notice{
		Kind: NoticeChecks, RoutineID: m.RoutineID, MatchID: m.ID, RunID: runID, CreatedAt: m.CreatedAt,
		Item: new(matchItem(m)), Title: m.Title,
		Key: "checks:" + strconv.FormatInt(m.ID, 10) + ":" + strconv.FormatInt(m.CreatedAt.Unix(), 10) + ":" +
			strconv.FormatInt(runID, 10),
	}

	name := "A routine"
	if r != nil {
		name, n.Routine = r.row.Name, r.row.Name

		if r.spec.Notify != nil {
			n.Banner = r.spec.Notify.Checks
		}
	}

	n.Body = textutil.OneLine(name + ": " + words)

	e.goWork(ctx, func(ctx context.Context) {
		if err := e.checkNotices.Notice(ctx, n); err != nil && ctx.Err() == nil {
			e.logger.InfoContext(ctx, "Autopilot could not tell a fix's checks", "match", n.MatchID, "error", err)
		}
	})
}
