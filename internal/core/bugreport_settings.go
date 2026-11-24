package core

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/statusprefs"
)

// otherSettings are the settings kept outside the settings service, worded for a bug
// report already: each part nil or "" when it couldn't be read, which leaves its line or
// clause out.
type otherSettings struct {
	// replies are the Reviewing page's default replies, and prompts the Launching page's
	// default prompts, each told as a starter, the user's own, none, or a missing prompt.
	replies []string
	prompts []string

	// profiles are the Profiles page's profiles, counted per agent.
	profiles []string

	// launchDefaults are the Repositories page's launch defaults, counted.
	launchDefaults string
}

// settingsSection words the settings for a bug report: one line per page of Settings,
// under the page's name and in the sidebar's order (frontend/src/lib/settingsIndex.ts),
// with the settings kept elsewhere from other. Autopilot's page and the integrations have
// sections of their own, and Account, Backup, and About hold no setting to word.
// TestABugReportWordsEverySetting fails on a setting that is neither worded here nor left
// out with a reason.
func settingsSection(s settings.Settings, other otherSettings) appinfo.StateSection {
	lines := []string{
		appearanceLine(s), notificationsLine(s), githubNotificationsLine(s), refreshingLine(s),
		serviceStatusLine(s), snoozingLine(s), cardsLine(s), reviewingLine(s),
	}
	lines = appendPage(lines, "Reviewing replies", other.replies)
	lines = append(lines, repositoriesLine(s, other.launchDefaults))
	lines = appendPage(lines, "Profiles", other.profiles)
	lines = append(lines, launchingLine(s))
	lines = appendPage(lines, "Launching prompts", other.prompts)
	lines = append(lines, trustLine(s), sensitiveFilesLine(s), worktreesLine(s), labsLine(s))

	return appinfo.StateSection{Title: "Settings", Lines: lines}
}

// pageLine is one page's line: its name, then each setting as "name value", between dots.
func pageLine(page string, parts ...string) string {
	return page + ": " + strings.Join(parts, " · ")
}

// appendPage appends a page's line to lines, or nothing when its parts couldn't be read.
func appendPage(lines []string, page string, parts []string) []string {
	if len(parts) == 0 {
		return lines
	}

	return append(lines, pageLine(page, parts...))
}

func appearanceLine(s settings.Settings) string {
	return pageLine("Appearance", "theme "+string(s.Theme), "text size "+string(s.TextSize),
		fmt.Sprintf("window %d×%d", s.WindowSize.Width, s.WindowSize.Height), "celebrations "+string(s.Celebrations))
}

func notificationsLine(s settings.Settings) string {
	return pageLine("Notifications", "menu bar "+onOff(s.MenuBar), fmt.Sprintf("toasts %d s", s.ToastSeconds),
		"banners off: "+listWords(s.BannersOff))
}

// githubNotificationsLine names the triggers turned off: they are the app's own words,
// never the user's.
func githubNotificationsLine(s settings.Settings) string {
	g := s.GitHubNotifications

	return pageLine("GitHub notifications", "read "+onOff(g.Read), "clear automatically "+onOff(g.Auto),
		"clearing marks "+string(g.Action), "triggers off: "+listWords(g.TriggersOff),
		"watched repositories "+onOff(g.Watching), "bot activity counts as seen "+onOff(g.BotActivity))
}

func refreshingLine(s settings.Settings) string {
	r := s.Refresh

	return pageLine("Refreshing", "inbox "+seconds(r.Inbox), "running checks "+seconds(r.Checks),
		"release checks "+seconds(r.ReleaseChecks), "worktrees "+seconds(r.Worktrees),
		"GitHub notifications "+seconds(r.Notifications), "pushes from this Mac "+onOff(s.WatchPushes))
}

// serviceStatusLine words how often the status pages are checked, then each provider: on
// or off, and its parts counted, told as the default or the user's own.
func serviceStatusLine(s settings.Settings) string {
	every := "off"
	if s.Refresh.ServiceStatus > 0 {
		every = "every " + seconds(s.Refresh.ServiceStatus)
	}

	parts := make([]string, 0, 1+len(s.ServiceStatus.Watch))
	parts = append(parts, every)
	defaults := statusprefs.Defaults()

	for _, w := range s.ServiceStatus.Watch {
		d, known := defaults.For(w.Provider)
		parts = append(parts, fmt.Sprintf("%s %s, parts %s", w.Provider, onOff(w.On),
			counted(known && sameSet(w.Parts, d.Parts), len(w.Parts))))
	}

	return pageLine("Service status", parts...)
}

func snoozingLine(s settings.Settings) string {
	w := s.SnoozeWake

	return pageLine("Snoozing", fmt.Sprintf("%d days", s.SnoozeDays),
		fmt.Sprintf("wake on commits %d, reviews %s, comments %d, ready for review %s", w.Commits, w.Reviews,
			w.Comments, onOff(w.Ready)))
}

// cardsLine counts the hidden labels and the statuses, never names them: a label or a
// project's status can be a private word.
func cardsLine(s settings.Settings) string {
	statusOrder := "the default"
	if len(s.InboxOrder.StatusOrder) > 0 {
		statusOrder = counted(false, len(s.InboxOrder.StatusOrder))
	}

	sorted := map[settings.ListSort]int{}
	for _, sort := range s.InboxOrder.Sorts {
		sorted[sort]++
	}

	return pageLine("Cards", "density "+string(s.CardDensity), "author pictures "+onOff(s.CardAvatars),
		"unread dots "+onOff(s.CardUnread),
		"hidden labels "+counted(sameSet(s.HiddenLabels, s.DefaultHiddenLabels), len(s.HiddenLabels)),
		fmt.Sprintf("lists not newest first %d (status %d, repo %d, author %d)", len(s.InboxOrder.Sorts),
			sorted[settings.ListSortStatus], sorted[settings.ListSortRepo], sorted[settings.ListSortAuthor]),
		"status order "+statusOrder,
		fmt.Sprintf("lists showing one kind %d", len(s.InboxOrder.Kinds)),
		fmt.Sprintf("collapsed lists %d", len(s.CollapsedSections)))
}

// reviewingLine counts the ignored workflows and the auto-merge bots, never names them.
func reviewingLine(s settings.Settings) string {
	nudge := "nudges off"
	if s.NudgeAfterHours > 0 {
		nudge = fmt.Sprintf("nudge after %d h", s.NudgeAfterHours)
	}

	return pageLine("Reviewing", "merge method "+string(s.MergeMethod), "merge own past the rules "+onOff(s.BypassOwn),
		nudge, "snooze after a nudge "+onOff(s.NudgeSnooze), fmt.Sprintf("ignored workflows %d", len(s.IgnoredWorkflows)),
		fmt.Sprintf("auto-merge bots %s (%d)", onOff(s.MergeBotsOn), len(s.MergeBots)),
		"conversation "+string(s.ConversationOrder)+" first")
}

// repositoriesLine tells the clone root as the default or the user's own, and counts the
// rest; launchDefaults is left out when "".
func repositoriesLine(s settings.Settings, launchDefaults string) string {
	parts := []string{
		"clone root " + ownOrDefault(s.CloneRoot == settings.DefaultCloneRoot),
		fmt.Sprintf("other locations %d", len(s.CloneLocations)),
		fmt.Sprintf("%d pinned, %d muted", len(s.RepoRules.Pinned), len(s.RepoRules.Muted)),
	}

	if launchDefaults != "" {
		parts = append(parts, "launch defaults "+launchDefaults)
	}

	return pageLine("Repositories", parts...)
}

func launchingLine(s settings.Settings) string {
	return pageLine("Launching", "terminal "+string(s.Terminal), "default agent "+string(s.DefaultAgent),
		"profiles "+profileWords(s.DefaultProfiles), "start in a worktree "+onOff(s.WorktreeDefault),
		"follow sessions "+onOff(s.FollowSessions),
		fmt.Sprintf("Codex effort %s, fast %s", onOff(s.CodexEffort), onOff(s.CodexFast)),
		fmt.Sprintf("API key for Claude Code %s, Codex %s", onOff(s.ClaudeAPIKey), onOff(s.CodexAPIKey)))
}

func trustLine(s settings.Settings) string {
	return pageLine("Trust", "trusted bots "+counted(sameSet(s.TrustedBots, s.DefaultTrustedBots), len(s.TrustedBots)))
}

// sensitiveFilesLine names the built-in categories turned off, the app's own words, and
// counts the user's own patterns.
func sensitiveFilesLine(s settings.Settings) string {
	f := s.SensitiveFiles

	return pageLine("Sensitive files", "scope "+string(f.Scope), "categories off: "+listWords(f.Off),
		fmt.Sprintf("own patterns %d", len(f.Patterns)))
}

func worktreesLine(s settings.Settings) string {
	b := s.Branches

	return pageLine("Worktrees", "automatic cleanup "+onOff(s.WorktreeAutoCleanup),
		fmt.Sprintf("delete own branch %s, pull request's %s, tick merged %s", onOff(b.Own), onOff(b.PullRequest),
			onOff(b.Merged)),
		"protected branches "+counted(sameSet(b.Protected, s.DefaultProtectedBranches), len(b.Protected)))
}

// labsLine is always there, so a report says whether Autopilot (beta) is on even when its
// section is left out.
func labsLine(s settings.Settings) string {
	return pageLine("Labs", "Autopilot (beta) "+onOff(s.Autopilot.Enabled))
}

// promptWords tells which prompt a default names without naming it: none, one of the
// starters, the user's own, or one no longer in the library.
func promptWords(id int64, library map[int64]prompts.Prompt) string {
	if id == 0 {
		return "none"
	}

	p, ok := library[id]

	switch {
	case !ok:
		return "a missing prompt"
	case p.StarterKey != "":
		return "a starter"
	default:
		return "the user's own"
	}
}

// promptsByID is the prompts by ID.
func promptsByID(list []prompts.Prompt) map[int64]prompts.Prompt {
	out := make(map[int64]prompts.Prompt, len(list))
	for _, p := range list {
		out[p.ID] = p
	}

	return out
}

// replyParts words the default reply of each review, in the order the editor lists them.
func replyParts(d prompts.DefaultPrompts, library map[int64]prompts.Prompt) []string {
	slots := prompts.ReplySlots()
	out := make([]string, 0, len(slots))

	for _, slot := range slots {
		out = append(out, strings.ReplaceAll(string(slot), "_", " ")+" "+promptWords(d.Replies.For(slot), library))
	}

	return out
}

// launchPromptParts words the default prompt of each kind of item and each situation a
// pull request can be in, as the Launching page lists them.
func launchPromptParts(d prompts.DefaultPrompts, library map[int64]prompts.Prompt) []string {
	return []string{
		"pull requests " + promptWords(d.PR, library), "issues " + promptWords(d.Issue, library),
		"checks fail " + promptWords(d.ChecksFailing, library), "conflicts " + promptWords(d.Conflicts, library),
		"changes requested " + promptWords(d.ChangesRequested, library),
		"review again " + promptWords(d.Rereview, library),
	}
}

// profileParts counts each agent's profiles, those signed in, and those whose folder is
// gone, in the agents' order, never naming one: a profile's name can name an employer.
func profileParts(list []profiles.Profile) []string {
	type tally struct{ profiles, signedIn, missing int }

	counts := map[agent.ID]tally{}

	for _, p := range list {
		c := counts[p.Agent]
		c.profiles++
		c.signedIn += oneIf(p.SignedIn)
		c.missing += oneIf(p.Missing)
		counts[p.Agent] = c
	}

	ids := agent.IDs()
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		c := counts[id]
		out = append(out, fmt.Sprintf("%s %d, %d signed in, %d missing folders", id, c.profiles, c.signedIn, c.missing))
	}

	return out
}

// oneIf counts a yes as one.
func oneIf(yes bool) int {
	if yes {
		return 1
	}

	return 0
}

// launchDefaultsWords counts the orgs and the repositories with launch defaults, never
// naming one.
func launchDefaultsWords(list []launchdefaults.Defaults) string {
	orgs, repos := 0, 0

	for _, d := range list {
		if d.Name == "" {
			orgs++
		} else {
			repos++
		}
	}

	return fmt.Sprintf("%d orgs, %d repositories", orgs, repos)
}

// describeAgentLine words the agent Describe it's conversations start with: its profile
// as the default or the user's own, and its model and effort, or the default where it
// inherits.
func describeAgentLine(c describeit.AgentChoice) string {
	return fmt.Sprintf("Describe it's agent: profile %s · model %s · effort %s",
		ownOrDefault(c.Profile == "" || c.Profile == profiles.DefaultName),
		cmp.Or(c.Model, "the default"), cmp.Or(c.Effort, "the default"))
}

// onOff words a switch.
func onOff(on bool) string {
	if on {
		return "on"
	}

	return "off"
}

// seconds words an interval in seconds, 0 being off.
func seconds(n int) string {
	if n == 0 {
		return "off"
	}

	return fmt.Sprintf("%d s", n)
}

// listWords joins a list of the app's own words, such as kinds of banner, or says none.
func listWords[T ~string](list []T) string {
	if len(list) == 0 {
		return "none"
	}

	words := make([]string, 0, len(list))
	for _, w := range list {
		words = append(words, string(w))
	}

	return strings.Join(words, ", ")
}

// ownOrDefault says whether the user changed a setting from its default, without saying
// what to.
func ownOrDefault(isDefault bool) string {
	if isDefault {
		return "the default"
	}

	return "the user's own"
}

// counted words a list of the user's by its length alone, as the default or the user's
// own.
func counted(isDefault bool, n int) string {
	return fmt.Sprintf("%s (%d)", ownOrDefault(isDefault), n)
}

// sameSet reports whether a and b hold the same values, in any order.
func sameSet[T cmp.Ordered](a, b []T) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)

	return slices.Equal(x, y)
}

// profileWords says which profile an agent launches with by default: the agent's own, or
// one of the user's, whose name, which may name an employer, is left out.
func profileWords(chosen map[agent.ID]string) string {
	ids := make([]string, 0, len(chosen))
	for id := range chosen {
		ids = append(ids, string(id))
	}

	slices.Sort(ids)

	parts := make([]string, 0, len(ids))

	for _, id := range ids {
		name := chosen[agent.ID(id)]
		if name == "" || name == profiles.DefaultName {
			parts = append(parts, id+" default")
		} else {
			parts = append(parts, id+" the user's own")
		}
	}

	if len(parts) == 0 {
		return "the agents' defaults"
	}

	return strings.Join(parts, ", ")
}
