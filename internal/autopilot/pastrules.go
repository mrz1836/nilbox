package autopilot

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A routine's Merge it may go past a branch's rules on the user's own pull request that
// lacks only an approval from someone else, which GitHub never lets them give, when three
// consents stand, each of them the user's alone:
//
//   - the step allows it (spec.MergeStep's AllowBypass, and AllowSensitive for workflow,
//     agent, or tooling files), in a routine with no agent step that fires when the checks
//     pass, which the spec's checks hold it to;
//   - the user accepted the routine so when they saved it (Draft.AcceptPastRules): the
//     routine's past_rules_hash is the full SHA-256 of the spec text they accepted, which
//     any rewrite of the spec leaves standing for nothing, so an edit, an import, or a
//     backup brought back asks again;
//   - Let routines merge your own pull requests past the rules is on in Autopilot's
//     settings (autopilotprefs.KeyMergePastRules), which no backup carries and Describe it
//     never sets; turning it off stops every such merge at once.
//
// At send time the engine adds what only it knows (pastReview): the match is the routine's
// own on the commit its checks passed on (never a click's run), no routine's run pushed to
// the pull request, and the match's steps are the routine the user accepted. Core re-reads
// the switch just before the merge, and the review sheet holds the merge to the pull
// request itself, on a fresh read (internal/review's pastrules.go). Anything short of that
// merges only what GitHub would merge anyway, or closes as no longer offered, saying why:
// nothing ever asks the user to click.

// PastRules is how a routine stands on merging past the rules, as the window shows it.
type PastRules string

// How a routine stands on merging past the rules.
const (
	// PastRulesNone is a routine whose Merge it never goes past the rules.
	PastRulesNone PastRules = ""

	// PastRulesAllowed is one the user accepted as it is now.
	PastRulesAllowed PastRules = "allowed"

	// PastRulesNeedsTick is one whose step allows it, which the user hasn't accepted as it
	// is now: it merges only what GitHub allows until they save it again, accepting it.
	PastRulesNeedsTick PastRules = "needsTick"
)

// pastRulesHash names a spec's stored text as the user accepted it: its full SHA-256, in
// lowercase hex.
func pastRulesHash(data string) string {
	sum := sha256.Sum256([]byte(data))

	return hex.EncodeToString(sum[:])
}

// accepted reports a routine whose stored spec is the text its user accepted as one that
// merges past the rules.
func accepted(row queries.AutopilotRoutine) bool {
	return row.PastRulesHash != "" && row.PastRulesHash == pastRulesHash(row.Spec)
}

// pastRulesOf is how the stored routine row stands on merging past the rules.
func pastRulesOf(row queries.AutopilotRoutine, sp spec.Spec) PastRules {
	switch {
	case !sp.PastRules():
		return PastRulesNone
	case accepted(row):
		return PastRulesAllowed
	default:
		return PastRulesNeedsTick
	}
}

// pastRulesToWrite is the hash Save stores beside the spec text data, sp loaded: the text's
// own when its Merge it goes past the rules and the user accepted it now (accept) or had
// accepted that same text (was, the hash stored), "" when it doesn't go past them. A spec
// that goes past them and wasn't accepted is refused, at its first such step.
func pastRulesToWrite(sp spec.Spec, data, was string, accept bool) (string, error) {
	if !sp.PastRules() {
		return "", nil
	}

	hash := pastRulesHash(data)
	if accept || was == hash {
		return hash, nil
	}

	field := ""

	for i, st := range sp.Steps {
		if st.Merge != nil && (st.Merge.AllowBypass || st.Merge.AllowSensitive) {
			field = fmt.Sprintf("[%d].merge.allowBypass", i)

			break
		}
	}

	v := &spec.ValidationError{Problems: []spec.Problem{{
		Part: spec.PartSteps, Field: field, Message: pastRulesUnaccepted,
	}}}

	return "", uierr.New(pastRulesUnaccepted, &ProblemsError{err: v})
}

// pastRulesUnaccepted says a routine that merges past the rules wasn't accepted as such.
const pastRulesUnaccepted = "Confirm that this routine may merge your own pull requests past the rules, then save it."

// PastRulesOffWords says the user's switch for merging past the rules is off: why a Merge
// it that allows going past them may not (consentWhy), and core's answer when the switch
// reads off right before the merge.
const PastRulesOffWords = "Let routines merge your own pull requests past the rules is off in Autopilot's settings."

// pastRulesWaitWords says what a match waiting on the switch waits for (BlockPastRulesOff).
const pastRulesWaitWords = "It needs to merge past the rules, which is off in Autopilot's settings: turn on Let " +
	"routines merge your own pull requests past the rules, and it merges by itself."

// resumeClosedFor is how long ago a match closed for want of the switch (an earlier build
// closed it rather than wait) may have closed for resumePastRules to set it waiting again.
const resumeClosedFor = 7 * 24 * time.Hour

// pastRulesSwitch keeps m, waiting on the switch to go past the rules, waiting while the
// switch is off; once it is on, the merge goes again, its pull request read afresh.
func (p *pass) pastRulesSwitch(m *match) verdict {
	if Blocker(m.BlockedBy) == BlockPastRulesOff && !p.cfg.MergePastRules {
		return wait(BlockPastRulesOff, pastRulesWaitWords)
	}

	return verdict{}
}

// resumePastRules sets waiting again, at the merge, each match an earlier build closed as
// no longer offered because the switch to go past the rules was off (the words it closed
// with say so), closed within resumeClosedFor: the merge then waits for the switch, and goes
// on by itself once it is on, its earlier steps kept as done, so a reply it sent isn't sent
// again. The cycle's gates decide it again (recheck closes one whose pull request moved on).
// It runs once, on the engine's first cycle.
func (p *pass) resumePastRules(ctx context.Context) error {
	if p.e.resumed {
		return nil
	}

	rows, err := p.e.ledger.q.ListClosedMatches(ctx, queries.ListClosedMatchesParams{
		State: string(StateClosed), Reason: string(ReasonNotOffered), Detail: PastRulesOffWords,
		Since: new(p.now.Add(-resumeClosedFor)),
	})
	if err != nil {
		return fmt.Errorf("read the matches that waited to merge past the rules: %w", err)
	}

	for _, row := range rows {
		m, readErr := readMatch(row)
		if readErr != nil || int(m.Step) >= len(m.steps) || m.steps[m.Step].Step.Merge == nil {
			continue
		}

		sr := &m.steps[m.Step]
		sr.Status, sr.RunID, sr.Note, sr.Started, sr.Ended = stepPending, 0, "", nil, nil
		m.Reason, m.ClosedAt = "", nil
		m.wait(BlockPastRulesOff, pastRulesWaitWords)

		p.e.save(ctx, m)
	}

	p.e.resumed = true

	return nil
}

// agentlessHead is the commit an agentless Merge it merges: the head m's Checks pass
// trigger fired on, only while m's fire key is that commit's and none of its steps is an
// agent's or a Wait for the checks; "" for any other, a click's run among them.
func agentlessHead(m *match) string {
	if m.HeadOID == "" || m.FireKey != eval.CommitKey(catalog.EventChecksPassed, m.HeadOID) {
		return ""
	}

	for _, sr := range m.steps {
		if sr.Step.Agent != nil || sr.Step.WaitForChecks != nil {
			return ""
		}
	}

	return m.HeadOID
}

// agentlessSteps reports m's steps holding no agent step and no Wait for the checks, as a
// routine's run, never a click's.
func agentlessSteps(m *match) bool {
	if m.Manual {
		return false
	}

	for _, sr := range m.steps {
		if sr.Step.Agent != nil || sr.Step.WaitForChecks != nil {
			return false
		}
	}

	return true
}

// recheck holds an agentless Merge it to the cycle's decision on its item right before it
// starts, which a match that started a step before it (its reply, say) is otherwise never
// decided again by: it goes while the routine still matches the item on the occurrence it
// matched (its fire key, the commit its checks passed on). One whose fire key moved closes
// as moved on, one a condition no longer holds for (a label taken off, a title changed) as
// no longer matching, and one whose trigger doesn't hold now (its checks running again)
// waits.
func (p *pass) recheck(r *routineRun, m *match, sr *stepRun) verdict {
	if sr.Step.Merge == nil || !agentlessSteps(m) {
		return verdict{}
	}

	d, ok := r.decided[store.Deref(m.ItemID)]

	switch {
	case !ok:
		return wait(BlockSettling, "It waits for the routine to decide it again before it merges.")
	case d.Match == eval.Yes && d.FireKey == m.FireKey:
		return verdict{}
	case d.Match == eval.Yes:
		return verdict{closeAs: StateClosed, reason: ReasonMovedOn, words: "It moved on: " + firstActual(d)}
	case d.Match == eval.No && triggerHolds(d):
		return verdict{closeAs: StateClosed, reason: ReasonNoLongerMatches, words: "It no longer matches: " + firstNo(d)}
	}

	return wait(BlockSettling, "It waits for its trigger to hold again before it merges: "+firstActual(d)+".")
}

// triggerHolds reports a decision whose trigger said yes.
func triggerHolds(d eval.Decision) bool {
	return len(d.Trace) > 0 && d.Trace[0].Result == eval.Yes
}

// pastReview decides whether r's Merge it, sr of m, goes past the rules where an approval
// from someone else is all the pull request lacks: it may (with sensitive files when the
// step allows them too), or, for a step that allows it, why it may not; a step that doesn't
// allow it says nothing. The review sheet checks the pull request itself on its fresh read.
func (p *pass) pastReview(r *routineRun, m *match, sr *stepRun) (bool, bool, string) {
	st := sr.Step.Merge
	if st == nil || (!st.AllowBypass && !st.AllowSensitive) {
		return false, false, ""
	}

	if why := cmp.Or(matchWhy(m), p.consentWhy(r, m, st)); why != "" {
		return false, false, why
	}

	return true, st.AllowSensitive, ""
}

// matchWhy says why m, a match of a routine whose Merge it allows going past the rules, may
// not: a click's run, or no commit its checks passed on; "" when it may.
func matchWhy(m *match) string {
	switch {
	case m.Manual:
		return "A run you started by hand never merges past the rules."
	case agentlessHead(m) == "":
		return "It merges past the rules only the commit its checks passed on, when they did."
	default:
		return ""
	}
}

// consentWhy says why r, whose Merge it st is, may not merge m's pull request past the
// rules for want of a consent: the step's tick, the switch, the user's acceptance of the
// routine as it is and as m matched it, and no routine's run having pushed to it; "" when
// every one stands.
func (p *pass) consentWhy(r *routineRun, m *match, st *spec.MergeStep) string {
	switch {
	case !st.AllowBypass:
		return "Its step doesn't allow merging past the rules."
	case !p.cfg.MergePastRules:
		return PastRulesOffWords
	case r == nil || !accepted(r.row):
		return "You haven't confirmed it merges past the rules since it changed: open it and save it again."
	case m.SpecHash != r.hash:
		return "The routine changed since it matched this pull request."
	case p.pushedTo(store.Deref(m.ItemID)):
		return "A routine's run pushed to it, so no routine merges it past the rules."
	default:
		return ""
	}
}
