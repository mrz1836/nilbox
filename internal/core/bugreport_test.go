package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// privateWords are the names and paths a user's settings may hold that a bug report must
// never write out.
func privateWords() []string {
	return []string{
		"employer-profile", "private-user", "private-bot", "secret-dir", "release-private", "Private status",
		"private-repo", "private-label", "private-workflow", "private-prompt", "private-org", boundOwner, boundRepo,
	}
}

// privateServiceStatus is the status pages as the user changed them: Claude's parts
// narrowed to one, and OpenAI's page off.
func privateServiceStatus() statusprefs.Config {
	c := statusprefs.Defaults()

	for i, w := range c.Watch {
		switch w.Provider {
		case statusprefs.ProviderGitHub:
			// GitHub's keeps its default parts.
		case statusprefs.ProviderClaude:
			c.Watch[i].Parts = []statusprefs.PartID{"claude.api"}
		case statusprefs.ProviderOpenAI:
			c.Watch[i].On = false
		}
	}

	return c
}

// privateSettings are settings full of names and paths of the user's own, every list in
// them holding something.
func privateSettings() settings.Settings {
	return settings.Settings{
		DefaultAgent:        agent.Claude,
		DefaultProfiles:     map[agent.ID]string{agent.Claude: "employer-profile", agent.Codex: "default"},
		WorktreeDefault:     true,
		WorktreeAutoCleanup: true,
		Terminal:            "iterm2",
		FollowSessions:      true,
		ClaudeAPIKey:        true,
		CloneRoot:           "/srv/private-user/work",
		CloneLocations: []localrepos.Location{
			{Path: "/srv/private-user/elsewhere", Kind: localrepos.KindFolder},
			{Path: "~/Desktop/private-repo", Kind: localrepos.KindClone},
		},
		TrustedBots:        []string{"dependabot[bot]", "private-bot[bot]"},
		DefaultTrustedBots: []string{"dependabot[bot]"},
		SnoozeDays:         3,
		SnoozeWake:         snooze.Rules{Commits: 1, Reviews: "decisions", Comments: 1, Ready: true},
		WindowSize:         settings.WindowSize{Width: 1280, Height: 800},
		SensitiveFiles: sensitive.Config{
			Scope: "collaborators", Off: []string{"docs"}, Patterns: []string{"secret-dir/**"},
		},
		Branches: branchrules.Rules{
			Own: true, PullRequest: true, Merged: false, Protected: []string{"main", "release-private-*"},
		},
		DefaultProtectedBranches: []string{"main", "master"},
		RepoRules: reporules.Rules{
			Pinned: []string{boundOwner + "/" + boundRepo}, Muted: []string{"private-user", "private-repo/x"},
		},
		MenuBar:           true,
		BannersOff:        []notifyprefs.Kind{"session_waiting"},
		Theme:             "dark",
		TextSize:          "default",
		Celebrations:      settings.CelebrationsQuiet,
		ToastSeconds:      20,
		CollapsedSections: []settings.ListKey{settings.ListYours, settings.ListReviewRequested},
		ConversationOrder: settings.ConversationNewest,
		InboxOrder: settings.InboxOrder{
			StatusSorted: []settings.ListKey{settings.ListYours}, StatusOrder: []string{"Private status"},
			Kinds: map[settings.ListKey]settings.ListKind{settings.ListReviewRequested: settings.ListKindPRs},
		},
		HiddenLabels:        []string{"size/*", "private-label/*", "wontfix"},
		DefaultHiddenLabels: settings.DefaultHiddenLabels(),
		CardAvatars:         true,
		CardDensity:         settings.CardCompact,
		CardUnread:          true,
		IgnoredWorkflows:    []string{"private-workflow"},
		MergeMethod:         "squash",
		BypassOwn:           true,
		NudgeAfterHours:     48,
		NudgeSnooze:         true,
		Integrations: integrationprefs.Config{
			Switches:      map[integrationprefs.ID]integrationprefs.Switch{integrationprefs.Scripts: "on"},
			Paths:         map[integrationprefs.ID]string{integrationprefs.MageX: "/srv/private-user/bin/magex"},
			RunsIn:        integrationprefs.RunsInTerminal,
			ScriptSources: integrationprefs.ScriptSources{MagexTargets: true, RepoFiles: false},
			LastRepo:      "~/projects/private-repo",
		},
		GitHubNotifications: threadrules.Rules{
			Read: true, Auto: true, Action: threadrules.ActionRead,
			TriggersOff: []threadrules.Trigger{threadrules.TriggerByHand}, BotActivity: true,
		},
		Refresh: settings.RefreshIntervals{
			Inbox: 600, Checks: 0, ReleaseChecks: 30, Worktrees: 900, Notifications: 120, ServiceStatus: 900,
		},
		WatchPushes:   true,
		ServiceStatus: privateServiceStatus(),
		Autopilot:     autopilotprefs.Config{Enabled: true},
	}
}

// privateOthers are the settings kept outside the settings service, as a user full of
// prompts, profiles, and launch defaults of their own has them.
func privateOthers() otherSettings {
	library := map[int64]prompts.Prompt{
		1: {ID: 1, Nickname: "Review", StarterKey: "review"},
		2: {ID: 2, Nickname: "private-prompt"},
	}
	defaults := prompts.DefaultPrompts{
		PR: 2, Issue: 1, ChecksFailing: 1, Conflicts: 9,
		Replies: prompts.ReplyDefaults{Approve: 1, RequestChanges: 2, Nudge: 9},
	}

	return otherSettings{
		replies: replyParts(defaults, library), prompts: launchPromptParts(defaults, library),
		profiles: profileParts([]profiles.Profile{
			{Agent: agent.Claude, Name: profiles.DefaultName, SignedIn: true},
			{Agent: agent.Claude, Name: "employer-profile", Folder: "~/.claude_employer-profile", Missing: true},
			{Agent: agent.Codex, Name: profiles.DefaultName, SignedIn: true},
		}),
		launchDefaults: launchDefaultsWords([]launchdefaults.Defaults{
			{Owner: "private-org"}, {Owner: "private-org", Name: "private-repo"}, {Owner: boundOwner, Name: boundRepo},
		}),
	}
}

// privateTools are the integrations, each copy at a path of the user's own.
func privateTools() integrations.Tools {
	return integrations.Tools{ShellChecked: true, ShellRead: true, Tools: []integrations.Tool{
		{
			ID: integrationprefs.MageX, Name: "magex", Switch: "auto", On: true, State: integrations.StateUpdate,
			Copy: integrations.Copy{
				Path: "/srv/private-user/bin/magex", DisplayPath: "~/bin/magex", Runs: true,
				Version: integrations.Version{Raw: "1.30.0"}, Source: integrations.SourceHomebrew, Link: true,
			},
			Others: []integrations.Copy{{Path: "/srv/private-user/go/bin/magex", Error: "private-user broke"}},
			Chosen: "/srv/private-user/bin/magex",
			Latest: integrations.Latest{Version: "1.30.1", URL: "https://github.com/" + boundOwner + "/" + boundRepo},
		},
		{ID: integrationprefs.GoPreCommit, Name: "go-pre-commit", Switch: "off", State: integrations.StateMissing},
	}}
}

// joined is every line of sections, one per line.
func joined(sections []appinfo.StateSection) string {
	var b strings.Builder

	for _, s := range sections {
		b.WriteString(s.Title + "\n")

		for _, line := range s.Lines {
			b.WriteString(line + "\n")
		}
	}

	return b.String()
}

// pages are the names a section's lines start with, in order.
func pages(section appinfo.StateSection) []string {
	out := make([]string, 0, len(section.Lines))

	for _, line := range section.Lines {
		page, _, _ := strings.Cut(line, ": ")
		out = append(out, page)
	}

	return out
}

func TestABugReportStatesTheSettingsAndIntegrationsWithNothingPersonal(t *testing.T) {
	t.Parallel()

	s := privateSettings()
	section := settingsSection(s, privateOthers())
	report := joined([]appinfo.StateSection{
		section,
		{Title: "Autopilot", Lines: []string{autopilotSettingsLine(s.Autopilot)}},
		integrationsSection(privateTools(), s.Integrations),
	})

	for _, word := range privateWords() {
		assert.NotContains(t, report, word, "a bug report never writes out a name or path of the user's own")
	}

	assert.Equal(t, []string{
		"Appearance", "Notifications", "GitHub notifications", "Refreshing", "Service status", "Snoozing", "Cards",
		"Reviewing", "Reviewing replies", "Repositories", "Profiles", "Launching", "Launching prompts", "Trust",
		"Sensitive files", "Worktrees", "Labs",
	}, pages(section), "one line per page of Settings, in the sidebar's order")

	for _, want := range []string{
		"Appearance: theme dark · text size default · window 1280×800 · celebrations quiet",
		"Notifications: menu bar on · toasts 20 s · banners off: session_waiting",
		"GitHub notifications: read on · clear automatically on · clearing marks read · triggers off: by_hand · " +
			"watched repositories off · bot activity counts as seen on",
		"Refreshing: inbox 600 s · running checks off · release checks 30 s · worktrees 900 s · " +
			"GitHub notifications 120 s · pushes from this Mac on",
		"Service status: every 900 s · github on, parts the default (5) · claude on, parts the user's own (1) · " +
			"openai off, parts the default (3)",
		"Snoozing: 3 days · wake on commits 1, reviews decisions, comments 1, ready for review on",
		"Cards: density compact · author pictures on · unread dots on · hidden labels the user's own (3) · " +
			"lists sorted by status 1 · status order the user's own (1) · lists showing one kind 1 · collapsed lists 2",
		"Reviewing: merge method squash · merge own past the rules on · nudge after 48 h · snooze after a nudge on · " +
			"ignored workflows 1 · conversation newest first",
		"Reviewing replies: approve a starter · approve bots none · request changes the user's own · comment none · " +
			"nudge a missing prompt",
		"Repositories: clone root the user's own · other locations 2 · 1 pinned, 2 muted · " +
			"launch defaults 1 orgs, 2 repositories",
		"Profiles: claude 2, 1 signed in, 1 missing folders · codex 1, 1 signed in, 0 missing folders",
		"Launching: terminal iterm2 · default agent claude · profiles claude the user's own, codex default · " +
			"start in a worktree on · follow sessions on · Codex effort off, fast off · " +
			"API key for Claude Code on, Codex off",
		"Launching prompts: pull requests the user's own · issues a starter · checks fail a starter · " +
			"conflicts a missing prompt · changes requested none · review again none",
		"Trust: trusted bots the user's own (2)",
		"Sensitive files: scope collaborators · categories off: docs · own patterns 1",
		"Worktrees: automatic cleanup on · delete own branch on, pull request's on, tick merged off · " +
			"protected branches the user's own (2)",
		"Labs: Autopilot (beta) on",
		"magex: on (auto), update · 1.30.0 from Homebrew, a link · a copy the user chose · latest 1.30.1 · " +
			"1 other copies",
		"go-pre-commit: off (off), missing",
		"Scripts: on · magex targets: on · repository files: off",
		"Runs in: terminal · offer to release after a merge: off",
		"Login shell's environment: read",
	} {
		assert.Contains(t, report, want+"\n")
	}
}

func TestABugReportSaysWhatIsLeftAtItsDefault(t *testing.T) {
	t.Parallel()

	s := privateSettings()
	s.CloneRoot = settings.DefaultCloneRoot
	s.TrustedBots = []string{"dependabot[bot]"}
	s.Branches.Protected = []string{"master", "main"}
	s.DefaultProfiles = map[agent.ID]string{}
	s.HiddenLabels = settings.DefaultHiddenLabels()
	s.InboxOrder.StatusOrder = []string{}
	s.ServiceStatus = statusprefs.Defaults()
	s.Refresh.ServiceStatus = 0
	s.NudgeAfterHours = 0
	s.BannersOff, s.SensitiveFiles.Off, s.GitHubNotifications.TriggersOff = nil, []string{}, []threadrules.Trigger{}
	s.Autopilot.Enabled = false

	report := joined([]appinfo.StateSection{
		settingsSection(s, otherSettings{}), integrationsSection(integrations.Tools{}, integrationprefs.Config{}),
	})

	for _, want := range []string{
		"clone root the default",
		"trusted bots the default (1)",
		"protected branches the default (2)",
		"profiles the agents' defaults",
		fmt.Sprintf("hidden labels the default (%d)", len(settings.DefaultHiddenLabels())),
		"status order the default ·",
		"Service status: off · github on, parts the default (5) · claude on, parts the default (2) · " +
			"openai on, parts the default (3)\n",
		"nudges off",
		"banners off: none",
		"categories off: none",
		"triggers off: none",
		"Labs: Autopilot (beta) off",
		"Scripts: auto · magex targets: off · repository files: off",
		"Runs in: app",
		"Login shell's environment: not read yet",
	} {
		assert.Contains(t, report, want)
	}

	for _, page := range []string{"Reviewing replies:", "Profiles:", "Launching prompts:", "launch defaults"} {
		assert.NotContains(t, report, page, "a part that couldn't be read is left out")
	}
}

func TestABugReportTellsWhichPromptADefaultNamesWithoutNamingIt(t *testing.T) {
	t.Parallel()

	library := map[int64]prompts.Prompt{
		1: {ID: 1, Nickname: "Review", StarterKey: "review", Modified: true},
		2: {ID: 2, Nickname: "private-prompt", Name: "Triage private-repo"},
	}

	for id, want := range map[int64]string{0: "none", 1: "a starter", 2: "the user's own", 3: "a missing prompt"} {
		assert.Equal(t, want, promptWords(id, library), "prompt %d", id)
	}
}

func TestABugReportCountsTheProfilesAndLaunchDefaultsAndNamesNone(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"claude 0, 0 signed in, 0 missing folders", "codex 0, 0 signed in, 0 missing folders"},
		profileParts(nil), "every agent is counted, one without a profile too")
	assert.Equal(t, "0 orgs, 0 repositories", launchDefaultsWords(nil))
}

func TestABugReportWordsDescribeItsAgentWithoutNamingItsProfile(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Describe it's agent: profile the default · model the default · effort the default",
		describeAgentLine(describeit.AgentChoice{}))
	assert.Equal(t, "Describe it's agent: profile the default · model the default · effort the default",
		describeAgentLine(describeit.AgentChoice{Profile: profiles.DefaultName}))
	assert.Equal(t, "Describe it's agent: profile the user's own · model opus · effort high",
		describeAgentLine(describeit.AgentChoice{Profile: "employer-profile", Model: "opus", Effort: "high"}))
}

func TestABugReportCountsTheRoutinesAndNamesNone(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	routines := autopilot.NewService(autopilot.Config{Pool: testutil.DB(t), Accounts: viewerLogin("private-user")})

	for _, name := range []string{"private-repo checks", "Release private-bot"} {
		_, err := routines.Save(ctx, autopilot.Draft{Name: name, Description: "For " + boundOwner, Spec: boundSpec()})
		require.NoError(t, err)
	}

	list, err := routines.List(ctx)
	require.NoError(t, err)

	_, err = routines.SetEnabled(ctx, list[0].ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	c := autopilotprefs.Defaults()
	c.Paused, c.DailyCap = true, autopilotprefs.DailyCap{Amount: 2_000_000, Unit: autopilotprefs.UnitTokens}

	report := joined(bugReportState{routines: routines, logger: slog.New(slog.DiscardHandler)}.autopilotState(ctx, c))

	for _, word := range privateWords() {
		assert.NotContains(t, report, word, "a bug report never names a routine, its account, or what it watches")
	}

	assert.Equal(t, "Autopilot\n"+
		"Paused: on · new routines: Ask first · agents at once: 2 · runs a day: 20 · actions a day: 500 · actions an hour: 120 · "+
		"daily cap: 2000000 tokens · keep awake: off · hold on outage: on · catch up after: 24 h · history: 90 days, "+
		"logs: 30 · merge own past the rules: off\n"+
		"Routines: 2 (0 dry run, 2 ask first, 0 autopilot) · on: 1 · broken: 0 · another account's: 0\n"+
		"Runs today: 0 in the background · 0 in the terminal · 0 GitHub actions\n", report)

	assert.Empty(t, bugReportState{}.autopilotState(ctx, c), "Autopilot off, and no routine read, no section")
}

func TestABugReportStatesAutopilotWhileItIsOnOrARoutineIsLeft(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	logger := slog.New(slog.DiscardHandler)
	routines := autopilot.NewService(autopilot.Config{Pool: testutil.DB(t)})

	off := autopilotprefs.Defaults()
	off.Paused = true

	assert.Empty(t, bugReportState{routines: routines, logger: logger}.autopilotState(ctx, off),
		"Autopilot off and no routine, no section, whatever the settings")

	made, err := routines.Save(ctx, autopilot.Draft{Name: "Gone", Spec: boundSpec()})
	require.NoError(t, err)
	require.NoError(t, routines.Delete(ctx, made.ID))

	assert.Empty(t, bugReportState{routines: routines, logger: logger}.autopilotState(ctx, off),
		"a deleted routine counts as none")

	unreadable := autopilot.NewService(autopilot.Config{Pool: testutil.ClosedDB(t)})
	assert.Empty(t, bugReportState{routines: unreadable, logger: logger}.autopilotState(ctx, off),
		"routines that can't be read leave Autopilot out while it is off")

	on := off
	on.Enabled = true

	assert.Equal(t, []string{autopilotSettingsLine(on), "Routines: 0 (0 dry run, 0 ask first, 0 autopilot) · " +
		"on: 0 · broken: 0 · another account's: 0", "Runs today: 0 in the background · 0 in the terminal · " +
		"0 GitHub actions"}, joinedLines(bugReportState{routines: routines, logger: logger}.autopilotState(ctx, on)),
		"Autopilot on states its settings without a routine")
	assert.Equal(t, []string{autopilotSettingsLine(on)},
		joinedLines(bugReportState{routines: unreadable, logger: logger}.autopilotState(ctx, on)),
		"and keeps them when the routines can't be read")
}

// joinedLines is the lines of the one section in sections.
func joinedLines(sections []appinfo.StateSection) []string {
	if len(sections) != 1 {
		return nil
	}

	return sections[0].Lines
}

// viewerLogin is an account signed in as login.
type viewerLogin string

// ViewerLogin returns the login.
func (v viewerLogin) ViewerLogin(context.Context) (string, error) {
	return string(v), nil
}
