package budget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/logging"
)

// The Tracker's state outlives the app. The app spends nothing while it is closed, so a
// count carried over from an earlier run, and continued, still counts everything the app
// spent from a window: only the first run of a build that tracks the budget misses what
// was spent before it opened, and says so (Reading.Complete).

// stateVersion is the saved state's format. A state of another version is not read: the
// next responses build a new one.
const stateVersion = 1

// ErrUnreadableState is returned for a saved state that cannot be read.
var ErrUnreadableState = errors.New("the saved API budget cannot be read")

// Keeper keeps the Tracker's state between runs, as one text.
type Keeper interface {
	// Load returns the state saved last, or "" when none was.
	Load(ctx context.Context) (string, error)

	// Save replaces the state saved.
	Save(ctx context.Context, state string) error
}

// savedState is the Tracker's state as it is kept.
type savedState struct {
	Version int `json:"version"`

	// TrackingSince is when the app first counted what it spends, on this database.
	TrackingSince time.Time `json:"trackingSince"`

	Budgets []savedBudget `json:"budgets"`
	Refresh *savedRefresh `json:"refresh,omitempty"`

	// Writes are when the app sent the edits of the last hour, oldest first, and
	// HeldUntil when GitHub's request to wait before the next edit ends. A state saved
	// before edits has neither.
	Writes    []time.Time `json:"writes,omitempty"`
	HeldUntil *time.Time  `json:"heldUntil,omitempty"`

	// Chores are when the app sent the chores of the last hour, oldest first. A state
	// saved before the chore lane has none, and a build before it ignores them, so the
	// version stays the same.
	Chores []time.Time `json:"chores,omitempty"`
}

// savedBudget is one budget's newest reading and what the app spent from its window.
type savedBudget struct {
	Resource    string    `json:"resource"`
	Limit       int       `json:"limit"`
	Remaining   int       `json:"remaining"`
	Used        int       `json:"used"`
	ResetAt     time.Time `json:"resetAt"`
	TakenAt     time.Time `json:"takenAt"`
	Spent       int       `json:"spent"`
	Requests    int       `json:"requests"`
	CountedFrom time.Time `json:"countedFrom"`
}

// savedRefresh is the last whole inbox refresh.
type savedRefresh struct {
	At     time.Time `json:"at"`
	Points int       `json:"points"`
	Items  int       `json:"items"`
}

// state encodes the Tracker's state for keeping.
func (t *Tracker) state() (string, error) {
	t.mu.Lock()

	out := savedState{Version: stateVersion, TrackingSince: t.started, Budgets: make([]savedBudget, 0, len(t.budgets))}

	for _, resource := range []string{github.ResourceGraphQL, github.ResourceCore, github.ResourceSearch} {
		if h := t.budgets[resource]; h != nil {
			r := h.reading
			out.Budgets = append(out.Budgets, savedBudget{
				Resource: resource, Limit: r.Limit, Remaining: r.Remaining, Used: r.Used, ResetAt: r.ResetAt,
				TakenAt: h.takenAt, Spent: h.spent, Requests: h.requests, CountedFrom: h.countedFrom,
			})
		}
	}

	if !t.last.at.IsZero() {
		out.Refresh = &savedRefresh{At: t.last.at, Points: t.last.points, Items: t.last.items}
	}

	now := t.clock.Now().UTC()
	t.writes.prune(now)
	t.chores.prune(now)
	out.Writes = append(out.Writes, t.writes.sent...)
	out.Chores = append(out.Chores, t.chores.sent...)

	if !t.heldUntil.IsZero() {
		out.HeldUntil = new(t.heldUntil)
	}

	t.mu.Unlock()

	raw, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("encode the API budget: %w", err)
	}

	return string(raw), nil
}

// Restore starts the Tracker from a state an earlier run kept: each budget's reading
// and what the app spent from its window, the last refresh, the edits and the chores of
// the last hour and any wait GitHub asked for, and when the app began counting. It is
// meant for startup, before any response; a budget already read in this run keeps its
// reading. A state that cannot be read changes nothing.
func (t *Tracker) Restore(state string) error {
	var saved savedState

	if err := json.Unmarshal([]byte(state), &saved); err != nil {
		return fmt.Errorf("%w: %w", ErrUnreadableState, err)
	}

	if saved.Version != stateVersion {
		return fmt.Errorf("%w: version %d", ErrUnreadableState, saved.Version)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if !saved.TrackingSince.IsZero() && saved.TrackingSince.Before(t.started) {
		t.started = saved.TrackingSince.UTC()
	}

	for _, b := range saved.Budgets {
		t.restoreBudgetLocked(b)
	}

	if saved.Refresh != nil && t.last.at.IsZero() {
		t.last = refresh{at: saved.Refresh.At.UTC(), points: saved.Refresh.Points, items: saved.Refresh.Items}
	}

	t.restoreWritesLocked(saved.Writes, saved.Chores, saved.HeldUntil)

	return nil
}

// restoreBudgetLocked holds a saved budget, unless it is not one or this run already
// read it. The caller holds mu.
func (t *Tracker) restoreBudgetLocked(b savedBudget) {
	if b.Resource == "" || b.Limit <= 0 || t.budgets[b.Resource] != nil {
		return
	}

	t.budgets[b.Resource] = &held{
		reading: github.RateReading{
			Resource: b.Resource, Limit: b.Limit, Remaining: b.Remaining, Used: b.Used, ResetAt: b.ResetAt.UTC(),
		},
		takenAt: b.TakenAt.UTC(), spent: max(b.Spent, 0), requests: max(b.Requests, 0),
		countedFrom: b.CountedFrom.UTC(),
	}
}

// SaveDelay is how long the Saver waits after a change before it saves, so the changes a
// refresh makes in a moment are kept in one write.
const SaveDelay = time.Second

// saveTimeout bounds the last save, made as the app stops.
const saveTimeout = 5 * time.Second

// Saver keeps the Tracker's state with a Keeper: soon after each change, and once more as
// it stops, so a restart picks up where the app left off. It runs as the app's tracked
// work, which the shell stops before it closes the database.
type Saver struct {
	tracker *Tracker
	keeper  Keeper
	logger  *slog.Logger
}

// NewSaver returns a Saver of tracker's state into keeper. A failed save is logged to
// logger, if any, and tried again with the next change.
func NewSaver(tracker *Tracker, keeper Keeper, logger *slog.Logger) *Saver {
	return &Saver{tracker: tracker, keeper: keeper, logger: logging.Or(logger)}
}

// Run saves the state SaveDelay after each change until ctx ends, then saves it once more.
func (s *Saver) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			s.final(ctx)

			return
		case <-s.tracker.dirty:
		}

		if clock.Sleep(ctx, SaveDelay) != nil {
			s.final(ctx)

			return
		}

		s.save(ctx)
	}
}

// final saves the state as the app stops, with a context of its own, since ctx has ended.
func (s *Saver) final(ctx context.Context) {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), saveTimeout)
	defer cancel()

	s.save(saveCtx)
}

// save writes the whole state. A failure is logged; the next change, or the stop, writes
// the whole state again.
func (s *Saver) save(ctx context.Context) {
	state, err := s.tracker.state()
	if err == nil {
		err = s.keeper.Save(ctx, state)
	}

	if err != nil {
		s.logger.WarnContext(ctx, "could not keep the API budget", "error", err)
	}
}
