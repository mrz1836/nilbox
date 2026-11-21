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

// ClearItemThreads clears, by the user's own hand, every thread about the pull request or
// issue owner/name#number that the mirror lists for the account the last pass read, as
// clearByHand does, and returns how many clears it queued: none while reading is off,
// before a pass read the account, or when the mirror lists no thread about it that the
// rules don't leave alone. Autopilot's Mark the GitHub notification done runs it.
func (s *Service) ClearItemThreads(ctx context.Context, owner, name string, number int) (int, error) {
	ids, err := s.itemThreads(ctx, item.Ref{Repo: repo.Name{Owner: owner, Repo: name}, Number: number})
	if err != nil {
		return 0, err
	}

	return s.clearByHand(ctx, ids)
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

// clearByHand queues the clears of the threads with ids, by the user's hand: each one the
// mirror lists for the account the last pass read is queued to go at once, with no hold and
// whether clearing on its own is on or off, and a clear already queued for it goes at once
// instead. A thread about neither a pull request nor an issue, one already read while
// clearing marks one read, and any while reading is off are left alone. It wakes the
// Syncer to send them, and returns how many it queued.
func (s *Service) clearByHand(ctx context.Context, ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return 0, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	st, err := s.loadState(ctx)
	if err != nil || !rules.Read || st.Account == "" {
		return 0, err
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
		return 0, fmt.Errorf("clear GitHub notifications: %w", err)
	}

	if queued > 0 {
		s.logger.InfoContext(ctx, "the user cleared GitHub notifications", slog.Int("count", queued))
		s.sendBy(now)
		s.nudges.signal()
	}

	return queued, nil
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
