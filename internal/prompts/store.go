package prompts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prompts/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Preference keys the library keeps in the prefs table. The library writes them, and no
// other package.
const (
	// prefSeeded lists, as a JSON array, the starter keys already added once.
	prefSeeded = "prompts.seeded"

	// prefDefaultPR and prefDefaultIssue hold the app's default prompt ID per kind.
	prefDefaultPR    = "prompts.default.pr"
	prefDefaultIssue = "prompts.default.issue"
)

// Store reads and writes the prompt library: the prompts table, which no other package
// touches, and the library's preferences, the app's default prompts among them.
//
// A Store from NewStore runs each call on the pool. One joined to a transaction with
// WithTx runs every call inside it, so a check and the write it allows commit together.
type Store struct {
	// db is the pool a Store from NewStore runs on. It is nil for a Store joined to a
	// transaction, whose queries run on that transaction.
	db    *sql.DB
	q     *queries.Queries
	prefs *prefs.Store
	clock clock.Clock
}

// NewStore returns a Store over the pool db. Writes stamp the rows from c; a nil c is
// the system clock.
func NewStore(db *sql.DB, c clock.Clock) *Store {
	c = clock.Or(c)

	return &Store{db: db, q: queries.New(db), prefs: prefs.New(db, c), clock: c}
}

// WithTx returns a Store that runs every call inside tx, a transaction on the same
// database, so its reads see what tx wrote and its writes commit or roll back with tx.
func (s *Store) WithTx(tx *sql.Tx) *Store {
	return &Store{q: s.q.WithTx(tx), prefs: s.prefs.WithTx(tx), clock: s.clock}
}

// All returns every stored prompt, without usage, sorted by nickname ignoring case and
// then by ID.
func (s *Store) All(ctx context.Context) ([]Prompt, error) {
	rows, err := s.q.ListPrompts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the prompts: %w", err)
	}

	out := make([]Prompt, 0, len(rows))

	for _, row := range rows {
		p, decodeErr := fromRow(row)
		if decodeErr != nil {
			return nil, fmt.Errorf("read the prompts: %w", decodeErr)
		}

		out = append(out, p)
	}

	return out, nil
}

// Lookup returns the stored prompt with id, without its usage, or ErrPromptNotFound.
func (s *Store) Lookup(ctx context.Context, id int64) (Prompt, error) {
	row, err := s.q.GetPrompt(ctx, id)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Prompt{}, fmt.Errorf("%w (id %d)", ErrPromptNotFound, id)
	case err != nil:
		return Prompt{}, fmt.Errorf("read the prompt: %w", err)
	}

	p, err := fromRow(row)
	if err != nil {
		return Prompt{}, fmt.Errorf("read the prompt: %w", err)
	}

	return p, nil
}

// Library returns every stored prompt, as All does, with the app's default prompt for
// each kind, as Defaults does, reading the prompts once.
func (s *Store) Library(ctx context.Context) (Library, error) {
	stored, err := s.storedDefaults(ctx)
	if err != nil {
		return Library{}, err
	}

	all, err := s.All(ctx)
	if err != nil {
		return Library{}, err
	}

	return Library{
		Prompts: all,
		Defaults: DefaultPrompts{
			PR:    pickDefault(all, item.KindPR, stored.PR),
			Issue: pickDefault(all, item.KindIssue, stored.Issue),
		},
	}, nil
}

// Defaults returns the app's default prompt for each kind. A stored default that is
// gone or no longer applies falls back to the first prompt, by nickname, that applies.
func (s *Store) Defaults(ctx context.Context) (DefaultPrompts, error) {
	lib, err := s.Library(ctx)
	if err != nil {
		return DefaultPrompts{}, err
	}

	return lib.Defaults, nil
}

// inTx runs fn with a Store on the transaction s joined, or else on a transaction of its
// own that commits when fn returns nil.
func (s *Store) inTx(ctx context.Context, fn func(*Store) error) error {
	if s.db == nil {
		return fn(s)
	}

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		return fn(s.WithTx(tx))
	})
}

// insert stores p as a new prompt, with its starter key, stamped now, and returns its
// ID.
func (s *Store) insert(ctx context.Context, p Prompt) (int64, error) {
	args, err := store.EncodeJSON(p.ExtraArgs)
	if err != nil {
		return 0, err
	}

	now := s.now()

	return s.q.InsertPrompt(ctx, queries.InsertPromptParams{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: string(p.AppliesTo), Body: p.Body, Model: p.Model,
		Effort: p.Effort, PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraArgs: args, StarterKey: p.StarterKey,
		CreatedAt: now, UpdatedAt: now,
	})
}

// update replaces the title, nickname, text, and settings of the prompt with id with
// p's, stamped now. Its starter key stays. It returns ErrPromptNotFound when no prompt
// has id.
func (s *Store) update(ctx context.Context, id int64, p Prompt) error {
	args, err := store.EncodeJSON(p.ExtraArgs)
	if err != nil {
		return err
	}

	n, err := s.q.UpdatePrompt(ctx, queries.UpdatePromptParams{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: string(p.AppliesTo), Body: p.Body, Model: p.Model,
		Effort: p.Effort, PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraArgs: args, UpdatedAt: s.now(), ID: id,
	})

	switch {
	case err != nil:
		return err
	case n == 0:
		return fmt.Errorf("%w (id %d)", ErrPromptNotFound, id)
	default:
		return nil
	}
}

// remove deletes the prompt with id. It returns ErrPromptNotFound when no prompt has
// id, and an InUseError naming the prompt, with its Usage left for the caller to fill
// in, when the prompt is one of the app's default prompts, or when launch defaults name
// it and their foreign keys refuse the delete.
func (s *Store) remove(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(st *Store) error {
		defaults, err := st.storedDefaults(ctx)
		if err != nil {
			return err
		}

		if defaults.PR != id && defaults.Issue != id {
			if err = st.deleteRow(ctx, id); !store.IsForeignKeyViolation(err) {
				return err
			}
		}

		// The prompt is in use; look it up to name it.
		p, err := st.Lookup(ctx, id)
		if err != nil {
			return err
		}

		return &InUseError{Nickname: p.Nickname}
	})
}

// deleteRow deletes the prompt row with id, or returns ErrPromptNotFound when there is
// none. A foreign key refusing it stays in the error's chain.
func (s *Store) deleteRow(ctx context.Context, id int64) error {
	n, err := s.q.DeletePrompt(ctx, id)

	switch {
	case err != nil:
		return fmt.Errorf("delete the prompt: %w", err)
	case n == 0:
		return fmt.Errorf("%w (id %d)", ErrPromptNotFound, id)
	default:
		return nil
	}
}

// nicknameTaken reports whether a prompt other than id has nickname, ignoring case.
func (s *Store) nicknameTaken(ctx context.Context, id int64, nickname string) (bool, error) {
	count, err := s.q.CountNickname(ctx, queries.CountNicknameParams{Nickname: nickname, ID: id})
	if err != nil {
		return false, fmt.Errorf("check the nickname: %w", err)
	}

	return count > 0, nil
}

// freeNickname returns base when no prompt has it, or else base followed by the lowest
// free number, as in "Review copy 2", cut so it stays within the nickname limit.
func (s *Store) freeNickname(ctx context.Context, base string) (string, error) {
	base = textutil.Clip(base, maxNicknameRunes)

	for n := 1; ; n++ {
		candidate := base

		if n > 1 {
			suffix := " " + strconv.Itoa(n)
			candidate = textutil.Clip(base, maxNicknameRunes-utf8.RuneCountInString(suffix)) + suffix
		}

		taken, err := s.nicknameTaken(ctx, 0, candidate)
		if err != nil || !taken {
			return candidate, err
		}
	}
}

// storedDefaults reads the stored default prompt IDs; 0 where none is stored, or where
// the stored value is not a number.
func (s *Store) storedDefaults(ctx context.Context) (DefaultPrompts, error) {
	pr, _, err := s.prefs.Get(ctx, prefDefaultPR)
	if err != nil {
		return DefaultPrompts{}, err
	}

	issue, _, err := s.prefs.Get(ctx, prefDefaultIssue)
	if err != nil {
		return DefaultPrompts{}, err
	}

	prID, _ := strconv.ParseInt(pr, 10, 64)       // a missing or bad value means no default
	issueID, _ := strconv.ParseInt(issue, 10, 64) // a missing or bad value means no default

	return DefaultPrompts{PR: prID, Issue: issueID}, nil
}

// setDefault stores id as the app's default prompt for kind.
func (s *Store) setDefault(ctx context.Context, kind item.Kind, id int64) error {
	return s.prefs.Set(ctx, defaultPrefKey(kind), strconv.FormatInt(id, 10))
}

// seeded reads the starter keys already added.
func (s *Store) seeded(ctx context.Context) ([]string, error) {
	keys, _, err := s.prefs.GetList(ctx, prefSeeded)
	if err != nil {
		return nil, fmt.Errorf("read the starter prompts added: %w", err)
	}

	return keys, nil
}

// libraryExists reports whether the library was ever started: a starter was added to it
// once, or it holds a prompt. A database created moments ago holds a library that does
// not exist yet, as does one whose library was lost.
func (s *Store) libraryExists(ctx context.Context) (bool, error) {
	_, seeded, err := s.prefs.Get(ctx, prefSeeded)
	if err != nil || seeded {
		return seeded, err
	}

	all, err := s.All(ctx)
	if err != nil {
		return false, err
	}

	return len(all) > 0, nil
}

// setSeeded records keys as the starter keys already added.
func (s *Store) setSeeded(ctx context.Context, keys []string) error {
	return s.prefs.SetList(ctx, prefSeeded, keys)
}

// now is the time a write stamps, in UTC so stored timestamps sort as text.
func (s *Store) now() time.Time {
	return s.clock.Now().UTC()
}

// pickDefault returns the stored default for kind when it exists and applies, else the
// first prompt that applies, else 0.
func pickDefault(all []Prompt, kind item.Kind, stored int64) int64 {
	var first int64

	for _, p := range all {
		if !p.AppliesTo.Covers(kind) {
			continue
		}

		if p.ID == stored {
			return stored
		}

		if first == 0 {
			first = p.ID
		}
	}

	return first
}

// defaultPrefKey returns the preference holding the default prompt for kind.
func defaultPrefKey(kind item.Kind) string {
	if kind == item.KindPR {
		return prefDefaultPR
	}

	return prefDefaultIssue
}

// fromRow converts a stored row to a Prompt with no usage. Its lists are never nil: the
// schema only accepts a JSON array in extra_args, and an empty one decodes to an empty
// list.
func fromRow(row queries.Prompt) (Prompt, error) {
	args, err := store.DecodeJSON[[]string](row.ExtraArgs)
	if err != nil {
		return Prompt{}, fmt.Errorf("prompt %q: extra arguments: %w", row.Nickname, err)
	}

	p := Prompt{
		ID: row.ID, Name: row.Name, Nickname: row.Nickname, AppliesTo: AppliesTo(row.AppliesTo), Body: row.Body,
		Model: row.Model, Effort: row.Effort, PlanMode: row.PlanMode, Worktree: row.Worktree,
		ExtraArgs: args, ExtraFlags: agent.Join(args), StarterKey: row.StarterKey,
		Usage:     normalizeUsage(Usage{}),
		CreatedAt: clock.Format(row.CreatedAt),
		UpdatedAt: clock.Format(row.UpdatedAt),
	}

	if st, ok := starterFor(row.StarterKey); ok {
		p.Modified = !matchesStarter(p, st)
	}

	return p, nil
}

// matchesStarter reports whether a prompt still has its starter's title, text, and
// settings. The nickname may differ: Reset keeps it when another prompt took it.
func matchesStarter(p Prompt, st Starter) bool {
	return p.Name == st.Name && p.AppliesTo == st.AppliesTo && p.Body == st.Body &&
		p.Model == st.Model && p.Effort == st.Effort && sameBool(p.PlanMode, st.PlanMode) &&
		sameBool(p.Worktree, st.Worktree) && slices.Equal(p.ExtraArgs, st.ExtraArgs)
}

// sameBool reports whether two optional settings are equal.
func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}
