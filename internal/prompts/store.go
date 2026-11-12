package prompts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

	// The default replies' IDs, one per ReplySlot.
	prefReplyApprove        = "prompts.default.reply.approve"
	prefReplyApproveBots    = "prompts.default.reply.approve.bots"
	prefReplyRequestChanges = "prompts.default.reply.request_changes"
	prefReplyComment        = "prompts.default.reply.comment"
)

// replyPrefKey returns the preference holding slot's default reply.
func replyPrefKey(slot ReplySlot) string {
	switch slot {
	case SlotApprove:
		return prefReplyApprove
	case SlotApproveBots:
		return prefReplyApproveBots
	case SlotRequestChanges:
		return prefReplyRequestChanges
	case SlotComment:
		return prefReplyComment
	default:
		return prefReplyApprove
	}
}

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

	out, err := store.ConvertRows(rows, fromRow)
	if err != nil {
		return nil, fmt.Errorf("read the prompts: %w", err)
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
			PR:      pickDefault(all, item.KindPR, stored.PR),
			Issue:   pickDefault(all, item.KindIssue, stored.Issue),
			Replies: pickReplies(all, stored.Replies),
		},
	}, nil
}

// Defaults returns the app's default prompt for each kind and its default replies. A
// stored default prompt that is gone or no longer applies falls back to the first agent
// prompt, by nickname, that applies; a default reply that is gone or no longer a reply for
// its review leaves that review with none.
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
	models, args, err := encodeSettings(p)
	if err != nil {
		return 0, err
	}

	now := s.now()

	return s.q.InsertPrompt(ctx, queries.InsertPromptParams{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: string(p.AppliesTo), Body: p.Body, Models: models,
		Effort: p.Effort, PlanMode: p.PlanMode, Worktree: p.Worktree, AgentArgs: args, StarterKey: p.StarterKey,
		CreatedAt: now, UpdatedAt: now, Purpose: string(purposeOf(p.Purpose)), ReplyAction: string(actionOf(p)),
	})
}

// encodeSettings returns a prompt's models and extra arguments as stored, as JSON objects
// keyed by agent.
func encodeSettings(p Prompt) (models, args string, err error) {
	if models, err = store.EncodeJSON(agent.CloneModels(p.Models)); err != nil {
		return "", "", err
	}

	if args, err = store.EncodeJSON(agent.CloneArgs(p.ExtraArgs)); err != nil {
		return "", "", err
	}

	return models, args, nil
}

// purposeOf returns purpose, or PurposeAgent for "".
func purposeOf(purpose Purpose) Purpose {
	if purpose == PurposeReply {
		return PurposeReply
	}

	return PurposeAgent
}

// actionOf returns a reply's action, and "" for an agent prompt.
func actionOf(p Prompt) ReplyAction {
	if purposeOf(p.Purpose) != PurposeReply {
		return ""
	}

	return p.ReplyAction
}

// update replaces the title, nickname, text, settings, and reply action of the prompt with
// id with p's, stamped now. Its purpose and starter key stay. It returns ErrPromptNotFound when no prompt
// has id.
func (s *Store) update(ctx context.Context, id int64, p Prompt) error {
	models, args, err := encodeSettings(p)
	if err != nil {
		return err
	}

	n, err := s.q.UpdatePrompt(ctx, queries.UpdatePromptParams{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: string(p.AppliesTo), Body: p.Body, Models: models,
		Effort: p.Effort, PlanMode: p.PlanMode, Worktree: p.Worktree, AgentArgs: args, ReplyAction: string(actionOf(p)),
		UpdatedAt: s.now(), ID: id,
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
// it and their foreign keys refuse the delete. A reply that is a default reply is deleted,
// and the reviews it was the default for are left with none.
func (s *Store) remove(ctx context.Context, id int64) error {
	return s.inTx(ctx, func(st *Store) error {
		defaults, err := st.storedDefaults(ctx)
		if err != nil {
			return err
		}

		if err = st.clearReplyDefaults(ctx, defaults.Replies, id); err != nil {
			return err
		}

		if defaults.PR == id || defaults.Issue == id {
			return st.inUse(ctx, id)
		}

		if err = st.deleteRow(ctx, id); !store.IsForeignKeyViolation(err) {
			return err
		}

		return st.inUse(ctx, id)
	})
}

// inUse is the InUseError for the prompt with id, which a default keeps.
func (s *Store) inUse(ctx context.Context, id int64) error {
	p, err := s.Lookup(ctx, id)
	if err != nil {
		return err
	}

	return &InUseError{Nickname: p.Nickname}
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

	replies, err := s.storedReplies(ctx)
	if err != nil {
		return DefaultPrompts{}, err
	}

	return DefaultPrompts{PR: prID, Issue: issueID, Replies: replies}, nil
}

// storedReplies reads the stored default reply IDs; 0 where none is stored, or where the
// stored value is not a number.
func (s *Store) storedReplies(ctx context.Context) (ReplyDefaults, error) {
	ids := make(map[ReplySlot]int64, len(ReplySlots()))

	for _, slot := range ReplySlots() {
		raw, _, err := s.prefs.Get(ctx, replyPrefKey(slot))
		if err != nil {
			return ReplyDefaults{}, err
		}

		ids[slot], _ = strconv.ParseInt(raw, 10, 64) // a missing or bad value means no default
	}

	return ReplyDefaults{
		Approve: ids[SlotApprove], ApproveBots: ids[SlotApproveBots], RequestChanges: ids[SlotRequestChanges],
		Comment: ids[SlotComment],
	}, nil
}

// clearReplyDefaults leaves every slot whose default reply is id with none.
func (s *Store) clearReplyDefaults(ctx context.Context, replies ReplyDefaults, id int64) error {
	for _, slot := range replies.Slots(id) {
		if err := s.setReplyDefault(ctx, slot, 0); err != nil {
			return err
		}
	}

	return nil
}

// setReplyDefault stores id as slot's default reply; 0 leaves the slot with none.
func (s *Store) setReplyDefault(ctx context.Context, slot ReplySlot, id int64) error {
	value := ""
	if id != 0 {
		value = strconv.FormatInt(id, 10)
	}

	return s.prefs.Set(ctx, replyPrefKey(slot), value)
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
// first agent prompt that applies, else 0. A reply is never a default prompt.
func pickDefault(all []Prompt, kind item.Kind, stored int64) int64 {
	var first int64

	for _, p := range all {
		if p.IsReply() || !p.AppliesTo.Covers(kind) {
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

// pickReplies returns each stored default reply that still exists and is a reply for its
// slot's review, and 0 for the others.
func pickReplies(all []Prompt, stored ReplyDefaults) ReplyDefaults {
	byID := make(map[int64]Prompt, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}

	keep := func(slot ReplySlot) int64 {
		p, ok := byID[stored.For(slot)]
		if !ok || !p.IsReply() || p.ReplyAction != slot.Action() {
			return 0
		}

		return p.ID
	}

	return ReplyDefaults{
		Approve: keep(SlotApprove), ApproveBots: keep(SlotApproveBots), RequestChanges: keep(SlotRequestChanges),
		Comment: keep(SlotComment),
	}
}

// defaultPrefKey returns the preference holding the default prompt for kind.
func defaultPrefKey(kind item.Kind) string {
	if kind == item.KindPR {
		return prefDefaultPR
	}

	return prefDefaultIssue
}

// fromRow converts a stored row to a Prompt with no usage. Its maps are never nil: the
// schema only accepts a JSON object in models and agent_args, and an empty one decodes to
// an empty map.
func fromRow(row queries.Prompt) (Prompt, error) {
	models, err := store.DecodeJSON[map[agent.ID]string](row.Models)
	if err != nil {
		return Prompt{}, fmt.Errorf("prompt %q: models: %w", row.Nickname, err)
	}

	args, err := store.DecodeJSON[map[agent.ID][]string](row.AgentArgs)
	if err != nil {
		return Prompt{}, fmt.Errorf("prompt %q: extra arguments: %w", row.Nickname, err)
	}

	p := Prompt{
		ID: row.ID, Name: row.Name, Nickname: row.Nickname, AppliesTo: AppliesTo(row.AppliesTo), Body: row.Body,
		Purpose: purposeOf(Purpose(row.Purpose)), ReplyAction: ReplyAction(row.ReplyAction),
		Models: agent.CloneModels(models), Effort: row.Effort, PlanMode: row.PlanMode, Worktree: row.Worktree,
		ExtraArgs: agent.CloneArgs(args), ExtraFlags: agent.JoinArgs(args), StarterKey: row.StarterKey,
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
	return p.Name == st.Name && p.AppliesTo == st.AppliesTo && p.Body == st.Body && p.ReplyAction == st.ReplyAction &&
		agent.EqualModels(p.Models, st.Models) && p.Effort == st.Effort && sameBool(p.PlanMode, st.PlanMode) &&
		sameBool(p.Worktree, st.Worktree) && agent.EqualArgs(p.ExtraArgs, st.ExtraArgs)
}

// sameBool reports whether two optional settings are equal.
func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}
