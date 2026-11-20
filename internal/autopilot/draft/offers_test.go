package draft_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/brand"
)

func TestTheCatalogsOffersAreWhatItMarksOffered(t *testing.T) {
	t.Parallel()

	got := draft.CatalogOffers()

	for _, s := range catalog.Steps() {
		assert.Equal(t, s.Offered, contains(got.Steps, s.Kind), s.Kind)
	}

	for _, a := range catalog.GitHubActions() {
		assert.Equal(t, a.Offered, contains(got.Actions, a.Kind), a.Kind)
	}

	for _, p := range catalog.Places() {
		assert.Equal(t, p.Offered, contains(got.Places, p.ID), p.ID)
	}

	var catalogs *draft.Offers
	assert.Equal(t, got, catalogs.OrCatalog(), "nil is the catalog's own")

	mine := &draft.Offers{Steps: []catalog.StepKind{catalog.StepPin}}
	assert.Same(t, mine, mine.OrCatalog(), "a build's own are its own")
}

// contains reports whether list holds v.
func contains[T comparable](list []T, v T) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}

	return false
}

func TestAnAgentRunsOnlyWhereThisBuildRunsOne(t *testing.T) {
	t.Parallel()

	var catalogs *draft.Offers

	assert.True(t, catalogs.RunsAt(catalog.WhereBackground))
	assert.True(t, catalogs.RunsAt(catalog.WhereTerminal))
	assert.Equal(t, "In the background and In my terminal", catalogs.PlaceWords())
	assert.Equal(t, catalog.WhereBackground, catalogs.PreferredPlace(), "the background first, once this build runs agents there")

	terminal := terminalOnly()
	assert.False(t, terminal.RunsAt(catalog.WhereBackground))
	assert.Equal(t, "In my terminal", terminal.PlaceWords())
	assert.Equal(t, catalog.WhereTerminal, terminal.PreferredPlace(), "else the first place it does")

	nowhere := &draft.Offers{Places: []catalog.Where{}}
	assert.Equal(t, catalog.DefaultWhere, nowhere.PreferredPlace(), "a build that runs no agent anywhere")
	assert.Equal(t, "its steps", nowhere.PlaceWords())

	background := agentSpec(spec.PromptRef{ItemDefault: true}, catalog.WhereBackground)

	problems := draft.Problems(t.Context(), background, &setup{}, terminal)
	require.Len(t, problems, 1)
	assert.Equal(t, spec.Problem{
		Part: spec.PartSteps, Field: "[0].agent.where",
		Message: "this version of " + brand.Name + " can't run an agent in the background yet: choose In my terminal",
	}, problems[0])

	assert.Empty(t, draft.Problems(t.Context(), background, &setup{}, nil), "this build runs it there")
}

func TestATemplateNeedsWhatThisBuildCantRunOfIt(t *testing.T) {
	t.Parallel()

	sp := spec.Spec{Steps: []spec.Step{
		{Agent: &spec.AgentStep{Where: catalog.WhereBackground}},
		{Agent: &spec.AgentStep{Where: catalog.WhereBackground}},
		{WaitForChecks: &spec.WaitStep{}},
		{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}, {Reply: &spec.ReplyAction{}}}},
		{Notify: &spec.NotifyStep{}},
	}}

	var catalogs *draft.Offers
	assert.Equal(t, []string{}, catalogs.Needs(sp), "this build runs every part of it")
	assert.Empty(t, draft.NeedsWords(catalogs.Needs(sp)))

	older := &draft.Offers{
		Steps: []catalog.StepKind{catalog.StepAgent, catalog.StepGitHub, catalog.StepNotify}, Places: []catalog.Where{catalog.WhereTerminal},
		Actions: []catalog.GitHubActionKind{catalog.GitHubRebase},
	}
	assert.Equal(t, []string{"agents in the background", "“Comment one of my replies”", "“Wait for the checks”"},
		older.Needs(sp), "its place once, then each step and action it doesn't run, in the catalog's order")
	assert.Equal(t, "It needs agents in the background, “Comment one of my replies”, and “Wait for the checks”, which "+
		"this version of "+brand.Name+" can't run yet.", draft.NeedsWords(older.Needs(sp)))

	noActions := draft.CatalogOffers()
	noActions.Actions = nil

	for _, a := range catalog.GitHubActions() {
		github := spec.Spec{Steps: []spec.Step{{GitHub: []spec.GitHubAction{action(a.Kind)}}}}
		assert.Equal(t, []string{"“" + a.Label + "”"}, noActions.Needs(github), "an action by the builder's label")
	}
}

// action is a GitHub action of kind.
func action(kind catalog.GitHubActionKind) spec.GitHubAction {
	switch kind {
	case catalog.GitHubRebase:
		return spec.GitHubAction{Rebase: &spec.Empty{}}
	case catalog.GitHubUpdateBranch:
		return spec.GitHubAction{UpdateBranch: &spec.Empty{}}
	case catalog.GitHubReply:
		return spec.GitHubAction{Reply: &spec.ReplyAction{}}
	case catalog.GitHubLabels:
		return spec.GitHubAction{Labels: &spec.LabelsAction{}}
	default:
		panic("no such action: " + string(kind))
	}
}
