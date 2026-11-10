package prompts

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// mineName names a prompt the user wrote.
const mineName = "Mine"

// exportedAt is when the transfer tests' files are written.
func exportedAt() time.Time { return time.Date(2026, time.November, 8, 17, 0, 0, 0, time.UTC) }

// keeping returns a Service over a database holding the starter prompts that keeps its
// library file, and the database, in a directory of their own: dir/app.db and
// dir/prompts.json.
func keeping(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()

	dir := t.TempDir()
	db := testutil.DBAt(t, filepath.Join(dir, "app.db"))
	st := NewStore(db, nil)
	require.NoError(t, Seed(t.Context(), st))

	s := NewService(Config{
		Store:    st,
		Usage:    launchDefaults{db: db},
		File:     NewLibraryFile(filepath.Join(dir, "prompts.json"), "v1.2.3", testutil.NewClock(exportedAt())),
		Database: filepath.Join(dir, "app.db"),
		App:      "v1.2.3",
		Clock:    testutil.NewClock(exportedAt()),
	})

	return s, db, dir
}

// bundleOf returns a prompt file holding prompts.
func bundleOf(prompts ...BundlePrompt) Bundle {
	return Bundle{Format: BundleFormat, Version: BundleVersion, StartersAdded: []string{}, Prompts: prompts}
}

// writeBundle writes b to a file in a new directory and returns its path.
func writeBundle(t *testing.T, b Bundle) string {
	t.Helper()

	data, err := encodeBundle(b)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "shared.json")
	testutil.WriteFile(t, path, string(data))

	return path
}

// preview previews importing b into s.
func preview(t *testing.T, s *Service, b Bundle) []ImportEntry {
	t.Helper()

	entries, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: writeBundle(t, b)})
	require.NoError(t, err)

	return entries
}

// outcomes lists each entry's outcome and the nickname it would be saved under.
func outcomes(entries []ImportEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, string(e.Outcome)+" "+e.Nickname)
	}

	return out
}

// starterAsBundled returns a starter in portable form, edited by edit.
func starterAsBundled(t *testing.T, key string, edit func(*BundlePrompt)) BundlePrompt {
	t.Helper()

	st, ok := starterFor(key)
	require.True(t, ok, key)

	p := BundlePrompt{
		Name: st.Name, Nickname: st.Nickname, AppliesTo: st.AppliesTo, Body: st.Body,
		Models: agent.CloneModels(st.Models), Effort: st.Effort, PlanMode: st.PlanMode, Worktree: st.Worktree,
		ExtraArgs: agent.CloneArgs(st.ExtraArgs), StarterKey: key,
	}

	if edit != nil {
		edit(&p)
	}

	return p
}

// toInput returns a stored prompt as the editor would save it unchanged.
func toInput(p Prompt) Input {
	return Input{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: p.AppliesTo, Body: p.Body, Models: p.Models, Effort: p.Effort,
		PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraFlags: p.ExtraFlags,
	}
}

// mine is a prompt the user wrote.
func mine(nickname string) BundlePrompt {
	return BundlePrompt{
		Name: nickname + " prompt", Nickname: nickname, AppliesTo: AppliesToAny, Body: "Look at {{.URL}} for " + nickname,
		Models: map[agent.ID]string{}, ExtraArgs: map[agent.ID][]string{},
	}
}

func TestBundleRoundTrips(t *testing.T) {
	t.Parallel()

	on := true
	b := bundleOf(BundlePrompt{
		Name: "Security <pass> & more", Nickname: "Security", AppliesTo: AppliesToAny, Body: "Check {{.URL}} for <script>",
		Models: map[agent.ID]string{agent.Claude: "opus", agent.Codex: "gpt-6-sol"}, Effort: "high", PlanMode: &on,
		ExtraArgs: map[agent.ID][]string{agent.Claude: {"--verbose"}, agent.Codex: {"--search"}}, StarterKey: "",
		CreatedAt: "2026-11-07T09:00:00Z", UpdatedAt: "2026-11-07T09:30:00Z",
	})
	b.ExportedAt, b.App = "2026-11-08T17:00:00Z", "v1.2.3"
	b.Defaults = BundleDefaults{PR: "Security"}

	data, err := encodeBundle(b)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<script>", "a prompt's text reads in the file as it was written")
	assert.True(t, strings.HasSuffix(string(data), "}\n"))

	got, err := DecodeBundle(data)
	require.NoError(t, err)
	assert.Equal(t, b, got)
}

func TestDecodeBundleRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	many := make([]string, maxBundlePrompts+1)
	for i := range many {
		many[i] = `{"name":"x","nickname":"x","appliesTo":"any","body":"x"}`
	}

	for name, tt := range map[string]struct {
		data string
		want error
	}{
		"not JSON":          {data: "{ not json", want: ErrNotBundle},
		"a list":            {data: `[{"name":"x"}]`, want: ErrNotBundle},
		"another format":    {data: `{"format":"other.prompts","version":1,"prompts":[]}`, want: ErrNotBundle},
		"no version":        {data: `{"format":"` + BundleFormat + `","prompts":[]}`, want: ErrNotBundle},
		"a newer version":   {data: `{"format":"` + BundleFormat + `","version":4,"prompts":[]}`, want: ErrNewerBundle},
		"bad extra args":    {data: `{"format":"` + BundleFormat + `","version":2,"prompts":[{"extraArgs":"--x"}]}`, want: ErrNotBundle},
		"too many prompts":  {data: `{"format":"` + BundleFormat + `","version":1,"prompts":[` + strings.Join(many, ",") + `]}`, want: ErrBundleTooLarge},
		"too large to read": {data: strings.Repeat(" ", maxBundleBytes+1), want: ErrBundleTooLarge},
	} {
		_, err := DecodeBundle([]byte(tt.data))
		require.ErrorIs(t, err, tt.want, name)
	}
}

func TestDecodeBundleReadsAFileItDoesNotKnowEveryFieldOf(t *testing.T) {
	t.Parallel()

	got, err := DecodeBundle([]byte(`{
		"format": "` + BundleFormat + `", "version": 1, "comingSoon": {"x": 1},
		"prompts": [{"name": "Hand written", "nickname": "Hand", "appliesTo": "pr", "body": "Look at {{.URL}}", "tags": ["a"]}]
	}`))
	require.NoError(t, err)
	require.Len(t, got.Prompts, 1)
	assert.Equal(t, "Hand written", got.Prompts[0].Name)
	assert.Equal(t, map[agent.ID][]string{}, got.Prompts[0].ExtraArgs, "a list left out reads as empty")
	assert.Equal(t, map[agent.ID]string{}, got.Prompts[0].Models)
	assert.Equal(t, []string{}, got.StartersAdded)
}

func TestExportWritesTheWholeLibrary(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)
	path := filepath.Join(dir, "export", "all.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), appdata.PrivateDirPerm))

	n, err := s.Export(t.Context(), path, nil)
	require.NoError(t, err)
	assert.Equal(t, len(Starters()), n)

	got, err := readBundleFile(path)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-08T17:00:00Z", got.ExportedAt)
	assert.Equal(t, "v1.2.3", got.App)
	assert.Equal(t, BundleDefaults{PR: "Review", Issue: "Work on it", Replies: map[ReplySlot]string{
		SlotApprove: "LGTM", SlotApproveBots: "Update OK", SlotRequestChanges: "Needs changes", SlotComment: "Question",
	}}, got.Defaults)
	assert.ElementsMatch(t, []string{
		StarterReviewPR, StarterWorkOnIssue, StarterQuickLook, StarterFixChecks,
		StarterReplyLGTM, StarterReplyUpdateOK, StarterReplyNeedsChanges, StarterReplyQuestion,
	}, got.StartersAdded)
	assert.Len(t, got.Prompts, len(Starters()))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateFilePerm, info.Mode().Perm())

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")
}

func TestExportWritesTheChosenPrompts(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)
	quick := byNickname(t, s, "Quick look")
	path := filepath.Join(dir, "one.json")

	n, err := s.Export(t.Context(), path, []int64{quick.ID})
	require.NoError(t, err)
	assert.Equal(t, 1, n)

	got, err := readBundleFile(path)
	require.NoError(t, err)
	require.Len(t, got.Prompts, 1)
	assert.Equal(t, "Quick look", got.Prompts[0].Nickname)
	assert.Equal(t, BundleDefaults{}, got.Defaults, "the defaults are left out when they are not among them")
	assert.Empty(t, got.StartersAdded, "the record of starters belongs to a whole library")
}

func TestExportRefusesWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)

	_, err := s.Export(t.Context(), "relative.json", nil)
	require.ErrorIs(t, err, ErrNoPath)

	_, err = s.Export(t.Context(), filepath.Join(dir, "x.json"), []int64{999})
	require.ErrorIs(t, err, ErrPromptNotFound)

	_, err = s.Export(t.Context(), filepath.Join(dir, "missing", "x.json"), nil)
	require.ErrorContains(t, err, "export the prompts")
}

func TestPreviewImportSaysWhatEachPromptWouldDo(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	entries := preview(t, s, bundleOf(
		// The user's own edit of a starter replaces the starter still as it was added.
		starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) { p.Body = "Review {{.URL}} my way." }),
		// A starter as it was added is in the library already.
		starterAsBundled(t, StarterQuickLook, nil),
		// A prompt with the same text and settings as one in the library, under another
		// nickname, is in the library already.
		starterAsBundled(t, StarterFixChecks, func(p *BundlePrompt) { p.Nickname, p.StarterKey = "Checks", "" }),
		// A new prompt keeps its nickname, or gets a free one.
		mine("Security"),
		mine("Work on it"),
		mine("Security"),
	))

	assert.Equal(t, []string{
		"replace Review", "skip Quick look", "skip Fix checks", "add Security", "rename Work on it 2", "skip Security",
	}, outcomes(entries))
	assert.Equal(t, "Review", entries[0].Replaces)

	// A preview changes nothing.
	assert.NotEqual(t, "Review {{.URL}} my way.", byNickname(t, s, "Review").Body)
	assert.Len(t, nicknames(t, s), len(Starters()))
}

func TestPreviewImportRenamesWithinOneFile(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	other := mine("Security")
	other.Body = "Another text for {{.URL}}"

	assert.Equal(t, []string{"add Security", "rename Security 2"},
		outcomes(preview(t, s, bundleOf(mine("Security"), other))))
}

func TestImportAddsAStarterEditOverAnEditedStarterAsTheUsersOwn(t *testing.T) {
	t.Parallel()

	s, db, _ := keeping(t)
	review := byNickname(t, s, "Review")
	in := toInput(review)
	in.Body = "My review of {{.URL}}"
	_, err := s.Update(t.Context(), review.ID, in)
	require.NoError(t, err)

	edit := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) { p.Body = "Their review of {{.URL}}" })

	assert.Equal(t, []string{"rename Review 2"}, outcomes(preview(t, s, bundleOf(edit))),
		"a starter the user edited is never saved over")

	res, err := s.Import(t.Context(), []BundlePrompt{edit})
	require.NoError(t, err)
	assert.Equal(t, ImportResult{Added: 1, First: res.First}, res)

	added := byNickname(t, s, "Review 2")
	assert.Empty(t, added.StarterKey, "a library holds one prompt per starter, so the second is the user's own")
	assert.Equal(t, "My review of {{.URL}}", byNickname(t, s, "Review").Body)
	assert.Equal(t, "My review of {{.URL}}", promptBody(t, db, review.ID))
}

// promptBody reads a prompt's text straight from the database.
func promptBody(t *testing.T, db *sql.DB, id int64) string {
	t.Helper()

	var body string
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT body FROM prompts WHERE id = ?", id).Scan(&body))

	return body
}

func TestImportNeverSavesOverADefaultWithAPromptThatCannotBeOne(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	// The review starter is the default for pull requests, so a version of it for issues
	// only cannot take its place.
	forIssues := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) {
		p.AppliesTo, p.Body = AppliesToIssue, "Review issue {{.URL}}"
	})

	assert.Equal(t, []string{"rename Review 2"}, outcomes(preview(t, s, bundleOf(forIssues))))
}

func TestImportStoresWhatThePreviewSaid(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)
	review := byNickname(t, s, "Review")

	edit := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) {
		p.Nickname, p.Body = "Review (mine)", "Review {{.URL}} my way."
	})
	res, err := s.Import(t.Context(), []BundlePrompt{edit, mine("Security"), starterAsBundled(t, StarterQuickLook, nil)})
	require.NoError(t, err)
	assert.Equal(t, ImportResult{Added: 1, Replaced: 1, Skipped: 1, First: review.ID}, res)

	replaced := byNickname(t, s, "Review (mine)")
	assert.Equal(t, review.ID, replaced.ID, "the starter is saved over in place, so it stays the default")
	assert.Equal(t, "Review {{.URL}} my way.", replaced.Body)
	assert.Equal(t, StarterReviewPR, replaced.StarterKey, "it can still be reset to the original")
	assert.True(t, replaced.Modified)
	assert.Equal(t, []item.Kind{item.KindPR}, replaced.Usage.Defaults)

	saved, err := readBundleFile(filepath.Join(dir, "prompts.json"))
	require.NoError(t, err)
	assert.Len(t, saved.Prompts, len(Starters())+1, "the library file is written after the import")
}

func TestImportRecordsTheStartersItAdds(t *testing.T) {
	t.Parallel()

	s, db := empty(t, nil)

	_, err := s.Import(t.Context(), []BundlePrompt{starterAsBundled(t, StarterQuickLook, nil)})
	require.NoError(t, err)
	require.NoError(t, Seed(t.Context(), s.store))

	assert.Len(t, nicknames(t, s), len(Starters()), "seeding later does not add the starter a second time")
	assert.Contains(t, promptPrefs(t, db)[prefSeeded], StarterQuickLook)
}

func TestImportIsAllOrNothing(t *testing.T) {
	t.Parallel()

	s, db, _ := keeping(t)
	before := nicknames(t, s)

	exec(t, db, `CREATE TRIGGER refuse_second BEFORE INSERT ON prompts WHEN NEW.nickname = 'Second'
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	_, err := s.Import(t.Context(), []BundlePrompt{mine("First"), mine("Second")})
	require.ErrorContains(t, err, `import "Second"`)
	assert.Equal(t, before, nicknames(t, s))
}

func TestImportRefusesTooManyPrompts(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	_, err := s.Import(t.Context(), make([]BundlePrompt, maxBundlePrompts+1))
	require.ErrorIs(t, err, ErrBundleTooLarge)
}

func TestImportKeepsWhatTheUserWroteAndLeavesOutWhatTheEditorWouldRefuse(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	entries := preview(t, s, bundleOf(BundlePrompt{
		Name:      "  A title\nthat runs on and on, far past the eighty characters a prompt's title may hold in all  ",
		Nickname:  "",
		AppliesTo: "everything",
		Body:      "Broken {{.Nope}}\r\nsecond line",
		Models:    map[agent.ID]string{agent.Claude: "--not-a-model", "example-agent": "x"},
		Effort:    "huge",
		ExtraArgs: map[agent.ID][]string{
			agent.Claude: {"--dangerously-skip-permissions"}, agent.Codex: {"--search"}, "example-agent": {"--x"},
		},
		StarterKey: "Not A Key",
	}))
	require.Len(t, entries, 1)

	p := entries[0].Prompt
	assert.Equal(t, "A title that runs on and on, far past the eighty characters a prompt's title may", p.Name)
	assert.Equal(t, "A title that runs on", p.Nickname, "a missing nickname comes from the title")
	assert.Equal(t, AppliesToAny, p.AppliesTo)
	assert.Equal(t, "Broken {{.Nope}}\nsecond line", p.Body, "a text with problems is kept to be fixed")
	assert.Empty(t, p.Models)
	assert.Empty(t, p.Effort)
	assert.Equal(t, map[agent.ID][]string{agent.Codex: {"--search"}}, p.ExtraArgs,
		"a file can never pass a flag the editor would refuse, and only an agent this version knows gets flags")
	assert.Empty(t, p.StarterKey)

	notes := strings.Join(entries[0].Notes, "\n")
	for _, want := range []string{
		"applies to both", "Claude model", `model for "example-agent"`, "effort", "Claude flags were left out",
		`extra flags for "example-agent"`, "problems to fix",
	} {
		assert.Contains(t, notes, want)
	}

	res, err := s.Import(t.Context(), []BundlePrompt{entries[0].Prompt})
	require.NoError(t, err)
	assert.Equal(t, 1, res.Added, "a prompt with problems is imported, never dropped")
}

func TestImportNamesAPromptThatHasNone(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	entries := preview(t, s, bundleOf(BundlePrompt{Body: lookAtURL, AppliesTo: AppliesToPR}))
	require.Len(t, entries, 1)
	assert.Equal(t, untitledName, entries[0].Prompt.Name)
	assert.Equal(t, untitledNickname, entries[0].Nickname)
}

func TestBackupsListsEveryCopyNewestFirst(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)
	db := filepath.Join(dir, "app.db")

	// An older library with one prompt of the user's, set aside, and an update backup that
	// cannot be read.
	old := filepath.Join(dir, "app.db.broken-20261108T165152Z")
	writeLibraryDB(t, old, mine("Security"), mine("Review"))
	testutil.WriteFile(t, db+".backup-20261106T201810Z-v10", "not a database at all, only text long enough")

	_, err := s.Create(t.Context(), Input{Name: mineName, Nickname: mineName, AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)

	backups, err := s.Backups(t.Context())
	require.NoError(t, err)
	require.Len(t, backups, 3)

	assert.Equal(t, BackupLibrary, backups[0].Kind, "the library file was written just now")
	assert.Equal(t, len(Starters())+1, backups[0].Prompts)
	assert.Zero(t, backups[0].New, "the library holds everything in its own copy")
	assert.Empty(t, backups[0].Error)

	assert.Equal(t, Backup{Path: old, Kind: BackupSetAside, At: "2026-11-08T16:51:52Z", Prompts: 2, New: 2}, backups[1])

	assert.Equal(t, BackupUpdate, backups[2].Kind)
	assert.Equal(t, "2026-11-06T20:18:10Z", backups[2].At)
	assert.NotEmpty(t, backups[2].Error)
}

func TestPreviewImportReadsAListedBackup(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)
	old := filepath.Join(dir, "app.db.broken-20261108T165152Z")
	writeLibraryDB(t, old, mine("Security"))

	entries, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceBackup, Path: old})
	require.NoError(t, err)
	assert.Equal(t, []string{"add Security"}, outcomes(entries))

	elsewhere := filepath.Join(t.TempDir(), "app.db.broken-20261108T165152Z")
	writeLibraryDB(t, elsewhere, mine("Security"))

	_, err = s.PreviewImport(t.Context(), ImportSource{Kind: SourceBackup, Path: elsewhere})
	require.ErrorIs(t, err, ErrUnknownBackup, "only a backup the list names is read")

	_, err = s.PreviewImport(t.Context(), ImportSource{Kind: "web", Path: old})
	require.ErrorIs(t, err, ErrUnknownSource)

	_, err = s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: "relative.json"})
	require.ErrorIs(t, err, ErrNoPath)
}

func TestPreviewImportReadsADatabaseFileTheUserChose(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)
	copied := filepath.Join(t.TempDir(), "kept-from-a-backup.db")
	writeLibraryDB(t, copied, mine("Security"))

	entries, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: copied})
	require.NoError(t, err)
	assert.Equal(t, []string{"add Security"}, outcomes(entries))
}

func TestPreviewImportRefusesAFileThatHoldsNoPrompts(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)
	path := filepath.Join(t.TempDir(), "notes.json")
	testutil.WriteFile(t, path, `{"hello": "world"}`)

	_, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: path})
	require.ErrorIs(t, err, ErrNotBundle)
}

// writeLibraryDB writes a migrated database at path holding prompts, as a library an
// older run of the app kept.
func writeLibraryDB(t *testing.T, path string, prompts ...BundlePrompt) {
	t.Helper()

	db := testutil.DBAt(t, path)
	st := NewStore(db, nil)

	for _, p := range prompts {
		_, err := st.insert(t.Context(), bundledAsPrompt(p))
		require.NoError(t, err)
	}

	require.NoError(t, db.Close())
}

func TestEveryChangeRewritesTheLibraryFile(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)
	file := filepath.Join(dir, "prompts.json")
	ctx := t.Context()

	savedNicknames := func() []string {
		b, err := readBundleFile(file)
		require.NoError(t, err)

		out := make([]string, 0, len(b.Prompts))
		for _, p := range b.Prompts {
			out = append(out, p.Nickname)
		}

		return out
	}

	created, err := s.Create(ctx, Input{Name: mineName, Nickname: mineName, AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)
	assert.Contains(t, savedNicknames(), mineName, "create")

	in := toInput(created)
	in.Nickname = "Mine too"
	_, err = s.Update(ctx, created.ID, in)
	require.NoError(t, err)
	assert.Contains(t, savedNicknames(), "Mine too", "update")

	dup, err := s.Duplicate(ctx, created.ID)
	require.NoError(t, err)
	assert.Contains(t, savedNicknames(), dup.Nickname, "duplicate")

	require.NoError(t, s.Delete(ctx, dup.ID))
	assert.NotContains(t, savedNicknames(), dup.Nickname, "delete")

	require.NoError(t, s.SetDefault(ctx, item.KindPR, created.ID))

	b, err := readBundleFile(file)
	require.NoError(t, err)
	assert.Equal(t, "Mine too", b.Defaults.PR, "set default")

	quick := byNickname(t, s, "Quick look")
	edited := toInput(quick)
	edited.Body = "Changed {{.URL}}"
	_, err = s.Update(ctx, quick.ID, edited)
	require.NoError(t, err)
	_, err = s.Reset(ctx, quick.ID)
	require.NoError(t, err)

	b, err = readBundleFile(file)
	require.NoError(t, err)

	for _, p := range b.Prompts {
		if p.Nickname == "Quick look" {
			assert.NotEqual(t, "Changed {{.URL}}", p.Body, "reset")
		}
	}
}

func TestALibraryFileThatCannotBeWrittenIsLoggedNotFailed(t *testing.T) {
	t.Parallel()

	var logs testutil.Logs

	db := testutil.DB(t)
	st := NewStore(db, nil)
	require.NoError(t, Seed(t.Context(), st))

	blocked := filepath.Join(t.TempDir(), "prompts.json")
	require.NoError(t, os.Mkdir(blocked, appdata.PrivateDirPerm))

	s := NewService(Config{Store: st, File: NewLibraryFile(blocked, "", nil), Logger: logs.Logger()})

	_, err := s.Create(t.Context(), Input{Name: mineName, Nickname: mineName, AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err, "the prompt is stored even when its copy cannot be written")
	assert.Contains(t, logs.String(), "could not save the copy of the prompt library")
}

func TestRestoreBringsBackTheLibraryFromTheDatabaseSetAside(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	old := filepath.Join(dir, "old.db")

	oldDB := testutil.DBAt(t, old)
	oldStore := NewStore(oldDB, nil)
	require.NoError(t, Seed(t.Context(), oldStore))

	oldService := NewService(Config{Store: oldStore})
	security, err := oldService.Create(t.Context(), Input{
		Name: "Security", Nickname: "Security", AppliesTo: AppliesToAny, Body: lookAtURL,
	})
	require.NoError(t, err)
	require.NoError(t, oldService.SetDefault(t.Context(), item.KindIssue, security.ID))
	require.NoError(t, oldService.Delete(t.Context(), byNickname(t, oldService, "Fix checks").ID))
	require.NoError(t, oldDB.Close())

	s, _ := empty(t, nil)
	file := NewLibraryFile(filepath.Join(dir, "missing.json"), "", nil)

	restored, err := Restore(t.Context(), s.store, old, file)
	require.NoError(t, err)
	assert.Equal(t, len(Starters()), restored.Count)
	assert.Equal(t, old, restored.From)
	assert.Equal(t, "Security", restored.Nicknames[security.ID], "what the database set aside names by ID can be matched")
	assert.Len(t, restored.Nicknames, len(Starters()))

	require.NoError(t, Seed(t.Context(), s.store))
	assert.ElementsMatch(t, []string{
		"LGTM", "Needs changes", "Question", "Quick look", "Review", "Security", "Update OK", "Work on it",
	}, nicknames(t, s), "the deleted starter stays deleted")
	assert.Equal(t, byNickname(t, s, "Security").ID, restored.IDs["Security"], "each nickname finds its restored prompt")

	defaults, err := s.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, byNickname(t, s, "Security").ID, defaults.Issue)
	assert.Equal(t, byNickname(t, s, "Review").ID, defaults.PR)
}

func TestRestoreFallsBackToTheLibraryFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	broken := filepath.Join(dir, "app.db.broken")
	testutil.WriteFile(t, broken, "not a database at all, only text long enough")

	path := writeBundle(t, bundleOf(mine("Security"), mine("Security")))

	s, _ := empty(t, nil)

	restored, err := Restore(t.Context(), s.store, broken, NewLibraryFile(path, "", nil))
	require.ErrorContains(t, err, "read the prompts in the database set aside", "the source that failed is named")
	assert.Equal(t, 2, restored.Count)
	assert.Equal(t, path, restored.From)
	assert.Nil(t, restored.Nicknames, "a file names prompts by nickname alone")
	assert.Equal(t, map[string]int64{"Security": byNickname(t, s, "Security").ID}, restored.IDs,
		"a nickname two prompts shared finds the first")
	assert.Equal(t, []string{"Security", "Security 2"}, nicknames(t, s), "a restore keeps every prompt")
}

func TestRestoreLeavesALibraryThatExists(t *testing.T) {
	t.Parallel()

	s, _, dir := keeping(t)
	before := nicknames(t, s)
	path := writeBundle(t, bundleOf(mine("Security")))

	restored, err := Restore(t.Context(), s.store, "", NewLibraryFile(path, "", nil))
	require.NoError(t, err)
	assert.Equal(t, Restored{}, restored)
	assert.Equal(t, before, nicknames(t, s))

	// A library with a prompt but no record of starters exists too.
	other, _ := empty(t, nil)
	_, err = other.Create(t.Context(), Input{Name: mineName, Nickname: mineName, AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)

	restored, err = Restore(t.Context(), other.store, "", NewLibraryFile(filepath.Join(dir, "prompts.json"), "", nil))
	require.NoError(t, err)
	assert.Equal(t, Restored{}, restored)
}

func TestRestoreWithNothingToRestoreFromChangesNothing(t *testing.T) {
	t.Parallel()

	s, db := empty(t, nil)

	restored, err := Restore(t.Context(), s.store, "", NewLibraryFile(filepath.Join(t.TempDir(), "none.json"), "", nil))
	require.NoError(t, err)
	assert.Equal(t, Restored{}, restored)

	restored, err = Restore(t.Context(), s.store, "", nil)
	require.NoError(t, err)
	assert.Equal(t, Restored{}, restored)
	assert.Empty(t, promptPrefs(t, db), "a new library stays new for Seed to start")
}

func TestRestoreSkipsADefaultThatNoLongerApplies(t *testing.T) {
	t.Parallel()

	b := bundleOf(mine("Security"))
	b.Prompts[0].AppliesTo = AppliesToIssue
	b.Defaults = BundleDefaults{PR: "Security", Issue: "Gone"}

	s, _ := empty(t, nil)

	_, err := Restore(t.Context(), s.store, "", NewLibraryFile(writeBundle(t, b), "", nil))
	require.NoError(t, err)

	stored, err := s.store.storedDefaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, DefaultPrompts{}, stored)
}

func TestRestoreReportsALibraryFileItCannotRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "prompts.json")
	testutil.WriteFile(t, path, "{ not json")

	s, _ := empty(t, nil)

	restored, err := Restore(t.Context(), s.store, "", NewLibraryFile(path, "", nil))
	require.ErrorIs(t, err, ErrNotBundle)
	assert.Equal(t, Restored{}, restored)
}

func TestDecodeBundleReadsVersionOne(t *testing.T) {
	t.Parallel()

	got, err := DecodeBundle([]byte(`{
		"format": "` + BundleFormat + `", "version": 1,
		"prompts": [
			{"name": "Old", "nickname": "Old", "appliesTo": "pr", "body": "x", "model": "opus", "extraArgs": ["--verbose"]},
			{"name": "Bare", "nickname": "Bare", "appliesTo": "pr", "body": "x", "model": "", "extraArgs": []},
			{"name": "Null", "nickname": "Null", "appliesTo": "pr", "body": "x", "extraArgs": null}
		]
	}`))
	require.NoError(t, err)
	require.Len(t, got.Prompts, 3)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "opus"}, got.Prompts[0].Models, "a version 1 model was Claude's")
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--verbose"}}, got.Prompts[0].ExtraArgs)
	assert.Equal(t, map[agent.ID]string{}, got.Prompts[1].Models)
	assert.Equal(t, map[agent.ID][]string{}, got.Prompts[1].ExtraArgs)
	assert.Equal(t, map[agent.ID][]string{}, got.Prompts[2].ExtraArgs)

	data, err := encodeBundle(got)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"model"`, "the file is written as version 2")
	assert.Contains(t, string(data), `"models": {`)
}

func TestReadSnapshotBundleReadsEitherSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pre-agents.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	for _, stmt := range []string{
		// The prompts table before the agents migration: one model and list, Claude's.
		`CREATE TABLE prompts (id INTEGER PRIMARY KEY, name TEXT, nickname TEXT, applies_to TEXT, body TEXT,
			model TEXT, effort TEXT, extra_args TEXT, created_at DATETIME, updated_at DATETIME)`,
		`INSERT INTO prompts VALUES (1, 'Old', 'Old', 'pr', 'x', 'opus', 'high', '["--verbose"]',
			'2026-01-01 10:00:00+00:00', '2026-01-02 10:00:00+00:00')`,
	} {
		_, err = raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())

	got, err := readSnapshotBundle(t.Context(), path)
	require.NoError(t, err)
	require.Len(t, got.Prompts, 1)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "opus"}, got.Prompts[0].Models)
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--verbose"}}, got.Prompts[0].ExtraArgs)

	// A backup this build took holds the per-agent columns.
	s, db, _ := keeping(t)
	_, err = s.Create(t.Context(), Input{
		Name: "New", Nickname: "New", AppliesTo: AppliesToPR, Body: lookAtURL,
		Models:     map[agent.ID]string{agent.Codex: "gpt-6-sol"},
		ExtraFlags: map[agent.ID]string{agent.Codex: "--search", agent.Claude: "--verbose"},
	})
	require.NoError(t, err)

	backup := filepath.Join(t.TempDir(), "backup.db")
	_, err = db.ExecContext(t.Context(), "VACUUM INTO ?", backup)
	require.NoError(t, err)

	got, err = readSnapshotBundle(t.Context(), backup)
	require.NoError(t, err)

	var found *BundlePrompt

	for i := range got.Prompts {
		if got.Prompts[i].Nickname == "New" {
			found = &got.Prompts[i]
		}
	}

	require.NotNil(t, found)
	assert.Equal(t, map[agent.ID]string{agent.Codex: "gpt-6-sol"}, found.Models)
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--verbose"}, agent.Codex: {"--search"}}, found.ExtraArgs)
}

func TestReadSnapshotBundleReadsAnOlderSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "older.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	for _, stmt := range []string{
		// An early prompts table, before launch settings and starters, and no prefs table.
		`CREATE TABLE prompts (id INTEGER PRIMARY KEY, name TEXT, nickname TEXT, applies_to TEXT, body TEXT,
			created_at DATETIME, updated_at DATETIME)`,
		`INSERT INTO prompts VALUES (1, 'Early', 'Early', 'pr', 'Look at {{.URL}}',
			'2026-01-01 10:00:00+00:00', '2026-01-02 10:00:00+00:00')`,
	} {
		_, err = raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())

	got, err := readSnapshotBundle(t.Context(), path)
	require.NoError(t, err)
	assert.Equal(t, []BundlePrompt{{
		Name: "Early", Nickname: "Early", AppliesTo: AppliesToPR, Body: lookAtURL, Purpose: PurposeAgent,
		Models: map[agent.ID]string{}, ExtraArgs: map[agent.ID][]string{},
		CreatedAt: "2026-01-01T10:00:00Z", UpdatedAt: "2026-01-02T10:00:00Z",
	}}, got.Prompts, "a backup from before replies holds agent prompts")
	assert.Equal(t, []string{}, got.StartersAdded)
	assert.Equal(t, BundleDefaults{}, got.Defaults)
}

func TestImportEntriesHoldNoNullLists(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	data, err := json.Marshal(preview(t, s, bundleOf(BundlePrompt{Name: "x", Nickname: "x", AppliesTo: AppliesToAny, Body: "{{.URL}}"})))
	require.NoError(t, err)
	assert.Contains(t, string(data), `"notes":[]`)
	assert.Contains(t, string(data), `"extraArgs":{}`)
	assert.Contains(t, string(data), `"models":{}`)
}

func TestEveryChangeTellsChanged(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	st := NewStore(db, nil)
	require.NoError(t, Seed(t.Context(), st))

	told := 0
	s := NewService(Config{Store: st, Changed: func(context.Context) { told++ }})
	ctx := t.Context()

	created, err := s.Create(ctx, Input{Name: mineName, Nickname: mineName, AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)

	in := toInput(created)
	in.Nickname = "Renamed"
	_, err = s.Update(ctx, created.ID, in)
	require.NoError(t, err)

	_, err = s.Create(ctx, Input{Name: "", Nickname: "", AppliesTo: AppliesToAny, Body: "x"})
	require.Error(t, err, "a refused change tells nothing")

	require.NoError(t, s.Delete(ctx, created.ID))
	assert.Equal(t, 3, told)
}
