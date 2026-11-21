package core

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// bugReportState words the app's state for a bug report (appinfo.StateReader): the
// settings, one line per page of Settings (bugreport_settings.go), the default prompts,
// the profiles, and the launch defaults among them; Autopilot, while it is on or the user
// has a routine; the app's updates; and the integrations. Nothing personal is in them: a
// name or a path the user chose (a profile, the clone root, a chosen copy of a tool, a
// pattern, a bot, a branch, a label, a workflow, a prompt, an org or a repository with
// launch defaults) is told only as the default or the user's own, or counted, never
// written out, and no account is read at all. Every service may be nil; a part that can't
// be read is logged and left out, and the report still opens. Core wires the prompts, the
// launch defaults, the profiles, and Describe it in once they are built.
type bugReportState struct {
	settings       *settings.Service
	tools          *integrations.Service
	updates        *update.Engine
	routines       *autopilot.Service
	prompts        *prompts.Service
	launchDefaults *launchdefaults.Service
	profiles       *profiles.Service
	describe       *describeit.Service
	logger         *slog.Logger
}

// BugReportState returns the settings, Autopilot, the app's updates, and the
// integrations, as the report words them.
func (b bugReportState) BugReportState(ctx context.Context) []appinfo.StateSection {
	var (
		out   []appinfo.StateSection
		prefs integrationprefs.Config
	)

	if b.settings != nil {
		s, err := b.settings.Get(ctx)
		if err != nil {
			b.logger.WarnContext(ctx, "a bug report leaves out the settings", "error", err)
		} else {
			out = append(out, settingsSection(s, b.otherSettings(ctx)))
			out = append(out, b.autopilotState(ctx, s.Autopilot)...)
			prefs = s.Integrations
		}
	}

	out = append(out, b.updatesState(ctx)...)

	if b.tools != nil {
		list, err := b.tools.List(ctx)
		if err != nil {
			b.logger.WarnContext(ctx, "a bug report leaves out the integrations", "error", err)

			return out
		}

		out = append(out, integrationsSection(list, prefs))
	}

	return out
}

// otherSettings reads the settings the settings service doesn't keep, every one of them a
// local read: the default prompts and replies, the profiles, and the launch defaults. Each
// that can't be read is logged and left out.
func (b bugReportState) otherSettings(ctx context.Context) otherSettings {
	other := otherSettings{profiles: b.profileParts(ctx), launchDefaults: b.launchDefaultsWords(ctx)}
	other.replies, other.prompts = b.promptParts(ctx)

	return other
}

// promptParts words the default replies and the default prompts, or nothing when the
// prompt library can't be read.
func (b bugReportState) promptParts(ctx context.Context) (replies, launches []string) {
	if b.prompts == nil {
		return nil, nil
	}

	list, err := b.prompts.List(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out the default prompts", "error", err)

		return nil, nil
	}

	defaults, err := b.prompts.Defaults(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out the default prompts", "error", err)

		return nil, nil
	}

	library := promptsByID(list)

	return replyParts(defaults, library), launchPromptParts(defaults, library)
}

// profileParts counts the profiles, or says nothing when they can't be read.
func (b bugReportState) profileParts(ctx context.Context) []string {
	if b.profiles == nil {
		return nil
	}

	list, err := b.profiles.List(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out the profiles", "error", err)

		return nil
	}

	return profileParts(list)
}

// launchDefaultsWords counts the launch defaults, or says nothing when they can't be read.
func (b bugReportState) launchDefaultsWords(ctx context.Context) string {
	if b.launchDefaults == nil {
		return ""
	}

	list, err := b.launchDefaults.List(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out the launch defaults", "error", err)

		return ""
	}

	return launchDefaultsWords(list)
}

// autopilotState is Autopilot as the report words it: its settings, the agent Describe it
// runs, and its routines, counted by mode, by switch, and by whether they are broken or
// another account's, never named, with today's runs. It says nothing while Autopilot is
// off and the user has no routine (not deleted). Routines that can't be read are logged
// and left out, and Autopilot with them unless it is on.
func (b bugReportState) autopilotState(ctx context.Context, c autopilotprefs.Config) []appinfo.StateSection {
	list, listed := b.listRoutines(ctx)
	if !c.Enabled && len(list) == 0 {
		return nil
	}

	lines := append([]string{autopilotSettingsLine(c)}, b.describeAgentLines(ctx)...)

	if listed {
		lines = append(lines, b.routineLines(ctx, list)...)
	}

	return []appinfo.StateSection{{Title: "Autopilot", Lines: lines}}
}

// listRoutines lists the routines, and reports whether they could be read.
func (b bugReportState) listRoutines(ctx context.Context) ([]autopilot.Routine, bool) {
	if b.routines == nil {
		return nil, false
	}

	list, err := b.routines.List(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out the routines", "error", err)

		return nil, false
	}

	return list, true
}

// describeAgentLines words the agent Describe it runs, or nothing when it can't be read.
func (b bugReportState) describeAgentLines(ctx context.Context) []string {
	if b.describe == nil {
		return nil
	}

	choice, err := b.describe.Agent(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out Describe it's agent", "error", err)

		return nil
	}

	return []string{describeAgentLine(choice)}
}

// routineLines counts the routines and today's runs, the runs left out when they can't
// be read.
func (b bugReportState) routineLines(ctx context.Context, list []autopilot.Routine) []string {
	today, err := b.routines.RunsToday(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out today's runs", "error", err)

		return []string{routinesLine(list)}
	}

	return []string{routinesLine(list), fmt.Sprintf(
		"Runs today: %d in the background · %d in the terminal · %d GitHub actions",
		today.Background, today.Terminal, today.Actions,
	)}
}

// autopilotSettingsLine words Autopilot's settings: whether it is paused, the mode new
// routines start in, and the limits. Whether it is on is the Labs line's.
func autopilotSettingsLine(c autopilotprefs.Config) string {
	daily := "off"
	if !c.DailyCap.Off() {
		daily = fmt.Sprintf("%d %s", c.DailyCap.Amount, c.DailyCap.Unit)
	}

	return fmt.Sprintf("Paused: %s · new routines: %s · agents at once: %d · runs a day: %d · "+
		"actions a day: %d · daily cap: %s · keep awake: %s · hold on outage: %s · catch up after: %d h · "+
		"history: %d days, logs: %d · merge own past the rules: %s",
		onOff(c.Paused), c.DefaultMode.Label(), c.AgentsAtOnce, c.RunsPerDay, c.ActionsPerDay, daily, onOff(c.KeepAwake),
		onOff(c.HoldOnOutage), c.CatchUpHours, c.HistoryDays, c.LogDays, onOff(c.MergePastRules))
}

// routinesLine counts the routines by mode, and those on, broken, or another account's.
func routinesLine(list []autopilot.Routine) string {
	modes := map[autopilotprefs.Mode]int{}
	on, broken, other := 0, 0, 0

	for _, r := range list {
		modes[r.Mode]++

		if r.Enabled {
			on++
		}

		if r.BrokenReason != "" {
			broken++
		}

		if r.OtherAccount {
			other++
		}
	}

	byMode := make([]string, 0, len(autopilotprefs.Modes()))
	for _, m := range autopilotprefs.Modes() {
		byMode = append(byMode, fmt.Sprintf("%d %s", modes[m], strings.ToLower(m.Label())))
	}

	return fmt.Sprintf("Routines: %d (%s) · on: %d · broken: %d · another account's: %d", len(list),
		strings.Join(byMode, ", "), on, broken, other)
}

// updatesState is the app's updates as the report words them, or nothing when they can't
// be read.
func (b bugReportState) updatesState(ctx context.Context) []appinfo.StateSection {
	if b.updates == nil {
		return nil
	}

	status, err := b.updates.Status(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out the updates", "error", err)

		return nil
	}

	return []appinfo.StateSection{updatesSection(status)}
}

// updateModeWords words what the app does when a new version is out, as Settings does.
func updateModeWords(mode updateprefs.Mode) string {
	switch mode {
	case updateprefs.ModeNotify:
		return "just tell me"
	case updateprefs.ModeOff:
		return "don't check"
	case updateprefs.ModeDownload:
		return "download it and tell me"
	default:
		return string(mode)
	}
}

// updatesSection words the app's updates for a bug report: the setting, the kind of
// build, whether it updates, where the update stands, and the outcome of the last restart
// to update. No path or log is written out.
func updatesSection(s update.Status) appinfo.StateSection {
	line := fmt.Sprintf("Updates: %s · %s build", updateModeWords(s.Mode), s.Flavor)

	switch {
	case !s.Enabled:
		line += " · off"
	case s.Latest != "":
		line += fmt.Sprintf(" · %s, %s", s.State, s.Latest)
	default:
		line += " · " + string(s.State)
	}

	if s.InstallFailed.Version != "" {
		line += " · last update: " + s.InstallFailed.Version + " didn't install"
	}

	return appinfo.StateSection{Title: "Updates", Lines: []string{
		line, fmt.Sprintf("Location: %s · can update there: %s", s.Location.Kind, yesNo(s.Location.OK)),
	}}
}

// yesNo words a yes or no.
func yesNo(yes bool) string {
	if yes {
		return "yes"
	}

	return "no"
}

// sourceWords names where a copy of a tool came from, or "" for a source it doesn't know.
func sourceWords(source integrations.Source) string {
	switch source {
	case integrations.SourceApp:
		return "installed by the app"
	case integrations.SourceHomebrew:
		return "Homebrew"
	case integrations.SourceGoInstall:
		return "go install"
	case integrations.SourceDevelopment:
		return "a development build"
	case integrations.SourceRelease:
		return "a GitHub release"
	default:
		return ""
	}
}

// toolLine words one tool: its switch, the copy in use (its version, where it came from,
// and whether it is a link or one the user chose), the newest release, and the other
// copies found, counted. No path is written out.
func toolLine(t integrations.Tool) string {
	parts := []string{fmt.Sprintf("%s: %s (%s), %s", t.Name, onOff(t.On), t.Switch, t.State)}

	if t.Copy.Version.Raw != "" {
		copyWords := t.Copy.Version.Raw
		if from := sourceWords(t.Copy.Source); from != "" {
			copyWords += " from " + from
		}

		if t.Copy.Link {
			copyWords += ", a link"
		}

		parts = append(parts, copyWords)
	}

	if t.Chosen != "" {
		parts = append(parts, "a copy the user chose")
	}

	if t.Latest.Version != "" {
		parts = append(parts, "latest "+t.Latest.Version)
	}

	if len(t.Others) > 0 {
		parts = append(parts, fmt.Sprintf("%d other copies", len(t.Others)))
	}

	return strings.Join(parts, " · ")
}

// integrationsSection words the integrations for a bug report: each tool, the Scripts
// switch, and where runs go.
func integrationsSection(list integrations.Tools, prefs integrationprefs.Config) appinfo.StateSection {
	lines := make([]string, 0, len(list.Tools)+3)

	for _, t := range list.Tools {
		lines = append(lines, toolLine(t))
	}

	scripts := cmp.Or(prefs.Switches[integrationprefs.Scripts], integrationprefs.SwitchAuto)

	runsIn := cmp.Or(prefs.RunsIn, integrationprefs.RunsInApp)

	lines = append(lines,
		fmt.Sprintf("Scripts: %s · magex targets: %s · repository files: %s", scripts,
			onOff(prefs.ScriptSources.MagexTargets), onOff(prefs.ScriptSources.RepoFiles)),
		fmt.Sprintf("Runs in: %s · offer to release after a merge: %s", runsIn, onOff(prefs.OfferAfterMerge)),
		fmt.Sprintf("Login shell's environment: %s", shellWords(list)),
	)

	return appinfo.StateSection{Title: "Integrations", Lines: lines}
}

// shellWords says whether the login shell's environment was read.
func shellWords(list integrations.Tools) string {
	switch {
	case !list.ShellChecked:
		return "not read yet"
	case list.ShellRead:
		return "read"
	default:
		return "couldn't be read, so a basic PATH is used"
	}
}
