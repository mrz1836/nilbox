package core

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// bugReportState words the app's state for a bug report (appinfo.StateReader): the
// settings and the integrations, with nothing personal in them. A name or a path the
// user chose (a profile, the clone root, a chosen copy of a tool, a pattern, a bot, a
// branch) is told only as the default or the user's own, or counted, never written out;
// no account, repository, or organization is read at all. A part that can't be read is
// left out, and the report still opens.
type bugReportState struct {
	settings *settings.Service
	tools    *integrations.Service
	updates  *update.Engine
	routines *autopilot.Service
	logger   *slog.Logger
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
			out = append(out, settingsSection(s))
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

// autopilotState is Autopilot as the report words it: its settings, and its routines
// counted by mode, by switch, and by whether they are broken or another account's, never
// named. It says nothing while the user has no routine (not deleted), or the routines
// can't be read, which is logged.
func (b bugReportState) autopilotState(ctx context.Context, c autopilotprefs.Config) []appinfo.StateSection {
	if b.routines == nil {
		return nil
	}

	list, err := b.routines.List(ctx)
	if err != nil {
		b.logger.WarnContext(ctx, "a bug report leaves out the routines", "error", err)

		return nil
	}

	if len(list) == 0 {
		return nil
	}

	lines := []string{autopilotSettingsLine(c), routinesLine(list)}

	if today, todayErr := b.routines.RunsToday(ctx); todayErr == nil {
		lines = append(lines, fmt.Sprintf("Runs today: %d in the background · %d in the terminal · %d GitHub actions",
			today.Background, today.Terminal, today.Actions))
	} else {
		b.logger.WarnContext(ctx, "a bug report leaves out today's runs", "error", todayErr)
	}

	return []appinfo.StateSection{{Title: "Autopilot", Lines: lines}}
}

// autopilotSettingsLine words Autopilot's settings: whether it is on and paused, the mode
// new routines start in, and the limits.
func autopilotSettingsLine(c autopilotprefs.Config) string {
	daily := "off"
	if !c.DailyCap.Off() {
		daily = fmt.Sprintf("%d %s", c.DailyCap.Amount, c.DailyCap.Unit)
	}

	return fmt.Sprintf("Beta: %s · paused: %s · new routines: %s · agents at once: %d · runs a day: %d · "+
		"actions a day: %d · daily cap: %s · keep awake: %s · hold on outage: %s · catch up after: %d h · "+
		"history: %d days, logs: %d",
		onOff(c.Enabled), onOff(c.Paused), c.DefaultMode.Label(), c.AgentsAtOnce, c.RunsPerDay, c.ActionsPerDay, daily, onOff(c.KeepAwake),
		onOff(c.HoldOnOutage), c.CatchUpHours, c.HistoryDays, c.LogDays)
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

// onOff words a switch.
func onOff(on bool) string {
	if on {
		return "on"
	}

	return "off"
}

// ownOrDefault says whether the user changed a setting from its default, without saying
// what to.
func ownOrDefault(isDefault bool) string {
	if isDefault {
		return "the default"
	}

	return "the user's own"
}

// sameSet reports whether a and b hold the same strings, in any order.
func sameSet(a, b []string) bool {
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

// settingsSection words the settings for a bug report.
func settingsSection(s settings.Settings) appinfo.StateSection {
	banners := "none"

	if len(s.BannersOff) > 0 {
		off := make([]string, 0, len(s.BannersOff))
		for _, k := range s.BannersOff {
			off = append(off, string(k))
		}

		banners = strings.Join(off, ", ")
	}

	sensitiveOff := "none"
	if len(s.SensitiveFiles.Off) > 0 {
		sensitiveOff = strings.Join(s.SensitiveFiles.Off, ", ")
	}

	return appinfo.StateSection{Title: "Settings", Lines: []string{
		fmt.Sprintf("Theme: %s · text size: %s · toasts: %d s · window: %d×%d", s.Theme, s.TextSize,
			s.ToastSeconds, s.WindowSize.Width, s.WindowSize.Height),
		fmt.Sprintf("Terminal: %s · menu bar: %s · banners off: %s", s.Terminal, onOff(s.MenuBar), banners),
		fmt.Sprintf("Default agent: %s · profiles: %s", s.DefaultAgent, profileWords(s.DefaultProfiles)),
		fmt.Sprintf("Codex effort: %s · Codex fast: %s", onOff(s.CodexEffort), onOff(s.CodexFast)),
		fmt.Sprintf("Start in a worktree: %s · automatic cleanup: %s · follow sessions: %s",
			onOff(s.WorktreeDefault), onOff(s.WorktreeAutoCleanup), onOff(s.FollowSessions)),
		fmt.Sprintf("Clone root: %s · other locations: %d", ownOrDefault(s.CloneRoot == settings.DefaultCloneRoot),
			len(s.CloneLocations)),
		fmt.Sprintf("Snooze: %d days · wake on commits: %d, reviews: %s, comments: %d", s.SnoozeDays,
			s.SnoozeWake.Commits, s.SnoozeWake.Reviews, s.SnoozeWake.Comments),
		fmt.Sprintf("Sensitive files: %s · categories off: %s · own patterns: %d", s.SensitiveFiles.Scope,
			sensitiveOff, len(s.SensitiveFiles.Patterns)),
		fmt.Sprintf("Branch rules: own %s, pull request %s, merged %s · protected branches: %s (%d)",
			onOff(s.Branches.Own), onOff(s.Branches.PullRequest), onOff(s.Branches.Merged),
			ownOrDefault(sameSet(s.Branches.Protected, s.DefaultProtectedBranches)), len(s.Branches.Protected)),
		fmt.Sprintf("Trusted bots: %s (%d)", ownOrDefault(sameSet(s.TrustedBots, s.DefaultTrustedBots)),
			len(s.TrustedBots)),
		fmt.Sprintf("Merge method: %s · lists sorted by status: %d · lists showing one kind: %d · collapsed lists: %d",
			s.MergeMethod, len(s.InboxOrder.StatusSorted), len(s.InboxOrder.Kinds), len(s.CollapsedSections)),
		fmt.Sprintf("Repositories and orgs: %d pinned, %d muted", len(s.RepoRules.Pinned), len(s.RepoRules.Muted)),
	}}
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
