package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeMethodRoundTrips(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.MergeMethod(ctx)
	require.NoError(t, err)
	assert.Equal(t, MergeMethodRepo, got, "a fresh database merges as each repository offers first")

	for _, m := range []MergeMethod{MergeMethodSquash, MergeMethodMerge, MergeMethodRebase, MergeMethodRepo} {
		saved, setErr := svc.SetMergeMethod(ctx, m)
		require.NoError(t, setErr)
		assert.Equal(t, m, saved.MergeMethod, "the setter returns what it stored")

		got, err = svc.MergeMethod(ctx)
		require.NoError(t, err)
		assert.Equal(t, m, got)
	}

	_, err = svc.SetMergeMethod(ctx, "fast-forward")
	require.ErrorIs(t, err, ErrInvalidMergeMethod)

	got, err = svc.MergeMethod(ctx)
	require.NoError(t, err)
	assert.Equal(t, MergeMethodRepo, got, "a refused method stores nothing")
}

func TestMergeMethodReadsAnUnknownValueAsTheDefault(t *testing.T) {
	t.Parallel()

	assert.Equal(t, MergeMethodRepo, decodeMergeMethod("fast-forward"))
	assert.Equal(t, MergeMethodRebase, decodeMergeMethod("rebase_merge"))
}
