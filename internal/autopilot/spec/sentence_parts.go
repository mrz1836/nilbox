package spec

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// watchPhrase says what the watch's sources hold beyond their kind: "by Dependabot in
// your account and acme-org", or several sources' words joined with "or".
func watchPhrase(w Watch) string {
	phrases := make([]string, 0, len(w.Sources))

	for _, src := range w.Sources {
		var p string
		if src.Search != nil {
			p = searchPhrase(*src.Search)
		} else {
			p = listsPhrase(src.Lists)
		}

		if p != "" {
			phrases = append(phrases, p)
		}
	}

	if len(phrases) < 2 {
		return strings.Join(phrases, "")
	}

	return strings.Join(phrases, ", or ")
}

// listsPhrase says a source's inbox lists: "in your Review requested and Assigned lists".
func listsPhrase(lists []catalog.ListSource) string {
	names := make([]string, 0, len(lists))

	for _, l := range lists {
		if info, ok := catalog.ListSourceByID(l); ok {
			names = append(names, info.Label)
		}
	}

	if len(names) == 0 {
		return ""
	}

	return "in your " + series(names, "and") + " " + map[bool]string{true: "list", false: "lists"}[len(names) == 1]
}

// searchPhrase says a search's fields in the order a sentence reads them.
func searchPhrase(s watch.Search) string {
	parts := make([]string, 0, 8)

	add := func(prefix string, words []string, conj string) {
		if len(words) > 0 {
			parts = append(parts, prefix+series(words, conj))
		}
	}

	add("by ", authorNames(s.Authors), "or")
	add("in ", placeNames(s.Where), "and")
	add("", involvingWords(s.Involving), "and")
	add("labeled ", quoted(s.Labels), "or")
	add("not labeled ", quoted(s.NotLabels), "or")

	if s.Base != "" {
		parts = append(parts, "into "+s.Base)
	}

	if s.HeadPrefix != "" {
		parts = append(parts, "from branches starting "+s.HeadPrefix)
	}

	add("", stateWords(s.State), "and")

	if s.More != "" {
		parts = append(parts, "matching "+s.More)
	}

	return strings.Join(parts, " ")
}

// authorNames says who opened the items: "you", "Dependabot", "octocat".
func authorNames(authors []string) []string {
	bots := map[string]string{"dependabot": "Dependabot", "renovate": "Renovate", "github-actions": "GitHub Actions"}
	out := make([]string, 0, len(authors))

	for _, a := range authors {
		switch {
		case a == watch.Me:
			out = append(out, "you")
		case strings.HasPrefix(a, "app/"):
			bot := strings.TrimPrefix(a, "app/")
			if name, ok := bots[bot]; ok {
				bot = name
			}

			out = append(out, bot)
		default:
			out = append(out, a)
		}
	}

	return out
}

// placeNames says where the items are: "your account", "acme-org", "acme-org/api".
func placeNames(where []string) []string {
	out := make([]string, 0, len(where))

	for _, w := range where {
		if w == watch.Me {
			out = append(out, "your account")

			continue
		}

		_, name, _ := strings.Cut(w, ":")
		out = append(out, name)
	}

	return out
}

// involvingWords says how the items involve the user.
func involvingWords(involving []watch.Involving) []string {
	words := map[watch.Involving]string{
		watch.InvolvingAssigned: "assigned to you", watch.InvolvingReviewRequested: "asking for your review",
		watch.InvolvingMentioned: "mentioning you",
	}
	out := make([]string, 0, len(involving))

	for _, inv := range involving {
		out = append(out, words[inv])
	}

	return out
}

// stateWords says the states the search asks for.
func stateWords(states []watch.State) []string {
	words := map[watch.State]string{
		watch.StateReady: "out of draft", watch.StateDraft: "in draft",
		watch.StateFailing: "with failing checks", watch.StatePassing: "with passing checks",
		watch.StatePending: "with pending checks", watch.StateChangesRequested: "with changes requested",
		watch.StateApproved: "with an approval", watch.StateReviewRequired: "awaiting a review",
		watch.StateUnreviewed: "with no review",
	}
	out := make([]string, 0, len(states))

	for _, st := range states {
		out = append(out, words[st])
	}

	return out
}

// stepsPhrase says the steps in order: each after the one before with ", then", or with
// "; if …," when it carries an Only if.
func stepsPhrase(steps []Step, names PromptNames) string {
	var b strings.Builder

	for i, st := range steps {
		phrase := stepPhrase(st, names)

		switch {
		case i == 0:
			b.WriteString(phrase)
		case st.OnlyIf != "":
			b.WriteString("; " + onlyIfWords(st.OnlyIf) + ", " + phrase)
		default:
			b.WriteString(", then " + phrase)
		}
	}

	return b.String()
}

// onlyIfWords says an Only if.
func onlyIfWords(o catalog.Outcome) string {
	words, ok := map[catalog.Outcome]string{
		catalog.OutcomePassedChecks: "if they pass", catalog.OutcomeFailingChecks: "if they still fail",
		catalog.OutcomePushed: "if it pushed", catalog.OutcomeNotPushed: "if it didn't push",
		catalog.OutcomeApprove: "if it approves", catalog.OutcomeRequestChanges: "if it requests changes",
	}[o]
	if !ok {
		return "if " + string(o)
	}

	return words
}

// stepPhrase says one step.
func stepPhrase(st Step, names PromptNames) string {
	phrases := map[catalog.StepKind]func() string{
		catalog.StepNotify: func() string { return "notify me" },
		catalog.StepPin:    func() string { return "pin it" },
		catalog.StepSnooze: func() string {
			return "snooze it for " + textutil.Counted(st.Snooze.SnoozeDays(), "day", "days")
		},
		catalog.StepDismiss:       func() string { return "dismiss it" },
		catalog.StepRestore:       func() string { return "restore it" },
		catalog.StepMarkDone:      func() string { return "mark its GitHub notification done" },
		catalog.StepGitHub:        func() string { return githubPhrase(st.GitHub, names) },
		catalog.StepAgent:         func() string { return agentPhrase(*st.Agent, names) },
		catalog.StepWaitForChecks: func() string { return "wait for the checks" },
		catalog.StepMerge:         func() string { return mergePhrase(*st.Merge) },
		catalog.StepPostReview:    func() string { return reviewPhrase(*st.PostReview) },
		catalog.StepTellMe: func() string {
			if st.TellMe.When == catalog.TellVerdict {
				return "tell me how it went"
			}

			return "tell me"
		},
	}

	if phrase, ok := phrases[st.Kind()]; ok {
		return phrase()
	}

	return "do nothing"
}

// githubPhrase says a GitHub step's actions.
func githubPhrase(actions []GitHubAction, names PromptNames) string {
	phrases := make([]string, 0, len(actions))

	for _, a := range actions {
		switch {
		case a.Rebase != nil:
			phrases = append(phrases, "ask Dependabot to rebase")
		case a.UpdateBranch != nil:
			phrases = append(phrases, "update its branch")
		case a.Reply != nil:
			phrases = append(phrases, fmt.Sprintf("comment your %q reply", promptName(a.Reply.Prompt, names)))
		case a.Labels != nil:
			phrases = append(phrases, labelsPhrase(*a.Labels))
		}
	}

	return series(phrases, "and")
}

// labelsPhrase says a labels action: "add the needs-repro label and remove the bug label".
func labelsPhrase(l LabelsAction) string {
	parts := make([]string, 0, 2)

	for _, part := range []struct {
		verb   string
		labels []string
	}{{"add", l.Add}, {"remove", l.Remove}} {
		if len(part.labels) > 0 {
			parts = append(parts, part.verb+" the "+series(part.labels, "and")+" "+
				map[bool]string{true: "label", false: "labels"}[len(part.labels) == 1])
		}
	}

	return strings.Join(parts, " and ")
}

// agentPhrase says an agent step: its prompt, where it runs, its tools when they aren't
// the default, and its launch settings.
func agentPhrase(a AgentStep, names PromptNames) string {
	phrase := "run its default prompt"
	if !a.Prompt.ItemDefault {
		phrase = fmt.Sprintf("run %q", promptName(a.Prompt, names))
	}

	if a.Place() == catalog.WhereTerminal {
		phrase += " in your terminal"
	} else {
		phrase += " in the background"
	}

	if tools := toolsWords(a); tools != "" {
		phrase += ", " + tools
	}

	if launch := launchWords(a.AgentID(), a.Profile, a.Model, a.Effort); launch != "" {
		phrase += ", with " + launch
	}

	if rows := launchRowsPhrase(a.LaunchFor); rows != "" {
		phrase += ", with " + rows
	}

	return phrase
}

// toolsWords says the agent's tools when they aren't the default.
func toolsWords(a AgentStep) string {
	if a.Place() == catalog.WhereTerminal {
		return ""
	}

	return map[catalog.Preset]string{
		catalog.PresetReadOnly: "read only", catalog.PresetGitHubTasks: "GitHub tasks only",
		catalog.PresetEditTest: "without pushing", catalog.PresetProfile: "with the profile's own permissions",
	}[a.Tools()]
}

// launchWords says an agent's launch settings: "the work profile on Opus at high
// effort", "" for none.
func launchWords(agentID, profile, model, effort string) string {
	subject := ""

	switch {
	case agentID != catalog.Inherit && agentID != "" && profile != "":
		subject = textutil.Capitalize(agentID) + "'s " + profile + " profile"
	case profile != "":
		subject = "the " + profile + " profile"
	case agentID != catalog.Inherit && agentID != "":
		subject = textutil.Capitalize(agentID)
	}

	if model != "" {
		subject = strings.TrimSpace(subject + " on " + textutil.Capitalize(model))
		subject = strings.TrimPrefix(subject, "on ")
	}

	if effort != "" {
		subject = strings.TrimSpace(subject + " at " + effort + " effort")
		subject = strings.TrimPrefix(subject, "at ")
	}

	return subject
}

// launchRowsPhrase says the rows per org or repository: "the work profile on Opus for
// acme-org and the default profile on Sonnet for acme-labs".
func launchRowsPhrase(rows []LaunchRow) string {
	phrases := make([]string, 0, len(rows))

	for _, r := range rows {
		if words := launchWords(r.Agent, r.Profile, r.Model, r.Effort); words != "" {
			phrases = append(phrases, words+" for "+r.Target)
		}
	}

	return series(phrases, "and")
}

// mergePhrase says a Merge it.
func mergePhrase(m MergeStep) string {
	method := map[catalog.MergeMethod]string{
		catalog.MergeCommit: " with a merge commit", catalog.MergeSquash: " with squash",
		catalog.MergeRebase: " with rebase",
	}[m.Method]

	merge := "merge it" + method + " when GitHub allows"
	if m.How == catalog.MergeAutoIfItWasOn {
		return "turn auto-merge back on if it was on, else " + merge
	}

	return merge
}

// reviewPhrase says a Post the agent's review.
func reviewPhrase(r ReviewStep) string {
	switch {
	case r.As == catalog.ReviewAsVerdict && r.AllowApprove:
		return "post its verdict, approving allowed"
	case r.As == catalog.ReviewAsVerdict:
		return "post its verdict"
	default:
		return "post its review as a comment"
	}
}

// promptName names a prompt: the names func's word for it, else its nickname, else its
// starter's key as words.
func promptName(p PromptRef, names PromptNames) string {
	if names != nil {
		if name := names(p); name != "" {
			return name
		}
	}

	if p.Nickname != "" {
		return p.Nickname
	}

	return textutil.Capitalize(strings.ReplaceAll(p.Starter, "-", " "))
}

// limitsPhrase says the routine's limits: its once-per, its tries when it has steps
// they count, and the rest where they differ from their defaults.
func limitsPhrase(s Spec, it items) string {
	l := s.Limits

	once := map[catalog.OncePer]string{
		catalog.OncePerItem: "once per " + it.one, catalog.OncePerHeadCommit: "once per commit",
		catalog.OncePerDay: "once a day",
	}[l.OncePer]

	parts := []struct {
		say  bool
		text string
	}{
		{say: once != "", text: once},
		{say: counted(s.Steps), text: "at most " + textutil.Counted(l.Tries(), "try", "tries") + " per " + it.one},
		{
			say:  l.RunsPerDay != nil && l.Runs() != catalog.DefaultRunsPerDay,
			text: "at most " + textutil.Counted(l.Runs(), "run", "runs") + " a day",
		},
		{
			say:  l.WaitMinutes != nil && *l.WaitMinutes != catalog.DefaultWaitMinutes,
			text: minutesWords(int(l.Wait()/time.Minute)) + " between tries",
		},
		{say: l.ActiveHours != nil, text: hoursPhrase(l.ActiveHours)},
		{say: l.BudgetPerDay != nil, text: "up to " + budgetWords(l.BudgetPerDay) + " a day"},
	}

	said := make([]string, 0, len(parts))

	for _, p := range parts {
		if p.say {
			said = append(said, p.text)
		}
	}

	return strings.Join(said, ", ")
}

// counted reports steps the tries count: an agent step or a GitHub step.
func counted(steps []Step) bool {
	for _, st := range steps {
		if st.Agent != nil || st.GitHub != nil {
			return true
		}
	}

	return false
}

// minutesWords says a number of minutes: "45 minutes", "2 hours", "90 minutes".
func minutesWords(m int) string {
	if m >= 60 && m%60 == 0 {
		return textutil.Counted(m/60, "hour", "hours")
	}

	return textutil.Counted(m, "minute", "minutes")
}

// hoursPhrase says the active hours: "on weekdays from 08:00 to 20:00"; "" for none.
func hoursPhrase(h *ActiveHours) string {
	if h == nil {
		return ""
	}

	days := "on " + series(dayNames(h.Days), "and")

	switch strings.Join(weekdayWords(h.Days), ",") {
	case "mon,tue,wed,thu,fri":
		days = "on weekdays"
	case "mon,tue,wed,thu,fri,sat,sun":
		days = "every day"
	case "sat,sun":
		days = "on weekends"
	}

	return days + " from " + h.From + " to " + h.To
}

// dayNames names the days.
func dayNames(days []catalog.Weekday) []string {
	out := make([]string, 0, len(days))
	for _, d := range days {
		out = append(out, weekdayName(d))
	}

	return out
}

// weekdayWords returns the days as the spec spells them.
func weekdayWords(days []catalog.Weekday) []string {
	out := make([]string, 0, len(days))
	for _, d := range days {
		out = append(out, string(d))
	}

	return out
}

// budgetWords says a budget: "$5", "$0.50", "200,000 tokens".
func budgetWords(b *Budget) string {
	if b == nil {
		return "no budget"
	}

	if b.USD != nil {
		return "$" + dollars(int(*b.USD*100+0.5))
	}

	if b.Tokens != nil {
		return grouped(*b.Tokens) + " tokens"
	}

	return "no budget"
}

// grouped writes n with a comma between each group of three digits.
func grouped(n int64) string {
	s := strconv.FormatInt(n, 10)

	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}

	return s
}
