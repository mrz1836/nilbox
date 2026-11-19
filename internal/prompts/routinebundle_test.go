package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// encoded returns b in the prompts' file format.
func encoded(t *testing.T, b Bundle) []byte {
	t.Helper()

	data, err := EncodeBundle(b)
	require.NoError(t, err)

	return data
}

// bundledNicknames returns the nicknames of the prompts a file in the prompts' format holds.
func bundledNicknames(t *testing.T, data []byte) []string {
	t.Helper()

	b, err := DecodeBundle(data)
	require.NoError(t, err)

	out := make([]string, 0, len(b.Prompts))
	for _, p := range b.Prompts {
		out = append(out, p.Nickname)
	}

	return out
}

func TestBundledCarriesTheNamedPromptsAndNoDefaults(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	r := NewRoutinePrompts(s)

	data, n, err := r.Bundled(t.Context(), []string{"review", lgtmNick, "Nothing by this name"})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{lgtmNick, reviewNick}, bundledNicknames(t, data), "nicknames match ignoring case")

	b, err := DecodeBundle(data)
	require.NoError(t, err)
	assert.Equal(t, BundleDefaults{}, b.Defaults, "a routine export never carries the library's defaults")
	assert.Empty(t, b.StartersAdded, "nor its record of starters")

	data, n, err = r.Bundled(t.Context(), []string{"Nothing by this name"})
	require.NoError(t, err)
	assert.Nil(t, data, "no prompt found carries no file")
	assert.Zero(t, n)
}

func TestBundledReportsALibraryItCannotRead(t *testing.T) {
	t.Parallel()

	_, _, err := NewRoutinePrompts(closedService(t)).Bundled(t.Context(), []string{reviewNick})
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)
}

func TestArrivalsSaysWhatBringingInEachPromptWouldDo(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	r := NewRoutinePrompts(s)

	same, _, err := r.Bundled(t.Context(), []string{reviewNick})
	require.NoError(t, err)

	b, err := DecodeBundle(same)
	require.NoError(t, err)

	differs := mine("quick look")
	b.Prompts = append(b.Prompts, differs, mine("Fresh"))

	got, err := r.Arrivals(t.Context(), encoded(t, b))
	require.NoError(t, err)
	assert.Equal(t, []RoutineArrival{
		{Nickname: reviewNick, Same: true},
		{Nickname: "quick look"},
		{Nickname: "Fresh", New: true},
	}, got, "a nickname matches ignoring case, and the copy here is kept whether or not it differs")
	assert.Len(t, nicknames(t, s), len(Starters()), "saying what would happen changes nothing")
}

func TestArrivalsRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	_, err := NewRoutinePrompts(s).Arrivals(t.Context(), []byte("{ not a prompt file"))
	require.ErrorIs(t, err, ErrNotBundle)

	_, err = NewRoutinePrompts(closedService(t)).Arrivals(t.Context(), encoded(t, bundleOf(mine("Fresh"))))
	require.ErrorContains(t, err, "read the prompts")
}

func TestAddNewAddsOnlyThePromptsNoNicknameHereHas(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	r := NewRoutinePrompts(s)
	review := byNickname(t, s, reviewNick)

	taken := mine("review")
	taken.Body = "Not the review prompt here"
	data := encoded(t, bundleOf(mine("Fresh"), taken))

	added, err := r.AddNew(t.Context(), data)
	require.NoError(t, err)
	assert.Equal(t, []string{"Fresh"}, added)
	assert.Contains(t, nicknames(t, s), "Fresh")
	assert.Equal(t, review, byNickname(t, s, reviewNick), "the prompt here keeps its own text")

	added, err = r.AddNew(t.Context(), data)
	require.NoError(t, err)
	assert.Equal(t, []string{}, added, "bringing the same prompts in again adds none")
	assert.Len(t, nicknames(t, s), len(Starters())+1)
}

func TestAddNewAddsAllOfThePromptsOrNone(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	before := nicknames(t, s)

	testutil.Exec(t, db, `CREATE TRIGGER refuse_second BEFORE INSERT ON prompts WHEN NEW.nickname = 'Second'
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	added, err := NewRoutinePrompts(s).AddNew(t.Context(), encoded(t, bundleOf(mine("First"), mine("Second"))))
	require.ErrorContains(t, err, "add the routines' prompts")
	assert.Nil(t, added)
	assert.Equal(t, before, nicknames(t, s), "the first prompt rolled back with the second")
}

func TestAddNewRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	_, err := NewRoutinePrompts(s).AddNew(t.Context(), []byte(`{"format": "other", "version": 1}`))
	require.ErrorIs(t, err, ErrNotBundle)

	_, err = NewRoutinePrompts(closedService(t)).AddNew(t.Context(), encoded(t, bundleOf(mine("Fresh"))))
	require.ErrorContains(t, err, "read the prompts")
}
