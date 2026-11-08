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

// testWorld is a world with two profiles, four prompts, and no launch defaults: the app
// default profile is "default", and the worktree default is on.
func testWorld() world {
	full := fullProfile()
	work := fullProfile()
	work.Name, work.Model, work.Effort, work.ExtraArgs = profileWork, strOpus, "high", []string{"--advisor", "fable"}

	return world{
		settings: LaunchSettings{DefaultProfile: profiles.DefaultName, WorktreeDefault: true},
		defaults: prompts.DefaultPrompts{PR: reviewID, Issue: workID},
		prompts: map[int64]prompts.Prompt{
			reviewID: {ID: reviewID, Nickname: "Review", Name: "Review a pull request", AppliesTo: prompts.AppliesToPR},
			quickID: {
				ID: quickID, Nickname: "Quick look", AppliesTo: prompts.AppliesToPR, Model: strSonnet, Effort: "low",
				Worktree: new(false), ExtraArgs: []string{flagDebug},
			},
			workID: {ID: workID, Nickname: "Work on it", AppliesTo: prompts.AppliesToIssue},
			bothID: {ID: bothID, Nickname: "Both", AppliesTo: prompts.AppliesToAny, PlanMode: new(true)},
		},
		profiles: map[string]profiles.Profile{profiles.DefaultName: full, profileWork: work},
		rules:    launchdefaults.NewIndex(nil),
	}
}

// prSubject is a trusted pull request in acme-org/example-repo.
func prSubject() subject {
	return subject{id: 7, kind: item.KindPR, repo: repo.Name{Owner: acmeOrg, Repo: "example-repo"}, trusted: true}
}

func TestResolveAppDefaults(t *testing.T) {
	res, profile, prompt, err := resolve(testWorld(), prSubject(), LaunchRequest{ItemID: 7})
	require.NoError(t, err)

	assert.Equal(t, profiles.DefaultName, profile.Name)
	assert.Equal(t, reviewID, prompt.ID)
	assert.Equal(t, agent.Default(), res.Agent)
	assert.Equal(t, SourceApp, res.AgentSource)
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
	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, Profile: profileWork, Model: "haiku", PlanMode: new(true), ExtraArgs: []string{"--org"}},
		{Owner: acmeOrg, Name: "example-repo", Model: strSonnet, Worktree: new(false), ExtraArgs: []string{"--repo"}},
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
	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, Name: "example-repo", Profile: "pruned", Agent: "codex"},
	})

	res, profile, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, profiles.DefaultName, profile.Name)
	assert.Equal(t, agent.Default(), res.Agent)
	assert.Equal(t, []string{
		`The repo default names a coding tool this version doesn't know ("codex"), so Claude is used.`,
		`The repo default profile "pruned" no longer exists, so it was skipped.`,
	}, res.Warnings)
}

func TestResolveProfileFallbacks(t *testing.T) {
	w := testWorld()
	w.settings.DefaultProfile = "pruned"

	res, _, _, err := resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, profiles.DefaultName, res.Profile.Value, "a missing app default falls back to default")

	delete(w.profiles, profiles.DefaultName)
	res, _, _, err = resolve(w, prSubject(), LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, profileWork, res.Profile.Value, "then to the first profile by name")

	w.profiles = map[string]profiles.Profile{}
	require.ErrorIs(t, resolveErr(w, prSubject(), LaunchRequest{}), ErrNoProfile)
}

func TestResolveRejectsBadChoices(t *testing.T) {
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
	w := testWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, ExtraArgs: []string{"--dangerously-skip-permissions"}},
	})

	err := resolveErr(w, prSubject(), LaunchRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the org launch defaults' flags")

	w = testWorld()
	quick := w.prompts[quickID]
	quick.ExtraArgs = []string{"--model=opus"}
	w.prompts[quickID] = quick

	err = resolveErr(w, prSubject(), LaunchRequest{PromptID: quickID})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `the "Quick look" prompt's flags`)
}

func TestEffectiveProfile(t *testing.T) {
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
