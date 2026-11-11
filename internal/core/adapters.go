package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/uierr"
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
// whether the menu bar icon shows, the window size, and the text size. It is not bound to the window, so
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

// TextSize returns the text size the window opens at.
func (s ShellSettings) TextSize(ctx context.Context) (settings.TextSize, error) {
	return s.settings.TextSize(ctx)
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

// worktreeRunner returns the runner the worktree service runs git with: search's
// execpath.Exec with git's terminal prompts off, so a fetch that needs a password fails at
// once rather than waiting for an answer no one can give.
func worktreeRunner(search execpath.Search) execpath.Exec {
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT=") })

	return execpath.Exec{Search: search, Env: append(env, "GIT_TERMINAL_PROMPT=0")}
}

// agentRunner runs a coding agent's command, for its --version or for the state of its
// sessions, with the command's own directory first on the search path. An agent installed
// with npm starts through a script that finds node beside it, and an app started from the
// Dock has launchd's search path, which lacks that directory.
type agentRunner struct {
	search execpath.Search

	// env is the environment the command runs with, or nil for this process's.
	env []string
}

// Run runs name with args, its directory first on the search path.
func (r agentRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	base := r.env
	if base == nil {
		base = os.Environ()
	}

	path := strings.Join([]string{filepath.Dir(name), os.Getenv("PATH")}, string(os.PathListSeparator))
	env := slices.DeleteFunc(slices.Clone(base), func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })

	return execpath.Exec{Search: r.search, Env: append(env, "PATH="+path)}.Run(ctx, name, args...)
}

// followSwitch passes the settings service's events on, and wakes the sessions watcher
// when the user turns following on or off, which the settings service says with an
// event.SessionsUpdated, so the watcher starts or stops following at once instead of at
// its next look at the switch. An event.IntegrationsChanged stays in Go: a chosen copy of a
// tool wakes the tool finder, which tells the window once it has looked, and a flipped
// switch, which changes no detection, tells the window at once. The watcher and the finder
// are set once, before any setting can change.
type followSwitch struct {
	sink    event.Sink
	watcher *sessions.Watcher
	tools   waker
}

// waker is woken to look again: *integrations.Worker.
type waker interface {
	Wake()
}

// Emit passes e on, waking the watcher first for an event.SessionsUpdated, and turns an
// event.IntegrationsChanged into a wake of the tool finder or an event.ToolsUpdated.
func (f *followSwitch) Emit(e event.Event) {
	switch e := e.(type) {
	case event.SessionsUpdated:
		if f.watcher != nil {
			f.watcher.Wake()
		}
	case event.IntegrationsChanged:
		if e.CopyChosen && f.tools != nil {
			f.tools.Wake()

			return
		}

		f.sink.Emit(event.ToolsUpdated{})

		return
	}

	f.sink.Emit(e)
}

// latestReleases hands the integrations the tools' latest releases from GitHub. It
// implements integrations.Releases.
type latestReleases struct {
	tokens github.Tokens
	client *github.Client
}

// LatestReleases asks GitHub for each repository's latest release. A token GitHub
// refuses is borrowed again once; with no token at all, as when signed out, nothing is
// asked, and the error is integrations.ErrSignedOut.
func (r latestReleases) LatestReleases(ctx context.Context, repos []repo.Name) (map[string]integrations.Release, error) {
	got, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (map[string]github.LatestRelease, error) {
		return r.client.FetchLatestReleases(ctx, tok, repos)
	})

	switch {
	case errors.Is(err, github.ErrNotLoggedIn) || errors.Is(err, github.ErrGHNotFound):
		return nil, fmt.Errorf("%w: %w", integrations.ErrSignedOut, err)
	case err != nil:
		return nil, err
	}

	out := make(map[string]integrations.Release, len(got))

	for key, release := range got {
		assets := make([]integrations.Asset, 0, len(release.Assets))
		for _, a := range release.Assets {
			assets = append(assets, integrations.Asset{Name: a.Name, URL: a.URL, Size: a.Size, Digest: a.Digest})
		}

		out[key] = integrations.Release{Tag: release.Tag, URL: release.URL, PublishedAt: release.PublishedAt, Assets: assets}
	}

	return out, nil
}

// sessionLedger hands the launcher the sessions ledger, which records each session a
// launch starts. It implements launcher.SessionLedger.
type sessionLedger struct {
	ledger *sessions.Ledger
}

// Started records a session a launch just started.
func (l sessionLedger) Started(ctx context.Context, s launcher.StartedSession) error {
	return l.ledger.Start(ctx, sessions.Record{
		ID: s.ID, AgentSessionID: s.AgentSessionID, ResumedFrom: s.ResumedFrom, Agent: s.Agent, Profile: s.Profile,
		ConfigDir: s.ConfigDir, Prompt: s.Prompt, PromptHead: s.PromptHead, Terminal: s.Terminal, ItemID: s.ItemID,
		Owner: s.Owner, Name: s.Name, Kind: s.Kind, Number: s.Number, Title: s.Title, URL: s.URL, Dir: s.Dir,
		ClonePath: s.ClonePath, Worktree: s.Worktree, LaunchedAt: s.LaunchedAt,
	})
}

// Resumable returns a recorded session, for resuming it.
func (l sessionLedger) Resumable(ctx context.Context, id string) (launcher.ResumableSession, error) {
	r, err := l.ledger.Get(ctx, id)
	if err != nil {
		return launcher.ResumableSession{}, err
	}

	carried := false
	if !r.State.Live() {
		if carried, err = l.ledger.ConversationLive(ctx, r.AgentSessionID); err != nil {
			return launcher.ResumableSession{}, err
		}
	}

	return launcher.ResumableSession{
		ID: r.ID, AgentSessionID: r.AgentSessionID, Agent: r.Agent, Profile: r.Profile, ItemID: r.ItemID, Dir: r.Dir,
		Worktree: r.Worktree, Live: r.State.Live(), Carried: carried,
	}, nil
}

// Running returns the sessions that have not ended running in the folder dir, newest
// first.
func (l sessionLedger) Running(ctx context.Context, dir string) ([]launcher.RunningSession, error) {
	running, err := l.ledger.Running(ctx, dir)
	if err != nil {
		return nil, err
	}

	out := make([]launcher.RunningSession, 0, len(running))
	for _, s := range running {
		out = append(out, launcher.RunningSession{
			SessionID: s.ID, Agent: s.Agent, Doing: s.State.Phrase(), Since: s.StateAt, Dir: s.Dir, Worktree: s.Worktree,
			Repo: s.Repo, Number: s.Number, CanFocus: s.CanFocus,
		})
	}

	return out, nil
}

// ShellSessions are the agent sessions as the desktop shell's menu bar reads them: the
// sessions waiting for the user, and the terminal a click on one brings forward. It
// implements desktop.Sessions.
type ShellSessions struct {
	ledger  *sessions.Ledger
	service *sessions.Service
}

// Waiting returns the running sessions waiting for the user, oldest first.
func (s ShellSessions) Waiting(ctx context.Context) ([]sessions.Session, error) {
	return s.ledger.Waiting(ctx)
}

// Focus brings forward the terminal tab a running session is in.
func (s ShellSessions) Focus(ctx context.Context, id string) error {
	return s.service.Focus(ctx, id)
}

// terminalFocuser hands the sessions service the launcher's terminal drivers, which bring
// a session's tab forward. It implements sessions.Focuser.
type terminalFocuser struct {
	registry terminal.Registry
}

// Focus brings forward the tab that runs on tty in the terminal named by its settings ID.
// A terminal that cannot is told in words.
func (f terminalFocuser) Focus(ctx context.Context, id, tty string) error {
	err := f.registry.Focus(ctx, terminal.ID(id), tty)

	switch {
	case errors.Is(err, terminal.ErrNoSuchTab):
		return uierr.New("Couldn't find the session's tab. It may have been closed, or moved to another app.", err)
	case errors.Is(err, terminal.ErrNoFocus), errors.Is(err, terminal.ErrUnknownTerminal),
		errors.Is(err, terminal.ErrUnsupportedOS):
		return uierr.New("This terminal can't show a session's tab.", err)
	default:
		return err
	}
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

// errNoCommand is returned for a run in the terminal with no command to run.
var errNoCommand = errors.New("the run has no command")

// terminalRuns opens the runs that go in the user's terminal: it writes each one's script
// into the launch folder, as a launch writes its own, recorded so the app follows it, and
// opens it in the terminal chosen in Settings → Launching (scriptrun.TerminalOpener).
type terminalRuns struct {
	launch string
	opener *launcher.CommandOpener
	clock  clock.Clock
	logger *slog.Logger

	// shell is the user's login shell, which a terminal opened in a folder starts.
	shell func() string
}

// OpenRun writes run's script and opens it.
func (t terminalRuns) OpenRun(ctx context.Context, run scriptrun.TerminalRun) error {
	if len(run.Argv) == 0 {
		return errNoCommand
	}

	path, err := terminal.WriteScript(t.launch, run.Name, terminal.Script{
		Dir: run.Dir, Binary: run.Argv[0], Args: run.Argv[1:], Env: envMap(run.Env), NoPrompt: true,
		Record: &terminal.Recording{Log: run.Log, PIDFile: run.PIDFile, ExitFile: run.ExitFile},
	}, clock.Or(t.clock).Now(), nil, t.logger)
	if err != nil {
		return fmt.Errorf("write the run's script: %w", err)
	}

	return t.opener.Open(ctx, path)
}

// OpenFolder opens the terminal in dir, for Open in terminal: a script that changes into
// dir and starts the user's login shell there, and runs nothing else.
func (t terminalRuns) OpenFolder(ctx context.Context, name, dir string) error {
	path, err := terminal.WriteScript(t.launch, name, terminal.Script{
		Purpose: "to open a terminal in a run's folder", Dir: dir, Binary: t.shell(), Args: []string{"-l"},
		NoPrompt: true,
	}, clock.Or(t.clock).Now(), nil, t.logger)
	if err != nil {
		return fmt.Errorf("write the terminal's script: %w", err)
	}

	return t.opener.Open(ctx, path)
}

// finderReveal shows a file in Finder with open -R (scriptrun.Revealer). Core wires it
// only on macOS.
type finderReveal struct {
	runner execpath.Runner
}

// Reveal shows path in Finder.
func (f finderReveal) Reveal(ctx context.Context, path string) error {
	if _, stderr, err := f.runner.Run(ctx, "/usr/bin/open", "-R", path); err != nil {
		reason := execpath.FirstLine(stderr)
		if reason == "" {
			reason = err.Error()
		}

		return uierr.New("Finder couldn't show the file: "+reason, err)
	}

	return nil
}

// revealer is finderReveal over runner on macOS, and nothing elsewhere, where there is
// no Finder: Show the log file then says so.
func revealer(runner execpath.Runner) scriptrun.Revealer {
	if runtime.GOOS != "darwin" {
		return nil
	}

	return finderReveal{runner: runner}
}

// envMap turns NAME=value entries into a map; an entry without "=" sets the name empty.
func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))

	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		out[name] = value
	}

	return out
}

// releaseClones lists the clones under the clone root the settings name, for the Release
// page, with each one's origin when origin is a GitHub repository (release.Clones).
type releaseClones struct {
	settings *settings.Service
	home     string
	git      execpath.Runner
}

// Root returns the clone root as it is set now, with ~ expanded.
func (c releaseClones) Root(ctx context.Context) (string, error) {
	all, err := c.settings.Get(ctx)
	if err != nil {
		return "", err
	}

	root, err := paths.ExpandHome(all.CloneRoot, c.home)
	if err != nil {
		return "", fmt.Errorf("clone root: %w", err)
	}

	return root, nil
}

// Clones scans root.
func (c releaseClones) Clones(ctx context.Context, root string) ([]release.Clone, error) {
	found, err := localrepos.Scan(ctx, root, c.git)
	if err != nil {
		return nil, fmt.Errorf("scan the clone root: %w", err)
	}

	out := make([]release.Clone, 0, len(found))

	for _, r := range found {
		origin, onHub := r.Remotes["origin"]
		out = append(out, release.Clone{Dir: r.Path, Origin: origin, OnHub: onHub})
	}

	return out, nil
}

// releaseGitHub asks GitHub what the Release page needs, borrowing the token as the other
// reads do (release.GitHub). With no token at all, as when signed out, nothing is asked,
// and the error is release.ErrSignedOut.
type releaseGitHub struct {
	tokens github.Tokens
	client *github.Client
}

// ReleaseStatus is round one: each repository's state, default branch, and newest tags.
// answered hears each batch GitHub answers.
func (r releaseGitHub) ReleaseStatus(
	ctx context.Context, repos []repo.Name, answered func(n int),
) (map[string]release.RepoStatus, error) {
	got, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (map[string]github.ReleaseStatus, error) {
		return r.client.FetchReleaseStatus(ctx, tok, repos, answered)
	})
	if err != nil {
		return nil, releaseSignedOut(err)
	}

	out := make(map[string]release.RepoStatus, len(got))

	for key, st := range got {
		tags := make([]release.Tag, 0, len(st.Tags))
		for _, t := range st.Tags {
			tags = append(tags, release.Tag{Name: t.Name, Commit: t.CommitOID})
		}

		out[key] = release.RepoStatus{
			Archived: st.Archived, Permission: st.ViewerPermission, DefaultBranch: st.DefaultBranch,
			HeadOID: st.Head.OID, HeadAt: st.Head.CommittedAt, Headline: st.Head.Headline,
			Checks: release.Checks(st.HeadChecks),
			Tags:   tags,
		}
	}

	return out, nil
}

// ReleaseCompare is round two: how far each default branch is ahead of its version tag.
// answered hears each batch GitHub answers.
func (r releaseGitHub) ReleaseCompare(
	ctx context.Context, reqs []release.CompareRequest, answered func(n int),
) (map[string]release.Compare, error) {
	asks := make([]github.CompareRequest, 0, len(reqs))
	for _, q := range reqs {
		asks = append(asks, github.CompareRequest{Repo: q.Repo, Tag: q.Tag, Branch: q.Branch})
	}

	got, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (map[string]github.ReleaseCompare, error) {
		return r.client.FetchReleaseCompare(ctx, tok, asks, answered)
	})
	if err != nil {
		return nil, releaseSignedOut(err)
	}

	out := make(map[string]release.Compare, len(got))

	for key, c := range got {
		commits := make([]release.Commit, 0, len(c.Commits))
		for _, cm := range c.Commits {
			commits = append(commits, release.Commit{OID: cm.OID, Headline: cm.Headline, At: cm.CommittedAt})
		}

		out[key] = release.Compare{Ahead: c.AheadBy, Commits: commits}
	}

	return out, nil
}

// releaseOrigins reads which GitHub repository a clone's origin is, with the read-only
// remote lookup localrepos.Inspect runs (release.OriginReader).
type releaseOrigins struct {
	git execpath.Runner
}

// Origin returns the repository the clone at dir's origin points at, and whether it is a
// GitHub repository.
func (o releaseOrigins) Origin(ctx context.Context, dir string) (repo.Name, bool, error) {
	found, err := localrepos.Inspect(ctx, dir, o.git)
	if err != nil {
		return repo.Name{}, false, err
	}

	origin, onGitHub := found.Remotes["origin"]

	return origin, onGitHub, nil
}

// releaseTool reads what the integrations know of magex, the copy in use and its
// definition's features, for a release (release.ToolSource). It never waits for a
// detection: List serves the last one.
type releaseTool struct {
	tools *integrations.Service
}

// ReleaseTool returns magex's facts; a list that can't be read is a magex that's missing.
func (r releaseTool) ReleaseTool(ctx context.Context) release.ToolFacts {
	def := integrations.MageX()

	list, err := r.tools.List(ctx)
	if err == nil {
		for _, t := range list.Tools {
			if t.ID == def.ID {
				return release.ToolFactsOf(def, t)
			}
		}
	}

	return release.ToolFactsOf(def, integrations.Tool{})
}

// releaseSettings reads what Release needs of the settings: where runs go, Runs in and the
// terminal chosen for launches, by name (release.Places), and whether a merge offers a
// release (release.MergeSetting).
type releaseSettings struct {
	settings *settings.Service
}

// Where returns where runs go.
func (p releaseSettings) Where(ctx context.Context) (release.Where, error) {
	all, err := p.settings.Get(ctx)
	if err != nil {
		return release.Where{}, err
	}

	return release.Where{RunsIn: all.Integrations.RunsIn, Terminal: terminal.Name(all.Terminal)}, nil
}

// OfferAfterMerge returns the integrations.offerAfterMerge setting.
func (p releaseSettings) OfferAfterMerge(ctx context.Context) (bool, error) {
	all, err := p.settings.Get(ctx)
	if err != nil {
		return false, err
	}

	return all.Integrations.OfferAfterMerge, nil
}

// releaseNotices tells the user of a release that reached GitHub with a notification
// about no item, release_done, and its banner when that kind is on (release.Notifier).
type releaseNotices struct {
	dispatcher *notify.Dispatcher
}

// Released records the notice and shows its banner.
func (n releaseNotices) Released(ctx context.Context, notice release.Notice) {
	n.dispatcher.Notify(ctx, notify.Entry{
		Kind: notify.KindReleaseDone, Title: notice.Title, Body: notice.Body, URL: notice.URL,
		RepoOwner: notice.Repo.Owner, RepoName: notice.Repo.Repo,
	})
}

// releaseSignedOut marks gh's "no one is signed in" as release.ErrSignedOut. A missing gh
// keeps the client's own words, which say to install it.
func releaseSignedOut(err error) error {
	if errors.Is(err, github.ErrNotLoggedIn) {
		return fmt.Errorf("%w: %w", release.ErrSignedOut, err)
	}

	return err
}
