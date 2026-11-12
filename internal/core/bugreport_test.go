package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// privateWords are the names and paths a user's settings may hold that a bug report must
// never write out.
func privateWords() []string {
	return []string{
		"employer-profile", "private-user", "private-bot", "secret-dir", "release-private", "Private status",
		"private-repo", boundOwner, boundRepo,
	}
}

// privateSettings are settings full of names and paths of the user's own.
func privateSettings() settings.Settings {
	return settings.Settings{
		DefaultAgent:        agent.Claude,
		DefaultProfiles:     map[agent.ID]string{agent.Claude: "employer-profile", agent.Codex: "default"},
		WorktreeDefault:     true,
		WorktreeAutoCleanup: true,
		Terminal:            "iterm2",
		FollowSessions:      true,
		CloneRoot:           "/srv/private-user/work",
		CloneLocations: []localrepos.Location{
			{Path: "/srv/private-user/elsewhere", Kind: localrepos.KindFolder},
			{Path: "~/Desktop/private-repo", Kind: localrepos.KindClone},
		},
		TrustedBots:        []string{"dependabot[bot]", "private-bot[bot]"},
		DefaultTrustedBots: []string{"dependabot[bot]"},
		SnoozeDays:         3,
		SnoozeWake:         snooze.Rules{Commits: 1, Reviews: "decisions", Comments: 1},
		WindowSize:         settings.WindowSize{Width: 1280, Height: 800},
		SensitiveFiles: sensitive.Config{
			Scope: "collaborators", Off: []string{"docs"}, Patterns: []string{"secret-dir/**"},
		},
		Branches: branchrules.Rules{
			Own: true, PullRequest: true, Merged: false, Protected: []string{"main", "release-private-*"},
		},
		DefaultProtectedBranches: []string{"main", "master"},
		MenuBar:                  true,
		BannersOff:               []notifyprefs.Kind{"session_waiting"},
		Theme:                    "dark",
		TextSize:                 "default",
		InboxOrder:               settings.InboxOrder{StatusOrder: []string{"Private status"}},
		MergeMethod:              "squash",
		Integrations: integrationprefs.Config{
			Switches:      map[integrationprefs.ID]integrationprefs.Switch{integrationprefs.Scripts: "on"},
			Paths:         map[integrationprefs.ID]string{integrationprefs.MageX: "/srv/private-user/bin/magex"},
			RunsIn:        integrationprefs.RunsInTerminal,
			ScriptSources: integrationprefs.ScriptSources{MagexTargets: true, RepoFiles: false},
			LastRepo:      "~/projects/private-repo",
		},
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

func TestABugReportStatesTheSettingsAndIntegrationsWithNothingPersonal(t *testing.T) {
	t.Parallel()

	s := privateSettings()
	report := joined([]appinfo.StateSection{settingsSection(s), integrationsSection(privateTools(), s.Integrations)})

	for _, word := range privateWords() {
		assert.NotContains(t, report, word, "a bug report never writes out a name or path of the user's own")
	}

	for _, want := range []string{
		"Theme: dark · text size: default · window: 1280×800",
		"Terminal: iterm2 · menu bar: on · banners off: session_waiting",
		"Default agent: claude · profiles: claude the user's own, codex default",
		"Start in a worktree: on · automatic cleanup: on · follow sessions: on",
		"Clone root: the user's own · other locations: 2",
		"Snooze: 3 days · wake on commits: 1, reviews: decisions, comments: 1",
		"Sensitive files: collaborators · categories off: docs · own patterns: 1",
		"Branch rules: own on, pull request on, merged off · protected branches: the user's own (2)",
		"Trusted bots: the user's own (2)",
		"Merge method: squash · lists sorted by status: 0 · collapsed lists: 0",
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

	report := joined([]appinfo.StateSection{
		settingsSection(s), integrationsSection(integrations.Tools{}, integrationprefs.Config{}),
	})

	for _, want := range []string{
		"Clone root: the default",
		"Trusted bots: the default (1)",
		"protected branches: the default (2)",
		"profiles: the agents' defaults",
		"Scripts: auto · magex targets: off · repository files: off",
		"Runs in: app",
		"Login shell's environment: not read yet",
	} {
		assert.Contains(t, report, want)
	}
}
