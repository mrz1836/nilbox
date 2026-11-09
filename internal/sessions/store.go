package sessions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sessions/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Retention: the sessions the ledger keeps.
const (
	// KeepEnded is how long an ended session is kept.
	KeepEnded = 30 * 24 * time.Hour

	// KeepPerItem is how many sessions of each item are kept, the newest; one still
	// running is always kept.
	KeepPerItem = 20

	// RecentWindow is how far back the window's list reaches for sessions that ended.
	RecentWindow = 7 * 24 * time.Hour

	// maxListed caps the sessions the window's list holds.
	maxListed = 500
)

var (
	// ErrNotFound is returned for a session the ledger does not hold.
	ErrNotFound = errors.New("the session is no longer recorded")

	// ErrInvalidRecord is returned for a session record missing what storing it needs.
	ErrInvalidRecord = errors.New("invalid session record")
)

// Record is one session as the ledger stores it.
type Record struct {
	// ID is the app's own ID, AgentSessionID the agent's, or "", and ResumedFrom the ID
	// of the session this one resumed, or "".
	ID             string
	AgentSessionID string
	ResumedFrom    string

	// Agent, Profile, and ConfigDir are what it runs with: ConfigDir is the profile's
	// config directory, or "" for the agent's default.
	Agent     agent.ID
	Profile   string
	ConfigDir string

	// Prompt is the nickname of the prompt it was started with, and PromptHead the start
	// of the prompt.
	Prompt     string
	PromptHead string

	// Terminal is the terminal it runs in.
	Terminal string

	// ItemID, Owner, Name, Kind, Number, Title, and URL are the item.
	ItemID int64
	Owner  string
	Name   string
	Kind   item.Kind
	Number int
	Title  string
	URL    string

	// Dir is the absolute folder it runs in; ClonePath and Worktree say whose worktree
	// that is, when it is one.
	Dir       string
	ClonePath string
	Worktree  string

	// PID is the agent's process, or 0 while unknown.
	PID int

	// State, Detail, and LastMessage are what it last reported.
	State       State
	Detail      string
	LastMessage string

	// LaunchedAt is when it was launched; LinkedAt when its agent's ID was found, StateAt
	// when it came to its state, and EndedAt when it ended, each zero when not yet.
	LaunchedAt time.Time
	LinkedAt   time.Time
	StateAt    time.Time
	EndedAt    time.Time
}

// Repo is the item's repository as "owner/name".
func (r Record) Repo() string {
	return repo.Name{Owner: r.Owner, Repo: r.Name}.String()
}

// Session is the record as the window shows it, with home shown as ~.
func (r Record) Session(home string) Session {
	return Session{
		ID: r.ID, AgentSessionID: r.AgentSessionID, ResumedFrom: r.ResumedFrom, Agent: r.Agent, Profile: r.Profile,
		Prompt: r.Prompt, Terminal: r.Terminal, ItemID: r.ItemID, Repo: r.Repo(), Kind: r.Kind, Number: r.Number,
		Title: r.Title, URL: r.URL, Dir: paths.Tilde(r.Dir, home), Worktree: r.Worktree, State: r.State,
		Detail: r.Detail, StateAt: stamp(r.StateAt), LastMessage: r.LastMessage, LaunchedAt: stamp(r.LaunchedAt),
		EndedAt: stamp(r.EndedAt), CanFocus: r.State.Live() && r.PID > 0,
		CanResume: !r.State.Live() && r.AgentSessionID != "",
	}
}

// valid reports whether a record holds what storing it needs.
func (r Record) valid() bool {
	return r.ID != "" && agent.Known(r.Agent) && r.Owner != "" && r.Name != "" && r.Kind.Valid() && r.Number > 0 &&
		filepath.IsAbs(r.Dir) && !r.LaunchedAt.IsZero()
}

// stamp is t as RFC 3339, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}

// Observation is what an Observer saw of one session. A zero field keeps what is
// stored.
type Observation struct {
	// ID is the app's ID of the session observed.
	ID string

	// AgentSessionID is the agent's own ID, once found.
	AgentSessionID string

	// State is what the session is doing, and Detail says more, or is "".
	State  State
	Detail string

	// PID is the agent's process, once found.
	PID int

	// LastMessage is what the agent last said.
	LastMessage string

	// Seen reports that the agent itself listed the session as running, rather than its
	// process or its folder suggesting so.
	Seen bool
}

// Change is a session whose record an observation changed: as it was, and as it is.
type Change struct {
	Before Record
	After  Record
}

// StateChanged reports a change of state.
func (c Change) StateChanged() bool {
	return c.Before.State != c.After.State
}

// Store reads and writes the ledger.
type Store struct {
	db *sql.DB
	q  *queries.Queries
}

// NewStore returns a Store over the pool db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, q: queries.New(db)}
}

// Insert records a session just launched. A session the agent already named is linked
// at once.
func (s *Store) Insert(ctx context.Context, r Record) error {
	if !r.valid() {
		return fmt.Errorf("%w: %s", ErrInvalidRecord, r.ID)
	}

	if !r.State.Valid() || r.State == StateEnded {
		r.State = StateStarting
	}

	params := queries.InsertSessionParams{
		ID: r.ID, AgentSessionID: r.AgentSessionID, ResumedFrom: r.ResumedFrom, Agent: string(r.Agent),
		Profile: r.Profile, ConfigDir: r.ConfigDir, Prompt: r.Prompt, PromptHead: r.PromptHead, Terminal: r.Terminal,
		ItemID: r.ItemID, Owner: r.Owner, Name: r.Name, Kind: string(r.Kind), Number: int64(r.Number), Title: r.Title,
		URL: r.URL, Dir: r.Dir, ClonePath: r.ClonePath, Worktree: r.Worktree, State: string(r.State),
		LaunchedAt: r.LaunchedAt.UTC(),
	}

	if r.AgentSessionID != "" {
		params.LinkedAt = new(r.LaunchedAt.UTC())
	}

	if err := s.q.InsertSession(ctx, params); err != nil {
		return fmt.Errorf("record the session: %w", err)
	}

	return nil
}

// Get returns one session, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	row, err := s.q.GetSession(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}

	if err != nil {
		return Record{}, fmt.Errorf("read the session: %w", err)
	}

	return fromRow(row), nil
}

// Live returns every session that has not ended, oldest first.
func (s *Store) Live(ctx context.Context) ([]Record, error) {
	rows, err := s.q.LiveSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the running sessions: %w", err)
	}

	return fromRows(rows), nil
}

// Recent returns every session that has not ended, and those launched since, newest
// first.
func (s *Store) Recent(ctx context.Context, since time.Time) ([]Record, error) {
	rows, err := s.q.RecentSessions(ctx, queries.RecentSessionsParams{Since: since.UTC(), RowLimit: maxListed})
	if err != nil {
		return nil, fmt.Errorf("read the recent sessions: %w", err)
	}

	return fromRows(rows), nil
}

// Apply stores what was observed at now, in one transaction, and returns the sessions it
// changed. An ended session is left as it ended, and an observation of a session the
// ledger no longer holds is dropped.
func (s *Store) Apply(ctx context.Context, now time.Time, observed []Observation) ([]Change, error) {
	if len(observed) == 0 {
		return nil, nil
	}

	var changes []Change

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, o := range observed {
			c, changed, err := apply(ctx, q, o, now)
			if err != nil {
				return err
			}

			if changed {
				changes = append(changes, c)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store what the sessions reported: %w", err)
	}

	return changes, nil
}

// Prune forgets, as of now, the sessions that ended more than KeepEnded ago, and each
// item's ended sessions past its newest KeepPerItem. It returns how many it forgot.
func (s *Store) Prune(ctx context.Context, now time.Time) (int64, error) {
	var forgot int64

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		old, err := q.PruneEndedBefore(ctx, new(now.Add(-KeepEnded).UTC()))
		if err != nil {
			return err
		}

		excess, err := q.PruneItemExcess(ctx, KeepPerItem)
		forgot = old + excess

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("forget old sessions: %w", err)
	}

	return forgot, nil
}

// apply stores one observation at now, and returns the change it made, if it made one.
func apply(ctx context.Context, q *queries.Queries, o Observation, now time.Time) (Change, bool, error) {
	row, err := q.GetSession(ctx, o.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return Change{}, false, nil
	}

	if err != nil {
		return Change{}, false, err //nolint:wrapcheck // Apply says what failed
	}

	before := fromRow(row)

	after, changed := merge(before, o, now)
	if !changed {
		return Change{}, false, nil
	}

	if _, err = q.UpdateSession(ctx, updateParams(after)); err != nil {
		return Change{}, false, err //nolint:wrapcheck // Apply says what failed
	}

	return Change{Before: before, After: after}, true, nil
}

// merge applies an observation at now to a record, and reports whether it changed it.
func merge(r Record, o Observation, now time.Time) (Record, bool) {
	if !r.State.Live() {
		return r, false
	}

	before := r

	if o.AgentSessionID != "" && r.AgentSessionID == "" {
		r.AgentSessionID, r.LinkedAt = o.AgentSessionID, now
	}

	if o.PID > 0 {
		r.PID = o.PID
	}

	if o.State.Valid() {
		if o.State != r.State {
			r.State, r.StateAt = o.State, now
		}

		r.Detail = textutil.Clip(strings.TrimSpace(o.Detail), MaxMessageRunes)

		if r.State == StateEnded {
			r.EndedAt = now
		}
	}

	if msg := textutil.Clip(strings.TrimSpace(o.LastMessage), MaxMessageRunes); msg != "" {
		r.LastMessage = msg
	}

	return r, r != before
}

// updateParams is the update that stores r.
func updateParams(r Record) queries.UpdateSessionParams {
	return queries.UpdateSessionParams{
		ID: r.ID, AgentSessionID: r.AgentSessionID, Pid: int64(r.PID), State: string(r.State), StateDetail: r.Detail,
		LastMessage: r.LastMessage, LinkedAt: timeOrNil(r.LinkedAt), StateAt: r.StateAt.UTC(),
		EndedAt: timeOrNil(r.EndedAt),
	}
}

// timeOrNil is t in UTC, or nil for the zero time.
func timeOrNil(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}

	return new(t.UTC())
}

// fromRows converts stored sessions.
func fromRows(rows []queries.AgentSession) []Record {
	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row))
	}

	return out
}

// fromRow converts a stored session. A state this build does not know reads as
// StateUnknown, so the session is still followed.
func fromRow(row queries.AgentSession) Record {
	r := Record{
		ID: row.ID, AgentSessionID: row.AgentSessionID, ResumedFrom: row.ResumedFrom, Agent: agent.ID(row.Agent),
		Profile: row.Profile, ConfigDir: row.ConfigDir, Prompt: row.Prompt, PromptHead: row.PromptHead,
		Terminal: row.Terminal, ItemID: row.ItemID, Owner: row.Owner, Name: row.Name, Kind: item.Kind(row.Kind),
		Number: int(row.Number), Title: row.Title, URL: row.URL, Dir: row.Dir, ClonePath: row.ClonePath,
		Worktree: row.Worktree, PID: int(row.Pid), State: State(row.State), Detail: row.StateDetail,
		LastMessage: row.LastMessage, LaunchedAt: row.LaunchedAt, StateAt: row.StateAt,
	}

	if !r.State.Valid() {
		r.State = StateUnknown
	}

	if row.LinkedAt != nil {
		r.LinkedAt = *row.LinkedAt
	}

	if row.EndedAt != nil {
		r.EndedAt = *row.EndedAt
	}

	return r
}
