package core

import (
	"bytes"
	"log/slog"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
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

func TestABugReportLeavesOutWhatItCantRead(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))

	var logs bytes.Buffer

	state := bugReportState{
		settings: c.Settings, tools: c.Integrations, updates: c.UpdateEngine, routines: c.Autopilot,
		logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}

	require.NoError(t, c.Close())

	got := state.BugReportState(t.Context())

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
