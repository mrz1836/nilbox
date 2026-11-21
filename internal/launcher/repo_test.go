package launcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The task the harness's repository launches make their worktree for, on its fixed day, and
// the starter they fill in.
const (
	repoTask     = "deps-20260314"
	upgradeDeps  = "Upgrade deps"
	exampleOwner = "acme-org/example-repo"
)

// unknownPush is what the launch sheet says of a repository whose access GitHub hasn't told.
const unknownPush = "GitHub hasn't said whether you can push to acme-org/example-repo. If you can't, the agent stops at " +
	"the push and says so."

// fakeRepoFacts answers what the inbox stored of each repository, by its key, or fails.
type fakeRepoFacts struct {
	facts map[string]StoredRepo
	err   error
}

func (f fakeRepoFacts) RepoFacts(_ context.Context, name repo.Name) (StoredRepo, error) {
	return f.facts[name.Key()], f.err
}

// withFacts has the harness's inbox know f of acme-org/example-repo.
func (h *launchHarness) withFacts(f StoredRepo) {
	h.cfg.RepoFacts = fakeRepoFacts{facts: map[string]StoredRepo{"acme-org/example-repo": f}}
}

// repoRequest asks to upgrade the dependencies of the harness's clone, as the Deps page
// names it.
func (h *launchHarness) repoRequest(t *testing.T) RepoLaunchRequest {
	t.Helper()

	return RepoLaunchRequest{Repo: exampleOwner, Dir: "~/projects/example-repo", PromptID: h.promptID(t, upgradeDeps)}
}

// taskWorktree is the folder of the harness's clone's task worktree named name.
func (h *launchHarness) taskWorktree(name string) string {
	return filepath.Join(h.root, "."+brand.Slug, "worktrees", exampleRepo, name)
}

// launchRepo runs one repository launch and requires it to start, returning its result,
// its script, and its prompt.
func (h *launchHarness) launchRepo(t *testing.T, req RepoLaunchRequest) (LaunchResult, string, string) {
	t.Helper()

	res, err := h.service().LaunchRepo(t.Context(), req)
	require.NoError(t, err)
	require.Nil(t, res.Hold)

	script, err := os.ReadFile(res.ScriptPath)
	require.NoError(t, err)

	return res, string(script), promptFor(t, res.ScriptPath)
}

func TestLaunchRepoStartsInANewWorktreeOfItsOwn(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.withFacts(StoredRepo{Found: true, DefaultBranch: "main", ViewerPermission: "WRITE"})

	res, script, prompt := h.launchRepo(t, h.repoRequest(t))

	assert.Equal(t, LaunchResult{
		ScriptPath: res.ScriptPath, ClonePath: "~/projects/example-repo",
		Dir:      "~/projects/." + brand.Slug + "/worktrees/example-repo/" + repoTask,
		Worktree: repoTask, WorktreeAction: WorktreeCreate, Prompt: upgradeDeps, Profile: profiles.DefaultName,
		Permission: "full access", Terminal: settings.TerminalITerm2, Agent: agent.Claude, SessionID: testSessionID,
	}, res, "trusted as the user's own repository: the profile's own access")
	assert.Equal(t, []string{res.ScriptPath}, h.iterm.opened)
	assert.True(t, strings.HasPrefix(filepath.Base(res.ScriptPath), "repo-20260314-20260314T150926Z-"),
		"named for the task's day, so the seven-day pruning takes it")

	assert.Equal(t, []WorktreeRequest{{
		Clone: h.clone, Remote: "origin", Owner: acmeOrg, Name: exampleRepo, DefaultBranch: "main", Task: repoTask,
	}}, h.maker.ensures(), "a task's worktree, which names no item")

	worktree := h.taskWorktree(repoTask)
	assert.Contains(t, script, "cd -- "+textutil.ShellQuote(worktree)+" || exit 1\n")
	assert.Contains(t, script, "'--dangerously-skip-permissions' '--name=Upgrade deps · example-repo' "+
		"'--session-id="+testSessionID+"'\n")

	assert.True(t, strings.HasPrefix(prompt, "Upgrade the Go and JavaScript dependencies of acme-org/example-repo"))
	assert.Contains(t, prompt, "You are in a fresh worktree, "+repoTask+", on a branch of its own")
	assert.Contains(t, prompt, "`git push -u origin HEAD`")
	assert.Contains(t, prompt, "`gh pr create --repo acme-org/example-repo --base main`")
	assert.NotContains(t, prompt, "outside contributor")

	assert.Empty(t, h.sessions.sessions, "the worktree has no record to touch")
	assert.Len(t, h.launchFiles(t), 2)

	for _, argv := range h.git.recorded() {
		assert.True(t, allowedGitArgv(argv), "only the remote lookup: %q", argv)
		assert.Equal(t, h.clone, argv[2])
	}
}

func TestLaunchRepoRecordsTheSessionWithNoItem(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	res, _, prompt := h.launchRepo(t, h.repoRequest(t))

	started := h.ledger.all()
	require.Len(t, started, 1)

	got := started[0]
	assert.Equal(t, StartedSession{
		ID: testSessionID, AgentSessionID: testSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		Prompt: upgradeDeps, PromptHead: textutil.Clip(prompt, promptHeadRunes), Terminal: "iterm2",
		Owner: acmeOrg, Name: exampleRepo, Title: "Upgrade dependencies", URL: "https://github.com/acme-org/example-repo",
		Dir: h.taskWorktree(repoTask), ClonePath: h.clone, Worktree: repoTask, WorktreeAction: WorktreeCreate,
		Permission: res.Permission, LaunchedAt: fixedNow(),
	}, got, "no item, kind, or number; its title the prompt's name, and its page the repository's")
}

func TestLaunchRepoTrustsItsWorktreeAndTheClone(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	config := filepath.Join(h.home, ".claude.json")

	_, err := h.service().PreviewRepo(t.Context(), h.repoRequest(t))
	require.NoError(t, err)
	assert.NoFileExists(t, config, "a preview trusts nothing")

	h.launchRepo(t, h.repoRequest(t))

	worktree, err := filepath.EvalSymlinks(h.taskWorktree(repoTask))
	require.NoError(t, err)

	clone, err := filepath.EvalSymlinks(h.clone)
	require.NoError(t, err)

	assert.True(t, trustedIn(t, config, worktree))
	assert.True(t, trustedIn(t, config, clone))
}

func TestLaunchRepoNamesTheWorktreeItMade(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.maker.raced = true

	preview, err := h.service().PreviewRepo(t.Context(), h.repoRequest(t))
	require.NoError(t, err)
	require.NotNil(t, preview.Worktree)
	assert.Equal(t, WorktreePlan{
		Name: repoTask, Path: "~/projects/." + brand.Slug + "/worktrees/example-repo/" + repoTask,
		Branch: "worktree-" + repoTask, Action: WorktreeCreate, Base: "origin/main",
	}, *preview.Worktree, "the name it would take now")
	assert.Contains(t, preview.Prompt, "You are in a fresh worktree, "+repoTask+",")
	assert.Empty(t, h.maker.ensures(), "a preview makes nothing")

	// Another launch takes that name between this one's plan and its worktree.
	res, _, prompt := h.launchRepo(t, h.repoRequest(t))
	assert.Equal(t, repoTask+"-2", res.Worktree)
	assert.Contains(t, prompt, "You are in a fresh worktree, "+repoTask+"-2,", "the prompt names the worktree made")
	assert.NotContains(t, prompt, repoTask+",")
	assert.Equal(t, repoTask+"-2", h.ledger.all()[0].Worktree)
	assert.Equal(t, h.taskWorktree(repoTask+"-2"), h.ledger.all()[0].Dir)
}

func TestLaunchRepoNamesTheDefaultBranchTheWorktreeStartsFrom(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.withFacts(StoredRepo{Found: true, DefaultBranch: "trunk", ViewerPermission: "ADMIN"})

	_, _, prompt := h.launchRepo(t, h.repoRequest(t))
	assert.Equal(t, "trunk", h.maker.ensures()[0].DefaultBranch, "the inbox's, for the maker to start from")
	assert.Contains(t, prompt, "--base trunk`")

	// The inbox never saw it: the maker works the default branch out.
	h = newLaunchHarness(t)

	_, _, prompt = h.launchRepo(t, h.repoRequest(t))
	assert.Empty(t, h.maker.ensures()[0].DefaultBranch)
	assert.Contains(t, prompt, "--base main`", "the one the maker found")
}

func TestResolveRepoResolvesTheRepositorysSettings(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	require.NoError(t, testutil.ErrOf(h.settings.SetWorktreeDefault(t.Context(), false)))

	deps := h.promptID(t, upgradeDeps)

	got, err := h.service().ResolveRepo(t.Context(), h.repoRequest(t))
	require.NoError(t, err)
	assert.Zero(t, got.ItemID)
	assert.Empty(t, got.Kind)
	assert.Equal(t, acmeOrg, got.Owner)
	assert.Equal(t, exampleOwner, got.Repo)
	assert.True(t, got.Trusted)
	assert.Equal(t, agent.Claude, got.Agent.ID)
	assert.Equal(t, PromptChoice{
		ID: deps, Nickname: upgradeDeps, Name: "Upgrade dependencies", Source: SourceLaunch, InheritedSource: SourceNone,
	}, got.Prompt, "the launch's own pick, inheriting none")
	assert.Equal(t, Toggle{Value: true, Source: SourceApp, Inherited: true, InheritedSource: SourceApp}, got.Worktree,
		"always a worktree of its own, whatever the settings say")
	assert.Equal(t, Toggle{Source: SourcePrompt, InheritedSource: SourcePrompt}, got.PlanMode, "the starter's own: off")
	assert.NotNil(t, got.ExtraArgs)
	assert.NotNil(t, got.Warnings)
	assert.NotNil(t, got.Situations)
	assert.Empty(t, got.Situations)
	assert.Equal(t, unknownPush, got.Notice)

	assert.Empty(t, h.maker.planned, "resolving plans no worktree")
	assert.Empty(t, h.launchFiles(t))

	// The repository's launch defaults apply, as an item's do.
	h.saveDefaults(t, launchdefaults.Input{Owner: acmeOrg, Name: exampleRepo, Agent: agent.Codex})

	req := h.repoRequest(t)
	req.PlanMode = new(true)

	got, err = h.service().ResolveRepo(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, agent.Codex, got.Agent.ID)
	assert.Equal(t, SourceRepo, got.AgentSource)
	assert.Equal(t, Toggle{Value: true, Source: SourceLaunch, InheritedSource: SourcePrompt}, got.PlanMode,
		"plan mode is still a choice")
}

func TestPreviewRepoShowsThePlan(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	got, err := h.service().PreviewRepo(t.Context(), h.repoRequest(t))
	require.NoError(t, err)
	assert.Equal(t, "~/projects/."+brand.Slug+"/worktrees/example-repo/"+repoTask, got.Dir)
	assert.Equal(t, "full access", got.Permission)
	assert.Contains(t, got.Command, "--name=Upgrade deps · example-repo")
	assert.Equal(t, settings.TerminalITerm2, got.Terminal)
	assert.Contains(t, got.Prompt, "`gh pr create --repo acme-org/example-repo --base main`")
	assert.Equal(t, exampleOwner, got.Resolution.Repo)
	assert.Empty(t, h.iterm.opened)
	assert.Empty(t, h.launchFiles(t))
	assert.Empty(t, h.ledger.all())
}

// repoCalls are the three ways the window reaches a repository's launch, each of which
// checks everything again.
func repoCalls() map[string]func(context.Context, *Service, RepoLaunchRequest) error {
	return map[string]func(context.Context, *Service, RepoLaunchRequest) error{
		"resolve": func(ctx context.Context, s *Service, req RepoLaunchRequest) error {
			_, err := s.ResolveRepo(ctx, req)

			return err
		},
		"preview": func(ctx context.Context, s *Service, req RepoLaunchRequest) error {
			_, err := s.PreviewRepo(ctx, req)

			return err
		},
		"launch": func(ctx context.Context, s *Service, req RepoLaunchRequest) error {
			_, err := s.LaunchRepo(ctx, req)

			return err
		},
	}
}

// requireRefused requires every repoCall to refuse req with want, in words, and to start
// and write nothing.
func requireRefused(t *testing.T, h *launchHarness, req RepoLaunchRequest, want error, words string) {
	t.Helper()

	for name, call := range repoCalls() {
		err := call(t.Context(), h.service(), req)
		require.ErrorIs(t, err, want, name)
		assert.Equal(t, words, uierr.Message(err), name)
	}

	assert.Empty(t, h.iterm.opened)
	assert.Empty(t, h.launchFiles(t))
	assert.Empty(t, h.maker.ensures())
	assert.Empty(t, h.ledger.all())
}

func TestRepoLaunchTakesOnlyAPromptForRepositories(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	req := h.repoRequest(t)

	req.PromptID = 0
	requireRefused(t, h, req, ErrNoRepoPrompt, "Pick a prompt for repositories to launch with.")

	req.PromptID = h.promptID(t, "Review")
	requireRefused(t, h, req, prompts.ErrWrongKind,
		"“Review” isn't a prompt for repositories. Pick a prompt for repositories to launch with.")

	req.PromptID = 9999
	requireRefused(t, h, req, prompts.ErrPromptNotFound, "That prompt is gone. Pick a prompt for repositories to launch with.")

	// An item's launch refuses a prompt for repositories in turn.
	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, PromptID: h.promptID(t, upgradeDeps)})
	require.ErrorIs(t, err, prompts.ErrWrongKind)
}

// withFolders gives the harness's home the folders a repository's launch may be asked to
// start from but never does: a clone of the repository outside the repository locations,
// another repository's clone, a folder that is no clone, and a linked worktree of the clone,
// each in the clone root but the first.
func (h *launchHarness) withFolders(t *testing.T) {
	t.Helper()

	elsewhere := filepath.Join(h.home, "elsewhere", exampleRepo)
	makeRepoDir(t, elsewhere)
	h.git.remotes[elsewhere] = "remote.origin.url https://github.com/acme-org/example-repo.git\n"

	other := filepath.Join(h.root, "other-repo")
	makeRepoDir(t, other)
	h.git.remotes[other] = "remote.origin.url https://github.com/acme-org/other-repo.git\n"

	require.NoError(t, os.MkdirAll(filepath.Join(h.root, "notes"), 0o700))

	linked := linkedWorktreeOf(t, h.clone, filepath.Join(h.root, "example-repo-feature"))
	h.git.remotes[linked] = h.git.remotes[h.clone]
}

func TestRepoLaunchChecksTheRepositoryAndItsClone(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name  string
		edit  func(*RepoLaunchRequest)
		want  error
		words string
	}{
		{
			name: "no repository's name", edit: func(r *RepoLaunchRequest) { r.Repo = "example-repo" },
			want: repo.ErrInvalid, words: `"example-repo" isn't a GitHub repository's name.`,
		},
		{
			name: "a relative folder", edit: func(r *RepoLaunchRequest) { r.Dir = "projects/example-repo" },
			want: ErrNotTheClone, words: "Choose a clone of acme-org/example-repo to launch from.",
		},
		{
			name: "a clone outside the locations", edit: func(r *RepoLaunchRequest) { r.Dir = "~/elsewhere/example-repo" },
			want: ErrNotInLocations, words: "~/elsewhere/example-repo isn't a clone in your repository locations.",
		},
		{
			name: "a linked worktree", edit: func(r *RepoLaunchRequest) { r.Dir = "~/projects/example-repo-feature" },
			want: ErrNotTheClone, words: "~/projects/example-repo-feature is a linked worktree, not a clone of its own. " +
				"Launch from its clone instead.",
		},
		{
			name: "another repository's clone", edit: func(r *RepoLaunchRequest) { r.Dir = "~/projects/other-repo" },
			want: ErrNotTheClone, words: "~/projects/other-repo isn't a clone of acme-org/example-repo; its origin is " +
				"acme-org/other-repo.",
		},
		{
			name: "no clone at all", edit: func(r *RepoLaunchRequest) { r.Dir = "~/projects/notes" },
			want: ErrNotTheClone, words: "~/projects/notes isn't a clone of acme-org/example-repo.",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			h.withFolders(t)

			req := h.repoRequest(t)
			c.edit(&req)

			requireRefused(t, h, req, c.want, c.words)
		})
	}
}

// linkedWorktreeOf lays out, at dir, a linked worktree of the clone at clone as git does:
// its .git file points at a folder of the clone's git folder whose commondir points back.
func linkedWorktreeOf(t *testing.T, clone, dir string) string {
	t.Helper()

	gitDir := filepath.Join(clone, ".git", "worktrees", filepath.Base(dir))
	testutil.WriteFile(t, filepath.Join(gitDir, "commondir"), "../..\n")
	testutil.WriteFile(t, filepath.Join(dir, ".git"), "gitdir: "+gitDir+"\n")

	return dir
}

func TestRepoLaunchRefusesWhatGitHubTakesNoPushTo(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name  string
		facts StoredRepo
		want  error
		words string
	}{
		{
			name: "archived", facts: StoredRepo{Found: true, Archived: true, ViewerPermission: "ADMIN"},
			want: ErrRepoClosed, words: "GitHub says acme-org/example-repo is archived, so nothing can be pushed to it.",
		},
		{
			name: "disabled", facts: StoredRepo{Found: true, Disabled: true},
			want: ErrRepoClosed, words: "GitHub says acme-org/example-repo is disabled, so nothing can be pushed to it.",
		},
		{
			name: "locked", facts: StoredRepo{Found: true, Locked: true, LockReason: "TRADE_RESTRICTION"},
			want: ErrRepoClosed, words: "GitHub says acme-org/example-repo is locked (trade restriction), so nothing can " +
				"be pushed to it.",
		},
		{
			name: "locked for a reason no word of GitHub's", facts: StoredRepo{Found: true, Locked: true, LockReason: "a b"},
			want: ErrRepoClosed, words: "GitHub says acme-org/example-repo is locked, so nothing can be pushed to it.",
		},
		{
			name: "read access", facts: StoredRepo{Found: true, ViewerPermission: "READ"},
			want: ErrNoPushAccess, words: "You can't push to acme-org/example-repo: GitHub gives you read access to it.",
		},
		{
			name: "triage access", facts: StoredRepo{Found: true, ViewerPermission: "TRIAGE"},
			want: ErrNoPushAccess, words: "You can't push to acme-org/example-repo: GitHub gives you triage access to it.",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			h.withFacts(c.facts)

			requireRefused(t, h, h.repoRequest(t), c.want, c.words)
		})
	}
}

func TestLaunchRepoChecksTheRepositoryAgain(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.withFacts(StoredRepo{Found: true, ViewerPermission: "WRITE"})

	_, err := h.service().ResolveRepo(t.Context(), h.repoRequest(t))
	require.NoError(t, err)

	// A refresh since the sheet opened learned the repository was archived.
	h.withFacts(StoredRepo{Found: true, ViewerPermission: "WRITE", Archived: true})

	_, err = h.service().LaunchRepo(t.Context(), h.repoRequest(t))
	require.ErrorIs(t, err, ErrRepoClosed)
	assert.Empty(t, h.iterm.opened)
}

func TestRepoLaunchSaysWhatTheAppCantTell(t *testing.T) {
	t.Parallel()

	const fork = "acme-org/example-repo is a fork of upstream-org/example-repo, so the pull request opens on the fork itself."

	for _, c := range []struct {
		name  string
		facts *StoredRepo
		want  string
	}{
		{name: "no facts to read", want: unknownPush},
		{name: "never seen", facts: &StoredRepo{}, want: unknownPush},
		{name: "access not said", facts: &StoredRepo{Found: true, DefaultBranch: "main"}, want: unknownPush},
		{name: "an access unknown to this build", facts: &StoredRepo{Found: true, ViewerPermission: "OWNER"}, want: unknownPush},
		{name: "write access", facts: &StoredRepo{Found: true, ViewerPermission: "WRITE"}},
		{name: "maintain access", facts: &StoredRepo{Found: true, ViewerPermission: "MAINTAIN"}},
		{
			name: "a fork", facts: &StoredRepo{Found: true, ViewerPermission: "ADMIN", Fork: true, Parent: "upstream-org/example-repo"},
			want: fork,
		},
		{
			name: "a fork of a parent unseen", facts: &StoredRepo{Found: true, Fork: true},
			want: unknownPush + " acme-org/example-repo is a fork, so the pull request opens on the fork itself.",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			if c.facts != nil {
				h.withFacts(*c.facts)
			}

			got, err := h.service().ResolveRepo(t.Context(), h.repoRequest(t))
			require.NoError(t, err)
			assert.Equal(t, c.want, got.Notice)

			h.launchRepo(t, h.repoRequest(t))
		})
	}
}

func TestRepoLaunchSaysWhenTheInboxCantBeRead(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.cfg.RepoFacts = fakeRepoFacts{err: assert.AnError}

	_, err := h.service().ResolveRepo(t.Context(), h.repoRequest(t))
	require.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "read what the inbox knows of acme-org/example-repo")
}

func TestLaunchRepoWaitsForASessionInItsFolder(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	running := h.runningIn("s1", repoTask)
	h.ledger.setRunning(running)

	_, err := h.service().PreviewRepo(t.Context(), h.repoRequest(t))
	require.NoError(t, err, "a preview is never held")

	res, err := h.service().LaunchRepo(t.Context(), h.repoRequest(t))
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, HoldRunning, res.Hold.Reason)
	assert.Equal(t, "running:s1", res.Hold.Key)
	assert.Equal(t, repoTask, res.Hold.Worktree)
	assert.Equal(t, []RunningSession{running.session}, res.Hold.Running)
	assert.Empty(t, h.maker.ensures(), "nothing made")
	assert.Empty(t, h.iterm.opened)

	req := h.repoRequest(t)
	req.Alongside = res.Hold.Key

	res, _, _ = h.launchRepo(t, req)
	assert.Equal(t, repoTask, res.Worktree, "started beside it once confirmed")
}

func TestRepoLaunchNeedsTheWorktreeMaker(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.cfg.Worktrees = nil

	_, err := h.service().LaunchRepo(t.Context(), h.repoRequest(t))
	require.ErrorIs(t, err, ErrNoWorktrees, "a repository's launch always makes a worktree of its own")
	assert.Empty(t, h.launchFiles(t))
}

func TestRepoLaunchRunsCodex(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	req := h.repoRequest(t)
	req.Agent, req.Profile = agent.Codex, profileWork

	res, script, _ := h.launchRepo(t, req)
	assert.Equal(t, agent.Codex, res.Agent)
	assert.Equal(t, "permission mode workspace-write", res.Permission)
	assert.Contains(t, script, "cd -- "+textutil.ShellQuote(h.taskWorktree(repoTask))+" || exit 1\n")
	assert.Empty(t, h.ledger.all()[0].AgentSessionID, "Codex names its own thread")
}

func TestResumeRefusesARepositorysSession(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: testSessionID, AgentSessionID: testSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		Dir: h.taskWorktree(repoTask), Worktree: repoTask,
	}}

	_, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.ErrorIs(t, err, ErrNoItem)
	assert.Equal(t, "This session was started for a repository rather than for a pull request or an issue, so it "+
		"can't be resumed from here. Copy the session's ID to resume it yourself.", uierr.Message(err))
	assert.Empty(t, h.iterm.opened)
}

func TestARepositorysLaunchPlanNamesItsFilesByItsTask(t *testing.T) {
	t.Parallel()

	req := baseRequest()
	req.Item = Item{Repo: exampleOwner, Trusted: true}
	req.Task = repoTask

	plan, err := BuildPlan(req)
	require.NoError(t, err)
	assert.Equal(t, repoTask, plan.Task)

	files, err := WriteLaunchFiles(testPaths(t), plan, "Upgrade it", fixedNow(), nil)
	require.NoError(t, err)
	assert.Regexp(t, `^repo-20260314-20260314T150926Z-[0-9a-f]{8}\.command$`, filepath.Base(files.Script))
	assert.Regexp(t, `^repo-20260314-20260314T150926Z-[0-9a-f]{8}\.prompt\.md$`, filepath.Base(files.Prompt))

	for name, edit := range map[string]func(*Request){
		"a task that isn't one": func(r *Request) { r.Task = "deps-x" },
		"an item too":           func(r *Request) { r.Item.Kind, r.Item.Number = item.KindPR, 7 },
		"a number too":          func(r *Request) { r.Item.Number = 7 },
	} {
		bad := req
		edit(&bad)

		_, err = BuildPlan(bad)
		require.ErrorIs(t, err, ErrInvalidTask, name)
	}

	plan.Kind = item.KindIssue
	_, err = WriteLaunchFiles(testPaths(t), plan, "Upgrade it", fixedNow(), nil)
	require.ErrorIs(t, err, ErrInvalidTask, "a plan names a task or an item, never both")

	req.Task = ""
	_, err = BuildPlan(req)
	require.ErrorIs(t, err, item.ErrInvalidKind, "with no task, an item is needed")
}
