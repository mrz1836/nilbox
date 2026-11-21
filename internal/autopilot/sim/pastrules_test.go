package sim_test

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// A routine with no agent step that fires when the checks pass merges the commit they
// passed on, past the rules where an approval from someone else is all it lacks only with
// every consent the user gave: the step's tick, their acceptance, and the switch.

// mergeOnPass merges each watched pull request whose checks pass, squashed, past the rules
// where its step allows it.
func mergeOnPass(bypass bool) spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{sim.Search()}},
		Trigger: spec.Trigger{Event: catalog.EventChecksPassed},
		Steps: []spec.Step{{Merge: &spec.MergeStep{
			How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash, AllowBypass: bypass,
		}}},
	}
}

// mergingOnPass starts a sim whose #1's checks passed, the switch for merging past the
// rules as on says, with mergeOnPass's routine on Autopilot, accepted, switched on, and
// returns what the engine asked the review sheet to merge.
func mergingOnPass(t *testing.T, on bool) []autopilot.MergeSend {
	t.Helper()

	s := sim.New(t)
	s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
	s.Begin()
	s.Change(func(c *autopilotprefs.Config) { c.MergePastRules = on })

	r, err := s.Service.Save(t.Context(), autopilot.Draft{
		Name: "Merge on a pass", Spec: mergeOnPass(true), Mode: autopilotprefs.ModeAuto, AcceptPastRules: true,
	})
	require.NoError(t, err)
	require.Equal(t, autopilot.PastRulesAllowed, r.PastRules)

	s.Enable(r.ID, autopilot.CurrentRun)

	for range 3 {
		s.Refresh()
	}

	return s.GitHub.MergesTried()
}

func TestAnAgentlessMergeGoesPastTheRulesWithEveryConsent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tried := mergingOnPass(t, true)
		require.Len(t, tried, 1)

		assert.Equal(t, sim.Commit(1, 1), tried[0].Head, "the commit its checks passed on")
		assert.True(t, tried[0].PastReview)
		assert.False(t, tried[0].Sensitive)
		assert.Empty(t, tried[0].PastReviewWhy)
		assert.Equal(t, catalog.MergeSquash, tried[0].Method)
	})
}

func TestAnAgentlessMergeSaysWhyNotWhileTheSwitchIsOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		tried := mergingOnPass(t, false)
		require.Len(t, tried, 1)

		assert.False(t, tried[0].PastReview)
		assert.Contains(t, tried[0].PastReviewWhy, "is off in Autopilot's settings")
	})
}

func TestAnAgentlessMergeAfterAnotherStepIsDecidedAgainFirst(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := mergeOnPass(false)
		sp.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{{
			Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: []string{"automerge"},
		}}}
		sp.Steps = append([]spec.Step{{GitHub: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{"queued"}}}}}},
			sp.Steps...)

		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4, Labels: []string{"automerge"}})
		s.Begin()

		// The label is taken off on GitHub as the first step goes.
		s.GitHub.OnSend(func(autopilot.GitHubSend) {
			s.World.Change(1, func(it *sim.Item) { it.Labels = []string{} })
		})

		r := s.Save("Merge on a pass", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)

		for range 4 {
			s.Refresh()
		}

		require.Len(t, s.GitHub.Jobs(), 1, "the first step went")
		assert.Empty(t, s.GitHub.MergesTried(), "the merge never went")

		m := only(t, s)
		assert.Equal(t, autopilot.StateClosed, m.State, m.Words)
		assert.Equal(t, autopilot.ReasonNoLongerMatches, m.Reason)
		assert.Contains(t, m.Words, "It no longer matches")
	})
}

func TestAnAgentlessMergeThatDoesntGoPastTheRulesMergesWhatGitHubAllows(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, mergeOnPass(false), autopilotprefs.ModeAuto)
		s.Refresh()
		s.Refresh()

		merges := s.GitHub.Merges()
		require.Len(t, merges, 1)
		assert.Equal(t, sim.Commit(1, 1), merges[0].Head)
		assert.False(t, merges[0].PastReview)
		assert.Empty(t, merges[0].PastReviewWhy)
	})
}
