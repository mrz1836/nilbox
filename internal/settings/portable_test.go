package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// Values the portable tests use more than once.
const (
	workProfile = "work"
	desktopExp  = "~/Desktop/exp"
)

// applyPlan applies plan to the settings in db, in a transaction of its own as an import
// does.
func applyPlan(t *testing.T, part *BackupPart, db *sql.DB, plan PortablePlan) {
	t.Helper()

	require.NoError(t, store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		_, err := part.ApplyPortable(t.Context(), tx, plan)

		return err
	}))
}

// changed lists the labels of the plan's items that change something.
func changed(p backup.Plan) []string {
	out := []string{}

	for _, item := range p.Items {
		if item.Change != backup.ChangeSame {
			out = append(out, item.Label)
		}
	}

	return out
}

// customize changes most of svc's settings away from their defaults, and returns a
// program a chosen copy of magex can name.
func customize(t *testing.T, svc *Service, db *sql.DB, home string) string {
	t.Helper()

	ctx := t.Context()
	magex := filepath.Join(home, "bin", "magex")
	testutil.WriteExecutable(t, magex, "")
	storeProfiles(t, db, profiles.Profile{Agent: agent.Claude, Name: workProfile, ConfigDir: "/opt/profiles/work"})

	for _, set := range []func() (Settings, error){
		func() (Settings, error) { return svc.SetDefaultAgent(ctx, agent.Codex) },
		func() (Settings, error) { return svc.SetDefaultProfile(ctx, agent.Claude, workProfile) },
		func() (Settings, error) { return svc.SetTerminal(ctx, TerminalApp) },
		func() (Settings, error) { return svc.SetWorktreeDefault(ctx, false) },
		func() (Settings, error) { return svc.SetFollowSessions(ctx, false) },
		func() (Settings, error) { return svc.SetCodexFast(ctx, true) },
		func() (Settings, error) {
			return svc.SetBranchRules(ctx, branchrules.Rules{Own: true, Protected: []string{"main", "release/*"}})
		},
		func() (Settings, error) { return svc.SetTrustedBots(ctx, []string{"renovate"}) },
		func() (Settings, error) { return svc.SetSnoozeDays(ctx, 3) },
		func() (Settings, error) {
			return svc.SetSnoozeWake(ctx, snooze.Rules{Commits: 2, Reviews: snooze.ReviewsAny, Comments: 4})
		},
		func() (Settings, error) { return svc.SetSensitiveScope(ctx, sensitive.ScopeOutside) },
		func() (Settings, error) { return svc.SetSensitivePatterns(ctx, []string{"deploy/**"}) },
		func() (Settings, error) { return svc.SetMenuBar(ctx, false) },
		func() (Settings, error) { return svc.SetNotificationBanner(ctx, notifyprefs.KindAssigned, false) },
		func() (Settings, error) { return svc.SetNotificationBanner(ctx, notifyprefs.KindSessionFinished, true) },
		func() (Settings, error) {
			return svc.SetGitHubNotificationSwitch(ctx, threadrules.SwitchAuto, true)
		},
		func() (Settings, error) { return svc.SetGitHubNotificationAction(ctx, threadrules.ActionRead) },
		func() (Settings, error) { return svc.SetRefreshInterval(ctx, RefreshInbox, 600) },
		func() (Settings, error) { return svc.SetMergeMethod(ctx, MergeMethodSquash) },
		func() (Settings, error) { return svc.SetTheme(ctx, ThemeDark) },
		func() (Settings, error) { return svc.SetTextSize(ctx, TextSizeLarge) },
		func() (Settings, error) { return svc.SetToastSeconds(ctx, 20) },
		func() (Settings, error) { return svc.SetStatusSort(ctx, ListAssigned, true) },
		func() (Settings, error) { return svc.SetListKind(ctx, ListAuthored, ListKindIssues) },
		func() (Settings, error) { return svc.SetStatusOrder(ctx, []string{"In progress", "Blocked"}) },
		func() (Settings, error) {
			return svc.SetIntegrationEnabled(ctx, integrationprefs.Scripts, integrationprefs.SwitchOff)
		},
		func() (Settings, error) { return svc.SetIntegrationPath(ctx, integrationprefs.MageX, magex) },
		func() (Settings, error) { return svc.SetRunsIn(ctx, integrationprefs.RunsInTerminal) },
		func() (Settings, error) { return svc.SetScriptSources(ctx, false, true) },
		func() (Settings, error) { return svc.SetShowRuns(ctx, true) },
		// The window's memory, which a backup never holds.
		func() (Settings, error) { return svc.SetWindowSize(ctx, WindowSize{Width: 1000, Height: 700}) },
		func() (Settings, error) { return svc.SetSectionCollapsed(ctx, ListAssigned, true) },
		func() (Settings, error) { return svc.SetProjectsTipDismissed(ctx, true) },
		func() (Settings, error) { return svc.SetReviewTab(ctx, ReviewTabComments) },
	} {
		_, err := set()
		require.NoError(t, err)
	}

	return magex
}

func TestPortableSettingsRoundTrip(t *testing.T) {
	t.Parallel()

	src, srcDB, home := newTestService(t)
	magex := customize(t, src, srcDB, home)

	p, err := NewBackupPart(src).Portable(t.Context())
	require.NoError(t, err)

	data, err := json.Marshal(p)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "windowSize", "the window's memory stays out")
	assert.NotContains(t, string(data), "collapsed")
	assert.NotContains(t, string(data), "cleanupCardDismissed")
	assert.NotContains(t, string(data), "reviewTab")

	var read Portable
	require.NoError(t, json.Unmarshal(data, &read))

	// Another Mac, with the same profile and the same program at the same place.
	dstDB := testutil.DB(t)
	dst := serviceOver(t, dstDB, home)
	storeProfiles(t, dstDB, profiles.Profile{Agent: agent.Claude, Name: workProfile, ConfigDir: "/opt/profiles/work"})

	part := NewBackupPart(dst)

	plan, err := part.PlanPortable(t.Context(), read)
	require.NoError(t, err)
	assert.Empty(t, plan.Notes)
	assert.Len(t, changed(plan.Plan), plan.Count.Add+plan.Count.Update)
	assert.Equal(t, NewBackupPart(src).PortableItems(p), plan.Count.Update+plan.Count.Same, "every setting is listed")

	applyPlan(t, part, dstDB, plan)

	want, err := src.Get(t.Context())
	require.NoError(t, err)

	got, err := dst.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, portableOf(want), portableOf(got), "every setting a backup holds comes back")
	assert.Equal(t, magex, got.Integrations.Paths[integrationprefs.MageX])
	assert.Equal(t, DefaultWindowSize(), got.WindowSize, "the window's memory is not imported")
	assert.Empty(t, got.CollapsedSections)
	assert.False(t, got.ProjectsTipDismissed)
	assert.Equal(t, DefaultReviewTab, got.ReviewTab)

	again, err := part.PlanPortable(t.Context(), read)
	require.NoError(t, err)
	assert.Empty(t, changed(again.Plan), "importing the same settings again changes nothing")
}

func TestPlanPortableLeavesOutWhatItRefusesWithANote(t *testing.T) {
	t.Parallel()

	svc, db, home := newTestService(t)
	notProgram := filepath.Join(home, "notes.txt")
	testutil.WriteFile(t, notProgram, "x")

	theme, profile, refresh := Theme("dim"), "missing", 7
	text := TextSizeLarger
	p := Portable{
		Theme: &theme, TextSize: &text, DefaultProfiles: map[agent.ID]string{agent.Claude: profile, agent.Codex: "default"},
		Refresh:      map[RefreshKind]int{RefreshChecks: refresh},
		Integrations: &PortableIntegration{Paths: map[integrationprefs.ID]string{integrationprefs.GoPreCommit: notProgram}},
	}

	part := NewBackupPart(svc)

	plan, err := part.PlanPortable(t.Context(), p)
	require.NoError(t, err)
	assert.Equal(t, []string{
		`Claude profile was left as it is. Unknown profile "missing".`,
		"Follow running checks was left as it is. Unknown refresh interval: 7 seconds for checks.",
		`Theme was left as it is. Unknown theme "dim": choose one of system, light, dark.`,
		"Copy of go-pre-commit was left as it is. ~/notes.txt can't be run: it isn't executable.",
	}, plan.Notes)
	assert.Equal(t, []string{"Text size"}, changed(plan.Plan), "only what it took is changed")

	var codex backup.Item

	for _, item := range plan.Items {
		if item.Label == "Codex profile" {
			codex = item
		}
	}

	assert.Equal(t, backup.ChangeSame, codex.Change, "the default profile needs no profile found")

	applyPlan(t, part, db, plan)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TextSizeLarger, got.TextSize)
	assert.Equal(t, DefaultTheme, got.Theme)
	assert.Equal(t, DefaultRefreshIntervals(), got.Refresh)
	assert.Equal(t, "default", got.DefaultProfiles[agent.Claude])
}

func TestPlanPortableLeavesWhatTheFileLacks(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	_, err := svc.SetMergeMethod(t.Context(), MergeMethodRebase)
	require.NoError(t, err)

	dark := ThemeDark
	part := NewBackupPart(svc)

	plan, err := part.PlanPortable(t.Context(), Portable{Theme: &dark})
	require.NoError(t, err)
	require.Equal(t, []backup.Item{{Label: "Theme", Change: backup.ChangeUpdate, Detail: "dark (now system)"}}, plan.Items)

	applyPlan(t, part, db, plan)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ThemeDark, got.Theme)
	assert.Equal(t, MergeMethodRebase, got.MergeMethod, "a setting the file lacks is left as it is")
}

func TestAnnounceTellsEachChangeOnce(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	db := testutil.DB(t)
	home := t.TempDir()
	svc := serviceOver(t, db, home, WithEvents(events))
	magex := filepath.Join(home, "magex")
	testutil.WriteExecutable(t, magex, "")

	dark, size, off, on := ThemeDark, TextSizeLarge, false, true
	p := Portable{
		Theme: &dark, TextSize: &size, MenuBar: &off, FollowSessions: &off, WorktreeDefault: &off,
		GitHubNotifications: &PortableThreadRules{Auto: &on, Watching: &off},
		Refresh:             map[RefreshKind]int{RefreshInbox: 600, RefreshChecks: 30},
		Integrations: &PortableIntegration{
			Switches: map[integrationprefs.ID]integrationprefs.Switch{integrationprefs.MageX: integrationprefs.SwitchOn},
			Paths:    map[integrationprefs.ID]string{integrationprefs.MageX: magex},
		},
	}

	part := NewBackupPart(svc)

	plan, err := part.PlanPortable(t.Context(), p)
	require.NoError(t, err)

	applyPlan(t, part, db, plan)
	assert.Zero(t, events.Count(), "nothing is told before the import commits")

	part.Announce(t.Context(), plan)
	assert.ElementsMatch(t, []event.Event{
		event.ThemeChanged{Theme: string(ThemeDark)},
		event.TextSizeChanged{Size: string(TextSizeLarge)},
		event.MenuBarChanged{On: false},
		event.SessionsUpdated{},
		event.IntegrationsChanged{CopyChosen: true},
		event.ThreadRulesChanged{},
		event.IntervalsChanged{},
	}, events.All(), "each setter's event, once")

	quiet := &testutil.Events{}
	again := NewBackupPart(serviceOver(t, db, home, WithEvents(quiet)))

	plan, err = again.PlanPortable(t.Context(), p)
	require.NoError(t, err)
	again.Announce(t.Context(), plan)
	assert.Zero(t, quiet.Count(), "an import that changes nothing tells nothing")
}

func TestPlanReposMergesAndReplaces(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)
	ctx := t.Context()

	r.add(t, "~/work")
	_, err := r.svc.SetRepoRule(ctx, "acme-org", reporules.RulePinned)
	require.NoError(t, err)
	_, err = r.svc.SetRepoRule(ctx, "acme-org/old", reporules.RuleMuted)
	require.NoError(t, err)

	root, missing := "~/src", "~/gone"
	file := PortableRepos{
		CloneRoot: &root,
		Locations: &[]localrepos.Location{{Path: desktopExp}, {Path: missing}},
		Pinned:    &[]string{"acme-org/web"},
		Muted:     &[]string{"acme-org", "not a target"},
	}

	part := NewBackupPart(r.svc)

	merge, err := part.PlanRepos(ctx, file, backup.ModeMerge)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"The location ~/gone was left out. ~/gone doesn't exist.",
		`not a target was left out. Not a GitHub org or repository: "not a target".`,
	}, merge.Notes)
	assert.Zero(t, merge.Count.Remove, "Merge never deletes")
	assert.Equal(t, []string{"Clone folder", desktopExp, "acme-org/web", "acme-org"}, changed(merge.Plan))

	replace, err := part.PlanRepos(ctx, file, backup.ModeReplace)
	require.NoError(t, err)
	assert.Equal(t, []string{"Clone folder", desktopExp, "~/work", "acme-org/web", "acme-org", "acme-org/old"},
		changed(replace.Plan))
	assert.Equal(t, 2, replace.Count.Remove, "~/work and acme-org/old; acme-org goes from pinned to muted")

	applyPlan(t, part, r.db, merge)

	got, err := r.svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, "~/src", got.CloneRoot)
	assert.Equal(t, []localrepos.Location{
		{Path: "~/work", Kind: localrepos.KindFolder}, {Path: desktopExp, Kind: localrepos.KindClone},
	}, got.CloneLocations)
	assert.Equal(t, reporules.Rules{Pinned: []string{"acme-org/web"}, Muted: []string{"acme-org", "acme-org/old"}},
		got.RepoRules, "the file's rule wins for a target in both")

	replace, err = part.PlanRepos(ctx, file, backup.ModeReplace)
	require.NoError(t, err)
	applyPlan(t, part, r.db, replace)

	got, err = r.svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []localrepos.Location{{Path: desktopExp, Kind: localrepos.KindClone}}, got.CloneLocations)
	assert.Equal(t, reporules.Rules{Pinned: []string{"acme-org/web"}, Muted: []string{"acme-org"}}, got.RepoRules,
		"Replace takes the file's lists")
}

func TestPlanReposKeepsAMissingCloneFolder(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	missing := "~/nowhere"

	plan, err := NewBackupPart(svc).PlanRepos(t.Context(), PortableRepos{CloneRoot: &missing}, backup.ModeMerge)
	require.NoError(t, err)
	assert.Equal(t, []string{"The clone folder ~/nowhere isn't a folder on this Mac, so it stays ~/projects."}, plan.Notes)
	assert.Empty(t, plan.Items)
	assert.Empty(t, plan.writes)
}

func TestPortableReposHoldsEveryRepositorySetting(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)
	r.add(t, "~/work")
	_, err := r.svc.SetRepoRule(t.Context(), "acme-org", reporules.RuleMuted)
	require.NoError(t, err)

	got, err := NewBackupPart(r.svc).PortableRepos(context.WithoutCancel(t.Context()))
	require.NoError(t, err)
	assert.Equal(t, DefaultCloneRoot, *got.CloneRoot)
	assert.Equal(t, []localrepos.Location{{Path: "~/work", Kind: localrepos.KindFolder}}, *got.Locations)
	assert.Equal(t, []string{}, *got.Pinned)
	assert.Equal(t, []string{"acme-org"}, *got.Muted)
}
