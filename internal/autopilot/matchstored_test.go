package autopilot_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// storedChecks is a pull request's checks as the inbox stored them once its checks
// follower read them: one check, with outcome, finished an hour ago.
func storedChecks(row facts.Row, name string, outcome facts.CheckOutcome) *facts.ChecksRow {
	return &facts.ChecksRow{Head: row.Details.HeadOID, Complete: true, Checks: []facts.CheckRow{
		{Name: name, Outcome: outcome, CompletedAt: startedAt().Add(-time.Hour)},
	}}
}

// storedItems is a fake inbox whose Opened by you list holds a pull request whose checks
// failed (7) and one whose checks passed (8), each with its checks stored, and one whose
// checks failed that the inbox hasn't read the checks of yet (9).
func storedItems() *fakeItems {
	failed, passed := failingPR(7, 70), failingPR(8, 80)
	failed.Checks = storedChecks(failed, "security", facts.CheckFailed)
	passed.Details.ChecksState, passed.Details.ChecksFailed = "SUCCESS", 0
	passed.Checks = storedChecks(passed, "security", facts.CheckPassed)

	return &fakeItems{
		listed: map[catalog.ListSource][]int64{catalog.SourceAuthored: {7, 8, 9}},
		rows:   map[int64]facts.Row{7: failed, 8: passed, 9: failingPR(9, 90)},
		owners: map[string]bool{importOrg: true},
	}
}

func TestMatchStoredCountsTheStoredItemsOfTheListsAlone(t *testing.T) {
	t.Parallel()

	items := storedItems()
	r := itemsRig(t, items)

	got, err := r.svc.MatchStored(t.Context(), draft("Failing", notifySpec()))
	require.NoError(t, err)
	assert.Equal(t, autopilot.Matching{Count: 1, PullRequests: 1, Unknown: 1}, got,
		"the failing pull request, not the passing one, and one whose checks it would have to read")

	items.mu.Lock()
	defer items.mu.Unlock()

	assert.Empty(t, items.searched, "it never asks GitHub")
	assert.Empty(t, items.read, "nor reads check names")
}

func TestMatchStoredSaysASearchIsLeftForTest(t *testing.T) {
	t.Parallel()

	items := storedItems()
	r := itemsRig(t, items)

	s := notifySpec()
	s.Watch.Sources = append(s.Watch.Sources, spec.Source{Search: &orgSearch})

	got, err := r.svc.MatchStored(t.Context(), draft("Failing", s))
	require.NoError(t, err)
	assert.Equal(t, autopilot.Matching{Count: 1, PullRequests: 1, Unknown: 1, Incomplete: true}, got,
		"the lists' count, which may be short")

	items.mu.Lock()
	defer items.mu.Unlock()

	assert.Empty(t, items.searched, "the search is never read")
}

func TestMatchStoredLeavesWhatWaitsOnAReadUnknown(t *testing.T) {
	t.Parallel()

	items := storedItems()
	r := itemsRig(t, items)

	s := notifySpec()
	s.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
		{Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"lint"}},
	}}

	got, err := r.svc.MatchStored(t.Context(), draft("Lint", s))
	require.NoError(t, err)
	assert.Equal(t, autopilot.Matching{Unknown: 1}, got,
		"no stored failure is lint's, and one pull request's check names aren't stored")

	items.mu.Lock()
	defer items.mu.Unlock()

	assert.Empty(t, items.read, "and stay unread")
}

func TestMatchStoredRefusesWhatSaveWouldAndAnInboxItCantRead(t *testing.T) {
	t.Parallel()

	r := itemsRig(t, storedItems())

	broken := notifySpec()
	broken.Steps = nil

	_, err := r.svc.MatchStored(t.Context(), draft("Nothing", broken))

	var problems *autopilot.ProblemsError
	require.ErrorAs(t, err, &problems, "each problem, as Save says them")

	_, err = newRig(t, autopilotprefs.ModeDryRun).svc.MatchStored(t.Context(), draft("Failing", notifySpec()))
	require.ErrorIs(t, err, autopilot.ErrNoItems)

	locked := storedItems()
	locked.err = errLocked

	_, err = itemsRig(t, locked).svc.MatchStored(t.Context(), draft("Failing", notifySpec()))
	require.ErrorIs(t, err, errLocked)
}
