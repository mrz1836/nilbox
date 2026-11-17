package settings

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
)

func TestHiddenLabelsStartAsTheSizeLabels(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"size/*"}, got.HiddenLabels)
	assert.Equal(t, []string{"size/*"}, got.DefaultHiddenLabels)
}

func TestSetHiddenLabels(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	got, err := svc.SetHiddenLabels(t.Context(), []string{" size/* ", "", "Priority: low", "SIZE/*", "wontfix"})
	require.NoError(t, err)
	assert.Equal(t, []string{"size/*", "Priority: low", "wontfix"}, got.HiddenLabels,
		"trimmed, the blank and the repeat ignoring case left out, in order")

	again, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, got.HiddenLabels, again.HiddenLabels)

	got, err = svc.SetHiddenLabels(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.HiddenLabels, "an empty list hides nothing, and isn't the defaults")

	stored, _, err := prefs.New(db, nil).Get(t.Context(), KeyHiddenLabels)
	require.NoError(t, err)
	assert.Equal(t, "[]", stored)

	got, err = svc.SetHiddenLabels(t.Context(), got.DefaultHiddenLabels)
	require.NoError(t, err)
	assert.Equal(t, DefaultHiddenLabels(), got.HiddenLabels, "Restore the defaults")
}

func TestSetHiddenLabelsRefusesAnUnboundedList(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	many := make([]string, maxHiddenLabels+1)
	for i := range many {
		many[i] = "label-" + strconv.Itoa(i)
	}

	_, err := svc.SetHiddenLabels(t.Context(), many)
	require.ErrorIs(t, err, ErrTooManyHiddenLabels)

	_, err = svc.SetHiddenLabels(t.Context(), []string{strings.Repeat("x", maxHiddenLabelLength+1)})
	require.ErrorIs(t, err, ErrHiddenLabelTooLong)

	_, err = svc.SetHiddenLabels(t.Context(), []string{"wip\n*"})
	require.ErrorIs(t, err, ErrInvalidHiddenLabel, "a line break would split it in two in the window")

	_, err = svc.SetHiddenLabels(t.Context(), []string{strings.Repeat("é", maxHiddenLabelLength)})
	require.NoError(t, err, "the length counts characters, not bytes")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{strings.Repeat("é", maxHiddenLabelLength)}, got.HiddenLabels,
		"only what the setter took is stored")
}

func TestHiddenLabelsThatCantBeReadAreTheDefaults(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{
		"not json", `{"size":1}`, `["` + strings.Repeat("x", maxHiddenLabelLength+1) + `"]`, `["wip\n*"]`,
	} {
		svc, db, _ := newTestService(t)
		setPref(t, db, KeyHiddenLabels, stored)

		got, err := svc.Get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, DefaultHiddenLabels(), got.HiddenLabels, stored)
	}
}
