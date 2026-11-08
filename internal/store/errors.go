package store

import (
	"errors"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	// primaryCodeMask keeps an SQLite result code's primary part: an extended code
	// carries it in its low byte.
	primaryCodeMask = 0xff

	// foreignKeyMessage is how SQLite words every foreign key failure.
	foreignKeyMessage = "FOREIGN KEY constraint failed"
)

// IsForeignKeyViolation reports whether err, from a statement, is SQLite refusing it
// because it would break a foreign key: deleting a row that another table's RESTRICT key
// still names, or naming a row that is not there. An owner maps it to its own error,
// such as a record still in use.
func IsForeignKeyViolation(err error) bool {
	var driverErr *sqlite.Error
	if !errors.As(err, &driverErr) {
		return false
	}

	switch driverErr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY:
		return true
	case sqlite3.SQLITE_CONSTRAINT_TRIGGER:
		// SQLite runs a RESTRICT action as a built-in trigger, so it fails the way a
		// trigger does; only its message tells it from one of the schema's triggers.
		return strings.Contains(driverErr.Error(), foreignKeyMessage)
	default:
		return false
	}
}

// IsBusy reports whether err is SQLite finding the database locked by another connection
// or process, such as another copy of the app, past the wait every connection allows.
func IsBusy(err error) bool {
	switch sqliteCode(err) {
	case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED:
		return true
	default:
		return false
	}
}

// sqliteCode returns the primary SQLite result code of the driver error in err's chain,
// or SQLITE_OK when it holds none.
func sqliteCode(err error) int {
	var driverErr *sqlite.Error
	if !errors.As(err, &driverErr) {
		return sqlite3.SQLITE_OK
	}

	return driverErr.Code() & primaryCodeMask
}
