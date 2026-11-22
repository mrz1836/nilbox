package kit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// A part's risks are worked out from what it changes, before and after, never from the
// agent's words, so a card's warnings can't be talked down. Each is one sentence, true to
// what the app does, and a routine's are of what the change adds: what a routine did
// before, and does still, is no new risk.

// RoutineRisks returns the risks of a routine whose definition goes from before's to
// after, a new routine's when before is nil: what it would do that it didn't, its agent's
// reach and budget, what it sends GitHub or runs, the prompts this proposal adds that its
// agent runs (adds), and the limits it loosens; and, for a routine that is on outside Dry
// run, a change to what it acts on. names names a starter prompt for the words, as the
// library does; nil writes a starter's key as words. The list is never nil.
func RoutineRisks(before *Routine, after spec.Spec, adds []NewPrompt, names spec.PromptNames) []Risk {
	var was spec.Spec
	if before != nil {
		was = before.Spec
	}

	risks := []Risk{}
	risks = append(risks, liveRisks(before, after)...)
	risks = append(risks, agentRisks(agentOf(was), agentOf(after), adds)...)
	risks = append(risks, mergeRisks(was, after)...)
	risks = append(risks, reviewRisks(was, after)...)
	risks = append(risks, githubRisks(was, after, adds, names)...)
	risks = append(risks, scriptRisks(was, after)...)

	return append(risks, limitRisks(was.Limits, after.Limits)...)
}

// SettingsRisks returns the risks of Autopilot's settings going from before to after:
// new routines starting in Autopilot, agents starting through an outage, a shorter
// history or run log (which deletes what is older), more agents, runs, or actions at
// once or a day, a daily cap raised or taken away, and longer gaps caught up on without
// asking. The list is never nil.
func SettingsRisks(before, after autopilotprefs.Config) []Risk {
	checks := []func(before, after autopilotprefs.Config) []Risk{
		modeRisks, outageRisks, historyRisks, countRisks, capRisks, catchUpRisks,
	}

	risks := make([]Risk, 0, len(checks))
	for _, check := range checks {
		risks = append(risks, check(before, after)...)
	}

	return risks
}

// RulesRisks returns the risks of the rules routines lean on going from before to after:
// each bot trusted now, each repository or org muted now, and each workflow whose held
// runs are ignored now. The list is never nil.
func RulesRisks(before, after Rules) []Risk {
	risks := []Risk{}

	trusted := botsOf(before.TrustedBots)

	for _, bot := range botsOf(after.TrustedBots) {
		if !slices.Contains(trusted, bot) {
			risks = append(risks, cardWarning("Trusts %s[bot]: its pull requests no longer count as an outside "+
				"contributor's, so routines act on them without that hold, and a click launches them with the "+
				"profile's permissions.", bot))
		}
	}

	for _, target := range after.Muted {
		if !slices.ContainsFunc(before.Muted, func(t string) bool { return sameTarget(t, target) }) {
			risks = append(risks, muteRisk(target))
		}
	}

	for _, name := range after.IgnoredWorkflows {
		if !slices.Contains(before.IgnoredWorkflows, name) {
			risks = append(risks, cardWarning("Ignores the held runs of %s: they never count toward where a pull "+
				"request stands, even while GitHub reports its merge blocked.", riskQuote(name)))
		}
	}

	return risks
}

// PromptRisks returns the risks of adding p beside the routines that may run it: an
// agent told to do what its text says, or a routine commenting it on GitHub as the user.
// A prompt no routine runs carries none. The list is never nil.
func PromptRisks(p NewPrompt, routines []spec.Spec) []Risk {
	risks := []Risk{}
	nick := strings.TrimSpace(p.Nickname)

	if nick == "" {
		return risks
	}

	var agentRuns, comments bool

	for _, sp := range routines {
		for _, st := range sp.Steps {
			agentRuns = agentRuns || (st.Agent != nil && namesPrompt(st.Agent.Prompt, nick))
			comments = comments || slices.ContainsFunc(st.GitHub, func(a spec.GitHubAction) bool {
				return a.Reply != nil && namesPrompt(a.Reply.Prompt, nick)
			})
		}
	}

	if agentRuns {
		risks = append(risks, cardWarning("A routine's agent runs it: its text is what the agent is told to do, so read "+
			"it before you add it."))
	}

	if comments {
		risks = append(risks, cardWarning("A routine comments it on GitHub as you: read its text before you add it."))
	}

	return risks
}

// routineCautions returns what is worth knowing of a checked routine that loosens
// nothing: it changed after the proposal was written, or its name, proposed, is taken.
func routineCautions(r CheckedRoutine, proposed string) []Risk {
	var risks []Risk

	if r.Stale {
		risks = append(risks, Risk{
			Level: RiskCaution, Text: "It changed after this proposal was written: saving it replaces those changes.",
		})
	}

	if r.NameTaken {
		risks = append(risks, Risk{Level: RiskCaution, Text: fmt.Sprintf(
			"Another routine is named %s, so this one is saved as %s.", riskQuote(proposed), riskQuote(r.Name),
		)})
	}

	return risks
}

// liveRisks returns the risk of a change to what a routine that is on, outside Dry run,
// acts on: its watch, trigger, conditions, or once-per.
func liveRisks(before *Routine, after spec.Spec) []Risk {
	if before == nil || !before.Enabled || before.Mode == autopilotprefs.ModeDryRun {
		return nil
	}

	was, now := spec.Normalize(before.Spec), spec.Normalize(after)

	var parts []string

	for _, p := range []struct {
		words   string
		changed bool
	}{
		{"its watch", !sameShape(was.Watch, now.Watch)},
		{"its trigger", !sameShape(was.Trigger, now.Trigger)},
		{"its conditions", !sameShape(was.Conditions, now.Conditions)},
		{"how often it acts on one item", was.Limits.OncePer != now.Limits.OncePer},
	} {
		if p.changed {
			parts = append(parts, p.words)
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return []Risk{cardWarning("It's on, in %s, and this changes %s: what it acts on changes as soon as it's saved.",
		before.Mode.Label(), textutil.Series(parts))}
}

// agentReach is what a routine's agent step may do beyond its worktree.
type agentReach struct {
	// terminal is a run in the user's terminal, as a click's launch; profile one in the
	// background with the profile's own permissions; push one that may push; and github
	// one that may run gh on the item.
	terminal, profile, push, github bool
}

// reachOf returns what a may do beyond its worktree, nothing for no agent step.
func reachOf(a *spec.AgentStep) agentReach {
	switch {
	case a == nil:
		return agentReach{}
	case a.Place() == catalog.WhereTerminal:
		return agentReach{terminal: true}
	case a.Tools() == catalog.PresetProfile:
		return agentReach{profile: true, push: true, github: true}
	}

	info, _ := presetByID(a.Tools())

	return agentReach{push: info.MayPush, github: a.Tools() == catalog.PresetGitHubTasks}
}

// agentRisks returns the risks of a routine's agent step going from was to now: its
// reach, its fence's budget, time limit, and extra commands, and a prompt this proposal
// adds that it runs.
func agentRisks(was, now *spec.AgentStep, adds []NewPrompt) []Risk {
	if now == nil {
		return nil
	}

	risks := reachRisks(reachOf(was), reachOf(now))

	if now.Place() == catalog.WhereBackground {
		var fence *spec.Fence
		if was != nil && was.Place() == catalog.WhereBackground {
			fence = was.Fence
		}

		risks = append(risks, budgetRisks(fence, now.Fence)...)
		risks = append(risks, timeLimitRisks(fence, now.Fence)...)
		risks = append(risks, allowRisks(fence, now.Fence)...)
	}

	if np, ok := addedPrompt(now.Prompt, adds); ok {
		risks = append(risks, cardWarning("Its agent runs %s, a prompt this proposal adds: read its text before you add it.",
			riskQuote(np.Nickname)))
	}

	return risks
}

// reachRisks returns the risks of an agent's reach going from was to now.
func reachRisks(was, now agentReach) []Risk {
	switch {
	case now.terminal && !was.terminal:
		return []Risk{cardWarning("Its agent runs in your terminal exactly as a click's launch does: with the profile's " +
			"own permissions, and no fence.")}
	case now.profile && !was.profile:
		return []Risk{cardWarning("Its agent runs in the background with the profile's own permissions: whatever a " +
			"click's launch may do, pushes included, within the fence's hard lines, with nobody there to answer.")}
	case now.profile:
		return nil
	}

	var risks []Risk

	if now.push && !was.push {
		risks = append(risks, cardWarning("Its agent may push to the pull request's own branch: one push, never by force."))
	}

	if now.github && !was.github {
		risks = append(risks, cardWarning("Its agent may run gh on the item: re-run its failed jobs, comment on it, or "+
			"label it."))
	}

	return risks
}

// budgetRisks returns the risk of an agent's budget a run going from was's fence to now's
// above it, or above the default when was set none in its unit.
func budgetRisks(was, now *spec.Fence) []Risk {
	if now == nil || now.Budget == nil || budgetEmpty(now.Budget) {
		return nil
	}

	tokens := now.Budget.Tokens != nil

	base, since := budgetBase(was, tokens)
	if budgetAmount(now.Budget) <= base {
		return nil
	}

	verb := "spend"
	if tokens {
		verb = "use"
	}

	return []Risk{cardWarning("Its agent may %s up to %s a run, %s.", verb, budgetWords(now.Budget), since)}
}

// budgetBase returns the budget a run's budget, in tokens or in dollars, is raised over,
// and its words: was's, set in that unit, else the default.
func budgetBase(was *spec.Fence, tokens bool) (float64, string) {
	switch {
	case was != nil && was.Budget != nil && !budgetEmpty(was.Budget) && (was.Budget.Tokens != nil) == tokens:
		return budgetAmount(was.Budget), "up from " + budgetWords(was.Budget)
	case tokens:
		return catalog.StartRunTokens, "where a budget in tokens starts at " + digitGroups(catalog.StartRunTokens)
	default:
		usd := float64(catalog.DefaultRunCents) / 100

		return usd, "more than the " + amountUSD(usd) + " default"
	}
}

// timeLimitRisks returns the risk of an agent's time limit going from was's fence to
// now's above it, or above the default when was set none.
func timeLimitRisks(was, now *spec.Fence) []Risk {
	if now == nil || now.TimeoutMinutes == nil {
		return nil
	}

	base, since := catalog.DefaultTimeoutMinutes, "longer than the default "+spanMinutes(catalog.DefaultTimeoutMinutes)
	if was != nil && was.TimeoutMinutes != nil {
		base, since = *was.TimeoutMinutes, "up from "+spanMinutes(*was.TimeoutMinutes)
	}

	if *now.TimeoutMinutes <= base {
		return nil
	}

	return []Risk{cardWarning("Its agent may run for up to %s, %s.", spanMinutes(*now.TimeoutMinutes), since)}
}

// allowRisks returns the risk of the extra commands now's fence allows that was's
// didn't.
func allowRisks(was, now *spec.Fence) []Risk {
	if now == nil {
		return nil
	}

	var added []string

	for _, cmd := range now.Allow {
		if was == nil || !slices.Contains(was.Allow, cmd) {
			added = append(added, cmd)
		}
	}

	if len(added) == 0 {
		return nil
	}

	return []Risk{cardWarning("Its agent may also run %s, beyond its tools preset, though never past the fence's hard "+
		"lines.", quotedSeries(added))}
}

// mergeRisks returns the risks of a routine that merges now and didn't, and of each tick
// that lets it merge past the rules now and didn't.
func mergeRisks(was, now spec.Spec) []Risk {
	m, before := mergeOf(now), mergeOf(was)
	if m == nil {
		return nil
	}

	var out []Risk

	if before == nil {
		out = append(out, newMergeRisk(*m, now.Agentless()))
	}

	if m.AllowBypass && (before == nil || !before.AllowBypass) {
		out = append(out, cardWarning("It merges your own pull request past the rules when an approval from someone "+
			"else is all it lacks and you signed every commit, with no click, once you confirm it as you add it and "+
			"turn on Let routines merge your own pull requests past the rules in Autopilot's settings."))
	}

	if m.AllowSensitive && (before == nil || !before.AllowSensitive) {
		out = append(out, cardWarning("It merges so even when the pull request changes workflow, agent, or tooling "+
			"files, which a person reviews otherwise."))
	}

	return out
}

// newMergeRisk is the risk of a routine that merges now, with m, and didn't: once its
// checks pass (agentless) or once its agent's push's do, inside GitHub's own rules unless m
// may go past them.
func newMergeRisk(m spec.MergeStep, agentless bool) Risk {
	method := string(m.Method)

	for _, info := range catalog.MergeMethods() {
		if info.ID == m.Method {
			method = info.Label
		}
	}

	passed := "once the checks on its agent's push pass"
	if agentless {
		passed = "once its checks pass"
	}

	within := ", inside GitHub's own rules"
	if m.AllowBypass {
		within = ""
	}

	return cardWarning("It merges the pull request on GitHub (%s) %s%s.", method, passed, within)
}

// reviewRisks returns the risk of a routine that posts its agent's review further now
// than it did: on GitHub at all, as its verdict, or approving.
func reviewRisks(was, now spec.Spec) []Risk {
	level := reviewLevel(now)
	if level <= reviewLevel(was) {
		return nil
	}

	switch level {
	case reviewComment:
		return []Risk{cardWarning("It posts the agent's review on the pull request as a comment, in your name.")}
	case reviewVerdict:
		return []Risk{cardWarning("It posts the agent's review as its verdict, in your name: a request for changes as " +
			"one, and an approval as a comment.")}
	default:
		return []Risk{cardWarning("It may approve the pull request in your name when the agent's verdict is to approve: " +
			"only a member's or a trusted bot's, with every file checked and none sensitive, and never one a " +
			"routine pushed to.")}
	}
}

// How far a routine posts its agent's review.
const (
	reviewNone = iota
	reviewComment
	reviewVerdict
	reviewApprove
)

// reviewLevel returns how far sp posts its agent's review.
func reviewLevel(sp spec.Spec) int {
	for _, st := range sp.Steps {
		switch r := st.PostReview; {
		case r == nil:
		case r.As == catalog.ReviewAsVerdict && r.AllowApprove:
			return reviewApprove
		case r.As == catalog.ReviewAsVerdict:
			return reviewVerdict
		default:
			return reviewComment
		}
	}

	return reviewNone
}

// githubRisks returns the risks of the GitHub actions a routine sends now that it didn't:
// a rebase Dependabot force-pushes, an update of the branch, a comment as the user, and
// labels added or taken off.
func githubRisks(was, now spec.Spec, adds []NewPrompt, names spec.PromptNames) []Risk {
	before, after := actionsOf(was), actionsOf(now)

	var risks []Risk

	if hasAction(after, catalog.GitHubRebase) && !hasAction(before, catalog.GitHubRebase) {
		risks = append(risks, cardWarning("It comments @dependabot rebase on the pull request, and Dependabot "+
			"force-pushes its branch, which can't be taken back."))
	}

	if hasAction(after, catalog.GitHubUpdateBranch) && !hasAction(before, catalog.GitHubUpdateBranch) {
		risks = append(risks, cardWarning("It merges the base branch into the pull request's branch on GitHub, and the "+
			"merge commit stays."))
	}

	risks = append(risks, replyRisks(before, after, adds, names)...)

	return append(risks, labelRisks(before, after)...)
}

// replyRisks returns the risk of each reply a routine comments now that it didn't.
func replyRisks(before, after []spec.GitHubAction, adds []NewPrompt, names spec.PromptNames) []Risk {
	var risks []Risk

	for _, ref := range replyRefs(after) {
		if slices.ContainsFunc(replyRefs(before), func(r spec.PromptRef) bool { return samePrompt(r, ref) }) {
			continue
		}

		if np, ok := addedPrompt(ref, adds); ok {
			risks = append(risks, cardWarning("It comments %s, a reply this proposal adds, on GitHub as you: read its text "+
				"before you add it.", riskQuote(np.Nickname)))

			continue
		}

		risks = append(risks, cardWarning("It comments your reply %s on GitHub as you: it can be deleted, but GitHub has "+
			"told people by then.", riskQuote(promptWords(ref, names))))
	}

	return risks
}

// labelRisks returns the risk of the labels a routine adds or takes off now that it
// didn't.
func labelRisks(before, after []spec.GitHubAction) []Risk {
	added := newLabels(labelsOf(before, true), labelsOf(after, true))
	removed := newLabels(labelsOf(before, false), labelsOf(after, false))

	var did string

	switch {
	case len(added) > 0 && len(removed) > 0:
		did = "adds " + quotedSeries(added) + ", and takes off " + quotedSeries(removed) + ","
	case len(added) > 0:
		did = "adds " + quotedSeries(added)
	case len(removed) > 0:
		did = "takes off " + quotedSeries(removed)
	default:
		return nil
	}

	return []Risk{cardWarning("It %s on GitHub; a label can start a repository's label-triggered workflows.", did)}
}

// scriptRisks returns the risk of each of the user's scripts a routine runs now that it
// didn't.
func scriptRisks(was, now spec.Spec) []Risk {
	var (
		risks []Risk
		seen  []int64
	)

	for _, st := range now.Steps {
		if st.Script == nil || slices.Contains(seen, st.Script.ID) || runsScript(was, st.Script.ID) {
			continue
		}

		seen = append(seen, st.Script.ID)
		risks = append(risks, cardWarning("It runs your script %s in the item's clone, as Repo scripts runs it.",
			riskQuote(st.Script.Name)))
	}

	return risks
}

// limitRisks returns the risks of a routine's limits going from was to now: more tries,
// more runs a day, a shorter wait between tries, wider active hours or none, and a
// higher daily budget or none. A limit left out is its default.
func limitRisks(was, now spec.Limits) []Risk {
	var risks []Risk

	if n, w := limitOr(now.TriesPerItem, catalog.DefaultTries), limitOr(was.TriesPerItem, catalog.DefaultTries); n > w {
		risks = append(risks, cardWarning("It may try up to %d times on one item, %s.", n,
			limitSince(was.TriesPerItem, catalog.DefaultTries, "more than", strconv.Itoa)))
	}

	if n, w := limitOr(now.RunsPerDay, catalog.DefaultRunsPerDay), limitOr(was.RunsPerDay, catalog.DefaultRunsPerDay); n > w {
		risks = append(risks, cardWarning("It may run up to %d times a day, %s.", n,
			limitSince(was.RunsPerDay, catalog.DefaultRunsPerDay, "more than", strconv.Itoa)))
	}

	risks = append(risks, waitRisks(was.WaitMinutes, now.WaitMinutes)...)
	risks = append(risks, hoursRisks(was.ActiveHours, now.ActiveHours)...)

	return append(risks, dailyBudgetRisks(was.BudgetPerDay, now.BudgetPerDay)...)
}

// waitRisks returns the risk of a routine waiting less between tries on one item than
// was, or the default, says.
func waitRisks(was, now *int) []Risk {
	n, w := limitOr(now, catalog.DefaultWaitMinutes), limitOr(was, catalog.DefaultWaitMinutes)

	switch {
	case n >= w:
		return nil
	case n > 0:
		return []Risk{cardWarning("It waits %s between tries on one item, %s.", spanMinutes(n),
			limitSince(was, catalog.DefaultWaitMinutes, "less than", spanMinutes))}
	case was == nil:
		return []Risk{cardWarning("It tries again on one item without waiting, where the default waits %s.",
			spanMinutes(catalog.DefaultWaitMinutes))}
	default:
		return []Risk{cardWarning("It tries again on one item without waiting, where it waited %s.", spanMinutes(*was))}
	}
}

// limitOr returns a limit's value, or def when it is left out.
func limitOr(n *int, def int) int {
	if n == nil {
		return def
	}

	return *n
}

// limitSince says what a limit moved from: the default, when the routine left it out
// before, else what it was, compared as than says, in words.
func limitSince(was *int, def int, than string, words func(int) string) string {
	if was == nil {
		return than + " the default " + words(def)
	}

	if than == "less than" {
		return "down from " + words(*was)
	}

	return "up from " + words(*was)
}

// hoursRisks returns the risk of a routine's active hours going from was to now: taken
// away, or letting it start at a time was didn't.
func hoursRisks(was, now *spec.ActiveHours) []Risk {
	switch {
	case was == nil:
		return nil
	case now == nil:
		return []Risk{cardWarning("It may start at any hour: this takes away its active hours (%s).", activeHoursWords(was))}
	case !widens(was, now):
		return nil
	}

	return []Risk{cardWarning("Its active hours widen from %s to %s.", activeHoursWords(was), activeHoursWords(now))}
}

// widens reports whether the active hours now let a routine start at a minute of the week
// was doesn't.
func widens(was, now *spec.ActiveHours) bool {
	for day := range catalog.Weekdays() {
		for minute := range 24 * 60 {
			if startsAt(now, day, minute) && !startsAt(was, day, minute) {
				return true
			}
		}
	}

	return false
}

// startsAt reports whether a routine with active hours h may start on the day of the week
// at day (0 is Monday) at minute past midnight, as the engine reads them: no hours is
// always, no days is every day, and hours whose From is after their To run past midnight
// into the next day.
func startsAt(h *spec.ActiveHours, day, minute int) bool {
	if h == nil {
		return true
	}

	from, okFrom := clockMinutes(h.From)
	to, okTo := clockMinutes(h.To)
	days := catalog.Weekdays()

	switch {
	case !okFrom || !okTo:
		return true
	case from <= to:
		return onWeekday(h.Days, days[day]) && minute >= from && minute < to
	case minute >= from:
		return onWeekday(h.Days, days[day])
	default:
		return minute < to && onWeekday(h.Days, days[(day+6)%7])
	}
}

// clockMinutes reads "HH:MM" as minutes past midnight.
func clockMinutes(s string) (int, bool) {
	hh, mm, ok := strings.Cut(s, ":")
	h, errH := strconv.Atoi(hh)
	m, errM := strconv.Atoi(mm)

	return h*60 + m, ok && errH == nil && errM == nil
}

// onWeekday reports whether days holds day; no days is every day.
func onWeekday(days []catalog.Weekday, day catalog.Weekday) bool {
	return len(days) == 0 || slices.Contains(days, day)
}

// activeHoursWords says active hours: "weekdays 09:00–17:00".
func activeHoursWords(h *spec.ActiveHours) string {
	return activeDaysWords(h.Days) + " " + h.From + "–" + h.To
}

// activeDaysWords says the days of active hours: "every day", "weekdays", "weekends", or
// the days, "Mon, Wed, and Fri".
func activeDaysWords(days []catalog.Weekday) string {
	all := catalog.Weekdays()
	sorted := slices.Clone(days)
	slices.SortFunc(sorted, func(a, b catalog.Weekday) int { return slices.Index(all, a) - slices.Index(all, b) })
	sorted = slices.Compact(sorted)

	switch {
	case len(sorted) == 0 || len(sorted) == len(all):
		return "every day"
	case slices.Equal(sorted, all[:5]):
		return "weekdays"
	case slices.Equal(sorted, all[5:]):
		return "weekends"
	}

	words := make([]string, 0, len(sorted))
	for _, d := range sorted {
		words = append(words, textutil.Capitalize(string(d)))
	}

	return textutil.Series(words)
}

// dailyBudgetRisks returns the risk of a routine's own daily budget going from was to now:
// taken away, raised, or set in another unit.
func dailyBudgetRisks(was, now *spec.Budget) []Risk {
	switch {
	case was == nil || budgetEmpty(was):
		return nil
	case now == nil || budgetEmpty(now):
		return []Risk{cardWarning("Its own daily budget (%s) is taken away, so only the daily cap in Settings holds what it "+
			"spends.", budgetWords(was))}
	case (was.Tokens != nil) != (now.Tokens != nil):
		return []Risk{cardWarning("Its own daily budget changes from %s to %s: a budget holds back only the runs "+
			"budgeted in its unit.", budgetWords(was), budgetWords(now))}
	case budgetAmount(now) > budgetAmount(was):
		return []Risk{cardWarning("Its own daily budget goes up from %s to %s.", budgetWords(was), budgetWords(now))}
	default:
		return nil
	}
}

// modeRisks returns the risk of new routines starting in Autopilot.
func modeRisks(before, after autopilotprefs.Config) []Risk {
	if after.DefaultMode != autopilotprefs.ModeAuto || before.DefaultMode == autopilotprefs.ModeAuto {
		return nil
	}

	return []Risk{cardWarning("New routines will start in Autopilot, running every step at once, with nobody asked first.")}
}

// outageRisks returns the risk of agents starting through their provider's outage.
func outageRisks(before, after autopilotprefs.Config) []Risk {
	if after.HoldOnOutage || !before.HoldOnOutage {
		return nil
	}

	return []Risk{cardWarning("Routines' agents will start even while their provider's status page reports a major " +
		"outage.")}
}

// historyRisks returns the risks of a shorter history, or shorter run logs, each of which
// deletes what is older.
func historyRisks(before, after autopilotprefs.Config) []Risk {
	var risks []Risk

	if after.HistoryDays < before.HistoryDays {
		risks = append(risks, cardWarning("History kept drops from %s to %s: the routines' history older than that is "+
			"deleted, for good.", spanDays(before.HistoryDays), spanDays(after.HistoryDays)))
	}

	if after.LogDays < before.LogDays {
		risks = append(risks, cardWarning("Run logs kept drops from %s to %s: older agent run logs are deleted, for good.",
			spanDays(before.LogDays), spanDays(after.LogDays)))
	}

	return risks
}

// countRisks returns the risks of more agents at once, agent runs a day, or GitHub
// actions a day.
func countRisks(before, after autopilotprefs.Config) []Risk {
	var risks []Risk

	for _, c := range []struct {
		was, now int
		text     string
	}{
		{before.AgentsAtOnce, after.AgentsAtOnce, "Agents at once goes up from %d to %d: more of your routines' agents " +
			"run at the same time."},
		{before.RunsPerDay, after.RunsPerDay, "Agent runs a day goes up from %d to %d: your routines may start that " +
			"many agent runs a day."},
		{before.ActionsPerDay, after.ActionsPerDay, "GitHub actions a day goes up from %d to %d: your routines may " +
			"send GitHub that many actions a day."},
		{before.ActionsPerHour, after.ActionsPerHour, "GitHub actions an hour goes up from %d to %d: your routines " +
			"may send GitHub that many actions an hour."},
	} {
		if c.now > c.was {
			risks = append(risks, cardWarning(c.text, c.was, c.now))
		}
	}

	return risks
}

// capRisks returns the risk of the daily cap taken away, raised, or set in another unit.
func capRisks(before, after autopilotprefs.Config) []Risk {
	was, now := before.DailyCap, after.DailyCap

	switch {
	case was.Off():
		return nil
	case now.Off():
		return []Risk{cardWarning("The daily cap of %s is taken away: nothing caps what your routines' agents spend in a day "+
			"but each run's and each routine's own budget.", capAmount(was))}
	case was.Unit != now.Unit:
		return []Risk{cardWarning("The daily cap changes from %s to %s: a cap holds back only the runs budgeted in its "+
			"unit.", capAmount(was), capAmount(now))}
	case now.Amount > was.Amount:
		return []Risk{cardWarning("The daily cap goes up from %s to %s.", capAmount(was), capAmount(now))}
	default:
		return nil
	}
}

// catchUpRisks returns the risk of longer gaps caught up on without asking.
func catchUpRisks(before, after autopilotprefs.Config) []Risk {
	if after.CatchUpHours <= before.CatchUpHours {
		return nil
	}

	return []Risk{cardWarning("Ask before catching up after goes up from %s to %s: after a gap shorter than that, "+
		"routines catch up without asking you.", spanHours(before.CatchUpHours), spanHours(after.CatchUpHours))}
}

// muteRisk returns the risk of muting target, a repository or an org.
func muteRisk(target string) Risk {
	what := target
	if !strings.Contains(target, "/") {
		what += " and every repository it owns"
	}

	return cardWarning("Mutes %s: its items wait in each list's Muted tab, count nowhere, show no banner, and nothing "+
		"follows their checks.", what)
}

// sameTarget reports whether two rules' targets are one repository or org, ignoring case.
func sameTarget(a, b string) bool {
	x, errA := reporules.ParseTarget(a)
	y, errB := reporules.ParseTarget(b)

	return errA == nil && errB == nil && x.Key() == y.Key()
}

// cardWarning is a RiskWarning saying format, filled in with args.
func cardWarning(format string, args ...any) Risk {
	return Risk{Level: RiskWarning, Text: fmt.Sprintf(format, args...)}
}

// agentOf returns sp's agent step, nil for none.
func agentOf(sp spec.Spec) *spec.AgentStep {
	for _, st := range sp.Steps {
		if st.Agent != nil {
			return st.Agent
		}
	}

	return nil
}

// mergeOf returns sp's Merge it step, nil for none.
func mergeOf(sp spec.Spec) *spec.MergeStep {
	for _, st := range sp.Steps {
		if st.Merge != nil {
			return st.Merge
		}
	}

	return nil
}

// presetByID returns the tools preset id names.
func presetByID(id catalog.Preset) (catalog.PresetInfo, bool) {
	for _, p := range catalog.Presets() {
		if p.ID == id {
			return p, true
		}
	}

	return catalog.PresetInfo{}, false
}

// actionsOf returns every action of sp's GitHub steps.
func actionsOf(sp spec.Spec) []spec.GitHubAction {
	var out []spec.GitHubAction
	for _, st := range sp.Steps {
		out = append(out, st.GitHub...)
	}

	return out
}

// hasAction reports whether actions hold one of kind.
func hasAction(actions []spec.GitHubAction, kind catalog.GitHubActionKind) bool {
	return slices.ContainsFunc(actions, func(a spec.GitHubAction) bool { return a.Kind() == kind })
}

// replyRefs returns the prompts actions comment as replies.
func replyRefs(actions []spec.GitHubAction) []spec.PromptRef {
	var out []spec.PromptRef

	for _, a := range actions {
		if a.Reply != nil {
			out = append(out, a.Reply.Prompt)
		}
	}

	return out
}

// labelsOf returns the labels actions add, or take off.
func labelsOf(actions []spec.GitHubAction, add bool) []string {
	var out []string

	for _, a := range actions {
		switch {
		case a.Labels == nil:
		case add:
			out = append(out, a.Labels.Add...)
		default:
			out = append(out, a.Labels.Remove...)
		}
	}

	return out
}

// newLabels returns the labels of after that before doesn't hold, ignoring case, as
// GitHub matches a label, each once.
func newLabels(before, after []string) []string {
	var out []string

	for _, l := range after {
		if !textutil.ContainsFold(before, l) && !textutil.ContainsFold(out, l) {
			out = append(out, l)
		}
	}

	return out
}

// runsScript reports whether sp runs the user's script id.
func runsScript(sp spec.Spec, id int64) bool {
	return slices.ContainsFunc(sp.Steps, func(st spec.Step) bool { return st.Script != nil && st.Script.ID == id })
}

// samePrompt reports whether two references name one prompt: one starter, or one
// nickname, ignoring case.
func samePrompt(a, b spec.PromptRef) bool {
	if a.Starter != "" || b.Starter != "" {
		return a.Starter == b.Starter
	}

	return a.ItemDefault == b.ItemDefault && strings.EqualFold(a.Nickname, b.Nickname)
}

// namesPrompt reports whether ref names the prompt whose nickname is nick.
func namesPrompt(ref spec.PromptRef, nick string) bool {
	return ref.Starter == "" && !ref.ItemDefault && strings.EqualFold(strings.TrimSpace(ref.Nickname), nick)
}

// addedPrompt returns the prompt of adds that ref names by its nickname.
func addedPrompt(ref spec.PromptRef, adds []NewPrompt) (NewPrompt, bool) {
	for _, np := range adds {
		if nick := strings.TrimSpace(np.Nickname); nick != "" && namesPrompt(ref, nick) {
			return np, true
		}
	}

	return NewPrompt{}, false
}

// promptWords names the prompt ref names: a starter by its nickname as names gives it,
// else its key as words; a nickname as it is.
func promptWords(ref spec.PromptRef, names spec.PromptNames) string {
	if ref.Starter == "" {
		return ref.Nickname
	}

	if names != nil {
		if n := names(ref); n != "" {
			return n
		}
	}

	return textutil.Capitalize(strings.ReplaceAll(ref.Starter, "-", " "))
}

// sameShape reports whether a and b encode alike.
func sameShape(a, b any) bool {
	x, errA := json.Marshal(a)
	y, errB := json.Marshal(b)

	return errA == nil && errB == nil && bytes.Equal(x, y)
}

// riskQuote writes a value the risk names, as one line in curly quotes, cut when long.
func riskQuote(s string) string {
	return "“" + textutil.Truncate(textutil.OneLine(s), 60) + "”"
}

// quotedSeries writes values as a series of quoted values, at most five, then how many
// more.
func quotedSeries(values []string) string {
	const shown = 5

	quoted := make([]string, 0, min(len(values), shown)+1)
	for _, v := range values[:min(len(values), shown)] {
		quoted = append(quoted, riskQuote(v))
	}

	if len(values) > shown {
		quoted = append(quoted, fmt.Sprintf("%d more", len(values)-shown))
	}

	return textutil.Series(quoted)
}

// budgetEmpty reports a budget that sets no amount.
func budgetEmpty(b *spec.Budget) bool {
	return b.USD == nil && b.Tokens == nil
}

// budgetAmount returns a budget's amount, in its unit.
func budgetAmount(b *spec.Budget) float64 {
	if b.Tokens != nil {
		return float64(*b.Tokens)
	}

	return *b.USD
}

// budgetWords says a budget: "$3.00", "2,000,000 tokens".
func budgetWords(b *spec.Budget) string {
	if b.Tokens != nil {
		return digitGroups(*b.Tokens) + " tokens"
	}

	return amountUSD(*b.USD)
}

// amountUSD says dollars to the cent: "$3.00".
func amountUSD(usd float64) string {
	return fmt.Sprintf("$%.2f", usd)
}

// capAmount says the daily cap as Settings does: "Off", "$20", "2,000,000 tokens".
func capAmount(c autopilotprefs.DailyCap) string {
	switch {
	case c.Off():
		return "Off"
	case c.Unit == autopilotprefs.UnitTokens:
		return digitGroups(int64(c.Amount)) + " tokens"
	default:
		return "$" + digitGroups(int64(c.Amount))
	}
}

// settingSwitch says a switch as Settings does: "On" or "Off".
func settingSwitch(on bool) string {
	if on {
		return "On"
	}

	return "Off"
}

// spanMinutes says a length of minutes: "45 minutes", "1 hour", "2 hours".
func spanMinutes(n int) string {
	if n > 0 && n%60 == 0 {
		return textutil.Counted(n/60, "hour", "hours")
	}

	return textutil.Counted(n, "minute", "minutes")
}

// spanHours says a length of hours as Settings does: "24 hours", "1 week".
func spanHours(n int) string {
	if n == 7*24 {
		return "1 week"
	}

	return textutil.Counted(n, "hour", "hours")
}

// spanDays says a length of days: "1 day", "90 days".
func spanDays(n int) string {
	return textutil.Counted(n, "day", "days")
}

// digitGroups writes n with commas between its thousands: "2,000,000".
func digitGroups[N int | int64](n N) string {
	s := strconv.FormatInt(int64(n), 10)

	sign, digits := "", s
	if n < 0 {
		sign, digits = "-", s[1:]
	}

	for i := len(digits) - 3; i > 0; i -= 3 {
		digits = digits[:i] + "," + digits[i:]
	}

	return sign + digits
}
