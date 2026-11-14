package ghthreads

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// decideRow is one of the account's threads about a pull request or an issue, as deciding
// whether it clears reads it.
type decideRow = queries.ListDecideThreadsRow

// moment is a moment the user finished an item in the app, as gh_thread_moments holds it:
// what finished it, when, and when the app heard of it, which the Undo hold runs from.
type moment struct {
	trigger threadrules.Trigger
	at      time.Time
	heard   time.Time
}

// momentsByItem returns the moments of rows by their item's key, in their order.
func momentsByItem(rows []queries.ListMomentsRow) map[string][]moment {
	out := make(map[string][]moment, len(rows))

	for _, r := range rows {
		key := item.Ref{Repo: repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}, Number: int(r.Number)}.Key()
		out[key] = append(out[key], moment{trigger: threadrules.Trigger(r.Trigger), at: r.At, heard: r.CreatedAt})
	}

	return out
}

// rowItem is the pull request or issue r is about.
func rowItem(r *decideRow) item.Ref {
	return item.Ref{Repo: repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}, Number: int(r.Number), Kind: item.Kind(r.ItemKind)}
}

// ruleThread is r as threadrules.Decide reads it. A bot's activity after the moment is
// known only for the update it was checked on: once the thread updated since, who acted is
// unknown again, until the next check. A person's stays until the thread is read.
func ruleThread(r *decideRow) threadrules.Thread {
	after := threadrules.AfterMoment(r.AfterMoment)
	if after == threadrules.AfterBot && (r.AfterMomentCheckedAt == nil || r.UpdatedAt.After(*r.AfterMomentCheckedAt)) {
		after = threadrules.AfterUnknown
	}

	return threadrules.Thread{
		Kind: item.Kind(r.ItemKind), Unread: r.Unread, UpdatedAt: r.UpdatedAt, Reason: r.Reason,
		State: threadrules.State(r.SubjectState), AfterMoment: after,
	}
}

// outsideMoment is the moment GitHub's own state says r's item was finished: merged, or
// closed, when GitHub closed it. It counts only on GitHub's word: a state checked within
// StateFreshFor of now (any time ago, when anyAge), with a close time; ok is false for any
// other, an item reopened included.
func outsideMoment(r *decideRow, now time.Time, anyAge bool) (threadrules.Moment, bool) {
	if r.SubjectClosedAt == nil || r.StateCheckedAt == nil {
		return threadrules.Moment{}, false
	}

	if !anyAge && now.Sub(*r.StateCheckedAt) > StateFreshFor {
		return threadrules.Moment{}, false
	}

	var trigger threadrules.Trigger

	switch threadrules.State(r.SubjectState) {
	case threadrules.StateMerged:
		trigger = threadrules.TriggerMerged
	case threadrules.StateClosed:
		trigger = threadrules.TriggerIssueClosed
		if item.Kind(r.ItemKind) == item.KindPR {
			trigger = threadrules.TriggerPRClosed
		}
	case threadrules.StateOpen, threadrules.StateUnknown:
		return threadrules.Moment{}, false
	default:
		return threadrules.Moment{}, false
	}

	return threadrules.Moment{Trigger: trigger, At: *r.SubjectClosedAt, Outside: true}, true
}

// decision is what deciding one thread came to: the verdict, and, for a clear, when it may
// be sent.
type decision struct {
	verdict   threadrules.Verdict
	sendAfter time.Time
}

// candidate is one moment a thread is decided against, and when a clear it chooses may go:
// a moment in the app once its Undo hold has passed, one from GitHub's state at once.
type candidate struct {
	moment threadrules.Moment
	end    time.Time
}

// rank orders the clearing candidates for the trigger a clear records: a moment in the app
// still holding first, so the window can offer Undo, then GitHub's, then a moment in the
// app whose hold has passed.
func (c candidate) rank(now time.Time) int {
	switch {
	case !c.moment.Outside && c.end.After(now):
		return 2
	case c.moment.Outside:
		return 1
	default:
		return 0
	}
}

// decideThread decides r, under rules, against every moment that could apply as of now:
// moments, those of the user's own in the app, whose clears wait hold after the app heard
// of them, and the moment GitHub's state says (outsideMoment, of any age when anyAge),
// whose clears go at once. ok is false when no moment applies.
//
// When any moment clears, so does the thread: its clear waits for the latest hold among
// those that clear, so GitHub's word never skips the Undo of the user's own merge, and it
// records the trigger of a moment in the app still holding, or else GitHub's, or else the
// latest. Otherwise the thread is held when any moment holds it, and skipped for the first
// moment's reason when none does.
func decideThread(
	rules threadrules.Rules, r *decideRow, moments []moment, now time.Time, hold time.Duration, anyAge bool,
) (decision, bool) {
	cands := candidates(r, moments, now, hold, anyAge)
	if len(cands) == 0 {
		return decision{}, false
	}

	t := ruleThread(r)
	tally := verdicts{now: now}

	for i := range cands {
		tally.add(&cands[i], threadrules.Decide(rules, t, cands[i].moment))
	}

	return tally.decision(), true
}

// candidates are the moments r is decided against as of now: moments, the user's own in
// the app, each held hold after the app heard of it, then GitHub's state's (of any age when
// anyAge), at once.
func candidates(r *decideRow, moments []moment, now time.Time, hold time.Duration, anyAge bool) []candidate {
	out := make([]candidate, 0, len(moments)+1)

	for _, m := range moments {
		out = append(out, candidate{moment: threadrules.Moment{Trigger: m.trigger, At: m.at}, end: m.heard.Add(hold)})
	}

	if om, found := outsideMoment(r, now, anyAge); found {
		out = append(out, candidate{moment: om, end: now})
	}

	return out
}

// verdicts tallies the verdicts of one thread's candidates, as of now, for decideThread.
type verdicts struct {
	now time.Time

	// chosen is the clearing candidate whose trigger the clear records, and sendAt the
	// latest end among those that clear.
	chosen *candidate
	sendAt time.Time

	// held is the first verdict that holds, and skipped the first that skips.
	held    *threadrules.Verdict
	skipped *threadrules.Verdict
}

// add tallies v, the verdict of c.
func (t *verdicts) add(c *candidate, v threadrules.Verdict) {
	switch v.Kind {
	case threadrules.VerdictClear:
		t.sendAt = later(t.sendAt, c.end)

		if t.chosen == nil || t.outranks(c) {
			t.chosen = c
		}
	case threadrules.VerdictHold:
		if t.held == nil {
			t.held = &v
		}
	case threadrules.VerdictSkip:
		if t.skipped == nil {
			t.skipped = &v
		}
	}
}

// outranks reports whether c records the clear's trigger rather than the one chosen so far.
func (t *verdicts) outranks(c *candidate) bool {
	if c.rank(t.now) != t.chosen.rank(t.now) {
		return c.rank(t.now) > t.chosen.rank(t.now)
	}

	return c.end.After(t.chosen.end)
}

// decision is what the tally comes to: a clear when any candidate clears, else a hold when
// any holds, else the first skip.
func (t *verdicts) decision() decision {
	switch {
	case t.chosen != nil:
		return decision{
			verdict:   threadrules.Verdict{Kind: threadrules.VerdictClear, Trigger: t.chosen.moment.Trigger},
			sendAfter: later(t.sendAt, t.now),
		}
	case t.held != nil:
		return decision{verdict: *t.held}
	case t.skipped != nil:
		return decision{verdict: *t.skipped}
	default:
		return decision{verdict: threadrules.Verdict{Kind: threadrules.VerdictSkip}}
	}
}

// byHand decides r cleared by the user's own hand, under rules, as of now.
func byHand(rules threadrules.Rules, r *decideRow, now time.Time) threadrules.Verdict {
	return threadrules.Decide(rules, ruleThread(r), threadrules.Moment{Trigger: threadrules.TriggerByHand, At: now})
}

// marks are the ledger's rows that bear on queueing a thread's clear on its own, by thread:
// one queued, and the latest update of the thread's a clear was sent for, the user kept it
// at (Undo), and a clear failed at (MaxAttempts).
type marks struct {
	queued map[int64]bool
	sent   map[int64]time.Time
	kept   map[int64]time.Time
	failed map[int64]time.Time
}

// marksOf reads rows into marks.
func marksOf(rows []queries.ListClearMarksRow) marks {
	out := marks{
		queued: map[int64]bool{}, sent: map[int64]time.Time{}, kept: map[int64]time.Time{}, failed: map[int64]time.Time{},
	}

	for _, r := range rows {
		switch ClearState(r.State) {
		case ClearQueued:
			out.queued[r.ThreadID] = true
		case ClearSent:
			out.sent[r.ThreadID] = later(out.sent[r.ThreadID], r.ThreadUpdatedAt)
		case ClearKept:
			out.kept[r.ThreadID] = later(out.kept[r.ThreadID], r.ThreadUpdatedAt)
		case ClearFailed:
			out.failed[r.ThreadID] = later(out.failed[r.ThreadID], r.ThreadUpdatedAt)
		case ClearSkipped:
		}
	}

	return out
}

// eligible reports whether a clear of r may be queued on its own: none is queued, and none
// was sent, kept by the user, or failed at its update or a later one, so an Undo and a
// failure both stick until the thread moves. The user's own clear may still be queued.
func (m marks) eligible(r *decideRow) bool {
	if m.queued[r.ThreadID] {
		return false
	}

	if settledAt(m.sent, r) {
		return false
	}

	_, stuck := m.stuckAt(r)

	return !stuck
}

// stuckAt reports why r waits until it moves before a clear of it is queued on its own: the
// user kept it at its update or a later one (SkipKept), or a clear of it failed there
// (SkipFailed).
func (m marks) stuckAt(r *decideRow) (SkipReason, bool) {
	switch {
	case settledAt(m.kept, r):
		return SkipKept, true
	case settledAt(m.failed, r):
		return SkipFailed, true
	default:
		return SkipNone, false
	}
}

// settledAt reports whether byThread holds r's thread at its update or a later one.
func settledAt(byThread map[int64]time.Time, r *decideRow) bool {
	at, ok := byThread[r.ThreadID]

	return ok && !r.UpdatedAt.After(at)
}

// inScope reports whether r is about an item of scope, nil holding every item.
func inScope(scope map[string]bool, r *decideRow) bool {
	return scope == nil || scope[rowItem(r).Key()]
}

// fingerprint is what of rules changes which threads clear, and how: "" while clearing on
// its own is off. A pass under rules whose fingerprint differs from the last one stored is
// the first after a change.
func fingerprint(rules threadrules.Rules) string {
	if !rules.Read || !rules.Auto {
		return ""
	}

	off := make([]string, 0, len(rules.TriggersOff))
	for _, t := range rules.TriggersOff {
		off = append(off, string(t))
	}

	var b strings.Builder

	b.WriteString("auto;action=")
	b.WriteString(string(threadrules.DecodeAction(string(rules.Action))))
	b.WriteString(";off=")
	b.WriteString(strings.Join(off, ","))
	b.WriteString(";watching=")
	b.WriteString(boolWord(rules.Watching))
	b.WriteString(";bots=")
	b.WriteString(boolWord(rules.BotActivity))

	return b.String()
}

// boolWord writes b as a word.
func boolWord(b bool) string {
	if b {
		return "on"
	}

	return "off"
}

// Hold is how long a clear chosen by the user's own action in the app waits before it is
// sent, so its toast's Undo can keep it, for toasts that stay toastSeconds: the toast's
// time and HoldPad more, never less than MinHold.
func Hold(toastSeconds int) time.Duration {
	return max(MinHold, time.Duration(toastSeconds)*time.Second+HoldPad)
}

// hold reads the toast setting and returns the Hold it makes: MinHold with no HoldSource,
// or one that can't be read.
func (s *Service) hold(ctx context.Context) time.Duration {
	if s.holds == nil {
		return MinHold
	}

	seconds, err := s.holds.ToastSeconds(ctx)
	if err != nil {
		s.logger.DebugContext(ctx, "the toast setting can't be read; holding clears the least", slog.Any("error", err))

		return MinHold
	}

	return Hold(seconds)
}
