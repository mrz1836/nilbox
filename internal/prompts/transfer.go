package prompts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// sqliteHeader starts every SQLite database file.
const sqliteHeader = "SQLite format 3\x00"

var (
	// ErrUnknownBackup is returned when Import is pointed at a backup Backups does not list.
	ErrUnknownBackup = errors.New("that backup isn't one " + brand.Name + " keeps; choose one from the list")

	// ErrNoPath is returned when a file to export to or import from is not an absolute
	// path.
	ErrNoPath = errors.New("choose a file")

	// ErrUnknownSource is returned for an ImportSource of no kind Import knows.
	ErrUnknownSource = errors.New("the prompts can't be read from there")
)

// SourceKind says what an ImportSource is.
type SourceKind string

// What an ImportSource is.
const (
	// SourceFile is a prompt file the user chose: one Export wrote, or the library file
	// copied from another Mac. A database file the app kept, copied out of a backup, reads
	// as well.
	SourceFile SourceKind = "file"

	// SourceBackup is one of the copies of the library Backups lists.
	SourceBackup SourceKind = "backup"
)

// ImportSource names where Import reads prompts from.
type ImportSource struct {
	Kind SourceKind `json:"kind"`

	// Path is the file: one the user chose, or a Backup's Path.
	Path string `json:"path"`
}

// BackupKind says what a Backup is.
type BackupKind string

// What a Backup is.
const (
	// BackupLibrary is the copy of the library kept beside the database (LibraryFile).
	BackupLibrary BackupKind = "library"

	// BackupSetAside is a database the app could not open, so it set it aside and started
	// a fresh one.
	BackupSetAside BackupKind = "set-aside"

	// BackupUpdate is the database as it was before an update changed it.
	BackupUpdate BackupKind = "update"
)

// Backup is a copy of the prompt library the app keeps, which Import can bring prompts
// back from.
type Backup struct {
	// Path is the file, as an ImportSource names it.
	Path string `json:"path"`

	// Kind says what it is.
	Kind BackupKind `json:"kind"`

	// At is when it was made, as RFC 3339.
	At string `json:"at"`

	// Prompts is how many prompts it holds, and New how many of them importing would add
	// or save over a starter: the ones the library does not hold already.
	Prompts int `json:"prompts"`
	New     int `json:"new"`

	// Error says why it could not be read, or is "" when it could.
	Error string `json:"error"`
}

// backupFile is a copy of the library on disk.
type backupFile struct {
	path string
	kind BackupKind
	at   time.Time
}

// read reads the prompts in the copy.
func (b backupFile) read(ctx context.Context) (Bundle, error) {
	if b.kind == BackupLibrary {
		return readBundleFile(b.path)
	}

	return readSnapshotBundle(ctx, b.path)
}

// Export writes the prompts with ids, or every prompt when ids is empty, to a prompt file
// at path, replacing any file there, and returns how many it wrote. A whole library is
// written with the app's default prompts and the record of the starters it was given, so
// importing it on another Mac, or restoring it here, brings back the library as it was.
func (s *Service) Export(ctx context.Context, path string, ids []int64) (int, error) {
	if path == "" || !filepath.IsAbs(path) {
		return 0, ErrNoPath
	}

	lib, err := s.store.Library(ctx)
	if err != nil {
		return 0, err
	}

	for _, id := range ids {
		if !slices.ContainsFunc(lib.Prompts, func(p Prompt) bool { return p.ID == id }) {
			return 0, fmt.Errorf("%w (id %d)", ErrPromptNotFound, id)
		}
	}

	added, err := s.store.seeded(ctx)
	if err != nil {
		return 0, err
	}

	b := newBundle(lib, added, ids, s.clock.Now(), s.app)

	data, err := encodeBundle(b)
	if err != nil {
		return 0, err
	}

	if err = appdata.WriteFile(path, data); err != nil {
		return 0, fmt.Errorf("export the prompts: %w", err)
	}

	return len(b.Prompts), nil
}

// Backups lists the copies of the prompt library the app keeps, newest first, with how
// many prompts each holds and how many of those the library does not: the library file,
// the databases the app set aside, and the backups it took before updates. A copy that
// cannot be read is listed with the reason.
func (s *Service) Backups(ctx context.Context) ([]Backup, error) {
	files, err := s.backupFiles()
	if err != nil {
		return nil, err
	}

	pl, err := s.planner(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Backup, 0, len(files))

	for _, f := range files {
		backup := Backup{Path: f.path, Kind: f.kind, At: clock.Format(f.at)}

		b, readErr := f.read(ctx)
		if readErr != nil {
			backup.Error = uierr.Message(readErr)
			out = append(out, backup)

			continue
		}

		backup.Prompts = len(b.Prompts)
		backup.New = countNew(clonePlanner(pl), b.Prompts)
		out = append(out, backup)
	}

	return out, nil
}

// PreviewImport reads the prompts a source holds and says what importing each one would
// do, changing nothing.
func (s *Service) PreviewImport(ctx context.Context, source ImportSource) ([]ImportEntry, error) {
	b, err := s.readSource(ctx, source)
	if err != nil {
		return nil, err
	}

	pl, err := s.planner(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]ImportEntry, 0, len(b.Prompts))
	for _, p := range b.Prompts {
		out = append(out, pl.plan(p).ImportEntry)
	}

	return out, nil
}

// Import adds prompts to the library, as PreviewImport said it would, all of them or,
// when a write fails, none. It never changes a prompt the user wrote or edited: a prompt
// the library already holds is skipped, one whose nickname is taken is added under a free
// one, and only a starter still as the app added it is saved over, by the user's own
// version of that starter. A text with problems is imported for the user to fix, and a
// setting this build would refuse is left out.
func (s *Service) Import(ctx context.Context, prompts []BundlePrompt) (ImportResult, error) {
	if len(prompts) > maxBundlePrompts {
		return ImportResult{}, ErrBundleTooLarge
	}

	// The launch defaults are read before the transaction begins, which their store
	// would otherwise wait on.
	usage, err := s.loadUsage(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	var res ImportResult

	err = s.store.inTx(ctx, func(st *Store) error {
		var importErr error

		res, importErr = importInto(ctx, st, prompts, usage)

		return importErr
	})
	if err != nil {
		return ImportResult{}, fmt.Errorf("import the prompts: %w", err)
	}

	if res.Added+res.Replaced > 0 {
		s.saved(ctx)
	}

	return res, nil
}

// planner returns a planner over the library as it stands.
func (s *Service) planner(ctx context.Context) (*planner, error) {
	lib, err := s.store.All(ctx)
	if err != nil {
		return nil, err
	}

	usage, err := s.loadUsage(ctx)
	if err != nil {
		return nil, err
	}

	return newPlanner(lib, usage), nil
}

// readSource reads the prompts source names.
func (s *Service) readSource(ctx context.Context, source ImportSource) (Bundle, error) {
	switch source.Kind {
	case SourceFile:
		return readImportFile(ctx, source.Path)
	case SourceBackup:
		files, err := s.backupFiles()
		if err != nil {
			return Bundle{}, err
		}

		for _, f := range files {
			if f.path == source.Path {
				return f.read(ctx)
			}
		}

		return Bundle{}, ErrUnknownBackup
	default:
		return Bundle{}, fmt.Errorf("%w: %q", ErrUnknownSource, source.Kind)
	}
}

// backupFiles lists the copies of the library on disk, newest first.
func (s *Service) backupFiles() ([]backupFile, error) {
	files, err := s.libraryBackup()
	if err != nil {
		return nil, err
	}

	snapshots, err := s.databaseBackups()
	if err != nil {
		return nil, err
	}

	files = append(files, snapshots...)
	slices.SortStableFunc(files, func(a, b backupFile) int { return b.at.Compare(a.at) })

	return files, nil
}

// libraryBackup lists the library file, when there is one.
func (s *Service) libraryBackup() ([]backupFile, error) {
	if s.file == nil {
		return nil, nil
	}

	info, err := os.Stat(s.file.Path())

	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("find the copy of the prompt library: %w", err)
	case !info.Mode().IsRegular():
		return nil, nil
	default:
		return []backupFile{{path: s.file.Path(), kind: BackupLibrary, at: info.ModTime().UTC()}}, nil
	}
}

// databaseBackups lists the databases kept beside the database: those set aside, and the
// backups taken before updates.
func (s *Service) databaseBackups() ([]backupFile, error) {
	if s.database == "" {
		return nil, nil
	}

	snapshots, err := store.Snapshots(s.database)
	if err != nil {
		return nil, err
	}

	files := make([]backupFile, 0, len(snapshots))

	for _, snap := range snapshots {
		kind := BackupUpdate
		if snap.Kind == store.SnapshotSetAside {
			kind = BackupSetAside
		}

		files = append(files, backupFile{path: snap.Path, kind: kind, at: snap.At})
	}

	return files, nil
}

// readImportFile reads the prompts in the file at path: a prompt file, or a database
// file the app kept.
func readImportFile(ctx context.Context, path string) (Bundle, error) {
	if path == "" || !filepath.IsAbs(path) {
		return Bundle{}, ErrNoPath
	}

	isDB, err := isDatabase(path)
	if err != nil {
		return Bundle{}, err
	}

	if isDB {
		return readSnapshotBundle(ctx, path)
	}

	return readBundleFile(path)
}

// isDatabase reports whether the file at path starts as an SQLite database does.
func isDatabase(path string) (bool, error) {
	f, err := os.Open(path) //nolint:gosec // a file the user chose
	if err != nil {
		return false, fmt.Errorf("read the file: %w", err)
	}

	defer func() { _ = f.Close() }()

	head := make([]byte, len(sqliteHeader))

	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read the file: %w", err)
	}

	return bytes.Equal(head[:n], []byte(sqliteHeader)), nil
}

// clonePlanner returns a copy of pl to plan with, leaving pl as it is.
func clonePlanner(pl *planner) *planner {
	return newPlanner(pl.lib, pl.usage)
}

// countNew counts the prompts that importing would add or save over a starter.
func countNew(pl *planner, prompts []BundlePrompt) int {
	n := 0

	for _, p := range prompts {
		if pl.plan(p).Outcome != ImportSkip {
			n++
		}
	}

	return n
}
