package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

const (
	// snapshotParams open a snapshot read in place: read-only, and immutable, so SQLite
	// takes no lock and creates no file beside it. Only a file with no write-ahead log
	// is read this way, since an immutable database ignores its log.
	snapshotParams = "mode=ro&immutable=1&_pragma=query_only(1)"

	// snapshotCopyParams open the scratch copy of a snapshot that has a write-ahead log:
	// read-only, letting SQLite read the log, and create the index it needs to, in the
	// scratch directory.
	snapshotCopyParams = "mode=ro&_pragma=query_only(1)"

	// snapshotCopyName is the name of a snapshot's copy in its scratch directory.
	snapshotCopyName = "snapshot.db"
)

var (
	// ErrNoTable is returned when a snapshot has no table of the name asked for.
	ErrNoTable = errors.New("the database has no such table")

	// ErrNotIdentifier is returned for a table or column name that is not a plain SQL
	// identifier.
	ErrNotIdentifier = errors.New("not a plain SQL identifier")

	// ErrNotAFile is returned when a snapshot's path is not a regular file.
	ErrNotAFile = errors.New("not a file")
)

// identifierPattern matches a plain SQL identifier, which needs no escaping inside double
// quotes.
var identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SnapshotKind says how a database kept beside the app's own came to be.
type SnapshotKind string

const (
	// SnapshotSetAside is a database Open could not use and set aside.
	SnapshotSetAside SnapshotKind = "set-aside"

	// SnapshotBackup is a copy taken before migrations changed the database.
	SnapshotBackup SnapshotKind = "backup"
)

// SnapshotFile is a database file kept beside the app's own.
type SnapshotFile struct {
	// Path is the file.
	Path string

	// Kind says how it came to be.
	Kind SnapshotKind

	// At is when it was set aside or backed up, read from its name, in UTC.
	At time.Time
}

// Snapshots lists the databases kept beside the database at path, the newest first: the
// ones Open set aside, which are kept until the user removes them, and the backups taken
// before migrations, of which the newest few are kept. A missing directory holds none.
func Snapshots(path string) ([]SnapshotFile, error) {
	dir, base := filepath.Split(path)

	entries, err := os.ReadDir(cmp.Or(dir, "."))
	if errors.Is(err, fs.ErrNotExist) {
		return []SnapshotFile{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("list the database's backups: %w", err)
	}

	found := []SnapshotFile{}

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}

		if file, ok := snapshotFile(base, entry.Name()); ok {
			file.Path = filepath.Join(dir, entry.Name())
			found = append(found, file)
		}
	}

	slices.SortFunc(found, func(a, b SnapshotFile) int {
		return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.Path, a.Path))
	})

	return found, nil
}

// snapshotFile reads the kind and time of a snapshot of the database named base from the
// file name name, and reports whether name is one. The -wal and -shm files beside a
// snapshot are not snapshots themselves.
func snapshotFile(base, name string) (SnapshotFile, bool) {
	if strings.HasSuffix(name, "-wal") || strings.HasSuffix(name, "-shm") {
		return SnapshotFile{}, false
	}

	kinds := []struct {
		prefix string
		kind   SnapshotKind
	}{
		{base + asideInfix, SnapshotSetAside},
		{base + migrate.BackupInfix, SnapshotBackup},
	}

	for _, k := range kinds {
		rest, ok := strings.CutPrefix(name, k.prefix)
		if !ok || len(rest) < len(clock.StampLayout) {
			continue
		}

		at, err := time.Parse(clock.StampLayout, rest[:len(clock.StampLayout)])
		if err != nil {
			continue
		}

		return SnapshotFile{Kind: k.kind, At: at.UTC()}, true
	}

	return SnapshotFile{}, false
}

// Snapshot is a read-only view of a database file the app keeps but no longer opens as
// its own: one Open set aside, a backup taken before migrations, or a copy the user
// kept. Any build may have written it, older or newer than this one, so Rows reads only
// the columns it is asked for that the file has.
//
// Reading one changes nothing on disk. A file with no write-ahead log beside it is read
// in place, immutable; one with a log is copied, with its log, to a scratch directory
// that Close removes, so SQLite never writes beside the original.
type Snapshot struct {
	db *sql.DB

	// scratch is the scratch directory holding the copy, or "" for a file read in place.
	scratch string
}

// OpenSnapshot opens the database file at path for reading. It returns an error for a
// path that is not a regular file, or a file that is not a database or is damaged. The
// caller closes the snapshot.
func OpenSnapshot(ctx context.Context, path string) (*Snapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open the backup: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("open the backup %s: %w", path, ErrNotAFile)
	}

	s := &Snapshot{}
	source, params := path, snapshotParams

	if _, err = os.Lstat(path + "-wal"); err == nil {
		if s.scratch, err = copySnapshot(path); err != nil {
			return nil, err
		}

		source, params = filepath.Join(s.scratch, snapshotCopyName), snapshotCopyParams
	}

	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(source), RawQuery: params}).String()

	if s.db, err = sql.Open(driverName, dsn); err != nil {
		return nil, errors.Join(fmt.Errorf("open the backup: %w", err), s.removeScratch())
	}

	s.db.SetMaxOpenConns(1)

	// Reading the schema now reports a file that is not a database, or is damaged, here
	// rather than at the first read.
	var tables int
	if err = s.db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&tables); err != nil {
		return nil, errors.Join(fmt.Errorf("read the backup: %w", err), s.Close())
	}

	return s, nil
}

// DB returns the snapshot's read-only connection pool, for an owner whose own queries
// read a table every build has kept the same, such as the prefs table.
func (s *Snapshot) DB() *sql.DB {
	return s.db
}

// Rows reads every row of table, holding only the columns named in columns that the
// table has: a column a build before or after this one lacks is left out of every row,
// and reads as its empty value. A table the snapshot does not have is ErrNoTable.
func (s *Snapshot) Rows(ctx context.Context, table string, columns ...string) ([]Row, error) {
	if err := checkIdentifiers(append([]string{table}, columns...)); err != nil {
		return nil, err
	}

	have, err := s.columns(ctx, table)
	if err != nil {
		return nil, err
	}

	if len(have) == 0 {
		return nil, fmt.Errorf("read the backup: %w: %s", ErrNoTable, table)
	}

	picked := slices.DeleteFunc(slices.Clone(columns), func(c string) bool { return !have[c] })

	rows, err := s.db.QueryContext(ctx, "SELECT "+selectList(picked)+` FROM "`+table+`"`) //nolint:gosec // identifiers checked above
	if err != nil {
		return nil, fmt.Errorf("read the backup's %s: %w", table, err)
	}

	defer func() { _ = rows.Close() }()

	out := []Row{}

	for rows.Next() {
		row, scanErr := scanRow(rows, picked)
		if scanErr != nil {
			return nil, fmt.Errorf("read the backup's %s: %w", table, scanErr)
		}

		out = append(out, row)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the backup's %s: %w", table, err)
	}

	return out, nil
}

// Close closes the snapshot and removes its scratch copy, if it has one.
func (s *Snapshot) Close() error {
	var err error
	if s.db != nil {
		err = s.db.Close()
	}

	return errors.Join(err, s.removeScratch())
}

// columns returns the set of table's column names; it is empty when there is no such
// table.
func (s *Snapshot) columns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return nil, fmt.Errorf("read the backup's schema: %w", err)
	}

	defer func() { _ = rows.Close() }()

	have := map[string]bool{}

	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read the backup's schema: %w", err)
		}

		have[name] = true
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read the backup's schema: %w", err)
	}

	return have, nil
}

// checkIdentifiers returns ErrNotIdentifier unless every name is a plain SQL identifier.
func checkIdentifiers(names []string) error {
	for _, name := range names {
		if !identifierPattern.MatchString(name) {
			return fmt.Errorf("read the backup: %q: %w", name, ErrNotIdentifier)
		}
	}

	return nil
}

// selectList is the quoted list of columns to select, or 1 when there are none, so each
// row is still counted.
func selectList(columns []string) string {
	if len(columns) == 0 {
		return "1"
	}

	return `"` + strings.Join(columns, `", "`) + `"`
}

// removeScratch removes the scratch directory, if there is one.
func (s *Snapshot) removeScratch() error {
	if s.scratch == "" {
		return nil
	}

	if err := os.RemoveAll(s.scratch); err != nil {
		return fmt.Errorf("remove the backup's scratch copy: %w", err)
	}

	s.scratch = ""

	return nil
}

// scanRow reads the current row's values into a Row keyed by columns.
func scanRow(rows *sql.Rows, columns []string) (Row, error) {
	values := make([]any, len(columns))
	ptrs := make([]any, len(columns))

	for i := range values {
		ptrs[i] = &values[i]
	}

	if len(columns) > 0 {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
	} else {
		var one int
		if err := rows.Scan(&one); err != nil {
			return nil, err
		}
	}

	row := make(Row, len(columns))
	for i, name := range columns {
		row[name] = values[i]
	}

	return row, nil
}

// copySnapshot copies the database at path and its write-ahead log to a new, user-only
// scratch directory, and returns the directory.
func copySnapshot(path string) (string, error) {
	scratch, err := os.MkdirTemp("", brand.Slug+"-backup-")
	if err != nil {
		return "", fmt.Errorf("copy the backup to read it: %w", err)
	}

	target := filepath.Join(scratch, snapshotCopyName)

	for _, suffix := range []string{"", "-wal"} {
		if err = copyFile(path+suffix, target+suffix); err != nil {
			return "", errors.Join(fmt.Errorf("copy the backup to read it: %w", err), os.RemoveAll(scratch))
		}
	}

	return scratch, nil
}

// copyFile copies the file at from to a new, user-only file at to.
func copyFile(from, to string) error {
	src, err := os.Open(from) //nolint:gosec // a database file the app keeps, or one the user chose
	if err != nil {
		return err
	}

	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, appdata.PrivateFilePerm) //nolint:gosec // a new file in the app's scratch directory
	if err != nil {
		return err
	}

	if _, err = io.Copy(dst, src); err != nil {
		return errors.Join(err, dst.Close())
	}

	return dst.Close()
}
