package launcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Prompt IDs in testWorld.
const (
	reviewID    int64 = 1
	quickID     int64 = 2
	workID      int64 = 3
	bothID      int64 = 4
	missingID   int64 = 99
	strOpus           = "opus"
	strSonnet         = "sonnet"
	profileWork       = "work"
	acmeOrg           = "acme-org"
	flagDebug         = "--debug"
)

// resolveErr resolves and returns only the error.
func resolveErr(w world, item subject, req LaunchRequest) error {
	_, _, _, err := resolve(w, item, req) //nolint:dogsled // only the error matters here

	return err
}

// claudeOnly is a per-agent setting only Claude has.
func claudeOnly[V any](v V) map[agent.ID]V {
	return map[agent.ID]V{agent.Claude: v}
}

// testWorld is a world with two Claude profiles and two Codex profiles, four prompts, and
// no launch defaults: the app's default agent is Claude, each agent's default profile is
// "default", and the worktree default is on.
func testWorld() world {
	full := fullProfile()
	work := fullProfile()
	work.Name, work.Model, work.Effort, work.ExtraArgs = profileWork, strOpus, "high", []string{"--advisor", "fable"}

	codexFull, codexWork := full, work
	codexFull.Agent = agent.Codex
	codexWork.Agent, codexWork.Model, codexWork.Effort, codexWork.ExtraArgs = agent.Codex, "gpt-6-sol", "ultra",
		[]string{"--search"}

	return world{
		settings: LaunchSettings{
			DefaultAgent:    agent.Claude,
			DefaultProfiles: map[agent.ID]string{agent.Claude: profiles.DefaultName, agent.Codex: profiles.DefaultName},
			WorktreeDefault: true,
		},
		defaults: prompts.DefaultPrompts{PR: reviewID, Issue: workID},
		prompts: map[int64]prompts.Prompt{
			reviewID: {ID: reviewID, Nickname: "Review", Name: "Review a pull request", AppliesTo: prompts.AppliesToPR},
			quickID: {
				ID: quickID, Nickname: "Quick look", AppliesTo: prompts.AppliesToPR, Models: claudeOnly(strSonnet),
				Effort: "low", Worktree: new(false), ExtraArgs: claudeOnly([]string{flagDebug}),
			},
			workID: {ID: workID, Nickname: "Work on it", AppliesTo: prompts.AppliesToIssue},
			bothID: {ID: bothID, Nickname: "Both", AppliesTo: prompts.AppliesToAny, PlanMode: new(true)},
		},
		profiles: map[profileKey]profiles.Profile{
			{agent: agent.Claude, name: profiles.DefaultName}: full,
			{agent: agent.Claude, name: profileWork}:          work,
			{agent: agent.Codex, name: profiles.DefaultName}:  codexFull,
			{agent: agent.Codex, name: profileWork}:           codexWork,
		},
		rules: launchdefaults.NewIndex(nil),
	}
}

// prSubject is a trusted pull request in acme-org/example-repo.
func prSubject() subject {
	return subject{id: 7, kind: item.KindPR, repo: repo.Name{Owner: acmeOrg, Repo: "example-repo"}, trusted: true}
}

func TestResolveAppDefaults(t *testing.T) {
	t.Parallel()

	res, profile, prompt, err := resolve(testWorld(), prSubject(), LaunchRequest{ItemID: 7})
	require.NoError(t, err)

	assert.Equal(t, profiles.DefaultName, profile.Name)
	assert.Equal(t, reviewID, prompt.ID)
	assert.Equal(t, agent.Default(), res.Agent)
	assert.Equal(t, SourceApp, res.AgentSource)
	assert.Equal(t, agent.Claude, res.AgentInherited)
	assert.Equal(t, SourceApp, res.AgentInheritedSource)
	assert.Equal(t, Setting{Value: "default", Source: SourceApp, Inherited: "default", InheritedSource: SourceApp}, res.Profile)
	assert.Equal(t, PromptChoice{
		ID: reviewID, Nickname: "Review", Name: "Review a pull request", Source: SourceApp,
		InheritedID: reviewID, InheritedSource: SourceApp,
	}, res.Prompt)
	assert.Equal(t, Setting{Source: SourceAgent, InheritedSource: SourceAgent}, res.Model, "nothing sets a model")
	assert.Equal(t, Toggle{Value: true, Source: SourceApp, Inherited: true, InheritedSource: SourceApp}, res.Worktree)
	assert.Equal(t, Toggle{Source: SourceProfile, InheritedSource: SourceProfile}, res.PlanMode)
	assert.Empty(t, res.ExtraArgs)
	assert.NotNil(t, res.ExtraArgs)
	assert.Empty(t, res.Warnings)
	assert.Equal(t, "acme-org/example-repo", res.Repo)
	assert.True(t, res.Trusted)

	res, _, prompt, err = resolve(testWorld(), subject{id: 3, kind: item.KindIssue, repo: repo.Name{Owner: acmeOrg, Repo: "x"}}, LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, workID, prompt.ID, "issues get the issue default")
	assert.Equal(t, "Work on it", res.Prompt.Nickname)
}

func TestResolvePrecedence(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{
			Owner: acmeOrg, Profiles: claudeOnly(profileWork), Models: claudeOnly("haiku"), PlanMode: new(true),
			ExtraArgs: claudeOnly([]string{"--org"}),
		},
		{
			Owner: acmeOrg, Name: "example-repo", Models: claudeOnly(strSonnet), Worktree: new(false),
			ExtraArgs: claudeOnly([]string{"--repo"}),
		},
	})

	res, profile, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)

	assert.Equal(t, profileWork, profile.Name)
	assert.Equal(t, SourceOrg, res.Profile.Source)
	assert.Equal(t, Setting{Value: strSonnet, Source: SourceRepo, Inherited: strSonnet, InheritedSource: SourceRepo}, res.Model,
		"the repository wins over its org, which wins over the profile")
	assert.Equal(t, Setting{Value: "high", Source: SourceProfile, Inherited: "high", InheritedSource: SourceProfile}, res.Effort)
	assert.Equal(t, Toggle{Value: true, Source: SourceOrg, Inherited: true, InheritedSource: SourceOrg}, res.PlanMode)
	assert.Equal(t, Toggle{Source: SourceRepo, InheritedSource: SourceRepo}, res.Worktree)
	assert.Equal(t, []string{"--advisor", "fable", "--org", "--repo"}, res.ExtraArgs,
		"the profile's flags, then the org's, then the repository's")

	// The prompt's own settings win over the repository's.
	res, _, _, err = resolve(w, prSubject(), LaunchRequest{PromptID: quickID})
	require.NoError(t, err)
	assert.Equal(t, PromptChoice{
		ID: quickID, Nickname: "Quick look", Source: SourceLaunch, InheritedID: reviewID, InheritedSource: SourceApp,
	}, res.Prompt)
	assert.Equal(t, Setting{Value: strSonnet, Source: SourcePrompt, Inherited: strSonnet, InheritedSource: SourcePrompt}, res.Model)
	assert.Equal(t, Setting{Value: "low", Source: SourcePrompt, Inherited: "low", InheritedSource: SourcePrompt}, res.Effort)
	assert.Equal(t, []string{"--advisor", "fable", "--org", "--repo", flagDebug}, res.ExtraArgs)

	// The launch's own choices win over everything, and remember what they replaced.
	res, profile, _, err = resolve(w, prSubject(), LaunchRequest{
		PromptID: quickID, Profile: profiles.DefaultName, Model: new(strOpus), Effort: new(""),
		PlanMode: new(false), Worktree: new(true),
	})
	require.NoError(t, err)
	assert.Equal(t, profiles.DefaultName, profile.Name)
	assert.Equal(t, Setting{Value: "default", Source: SourceLaunch, Inherited: profileWork, InheritedSource: SourceOrg}, res.Profile)
	assert.Equal(t, Setting{Value: strOpus, Source: SourceLaunch, Inherited: strSonnet, InheritedSource: SourcePrompt}, res.Model)
	assert.Equal(t, Setting{Value: "", Source: SourceLaunch, Inherited: "low", InheritedSource: SourcePrompt}, res.Effort,
		"an empty choice asks for the agent's own default")
	assert.Equal(t, Toggle{Value: false, Source: SourceLaunch, Inherited: true, InheritedSource: SourceOrg}, res.PlanMode)
	assert.Equal(t, Toggle{Value: true, Source: SourceLaunch, Inherited: false, InheritedSource: SourcePrompt}, res.Worktree)
	assert.Equal(t, []string{"--org", "--repo", flagDebug}, res.ExtraArgs, "the default profile has no flags")
}

func TestResolveDefaultPrompts(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, PRPromptID: new(bothID)},
		{Owner: acmeOrg, Name: "example-repo", PRPromptID: new(quickID)},
		{Owner: acmeOrg, Name: "other-repo", PRPromptID: new(missingID), IssuePromptID: new(reviewID)},
	})

	res, _, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, quickID, res.Prompt.ID)
	assert.Equal(t, SourceRepo, res.Prompt.Source)

	other := prSubject()
	other.repo.Repo = "third-repo"
	res, _, _, err = resolve(w, other, LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, bothID, res.Prompt.ID)
	assert.Equal(t, SourceOrg, res.Prompt.Source)
	assert.Equal(t, Toggle{Value: true, Source: SourcePrompt, Inherited: true, InheritedSource: SourcePrompt}, res.PlanMode)

	// A repository default that is gone, or for the other kind, is skipped with a warning.
	other.repo.Repo = "other-repo"
	res, _, _, err = resolve(w, other, LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, bothID, res.Prompt.ID, "the org's default is next")
	assert.Equal(t, []string{"The repo default prompt for pull requests no longer exists, so it was skipped."}, res.Warnings)

	issue := subject{id: 3, kind: item.KindIssue, repo: repo.Name{Owner: acmeOrg, Repo: "other-repo"}}
	res, _, _, err = resolve(w, issue, LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, workID, res.Prompt.ID, "a pull request prompt is no issue default")
	assert.Len(t, res.Warnings, 1)
}

func TestResolveSkipsDefaultsThatAreGone(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, Name: "example-repo", Profiles: claudeOnly("pruned"), Agent: "example-agent"},
	})

	res, profile, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, profiles.DefaultName, profile.Name)
	assert.Equal(t, agent.Default(), res.Agent)
	assert.Equal(t, []string{
		`The repo default names a coding agent this version doesn't know ("example-agent"), so it was skipped.`,
		`The repo default Claude profile "pruned" no longer exists, so it was skipped.`,
	}, res.Warnings)
}

func TestResolveTheAgent(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.settings.DefaultAgent = agent.Codex

	res, profile, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, agent.Codex, res.Agent.ID, "the app's default agent")
	assert.Equal(t, SourceApp, res.AgentSource)
	assert.Equal(t, agent.Codex, profile.Agent, "with one of its own profiles")

	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, Agent: agent.Claude},
		{Owner: acmeOrg, Name: "example-repo", Agent: agent.Codex},
	})

	res, _, _, err = resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, agent.Codex, res.Agent.ID)
	assert.Equal(t, SourceRepo, res.AgentSource, "the repository wins over its org")

	other := prSubject()
	other.repo.Repo = "other-repo"
	res, _, _, err = resolve(w, other, LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, agent.Claude, res.Agent.ID)
	assert.Equal(t, SourceOrg, res.AgentSource, "the org wins over the app")

	res, _, _, err = resolve(w, prSubject(), LaunchRequest{Agent: agent.Claude})
	require.NoError(t, err)
	assert.Equal(t, agent.Claude, res.Agent.ID, "the launch's own switch wins over everything")
	assert.Equal(t, SourceLaunch, res.AgentSource)
	assert.Equal(t, agent.Codex, res.AgentInherited, "and remembers what it replaced")
	assert.Equal(t, SourceRepo, res.AgentInheritedSource)

	require.ErrorIs(t, resolveErr(w, prSubject(), LaunchRequest{Agent: "example-agent"}), agent.ErrUnknownAgent)
}

func TestResolveEachAgentsOwnSettings(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{
			Owner: acmeOrg, Effort: "ultra",
			Profiles:  map[agent.ID]string{agent.Claude: profiles.DefaultName, agent.Codex: profileWork},
			Models:    map[agent.ID]string{agent.Claude: "haiku", agent.Codex: "gpt-6-luna"},
			ExtraArgs: map[agent.ID][]string{agent.Claude: {"--org-claude"}, agent.Codex: {"--org-codex"}},
		},
	})

	claude, profile, _, err := resolve(w, prSubject(), LaunchRequest{PromptID: quickID})
	require.NoError(t, err)
	assert.Equal(t, profiles.DefaultName, profile.Name)
	assert.Equal(t, strSonnet, claude.Model.Value, "the prompt's Claude model")
	assert.Equal(t, []string{"--org-claude", flagDebug}, claude.ExtraArgs)
	assert.Equal(t, Setting{Value: "low", Source: SourcePrompt, Inherited: "low", InheritedSource: SourcePrompt}, claude.Effort,
		"the prompt's effort wins")

	codex, profile, _, err := resolve(w, prSubject(), LaunchRequest{PromptID: quickID, Agent: agent.Codex})
	require.NoError(t, err)
	assert.Equal(t, profileWork, profile.Name, "the org's Codex profile")
	assert.Equal(t, agent.Codex, profile.Agent)
	assert.Equal(t, "gpt-6-luna", codex.Model.Value, "the prompt sets no Codex model, so the org's is used")
	assert.Equal(t, []string{"--search", "--org-codex"}, codex.ExtraArgs, "only Codex's flags")
	assert.Equal(t, "low", codex.Effort.Value, "the prompt's effort is shared by every agent")

	codex, _, _, err = resolve(w, prSubject(), LaunchRequest{Agent: agent.Codex})
	require.NoError(t, err)
	assert.Equal(t, "ultra", codex.Effort.Value, "Codex takes ultra")
	assert.Empty(t, codex.Warnings)
}

func TestResolveSkipsAnEffortTheAgentLacks(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{{Owner: acmeOrg, Effort: "ultra"}})

	res, _, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, Setting{Source: SourceAgent, InheritedSource: SourceAgent}, res.Effort)
	assert.Equal(t, []string{`Claude doesn't take the "ultra" effort level, so its default is used.`}, res.Warnings)

	res, _, _, err = resolve(w, prSubject(), LaunchRequest{Effort: new("ultra")})
	require.NoError(t, err)
	assert.Equal(t, Setting{Source: SourceAgent, InheritedSource: SourceAgent}, res.Effort, "a launch's choice too")

	res, _, _, err = resolve(w, prSubject(), LaunchRequest{Effort: new("max")})
	require.NoError(t, err)
	assert.Equal(t, Setting{Value: "max", Source: SourceLaunch, InheritedSource: SourceAgent}, res.Effort)
}

func TestResolveProfileFallbacks(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.settings.DefaultProfiles[agent.Claude] = "pruned"

	res, _, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, profiles.DefaultName, res.Profile.Value, "a missing app default falls back to default")

	delete(w.profiles, profileKey{agent: agent.Claude, name: profiles.DefaultName})
	res, _, _, err = resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, profileWork, res.Profile.Value, "then to the first profile by name")

	delete(w.profiles, profileKey{agent: agent.Codex, name: profiles.DefaultName})
	delete(w.profiles, profileKey{agent: agent.Codex, name: profileWork})

	err = resolveErr(w, prSubject(), LaunchRequest{Agent: agent.Codex})
	require.ErrorIs(t, err, ErrNoProfile, "another agent's profiles never stand in")
	assert.Contains(t, err.Error(), "no Codex profile was found; run codex once so it creates ~/.codex")
}

func TestResolveRejectsBadChoices(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, resolveErr(testWorld(), prSubject(), LaunchRequest{Profile: "nope"}), profiles.ErrProfileNotFound)
	require.ErrorIs(t, resolveErr(testWorld(), prSubject(), LaunchRequest{PromptID: missingID}), prompts.ErrPromptNotFound)

	err := resolveErr(testWorld(), prSubject(), LaunchRequest{PromptID: workID})
	require.ErrorIs(t, err, prompts.ErrWrongKind)
	assert.Contains(t, err.Error(), `"Work on it" isn't for pull requests`)

	w := testWorld()
	w.defaults = prompts.DefaultPrompts{}
	delete(w.prompts, workID)
	delete(w.prompts, bothID)
	err = resolveErr(w, subject{id: 3, kind: item.KindIssue, repo: repo.Name{Owner: acmeOrg, Repo: "x"}}, LaunchRequest{})
	require.ErrorIs(t, err, ErrNoPrompt)
}

func TestResolveRechecksStoredFlags(t *testing.T) {
	t.Parallel()

	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, ExtraArgs: claudeOnly([]string{"--dangerously-skip-permissions"})},
	})

	err := resolveErr(w, prSubject(), LaunchRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the org launch defaults' Claude flags")
	require.NoError(t, resolveErr(w, prSubject(), LaunchRequest{Agent: agent.Codex}), "only the agent's own flags count")

	w = testWorld()
	quick := w.prompts[quickID]
	quick.ExtraArgs = map[agent.ID][]string{agent.Codex: {"--yolo"}}
	w.prompts[quickID] = quick

	err = resolveErr(w, prSubject(), LaunchRequest{PromptID: quickID, Agent: agent.Codex})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the "Quick look" prompt's Codex flags`)
}

func TestEffectiveProfile(t *testing.T) {
	t.Parallel()

	p := fullProfile()
	p.Model, p.ExtraArgs = strOpus, []string{"--a"}

	res := Resolution{
		Model: Setting{Value: strSonnet}, Effort: Setting{Value: "max"}, PlanMode: Toggle{Value: true},
		ExtraArgs: []string{"--a", "--b"},
	}

	got := effectiveProfile(p, res)
	assert.Equal(t, strSonnet, got.Model)
	assert.Equal(t, "max", got.Effort)
	assert.True(t, got.PlanMode)
	assert.Equal(t, []string{"--a", "--b"}, got.ExtraArgs)
	assert.Equal(t, profiles.PermissionFull, got.PermissionMode, "permissions stay the profile's")
}

func TestChecksAndReviewWords(t *testing.T) {
	t.Parallel()

	for state, want := range map[string]string{
		"SUCCESS": "passing", "FAILURE": "failing", "ERROR": "failing", "PENDING": "pending", "EXPECTED": "pending", "": "",
	} {
		assert.Equal(t, want, checksWord(state), state)
	}

	for decision, want := range map[string]string{
		"APPROVED": "approved", "CHANGES_REQUESTED": "changes requested", "REVIEW_REQUIRED": "review required", "": "",
	} {
		assert.Equal(t, want, reviewWord(decision), decision)
	}
}
