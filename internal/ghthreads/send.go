package ghthreads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errOtherAccount is a clear the token of another account than its own would send.
var errOtherAccount = errors.New("the token belongs to another account than the clear's")

// sendOutcome is when the next queued clear may be sent, as a run's sends left it: next is
// zero when none waits on a time, as when none is queued, or what holds them back ends
// only with a pass (another account's token, GitHub refusing the listing, no token).
type sendOutcome struct {
	next time.Time
}

// sendStep is what one clear's send says of the run's next: go on, wait for a pass
// (blocked), or try again at.
type sendStep struct {
	stop    bool
	blocked bool
	at      time.Time
}

// goOn is the step that goes on to the next due clear.
var goOn = sendStep{} //nolint:gochecknoglobals // a fixed value

// sendDue sends st's account's queued clears that are due, one at a time, each checked
// again first (sendOne), until none is due, the chore lane refuses the next, or a send
// fails, and returns when the next may go. Nothing is sent while GitHub refuses the
// listing, or before a pass learned the account. The rules are read again before each
// clear after the first, so reading or clearing turned off stops the run at once.
func (s *Service) sendDue(ctx context.Context, rules threadrules.Rules, st syncState) (sendOutcome, error) {
	if st.Account == "" || st.Access == AccessRefused {
		return sendOutcome{}, nil
	}

	hold := s.hold(ctx)

	for i := range maxSendsPerRun {
		if i > 0 {
			read, stop, err := s.rulesStill(ctx)
			if stop != nil {
				return *stop, err
			}

			rules = read
		}

		if out, more, err := s.sendNext(ctx, rules, st.Account, hold); !more {
			return out, err
		}
	}

	return s.nextQueued(ctx, st.Account)
}

// rulesStill reads the rules again between two sends; stop is what the run comes to when
// they can't be read, or reading is off, which the next run sees, skipping what is queued.
func (s *Service) rulesStill(ctx context.Context) (threadrules.Rules, *sendOutcome, error) {
	now := s.clock.Now()

	read, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return read, &sendOutcome{next: now.Add(FailedRetry)}, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	if !read.Read {
		return read, &sendOutcome{next: now}, nil
	}

	return read, nil, nil
}

// sendNext sends account's queued clear due first, if any (sendOne). more reports whether
// the run goes on to the next; otherwise out and err are what the run comes to: when the
// next may go once none is due, or the stop the clear's send called for.
func (s *Service) sendNext(
	ctx context.Context, rules threadrules.Rules, account string, hold time.Duration,
) (out sendOutcome, more bool, err error) {
	now := s.clock.Now()

	id, err := s.q.NextDueClear(ctx, queries.NextDueClearParams{AccountLogin: account, Now: now.UTC()})
	if errors.Is(err, sql.ErrNoRows) {
		out, err = s.nextQueued(ctx, account)

		return out, false, err
	}

	if err != nil {
		return sendOutcome{next: now.Add(FailedRetry)}, false, fmt.Errorf("read the queued clears of GitHub notifications: %w", err)
	}

	step, err := s.sendOne(ctx, rules, id, hold)

	switch {
	case step.blocked:
		return sendOutcome{}, false, err
	case step.stop:
		return sendOutcome{next: step.at}, false, err
	case err != nil:
		return sendOutcome{next: now.Add(FailedRetry)}, false, err
	default:
		return sendOutcome{}, true, nil
	}
}

// nextQueued returns when account's queued clear due first may be sent.
func (s *Service) nextQueued(ctx context.Context, account string) (sendOutcome, error) {
	next, err := s.q.NextSendAfter(ctx, account)
	if err != nil {
		return sendOutcome{next: s.clock.Now().Add(FailedRetry)}, fmt.Errorf("read the queued clears of GitHub notifications: %w", err)
	}

	if len(next) == 0 {
		return sendOutcome{}, nil
	}

	return sendOutcome{next: next[0]}, nil
}

// sendOne checks the queued clear id again and sends it, as long as nothing changed that
// holds it back: clearing on its own still on (for a clear the user didn't ask for), the
// clear queued less than QueuedFor ago, its thread still listed at the update it was
// decided on, and the rules still clearing it. A clear that fails a check is skipped, and
// one whose thread changed, or that waited too long, is decided again. The token's account
// must be the clear's, or the queue waits.
//
// GitHub taking it, or having no such thread, sends it: the thread's row is dropped when
// it was marked done, or marked read. A chore lane refusal stops the run until the lane
// lets the next go, and costs no attempt; any other failure costs one, and the clear fails
// after MaxAttempts.
func (s *Service) sendOne(ctx context.Context, rules threadrules.Rules, id int64, hold time.Duration) (sendStep, error) {
	s.sending.Lock()
	defer s.sending.Unlock()

	now := s.clock.Now()

	row, err := s.q.GetClear(ctx, id)
	if err != nil {
		return sendStep{stop: true, at: now.Add(FailedRetry)}, fmt.Errorf("read a queued clear: %w", err)
	}

	if ClearState(row.State) != ClearQueued {
		return goOn, nil
	}

	verdict, th, err := s.recheck(ctx, rules, &row, now, hold)
	if err != nil || verdict.Kind != threadrules.VerdictClear {
		return goOn, err
	}

	action := threadrules.DecodeAction(string(rules.Action))
	if string(action) != row.Action {
		if _, err = s.q.SetClearAction(ctx, queries.SetClearActionParams{Action: string(action), ID: row.ID}); err != nil {
			return goOn, fmt.Errorf("record what a clear does: %w", err)
		}
	}

	err = s.sendClear(ctx, row.AccountLogin, row.ThreadID, action)

	return s.afterSend(ctx, &row, th, action, err, now)
}

// recheck decides the queued clear row again, under rules, as of now, and settles it when
// it no longer clears: skipped, or decided again when its thread changed or it waited
// QueuedFor. It returns the verdict, which clears only when the clear may go as it is,
// and the thread.
func (s *Service) recheck(
	ctx context.Context, rules threadrules.Rules, row *queries.GetClearRow, now time.Time, hold time.Duration,
) (threadrules.Verdict, *decideRow, error) {
	byUser := threadrules.Trigger(row.Trigger) == threadrules.TriggerByHand
	none := threadrules.Verdict{Kind: threadrules.VerdictSkip}

	if !byUser && !rules.Auto {
		return none, nil, s.settle(ctx, row.ID, ClearSkipped, now, "", SkipOff)
	}

	got, err := s.q.GetDecideThread(ctx, queries.GetDecideThreadParams{AccountLogin: row.AccountLogin, ThreadID: row.ThreadID})
	if errors.Is(err, sql.ErrNoRows) {
		return none, nil, s.settle(ctx, row.ID, ClearSkipped, now, "", SkipNotListed)
	}

	if err != nil {
		return none, nil, fmt.Errorf("read a GitHub notification to clear: %w", err)
	}

	th := decideRow(got)

	switch {
	case now.Sub(row.QueuedAt) >= QueuedFor:
		return none, nil, s.decideAgain(ctx, rules, row, &th, now, hold, SkipStale)
	case !th.UpdatedAt.Equal(row.ThreadUpdatedAt):
		return none, nil, s.decideAgain(ctx, rules, row, &th, now, hold, SkipChanged)
	}

	verdict, _, err := s.verdictOf(ctx, rules, row.AccountLogin, threadrules.Trigger(row.Trigger), &th, now, hold)
	if err != nil {
		return none, nil, err
	}

	if verdict.Kind != threadrules.VerdictClear {
		return verdict, nil, s.settle(ctx, row.ID, ClearSkipped, now, "", SkipReason(verdict.Reason))
	}

	return verdict, &th, nil
}

// verdictOf decides th, a thread of account's, as of now: by the user's hand for a clear
// they asked for, else against its item's moments. It also returns when a clear may go.
func (s *Service) verdictOf(
	ctx context.Context, rules threadrules.Rules, account string, trigger threadrules.Trigger, th *decideRow, now time.Time,
	hold time.Duration,
) (threadrules.Verdict, decision, error) {
	if trigger == threadrules.TriggerByHand {
		v := byHand(rules, th, now)

		return v, decision{verdict: v, sendAfter: now}, nil
	}

	rows, err := s.q.ListItemMoments(ctx, queries.ListItemMomentsParams{
		AccountLogin: account, RepoOwner: th.RepoOwner, RepoName: th.RepoName, Number: th.Number,
	})
	if err != nil {
		return threadrules.Verdict{}, decision{}, fmt.Errorf("read the moments that clear GitHub notifications: %w", err)
	}

	moments := make([]moment, 0, len(rows))
	for _, m := range rows {
		moments = append(moments, moment{trigger: threadrules.Trigger(m.Trigger), at: m.At, heard: m.CreatedAt})
	}

	d, ok := decideThread(rules, th, moments, now, hold, false)
	if !ok {
		return threadrules.Verdict{Kind: threadrules.VerdictSkip, Reason: threadrules.ReasonStillOpen}, d, nil
	}

	return d.verdict, d, nil
}

// decideAgain settles the queued clear row as skipped for reason, then decides its thread
// th again and, when it still clears and may be queued, queues a fresh clear at its
// current update, in one transaction: by the user's hand at once for one they asked for,
// else as the moments say.
func (s *Service) decideAgain(
	ctx context.Context, rules threadrules.Rules, row *queries.GetClearRow, th *decideRow, now time.Time,
	hold time.Duration, reason SkipReason,
) error {
	trigger := threadrules.Trigger(row.Trigger)

	verdict, d, err := s.verdictOf(ctx, rules, row.AccountLogin, trigger, th, now, hold)
	if err != nil {
		return err
	}

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if err := settleWith(ctx, q, row.ID, ClearSkipped, now, "", reason); err != nil {
			return err
		}

		if verdict.Kind != threadrules.VerdictClear {
			return nil
		}

		return queueAgain(ctx, q, row.AccountLogin, th, trigger, verdict.Trigger, rules, d.sendAfter, now)
	})
}

// queueAgain queues account's thread th again, through q, at now, to go at sendAfter: by
// the user's hand when was is by hand, else by chosen, the trigger deciding it again chose,
// unless a clear of it may not be queued on its own (marks' eligible).
func queueAgain(
	ctx context.Context, q *queries.Queries, account string, th *decideRow, was, chosen threadrules.Trigger,
	rules threadrules.Rules, sendAfter, now time.Time,
) error {
	trigger := threadrules.TriggerByHand

	if was != threadrules.TriggerByHand {
		clears, err := q.ListClearMarks(ctx, account)
		if err != nil {
			return fmt.Errorf("read the cleared GitHub notifications: %w", err)
		}

		if !marksOf(clears).eligible(th) {
			return nil
		}

		trigger = chosen
	}

	action := threadrules.DecodeAction(string(rules.Action))
	if _, err := q.InsertClear(ctx, clearParams(account, th, trigger, action, sendAfter, now)); err != nil {
		return fmt.Errorf("queue a GitHub notification's clear again: %w", err)
	}

	return nil
}

// sendClear marks account's thread id done or read, as action says, with a token that
// belongs to account (errOtherAccount otherwise). A token that can't be borrowed, or whose
// account can't be learned, is a waitError: not GitHub refusing the clear.
func (s *Service) sendClear(ctx context.Context, account string, id int64, action threadrules.Action) error {
	borrowed := false

	_, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (struct{}, error) {
		borrowed = true

		login, err := s.viewer(ctx, tok)
		if err != nil {
			return struct{}{}, &waitError{err: err}
		}

		if !strings.EqualFold(login, account) {
			return struct{}{}, errOtherAccount
		}

		return struct{}{}, s.sendMark(ctx, tok, id, action)
	})

	if err != nil && !borrowed {
		return &waitError{err: err, blocked: true}
	}

	return err
}

// waitError is a clear not sent for a reason that isn't GitHub refusing it, which costs
// no attempt: blocked until the next pass (no token), or tried again FailedRetry on.
type waitError struct {
	err     error
	blocked bool
}

func (e *waitError) Error() string { return e.err.Error() }

func (e *waitError) Unwrap() error { return e.err }

// afterSend records what sending row came to (err), at now, and says what the run does
// next.
func (s *Service) afterSend(
	ctx context.Context, row *queries.GetClearRow, th *decideRow, action threadrules.Action, err error, now time.Time,
) (sendStep, error) {
	wait, waiting := errors.AsType[*waitError](err)

	switch {
	case err == nil || errors.Is(err, github.ErrThreadNotFound):
		return goOn, s.recordSent(ctx, row, th, action, err != nil, now)
	case errors.Is(err, errOtherAccount):
		s.logger.InfoContext(ctx, "the GitHub token belongs to another account; its notifications' clears wait")

		return sendStep{stop: true, blocked: true}, nil
	case waiting:
		if wait.blocked {
			return sendStep{stop: true, blocked: true}, nil
		}

		return sendStep{stop: true, at: now.Add(FailedRetry)}, nil
	case errors.Is(err, github.ErrChoreRefused):
		s.logger.WarnContext(ctx, "the GitHub client sends no clears of notifications", slog.Any("error", err))

		return sendStep{stop: true, blocked: true}, nil
	case choreLaneRefused(err):
		return sendStep{stop: true, at: now.Add(max(budget.RetryIn(err), time.Millisecond))}, nil
	case ctx.Err() != nil:
		return sendStep{stop: true, at: now.Add(FailedRetry)}, err
	}

	return s.failedSend(ctx, row, err, now)
}

// choreLaneRefused reports whether err is the chore lane holding a clear back before it was
// sent: the app's pace, GitHub's hold, or the budget's reserve.
func choreLaneRefused(err error) bool {
	return errors.Is(err, budget.ErrWritePace) || errors.Is(err, budget.ErrWritesHeld) ||
		errors.Is(err, budget.ErrBudgetReserved)
}

// failedSend records a failed send of row, at now: tried again FailedRetry on, or failed
// after MaxAttempts. The run stops either way, since GitHub is failing.
func (s *Service) failedSend(ctx context.Context, row *queries.GetClearRow, err error, now time.Time) (sendStep, error) {
	attempts := row.Attempts + 1
	message := uierr.Message(err)

	s.logger.WarnContext(ctx, "clearing a GitHub notification failed", slog.Int64("thread", row.ThreadID),
		slog.Int64("attempt", attempts), slog.Any("error", err))

	if attempts >= MaxAttempts {
		at := now.UTC()

		_, failErr := s.q.FailClear(ctx, queries.FailClearParams{SettledAt: &at, Attempts: attempts, Error: message, ID: row.ID})
		if failErr != nil {
			failErr = fmt.Errorf("record a failed clear: %w", failErr)
		}

		return sendStep{stop: true, at: now.Add(FailedRetry)}, failErr
	}

	_, retryErr := s.q.RetryClear(ctx, queries.RetryClearParams{
		Attempts: attempts, Error: message, SendAfter: now.Add(FailedRetry).UTC(), ID: row.ID,
	})
	if retryErr != nil {
		return sendStep{stop: true, at: now.Add(FailedRetry)}, fmt.Errorf("record a failed clear: %w", retryErr)
	}

	return sendStep{stop: true, at: now.Add(FailedRetry)}, nil
}

// recordSent records that GitHub took row's clear, doing action, or had no such thread
// (gone), at now, in one transaction: the clear is sent, and its thread is dropped once
// done, or gone, or marked read.
func (s *Service) recordSent(
	ctx context.Context, row *queries.GetClearRow, th *decideRow, action threadrules.Action, gone bool, now time.Time,
) error {
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if err := markInMirror(ctx, q, row.AccountLogin, row.ThreadID, action, gone); err != nil {
			return err
		}

		return settleWith(ctx, q, row.ID, ClearSent, now, "", SkipNone)
	})
	if err != nil {
		return fmt.Errorf("record a cleared GitHub notification: %w", err)
	}

	title := row.Title
	if th != nil {
		title = th.SubjectTitle
	}

	s.logger.DebugContext(ctx, "cleared a GitHub notification", slog.Int64("thread", row.ThreadID),
		slog.String("title", title), slog.String("action", string(action)), slog.Bool("gone", gone))

	return nil
}

// settle takes the queued clear id out of the queue as state, at now, with the error or
// the reason it was skipped.
func (s *Service) settle(ctx context.Context, id int64, state ClearState, now time.Time, message string, reason SkipReason) error {
	return settleWith(ctx, s.q, id, state, now, message, reason)
}

// settleWith is settle through q.
func settleWith(
	ctx context.Context, q *queries.Queries, id int64, state ClearState, now time.Time, message string, reason SkipReason,
) error {
	at := now.UTC()
	params := queries.SettleClearParams{
		State: string(state), SettledAt: &at, Error: message, SkipReason: string(reason), ID: id,
	}

	if state == ClearSent {
		params.SentAt = &at
	}

	if _, err := q.SettleClear(ctx, params); err != nil {
		return fmt.Errorf("record a clear as %s: %w", state, err)
	}

	return nil
}
