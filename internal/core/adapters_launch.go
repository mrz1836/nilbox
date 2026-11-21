package core

import (
	"context"
	"log/slog"
	"maps"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
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
		APIKeys:         s.APIKeys(),
	}
}

// agentFinder is what a launch finds the agent's program with: opts' AgentFinder, else
// search.
func agentFinder(opts Options, search execpath.Search) execpath.Finder {
	if opts.AgentFinder != nil {
		return opts.AgentFinder
	}

	return search
}

// launchFiles checks the files of the pull requests a launch is for (launcher.FileChecker):
// the inbox's own checks, and the unattended one on its Autopilot face, which the window
// never reaches.
type launchFiles struct {
	*inbox.Service

	routines *inbox.Routines
}

// CheckFilesUnattended checks an item's files under the unattended scope.
func (f launchFiles) CheckFilesUnattended(ctx context.Context, itemID int64) (*sensitive.Check, error) {
	return f.routines.CheckFilesUnattended(ctx, itemID)
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

// DiscardWorktree removes a task's worktree a repository's launch made but couldn't start
// its session in, with its own branch, as the Worktrees tab's Remove does. It implements
// launcher.WorktreeDiscarder.
func (m worktreeMaker) DiscardWorktree(ctx context.Context, req launcher.WorktreeRequest, wt launcher.Worktree) error {
	_, err := m.maker.Discard(ctx, req.Clone, wt.Path, wt.Branch)

	return err
}

// ensureRequest converts the launcher's request for the worktrees package.
func ensureRequest(req launcher.WorktreeRequest) worktrees.EnsureRequest {
	return worktrees.EnsureRequest{
		Clone: req.Clone, Remote: req.Remote, Owner: req.Owner, Name: req.Name, Kind: req.Kind, Number: req.Number,
		DefaultBranch: req.DefaultBranch, Base: req.Base, Head: req.Head, Task: req.Task,
	}
}

// launcherWorktree converts the worktrees package's answer for the launcher.
func launcherWorktree(e worktrees.Ensured) launcher.Worktree {
	return launcher.Worktree{
		Path: e.Path, Branch: e.Branch, Action: launcher.WorktreeAction(e.Action), Base: e.Base, Head: e.Head,
		Dirty: e.Dirty, Notice: e.Notice, Task: e.Task, DefaultBranch: e.DefaultBranch,
	}
}

// repoFacts hands the launcher what the inbox last stored about a repository, which a
// repository's launch checks. It implements launcher.RepoFacts.
type repoFacts struct {
	store *inbox.Store
}

// RepoFacts returns what the last refresh stored about the repository name.
func (r repoFacts) RepoFacts(ctx context.Context, name repo.Name) (launcher.StoredRepo, error) {
	facts, err := r.store.RepoFacts(ctx, name)

	// The two types have the same fields, so a conversion copies every one, and a field
	// added to one but not the other stops the build here.
	return launcher.StoredRepo(facts), err
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
