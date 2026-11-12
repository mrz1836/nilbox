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
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/settings"
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
	logger   *slog.Logger
}

// BugReportState returns the settings and the integrations, as the report words them.
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
			prefs = s.Integrations
		}
	}

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
		fmt.Sprintf("Theme: %s · text size: %s · window: %d×%d", s.Theme, s.TextSize, s.WindowSize.Width,
			s.WindowSize.Height),
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
		fmt.Sprintf("Merge method: %s · lists sorted by status: %d · collapsed lists: %d", s.MergeMethod,
			len(s.InboxOrder.StatusSorted), len(s.CollapsedSections)),
	}}
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
