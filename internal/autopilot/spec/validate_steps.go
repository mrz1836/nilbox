package spec

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// before is what the steps before one hold, for checking its order.
type before struct {
	// agent is the agent step before, nil for none.
	agent *AgentStep

	// wait reports a Wait for the checks before, and waitOnPush that the latest one runs
	// only if the agent pushed, so the checks it follows are on the agent's own commit.
	wait, waitOnPush bool

	// changesBranch reports a step before that may change the pull request's branch: a
	// rebase, an update of the branch, or one of the user's scripts.
	changesBranch bool
}

// steps checks each step, and their order.
func (v *validator) steps(steps []Step, kinds []item.Kind) {
	switch {
	case len(steps) == 0:
		v.add(PartSteps, "", "add a step, so the routine does something")
	case len(steps) > MaxSteps:
		v.add(PartSteps, "", "use at most %d steps", MaxSteps)
	}

	var prior before

	for i, st := range steps {
		field := fmt.Sprintf("[%d]", i)

		kind := v.stepKind(field, st, kinds)
		if kind == "" {
			continue
		}

		v.onlyIf(field, i, st.OnlyIf, prior)
		v.order(field, kind, st, prior)
		v.stepSettings(field, kind, st)

		if kind == catalog.StepAgent && prior.agent != nil {
			v.add(PartSteps, field, "use one agent step per routine; two agents on one item take two routines")
		}

		prior = prior.then(kind, st)
	}
}

// then returns what the steps before the next one hold, once st, of kind, is among them.
func (b before) then(kind catalog.StepKind, st Step) before {
	switch kind {
	case catalog.StepAgent:
		b.agent = st.Agent
	case catalog.StepWaitForChecks:
		b.wait, b.waitOnPush = true, st.OnlyIf == catalog.OutcomePushed
	case catalog.StepScript:
		b.changesBranch = true
	case catalog.StepGitHub:
		b.changesBranch = b.changesBranch || slices.ContainsFunc(st.GitHub, func(a GitHubAction) bool {
			return a.Rebase != nil || a.UpdateBranch != nil
		})
	case catalog.StepNotify, catalog.StepPin, catalog.StepSnooze, catalog.StepDismiss, catalog.StepRestore,
		catalog.StepMarkDone, catalog.StepMerge, catalog.StepPostReview, catalog.StepTellMe:
	}

	return b
}

// stepKind returns the step's one kind, recording a problem for a step that sets none or
// several, or one that can't act on the kinds the watch holds.
func (v *validator) stepKind(field string, st Step, kinds []item.Kind) catalog.StepKind {
	set := st.Kinds()
	if len(set) != 1 {
		v.add(PartSteps, field, "a step is one kind of step; this one sets %d", len(set))

		return ""
	}

	info, _ := catalog.StepByKind(set[0])
	if !fits(kinds, info.Applies) {
		v.add(PartSteps, field, "%s acts only on pull requests, and this routine watches only %s",
			info.Label, kindWords(kinds))
	}

	return set[0]
}

// onlyIf checks a step's Only if: an outcome an earlier step ends with.
func (v *validator) onlyIf(field string, i int, onlyIf catalog.Outcome, prior before) {
	if onlyIf == "" {
		return
	}

	if msg := onlyIfProblem(i, onlyIf, prior); msg != "" {
		v.add(PartSteps, field+".onlyIf", "%s", msg)
	}
}

// onlyIfProblem says what is wrong with an Only if, or "".
func onlyIfProblem(i int, onlyIf catalog.Outcome, prior before) string {
	out, ok := catalog.OutcomeByID(onlyIf)

	switch {
	case !ok:
		return fmt.Sprintf("%q isn't an outcome a step ends with", onlyIf)
	case i == 0:
		return "the first step has no step before it to follow"
	case out.From == catalog.StepWaitForChecks && !prior.wait:
		return "only if " + out.Label + " needs a Wait for the checks before it"
	case out.From == catalog.StepWaitForChecks:
		return ""
	default:
		return agentOutcomeProblem(onlyIf, out, prior)
	}
}

// agentOutcomeProblem says what is wrong with an Only if on the agent step's outcome, or
// "": a verdict needs a read-only agent step, and whether it pushed one that may push.
func agentOutcomeProblem(onlyIf catalog.Outcome, out catalog.OutcomeInfo, prior before) string {
	verdict := onlyIf == catalog.OutcomeApprove || onlyIf == catalog.OutcomeRequestChanges

	switch {
	case prior.agent == nil:
		return "only if " + out.Label + " needs an agent step before it"
	case verdict && !prior.readOnly():
		return "only a read-only agent step in the background gives a verdict"
	case !verdict && !prior.mayPush():
		return "the agent step before can't push, so whether it pushed says nothing"
	default:
		return ""
	}
}

// mayPush and readOnly report an agent step before that may push, and one that is read
// only.
func (b before) mayPush() bool  { return b.agent != nil && b.agent.MayPush() }
func (b before) readOnly() bool { return b.agent != nil && b.agent.ReadOnly() }

// order holds a step to where it may sit.
func (v *validator) order(field string, kind catalog.StepKind, st Step, prior before) {
	if prior.agent != nil && prior.agent.Place() == catalog.WhereTerminal &&
		kind != catalog.StepNotify && kind != catalog.StepTellMe {
		v.add(PartSteps, field, "a terminal run has no end the app can see, so only a notice can follow it")

		return
	}

	switch {
	case kind == catalog.StepWaitForChecks:
		v.waitOrder(field, st, prior)
	case kind == catalog.StepPostReview && !prior.readOnly():
		v.add(PartSteps, field, "Post the agent's review follows a read-only agent step in the background")
	case kind == catalog.StepMerge:
		v.mergeOrder(field, st, prior)
	}
}

// waitOrder holds Wait for the checks to after an agent step that may push, and to the
// agent's push: it never runs only if the agent didn't push.
func (v *validator) waitOrder(field string, st Step, prior before) {
	switch {
	case !prior.mayPush():
		v.add(PartSteps, field, "Wait for the checks follows an agent step in the background that may push")
	case st.OnlyIf == catalog.OutcomeNotPushed:
		v.add(PartSteps, field+".onlyIf", "Wait for the checks follows the agent's push, so it can't run only if "+
			"the agent didn't push")
	}
}

// mergeOrder holds Merge it to one of two places. After an agent step, it follows a Wait
// for the checks on the agent's own push, and runs when they passed: a pass on a commit the
// agent never made, such as a flaky check run again, is no reason to merge. With no agent
// step and no wait, it merges the commit the routine's checks passed on (agentlessMerge).
// Only there may it go past the rules (pastRules).
func (v *validator) mergeOrder(field string, st Step, prior before) {
	v.pastRules(field+".merge", *st.Merge)

	if v.agentless {
		v.agentlessMerge(field, st, prior)

		return
	}

	switch {
	case !prior.wait:
		v.add(PartSteps, field, "Merge it follows a Wait for the checks")
	case !prior.waitOnPush:
		v.add(PartSteps, field, `Merge it follows a Wait for the checks on the agent's push: set the latest `+
			`wait's "onlyIf" to "pushed"`)
	}

	if st.OnlyIf != catalog.OutcomePassedChecks {
		v.add(PartSteps, field+".onlyIf", `Merge it runs only if the checks passed: set "onlyIf" to "passed"`)
	}
}

// agentlessMerge holds a Merge it with no agent step to a routine that fires when the
// checks pass, once per head commit, as its trigger's own once-per is, with no Only if, and
// with nothing before it that may change the branch: the commit it merges is the one whose
// checks passed.
func (v *validator) agentlessMerge(field string, st Step, prior before) {
	switch {
	case v.event != catalog.EventChecksPassed:
		v.add(PartSteps, field, "Merge it follows a Wait for the checks on an agent's push, or, with no agent step, "+
			"comes in a routine that fires when the checks pass")
	case v.oncePer != "" && v.oncePer != catalog.OncePerHeadCommit:
		v.add(PartLimits, "oncePer", "a routine that merges with no agent step acts once per head commit: leave out "+
			`"oncePer", or set it to "headCommit"`)
	}

	if st.OnlyIf != "" {
		v.add(PartSteps, field+".onlyIf", `Merge it with no agent step merges what the checks passed on: leave out "onlyIf"`)
	}

	if prior.changesBranch {
		v.add(PartSteps, field, "Merge it with no agent step merges the commit whose checks passed, so no rebase, "+
			"update of the branch, or script may come before it")
	}
}

// pastRules holds Merge it past the rules to a Merge it with no agent step (agentlessMerge),
// merging when GitHub allows, never by auto-merge; and sensitive files to one that goes
// past the rules at all.
func (v *validator) pastRules(field string, m MergeStep) {
	if !m.AllowBypass && !m.AllowSensitive {
		return
	}

	switch {
	case !v.agentless:
		v.add(PartSteps, field+".allowBypass", "only Merge it with no agent step, in a routine that fires when the "+
			"checks pass, can merge past the rules")
	case m.How != catalog.MergeWhenMergeable:
		v.add(PartSteps, field+".how", `a merge past the rules merges when GitHub allows: set "how" to "whenMergeable"`)
	}

	if m.AllowSensitive && !m.AllowBypass {
		v.add(PartSteps, field+".allowSensitive", `sensitive files go past the rules only with "allowBypass"`)
	}
}

// stepSettings checks the settings of a step's kind.
func (v *validator) stepSettings(field string, kind catalog.StepKind, st Step) {
	checks := map[catalog.StepKind]func(){
		catalog.StepNotify: func() {
			if msg := checkText(st.Notify.Note, MaxTextRunes); st.Notify.Note != "" && msg != "" {
				v.add(PartSteps, field+".notify.note", "%s", msg)
			}
		},
		catalog.StepSnooze: func() {
			v.inRange(PartSteps, field+".snooze.days", st.Snooze.Days, catalog.MinSnoozeDays, catalog.MaxSnoozeDays)
		},
		catalog.StepGitHub:        func() { v.github(field+".github", st.GitHub) },
		catalog.StepAgent:         func() { v.agent(field+".agent", *st.Agent) },
		catalog.StepWaitForChecks: func() { v.wait(field+".waitForChecks", *st.WaitForChecks) },
		catalog.StepMerge:         func() { v.merge(field+".merge", *st.Merge) },
		catalog.StepPostReview:    func() { v.review(field+".postReview", *st.PostReview) },
		catalog.StepTellMe:        func() { v.tell(field+".tellMe", *st.TellMe) },
		catalog.StepScript:        func() { v.script(field+".script", *st.Script) },
	}

	if check, ok := checks[kind]; ok {
		check()
	}
}

// github checks a GitHub step's actions: one to MaxActions, each one kind, no kind twice.
func (v *validator) github(field string, actions []GitHubAction) {
	if len(actions) == 0 || len(actions) > MaxActions {
		v.add(PartSteps, field, "give 1 to %d actions", MaxActions)
	}

	seen := make([]catalog.GitHubActionKind, 0, len(actions))

	for i, a := range actions {
		v.action(fmt.Sprintf("%s[%d]", field, i), a, seen)
		seen = append(seen, a.Kind())
	}
}

// action checks one GitHub action, given the kinds the step's earlier actions are.
func (v *validator) action(field string, a GitHubAction, seen []catalog.GitHubActionKind) {
	kind := a.Kind()

	switch {
	case kind == "":
		v.add(PartSteps, field, "an action is one kind of action; this one sets %d", len(a.Kinds()))
	case slices.Contains(seen, kind):
		v.add(PartSteps, field, "the step already has this action")
	case (a.Rebase != nil || a.UpdateBranch != nil) && !fits(v.kinds, isPR):
		v.add(PartSteps, field, "only a pull request has a branch, and this routine watches only %s", kindWords(v.kinds))
	case a.Reply != nil:
		v.prompt(field+".reply.prompt", a.Reply.Prompt, true)
	case a.Labels != nil:
		v.labels(field+".labels", *a.Labels)
	}
}

// labels checks a labels action: some to add or remove, none in both.
func (v *validator) labels(field string, l LabelsAction) {
	if len(l.Add) == 0 && len(l.Remove) == 0 {
		v.add(PartSteps, field, "name a label to add or to remove")
	}

	for _, part := range []struct {
		name   string
		labels []string
	}{{"add", l.Add}, {"remove", l.Remove}} {
		if len(part.labels) > MaxLabels {
			v.add(PartSteps, field+"."+part.name, "at most %d labels", MaxLabels)
		}

		for i, label := range part.labels {
			v.label(PartSteps, fmt.Sprintf("%s.%s[%d]", field, part.name, i), label)
		}
	}

	for _, a := range l.Add {
		if textutil.ContainsFold(l.Remove, a) {
			v.add(PartSteps, field+".remove", "%q is both added and removed", a)
		}
	}
}

// agent checks an agent step's settings.
func (v *validator) agent(field string, a AgentStep) {
	v.agentName(field+".agent", a.AgentID(), true)
	v.prompt(field+".prompt", a.Prompt, false)

	switch place := a.Place(); {
	case !member(place):
		v.add(PartSteps, field+".where", "run it %s", choices[catalog.Where]())
	case place == catalog.WhereBackground && (a.PlanMode != nil || a.Worktree != nil):
		v.add(PartSteps, field, "in the background the tools decide plan mode and the worktree is always on")
	case place == catalog.WhereTerminal && a.Fence != nil:
		v.add(PartSteps, field+".fence", "a terminal run has a click's access; the fence holds in the background")
	}

	v.name(field+".profile", a.Profile)
	v.name(field+".model", a.Model)
	v.name(field+".effort", a.Effort)
	v.fallbacks(field+".fallbacks", a.Fallbacks)
	v.launchRows(field+".launchFor", a.LaunchFor)

	if a.Fence != nil {
		v.fence(field+".fence", *a.Fence)
	}
}

// fallbacks checks an agent step's fallback profiles.
func (v *validator) fallbacks(field string, refs []ProfileRef) {
	if len(refs) > catalog.MaxFallbacks {
		v.add(PartSteps, field, "use at most %d fallbacks", catalog.MaxFallbacks)
	}

	for i, r := range refs {
		f := fmt.Sprintf("%s[%d]", field, i)
		v.agentName(f+".agent", r.Agent, false)

		if msg := checkText(r.Profile, MaxTextRunes); msg != "" {
			v.add(PartSteps, f+".profile", "%s", msg)
		}
	}
}

// launchRows checks an agent step's rows per org or repository: a target each, once,
// that sets something.
func (v *validator) launchRows(field string, rows []LaunchRow) {
	if len(rows) > MaxLaunchRows {
		v.add(PartSteps, field, "use at most %d rows", MaxLaunchRows)
	}

	seen := map[string]bool{}

	for i, r := range rows {
		f := fmt.Sprintf("%s[%d]", field, i)

		switch key := strings.ToLower(r.Target); {
		case !validTarget(r.Target):
			v.add(PartSteps, f+".target", `name an org, "acme-org", or a repository, "acme-org/api"`)
		case seen[key]:
			v.add(PartSteps, f+".target", "%s already has a row", r.Target)
		default:
			seen[key] = true
		}

		if r.Agent == "" && r.Profile == "" && r.Model == "" && r.Effort == "" {
			v.add(PartSteps, f, "set the agent, the profile, the model, or the effort for %s", r.Target)
		}

		if r.Agent != "" {
			v.agentName(f+".agent", r.Agent, true)
		}

		v.name(f+".profile", r.Profile)
		v.name(f+".model", r.Model)
		v.name(f+".effort", r.Effort)
	}
}

// fence checks a background run's fence.
func (v *validator) fence(field string, f Fence) {
	if f.Budget != nil {
		v.budget(PartSteps, field+".budget", *f.Budget, budgetRange{
			minCents: catalog.MinRunCents, maxCents: catalog.MaxRunCents,
			minTokens: catalog.MinRunTokens, maxTokens: catalog.MaxRunTokens,
		})
	}

	v.inRange(PartSteps, field+".timeoutMinutes", f.TimeoutMinutes, catalog.MinTimeoutMinutes,
		catalog.MaxTimeoutMinutes)

	if !member(f.Tools) {
		v.add(PartSteps, field+".tools", "%q isn't a tools preset", f.Tools)
	}

	v.texts(PartSteps, field+".allow", f.Allow, MaxCommands)
	v.texts(PartSteps, field+".deny", f.Deny, MaxCommands)
}

// wait checks a Wait for the checks.
func (v *validator) wait(field string, w WaitStep) {
	v.inRange(PartSteps, field+".settleMinutes", w.SettleMinutes, catalog.MinSettleMinutes, catalog.MaxSettleMinutes)
	v.inRange(PartSteps, field+".giveUpHours", w.GiveUpHours, catalog.MinGiveUpHours, catalog.MaxGiveUpHours)
}

// merge checks a Merge it: how it merges, and its method, if any.
func (v *validator) merge(field string, m MergeStep) {
	if !member(m.How) {
		v.add(PartSteps, field+".how", "merge %s", choices[catalog.MergeHow]())
	}

	if !member(m.Method) {
		v.add(PartSteps, field+".method", "pick a merge method: %s, or leave it out for the repository's own",
			series(valuesOf[catalog.MergeMethod](), "or"))
	}
}

// review checks a Post the agent's review: how it posts, and approving only with a
// verdict.
func (v *validator) review(field string, r ReviewStep) {
	switch {
	case !member(r.As):
		v.add(PartSteps, field+".as", "post it as %s", choices[catalog.ReviewAs]())
	case r.AllowApprove && r.As != catalog.ReviewAsVerdict:
		v.add(PartSteps, field+".allowApprove", "only a review posted as its verdict can approve")
	}
}

// tell checks a Tell me.
func (v *validator) tell(field string, t TellStep) {
	if !member(t.When) {
		v.add(PartSteps, field+".when", "say %s", choices[catalog.TellWhen]())
	}
}

// script checks Run one of my scripts: a script, by its ID and, once picked since UUIDs,
// its UUID, with the name it was picked under.
func (v *validator) script(field string, s ScriptStep) {
	if s.ID <= 0 {
		v.add(PartSteps, field+".id", "pick one of your scripts")

		return
	}

	if s.UUID != "" && !canonicalUUID(s.UUID) {
		v.add(PartSteps, field+".uuid", "%q isn't a script's UUID", s.UUID)
	}

	if msg := checkText(s.Name, MaxTextRunes); msg != "" {
		v.add(PartSteps, field+".name", "the script's name: %s", msg)
	}
}

// isPR reports a pull request's kind.
func isPR(k item.Kind) bool {
	return k == item.KindPR
}
