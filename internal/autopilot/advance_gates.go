package autopilot

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// verdict is what the limits say of a start: go, wait on a blocker, or close.
type verdict struct {
	closeAs State
	reason  Reason
	block   Blocker
	words   string
}

// wait is a verdict that waits on b.
func wait(b Blocker, words string) verdict { return verdict{block: b, words: words} }

// pass is a verdict that lets a start through.
func (v verdict) through() bool { return v.closeAs == "" && v.block == "" }

// gate checks every limit on a start, in Go, cheapest first, and says whether it goes,
// waits, or closes. A manual run counts toward no cap or try, and keeps to the active
// hours of no routine, but waits for a free slot and the daily cap like any other.
func (p *pass) gate(r *routineRun, m *match, sr *stepRun, info catalog.Step) verdict {
	agent, github := info.Kind == catalog.StepAgent, writesGitHub(info)
	id := store.Deref(m.ItemID)

	for _, check := range []func() verdict{
		func() verdict { return p.mergeRules(id, sr) },
		func() verdict { return p.recheck(r, m, sr) },
		func() verdict { return p.pastRulesSwitch(m) },
		func() verdict { return p.scriptAsks(sr) },
		func() verdict { return p.askFirst(r, m, sr) },
		func() verdict { return p.held(r, m) },
		func() verdict { return p.limits(r, m, id, agent, github) },
		func() verdict { return p.room(r, sr, agent, github) },
		func() verdict { return p.itemRules(m, id, sr, agent) },
		func() verdict {
			if !agent {
				return verdict{}
			}

			return p.readyFor(r)
		},
	} {
		if v := check(); !v.through() {
			return v
		}
	}

	return verdict{}
}

// held holds every start, a local step's too, while Autopilot is paused, and outside the
// routine's active hours, which a manual run keeps to no routine's.
func (p *pass) held(r *routineRun, m *match) verdict {
	switch {
	case p.inbox.SignedOut:
		return wait(BlockSignedOut, "No one is signed in to GitHub; it starts once you sign in again.")
	case p.cfg.Paused:
		return wait(BlockPaused, "Autopilot is paused.")
	case !m.Manual && !inActiveHours(r.spec.Limits.ActiveHours, p.now):
		return wait(BlockActiveHours, "It waits for the routine's active hours.")
	}

	return verdict{}
}

// room checks there is room for an agent's run (a free slot, and the daily cap) and for a
// GitHub action (the pace Autopilot keeps under the write gate, and any hold the gate
// asked for).
func (p *pass) room(r *routineRun, sr *stepRun, agent, github bool) verdict {
	switch {
	case github:
		return p.pace()
	case !agent:
		return verdict{}
	case p.e.runnerClosed:
		return wait(BlockSlot, "The runner is closing, as the app quits; it starts on the next start.")
	case p.counts.activeAgents >= p.cfg.AgentsAtOnce:
		return wait(BlockSlot, textutil.Counted(p.cfg.AgentsAtOnce, "agent is", "agents are")+" working already.")
	}

	return p.spend(r, sr.Step.Agent)
}

// pace holds a GitHub action while Autopilot sent its pace a minute (actionsPerMinute) in
// the last minute, or its setting's GitHub actions an hour in the last hour, under the write
// gate's edit lane, so the user's own clicks always have room; and while the gate refused
// the last one, until it said the next may go. A manual run keeps to it too: it is about
// GitHub's limits, not a cap.
func (p *pass) pace() verdict {
	perMinute, perHour := actionsPerMinute(p.cfg.ActionsPerHour), p.cfg.ActionsPerHour

	switch {
	case p.now.Before(p.e.writesHeld):
		return wait(BlockWritesHeld, p.e.writesHeldWords)
	case p.counts.paceMinute >= perMinute:
		return wait(BlockWritesHeld, "Autopilot sent "+
			textutil.Counted(perMinute, "GitHub action", "GitHub actions")+
			" in the last minute: the next goes in a moment, so your own changes always have room.")
	case p.counts.paceHour >= perHour:
		return wait(BlockWritesHeld, "Autopilot sent "+
			textutil.Counted(perHour, "GitHub action", "GitHub actions")+
			" in the last hour, the most Settings → Autopilot allows an hour: the next goes as that eases.")
	}

	return verdict{}
}

// actionsPerMinute is the most GitHub actions Autopilot sends in a minute at perHour an hour:
// a sixth of it, so an hour's worth goes out over ten minutes or more, at least one, and
// never more than MaxActionsPerMinute.
func actionsPerMinute(perHour int) int {
	return min(max(perHour/6, 1), MaxActionsPerMinute)
}

// askFirst holds a start for the user's OK: a routine that asks first, and an Autopilot
// routine's start after a long gap. A run the user started by hand asks no OK to start, but
// an Ask first routine still asks before it merges or posts what an agent wrote.
func (p *pass) askFirst(r *routineRun, m *match, sr *stepRun) verdict {
	switch {
	case r.mode == autopilotprefs.ModeAsk && followsRun(sr.Step) && !sr.OK:
		// Merging, and posting what an agent wrote, are decided on what the run came to:
		// an Ask first routine asks again, showing it, whatever its OK to start.
		return wait(BlockAsk, followWords(sr.Step))
	case m.Manual:
		return verdict{}
	case m.Approved:
		return verdict{}
	case r.mode == autopilotprefs.ModeAsk:
		return wait(BlockAsk, "Waiting for your OK.")
	case p.catchingUp(r) && p.searchFailing(r):
		return wait(BlockCatchUp, "Its search is failing, so what it matches may be out of date: it waits for your OK.")
	case p.catchingUp(r):
		return wait(BlockCatchUp, "Nothing was read for a while: it waits for your OK before it catches up.")
	}

	return verdict{}
}

// limits checks the routine's tries, the day's counts, and the wait between tries, none of
// which a manual run counts toward. A match is one try and one of its routine's runs
// however many runs it starts (an agent's and the steps after it, or each of a GitHub
// step's actions), so only its first is held to the tries, the routine's runs a day, and
// the wait; the agent runs and the GitHub actions a day count each one.
func (p *pass) limits(r *routineRun, m *match, id int64, agent, github bool) verdict {
	if m.Manual {
		return verdict{}
	}

	first := m.Attempt == 0
	if v := p.firstTry(r, id, first); !v.through() {
		return v
	}

	switch {
	case agent && p.counts.agentRuns >= p.cfg.RunsPerDay:
		return wait(BlockCapRuns, untilTomorrow+"Autopilot ran "+
			textutil.Counted(p.cfg.RunsPerDay, "agent", "agents")+" today, "+capInSettings)
	case github && p.counts.actions >= p.cfg.ActionsPerDay:
		return wait(BlockCapActions, untilTomorrow+"Autopilot sent "+
			textutil.Counted(p.cfg.ActionsPerDay, "GitHub action", "GitHub actions")+" today, "+capInSettings)
	}

	if last := p.lastTry(r.id(), id); first && !last.IsZero() && p.now.Sub(last) < r.spec.Limits.Wait() {
		return wait(BlockWait, "It waits "+r.spec.Limits.Wait().String()+" between tries.")
	}

	return verdict{}
}

// untilTomorrow and capInSettings word a start a daily cap holds back: it starts by itself the
// next day, and the cap is the user's to change.
const (
	untilTomorrow = "It waits until tomorrow: "
	capInSettings = "the most Settings → Autopilot allows in a day."
)

// firstTry checks a match's first run, which is its try (first): the routine's tries on the
// item, and its runs a day; a later run of the match passes.
func (p *pass) firstTry(r *routineRun, id int64, first bool) verdict {
	if !first {
		return verdict{}
	}

	h := p.historyOf(r, id)
	lim := r.spec.Limits

	switch {
	case h.Tries >= lim.Tries():
		return verdict{closeAs: StateSkipped, reason: ReasonTries, words: gaveUpWords(lim.Tries())}
	case p.counts.routineRuns[r.id()] >= lim.Runs():
		return wait(BlockCapRuns, untilTomorrow+"the routine ran "+textutil.Counted(lim.Runs(), "time", "times")+
			" today, the most its limits allow in a day (its runs a day).")
	}

	return verdict{}
}

// lastTry returns when the routine's last counted run on the item ended, zero for none.
func (p *pass) lastTry(routineID, id int64) time.Time {
	var last time.Time

	for _, run := range p.runs[id] {
		if run.RoutineID == routineID && countsTry(run) && run.FinishedAt != nil && run.FinishedAt.After(last) {
			last = *run.FinishedAt
		}
	}

	return last
}

// spend checks the daily cap, and the routine's own, against the run's budget: today's use,
// plus the budgets of the runs still working, plus this one's, must fit under each. A
// budget in another unit than a cap's can't be counted against it.
func (p *pass) spend(r *routineRun, a *spec.AgentStep) verdict {
	amount, unit := runBudget(a)

	if c := p.cfg.DailyCap; !c.Off() && c.Unit == unit && p.counts.spend[unit]+amount > float64(c.Amount) {
		return wait(BlockCapSpend, untilTomorrow+"it would go over today's cap (Daily cap, in Settings → Autopilot).")
	}

	if b := r.spec.Limits.BudgetPerDay; b != nil {
		limit, limitUnit := budgetOf(b)
		if limitUnit == unit && p.counts.routineSpend[r.id()][unit]+amount > limit {
			return wait(BlockCapSpend, untilTomorrow+"it would go over the routine's budget for today (its limits).")
		}
	}

	return verdict{}
}

// itemRules checks what the item itself allows: an outside contributor's item, a fork's
// branch for a run that pushes, and one run on the item at a time. A fork's pull request
// is an outside contributor's whoever opened it, so it reaches the fork's check only where
// its org or repository lets agents run on such items.
func (p *pass) itemRules(m *match, id int64, sr *stepRun, agent bool) verdict {
	if f, ok := p.facts[id]; ok {
		allowed, _ := p.outsideAllowed(m)
		if v := outsideRule(f, agent, m.Approved, allowed, m.RepoOwner+"/"+m.RepoName); !v.through() {
			return v
		}

		fork := f.Value(catalog.FieldFromFork)
		if agent && sr.Step.Agent.MayPush() && fork.Known() && fork.Bool {
			return wait(BlockCrossRepo, "Its branch is in a fork, which a run can't push to.")
		}
	}

	if other, ok := p.active[[2]int64{m.RoutineID, id}]; ok && other != m.ID {
		return wait(BlockSettling, "It waits for the checks on this routine's last run on it.")
	}

	if p.counts.activeItems[id] {
		return wait(BlockBusyWorktree, "Something else runs on it; it starts once that ends.")
	}

	return verdict{}
}

// writesGitHub reports a step the app's own GitHub write sends: a GitHub step, Merge it,
// and Post the agent's review, each paced and capped as a GitHub action.
func writesGitHub(info catalog.Step) bool {
	return info.Dispatcher == catalog.DispatchGitHub && !info.Local || info.Dispatcher == catalog.DispatchMerge ||
		info.Dispatcher == catalog.DispatchReview
}

// mergeRules closes a Merge it on an outside contributor's pull request, which no routine
// ever merges, whatever its org or repository allows agents.
func (p *pass) mergeRules(id int64, sr *stepRun) verdict {
	if f, ok := p.facts[id]; ok && sr.Step.Merge != nil && outsideContributor(f) {
		return verdict{
			closeAs: StateClosed, reason: ReasonNotOffered,
			words: "Merge it never merges an outside contributor's pull request.",
		}
	}

	return verdict{}
}

// outsideRule holds an outside contributor's item: an agent runs on one only where its org
// or repository allows it (allowed), in plan mode even then, since no routine ever lifts
// the restriction; elsewhere it closes its match, saying where to allow it. A GitHub
// action waits for the user's OK.
func outsideRule(f facts.Facts, agent, approved, allowed bool, repo string) verdict {
	t := f.Value(catalog.FieldTrust)

	switch {
	case !t.Known() || t.Text != string(trust.ClassOutside) || approved && !agent:
		return verdict{}
	case agent && allowed:
		return verdict{}
	case agent:
		return verdict{closeAs: StateSkipped, reason: ReasonOutside, words: outsideWords(repo)}
	}

	return wait(BlockOutside, "An outside contributor's item: it waits for your OK.")
}

// outsideAllowed reports whether the user lets agents run on outside contributors' items
// in m's repository, and where that was chosen.
func (p *pass) outsideAllowed(m *match) (bool, string) {
	if p.outside == nil {
		return false, ""
	}

	return p.outside.Allowed(m.RepoOwner + "/" + m.RepoName)
}

// runBudget returns an agent step's budget per run and its unit.
func runBudget(a *spec.AgentStep) (float64, autopilotprefs.Unit) {
	if a != nil && a.Fence != nil && a.Fence.Budget != nil {
		return budgetOf(a.Fence.Budget)
	}

	return float64(catalog.DefaultRunCents) / 100, autopilotprefs.UnitUSD
}

// budgetOf returns a budget's amount and unit.
func budgetOf(b *spec.Budget) (float64, autopilotprefs.Unit) {
	if b.Tokens != nil {
		return float64(*b.Tokens), autopilotprefs.UnitTokens
	}

	if b.USD != nil {
		return *b.USD, autopilotprefs.UnitUSD
	}

	return 0, autopilotprefs.UnitUSD
}

// inActiveHours reports whether now is within a routine's active hours: always with none.
// Hours whose end comes before their start run past midnight, from the start's day.
func inActiveHours(h *spec.ActiveHours, now time.Time) bool {
	if h == nil {
		return true
	}

	from, okFrom := minutesOf(h.From)
	to, okTo := minutesOf(h.To)

	if !okFrom || !okTo {
		return true
	}

	t := now.Hour()*60 + now.Minute()
	day := weekdayOf(now)

	switch {
	case from <= to:
		return onDay(h.Days, day) && t >= from && t < to
	case t >= from:
		return onDay(h.Days, day)
	case t < to:
		return onDay(h.Days, weekdayOf(now.AddDate(0, 0, -1)))
	}

	return false
}

// minutesOf reads "HH:MM" as minutes past midnight.
func minutesOf(s string) (int, bool) {
	hh, mm, ok := strings.Cut(s, ":")
	if !ok {
		return 0, false
	}

	h, errH := strconv.Atoi(hh)
	m, errM := strconv.Atoi(mm)

	return h*60 + m, errH == nil && errM == nil
}

// weekdayOf names t's day as the catalog does.
func weekdayOf(t time.Time) catalog.Weekday {
	return catalog.Weekdays()[(int(t.Weekday())+6)%7]
}

// onDay reports whether days holds day; no days is every day.
func onDay(days []catalog.Weekday, day catalog.Weekday) bool {
	if len(days) == 0 {
		return true
	}

	return slices.Contains(days, day)
}
