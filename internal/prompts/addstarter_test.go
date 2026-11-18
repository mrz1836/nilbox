package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddStarterBringsBackADeletedStarter checks that a starter the user deleted, which
// Seed never adds back, comes back on AddStarter, once, as the build ships it.
func TestAddStarterBringsBackADeletedStarter(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	gone := byNickname(t, s, "Resolve conflicts")
	require.NoError(t, s.SetSlotDefault(ctx, DefaultSlotConflicts, 0))
	require.NoError(t, s.Delete(ctx, gone.ID))
	require.NoError(t, Seed(ctx, s.store))
	assert.NotContains(t, nicknames(t, s), "Resolve conflicts", "Seed leaves a deleted starter deleted")

	back, err := s.AddStarter(ctx, StarterResolveConflicts)
	require.NoError(t, err)
	assert.Equal(t, StarterResolveConflicts, back.StarterKey)
	assert.Equal(t, "Resolve conflicts", back.Nickname)
	assert.Equal(t, resolveConflictsBody, back.Body)
	assert.False(t, back.Modified)

	again, err := s.AddStarter(ctx, StarterResolveConflicts)
	require.NoError(t, err)
	assert.Equal(t, back.ID, again.ID, "a starter the library holds isn't added twice")

	require.NoError(t, Seed(ctx, s.store))

	count := 0

	for _, n := range nicknames(t, s) {
		if n == "Resolve conflicts" {
			count++
		}
	}

	assert.Equal(t, 1, count, "Seed adds no second copy after it")
}

// TestAddStarterKeepsAnEditedCopy checks that a starter the library still holds, edited or
// not, is the one AddStarter returns, untouched.
func TestAddStarterKeepsAnEditedCopy(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	heal := byNickname(t, s, "Heal Dependabot PR")
	in := toInput(heal)
	in.Body = "Heal {{.URL}} gently."

	_, err := s.Update(ctx, heal.ID, in)
	require.NoError(t, err)

	got, err := s.AddStarter(ctx, StarterHealDependabot)
	require.NoError(t, err)
	assert.Equal(t, heal.ID, got.ID)
	assert.Equal(t, "Heal {{.URL}} gently.", got.Body)
}

// TestAddStarterRefusesAnUnknownKey checks that only a starter this build ships is added.
func TestAddStarterRefusesAnUnknownKey(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	_, err := s.AddStarter(t.Context(), "a-later-starter")
	require.ErrorIs(t, err, ErrUnknownStarter)
}
