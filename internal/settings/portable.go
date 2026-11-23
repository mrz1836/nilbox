package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// BackupPart is the settings' part of a backup of the user's setup: their settings and, apart, their
// repositories, in portable form, and what importing each does. Core adapts it to a backup.Part; it is a type of its own, apart
// from the Service, so the window, which binds the Service, never reaches it.
type BackupPart struct {
	s *Service
}

// NewBackupPart returns the backup's part over s.
func NewBackupPart(s *Service) *BackupPart {
	return &BackupPart{s: s}
}

// Portable is the user's settings in portable form, as a backup holds them: every setting
// but the repositories (see PortableRepos) and the window's memory (its size, the folded
// sections, the tips put away, the review sheet's tab, the Notifications page's tab, the
// conversations' order, the Scripts tab's last page and clone, and the Release and Deps
// pages' sorts). Each field is a pointer, or a map whose entries each stand alone, so a
// field a backup lacks is left as it is when the backup is imported.
type Portable struct {
	DefaultAgent        *agent.ID            `json:"defaultAgent,omitempty"`
	DefaultProfiles     map[agent.ID]string  `json:"defaultProfiles,omitempty"`
	Terminal            *terminal.ID         `json:"terminal,omitempty"`
	WorktreeDefault     *bool                `json:"worktreeDefault,omitempty"`
	FollowSessions      *bool                `json:"followSessions,omitempty"`
	CodexEffort         *bool                `json:"codexEffort,omitempty"`
	CodexFast           *bool                `json:"codexFast,omitempty"`
	ClaudeAPIKey        *bool                `json:"claudeApiKey,omitempty"`
	CodexAPIKey         *bool                `json:"codexApiKey,omitempty"`
	WorktreeAutoCleanup *bool                `json:"worktreeAutoCleanup,omitempty"`
	Branches            *branchrules.Rules   `json:"branches,omitempty"`
	TrustedBots         *[]string            `json:"trustedBots,omitempty"`
	SnoozeDays          *int                 `json:"snoozeDays,omitempty"`
	SnoozeWake          *snooze.Rules        `json:"snoozeWake,omitempty"`
	SensitiveScope      *sensitive.Scope     `json:"sensitiveScope,omitempty"`
	SensitiveOff        *[]string            `json:"sensitiveOff,omitempty"`
	SensitivePatterns   *[]string            `json:"sensitivePatterns,omitempty"`
	MenuBar             *bool                `json:"menuBar,omitempty"`
	BannersOff          *[]notifyprefs.Kind  `json:"bannersOff,omitempty"`
	GitHubNotifications *PortableThreadRules `json:"githubNotifications,omitempty"`
	Refresh             map[RefreshKind]int  `json:"refresh,omitempty"`
	WatchPushes         *bool                `json:"watchPushes,omitempty"`
	ServiceStatus       *PortableStatus      `json:"serviceStatus,omitempty"`
	MergeMethod         *MergeMethod         `json:"mergeMethod,omitempty"`
	BypassOwn           *bool                `json:"bypassOwn,omitempty"`
	NudgeAfterHours     *int                 `json:"nudgeAfterHours,omitempty"`
	NudgeSnooze         *bool                `json:"nudgeSnooze,omitempty"`
	Theme               *Theme               `json:"theme,omitempty"`
	TextSize            *TextSize            `json:"textSize,omitempty"`
	Celebrations        *Celebrations        `json:"celebrations,omitempty"`
	ToastSeconds        *int                 `json:"toastSeconds,omitempty"`
	InboxOrder          *InboxOrder          `json:"inboxOrder,omitempty"`
	HiddenLabels        *[]string            `json:"hiddenLabels,omitempty"`
	CardAvatars         *bool                `json:"cardAvatars,omitempty"`
	CardDensity         *CardDensity         `json:"cardDensity,omitempty"`
	CardUnread          *bool                `json:"cardUnread,omitempty"`
	IgnoredWorkflows    *[]string            `json:"ignoredWorkflows,omitempty"`
	MergeBotsOn         *bool                `json:"mergeBotsOn,omitempty"`
	MergeBots           *[]string            `json:"mergeBots,omitempty"`
	Integrations        *PortableIntegration `json:"integrations,omitempty"`
	UpdateMode          *updateprefs.Mode    `json:"updateMode,omitempty"`
	Autopilot           *PortableAutopilot   `json:"autopilot,omitempty"`
}

// PortableAutopilot is Autopilot's settings in portable form, each left as it is when a
// backup lacks it. The routines are a backup section of their own, and the engine's own
// state is no setting.
type PortableAutopilot struct {
	Enabled        *bool                    `json:"enabled,omitempty"`
	Paused         *bool                    `json:"paused,omitempty"`
	DefaultMode    *autopilotprefs.Mode     `json:"defaultMode,omitempty"`
	AgentsAtOnce   *int                     `json:"agentsAtOnce,omitempty"`
	RunsPerDay     *int                     `json:"runsPerDay,omitempty"`
	ActionsPerDay  *int                     `json:"actionsPerDay,omitempty"`
	ActionsPerHour *int                     `json:"actionsPerHour,omitempty"`
	DailyCap       *autopilotprefs.DailyCap `json:"dailyCap,omitempty"`
	KeepAwake      *bool                    `json:"keepAwake,omitempty"`
	HoldOnOutage   *bool                    `json:"holdOnOutage,omitempty"`
	CatchUpHours   *int                     `json:"catchUpHours,omitempty"`
	HistoryDays    *int                     `json:"historyDays,omitempty"`
	LogDays        *int                     `json:"logDays,omitempty"`
}

// legacyRefreshGitHubStatus is the refresh kind the service status check had while it read
// GitHub's status page alone, which an older backup holds. It is no RefreshKind of its own.
const legacyRefreshGitHubStatus = "githubStatus"

// UnmarshalJSON reads a backup's settings, taking an older backup's interval of the GitHub
// status check as the service status check's, unless the backup holds that too.
func (p *Portable) UnmarshalJSON(data []byte) error {
	type plain Portable

	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}

	if n, ok := v.Refresh[RefreshKind(legacyRefreshGitHubStatus)]; ok {
		if _, has := v.Refresh[RefreshServiceStatus]; !has {
			v.Refresh[RefreshServiceStatus] = n
		}

		delete(v.Refresh, RefreshKind(legacyRefreshGitHubStatus))
	}

	*p = Portable(v)

	return nil
}

// PortableStatus is what the service status check watches in portable form, each left as
// it is when a backup lacks it: the status pages turned off, and every part watched,
// across the providers, by the app's own names for them (statusprefs.PartID).
type PortableStatus struct {
	Off   *[]statusprefs.ProviderID `json:"off,omitempty"`
	Parts *[]statusprefs.PartID     `json:"parts,omitempty"`
}

// PortableThreadRules are the rules for the user's GitHub notifications in portable form,
// each left as it is when a backup lacks it.
type PortableThreadRules struct {
	Read        *bool                  `json:"read,omitempty"`
	Auto        *bool                  `json:"auto,omitempty"`
	Action      *threadrules.Action    `json:"action,omitempty"`
	TriggersOff *[]threadrules.Trigger `json:"triggersOff,omitempty"`
	Watching    *bool                  `json:"watching,omitempty"`
	BotActivity *bool                  `json:"botActivity,omitempty"`
}

// PortableIntegration is the integrations' settings in portable form, each left as it is
// when a backup lacks it: each integration's switch, the copy of each tool chosen, where
// the runs go, whether a merge offers a release, Repo scripts' sources, and whether a run
// it starts shows beside it.
type PortableIntegration struct {
	Switches        map[integrationprefs.ID]integrationprefs.Switch `json:"switches,omitempty"`
	Paths           map[integrationprefs.ID]string                  `json:"paths,omitempty"`
	RunsIn          *integrationprefs.RunsIn                        `json:"runsIn,omitempty"`
	OfferAfterMerge *bool                                           `json:"offerAfterMerge,omitempty"`
	MagexTargets    *bool                                           `json:"magexTargets,omitempty"`
	RepoFiles       *bool                                           `json:"repoFiles,omitempty"`
	ShowRuns        *bool                                           `json:"showRuns,omitempty"`
}

// Portable returns the user's settings in portable form, every field set.
func (bp *BackupPart) Portable(ctx context.Context) (Portable, error) {
	s := bp.s

	set, err := s.Get(ctx)
	if err != nil {
		return Portable{}, err
	}

	return portableOf(set), nil
}

// portableOf returns set in portable form, every field set.
func portableOf(set Settings) Portable {
	ig := set.Integrations
	gh := set.GitHubNotifications
	off, picked := set.ServiceStatus.Off(), set.ServiceStatus.Picked()

	return Portable{
		DefaultAgent: &set.DefaultAgent, DefaultProfiles: maps.Clone(set.DefaultProfiles), Terminal: &set.Terminal,
		WorktreeDefault: &set.WorktreeDefault, FollowSessions: &set.FollowSessions, CodexEffort: &set.CodexEffort,
		CodexFast: &set.CodexFast, ClaudeAPIKey: &set.ClaudeAPIKey, CodexAPIKey: &set.CodexAPIKey,
		WorktreeAutoCleanup: &set.WorktreeAutoCleanup, Branches: &set.Branches,
		TrustedBots: &set.TrustedBots, SnoozeDays: &set.SnoozeDays, SnoozeWake: &set.SnoozeWake,
		SensitiveScope: &set.SensitiveFiles.Scope, SensitiveOff: &set.SensitiveFiles.Off,
		SensitivePatterns: &set.SensitiveFiles.Patterns, MenuBar: &set.MenuBar, BannersOff: &set.BannersOff,
		GitHubNotifications: &PortableThreadRules{
			Read: &gh.Read, Auto: &gh.Auto, Action: &gh.Action, TriggersOff: &gh.TriggersOff, Watching: &gh.Watching,
			BotActivity: &gh.BotActivity,
		},
		Refresh: map[RefreshKind]int{
			RefreshInbox: set.Refresh.Inbox, RefreshChecks: set.Refresh.Checks,
			RefreshReleaseChecks: set.Refresh.ReleaseChecks, RefreshWorktrees: set.Refresh.Worktrees,
			RefreshNotifications: set.Refresh.Notifications, RefreshServiceStatus: set.Refresh.ServiceStatus,
		},
		WatchPushes:   &set.WatchPushes,
		ServiceStatus: &PortableStatus{Off: &off, Parts: &picked},
		MergeMethod:   &set.MergeMethod, BypassOwn: &set.BypassOwn, NudgeAfterHours: &set.NudgeAfterHours,
		NudgeSnooze: &set.NudgeSnooze,
		Theme:       &set.Theme, TextSize: &set.TextSize, Celebrations: &set.Celebrations, ToastSeconds: &set.ToastSeconds,
		InboxOrder: &set.InboxOrder, HiddenLabels: &set.HiddenLabels, CardAvatars: &set.CardAvatars,
		CardDensity: &set.CardDensity, CardUnread: &set.CardUnread, IgnoredWorkflows: &set.IgnoredWorkflows,
		MergeBotsOn: &set.MergeBotsOn, MergeBots: &set.MergeBots,
		Integrations: &PortableIntegration{
			Switches: maps.Clone(ig.Switches), Paths: maps.Clone(ig.Paths), RunsIn: &ig.RunsIn,
			OfferAfterMerge: &ig.OfferAfterMerge, MagexTargets: &ig.ScriptSources.MagexTargets,
			RepoFiles: &ig.ScriptSources.RepoFiles, ShowRuns: &ig.ShowRuns,
		},
		UpdateMode: &set.UpdateMode,
		Autopilot:  portableAutopilot(set.Autopilot),
	}
}

// portableAutopilot returns Autopilot's settings in portable form, every field set.
func portableAutopilot(c autopilotprefs.Config) *PortableAutopilot {
	return &PortableAutopilot{
		Enabled: &c.Enabled, Paused: &c.Paused, DefaultMode: &c.DefaultMode, AgentsAtOnce: &c.AgentsAtOnce, RunsPerDay: &c.RunsPerDay,
		ActionsPerDay: &c.ActionsPerDay, ActionsPerHour: &c.ActionsPerHour, DailyCap: &c.DailyCap, KeepAwake: &c.KeepAwake, HoldOnOutage: &c.HoldOnOutage,
		CatchUpHours: &c.CatchUpHours, HistoryDays: &c.HistoryDays, LogDays: &c.LogDays,
	}
}

// announcement is an event a setting's setter emits once it is stored, which an import
// that changed the setting emits too.
type announcement int

// The announcements.
const (
	tellsNothing announcement = iota
	tellsTheme
	tellsTextSize
	tellsMenuBar
	tellsSessions
	tellsIntegrations
	tellsToolCopy
	tellsThreadRules
	tellsIntervals
	tellsRepoRules
	tellsStatusRules
	tellsUpdates
	tellsAutopilot
)

// PortablePlan is what importing a backup's settings, or its repositories, does: the plan
// the window shows, and the writes it makes.
type PortablePlan struct {
	backup.Plan

	writes map[string]string
	tells  map[announcement]bool
}

// portableField is one setting a backup can hold: what the window calls it, the event its
// setter emits, and how the backup's value is checked and written.
type portableField struct {
	label string
	tells announcement

	// check returns the writes of p's value of the field, refusing one its setter would
	// refuse; has reports whether p holds the field at all.
	has   func(p Portable) bool
	check func(ctx context.Context, p Portable) (map[string]string, error)

	// show words p's value of the field, or "" to say nothing of it.
	show func(p Portable) string

	// notes words what of p's value of the field is left out though the rest is imported,
	// or nil for a field that leaves nothing out.
	notes func(p Portable) []string
}

// PlanPortable works out what importing p, a backup's settings, does, changing nothing:
// each setting p holds is checked as its setter checks it and set, and one p lacks is left
// as it is. A value the setter would refuse, a profile that isn't on this Mac, and a copy
// of a tool that isn't a program here are left out, each with a note. Both modes import
// settings the same way.
func (bp *BackupPart) PlanPortable(ctx context.Context, p Portable) (PortablePlan, error) {
	s := bp.s

	stored, err := s.prefs.GetMany(ctx, settingKeys()...)
	if err != nil {
		return PortablePlan{}, fmt.Errorf("read settings: %w", err)
	}

	before := fromStored(stored)
	current := portableOf(before)
	out := PortablePlan{writes: map[string]string{}, tells: map[announcement]bool{}}

	for _, f := range s.portableFields() {
		if f.has(p) {
			out.planField(ctx, f, p, current, stored, before)
		}
	}

	return out, nil
}

// planField plans f, one of the settings p holds, against stored, the preferences as they
// are, which read as before, the settings as they are, and current, them in portable form.
func (plan *PortablePlan) planField(ctx context.Context, f portableField, p, current Portable,
	stored map[string]string, before Settings,
) {
	writes, err := f.check(ctx, p)
	if err != nil {
		plan.AddNote(f.label + " was left as it is. " + uierr.Message(err))

		return
	}

	if f.notes != nil {
		for _, note := range f.notes(p) {
			plan.AddNote(note)
		}
	}

	if reflect.DeepEqual(fromStored(withWrites(stored, writes)), before) {
		plan.Add(backup.Item{Label: f.label, Change: backup.ChangeSame})

		return
	}

	item := backup.Item{Label: f.label, Change: backup.ChangeUpdate}
	if f.show != nil {
		item.Detail = f.show(p) + " (now " + f.show(current) + ")"
	}

	plan.Add(item)
	plan.tells[f.tells] = true
	maps.Copy(plan.writes, writes)
}

// ApplyPortable makes plan's writes inside tx, the import's transaction, and returns what
// it changed.
func (bp *BackupPart) ApplyPortable(ctx context.Context, tx *sql.Tx, plan PortablePlan) (backup.Count, error) {
	s := bp.s

	if err := s.prefs.WithTx(tx).SetMany(ctx, plan.writes); err != nil {
		return backup.Count{}, fmt.Errorf("save the settings: %w", err)
	}

	return plan.Count, nil
}

// Announce emits, once each, the events the setters of the settings plan changed emit, as
// they would had the user changed each one: the theme, the text size, the menu bar, the
// sessions followed, the integrations, the GitHub notification rules, the refresh
// intervals, the pinned and muted repositories, what the service status check watches,
// what the app does when a new version is out, and Autopilot's settings.
func (bp *BackupPart) Announce(ctx context.Context, plan PortablePlan) {
	s := bp.s

	set, err := s.Get(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "the settings were imported, but could not be read back", "error", err)

		return
	}

	events := map[announcement]event.Event{
		tellsTheme:        event.ThemeChanged{Theme: string(set.Theme)},
		tellsTextSize:     event.TextSizeChanged{Size: string(set.TextSize)},
		tellsMenuBar:      event.MenuBarChanged{On: set.MenuBar},
		tellsSessions:     event.SessionsUpdated{},
		tellsIntegrations: event.IntegrationsChanged{},
		tellsToolCopy:     event.IntegrationsChanged{CopyChosen: true},
		tellsThreadRules:  event.ThreadRulesChanged{},
		tellsIntervals:    event.IntervalsChanged{},
		tellsRepoRules:    event.RepoRulesChanged{},
		tellsStatusRules:  event.StatusRulesChanged{},
		tellsUpdates:      event.UpdatesChanged{},
		tellsAutopilot:    event.AutopilotChanged{},
	}

	// A chosen copy wakes the tools' finder, which tells the window too.
	if plan.tells[tellsToolCopy] {
		delete(events, tellsIntegrations)
	}

	for _, tell := range slices.Sorted(maps.Keys(plan.tells)) {
		if e, ok := events[tell]; ok {
			s.events.Emit(e)
		}
	}
}

// withWrites returns stored with writes made over it, leaving stored as it is.
func withWrites(stored, writes map[string]string) map[string]string {
	out := maps.Clone(stored)
	if out == nil {
		out = map[string]string{}
	}

	maps.Copy(out, writes)

	return out
}

// portableFields are the settings a backup can hold, in the order the window lists them.
func (s *Service) portableFields() []portableField {
	fields := []portableField{
		pointerField("Default coding agent", tellsNothing, func(p Portable) *agent.ID { return p.DefaultAgent },
			func(_ context.Context, id agent.ID) (map[string]string, error) { return checkDefaultAgent(id) }, agentName),
	}

	for _, id := range agent.IDs() {
		fields = append(fields, portableField{
			label: agentName(id) + " profile",
			has:   func(p Portable) bool { _, ok := p.DefaultProfiles[id]; return ok },
			check: func(ctx context.Context, p Portable) (map[string]string, error) {
				// The default profile is what the settings fall back to, found or not.
				if name := p.DefaultProfiles[id]; name == DefaultProfile {
					return map[string]string{KeyDefaultProfile(id): name}, nil
				}

				return s.checkDefaultProfile(ctx, id, p.DefaultProfiles[id])
			},
			show: func(p Portable) string { return p.DefaultProfiles[id] },
		})
	}

	fields = append(fields,
		pointerField("Terminal", tellsNothing, func(p Portable) *terminal.ID { return p.Terminal },
			func(_ context.Context, id terminal.ID) (map[string]string, error) { return checkTerminal(id) },
			func(id terminal.ID) string { return string(id) }),
		boolField("Start in a worktree", tellsNothing, KeyWorktreeDefault, func(p Portable) *bool { return p.WorktreeDefault }),
		boolField("Follow agent sessions", tellsSessions, KeyFollowSessions, func(p Portable) *bool { return p.FollowSessions }),
		boolField("Pass Codex its effort", tellsNothing, KeyCodexEffort, func(p Portable) *bool { return p.CodexEffort }),
		boolField("Codex Fast mode", tellsNothing, KeyCodexFast, func(p Portable) *bool { return p.CodexFast }),
		boolField("Claude Code's API key", tellsNothing, KeyClaudeAPIKey, func(p Portable) *bool { return p.ClaudeAPIKey }),
		boolField("Codex's API key", tellsNothing, KeyCodexAPIKey, func(p Portable) *bool { return p.CodexAPIKey }),
		boolField("Clean up finished worktrees", tellsNothing, KeyWorktreeAutoCleanup,
			func(p Portable) *bool { return p.WorktreeAutoCleanup }),
		pointerField("Branch rules", tellsNothing, func(p Portable) *branchrules.Rules { return p.Branches },
			func(_ context.Context, r branchrules.Rules) (map[string]string, error) { return r.Encode() }, nil),
		pointerField("Trusted bots", tellsNothing, func(p Portable) *[]string { return p.TrustedBots },
			func(_ context.Context, bots []string) (map[string]string, error) { return checkTrustedBots(bots) }, nil),
		pointerField("Snooze length", tellsNothing, func(p Portable) *int { return p.SnoozeDays },
			func(_ context.Context, days int) (map[string]string, error) { return checkSnoozeDays(days) },
			func(days int) string { return strconv.Itoa(days) + " days" }),
		pointerField("Snooze wake rules", tellsNothing, func(p Portable) *snooze.Rules { return p.SnoozeWake },
			func(_ context.Context, r snooze.Rules) (map[string]string, error) { return checkSnoozeWake(r) }, nil),
		pointerField("Whose files are checked", tellsNothing, func(p Portable) *sensitive.Scope { return p.SensitiveScope },
			func(_ context.Context, scope sensitive.Scope) (map[string]string, error) {
				return checkSensitiveScope(scope)
			}, func(scope sensitive.Scope) string { return string(scope) }),
		pointerField("Sensitive file categories", tellsNothing, func(p Portable) *[]string { return p.SensitiveOff },
			func(_ context.Context, off []string) (map[string]string, error) { return checkSensitiveOff(off) }, nil),
		pointerField("Your sensitive file patterns", tellsNothing, func(p Portable) *[]string { return p.SensitivePatterns },
			func(_ context.Context, patterns []string) (map[string]string, error) {
				return checkSensitivePatterns(patterns)
			}, nil),
		boolField("Menu bar icon", tellsMenuBar, KeyMenuBar, func(p Portable) *bool { return p.MenuBar }),
		pointerField("Notification banners", tellsNothing, func(p Portable) *[]notifyprefs.Kind { return p.BannersOff },
			func(_ context.Context, off []notifyprefs.Kind) (map[string]string, error) {
				return checkBannersOff(off)
			}, nil),
	)

	fields = append(fields, threadFields()...)

	for _, kind := range RefreshKinds() {
		fields = append(fields, portableField{
			label: refreshLabel(kind), tells: tellsIntervals,
			has: func(p Portable) bool { _, ok := p.Refresh[kind]; return ok },
			check: func(_ context.Context, p Portable) (map[string]string, error) {
				return checkRefresh(kind, p.Refresh[kind])
			},
			show: func(p Portable) string { return seconds(p.Refresh[kind]) },
		})
	}

	fields = append(fields, boolField("Notice pushes from this Mac", tellsIntervals, KeyWatchPushes,
		func(p Portable) *bool { return p.WatchPushes }))

	fields = append(fields, statusFields()...)

	fields = append(fields,
		pointerField("Merge method", tellsNothing, func(p Portable) *MergeMethod { return p.MergeMethod },
			func(_ context.Context, m MergeMethod) (map[string]string, error) { return checkMergeMethod(m) },
			func(m MergeMethod) string { return string(m) }),
		boolField("Merge your own pull requests past the rules", tellsNothing, KeyBypassOwn,
			func(p Portable) *bool { return p.BypassOwn }),
		pointerField("Offer a nudge after", tellsNothing, func(p Portable) *int { return p.NudgeAfterHours },
			func(_ context.Context, n int) (map[string]string, error) { return checkNudgeAfterHours(n) }, nudgeHours),
		boolField("Snooze after a nudge", tellsNothing, KeyNudgeSnooze, func(p Portable) *bool { return p.NudgeSnooze }),
		pointerField("Theme", tellsTheme, func(p Portable) *Theme { return p.Theme },
			func(_ context.Context, theme Theme) (map[string]string, error) { return checkTheme(theme) },
			func(theme Theme) string { return string(theme) }),
		pointerField("Text size", tellsTextSize, func(p Portable) *TextSize { return p.TextSize },
			func(_ context.Context, size TextSize) (map[string]string, error) { return checkTextSize(size) },
			func(size TextSize) string { return string(size) }),
		pointerField("Celebrations", tellsNothing, func(p Portable) *Celebrations { return p.Celebrations },
			func(_ context.Context, c Celebrations) (map[string]string, error) { return checkCelebrations(c) },
			func(c Celebrations) string { return string(c) }),
		pointerField("How long messages stay", tellsNothing, func(p Portable) *int { return p.ToastSeconds },
			func(_ context.Context, n int) (map[string]string, error) { return checkToastSeconds(n) }, seconds),
		pointerField("Inbox sorting", tellsNothing, func(p Portable) *InboxOrder { return p.InboxOrder },
			func(_ context.Context, order InboxOrder) (map[string]string, error) {
				return checkInboxOrder(adoptFormerOrder(order))
			}, nil),
		pointerField("Hidden labels", tellsNothing, func(p Portable) *[]string { return p.HiddenLabels },
			func(_ context.Context, patterns []string) (map[string]string, error) {
				return checkHiddenLabels(patterns)
			}, nil),
		boolField("Author pictures", tellsNothing, KeyCardAvatars, func(p Portable) *bool { return p.CardAvatars }),
		pointerField("Card density", tellsNothing, func(p Portable) *CardDensity { return p.CardDensity },
			func(_ context.Context, d CardDensity) (map[string]string, error) { return checkCardDensity(d) },
			func(d CardDensity) string { return string(d) }),
		boolField("Unread dots", tellsNothing, KeyCardUnread, func(p Portable) *bool { return p.CardUnread }),
		pointerField("Ignored workflows", tellsNothing, func(p Portable) *[]string { return p.IgnoredWorkflows },
			func(_ context.Context, names []string) (map[string]string, error) {
				return checkIgnoredWorkflows(names)
			}, nil),
		boolField("Show auto-merge bots' approvals", tellsNothing, KeyMergeBotsOn,
			func(p Portable) *bool { return p.MergeBotsOn }),
		pointerField("Auto-merge bots", tellsNothing, func(p Portable) *[]string { return p.MergeBots },
			func(_ context.Context, bots []string) (map[string]string, error) { return checkMergeBots(bots) }, nil),
		pointerField("When a new version is out", tellsUpdates, func(p Portable) *updateprefs.Mode { return p.UpdateMode },
			func(_ context.Context, m updateprefs.Mode) (map[string]string, error) { return checkUpdateMode(m) },
			func(m updateprefs.Mode) string { return string(m) }),
	)

	return append(append(fields, s.integrationFields()...), s.autopilotFields()...)
}

// threadFields are the GitHub notification rules a backup can hold.
func threadFields() []portableField {
	rules := func(p Portable) PortableThreadRules {
		if p.GitHubNotifications == nil {
			return PortableThreadRules{}
		}

		return *p.GitHubNotifications
	}

	switchField := func(label string, name threadrules.Switch, get func(PortableThreadRules) *bool) portableField {
		return boolField(label, tellsThreadRules, name.Key(), func(p Portable) *bool { return get(rules(p)) })
	}

	return []portableField{
		switchField("Read GitHub notifications", threadrules.SwitchRead, func(r PortableThreadRules) *bool { return r.Read }),
		switchField("Clear GitHub notifications on their own", threadrules.SwitchAuto,
			func(r PortableThreadRules) *bool { return r.Auto }),
		pointerField("What clearing a notification does", tellsThreadRules,
			func(p Portable) *threadrules.Action { return rules(p).Action },
			func(_ context.Context, a threadrules.Action) (map[string]string, error) { return checkThreadAction(a) },
			func(a threadrules.Action) string { return string(a) }),
		pointerField("What clears a notification", tellsThreadRules,
			func(p Portable) *[]threadrules.Trigger { return rules(p).TriggersOff },
			func(_ context.Context, off []threadrules.Trigger) (map[string]string, error) {
				return checkTriggersOff(off)
			}, nil),
		switchField("Clear notifications of watched repositories", threadrules.SwitchWatching,
			func(r PortableThreadRules) *bool { return r.Watching }),
		switchField("Count a bot's activity as seen", threadrules.SwitchBotActivity,
			func(r PortableThreadRules) *bool { return r.BotActivity }),
	}
}

// statusFields are what the service status check watches, as a backup can hold it: the
// status pages turned off, and the parts watched. An id this build doesn't know is left
// out with a note, and the rest imported.
func statusFields() []portableField {
	status := func(p Portable) PortableStatus {
		if p.ServiceStatus == nil {
			return PortableStatus{}
		}

		return *p.ServiceStatus
	}

	off := pointerField("Status pages checked", tellsStatusRules,
		func(p Portable) *[]statusprefs.ProviderID { return status(p).Off },
		func(_ context.Context, off []statusprefs.ProviderID) (map[string]string, error) {
			writes, _ := checkStatusOff(off)

			return writes, nil
		}, nil)
	off.notes = func(p Portable) []string {
		_, notes := checkStatusOff(*status(p).Off)

		return notes
	}

	parts := pointerField("Parts of status pages watched", tellsStatusRules,
		func(p Portable) *[]statusprefs.PartID { return status(p).Parts },
		func(_ context.Context, parts []statusprefs.PartID) (map[string]string, error) {
			writes, _ := checkStatusParts(parts)

			return writes, nil
		}, nil)
	parts.notes = func(p Portable) []string {
		_, notes := checkStatusParts(*status(p).Parts)

		return notes
	}

	return []portableField{off, parts}
}

// integrationFields are the integrations' settings a backup can hold.
func (s *Service) integrationFields() []portableField {
	ig := func(p Portable) PortableIntegration {
		if p.Integrations == nil {
			return PortableIntegration{}
		}

		return *p.Integrations
	}

	var fields []portableField

	for _, id := range integrationprefs.IDs() {
		fields = append(fields, portableField{
			label: string(id) + " integration", tells: tellsIntegrations,
			has: func(p Portable) bool { _, ok := ig(p).Switches[id]; return ok },
			check: func(_ context.Context, p Portable) (map[string]string, error) {
				return checkIntegrationSwitch(id, ig(p).Switches[id])
			},
			show: func(p Portable) string { return string(ig(p).Switches[id]) },
		})
	}

	for _, id := range integrationprefs.Tools() {
		fields = append(fields, portableField{
			label: "Copy of " + string(id), tells: tellsToolCopy,
			has: func(p Portable) bool { _, ok := ig(p).Paths[id]; return ok },
			check: func(_ context.Context, p Portable) (map[string]string, error) {
				return s.checkIntegrationPath(id, ig(p).Paths[id])
			},
			show: func(p Portable) string {
				if path := ig(p).Paths[id]; path != "" {
					return path
				}

				return "the one your shell finds"
			},
		})
	}

	return append(fields,
		pointerField("Where runs go", tellsNothing, func(p Portable) *integrationprefs.RunsIn { return ig(p).RunsIn },
			func(_ context.Context, place integrationprefs.RunsIn) (map[string]string, error) {
				return checkRunsIn(place)
			}, func(place integrationprefs.RunsIn) string { return string(place) }),
		boolField("Offer a release after a merge", tellsNothing, integrationprefs.KeyOfferAfterMerge,
			func(p Portable) *bool { return ig(p).OfferAfterMerge }),
		boolField("List magex targets", tellsNothing, integrationprefs.KeyMagexTargets,
			func(p Portable) *bool { return ig(p).MagexTargets }),
		boolField("List repositories' own scripts", tellsNothing, integrationprefs.KeyRepoFiles,
			func(p Portable) *bool { return ig(p).RepoFiles }),
		boolField("Show a run beside its scripts", tellsNothing, integrationprefs.KeyShowRuns,
			func(p Portable) *bool { return ig(p).ShowRuns }),
	)
}

// pointerField is a setting held in a pointer field, checked by check and worded by show
// (nil to say nothing of its value).
func pointerField[T any](label string, tells announcement, get func(Portable) *T,
	check func(context.Context, T) (map[string]string, error), show func(T) string,
) portableField {
	f := portableField{
		label: label, tells: tells,
		has:   func(p Portable) bool { return get(p) != nil },
		check: func(ctx context.Context, p Portable) (map[string]string, error) { return check(ctx, *get(p)) },
	}

	if show != nil {
		f.show = func(p Portable) string {
			if v := get(p); v != nil {
				return show(*v)
			}

			return ""
		}
	}

	return f
}

// boolField is a switch stored under key.
func boolField(label string, tells announcement, key string, get func(Portable) *bool) portableField {
	return pointerField(label, tells, get, func(_ context.Context, on bool) (map[string]string, error) {
		return map[string]string{key: prefval.EncodeBool(on)}, nil
	}, func(on bool) string {
		if on {
			return "on"
		}

		return "off"
	})
}

// agentName is the agent's name, or its ID when this build doesn't know it.
func agentName(id agent.ID) string {
	if a, err := agent.Lookup(id); err == nil {
		return a.Name
	}

	return string(id)
}

// refreshLabel names how often the app reads kind again.
func refreshLabel(kind RefreshKind) string {
	switch kind {
	case RefreshInbox:
		return "Refresh the inbox"
	case RefreshChecks:
		return "Follow running checks"
	case RefreshReleaseChecks:
		return "Follow running checks on the Release page"
	case RefreshWorktrees:
		return "Sweep the worktrees"
	case RefreshNotifications:
		return "Read GitHub notifications every"
	case RefreshServiceStatus:
		return "Check service status"
	default:
		return "Refresh " + string(kind)
	}
}

// seconds words a number of seconds, "off" for 0.
func seconds(n int) string {
	switch {
	case n == 0:
		return "off"
	case n%60 == 0:
		return strconv.Itoa(n/60) + " min"
	default:
		return strconv.Itoa(n) + " s"
	}
}

// nudgeHours words the hours before a nudge, "off" for 0: in days when they are whole
// days, as "1 day" or "7 days", and in hours otherwise.
func nudgeHours(n int) string {
	switch {
	case n == 0:
		return "off"
	case n == 24:
		return "1 day"
	case n%24 == 0:
		return strconv.Itoa(n/24) + " days"
	case n == 1:
		return "1 hour"
	default:
		return strconv.Itoa(n) + " hours"
	}
}

// PortableItems counts the settings p holds.
func (bp *BackupPart) PortableItems(p Portable) int {
	s := bp.s

	n := 0

	for _, f := range s.portableFields() {
		if f.has(p) {
			n++
		}
	}

	return n
}
