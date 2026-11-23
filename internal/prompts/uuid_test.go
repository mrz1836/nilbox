package prompts

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// uuidPattern matches a UUID the store gives a prompt: a random one, in its canonical form.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// fileUUID is a prompt's UUID a file gives it, so a test can see a library keep it.
const fileUUID = "6f9619ff-8b86-4d01-b42d-00cf4fc964ff"

// uuidsByNickname lists every prompt's UUID by its nickname.
func uuidsByNickname(t *testing.T, s *Service) map[string]string {
	t.Helper()

	all, err := s.List(t.Context())
	require.NoError(t, err)

	out := make(map[string]string, len(all))
	for _, p := range all {
		out[p.Nickname] = p.UUID
	}

	return out
}

func TestEveryPromptHasAUUIDOfItsOwnThatNeverChanges(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	before := uuidsByNickname(t, s)
	seen := map[string]bool{}

	for nickname, u := range before {
		assert.Regexp(t, uuidPattern, u, nickname)
		assert.False(t, seen[u], "%s has a UUID of its own", nickname)
		seen[u] = true
	}

	made, err := s.Create(ctx, Input{Name: mineName, Nickname: mineName, AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)
	assert.Regexp(t, uuidPattern, made.UUID)
	assert.False(t, seen[made.UUID])

	review := byNickname(t, s, reviewNick)
	in := toInput(review)
	in.Nickname, in.Body = "review-it", "Look hard at {{.URL}}"

	renamed, err := s.Update(ctx, review.ID, in)
	require.NoError(t, err)
	assert.Equal(t, review.UUID, renamed.UUID, "a rename keeps the UUID")

	reset, err := s.Reset(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, reviewNick, reset.Nickname)
	assert.Equal(t, review.UUID, reset.UUID, "a reset keeps the UUID")

	dup, err := s.Duplicate(ctx, review.ID)
	require.NoError(t, err)
	assert.Regexp(t, uuidPattern, dup.UUID)
	assert.NotEqual(t, review.UUID, dup.UUID, "a copy has a UUID of its own")
}

func TestSeedGivesAPromptWithoutAUUIDOne(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	before := uuidsByNickname(t, s)

	// A build from before UUIDs inserts a prompt with none.
	_, err := db.ExecContext(t.Context(), `INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
		VALUES ('Old', 'old', 'any', 'Look at {{.URL}}', '2026-11-01 10:00:00+00:00', '2026-11-01 10:00:00+00:00')`)
	require.NoError(t, err)
	assert.Empty(t, uuidsByNickname(t, s)["old"])

	require.NoError(t, Seed(t.Context(), s.store))

	after := uuidsByNickname(t, s)
	assert.Regexp(t, uuidPattern, after["old"], "Seed gives it one")

	delete(after, "old")
	assert.Equal(t, before, after, "every other prompt keeps its own")
}

func TestAnImportKeepsAPromptsUUIDWhereNoPromptHasIt(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	p := mine(mineName)
	p.UUID = fileUUID

	_, err := s.Import(ctx, []BundlePrompt{p}, BundleDefaults{})
	require.NoError(t, err)
	assert.Equal(t, fileUUID, byNickname(t, s, mineName).UUID, "the file's UUID is free here")

	// The same UUID again, on a prompt that differs, is added under a UUID of its own.
	other := mine("Other")
	other.UUID = fileUUID

	_, err = s.Import(ctx, []BundlePrompt{other}, BundleDefaults{})
	require.NoError(t, err)

	got := byNickname(t, s, "Other").UUID
	assert.Regexp(t, uuidPattern, got)
	assert.NotEqual(t, fileUUID, got, "a UUID a prompt here has is never given twice")

	// A UUID that isn't one is dropped, and the prompt is given its own.
	odd := mine("Odd")
	odd.UUID = "not-a-uuid"

	entries := preview(t, s, bundleOf(odd))
	require.Len(t, entries, 1)
	assert.Empty(t, entries[0].Prompt.UUID)

	_, err = s.Import(ctx, []BundlePrompt{odd}, BundleDefaults{})
	require.NoError(t, err)
	assert.Regexp(t, uuidPattern, byNickname(t, s, "Odd").UUID)
}

func TestTwoPromptsOfOneFileWithOneUUIDGetTwo(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)

	a, b := mine("A"), mine("B")
	a.UUID, b.UUID = fileUUID, fileUUID

	_, err := s.Import(t.Context(), []BundlePrompt{a, b}, BundleDefaults{})
	require.NoError(t, err)

	got := uuidsByNickname(t, s)
	assert.Equal(t, fileUUID, got["A"])
	assert.Regexp(t, uuidPattern, got["B"])
	assert.NotEqual(t, fileUUID, got["B"])
}

func TestAnImportOverAStarterKeepsTheStartersUUID(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	starter := byNickname(t, s, reviewNick)

	edited := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) {
		p.Body, p.Nickname, p.UUID = "Review it my way: {{.URL}}", "my-review", fileUUID
	})

	entries := preview(t, s, bundleOf(edited))
	require.Len(t, entries, 1)
	require.Equal(t, ImportReplace, entries[0].Outcome)
	assert.Equal(t, starter.UUID, entries[0].Prompt.UUID, "the preview says which UUID it keeps")

	_, err := s.Import(t.Context(), []BundlePrompt{edited}, BundleDefaults{})
	require.NoError(t, err)

	saved := byNickname(t, s, "my-review")
	assert.Equal(t, starter.ID, saved.ID)
	assert.Equal(t, starter.UUID, saved.UUID, "the prompt saved over keeps its own UUID, renamed or not")
}

func TestAnExportCarriesEachPromptsUUIDToAnotherLibrary(t *testing.T) {
	t.Parallel()

	from, _, _ := keeping(t)
	want := uuidsByNickname(t, from)

	path := filepath.Join(t.TempDir(), "prompts.json")
	_, err := from.Export(t.Context(), path, nil)
	require.NoError(t, err)

	b, err := readBundleFile(path)
	require.NoError(t, err)

	for _, p := range b.Prompts {
		assert.Equal(t, want[p.Nickname], p.UUID, p.Nickname)
	}

	to, _ := empty(t, nil)
	_, err = to.Import(t.Context(), b.Prompts, b.Defaults)
	require.NoError(t, err)
	assert.Equal(t, want, uuidsByNickname(t, to), "every prompt keeps its UUID in a library that had none of them")
}

func TestRestoreBringsEachPromptBackUnderItsUUID(t *testing.T) {
	t.Parallel()

	from, _, dir := keeping(t)
	want := uuidsByNickname(t, from)
	require.NoError(t, from.file.Save(t.Context(), from.store))

	t.Run("from the database set aside", func(t *testing.T) {
		t.Parallel()

		s, _ := empty(t, nil)

		_, err := Restore(t.Context(), s.store, filepath.Join(dir, "app.db"), nil)
		require.NoError(t, err)
		assert.Equal(t, want, uuidsByNickname(t, s))
	})

	t.Run("from the library file", func(t *testing.T) {
		t.Parallel()

		s, _ := empty(t, nil)
		file := NewLibraryFile(filepath.Join(dir, "prompts.json"), "", nil)

		_, err := Restore(t.Context(), s.store, "", file)
		require.NoError(t, err)
		assert.Equal(t, want, uuidsByNickname(t, s))
	})
}

func TestRestoreGivesAPromptFromAnOlderDatabaseAUUID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old.db")
	writeLibraryDB(t, path, mine(mineName))

	old := testutil.DBAt(t, path)
	_, err := old.ExecContext(t.Context(), `UPDATE prompts SET uuid = ''`)
	require.NoError(t, err)
	require.NoError(t, old.Close())

	s, _ := empty(t, nil)

	_, err = Restore(t.Context(), s.store, path, nil)
	require.NoError(t, err)
	assert.Regexp(t, uuidPattern, byNickname(t, s, mineName).UUID)
}

func TestABackupReplaceKeepsTheUUIDsOfThePromptsItMatches(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	review := byNickname(t, s, reviewNick)

	// The file's Review has another UUID: Replace matches it by nickname and saves over the
	// one here, which keeps its own.
	again := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) {
		p.Body, p.UUID = "Review it again: {{.URL}}", "0b1c2d3e-4f50-4a61-8b72-93a4b5c6d7e8"
	})
	added := mine(mineName)
	added.UUID = fileUUID

	plan, _, _ := applyBundle(t, s, db, bundleOf(again, added), backup.ModeReplace, true)
	assert.Contains(t, items(plan.Plan), "update "+reviewNick)

	got := uuidsByNickname(t, s)
	assert.Equal(t, review.UUID, got[reviewNick], "the prompt saved over keeps its UUID")
	assert.Equal(t, fileUUID, got[mineName], "a prompt added keeps the file's UUID, which no prompt here has")
}

func TestARoutinesPromptHereUnderItsUUIDIsNeverAddedAgain(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()
	review := byNickname(t, s, reviewNick)

	// The export names Review by the UUID it has here, under a nickname it no longer has.
	exported := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) {
		p.Nickname, p.UUID = "old-review", review.UUID
	})
	fresh := mine(mineName)
	data := encoded(t, bundleOf(exported, fresh))

	r := NewRoutinePrompts(s)

	arrivals, err := r.Arrivals(ctx, data)
	require.NoError(t, err)
	assert.Equal(t, []RoutineArrival{
		{Nickname: "old-review", Same: true},
		{Nickname: mineName, New: true},
	}, arrivals, "a prompt here under its UUID is the same, whatever either is called")

	added, err := r.AddNew(ctx, data)
	require.NoError(t, err)
	assert.Equal(t, []string{mineName}, added)
	assert.NotContains(t, nicknames(t, s), "old-review")
}
