package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Errors the fake parts fail with.
var (
	errBroken     = errors.New("the prompts are broken")
	errUnreadable = errors.New("unreadable")
)

// exportedAt is when the tests' backups are written.
func exportedAt() time.Time { return time.Date(2026, time.November, 14, 17, 0, 0, 0, time.UTC) }

// journal records what the fake parts were asked to do, in order.
type journal struct {
	mu    sync.Mutex
	lines []string
}

func (j *journal) add(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.lines = append(j.lines, line)
}

func (j *journal) all() []string {
	j.mu.Lock()
	defer j.mu.Unlock()

	return slices.Clone(j.lines)
}

// fakePart is a category that exports one value and imports it into the pref named after
// its section, recording every call in its journal.
type fakePart struct {
	section Section
	db      *sql.DB
	value   string
	log     *journal

	// plans are what Prepare says under each mode; prepareErr fails it.
	plans      map[Mode]Plan
	prepareErr error

	// provides is what the prompts part says the library will hold, and seen what the part
	// was told the prompts part provides.
	provides map[string]PromptKind
	seen     *map[string]PromptKind
}

// section is what a fake part writes into a backup.
type section struct {
	Value string `json:"value"`
}

func (f *fakePart) Export(context.Context) (json.RawMessage, int, error) {
	raw, err := EncodeSection(section{Value: f.value})

	return raw, 1, err
}

func (f *fakePart) Prepare(_ context.Context, raw json.RawMessage, how How) (Prepared, error) {
	f.log.add("prepare " + string(f.section) + " " + string(how.Mode))

	if f.seen != nil {
		*f.seen = how.Prompts
	}

	if f.prepareErr != nil {
		return Prepared{}, f.prepareErr
	}

	var s section
	if err := json.Unmarshal(raw, &s); err != nil {
		return Prepared{}, err
	}

	return Prepared{Plan: f.plans[how.Mode], Prompts: f.provides, State: s.Value}, nil
}

func (f *fakePart) Apply(ctx context.Context, tx *sql.Tx, p Prepared, links Links) (Count, error) {
	f.log.add("apply " + string(f.section))

	if f.section == SectionPrompts {
		links["Review"] = 7
	}

	if f.section == SectionLaunchDefaults {
		f.log.add("links " + strings.TrimSpace(func() string { b, _ := json.Marshal(links); return string(b) }()))
	}

	value, _ := p.State.(string)
	if err := prefs.New(f.db, nil).WithTx(tx).Set(ctx, "test."+string(f.section), value); err != nil {
		return Count{}, err
	}

	return Count{Update: 1}, nil
}

func (f *fakePart) Done(context.Context, Prepared) {
	f.log.add("done " + string(f.section))
}

// clearingPart is a fakePart that is a Clearer.
type clearingPart struct {
	*fakePart
}

func (c clearingPart) Clear(ctx context.Context, tx *sql.Tx) error {
	c.log.add("clear " + string(c.section))

	return prefs.New(c.db, nil).WithTx(tx).Delete(ctx, "test.cleared")
}

// rig is a Service over fake parts for every category, writing into a fresh database.
type rig struct {
	svc   *Service
	db    *sql.DB
	parts map[Section]*fakePart
	log   *journal
	dir   string
	home  string
	clock *testutil.Clock
}

func newRig(t *testing.T, value string) rig {
	t.Helper()

	db := testutil.DB(t)
	home := t.TempDir()
	r := rig{
		db: db, parts: map[Section]*fakePart{}, log: &journal{}, home: home,
		dir: filepath.Join(home, "data", "backups"), clock: testutil.NewClock(exportedAt()),
	}

	parts := map[Section]Part{}

	for _, s := range Sections() {
		f := &fakePart{section: s, db: db, value: value + " " + string(s), log: r.log, plans: map[Mode]Plan{
			ModeMerge:   {Count: Count{Add: 1}, Items: []Item{{Label: "x", Change: ChangeAdd}}},
			ModeReplace: {Count: Count{Add: 1, Remove: 2}},
		}}
		r.parts[s] = f
		parts[s] = f

		if s == SectionLaunchDefaults {
			parts[s] = clearingPart{f}
		}
	}

	r.svc = NewService(Config{Pool: db, Parts: parts, Dir: r.dir, App: "v1.2.3", Home: home, Clock: r.clock})

	return r
}

// export writes a backup of sections to a new file and returns its path.
func (r rig) export(t *testing.T, sections ...Section) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "backup.json")

	_, err := r.svc.Export(t.Context(), path, sections)
	require.NoError(t, err)

	return path
}

// stored reads the pref a fake part writes.
func (r rig) stored(t *testing.T, s Section) (string, bool) {
	t.Helper()

	v, ok, err := prefs.New(r.db, nil).Get(t.Context(), "test."+string(s))
	require.NoError(t, err)

	return v, ok
}

func TestFileRoundTrips(t *testing.T) {
	t.Parallel()

	in := File{Format: Format, Version: Version, ExportedAt: "2026-11-14T17:00:00Z", App: "v1.2.3"}
	raw, err := EncodeSection(section{Value: "<b> & more"})
	require.NoError(t, err)

	in.Prompts = raw

	data, err := Encode(in)
	require.NoError(t, err)
	assert.Contains(t, string(data), "<b> & more", "text reads in the file as it was written")
	assert.NotContains(t, string(data), `"settings"`, "a category not written is absent")
	assert.True(t, strings.HasSuffix(string(data), "}\n"))

	out, err := Decode(data)
	require.NoError(t, err)
	assert.Equal(t, in.ExportedAt, out.ExportedAt)
	assert.Nil(t, out.Section(SectionSettings))
	assert.JSONEq(t, string(raw), string(out.Section(SectionPrompts)))
	assert.Nil(t, out.Section("later"))
}

func TestDecodeRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()

	header := `{"format": "` + Format + `", "version": `

	for name, tc := range map[string]struct {
		data string
		want error
	}{
		"not JSON":          {data: "not json", want: ErrNotBackup},
		"another format":    {data: `{"format": "other", "version": 1}`, want: ErrNotBackup},
		"no version":        {data: header + `0}`, want: ErrNotBackup},
		"a newer build's":   {data: header + `2}`, want: ErrNewerBackup},
		"a section a list":  {data: header + `1, "scripts": []}`, want: ErrNotBackup},
		"a section a word":  {data: header + `1, "settings": "dark"}`, want: ErrNotBackup},
		"a prompt file":     {data: `{"format": "x.prompts", "version": 3, "prompts": []}`, want: ErrNotBackup},
		"too large to read": {data: strings.Repeat(" ", MaxBytes+1), want: ErrTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Decode([]byte(tc.data))
			require.ErrorIs(t, err, tc.want)
		})
	}

	f, err := Decode([]byte(header + `1, "settings": null, "unknown": [1]}`))
	require.NoError(t, err, "a field this build doesn't know is ignored")
	assert.Nil(t, f.Section(SectionSettings), "a section written as null is absent")
}

func TestExportWritesOnlyTheChosenCategories(t *testing.T) {
	t.Parallel()

	r := newRig(t, "here")
	path := filepath.Join(t.TempDir(), "backup.json")

	got, err := r.svc.Export(t.Context(), path, []Section{SectionScripts, SectionPrompts, SectionPrompts})
	require.NoError(t, err)
	assert.Equal(t, Exported{Path: path, Sections: []SectionCount{
		{Section: SectionPrompts, Items: 1}, {Section: SectionScripts, Items: 1},
	}}, got, "each category once, in order")

	data, err := os.ReadFile(path) //nolint:gosec // a file the test wrote
	require.NoError(t, err)

	f, err := Decode(data)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-14T17:00:00Z", f.ExportedAt)
	assert.Equal(t, "v1.2.3", f.App)
	assert.NotNil(t, f.Section(SectionPrompts))
	assert.NotNil(t, f.Section(SectionScripts))
	assert.Nil(t, f.Section(SectionSettings), "a category not ticked is absent")

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateFilePerm, info.Mode().Perm())

	_, err = r.svc.Export(t.Context(), path, nil)
	require.ErrorIs(t, err, ErrNoSections)
	assert.Equal(t, "Choose what to include.", uierr.Message(err))

	_, err = r.svc.Export(t.Context(), path, []Section{"history"})
	require.ErrorIs(t, err, ErrUnknownSection)

	_, err = r.svc.Export(t.Context(), "backup.json", Sections())
	require.ErrorIs(t, err, ErrNoPath)
}

func TestPreviewShowsEveryCategoryUnderBothModes(t *testing.T) {
	t.Parallel()

	r := newRig(t, "there")
	path := r.export(t, SectionSettings, SectionPrompts, SectionLaunchDefaults)
	r.parts[SectionPrompts].prepareErr = errBroken

	got, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	data, err := os.ReadFile(path) //nolint:gosec // a file the test wrote
	require.NoError(t, err)
	assert.Equal(t, Hash(data), got.Hash)
	assert.Equal(t, path, got.Path)
	assert.Equal(t, "v1.2.3", got.App)

	require.Len(t, got.Sections, len(Sections()))

	present := map[Section]bool{}
	for _, s := range got.Sections {
		present[s.Section] = s.Present
	}

	assert.Equal(t, map[Section]bool{
		SectionSettings: true, SectionRepositories: false, SectionPrompts: true, SectionLaunchDefaults: true,
		SectionScripts: false,
	}, present)

	settings := got.Sections[0]
	assert.Equal(t, Count{Add: 1}, settings.Merge.Count)
	assert.Equal(t, Count{Add: 1, Remove: 2}, settings.Replace.Count)
	assert.Equal(t, []string{}, settings.Replace.Notes, "a list is never nil")
	assert.Empty(t, settings.Error)

	assert.Equal(t, "The prompts are broken.", got.Sections[2].Error, "a section that can't be read says why")
	assert.Empty(t, got.Sections[3].Error, "and the others still show")

	assert.Equal(t, []Item{}, got.Sections[1].Merge.Items, "an absent section has nothing to show")
	assert.Empty(t, r.applied(), "a preview changes nothing")
}

// applied lists the parts' applies in r's journal.
func (r rig) applied() []string {
	var out []string

	for _, line := range r.log.all() {
		if strings.HasPrefix(line, "apply ") || strings.HasPrefix(line, "clear ") {
			out = append(out, line)
		}
	}

	return out
}

func TestImportAppliesOnlyTheChosenCategoriesInOrder(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, Sections()...)
	r.parts[SectionPrompts].provides = map[string]PromptKind{"Review": {AppliesTo: "pr"}}

	var told map[string]PromptKind

	r.parts[SectionLaunchDefaults].seen = &told

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	got, err := r.svc.Import(t.Context(), Request{
		Path: path, Hash: p.Hash, Mode: ModeMerge,
		Sections: []Section{SectionScripts, SectionLaunchDefaults, SectionPrompts, SectionSettings},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"apply settings", "apply prompts", "apply launchDefaults", "apply scripts"}, r.applied(),
		"Merge clears nothing, and applies in order")
	assert.Equal(t, map[string]PromptKind{"Review": {AppliesTo: "pr"}}, told,
		"the parts after the prompts hear what the library will hold")
	assert.Contains(t, r.log.all(), `links {"Review":7}`, "the launch defaults see the prompts' links")

	value, ok := r.stored(t, SectionSettings)
	assert.True(t, ok)
	assert.Equal(t, "file settings", value)

	_, ok = r.stored(t, SectionRepositories)
	assert.False(t, ok, "a category not chosen is untouched")

	require.Len(t, got.Sections, 4)
	assert.Equal(t, SectionResult{Section: SectionSettings, Count: Count{Update: 1}, Notes: []string{}}, got.Sections[0])

	lines := r.log.all()
	assert.Less(t, slices.Index(lines, "apply scripts"), slices.Index(lines, "done settings"),
		"Done runs once everything is applied")
}

func TestReplaceClearsFirst(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, Sections()...)

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	_, err = r.svc.Import(t.Context(), Request{
		Path: path, Hash: p.Hash, Mode: ModeReplace, Sections: []Section{SectionPrompts, SectionLaunchDefaults},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"clear launchDefaults", "apply prompts", "apply launchDefaults"}, r.applied(),
		"Replace clears the launch defaults before the prompts are applied")
}

func TestImportRefusesAFileThatChanged(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, Sections()...)

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	r.parts[SectionSettings].value = "edited"
	_, err = r.svc.Export(t.Context(), path, Sections())
	require.NoError(t, err)

	_, err = r.svc.Import(t.Context(), Request{Path: path, Hash: p.Hash, Mode: ModeMerge, Sections: Sections()})
	require.ErrorIs(t, err, ErrChanged)
	assert.Equal(t, "The file changed since you opened it; open it again to see what it holds now.", uierr.Message(err))
	assert.Empty(t, r.applied())

	_, err = os.Stat(r.dir)
	require.ErrorIs(t, err, os.ErrNotExist, "nothing is saved for an import that never starts")
}

func TestImportRefusesWhatItCannotDo(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, SectionSettings)

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	ctx := t.Context()

	_, err = r.svc.Import(ctx, Request{Path: path, Hash: p.Hash, Mode: ModeMerge, Sections: []Section{SectionScripts}})
	require.ErrorIs(t, err, ErrNotInFile)

	_, err = r.svc.Import(ctx, Request{Path: path, Hash: p.Hash, Mode: "overwrite", Sections: Sections()})
	require.ErrorIs(t, err, ErrUnknownMode)

	_, err = r.svc.Import(ctx, Request{Path: path, Hash: p.Hash, Mode: ModeMerge})
	require.ErrorIs(t, err, ErrNoSections)

	_, err = r.svc.Import(ctx, Request{Path: "backup.json", Mode: ModeMerge, Sections: Sections()})
	require.ErrorIs(t, err, ErrNoPath)

	r.parts[SectionSettings].prepareErr = errUnreadable

	_, err = r.svc.Import(ctx, Request{Path: path, Hash: p.Hash, Mode: ModeMerge, Sections: []Section{SectionSettings}})
	require.EqualError(t, err, "unreadable", "a category that can't be worked out stops the import")
	assert.Empty(t, r.applied())
}

func TestImportIsAllOrNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, Sections()...)

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	testutil.Exec(t, r.db, `CREATE TRIGGER refuse_scripts BEFORE INSERT ON prefs WHEN NEW.key = 'test.scripts'
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	_, err = r.svc.Import(t.Context(), Request{Path: path, Hash: p.Hash, Mode: ModeMerge, Sections: Sections()})
	require.ErrorContains(t, err, "refused by the test")

	for _, s := range Sections() {
		_, ok := r.stored(t, s)
		assert.False(t, ok, "%s: a failure in one category rolls every one back", s)
	}

	assert.NotContains(t, r.log.all(), "done settings", "nothing is told of an import that rolled back")
}

func TestImportSavesTheSetupFirstAndKeepsTheNewestFive(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, SectionSettings)

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	saved := make([]string, 0, KeepBefore+2)

	for range KeepBefore + 2 {
		r.clock.Advance(time.Minute)

		got, importErr := r.svc.Import(t.Context(), Request{
			Path: path, Hash: p.Hash, Mode: ModeMerge, Sections: []Section{SectionSettings},
		})
		require.NoError(t, importErr)

		saved = append(saved, got.SavedTo)
	}

	assert.Equal(t, "~/data/backups/"+beforePrefix+"20261114T170700Z.json", saved[len(saved)-1],
		"the copy is named with ~ for home")

	entries, err := os.ReadDir(r.dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	assert.Equal(t, []string{
		beforePrefix + "20261114T170300Z.json", beforePrefix + "20261114T170400Z.json",
		beforePrefix + "20261114T170500Z.json", beforePrefix + "20261114T170600Z.json",
		beforePrefix + "20261114T170700Z.json",
	}, names, "the newest five are kept")

	data, err := os.ReadFile(filepath.Join(r.dir, names[0]))
	require.NoError(t, err)

	f, err := Decode(data)
	require.NoError(t, err)

	for _, s := range Sections() {
		assert.NotNil(t, f.Section(s), "%s: the copy holds every category", s)
	}
}

func TestSectionsAndModes(t *testing.T) {
	t.Parallel()

	for _, s := range Sections() {
		assert.True(t, s.Valid(), s)
	}

	assert.False(t, Section("history").Valid())
	assert.True(t, ModeMerge.Valid())
	assert.True(t, ModeReplace.Valid())
	assert.False(t, Mode("").Valid())

	var p Plan
	for _, c := range []Change{ChangeAdd, ChangeUpdate, ChangeSame, ChangeRemove, ChangeRemove} {
		p.Add(Item{Label: string(c), Change: c})
	}

	p.AddNote("kept")
	assert.Equal(t, Count{Add: 1, Update: 1, Same: 1, Remove: 2}, p.Count)
	assert.True(t, p.Count.Changes())
	assert.False(t, Count{Same: 3}.Changes())
	assert.Equal(t, []string{"kept"}, p.Notes)
}
