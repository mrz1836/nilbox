package core

import (
	"context"
	"log/slog"
	"maps"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// launchSettings is the part of the settings a launch reads.
func launchSettings(s settings.Settings) launcher.LaunchSettings {
	return launcher.LaunchSettings{
		DefaultAgent:    s.DefaultAgent,
		DefaultProfiles: maps.Clone(s.DefaultProfiles),
		WorktreeDefault: s.WorktreeDefault,
		Terminal:        s.Terminal,
		CloneRoot:       s.CloneRoot,
		CloneLocations:  s.CloneLocations,
		TrustedBots:     s.TrustedBots,
		EffortOverrides: s.CodexEffort,
		FastMode:        s.CodexFast,
	}
}

// worktreeMaker hands the launcher the worktrees package's Maker. It implements
// launcher.WorktreeMaker.
type worktreeMaker struct {
	maker *worktrees.Maker
}

// PlanWorktree says what EnsureWorktree would do.
func (m worktreeMaker) PlanWorktree(ctx context.Context, req launcher.WorktreeRequest) (launcher.Worktree, error) {
	got, err := m.maker.Plan(ctx, ensureRequest(req))

	return launcherWorktree(got), err
}

// EnsureWorktree finds or makes the item's worktree.
func (m worktreeMaker) EnsureWorktree(ctx context.Context, req launcher.WorktreeRequest) (launcher.Worktree, error) {
	got, err := m.maker.Ensure(ctx, ensureRequest(req))

	return launcherWorktree(got), err
}

// ensureRequest converts the launcher's request for the worktrees package.
func ensureRequest(req launcher.WorktreeRequest) worktrees.EnsureRequest {
	return worktrees.EnsureRequest{
		Clone: req.Clone, Remote: req.Remote, Owner: req.Owner, Name: req.Name, Kind: req.Kind, Number: req.Number,
		DefaultBranch: req.DefaultBranch, Head: req.Head,
	}
}

// launcherWorktree converts the worktrees package's answer for the launcher.
func launcherWorktree(e worktrees.Ensured) launcher.Worktree {
	return launcher.Worktree{
		Path: e.Path, Branch: e.Branch, Action: launcher.WorktreeAction(e.Action), Base: e.Base, Head: e.Head,
		Dirty: e.Dirty, Notice: e.Notice,
	}
}

// trusters returns each agent's Truster, which marks the folder a session starts in
// trusted in that agent's config.
func trusters(home string, logger *slog.Logger) map[agent.ID]launcher.Truster {
	return map[agent.ID]launcher.Truster{
		agent.Claude: workspacetrust.Claude{Home: home},
		agent.Codex:  workspacetrust.Codex{Home: home, Logger: logger},
	}
}

// launchItems hands the launcher the inbox's stored items, the signed-in account, and
// the clone each repository resolved to. It implements launcher.ItemStore.
type launchItems struct {
	*inbox.Store
}

// LaunchItems returns the stored items among ids, by id.
func (s launchItems) LaunchItems(ctx context.Context, ids []int64) ([]launcher.StoredItem, error) {
	stored, err := s.Store.LaunchItems(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]launcher.StoredItem, 0, len(stored))

	// The two types have the same fields, so a conversion copies every one, and a field
	// added to one but not the other stops the build here.
	for _, it := range stored {
		items = append(items, launcher.StoredItem(it))
	}

	return items, nil
}
