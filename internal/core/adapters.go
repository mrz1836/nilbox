package core

import (
	"context"
	"database/sql"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// settingsView hands a service the part of the settings it reads. Each Get reads the
// settings as they are now, so a change the user just made applies to the next launch
// or sweep.
type settingsView[T any] struct {
	settings *settings.Service
	part     func(settings.Settings) T
}

// Get reads the settings and returns the part.
func (v settingsView[T]) Get(ctx context.Context) (T, error) {
	all, err := v.settings.Get(ctx)
	if err != nil {
		var zero T

		return zero, err
	}

	return v.part(all), nil
}

// ShellSettings are the settings the desktop shell reads when the app launches: the theme,
// whether the menu bar icon shows, and the window size. It is not bound to the window, so
// the size, which only the shell reads on its own, stays out of the frontend's API.
type ShellSettings struct {
	settings *settings.Service
	prefs    *prefs.Store
}

// newShellSettings returns the shell's settings over the settings service and the
// preferences in pool.
func newShellSettings(svc *settings.Service, pool *sql.DB, c clock.Clock) ShellSettings {
	return ShellSettings{settings: svc, prefs: prefs.New(pool, c)}
}

// Theme returns the stored theme, or the default.
func (s ShellSettings) Theme(ctx context.Context) (settings.Theme, error) {
	return s.settings.Theme(ctx)
}

// MenuBar returns whether the menu bar icon shows.
func (s ShellSettings) MenuBar(ctx context.Context) (bool, error) {
	return s.settings.MenuBar(ctx)
}

// WindowSize returns the size the window opens at.
func (s ShellSettings) WindowSize(ctx context.Context) (settings.WindowSize, error) {
	return settings.ReadWindowSize(ctx, s.prefs)
}

// launchSettings is the part of the settings a launch reads.
func launchSettings(s settings.Settings) launcher.LaunchSettings {
	return launcher.LaunchSettings{
		DefaultAgent:    s.DefaultAgent,
		DefaultProfiles: maps.Clone(s.DefaultProfiles),
		WorktreeDefault: s.WorktreeDefault,
		Terminal:        s.Terminal,
		CloneRoot:       s.CloneRoot,
		TrustedBots:     s.TrustedBots,
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

// worktreeRunner returns the runner the worktree service runs git with: search's
// execpath.Exec with git's terminal prompts off, so a fetch that needs a password fails at
// once rather than waiting for an answer no one can give.
func worktreeRunner(search execpath.Search) execpath.Exec {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") })

	return execpath.Exec{Search: search, Env: append(env, "GIT_TERMINAL_PROMPT=0")}
}

// agentRunner runs a coding agent's command, for its --version, with the command's own
// directory first on the search path. An agent installed with npm starts through a script
// that finds node beside it, and an app started from the Dock has launchd's search path,
// which lacks that directory.
type agentRunner struct {
	search execpath.Search
}

// Run runs name with args, its directory first on the search path.
func (r agentRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	path := strings.Join([]string{filepath.Dir(name), os.Getenv("PATH")}, string(os.PathListSeparator))
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })

	return execpath.Exec{Search: r.search, Env: append(env, "PATH="+path)}.Run(ctx, name, args...)
}

// sweepSettings is the part of the settings the worktree service reads.
func sweepSettings(s settings.Settings) worktrees.SweepSettings {
	return worktrees.SweepSettings{CloneRoot: s.CloneRoot, AutoCleanup: s.WorktreeAutoCleanup, Branches: s.Branches}
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

// rateReader reads every API budget from GitHub's free rate limit endpoint, with the GitHub
// CLI's token, for the budget service. The readings reach the budget tracker through the
// client.
type rateReader struct {
	tokens github.Tokens
	client *github.Client
}

// ReadRates reads every budget. A token GitHub refuses is borrowed again once.
func (r rateReader) ReadRates(ctx context.Context) error {
	_, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (*github.RateLimits, error) {
		return r.client.FetchRateLimits(ctx, tok)
	})

	return err
}

// keyBudgetState is where the API budget's state is kept between runs. Only the budget
// writes it, through budgetKeeper.
const keyBudgetState = "budget.state"

// budgetKeeper keeps the API budget's state in the prefs table, for the budget's Saver.
type budgetKeeper struct {
	prefs *prefs.Store
}

// newBudgetKeeper returns a budgetKeeper over the prefs in pool, stamped from c.
func newBudgetKeeper(pool *sql.DB, c clock.Clock) budgetKeeper {
	return budgetKeeper{prefs: prefs.New(pool, c)}
}

// Load returns the state kept, or "" when none is.
func (k budgetKeeper) Load(ctx context.Context) (string, error) {
	state, _, err := k.prefs.Get(ctx, keyBudgetState)

	return state, err
}

// Save replaces the state kept.
func (k budgetKeeper) Save(ctx context.Context, state string) error {
	return k.prefs.Set(ctx, keyBudgetState, state)
}

// storedRates converts the budget readings the last refresh stored, for seeding the budget
// tracker.
func storedRates(gauge inbox.RateGauge) []budget.Stored {
	out := make([]budget.Stored, 0, 3)

	for resource, r := range map[string]inbox.Rate{
		github.ResourceGraphQL: gauge.GraphQL, github.ResourceCore: gauge.Core, github.ResourceSearch: gauge.Search,
	} {
		if !r.Known {
			continue
		}

		resetAt, errReset := time.Parse(time.RFC3339, r.ResetAt)
		takenAt, errTaken := time.Parse(time.RFC3339, r.TakenAt)

		if errReset != nil || errTaken != nil {
			continue
		}

		out = append(out, budget.Stored{
			Reading: github.RateReading{
				Resource: resource, Limit: r.Limit, Remaining: r.Remaining, Used: r.Used, ResetAt: resetAt,
			},
			TakenAt: takenAt,
		})
	}

	return out
}

// itemStates reads the state of the pull requests and issues worktrees are linked to,
// with the GitHub CLI's token, for the worktree service.
type itemStates struct {
	tokens github.Tokens
	client *github.Client
}

// ItemStates asks GitHub about refs. A token GitHub refuses is borrowed again once.
func (s itemStates) ItemStates(ctx context.Context, refs []item.Ref) (map[string]worktrees.ItemState, error) {
	states, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (map[string]github.ItemState, error) {
		return s.client.FetchItemStates(ctx, tok, refs)
	})
	if err != nil {
		return nil, err
	}

	out := make(map[string]worktrees.ItemState, len(states))
	for key, state := range states {
		out[key] = worktreeState(state)
	}

	return out, nil
}

// MergedPullRequests asks GitHub which branch names were the head of a merged pull
// request. A token GitHub refuses is borrowed again once.
func (s itemStates) MergedPullRequests(ctx context.Context, queries []worktrees.BranchQuery,
) (map[worktrees.BranchKey][]worktrees.MergedPR, error) {
	asks := make([]github.BranchQuery, 0, len(queries))
	for _, q := range queries {
		asks = append(asks, github.BranchQuery{Repo: q.Repo, Branches: q.Branches})
	}

	answers, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (map[string][]github.MergedPR, error) {
		return s.client.FetchMergedPullRequests(ctx, tok, asks)
	})
	if err != nil {
		return nil, err
	}

	out := map[worktrees.BranchKey][]worktrees.MergedPR{}

	for _, q := range queries {
		for _, branch := range q.Branches {
			if prs := answers[github.MergedKey(q.Repo, branch)]; len(prs) > 0 {
				out[worktrees.BranchKey{Repo: q.Repo.Key(), Branch: branch}] = worktreeMerged(prs)
			}
		}
	}

	return out, nil
}

// worktreeMerged converts GitHub's merged pull requests for the worktree service.
func worktreeMerged(prs []github.MergedPR) []worktrees.MergedPR {
	out := make([]worktrees.MergedPR, 0, len(prs))
	for _, pr := range prs {
		out = append(out, worktrees.MergedPR{
			Number: pr.Number, Title: pr.Title, URL: pr.URL, MergedAt: pr.MergedAt, HeadOID: pr.HeadOID,
		})
	}

	return out
}

// worktreeState converts GitHub's answer about an item for the worktree service. An item
// GitHub reports neither merged nor closed is open.
func worktreeState(s github.ItemState) worktrees.ItemState {
	out := worktrees.ItemState{
		PullRequest: s.PullRequest, Title: s.Title, URL: s.URL, State: worktrees.ItemOpen, ClosedAt: s.ClosedAt,
		HeadOID: s.HeadOID, HeadRef: s.HeadRef,
	}

	switch s.State {
	case github.StateMerged:
		out.State = worktrees.ItemMerged
	case github.StateClosed:
		out.State = worktrees.ItemClosed
	}

	return out
}
