package ghthreads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// Preview is what clearing would do now, for the cleanup sheet and the card that first
// offers it: the threads that would clear, by repository, and those held back, by reason.
// It is decided as if clearing on its own were on, so it counts the candidates while it is
// off, and it takes GitHub's last word on an item's state however old it is: a pass reads a
// state again before it clears on it.
type Preview struct {
	// Reading is whether the app reads the user's notifications; off, nothing would clear.
	Reading bool `json:"reading"`

	// Auto is whether clearing on its own is on, and Action what clearing does.
	Auto   bool               `json:"auto"`
	Action threadrules.Action `json:"action"`

	// Count is how many threads would clear.
	Count int `json:"count"`

	// Repos are the threads that would clear, by repository, in the order of each one's
	// most recently updated thread.
	Repos []PreviewRepo `json:"repos"`

	// Held are the threads a moment finished but that would not clear now, by reason, in
	// the order each reason is first met.
	Held []PreviewHeld `json:"held"`
}

// PreviewRepo is one repository's threads that would clear.
type PreviewRepo struct {
	Owner   string          `json:"owner"`
	Name    string          `json:"name"`
	Threads []PreviewThread `json:"threads"`
}

// PreviewHeld is the threads held back from clearing for one reason.
type PreviewHeld struct {
	Reason  SkipReason      `json:"reason"`
	Count   int             `json:"count"`
	Threads []PreviewThread `json:"threads"`
}

// PreviewThread is one thread in a Preview.
type PreviewThread struct {
	// ID is GitHub's ID of the thread, which Clear takes.
	ID int64 `json:"id"`

	// Owner and Name are its repository, Kind and Number the item it is about.
	Owner  string    `json:"owner"`
	Name   string    `json:"name"`
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`

	// Title is the item's title, Reason GitHub's reason for the thread, and Unread whether
	// it is unread.
	Title  string `json:"title"`
	Reason string `json:"reason"`
	Unread bool   `json:"unread"`

	// UpdatedAt is when it last changed on GitHub, as RFC 3339.
	UpdatedAt string `json:"updatedAt"`

	// Trigger is the moment that finished its item.
	Trigger threadrules.Trigger `json:"trigger"`
}

// Preview returns what clearing would do now, from the database alone: it never asks
// GitHub.
func (s *Service) Preview(ctx context.Context) (Preview, error) {
	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return Preview{}, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	out := Preview{
		Reading: rules.Read, Auto: rules.Auto, Action: threadrules.DecodeAction(string(rules.Action)),
		Repos: []PreviewRepo{}, Held: []PreviewHeld{},
	}

	st, err := s.loadState(ctx)
	if err != nil || !rules.Read || st.Account == "" {
		return out, err
	}

	in, err := loadInputs(ctx, s.q, st.Account)
	if err != nil {
		return Preview{}, err
	}

	rules.Auto = true
	now, hold := s.clock.Now(), s.hold(ctx)
	groups := previewGroups{repos: map[string]int{}, held: map[SkipReason]int{}}

	for i := range in.threads {
		r := &in.threads[i]
		if in.marks.queued[r.ThreadID] {
			continue
		}

		if d, ok := decideThread(rules, r, in.momentsOf(r), now, hold, true); ok {
			out.sort(r, d.verdict, in.marks, groups)
		}
	}

	return out, nil
}

// previewGroups holds the index of each repository's group and each reason's in a Preview.
type previewGroups struct {
	repos map[string]int
	held  map[SkipReason]int
}

// sort lists r, decided as verdict, where it goes: among those that would clear, or those
// held back, for its reason, or because the user kept it, or its clear failed, at its
// update. One whose clear was sent at its update isn't listed.
func (p *Preview) sort(r *decideRow, verdict threadrules.Verdict, m marks, groups previewGroups) {
	t := previewThread(r, verdict.Trigger)

	if verdict.Kind != threadrules.VerdictClear {
		p.hold(SkipReason(verdict.Reason), t, groups.held)

		return
	}

	if reason, stuck := m.stuckAt(r); stuck {
		p.hold(reason, t, groups.held)

		return
	}

	if m.eligible(r) {
		p.add(t, groups.repos)
	}
}

// add lists t among the threads that would clear; repos holds each repository's index.
func (p *Preview) add(t PreviewThread, repos map[string]int) {
	key := repo.Name{Owner: t.Owner, Repo: t.Name}.Key()

	at, ok := repos[key]
	if !ok {
		at = len(p.Repos)
		repos[key] = at

		p.Repos = append(p.Repos, PreviewRepo{Owner: t.Owner, Name: t.Name, Threads: []PreviewThread{}})
	}

	p.Repos[at].Threads = append(p.Repos[at].Threads, t)
	p.Count++
}

// hold lists t among the threads held back for reason; held holds each reason's index.
func (p *Preview) hold(reason SkipReason, t PreviewThread, held map[SkipReason]int) {
	at, ok := held[reason]
	if !ok {
		at = len(p.Held)
		held[reason] = at

		p.Held = append(p.Held, PreviewHeld{Reason: reason, Threads: []PreviewThread{}})
	}

	p.Held[at].Threads = append(p.Held[at].Threads, t)
	p.Held[at].Count++
}

// previewThread is r as a Preview lists it, finished by trigger.
func previewThread(r *decideRow, trigger threadrules.Trigger) PreviewThread {
	return PreviewThread{
		ID: r.ThreadID, Owner: r.RepoOwner, Name: r.RepoName, Kind: item.Kind(r.ItemKind), Number: int(r.Number),
		Title: r.SubjectTitle, Reason: r.Reason, Unread: r.Unread, UpdatedAt: stamp(r.UpdatedAt), Trigger: trigger,
	}
}

// Clear clears the threads with threadIDs by the user's own hand, the cleanup sheet's
// button: each one the mirror lists for the account the last pass read is queued to go at
// once, with no hold and whether clearing on its own is on or off, and a clear already
// queued for it goes at once instead. A thread about neither a pull request nor an issue,
// one already read while clearing marks one read, and any while reading is off are left
// alone. It returns the Snapshot, whose Queued lists them until they are sent.
func (s *Service) Clear(ctx context.Context, threadIDs []int64) (Snapshot, error) {
	if err := s.clearByHand(ctx, threadIDs); err != nil {
		return Snapshot{}, err
	}

	return s.Snapshot(ctx)
}

// ClearItem clears, by the user's own hand, every thread about the pull request or issue
// owner/name#number that the mirror lists for the account the last pass read, as Clear
// does: the card's menu action.
func (s *Service) ClearItem(ctx context.Context, owner, name string, number int) (Snapshot, error) {
	ids, err := s.itemThreads(ctx, item.Ref{Repo: repo.Name{Owner: owner, Repo: name}, Number: number})
	if err != nil {
		return Snapshot{}, err
	}

	if err = s.clearByHand(ctx, ids); err != nil {
		return Snapshot{}, err
	}

	return s.Snapshot(ctx)
}

// itemThreads returns the IDs of the threads about ref that the mirror lists for the account
// the last pass read; none before a pass read one.
func (s *Service) itemThreads(ctx context.Context, ref item.Ref) ([]int64, error) {
	st, err := s.loadState(ctx)
	if err != nil || st.Account == "" {
		return nil, err
	}

	threads, err := s.q.ListDecideThreads(ctx, st.Account)
	if err != nil {
		return nil, fmt.Errorf("read the GitHub notifications to clear: %w", err)
	}

	var ids []int64

	for i := range threads {
		if rowItem(&threads[i]).Key() == ref.Key() {
			ids = append(ids, threads[i].ThreadID)
		}
	}

	return ids, nil
}

// clearByHand queues the clears of the threads with ids, by the user's hand, as Clear
// says, and wakes the Syncer to send them.
func (s *Service) clearByHand(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	st, err := s.loadState(ctx)
	if err != nil || !rules.Read || st.Account == "" {
		return err
	}

	now := s.clock.Now()
	queued := 0

	s.sending.Lock()
	defer s.sending.Unlock()

	err = store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var queueErr error

		queued, queueErr = queueAllByHand(ctx, s.q.WithTx(tx), st.Account, ids, rules, now)

		return queueErr
	})
	if err != nil {
		return fmt.Errorf("clear GitHub notifications: %w", err)
	}

	if queued > 0 {
		s.logger.InfoContext(ctx, "the user cleared GitHub notifications", slog.Int("count", queued))
		s.sendBy(now)
		s.nudges.signal()
	}

	return nil
}

// queueAllByHand queues the clears of account's threads with ids, each once, by the user's
// hand, at now, through q, and returns how many it queued.
func queueAllByHand(
	ctx context.Context, q *queries.Queries, account string, ids []int64, rules threadrules.Rules, now time.Time,
) (int, error) {
	clears, err := q.ListClearMarks(ctx, account)
	if err != nil {
		return 0, fmt.Errorf("read the cleared GitHub notifications: %w", err)
	}

	waiting := map[int64]int64{}

	for _, c := range clears {
		if ClearState(c.State) == ClearQueued {
			waiting[c.ThreadID] = c.ID
		}
	}

	action := threadrules.DecodeAction(string(rules.Action))
	queued := 0

	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		n, err := queueByHand(ctx, q, account, id, waiting, rules, action, now)
		if err != nil {
			return 0, err
		}

		queued += n
	}

	return queued, nil
}

// queueByHand queues the clear of account's thread id by the user's hand, at now, through
// q, unless the rules leave it alone; one already queued (waiting holds them by thread)
// goes at once instead. It returns how many it queued: 1 or 0.
func queueByHand(
	ctx context.Context, q *queries.Queries, account string, id int64, waiting map[int64]int64,
	rules threadrules.Rules, action threadrules.Action, now time.Time,
) (int, error) {
	got, err := q.GetDecideThread(ctx, queries.GetDecideThreadParams{AccountLogin: account, ThreadID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("read a GitHub notification to clear: %w", err)
	}

	th := decideRow(got)
	if byHand(rules, &th, now).Kind != threadrules.VerdictClear {
		return 0, nil
	}

	if queuedID, ok := waiting[id]; ok {
		_, err = q.ClearByHand(ctx, queries.ClearByHandParams{Action: string(action), SendAfter: now.UTC(), ID: queuedID})
	} else {
		_, err = q.InsertClear(ctx, clearParams(account, &th, threadrules.TriggerByHand, action, now, now))
	}

	if err != nil {
		return 0, fmt.Errorf("queue a GitHub notification's clear: %w", err)
	}

	return 1, nil
}

// Keep takes back the queued clears with clearIDs, the toast's Undo: each one still queued
// is kept, and never sent. A clear kept stays kept: its thread isn't queued again on its
// own until it changes on GitHub. It returns the Snapshot.
func (s *Service) Keep(ctx context.Context, clearIDs []int64) (Snapshot, error) {
	if len(clearIDs) > 0 {
		now := s.clock.Now().UTC()

		s.sending.Lock()
		kept, err := s.q.KeepClears(ctx, queries.KeepClearsParams{SettledAt: &now, Ids: clearIDs})
		s.sending.Unlock()

		if err != nil {
			return Snapshot{}, fmt.Errorf("keep the GitHub notifications: %w", err)
		}

		if kept > 0 {
			s.logger.InfoContext(ctx, "the user kept GitHub notifications from clearing", slog.Int64("count", kept))
		}
	}

	return s.Snapshot(ctx)
}
