package devtools_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/devtools"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// keep adds a note with words text and picture to notes, numbered as notes reserves.
func keep(t *testing.T, notes *devtools.Notes, text string, picture []byte) {
	t.Helper()

	require.NoError(t, notes.Add(devtools.Note{N: notes.Reserve(), Time: fixedNow, Text: text, Shot: picture}))
}

// texts are the notes' words.
func texts(notes []devtools.Note) []string {
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		out = append(out, n.Text)
	}

	return out
}

func TestNotesOutliveTheApp(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "notes")

	first := devtools.NewNotes(10)
	require.NoError(t, first.Open(dir))
	keep(t, first, "grey", []byte("png1"))
	keep(t, first, "too wide", nil)

	_, err := first.Done([]int{1}, false)
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, "1.json"))
	assert.FileExists(t, filepath.Join(dir, "1.png"))
	assert.NoFileExists(t, filepath.Join(dir, "2.png"), "a note without a picture keeps none")

	// The app starts again.
	again := devtools.NewNotes(10)
	require.NoError(t, again.Open(dir))

	all := again.List(true)
	require.Len(t, all, 2)
	assert.Equal(t, []string{"grey", "too wide"}, texts(all))
	assert.True(t, all[0].Done, "a note marked done stays done")
	assert.Equal(t, []byte("png1"), all[0].Shot)
	assert.Equal(t, []string{"too wide"}, texts(again.List(false)), "the open notes alone")

	assert.Equal(t, 3, again.Reserve(), "numbers go on after the last one kept")
}

func TestNotesDone(t *testing.T) {
	t.Parallel()

	notes := devtools.NewNotes(10)
	for _, text := range []string{"a", "b", "c"} {
		keep(t, notes, text, nil)
	}

	marked, err := notes.Done([]int{1, 3, 9}, false)
	require.NoError(t, err)
	assert.Equal(t, 2, marked, "a number no note has is left alone")

	marked, err = notes.Done([]int{1}, false)
	require.NoError(t, err)
	assert.Equal(t, 0, marked, "a note done already is not marked again")

	open, kept := notes.Counts()
	assert.Equal(t, []int{1, 3}, []int{open, kept})

	marked, err = notes.Done(nil, true)
	require.NoError(t, err)
	assert.Equal(t, 1, marked, "all marks every open note")
	assert.Empty(t, notes.List(false))
}

func TestNotesClear(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	notes := devtools.NewNotes(10)
	require.NoError(t, notes.Open(dir))
	keep(t, notes, "a", []byte("png"))
	keep(t, notes, "b", nil)
	testutil.WriteFile(t, filepath.Join(dir, "README"), "not a note")

	n, err := notes.Clear()
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Empty(t, notes.List(true))
	assert.Equal(t, 0, notes.Len())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the notes' own files go")
	assert.Equal(t, "README", entries[0].Name())
}

func TestNotesKeepTheLastOnes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	notes := devtools.NewNotes(2)
	require.NoError(t, notes.Open(dir))

	for _, text := range []string{"a", "b", "c"} {
		keep(t, notes, text, []byte(text))
	}

	assert.Equal(t, []string{"b", "c"}, texts(notes.List(true)))
	assert.NoFileExists(t, filepath.Join(dir, "1.json"), "the oldest goes from the folder too")
	assert.NoFileExists(t, filepath.Join(dir, "1.png"))
}

func TestNotesInMemoryUntilOpen(t *testing.T) {
	t.Parallel()

	notes := devtools.NewNotes(10)
	keep(t, notes, "early", []byte("png"))
	assert.Equal(t, []byte("png"), notes.List(true)[0].Shot, "kept in memory, picture and all")

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "1.json"), `{"n":1,"text":"on disk"}`)
	testutil.WriteFile(t, filepath.Join(dir, "2.json"), `{"n":2,"text":"also on disk"}`)

	require.NoError(t, notes.Open(dir))

	assert.Equal(t, []string{"early", "also on disk"}, texts(notes.List(true)),
		"a note in memory takes the place of the one on disk of its number")
	assert.FileExists(t, filepath.Join(dir, "1.png"), "and goes on the disk")
}

func TestNotesSkipWhatTheyCannotRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "1.json"), `{"n":1,"text":"fine"}`)
	testutil.WriteFile(t, filepath.Join(dir, "2.json"), `{not json`)
	testutil.WriteFile(t, filepath.Join(dir, "3.json"), `{"n":4,"text":"misnamed"}`)
	testutil.WriteFile(t, filepath.Join(dir, "notes.json"), `{"n":5}`)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "6.json"), 0o700))

	notes := devtools.NewNotes(10)
	require.NoError(t, notes.Open(dir))

	assert.Equal(t, []string{"fine"}, texts(notes.List(true)))
}

func TestNotesSayWhyTheyCannotOpen(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "taken")
	testutil.WriteFile(t, file, "a file where the folder would be")

	require.ErrorContains(t, devtools.NewNotes(1).Open(file), "make the notes' folder")
}

func TestNotesFollow(t *testing.T) {
	t.Parallel()

	notes := devtools.NewNotes(10)

	next, stop := notes.Follow()
	defer stop()

	keep(t, notes, "new", []byte("png"))

	note := <-next
	assert.Equal(t, "new", note.Text)
	assert.Equal(t, []byte("png"), note.Shot, "a follower gets the picture too")
}
