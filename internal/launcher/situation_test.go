package launcher

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/trust"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Prompts the situation tests add to testWorld: one for fixing, and one for issues alone.
const (
	fixID   int64 = 5
	replyID int64 = 6
)

// situationWorld is testWorld with a prompt for fixing checks added.
func situationWorld() world {
	w := testWorld()
	w.prompts[fixID] = prompts.Prompt{ID: fixID, Nickname: "Fix checks", Name: "Fix the checks", AppliesTo: prompts.AppliesToPR}

	return w
}

// withSituation returns d naming id as its prompt for s.
func withSituation(d launchdefaults.Defaults, s Situation, id *int64) launchdefaults.Defaults {
	switch s {
	case SituationChecksFailing:
		d.ChecksPromptID = id
	case SituationConflicts:
		d.ConflictsPromptID = id
	case SituationChangesRequested:
		d.ChangesPromptID = id
	case SituationRereview:
		d.RereviewPromptID = id
	case SituationNone:
	}

	return d
}

// withAppSituation returns d with id as the app's prompt for s.
func withAppSituation(d prompts.DefaultPrompts, s Situation, id int64) prompts.DefaultPrompts {
	switch s {
	case SituationChecksFailing:
		d.ChecksFailing = id
	case SituationConflicts:
		d.Conflicts = id
	case SituationChangesRequested:
		d.ChangesRequested = id
	case SituationRereview:
		d.Rereview = id
	case SituationNone:
	}

	return d
}

func TestEverySituationHasTheLibrarysSlotOfTheSameName(t *testing.T) {
	t.Parallel()

	require.Len(t, situations(), 4)
	assert.NotContains(t, situations(), SituationNone)

	slots := make([]prompts.DefaultSlot, 0, len(situations()))

	for _, s := range situations() {
		assert.Contains(t, prompts.DefaultSlots(), prompts.DefaultSlot(s), s)
		assert.NotEmpty(t, s.when(), s)
		slots = append(slots, s.slot())
	}

	// The library lists the situations' slots in the same order, after the kinds'.
	assert.Equal(t, slots, prompts.DefaultSlots()[2:6])
}

// TestResolveASituationsPrompt resolves a launch for each situation through every layer:
// the repository's prompt for it, the org's, the app's, and with none of them, exactly the
// pull request's own, and skips one that is gone or isn't for pull requests.
func TestResolveASituationsPrompt(t *testing.T) {
	t.Parallel()

	missing, issueOnly := new(missingID), new(workID)

	for _, s := range situations() {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()

			skipped := func(source Source) string {
				return "The " + string(source) + " default prompt " + s.when() + " no longer exists, so it was skipped."
			}

			tests := []struct {
				name string

				// org and repo are the situation's prompt in the org's and the repository's
				// defaults, orgPR the org's prompt for pull requests, and app the app's for
				// the situation.
				org, repo, orgPR *int64
				app              int64

				want     int64
				source   Source
				warnings []string
			}{
				{name: "the repository's", org: new(quickID), repo: new(bothID), app: fixID, want: bothID, source: SourceRepo},
				{name: "the org's when the repository inherits", org: new(quickID), app: fixID, want: quickID, source: SourceOrg},
				{name: "the app's when both inherit", app: fixID, want: fixID, source: SourceApp},
				{
					name: "the app's over the org's prompt for pull requests", orgPR: new(bothID), app: fixID, want: fixID,
					source: SourceApp,
				},
				{name: "none set: the org's prompt for pull requests", orgPR: new(bothID), want: bothID, source: SourceOrg},
				{name: "none set at all: the app's prompt for pull requests", want: reviewID, source: SourceApp},
				{
					name: "a prompt that is gone is skipped", repo: missing, org: new(quickID), want: quickID, source: SourceOrg,
					warnings: []string{skipped(SourceRepo)},
				},
				{
					name: "a reply is skipped", org: new(replyID), app: fixID, want: fixID, source: SourceApp,
					warnings: []string{skipped(SourceOrg)},
				},
				{
					name: "a prompt for issues is skipped, then the pull request's", repo: issueOnly, want: reviewID,
					source: SourceApp, warnings: []string{skipped(SourceRepo)},
				},
			}

			for _, tt := range tests {
				w := situationWorld()
				w.defaults = withAppSituation(w.defaults, s, tt.app)
				w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
					withSituation(launchdefaults.Defaults{Owner: acmeOrg, PRPromptID: tt.orgPR}, s, tt.org),
					withSituation(launchdefaults.Defaults{Owner: acmeOrg, Name: exampleRepo}, s, tt.repo),
				})

				res, _, prompt, err := resolve(w, prSubject(), LaunchRequest{Situation: s})
				require.NoError(t, err, tt.name)
				assert.Equal(t, tt.want, prompt.ID, tt.name)
				assert.Equal(t, PromptChoice{
					ID: tt.want, Nickname: w.prompts[tt.want].Nickname, Name: w.prompts[tt.want].Name, Source: tt.source,
					InheritedID: tt.want, InheritedSource: tt.source,
				}, res.Prompt, tt.name)
				assert.Equal(t, textOr(tt.warnings), res.Warnings, tt.name)

				if tt.org == nil && tt.repo == nil && tt.app == 0 {
					plain, _, _, plainErr := resolve(w, prSubject(), LaunchRequest{})
					require.NoError(t, plainErr)
					assert.Equal(t, plain.Prompt, res.Prompt, "%s: exactly the pull request's own choice", tt.name)
				}
			}
		})
	}
}

// textOr returns list, or an empty list for nil, as Resolution.Warnings holds.
func textOr(list []string) []string {
	if list == nil {
		return []string{}
	}

	return list
}

func TestASituationsPromptBringsItsSettingsAndYieldsToTheLaunchsOwn(t *testing.T) {
	t.Parallel()

	w := situationWorld()
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, Name: exampleRepo, ChecksPromptID: new(quickID)},
	})

	res, _, _, err := resolve(w, prSubject(), LaunchRequest{Situation: SituationChecksFailing})
	require.NoError(t, err)
	assert.Equal(t, quickID, res.Prompt.ID)
	assert.Equal(t, Setting{Value: strSonnet, Source: SourcePrompt, Inherited: strSonnet, InheritedSource: SourcePrompt},
		res.Model, "the model the situation's prompt brings")

	low := w.prompts[quickID].Effort
	assert.Equal(t, Setting{Value: low, Source: SourcePrompt, Inherited: low, InheritedSource: SourcePrompt}, res.Effort)
	assert.Equal(t, Toggle{Source: SourcePrompt, InheritedSource: SourcePrompt}, res.Worktree)
	assert.Equal(t, []string{flagDebug}, res.ExtraArgs)

	res, _, prompt, err := resolve(w, prSubject(), LaunchRequest{Situation: SituationChecksFailing, PromptID: bothID})
	require.NoError(t, err)
	assert.Equal(t, bothID, prompt.ID, "a prompt the launch chooses wins")
	assert.Equal(t, PromptChoice{
		ID: bothID, Nickname: "Both", Source: SourceLaunch, InheritedID: quickID, InheritedSource: SourceRepo,
	}, res.Prompt, "and the situation's is what it would have had")

	res, _, _, err = resolve(w, prSubject(), LaunchRequest{Situation: SituationConflicts})
	require.NoError(t, err)
	assert.Equal(t, reviewID, res.Prompt.ID, "another situation's prompt is not this one's")
}

func TestASituationOnAnIssueIsIgnored(t *testing.T) {
	t.Parallel()

	w := situationWorld()
	w.defaults.ChecksFailing = fixID
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{{Owner: acmeOrg, ChecksPromptID: new(bothID)}})

	issue := subject{id: 3, kind: item.KindIssue, repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}}

	res, _, prompt, err := resolve(w, issue, LaunchRequest{Situation: SituationChecksFailing})
	require.NoError(t, err)
	assert.Equal(t, workID, prompt.ID, "an issue launches with its own prompt")
	assert.Equal(t, SourceApp, res.Prompt.Source)
	assert.Empty(t, res.Warnings)
}

func TestAnUnknownSituationIsRefused(t *testing.T) {
	t.Parallel()

	issue := subject{id: 3, kind: item.KindIssue, repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}}

	for _, it := range []subject{prSubject(), issue} {
		err := resolveErr(situationWorld(), it, LaunchRequest{Situation: "flaky_tests"})
		require.ErrorIs(t, err, ErrUnknownSituation)
		assert.Contains(t, err.Error(), `"flaky_tests"`, "the log says which")
		assert.Equal(t, "This version doesn't know that situation, so it can't choose its prompt.", uierr.Message(err))
	}
}

func TestResolutionNamesEachSituationsPrompt(t *testing.T) {
	t.Parallel()

	w := situationWorld()
	w.defaults.ChecksFailing, w.defaults.Rereview = fixID, missingID
	w.rules = launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acmeOrg, PRPromptID: new(bothID), ConflictsPromptID: new(quickID)},
		{Owner: acmeOrg, Name: exampleRepo, ChangesPromptID: new(missingID)},
	})

	res, _, _, err := resolve(w, prSubject(), LaunchRequest{PromptID: quickID})
	require.NoError(t, err)
	assert.Equal(t, []SituationChoice{
		{Situation: SituationChecksFailing, Prompt: PromptChoice{
			ID: fixID, Nickname: "Fix checks", Name: "Fix the checks", Source: SourceApp, InheritedID: fixID,
			InheritedSource: SourceApp,
		}},
		{Situation: SituationConflicts, Prompt: PromptChoice{
			ID: quickID, Nickname: "Quick look", Source: SourceOrg, InheritedID: quickID, InheritedSource: SourceOrg,
		}},
		{Situation: SituationChangesRequested, Prompt: PromptChoice{
			ID: bothID, Nickname: "Both", Source: SourceOrg, InheritedID: bothID, InheritedSource: SourceOrg,
		}},
		{Situation: SituationRereview, Prompt: PromptChoice{
			ID: bothID, Nickname: "Both", Source: SourceOrg, InheritedID: bothID, InheritedSource: SourceOrg,
		}},
	}, res.Situations, "each without the launch's own choice, the pull request's where a situation has none")
	assert.Empty(t, res.Warnings, "a situation the launch isn't for warns of nothing")

	issue := subject{id: 3, kind: item.KindIssue, repo: repo.Name{Owner: acmeOrg, Repo: exampleRepo}}
	res, _, _, err = resolve(w, issue, LaunchRequest{})
	require.NoError(t, err)
	assert.Equal(t, []SituationChoice{}, res.Situations, "an issue has none, and the window gets [] rather than null")
}

func TestPromptDataHoldsOnlyACommitAsTheReviewedCommit(t *testing.T) {
	t.Parallel()

	const full = "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d"

	tests := []struct {
		name, stored, want string
	}{
		{name: "a full commit", stored: full, want: full},
		{name: "upper case", stored: "9E8D7C6B5A4F3E2D1C0B9A8F7E6D5C4B3A2F1E0D", want: full},
		{name: "an abbreviated commit", stored: "9e8d7c6", want: "9e8d7c6"},
		{name: "too short", stored: "9e8d7c", want: ""},
		{name: "not hex", stored: "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0z", want: ""},
		{name: "a branch name", stored: "main", want: ""},
		{name: "a command", stored: "x;$(id)", want: ""},
		{name: "none", stored: "", want: ""},
	}

	for _, tt := range tests {
		row := launchItemRow{
			Kind: item.KindPR, Number: 7, Owner: acmeOrg, Name: exampleRepo, ViewerReviewCommit: tt.stored,
		}

		d := row.promptData(trust.NewPolicy(nil), "default", "octo-user", nil)
		assert.Equal(t, tt.want, d.ReviewedCommit, tt.name)
	}

	issue := launchItemRow{Kind: item.KindIssue, Number: 3, ViewerReviewCommit: full}
	assert.Empty(t, issue.promptData(trust.NewPolicy(nil), "default", "octo-user", nil).ReviewedCommit,
		"an issue has no reviewed commit")
}

func TestASituationsPromptResolvesFromTheStoredDefaults(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	ctx := t.Context()

	res, err := h.service().Resolve(ctx, LaunchRequest{ItemID: h.trustedPR, Situation: SituationChecksFailing})
	require.NoError(t, err)
	assert.Equal(t, "Fix checks", res.Prompt.Nickname, "the app's prompt when checks fail starts on its starter")
	assert.Equal(t, SourceApp, res.Prompt.Source)

	quick := new(h.promptID(t, "Quick look"))
	h.saveDefaults(t, launchdefaults.Input{Owner: acmeOrg, Name: exampleRepo, ChecksPromptID: quick})

	res, err = h.service().Resolve(ctx, LaunchRequest{ItemID: h.trustedPR, Situation: SituationChecksFailing})
	require.NoError(t, err)
	assert.Equal(t, "Quick look", res.Prompt.Nickname)
	assert.Equal(t, SourceRepo, res.Prompt.Source)

	got, err := h.service().ResolveItems(ctx, []int64{h.trustedPR, h.issue})
	require.NoError(t, err)
	require.Len(t, got, 2)

	for _, r := range got {
		nicknames := make([]string, 0, len(r.Situations))
		for _, c := range r.Situations {
			nicknames = append(nicknames, c.Prompt.Nickname)
		}

		if r.Kind == item.KindIssue {
			assert.Empty(t, r.Situations)
			assert.NotNil(t, r.Situations)

			continue
		}

		assert.Equal(t, []string{"Quick look", "Resolve conflicts", "Address review", "Re-review"}, nicknames)
		assert.True(t, slices.IsSortedFunc(r.Situations, func(a, b SituationChoice) int {
			return slices.Index(situations(), a.Situation) - slices.Index(situations(), b.Situation)
		}))
	}

	res2, script := h.launch(t, LaunchRequest{ItemID: h.trustedPR, Situation: SituationRereview})
	assert.Equal(t, "Re-review", res2.Prompt)
	assert.Contains(t, script, "'--name=Re-review · example-repo#7'")
	assert.Contains(t, promptFor(t, res2.ScriptPath), "Review pull request #7 in acme-org/example-repo again")
}
