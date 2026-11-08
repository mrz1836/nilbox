package worklog

import "database/sql"

// NewKeeping returns a Store over db that keeps only the newest keep cleanups, so a test
// can run past the cap with few writes.
func NewKeeping(db *sql.DB, keep int64) *Store {
	s := New(db)
	s.keepCleanups = keep

	return s
}
