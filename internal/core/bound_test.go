package core

import (
	"context"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/hooks"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// boundOwner owns the repositories of the seeded items.
const boundOwner = "acme-org"

// boundRepo is the repository the core tests' clones and items belong to.
const boundRepo = "example-repo"

// seededItems are the stored items the bound calls use: a pull request whose launch
// settings resolve, and an issue whose settings cannot, because its repository's launch
// defaults were edited by hand into flags the agent refuses.
type seededItems struct {
	item, stuck int64
}

// boundCall calls one bound method as the window does, on a core holding the seeded
// items, a notification, a worktree cleanup, and a clone with a linked worktree.
type boundCall func(ctx context.Context, c *Core, ids seededItems) (any, error)

// boundCalls are the bound methods whose results hold a list or a map, each by its
// service's type and its name. A list the window receives is [] and a map {}, never null,
// so the frontend reads them without a fallback.
func boundCalls(t *testing.T) map[string]boundCall {
	t.Helper()

	draft := prompts.Input{Name: "Triage", Nickname: "Triage", AppliesTo: prompts.AppliesToAny, Body: "Look at {{.URL}}"}
	defaults := launchdefaults.Input{Owner: boundOwner}

	return map[string]boundCall{
		"appinfo.Service.AgentVersions": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.AppInfo.AgentVersions(ctx), nil
		},
		"appinfo.Service.ToolVersions": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.AppInfo.ToolVersions(ctx), nil
		},
		"settings.Service.Get": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.Get(ctx)
		},
		"settings.Service.CollapsedSections": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.CollapsedSections(ctx)
		},
		"settings.Service.InboxOrder": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.InboxOrder(ctx)
		},
		"settings.Service.SetStatusSort": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetStatusSort(ctx, settings.ListAssigned, true)
		},
		"settings.Service.SetStatusOrder": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetStatusOrder(ctx, nil)
		},
		"settings.Service.SetTerminal": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetTerminal(ctx, settings.TerminalApp)
		},
		"settings.Service.SetDefaultProfile": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetDefaultProfile(ctx, agent.Claude, settings.DefaultProfile)
		},
		"settings.Service.SetBranchRules": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetBranchRules(ctx, branchrules.Defaults())
		},
		"settings.Service.SetDefaultAgent": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetDefaultAgent(ctx, agent.Codex)
		},
		"settings.Service.SetFollowSessions": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetFollowSessions(ctx, false)
		},
		"settings.Service.SetCodexEffort": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetCodexEffort(ctx, true)
		},
		"settings.Service.SetCodexFast": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetCodexFast(ctx, true)
		},
		"sessions.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Sessions.List(ctx)
		},
		"profiles.Service.Agents": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Profiles.Agents(ctx)
		},
		"settings.Service.SetWorktreeDefault": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetWorktreeDefault(ctx, false)
		},
		"settings.Service.SetWorktreeAutoCleanup": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetWorktreeAutoCleanup(ctx, false)
		},
		"settings.Service.SetCloneRoot": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetCloneRoot(ctx, "~/projects")
		},
		"settings.Service.AddCloneLocations": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.AddCloneLocations(ctx, []string{"~/work"})
		},
		"settings.Service.RemoveCloneLocation": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.RemoveCloneLocation(ctx, "~/elsewhere")
		},
		"settings.Service.PreviewCloneLocation": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.PreviewCloneLocation(ctx, "~/src")
		},
		"settings.Service.CheckCloneLocations": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.CheckCloneLocations(ctx)
		},
		"settings.Service.SetTrustedBots": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetTrustedBots(ctx, nil)
		},
		"settings.Service.ResetTrustedBots": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.ResetTrustedBots(ctx)
		},
		"settings.Service.SetSnoozeDays": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetSnoozeDays(ctx, 5)
		},
		"settings.Service.SetSnoozeWake": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetSnoozeWake(ctx, snooze.DefaultRules())
		},
		"settings.Service.SetSensitiveScope": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetSensitiveScope(ctx, sensitive.ScopeOutside)
		},
		"settings.Service.SetSensitiveCategory": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetSensitiveCategory(ctx, sensitive.CategoryReview, true)
		},
		"settings.Service.SetSensitivePatterns": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetSensitivePatterns(ctx, nil)
		},
		"settings.Service.SetMenuBar": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetMenuBar(ctx, true)
		},
		"settings.Service.SetNotificationBanner": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetNotificationBanner(ctx, notifyprefs.KindWoke, true)
		},
		"settings.Service.SetProjectsTipDismissed": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetProjectsTipDismissed(ctx, true)
		},
		"settings.Service.SetSectionCollapsed": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetSectionCollapsed(ctx, settings.ListAuthored, false)
		},
		"settings.Service.SetTheme": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetTheme(ctx, settings.ThemeDark)
		},
		"settings.Service.SetTextSize": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetTextSize(ctx, settings.TextSizeLarge)
		},
		"settings.Service.SetWindowSize": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetWindowSize(ctx, settings.DefaultWindowSize())
		},
		"profiles.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Profiles.List(ctx)
		},
		"prompts.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Prompts.List(ctx)
		},
		"prompts.Service.Create": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Prompts.Create(ctx, draft)
		},
		"prompts.Service.Update": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Prompts.Update(ctx, firstPrompt(ctx, t, c).ID, firstInput(ctx, t, c))
		},
		"prompts.Service.Duplicate": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Prompts.Duplicate(ctx, firstPrompt(ctx, t, c).ID)
		},
		"prompts.Service.Reset": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Prompts.Reset(ctx, firstPrompt(ctx, t, c).ID)
		},
		"prompts.Service.Check": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			return c.Prompts.Check(ctx, prompts.CheckRequest{Input: draft, ItemID: ids.item})
		},
		"prompts.Service.Variables": func(_ context.Context, c *Core, _ seededItems) (any, error) {
			return c.Prompts.Variables(), nil
		},
		"prompts.Service.Backups": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Prompts.Backups(ctx)
		},
		"prompts.Service.PreviewImport": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			path := filepath.Join(t.TempDir(), "prompts.json")
			if _, err := c.Prompts.Export(ctx, path, nil); err != nil {
				return nil, err
			}

			return c.Prompts.PreviewImport(ctx, prompts.ImportSource{Kind: prompts.SourceFile, Path: path})
		},
		"launchdefaults.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.LaunchDefaults.List(ctx)
		},
		"launchdefaults.Service.Check": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.LaunchDefaults.Check(ctx, defaults)
		},
		"launchdefaults.Service.Save": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.LaunchDefaults.Save(ctx, defaults)
		},
		"launchdefaults.Service.Targets": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.LaunchDefaults.Targets(ctx)
		},
		"inbox.Service.Snapshot": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Inbox.Snapshot(ctx)
		},
		"inbox.Service.Pin": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			return c.Inbox.Pin(ctx, ids.item)
		},
		"inbox.Service.Unpin": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			return c.Inbox.Unpin(ctx, ids.item)
		},
		"inbox.Service.Snooze": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			return c.Inbox.Snooze(ctx, ids.item, 1)
		},
		"inbox.Service.Dismiss": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			return c.Inbox.Dismiss(ctx, ids.item)
		},
		"inbox.Service.Restore": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			return c.Inbox.Restore(ctx, ids.item)
		},
		"launcher.Service.Resolve": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			return c.Launcher.Resolve(ctx, launcher.LaunchRequest{ItemID: ids.item})
		},
		"launcher.Service.ResolveItems": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			got, err := c.Launcher.ResolveItems(ctx, []int64{ids.item, ids.stuck, ids.stuck + 100})
			require.NoError(t, err)
			require.Len(t, got, 2, "an item no longer stored is left out")
			require.NotEmpty(t, got[0].Problem+got[1].Problem, "one item cannot resolve")

			return got, nil
		},
		"worktrees.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Worktrees.List(ctx)
			require.NoError(t, err)
			require.Len(t, got.Repos, 1)
			require.Len(t, got.Repos[0].Worktrees, 2, "the main worktree and the linked one")

			return got, nil
		},
		"worktrees.Service.RecentCleanups": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Worktrees.RecentCleanups(ctx)
			require.NotEmpty(t, got)

			return got, err
		},
		"worktrees.Service.RemoveAllClean": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Worktrees.RemoveAllClean(ctx, nil)
		},
		"worktrees.Service.CleanUpFinished": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Worktrees.CleanUpFinished(ctx, nil)
		},
		"worktrees.Service.DeleteBranches": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Worktrees.DeleteBranches(ctx, nil)
		},
		"worktrees.Service.Sweep": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Worktrees.Sweep(ctx), nil
		},
		"worktrees.Service.LastSweep": func(_ context.Context, c *Core, _ seededItems) (any, error) {
			return c.Worktrees.LastSweep(), nil
		},
		"settings.Service.SetMergeMethod": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetMergeMethod(ctx, settings.MergeMethodSquash)
		},
		"settings.Service.SetIntegrationEnabled": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetIntegrationEnabled(ctx, integrationprefs.MageX, integrationprefs.SwitchOff)
		},
		"settings.Service.SetIntegrationPath": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetIntegrationPath(ctx, integrationprefs.MageX, "")
		},
		"settings.Service.SetRunsIn": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetRunsIn(ctx, integrationprefs.RunsInTerminal)
		},
		"settings.Service.SetOfferAfterMerge": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetOfferAfterMerge(ctx, false)
		},
		"scriptrun.Service.Runs": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.ScriptRuns.Runs(ctx, scriptrun.Filter{})
			require.Len(t, got, 4)

			return got, err
		},
		"settings.Service.SetScriptSources": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetScriptSources(ctx, true, true)
		},
		"settings.Service.SetIntegrationsAnnounced": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetIntegrationsAnnounced(ctx, true)
		},
		"settings.Service.SetScriptsLastPage": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetScriptsLastPage(ctx, integrationprefs.PageHooks)
		},
		"settings.Service.SetScriptsLastRepo": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetScriptsLastRepo(ctx, "~/projects/example-repo")
		},
		"settings.Service.SetScriptsSectionOpen": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetScriptsSectionOpen(ctx, integrationprefs.SectionHooksReady, false)
		},
		"settings.Service.SetShowRuns": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetShowRuns(ctx, true)
		},
		"settings.Service.SetRefreshInterval": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Settings.SetRefreshInterval(ctx, settings.RefreshChecks, 30)
		},
		"scripts.Service.Repos": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Scripts.Repos(ctx)
			require.Len(t, got, 1)

			return got, err
		},
		"scripts.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			off, err := c.Scripts.List(ctx, "~/projects/example-repo", false)
			require.NoError(t, err)
			require.True(t, off.Off, "automatic, with neither tool found")
			require.NotNil(t, off.Sections)

			_, err = c.Settings.SetIntegrationEnabled(ctx, integrationprefs.Scripts, integrationprefs.SwitchOn)
			require.NoError(t, err)

			_, err = c.Scripts.Save(ctx, scripts.UserScript{
				Name: "Fix all", Command: "magex format:fix", Icon: scripts.IconPlay, Scope: scripts.ScopeAll,
			})
			require.NoError(t, err)

			got, err := c.Scripts.List(ctx, "~/projects/example-repo", true)
			require.Len(t, got.Sections, 4)
			require.NotEmpty(t, got.Sections[0].Entries, "the user's own script, with its repositories")
			require.Len(t, got.Sections[1].Entries, 1, "the clone's t3.json")

			return got, err
		},
		"scripts.Service.Save": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.Scripts.Save(ctx, scripts.UserScript{
				Name: "Test", Command: "magex test", Icon: scripts.IconTest, Scope: scripts.ScopeGo,
			})
		},
		"scripts.Service.Duplicate": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			saved, err := c.Scripts.Save(ctx, scripts.UserScript{
				Name: "Deploy", Command: "magex deploy", Icon: scripts.IconBuild, Scope: scripts.ScopeRepos,
				Repos: []string{"acme-org/example-repo"},
			})
			require.NoError(t, err)

			return c.Scripts.Duplicate(ctx, saved.ID)
		},
		"scripts.Service.RepoNames": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Scripts.RepoNames(ctx)
			require.Empty(t, got, "the fake git reads no origin")

			return got, err
		},
		"hooks.Service.Repos": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Hooks.Repos(ctx, false)
			require.Len(t, got.Repos, 1, "the clone with a config, which the fake git can't read")

			return got, err
		},
		"hooks.Service.ActionResult": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Hooks.ActionResult(ctx, 3)
			require.Equal(t, hooks.OutcomeInstalled, got.Outcome)

			return got, err
		},
		"hooks.Service.ChecksResult": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Hooks.ChecksResult(ctx, 4)
			require.Equal(t, hooks.ScopeAll, got.Scope)

			return got, err
		},
		"release.Service.ReleaseResult": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Release.ReleaseResult(ctx, 2)
			require.Equal(t, release.ResultNotPushed, got.Kind)

			return got, err
		},
		"scriptrun.Service.Run": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			return c.ScriptRuns.Run(ctx, 1)
		},
		"integrations.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Integrations.List(ctx)
			require.Len(t, got.Tools, 2)

			return got, err
		},
		"review.Service.Open": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			got, err := c.Review.Open(ctx, ids.item)
			require.NotEmpty(t, got.Files)

			return got, err
		},
		"review.Service.Patches": func(ctx context.Context, c *Core, ids seededItems) (any, error) {
			got, err := c.Review.Patches(ctx, ids.item, "abc123", []string{"login.go"})
			require.NotEmpty(t, got)

			return got, err
		},
		"review.Service.Replies": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Review.Replies(ctx, review.ReplyApprove)
			require.NotEmpty(t, got.Replies)

			return got, err
		},
		"notify.Service.List": func(ctx context.Context, c *Core, _ seededItems) (any, error) {
			got, err := c.Notifications.List(ctx)
			require.Len(t, got.Items, 3, "an item's notification, one about no item, and one about a run")
			assert.Equal(t, int64(1), got.Items[0].RunID, "the run a click opens")

			return got, err
		},
	}
}

// unreachedBound are the bound methods with a list or a map in their result that
// boundCalls does not call, each with why: they need GitHub, a clone, or a terminal, which
// the tests do not have. Their results come from the same code as a method that is called.
func unreachedBound() map[string]string {
	return map[string]string{
		"integrations.Service.CheckAgain":   "reads the user's login shell and GitHub; its result is built as List's",
		"integrations.Service.Install":      "downloads from GitHub; its tools are built as List's",
		"integrations.Service.Update":       "runs a tool's own updater or downloads from GitHub; its tools are built as List's",
		"integrations.Service.Reinstall":    "downloads from GitHub; its tools are built as List's",
		"integrations.Service.Trash":        "moves a copy of a tool through Finder; its result is built as List's",
		"inbox.Service.Refresh":             "reads GitHub; its snapshot is built as Snapshot's",
		"inbox.Service.RefreshItem":         "reads GitHub; its snapshot is built as Snapshot's",
		"inbox.Service.Unassign":            "edits GitHub; its snapshot is built as Snapshot's",
		"inbox.Service.RemoveReviewRequest": "edits GitHub; its snapshot is built as Snapshot's",
		"inbox.Service.CheckFiles":          "reads GitHub; nil for an item whose files are not checked",
		"inbox.Service.StoredFiles":         "nil for an item whose files are not checked",
		"launcher.Service.Preview":          "needs a clone; its resolution is built as Resolve's",
		"launcher.Service.LinkClone":        "needs a clone's remotes; its location is built as settings' PreviewCloneLocation's",
		"launcher.Service.Launch":           "opens a terminal",
		"launcher.Service.Resume":           "opens a terminal",
		"worktrees.Service.Remove":          "needs a worktree to remove; its result starts its lists empty, as RemoveAllClean's",
		"worktrees.Service.ForceRemove":     "needs a dirty worktree; its result is built as Remove's",
		"worktrees.Service.CleanUp":         "reads GitHub; its result is built as CleanUpFinished's",
		"worktrees.Service.MergedBranches":  "reads GitHub; it starts its list empty, as DeleteBranches does",
		"review.Service.Refresh":            "reads GitHub; its review is built as Open's",
		"review.Service.ReadAgain":          "reads GitHub; its review is built as Open's",
		"review.Service.Act":                "edits GitHub; its steps start empty",
		"release.Service.Candidates":        "scans the clone root and reads GitHub; its lists start empty",
		"release.Service.Prepare":           "needs a clone of a GitHub repository; Decide starts every list empty",
		"hooks.Service.InstallAll":          "needs go-pre-commit; its lines start empty",
	}
}

// firstPrompt returns the first stored prompt.
func firstPrompt(ctx context.Context, t *testing.T, c *Core) prompts.Prompt {
	t.Helper()

	all, err := c.Prompts.List(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, all)

	return all[0]
}

// firstInput returns the first stored prompt as the editor would save it.
func firstInput(ctx context.Context, t *testing.T, c *Core) prompts.Input {
	t.Helper()

	p := firstPrompt(ctx, t, c)

	return prompts.Input{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: p.AppliesTo, Body: p.Body, Models: p.Models, Effort: p.Effort,
		PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraFlags: p.ExtraFlags,
	}
}

// exitError is a program that ran and failed, as a real one exits with a status.
type exitError struct{}

func (exitError) Error() string { return "exit status 1" }

// ExitCode reports the status a failed program exits with.
func (exitError) ExitCode() int { return 1 }

// worktreeGit answers git as a clone at repo with one linked worktree would: it lists the
// main worktree and the linked one, and fails every other command, as git does in a
// clone without commits. It runs no program.
func worktreeGit(repo string) *testutil.Runner {
	listed := "worktree " + repo + "\x00HEAD " + strings.Repeat("a", 40) + "\x00branch refs/heads/main\x00\x00" +
		"worktree " + filepath.Join(repo, ".claude", "worktrees", "scratch") + "\x00HEAD " + strings.Repeat("b", 40) +
		"\x00branch refs/heads/worktree-scratch\x00\x00"

	return &testutil.Runner{Script: func(_ context.Context, argv []string) ([]byte, []byte, error) {
		if slices.Contains(argv, "worktree") && slices.Contains(argv, "list") {
			return []byte(listed), nil, nil
		}

		return nil, []byte("not run"), exitError{}
	}}
}

// seedBound returns a core whose database holds the seeded items, a notification, and a
// worktree cleanup, and whose clone root, ~/projects, holds a clone with a linked
// worktree, so every list a bound call returns has an element to look inside.
func seedBound(t *testing.T) (*Core, seededItems) {
	t.Helper()

	ctx := t.Context()
	home := testutil.RealTempDir(t)
	repo := filepath.Join(home, "projects", "example-repo")
	testutil.MakeClaudeDirs(t, home, ".claude", "projects/example-repo/.git",
		"projects/example-repo/.claude/worktrees/scratch", "work/web/.git", "src/acme-org/tool/.git")
	// A go-pre-commit config, so the Hooks page lists the clone.
	testutil.WriteFile(t, filepath.Join(repo, ".github", ".env.base"), "ENABLE_GO_PRE_COMMIT=true\n")
	// A t3.json, so Repo scripts lists a script.
	testutil.WriteFile(t, filepath.Join(repo, "t3.json"), `{"scripts": [{"name": "Test", "command": "magex test"}]}`)

	opts := testOptions(t)
	opts.Home = home
	opts.Runner = worktreeGit(repo)
	c := bootstrap(t, opts)

	now := time.Now()
	stored := func(kind item.Kind, number int, repoName string) inbox.FixtureItem {
		return inbox.FixtureItem{
			Item: github.Item{
				IsPR: kind == item.KindPR, NodeID: "NODE_" + strconv.Itoa(number), Number: number,
				Title:     "Fix the login redirect",
				URL:       "https://github.com/acme-org/" + repoName + "/issues/" + strconv.Itoa(number),
				CreatedAt: now, UpdatedAt: now, HeadRef: "fix-login", AuthorLogin: "octo-other",
				AuthorAssociation: "CONTRIBUTOR", RepoOwner: boundOwner, RepoName: repoName,
			},
			ReviewRequested: true,
			Details:         &github.ItemDetails{HeadOID: "abc123"},
		}
	}

	ids, err := inbox.StoreFixture(ctx, c.DB, inbox.Fixture{
		Viewer: "octo-user",
		Items:  []inbox.FixtureItem{stored(item.KindPR, 7, "example-repo"), stored(item.KindIssue, 8, "stuck-repo")},
	})
	require.NoError(t, err)

	// Launch defaults edited by hand into a flag the agent refuses, so the issue in that
	// repository cannot resolve.
	_, err = c.DB.ExecContext(ctx, `INSERT INTO launch_defaults (owner, name, agent_args, created_at, updated_at)
		VALUES ('acme-org', 'stuck-repo', '{"claude": ["--model"]}', ?, ?)`, now, now)
	require.NoError(t, err)

	_, err = notify.NewStore(c.DB).Record(ctx, now, []inbox.Arrival{{
		Kind: inbox.ArrivalWoke, ItemID: ids[0], ItemKind: item.KindPR, RepoOwner: boundOwner,
		RepoName: "example-repo", Number: 7, Title: "Fix the login redirect", Author: "octo-other",
		Woke: snooze.Reasons{Review: snooze.ReviewApproved},
	}})
	require.NoError(t, err)

	// And one about no item: a release that reached GitHub.
	_, err = notify.NewStore(c.DB).RecordEvent(ctx, now, notify.Entry{
		Kind: notify.KindReleaseDone, Title: "v1.4.3 is on GitHub", RepoOwner: boundOwner, RepoName: "example-repo",
	})
	require.NoError(t, err)

	// And one about a run that failed, which names the run.
	_, err = notify.NewStore(c.DB).RecordEvent(ctx, now.Add(time.Second), notify.Entry{
		Kind: notify.KindRunFailed, Title: "A script failed: Lint", Body: "It exited with status 1.",
		RepoOwner: boundOwner, RepoName: "example-repo", RunID: 1, RunStarted: clock.Format(now),
	})
	require.NoError(t, err)

	require.NoError(t, worklog.New(c.DB).RecordCleanup(ctx, worklog.Cleanup{
		CleanedAt: now, Repo: "example-repo", Worktree: "pr-7", Head: "abc123", ItemKind: string(item.KindPR),
		ItemOwner: boundOwner, ItemName: "example-repo", ItemNumber: 7, ItemState: string(worktrees.ItemMerged),
	}))

	// The review sheet's cache of the pull request, as a refresh leaves it.
	_, err = c.DB.ExecContext(ctx, `INSERT INTO review_diffs (item_id, head_oid, total, complete, context, context_at,
		fetched_at, used_at) VALUES (?, 'abc123', 1, 1, '{"head":"abc123","state":"OPEN"}', ?, ?, ?)`, ids[0], now, now, now)
	require.NoError(t, err)

	_, err = c.DB.ExecContext(ctx, `INSERT INTO review_diff_files (item_id, position, path, status, additions, patch)
		VALUES (?, 0, 'login.go', 'modified', 1, '@@ -1 +1 @@
-a
+b')`, ids[0])
	require.NoError(t, err)

	// A run that ended, with its result as sparse as an older build's, and a release whose
	// outcome is as sparse.
	_, err = c.DB.ExecContext(ctx, `INSERT INTO script_runs (id, kind, title, dir, argv, status, exit_code, result,
		started_at, finished_at) VALUES (1, 'script', 'Lint', '/work/example-repo', '["/usr/local/bin/magex","lint"]',
		'failed', 1, '{}', ?, ?)`, now, now)
	require.NoError(t, err)

	_, err = c.DB.ExecContext(ctx, `INSERT INTO script_runs (id, kind, title, dir, argv, status, exit_code, result,
		started_at, finished_at) VALUES (2, 'release', 'Release v1.4.3', '/work/example-repo',
		'["/usr/local/bin/magex","version:bump"]', 'failed', 0, '{"outcome":{"kind":"notPushed","tag":"v1.4.3"}}', ?, ?)`,
		now, now)
	require.NoError(t, err)

	// A hooks action and a checks run whose results are as sparse.
	_, err = c.DB.ExecContext(ctx, `INSERT INTO script_runs (id, kind, title, dir, argv, status, exit_code, result,
		started_at, finished_at) VALUES (3, 'hooks', 'Install the hook', '/work/example-repo',
		'["/usr/local/bin/go-pre-commit","install"]', 'succeeded', 0,
		'{"outcome":{"action":"install","outcome":"installed"}}', ?, ?),
		(4, 'checks', 'Checks · all files', '/work/example-repo', '["/usr/local/bin/go-pre-commit","run"]',
		'failed', 1, '{"outcome":{"scope":"all","checks":[{"name":"eof","state":"failed"}],"changed":[{"path":"a"}]}}', ?, ?)`,
		now, now, now, now)
	require.NoError(t, err)

	return c, seededItems{item: ids[0], stuck: ids[1]}
}

func TestBoundResultsHoldNoNullLists(t *testing.T) {
	t.Parallel()

	c, ids := seedBound(t)
	calls := boundCalls(t)

	for _, name := range slices.Sorted(maps.Keys(calls)) {
		got, err := calls[name](t.Context(), c, ids)
		require.NoError(t, err, name)

		for _, path := range nullLists(reflect.ValueOf(got), name) {
			assert.Fail(t, "a list or map the window reads is null", path)
		}
	}

	assertEveryListIsCalled(t, c, calls)
}

// assertEveryListIsCalled checks that boundCalls or unreachedBound names every bound
// method with a list or a map in its result, and nothing else, so a new method is not
// left out.
func assertEveryListIsCalled(t *testing.T, c *Core, calls map[string]boundCall) {
	t.Helper()

	unreached := unreachedBound()
	holding := map[string]bool{}

	for _, svc := range c.Services() {
		v := reflect.ValueOf(svc)
		for i := range v.NumMethod() {
			m := v.Type().Method(i)
			if !resultHoldsLists(m.Type) {
				continue
			}

			name := v.Type().Elem().String() + "." + m.Name
			holding[name] = true

			_, called := calls[name]
			_, skipped := unreached[name]
			assert.True(t, called || skipped, "%s returns a list or a map: call it in boundCalls", name)
		}
	}

	for name := range calls {
		assert.True(t, holding[name], "boundCalls names %s, which is not a bound method with a list", name)
	}

	for name := range unreached {
		assert.True(t, holding[name], "unreachedBound names %s, which is not a bound method with a list", name)
	}
}

// resultHoldsLists reports whether a method's results, its error aside, hold a list or a
// map the window would read.
func resultHoldsLists(method reflect.Type) bool {
	for out := range method.Outs() {
		if holdsList(out, map[reflect.Type]bool{}) {
			return true
		}
	}

	return false
}

// holdsList reports whether a value of type t can hold a slice or a map, directly or in a
// field the JSON encoding writes.
func holdsList(t reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[t] {
		return false
	}

	seen[t] = true
	kind := t.Kind()

	if kind == reflect.Slice || kind == reflect.Map {
		return true
	}

	if kind == reflect.Pointer || kind == reflect.Array {
		return holdsList(t.Elem(), seen)
	}

	return kind == reflect.Struct &&
		slices.ContainsFunc(encodedFields(t), func(f reflect.StructField) bool { return holdsList(f.Type, seen) })
}

// encodedFields returns the fields of the struct type t that the JSON encoding writes.
func encodedFields(t reflect.Type) []reflect.StructField {
	var out []reflect.StructField

	for f := range t.Fields() {
		if f.IsExported() && f.Tag.Get("json") != "-" {
			out = append(out, f)
		}
	}

	return out
}

// nullLists returns the path of every nil slice or map in v, which the JSON encoding
// writes as null. A nil pointer is not one: it stands for a value that is absent.
func nullLists(v reflect.Value, path string) []string {
	kind := v.Kind()

	switch {
	case (kind == reflect.Pointer || kind == reflect.Interface) && !v.IsNil():
		return nullLists(v.Elem(), path)
	case (kind == reflect.Slice || kind == reflect.Map) && v.IsNil():
		return []string{path}
	case kind == reflect.Struct:
		return fieldNulls(v, path)
	case kind == reflect.Slice:
		return elemNulls(v, path)
	case kind == reflect.Map:
		return valueNulls(v, path)
	default:
		return nil
	}
}

// fieldNulls returns nullLists for each field of the struct v the JSON encoding writes.
func fieldNulls(v reflect.Value, path string) []string {
	fields := encodedFields(v.Type())

	out := make([]string, 0, len(fields))
	for _, f := range fields {
		out = append(out, nullLists(v.FieldByIndex(f.Index), path+"."+f.Name)...)
	}

	return out
}

// elemNulls returns nullLists for each element of the slice v.
func elemNulls(v reflect.Value, path string) []string {
	out := make([]string, 0, v.Len())
	for i := range v.Len() {
		out = append(out, nullLists(v.Index(i), path+"["+strconv.Itoa(i)+"]")...)
	}

	return out
}

// valueNulls returns nullLists for each value of the map v.
func valueNulls(v reflect.Value, path string) []string {
	var out []string
	for key, value := range v.Seq2() {
		out = append(out, nullLists(value, path+"["+key.String()+"]")...)
	}

	return out
}
