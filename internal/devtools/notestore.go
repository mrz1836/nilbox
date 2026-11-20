package devtools

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

const (
	// noteFilePerm and noteDirPerm keep the notes, and the folder they are in, to the user
	// alone.
	noteFilePerm = 0o600
	noteDirPerm  = 0o700
)

// Notes keeps the notes the user leaves, the last limit of them, oldest first: in a folder
// once it has one (Open), each as <n>.json with its picture as <n>.png, so they outlive the
// app, and in memory alone until then. It hands each new note to its followers. It is safe
// for concurrent use.
type Notes struct {
	mu    sync.Mutex
	dir   string
	limit int
	kept  []Note
	last  int

	// pictures holds each note's picture while there is no folder to keep it in.
	pictures map[int][]byte

	// feed hands each new note, with its picture, to the notes' followers.
	feed *Feed[Note]
}

// NewNotes returns notes that keep the last maxNotes notes, in memory until Open.
func NewNotes(maxNotes int) *Notes {
	return &Notes{limit: max(maxNotes, 1), pictures: map[int][]byte{}, feed: NewFeed[Note](1)}
}

// Open keeps the notes in dir from now on, making it when it is missing, and reads back
// the notes kept there, the last limit of them; a note it can't read is left out. Notes kept
// in memory before go on the disk too.
func (s *Notes) Open(dir string) error {
	if err := os.MkdirAll(dir, noteDirPerm); err != nil {
		return fmt.Errorf("make the notes' folder: %w", err)
	}

	read, err := readNotes(dir)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.dir = dir

	// The app opens the folder before its window, where every note comes from, so memory
	// holds none; one it does hold takes the place of the note on disk of its number.
	var errs []error

	for _, note := range s.kept {
		errs = append(errs, s.write(note, s.pictures[note.N]))
	}

	inMemory := s.kept
	read = slices.DeleteFunc(read, func(r Note) bool {
		return slices.ContainsFunc(inMemory, func(m Note) bool { return m.N == r.N })
	})

	s.pictures = map[int][]byte{}

	s.kept = append(read, inMemory...)
	slices.SortStableFunc(s.kept, func(a, b Note) int { return a.N - b.N })

	for _, note := range s.kept {
		s.last = max(s.last, note.N)
	}

	errs = append(errs, s.prune())

	return errors.Join(errs...)
}

// Reserve returns the number the next note takes.
func (s *Notes) Reserve() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.last++

	return s.last
}

// Add keeps note, with its picture in note.Shot, dropping the oldest beyond the limit, and
// hands it to the followers. A note it can't write to the folder is still kept in memory,
// and the error says why.
func (s *Notes) Add(note Note) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.last = max(s.last, note.N)

	picture := note.Shot
	note.Shot = nil

	var err error

	if s.dir == "" {
		s.pictures[note.N] = picture
	} else {
		err = s.write(note, picture)
	}

	s.kept = append(s.kept, note)

	note.Shot = picture
	s.feed.Add(note)

	return errors.Join(err, s.prune())
}

// List returns the notes kept, oldest first, each with its picture: the open ones, or all
// of them with all.
func (s *Notes) List(all bool) []Note {
	s.mu.Lock()
	dir := s.dir
	notes := make([]Note, 0, len(s.kept))

	for _, note := range s.kept {
		if all || !note.Done {
			note.Shot = s.pictures[note.N]
			notes = append(notes, note)
		}
	}
	s.mu.Unlock()

	// The pictures are read outside the lock: they are the largest part of a note.
	if dir != "" {
		for i := range notes {
			notes[i].Shot = readPicture(dir, notes[i].N)
		}
	}

	return notes
}

// Done marks done the open notes numbered ns, or every open note when all, and returns how
// many it marked.
func (s *Notes) Done(ns []int, all bool) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	marked := 0

	var errs []error

	for i, note := range s.kept {
		if note.Done || (!all && !slices.Contains(ns, note.N)) {
			continue
		}

		s.kept[i].Done = true
		marked++

		if s.dir != "" {
			errs = append(errs, writeJSON(s.dir, s.kept[i]))
		}
	}

	return marked, errors.Join(errs...)
}

// Clear drops every note kept, from the folder too, and returns how many there were.
func (s *Notes) Clear() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(s.kept)

	errs := make([]error, 0, n)
	for _, note := range s.kept {
		errs = append(errs, s.remove(note.N))
	}

	s.kept, s.pictures = nil, map[int][]byte{}

	return n, errors.Join(errs...)
}

// Counts returns how many of the notes kept are open, and how many there are.
func (s *Notes) Counts() (open, kept int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, note := range s.kept {
		if !note.Done {
			open++
		}
	}

	return open, len(s.kept)
}

// Len returns how many notes are kept.
func (s *Notes) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.kept)
}

// Follow returns a channel that receives each note added from now on, with its picture,
// and the function that stops it, as Feed.Follow.
func (s *Notes) Follow() (<-chan Note, func()) {
	return s.feed.Follow()
}

// prune drops the oldest notes beyond the limit, from the folder too.
func (s *Notes) prune() error {
	var errs []error

	for len(s.kept) > s.limit {
		errs = append(errs, s.remove(s.kept[0].N))
		s.kept = s.kept[1:]
	}

	return errors.Join(errs...)
}

// write writes note and its picture, if it has one, to the folder.
func (s *Notes) write(note Note, picture []byte) error {
	if s.dir == "" {
		return nil
	}

	var errs []error
	if len(picture) > 0 {
		errs = append(errs, writeAtomic(filepath.Join(s.dir, strconv.Itoa(note.N)+".png"), picture))
	}

	return errors.Join(append(errs, writeJSON(s.dir, note))...)
}

// remove forgets note n's picture, and takes it and its picture out of the folder.
func (s *Notes) remove(n int) error {
	delete(s.pictures, n)

	if s.dir == "" {
		return nil
	}

	var errs []error

	for _, name := range []string{strconv.Itoa(n) + ".json", strconv.Itoa(n) + ".png"} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove note %d: %w", n, err))
		}
	}

	return errors.Join(errs...)
}

// readNotes reads the notes kept in dir, each <n>.json, oldest first, leaving out a file
// it can't read or whose number is not its name's.
func readNotes(dir string) ([]Note, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read the notes' folder: %w", err)
	}

	notes := make([]Note, 0, len(entries))

	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		n, numErr := strconv.Atoi(name)

		if !ok || numErr != nil || !entry.Type().IsRegular() {
			continue
		}

		data, readErr := os.ReadFile(filepath.Join(dir, entry.Name())) //nolint:gosec // the inspector's own folder
		if readErr != nil {
			continue
		}

		var note Note
		if json.Unmarshal(data, &note) != nil || note.N != n {
			continue
		}

		notes = append(notes, note)
	}

	slices.SortFunc(notes, func(a, b Note) int { return a.N - b.N })

	return notes, nil
}

// readPicture returns note n's picture from dir, or nil when there is none.
func readPicture(dir string, n int) []byte {
	data, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(n)+".png")) //nolint:gosec // the inspector's own folder
	if err != nil {
		return nil
	}

	return data
}

// writeJSON writes note, without its picture, as <n>.json in dir.
func writeJSON(dir string, note Note) error {
	note.Shot = nil

	data, err := json.MarshalIndent(note, "", "  ")
	if err != nil {
		return fmt.Errorf("encode note %d: %w", note.N, err)
	}

	return writeAtomic(filepath.Join(dir, strconv.Itoa(note.N)+".json"), append(data, '\n'))
}

// writeAtomic writes data to path through a file beside it, so a reader never sees half of
// it.
func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, data, noteFilePerm); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	return nil
}
