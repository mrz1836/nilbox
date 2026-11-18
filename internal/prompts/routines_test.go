package prompts

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// routineUses is a fixed list of where routines name prompts.
type routineUses []RoutineUse

func (r routineUses) RoutineUses(context.Context) ([]RoutineUse, error) { return r, nil }

// renames records the renames a Service tells.
type renames struct {
	mu   sync.Mutex
	told [][2]string
}

func (r *renames) renamed(_ context.Context, from, to string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.told = append(r.told, [2]string{from, to})
}

// withRoutines returns a Service over a seeded database whose routines name uses.
func withRoutines(t *testing.T, uses routineUses, told *renames) *Service {
	t.Helper()

	db := testutil.DB(t)
	cfg := Config{Store: NewStore(db, nil), Usage: launchDefaults{db: db}, Routines: uses}

	if told != nil {
		cfg.Renamed = told.renamed
	}

	s := NewService(cfg)
	require.NoError(t, Seed(t.Context(), s.store))

	return s
}

func TestUsageNamesTheRoutinesThatRunAPrompt(t *testing.T) {
	t.Parallel()

	s := withRoutines(t, routineUses{
		{Routine: "Fix my checks", Starter: "fix-checks"},
		{Routine: "Another", Nickname: "FIX CHECKS"},
		{Routine: "Fix my checks", Starter: "fix-checks"},
		{Routine: "Nothing", Nickname: "no such prompt"},
	}, nil)

	fix := byNickname(t, s, "Fix checks")
	assert.Equal(t, []string{"Another", "Fix my checks"}, fix.Usage.Routines)
	assert.True(t, fix.Usage.InUse())

	other := byNickname(t, s, "Quick look")
	assert.Equal(t, []string{}, other.Usage.Routines)
}

func TestDeleteIsRefusedWhileARoutineRunsThePrompt(t *testing.T) {
	t.Parallel()

	s := withRoutines(t, routineUses{{Routine: "Fix my checks", Starter: "fix-checks"}}, nil)
	ctx := t.Context()
	require.NoError(t, s.SetSlotDefault(ctx, DefaultSlotChecksFailing, 0), "only the routine uses it")

	fix := byNickname(t, s, "Fix checks")
	err := s.Delete(ctx, fix.ID)
	require.ErrorIs(t, err, ErrPromptInUse)
	assert.Equal(t, `"Fix checks" can't be deleted while it's used by the routine “Fix my checks”; `+
		`choose other prompts there first`, err.Error())

	inUse, ok := errors.AsType[*InUseError](err)
	require.True(t, ok)
	assert.Equal(t, []string{"Fix my checks"}, inUse.Usage.Routines)

	assert.Contains(t, nicknames(t, s), "Fix checks", "nothing was deleted")

	// A prompt no routine names deletes as before.
	quick := byNickname(t, s, "Quick look")
	require.NoError(t, s.Delete(ctx, quick.ID))
}

func TestARenameIsFollowed(t *testing.T) {
	t.Parallel()

	told := &renames{}
	s := withRoutines(t, nil, told)
	ctx := t.Context()

	mine, err := s.Create(ctx, Input{Name: "Mine", Nickname: "mine", AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)

	_, err = s.Update(ctx, mine.ID, Input{Name: "Mine", Nickname: "ours", AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)

	// Saving without a new nickname tells nothing.
	_, err = s.Update(ctx, mine.ID, Input{Name: "Ours", Nickname: "ours", AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)

	assert.Equal(t, [][2]string{{"mine", "ours"}}, told.told)
}
