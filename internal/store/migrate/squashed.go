package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
)

// squashedFiles holds the squashed histories shipped with the binary. Each NNNN.json
// sits beside NNNN.schema, the schema its migrations built, which only the tests read.
//
//go:embed squashed/*.json
var squashedFiles embed.FS

var (
	// squashedNamePattern matches NNNN.json and captures NNNN, the history's newest version.
	squashedNamePattern = regexp.MustCompile(`^(\d{4})\.json$`)

	// migrationNamePattern matches the <name> part of a migration's file name.
	migrationNamePattern = regexp.MustCompile(`^[a-z0-9_]+$`)

	// checksumPattern matches the hex SHA-256 of a migration's file.
	checksumPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

	// errHistoryChanged is returned by a carry over that found the history changed since
	// it was read.
	errHistoryChanged = errors.New("the migration history changed while it was read")
)

// squashedHistory is a migration history that builds recorded, of migrations a squash
// since combined into this build's base migrations. A database whose history is exactly
// this one is carried over: its schema is the one the base migrations through Through
// build, which the tests check against the schema the history's migrations built.
type squashedHistory struct {
	// Through is the newest base migration whose schema equals the one the history's
	// migrations built.
	Through int `json:"through"`

	// About says which builds recorded the history, for whoever reads the file.
	About string `json:"about"`

	// Migrations is the history, oldest first, as schema_migrations recorded it.
	Migrations []appliedMigration `json:"migrations"`
}

// embeddedSquashed returns the squashed histories shipped with the binary.
func embeddedSquashed() fs.FS {
	sub, err := fs.Sub(squashedFiles, "squashed")
	if err != nil {
		// fs.Sub fails only for an invalid directory name, and this one is a constant.
		return squashedFiles
	}

	return sub
}

// loadAll reads the migrations in opts.Source and the squashed histories in
// opts.Squashed, checking each history against the migrations.
func loadAll(opts Options) ([]Migration, []squashedHistory, error) {
	migrations, err := Load(opts.Source)
	if err != nil {
		return nil, nil, err
	}

	if opts.Squashed == nil {
		return migrations, nil, nil
	}

	histories, err := loadSquashed(opts.Squashed, migrations)
	if err != nil {
		return nil, nil, err
	}

	return migrations, histories, nil
}

// loadSquashed reads and validates the squashed histories in src against migrations,
// sorted by version.
func loadSquashed(src fs.FS, migrations []Migration) ([]squashedHistory, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, fmt.Errorf("read squashed histories: %w", err)
	}

	var histories []squashedHistory

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		h, err := loadSquashedFile(src, entry.Name(), migrations)
		if err != nil {
			return nil, err
		}

		histories = append(histories, h)
	}

	return histories, nil
}

// loadSquashedFile reads one squashed history and checks it: named for its newest
// version, its versions rising and all older than the oldest migration, each with a name
// and a checksum as a migration file has, and carried over through a migration there is.
func loadSquashedFile(src fs.FS, file string, migrations []Migration) (squashedHistory, error) {
	match := squashedNamePattern.FindStringSubmatch(file)
	if match == nil {
		return squashedHistory{}, invalidSquashed(file, "want NNNN.json, NNNN its newest version")
	}

	body, err := fs.ReadFile(src, file)
	if err != nil {
		return squashedHistory{}, fmt.Errorf("read squashed history %s: %w", file, err)
	}

	var h squashedHistory

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()

	if err = dec.Decode(&h); err != nil {
		return squashedHistory{}, invalidSquashed(file, "%v", err)
	}

	if err = h.check(file, migrations); err != nil {
		return squashedHistory{}, err
	}

	if named, _ := strconv.Atoi(match[1]); named != h.newest() {
		return squashedHistory{}, invalidSquashed(file, "its newest version is %d", h.newest())
	}

	return h, nil
}

// invalidSquashed is ErrInvalidMigration for the squashed history in file, saying why.
func invalidSquashed(file, format string, args ...any) error {
	return fmt.Errorf("%w: squashed history %s: %s", ErrInvalidMigration, file, fmt.Sprintf(format, args...))
}

// check checks h, read from file, against migrations: its versions rise, all older than
// the oldest migration, each with a name and a checksum as a migration file has, and it is
// carried over through a migration there is.
func (h squashedHistory) check(file string, migrations []Migration) error {
	if len(h.Migrations) == 0 {
		return invalidSquashed(file, "it lists no migrations")
	}

	oldest := 0
	if len(migrations) > 0 {
		oldest = migrations[0].Version
	}

	prev := 0

	for _, m := range h.Migrations {
		switch {
		case m.Version <= prev:
			return invalidSquashed(file, "version %d does not follow %d", m.Version, prev)
		case m.Version >= oldest:
			return invalidSquashed(file, "version %d is not older than the oldest migration, %d: "+
				"a squash numbers its base above every version a build shipped", m.Version, oldest)
		case !migrationNamePattern.MatchString(m.Name):
			return invalidSquashed(file, "version %d has no name a migration file could have", m.Version)
		case !checksumPattern.MatchString(m.Checksum):
			return invalidSquashed(file, "version %d has no SHA-256 checksum", m.Version)
		}

		prev = m.Version
	}

	if !slices.ContainsFunc(migrations, func(m Migration) bool { return m.Version == h.Through }) {
		return invalidSquashed(file, "it is carried over through %d, which is no migration this build has", h.Through)
	}

	return nil
}

// newest returns the newest version of h, which lists at least one.
func (h squashedHistory) newest() int { return h.Migrations[len(h.Migrations)-1].Version }

// recordedBy reports whether applied is exactly h: every version with the same checksum,
// and no other.
func (h squashedHistory) recordedBy(applied map[int]appliedMigration) bool {
	if len(h.Migrations) != len(applied) {
		return false
	}

	for _, m := range h.Migrations {
		if a, ok := applied[m.Version]; !ok || a.Checksum != m.Checksum {
			return false
		}
	}

	return true
}

// plan is what Apply does to a database: carry its squashed history over, when it has
// one, then apply the pending migrations.
type plan struct {
	squashed *squashedHistory
	pending  []Migration
}

// empty reports whether the plan leaves the database as it is.
func (p plan) empty() bool { return p.squashed == nil && len(p.pending) == 0 }

// makePlan works out the plan for a database that recorded applied, or the error Apply
// refuses it with.
func makePlan(migrations []Migration, histories []squashedHistory, applied map[int]appliedMigration) (plan, error) {
	i := slices.IndexFunc(histories, func(h squashedHistory) bool { return h.recordedBy(applied) })
	if i < 0 {
		pending, err := verify(migrations, applied)

		return plan{pending: pending}, err
	}

	h := &histories[i]
	carried := make(map[int]appliedMigration, len(migrations))

	for _, m := range migrations {
		if m.Version <= h.Through {
			carried[m.Version] = appliedMigration{Version: m.Version, Name: m.Name, Checksum: m.Checksum}
		}
	}

	pending, err := verify(migrations, carried)

	return plan{squashed: h, pending: pending}, err
}

// carryOver rewrites the record of a database whose history is h to the migrations
// through h.Through, in one transaction, and touches nothing else: those migrations build
// the schema h's built. It deletes h's rows alone, and rolls back unless they were all the
// rows there were, so a history that changed since it was read is left as it was.
func carryOver(ctx context.Context, db *sql.DB, h squashedHistory, migrations []Migration, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("carry over the squashed history: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if err = forgetSquashed(ctx, tx, h); err != nil {
		return fmt.Errorf("carry over the squashed history: %w", err)
	}

	for _, m := range migrations {
		if m.Version > h.Through {
			break
		}

		_, err = tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
			m.Version, m.Name, m.Checksum, clock.Format(now))
		if err != nil {
			return fmt.Errorf("carry over the squashed history: record %s: %w", m.File, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("carry over the squashed history: %w", err)
	}

	return nil
}

// forgetSquashed deletes h's rows of schema_migrations in tx, and fails unless each was
// there as h has it and no other row is left.
func forgetSquashed(ctx context.Context, tx *sql.Tx, h squashedHistory) error {
	for _, m := range h.Migrations {
		res, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = ? AND checksum = ?`,
			m.Version, m.Checksum)
		if err != nil {
			return fmt.Errorf("forget version %d: %w", m.Version, err)
		}

		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("forget version %d: %w", m.Version, err)
		}

		if n != 1 {
			return fmt.Errorf("%w: version %d", errHistoryChanged, m.Version)
		}
	}

	var left int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&left); err != nil {
		return fmt.Errorf("count the history left: %w", err)
	}

	if left != 0 {
		return fmt.Errorf("%w: %d more migrations", errHistoryChanged, left)
	}

	return nil
}

// Schemas says which databases a build opens, by the newest migration each records, as a
// backup's name gives it (-v<schema>).
type Schemas struct {
	// Oldest and Newest are the build's oldest and newest migrations: a database whose
	// newest is between them, both included, is one of its own, which it brings up to date.
	Oldest, Newest int

	// Squashed holds the newest version of each squashed history the build carries over.
	Squashed []int
}

// Opens reports whether a database whose newest migration is schema opens: a newer one was
// saved by a newer build, and an older one that ends no squashed history records
// migrations the build squashed and can't carry over.
func (s Schemas) Opens(schema int) bool {
	return (schema > 0 && schema >= s.Oldest && schema <= s.Newest) || slices.Contains(s.Squashed, schema)
}

// KnownSchemas returns the Schemas of the migrations and squashed histories opts names.
func KnownSchemas(opts Options) (Schemas, error) {
	migrations, histories, err := loadAll(opts.withDefaults())
	if err != nil {
		return Schemas{}, err
	}

	s := Schemas{Squashed: make([]int, 0, len(histories))}

	if len(migrations) > 0 {
		s.Oldest, s.Newest = migrations[0].Version, migrations[len(migrations)-1].Version
	}

	for _, h := range histories {
		s.Squashed = append(s.Squashed, h.newest())
	}

	return s, nil
}
