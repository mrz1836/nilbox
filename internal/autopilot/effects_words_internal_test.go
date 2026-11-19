package autopilot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
)

func TestATriageStepThatChangedNothingSaysWhy(t *testing.T) {
	t.Parallel()

	for kept, want := range map[TriageKept]string{
		KeptPinned:      "It was pinned already.",
		KeptDismissed:   "It was dismissed already.",
		KeptYours:       "You set it aside yourself, so it's left as you left it.",
		KeptNotSetAside: "It wasn't snoozed or dismissed.",
		KeptChanged:     "Not done: the item changed since this matched.",
		"":              "Nothing changed.",
	} {
		assert.Equal(t, want, keptWords(kept), kept)
	}

	assert.Equal(t, "Snoozed it for 1 day.", effectWords(catalog.StepSnooze, 1))
	assert.Equal(t, "Brought it back.", effectWords(catalog.StepRestore, 0))
}

func TestActivityGroupsEachDaysLocalStepsByRoutineAndKindNewestDayFirst(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC)
	tuesday, wednesday := monday.AddDate(0, 0, 1), monday.AddDate(0, 0, 2)
	match := func(id int64, effects ...Effect) Match {
		return Match{ID: id, RoutineID: 1, Routine: "Tidy", Effects: effects, Item: ItemRef{ID: id}}
	}
	days := []ActivityDay{
		{Date: wednesday.Format(time.DateOnly), Matches: []Match{}, Groups: []ActivityGroup{}},
		{Date: monday.Format(time.DateOnly), Matches: []Match{}, Groups: []ActivityGroup{}},
	}

	got := groupLocal(days, []Match{
		match(1, Effect{ID: 11, Action: catalog.StepPin, AppliedAt: wednesday, CanUndo: true}),
		match(2, Effect{ID: 12, Action: catalog.StepPin, AppliedAt: wednesday}),
		match(3, Effect{ID: 13, Action: catalog.StepSnooze, AppliedAt: tuesday, CanUndo: true}),
		match(4, Effect{ID: 14, Action: "label", AppliedAt: monday}),
	}, time.UTC)

	require.Len(t, got, 3, "a day with steps and no match of its own gets its place")
	assert.Equal(t, []string{wednesday.Format(time.DateOnly), tuesday.Format(time.DateOnly), monday.Format(time.DateOnly)},
		[]string{got[0].Date, got[1].Date, got[2].Date})

	require.Len(t, got[0].Groups, 1)
	assert.Equal(t, "Pinned 2", got[0].Groups[0].Words)
	assert.Equal(t, []int64{1, 2}, got[0].Groups[0].Matches)

	require.Len(t, got[1].Groups, 1)
	assert.Equal(t, "Snoozed 1", got[1].Groups[0].Words)

	require.Len(t, got[2].Groups, 1)
	assert.Equal(t, "Label 1", got[2].Groups[0].Words, "a kind of step it has no verb for is named as it is")
}
