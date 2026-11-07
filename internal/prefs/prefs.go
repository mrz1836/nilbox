// Package prefs owns the prefs table: the user's settings and the app state that has no
// table of its own, each stored as text under a string key.
//
// Any package may read any key, but every key has one writer, the package that declares
// it as a constant:
//
//   - settings writes the user's settings, among them the rules that the trust, snooze,
//     sensitive, and notifyprefs packages declare and decode (trust.*, snooze.*, files.*,
//     and notify.*).
//   - inbox writes inbox.*: the account and status of the last refresh, and the cached
//     avatar.
//   - prompts writes prompts.*: the default prompts and the starters already added.
//
// A Store from New runs each call on the pool. One joined to a transaction with WithTx
// runs every call inside it, so prefs are written together with another owner's tables
// and roll back with them. Update reads a value and writes the next one in a single
// transaction, which the store begins IMMEDIATE, so two updates of one key never lose
// either change.
package prefs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/prefs/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// Store reads and writes the prefs table.
type Store struct {
	// db is the pool a Store from New runs on. It is nil for a Store joined to a
	// transaction, whose queries run on that transaction.
	db    *sql.DB
	q     *queries.Queries
	clock clock.Clock
}

// New returns a Store over the pool db. Each write stamps the row's updated_at from c; a
// nil c is the system clock.
func New(db *sql.DB, c clock.Clock) *Store {
	return &Store{db: db, q: queries.New(db), clock: clock.Or(c)}
}

// WithTx returns a Store that runs every call inside tx, a transaction on the same
// database, so its reads see what tx wrote and its writes commit or roll back with tx.
// SetMany and Update then run in tx rather than in a transaction of their own.
func (s *Store) WithTx(tx *sql.Tx) *Store {
	return &Store{q: s.q.WithTx(tx), clock: s.clock}
}

// Get returns the value stored under key. ok is false, with an empty value, when none is
// stored.
func (s *Store) Get(ctx context.Context, key string) (value string, ok bool, err error) {
	return get(ctx, s.q, key)
}

// GetMany returns the values stored under keys, by key. A key with no value is left out,
// and no keys read nothing.
func (s *Store) GetMany(ctx context.Context, keys ...string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}

	rows, err := s.q.GetMany(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}

	out := make(map[string]string, len(rows))
	for _, r := range rows {
		out[r.Key] = r.Value
	}

	return out, nil
}

// GetList returns the list stored under key as a JSON array of strings. ok is false, with
// a nil list, when no value or an empty one is stored. A value that is not such an array
// is an error.
func (s *Store) GetList(ctx context.Context, key string) (list []string, ok bool, err error) {
	raw, _, err := s.Get(ctx, key)
	if err != nil || raw == "" {
		return nil, false, err
	}

	if list, err = store.DecodeJSON[[]string](raw); err != nil {
		return nil, false, fmt.Errorf("read setting %s: %w", key, err)
	}

	return list, true, nil
}

// Set stores value under key, replacing any value stored before.
func (s *Store) Set(ctx context.Context, key, value string) error {
	return set(ctx, s.q, key, value, s.now())
}

// SetMany stores each value under its key, all of them or none: they are written in one
// transaction, or in the one the Store joined, and share one updated_at.
func (s *Store) SetMany(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}

	now := s.now()

	return s.inTx(ctx, func(q *queries.Queries) error {
		for _, key := range slices.Sorted(maps.Keys(values)) {
			if err := set(ctx, q, key, values[key], now); err != nil {
				return err
			}
		}

		return nil
	})
}

// SetList stores list under key as a JSON array; a nil list is stored as an empty one.
func (s *Store) SetList(ctx context.Context, key string, list []string) error {
	value, err := store.EncodeJSON(list)
	if err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}

	return s.Set(ctx, key, value)
}

// Delete removes the value stored under key, so readers fall back to their default. A key
// with no value is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := s.q.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete setting %s: %w", key, err)
	}

	return nil
}

// Update replaces the value stored under key with what fn returns for it. fn gets the
// stored value and whether there is one; the read and the write run in one transaction,
// or in the one the Store joined, so no other write to the key comes between them. When
// fn returns an error, nothing is written and Update returns that error as it is.
func (s *Store) Update(ctx context.Context, key string, fn func(value string, ok bool) (string, error)) error {
	return s.inTx(ctx, func(q *queries.Queries) error {
		value, ok, err := get(ctx, q, key)
		if err != nil {
			return err
		}

		next, err := fn(value, ok)
		if err != nil {
			return err
		}

		return set(ctx, q, key, next, s.now())
	})
}

// inTx runs fn on the transaction the Store joined, or else in a transaction of its own.
func (s *Store) inTx(ctx context.Context, fn func(q *queries.Queries) error) error {
	if s.db == nil {
		return fn(s.q)
	}

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		return fn(s.q.WithTx(tx))
	})
}

// now is the time a write stamps, in UTC so stored timestamps sort as text.
func (s *Store) now() time.Time {
	return s.clock.Now().UTC()
}

// get reads the value stored under key with q.
func get(ctx context.Context, q *queries.Queries, key string) (string, bool, error) {
	value, err := q.Get(ctx, key)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("read setting %s: %w", key, err)
	default:
		return value, true, nil
	}
}

// set stores value under key with q, stamped now.
func set(ctx context.Context, q *queries.Queries, key, value string, now time.Time) error {
	if err := q.Set(ctx, queries.SetParams{Key: key, Value: value, UpdatedAt: now}); err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}

	return nil
}
