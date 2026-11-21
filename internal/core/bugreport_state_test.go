package core

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// sectionTitles are the titles of a report's sections, in order.
func sectionTitles(body string) []string {
	var out []string

	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "**") && strings.HasSuffix(line, "**") {
			out = append(out, strings.Trim(line, "*"))
		}
	}

	return out
}

func TestABugReportFromTheAppStatesEachPartOfItsState(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))

	_, err := c.Autopilot.Save(t.Context(), autopilot.Draft{Name: "Triage private-repo", Spec: boundSpec()})
	require.NoError(t, err)

	link, err := url.Parse(c.AppInfo.BugReportURL(t.Context()))
	require.NoError(t, err)

	var body string
	for _, values := range link.Query() {
		body += strings.Join(values, "\n")
	}

	assert.Equal(t, []string{"Settings", "Autopilot", "Updates", "Integrations"}, sectionTitles(body),
		"the report holds the settings, Autopilot, the updates, and the integrations, in that order")
	assert.Contains(t, body, "Routines: 1 (")
	assert.Contains(t, body, "Updates: ")
	assert.NotContains(t, body, "private-repo", "and names no routine")
}

// reportBody is the environment a bug report from c fills in.
func reportBody(t *testing.T, c *Core) string {
	t.Helper()

	link, err := url.Parse(c.AppInfo.BugReportURL(t.Context()))
	require.NoError(t, err)

	return link.Query().Get("environment")
}

func TestABugReportFromTheAppWordsTheSettingsKeptElsewhere(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude", ".claude_employer-profile", ".codex")
	c := bootstrap(t, opts)
	ctx := t.Context()

	own, err := c.Prompts.Create(ctx, prompts.Input{
		Name: "Triage private-repo", Nickname: "private-prompt", AppliesTo: prompts.AppliesToPR, Body: "Look at it.",
	})
	require.NoError(t, err)
	require.NoError(t, c.Prompts.SetDefault(ctx, item.KindPR, own.ID))

	for _, name := range []string{"", "private-repo"} {
		_, err = c.LaunchDefaults.Save(ctx, launchdefaults.Input{
			Owner: "private-org", Name: name, AutopilotOutside: launchdefaults.OutsideOn,
		})
		require.NoError(t, err)
	}

	require.NoError(t, os.RemoveAll(filepath.Join(opts.Home, ".claude_employer-profile")))

	_, err = c.Settings.SetAutopilotEnabled(ctx, true)
	require.NoError(t, err)

	body := reportBody(t, c)

	for _, word := range privateWords() {
		assert.NotContains(t, body, word, "a bug report never names a prompt, a profile, an org, or a repository")
	}

	for _, want := range []string{
		"Launching prompts: pull requests the user's own · issues ",
		"Reviewing replies: approve ",
		"Profiles: claude 2, 2 signed in, 1 missing folders · codex 1, 0 signed in, 0 missing folders",
		"launch defaults 1 orgs, 1 repositories",
		"Labs: Autopilot (beta) on",
		"Describe it's agent: profile the default · model the default · effort the default",
		"Routines: 0 (",
	} {
		assert.Contains(t, body, want)
	}

	assert.Equal(t, []string{"Settings", "Autopilot", "Updates", "Integrations"}, sectionTitles(body),
		"Autopilot on states its section without a routine")
}

// fixedState is a report's state, worded already.
type fixedState []appinfo.StateSection

// BugReportState returns the state.
func (f fixedState) BugReportState(context.Context) []appinfo.StateSection {
	return f
}

func TestABugReportStaysShortEnoughToOpen(t *testing.T) {
	t.Parallel()

	s := privateSettings()
	routines := make([]autopilot.Routine, 0, 40)

	for i := range 40 {
		routines = append(routines, autopilot.Routine{
			Name: fmt.Sprintf("private-repo %d", i), Mode: autopilotprefs.ModeAuto, Enabled: i%2 == 0,
		})
	}

	state := fixedState{
		settingsSection(s, privateOthers()),
		{Title: "Autopilot", Lines: []string{
			autopilotSettingsLine(s.Autopilot),
			describeAgentLine(describeit.AgentChoice{Profile: "employer-profile", Model: "opus", Effort: "high"}),
			routinesLine(routines),
			"Runs today: 10 in the background · 4 in the terminal · 25 GitHub actions",
		}},
		updatesSection(update.Status{
			Mode: updateprefs.ModeDownload, Flavor: update.FlavorRelease, Enabled: true, State: update.StateIdle,
			Latest: "10.20.30", InstallFailed: update.InstallFailure{Version: "10.20.29"},
		}),
		integrationsSection(privateTools(), s.Integrations),
	}

	link := appinfo.NewService(appinfo.Config{
		Version: "v10.20.30-0-gabcdef0", Release: "10.20.30", Build: "1234", System: "macOS 26.1", State: state,
	}).BugReportURL(t.Context())

	// Browsers and GitHub open a link of this length; one much longer can be refused.
	assert.Less(t, len(link), 8000, "a full report's link stays short enough to open")
	t.Logf("a full report's link is %d characters", len(link))
}

func TestABugReportLeavesOutWhatItCantRead(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))

	var logs bytes.Buffer

	state := bugReportState{
		settings: c.Settings, tools: c.Integrations, updates: c.UpdateEngine, routines: c.Autopilot,
		prompts: c.Prompts, launchDefaults: c.LaunchDefaults, profiles: c.Profiles,
		logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}

	require.NoError(t, c.Close())

	got := state.BugReportState(t.Context())

	assert.Equal(t, otherSettings{}, state.otherSettings(t.Context()),
		"the prompts, the profiles, and the launch defaults that can't be read are left out")

	for _, part := range []string{"the default prompts", "the profiles", "the launch defaults"} {
		assert.Contains(t, logs.String(), "a bug report leaves out "+part)
	}

	titles := make([]string, 0, len(got))
	for _, s := range got {
		titles = append(titles, s.Title)
	}

	assert.Equal(t, []string{"Updates"}, titles,
		"the report still opens, holding what a source build knows of its updates without the database")
	assert.Contains(t, logs.String(), "a bug report leaves out the settings")
	assert.Contains(t, logs.String(), "a bug report leaves out the integrations")
}

func TestABugReportCountsBrokenRoutinesAndAnotherAccountsWithoutNamingThem(t *testing.T) {
	t.Parallel()

	got := routinesLine([]autopilot.Routine{
		{Name: "private-repo checks", Mode: autopilotprefs.ModeAuto, Enabled: true},
		{Name: "Release private-bot", Mode: autopilotprefs.ModeDryRun, BrokenReason: "its prompt is gone"},
		{Name: "private-org triage", Mode: autopilotprefs.ModeAsk, OtherAccount: true},
	})

	assert.Equal(t, "Routines: 3 (1 dry run, 1 ask first, 1 autopilot) · on: 1 · broken: 1 · another account's: 1", got)
}

func TestABugReportWordsWhereTheAppsUpdatesStand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status update.Status
		want   string
	}{
		{
			name:   "never checks",
			status: update.Status{Mode: updateprefs.ModeOff, Flavor: update.FlavorRelease, Enabled: true, State: update.StateIdle},
			want:   "Updates: don't check · release build · idle",
		},
		{
			name: "a mode a later version writes",
			status: update.Status{
				Mode: "weekly", Flavor: update.FlavorRehearsal, Enabled: true, State: update.StateChecking,
				InstallFailed: update.InstallFailure{Version: "1.4.0"},
			},
			want: "Updates: weekly · rehearsal build · checking · last update: 1.4.0 didn't install",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := updatesSection(tt.status)
			require.Len(t, got.Lines, 2)
			assert.Equal(t, tt.want, got.Lines[0])
		})
	}
}

func TestABugReportNamesWhereEachCopyOfAToolCameFrom(t *testing.T) {
	t.Parallel()

	for source, words := range map[integrations.Source]string{
		integrations.SourceApp:         "1.30.0 from installed by the app",
		integrations.SourceGoInstall:   "1.30.0 from go install",
		integrations.SourceDevelopment: "1.30.0 from a development build",
		integrations.SourceRelease:     "1.30.0 from a GitHub release",
		"":                             "1.30.0",
	} {
		got := toolLine(integrations.Tool{
			Name: "magex", Switch: integrationprefs.SwitchAuto, On: true, State: integrations.StateFound,
			Copy: integrations.Copy{Version: integrations.Version{Raw: "1.30.0"}, Source: source},
		})
		assert.Equal(t, "magex: on (auto), found · "+words, got, string(source))
	}

	section := integrationsSection(integrations.Tools{ShellChecked: true}, integrationprefs.Config{})
	assert.Contains(t, section.Lines, "Login shell's environment: couldn't be read, so a basic PATH is used",
		"a login shell that couldn't be read says the tools are looked for with a basic PATH")
}
