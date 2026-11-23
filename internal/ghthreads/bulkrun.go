package ghthreads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// bulkOutcome is what a bulk run leaves for the next: when it is due (next, zero to wait
// for a pass, or for none, the job over), and whether the window is told at once (tell).
type bulkOutcome struct {
	next time.Time
	tell bool
}

// bulkStep is what one step of a bulk run says of the next: go on (more), or stop the run
// with out.
type bulkStep struct {
	more bool
	out  bulkOutcome
}

// goOnBulk is the step that goes on to the next thread.
var goOnBulk = bulkStep{more: true} //nolint:gochecknoglobals // a fixed value

// bulkPage is the page of the listing a bulk run read last, which the runs that follow
// mark while it stands (bulkPageFor): the job (by its start) and the cursor it was read
// for, when, the threads on it still to mark, in order, whether it was full, and its
// oldest update.
type bulkPage struct {
	job     time.Time
	cursor  time.Time
	readAt  time.Time
	targets []int64
	full    bool
	oldest  time.Time
}

// bulkRun is one bulk run: the job as it stands, kept current as the run changes it, and
// whether GitHub holds more than the mirror.
type bulkRun struct {
	svc       *Service
	b         bulkState
	truncated bool
}

// bulkDue runs the bulk mark, when one runs: as many marks as its lane lets go now, with a
// token of the job's account, then says when the next run is due. A failure GitHub, or
// the token, is behind pauses the job and is recorded in it, so only a failure of the
// database is returned.
func (s *Service) bulkDue(ctx context.Context) (bulkOutcome, error) {
	now := s.clock.Now()

	b, err := s.loadBulk(ctx)
	if err != nil {
		return bulkOutcome{next: now.Add(FailedRetry)}, err
	}

	if !b.running() {
		s.bulkPage = nil

		return bulkOutcome{}, nil
	}

	st, err := s.loadState(ctx)
	if err != nil {
		return bulkOutcome{next: now.Add(FailedRetry)}, err
	}

	r := &bulkRun{svc: s, b: b, truncated: st.Truncated}
	was := b.settled()

	out, err := r.run(ctx)
	out.tell = out.tell || !reflect.DeepEqual(was, r.b.settled())

	if !r.b.running() {
		s.bulkPage = nil
		out = bulkOutcome{tell: true}
	}

	return out, err
}

// bulkReadingOff is a bulk run while reading is off: the job waits, sending nothing, until
// a pass with reading back on has it go on.
func (s *Service) bulkReadingOff(ctx context.Context) (bulkOutcome, error) {
	_, changed, err := s.changeBulk(ctx, func(_ *queries.Queries, b *bulkState) (bool, error) {
		if !b.running() || b.Pause == BulkPauseReadingOff {
			return false, nil
		}

		b.Pause, b.NextAt = BulkPauseReadingOff, time.Time{}

		return true, nil
	})

	return bulkOutcome{tell: changed}, err
}

// run borrows a token, checks it is the job's account's, and marks the threads due. A
// token of another account, or none, pauses the job until the next pass; a token GitHub
// refused twice, or an account that can't be learned, pauses it as failing.
func (r *bulkRun) run(ctx context.Context) (bulkOutcome, error) {
	borrowed := false

	out, err := github.WithToken(ctx, r.svc.tokens, func(tok github.Token) (bulkOutcome, error) {
		borrowed = true

		login, err := r.svc.viewer(ctx, tok)
		if err != nil {
			return bulkOutcome{}, &waitError{err: err}
		}

		if !strings.EqualFold(login, r.b.Account) {
			return bulkOutcome{}, errOtherAccount
		}

		return r.sends(ctx, tok)
	})

	_, waiting := errors.AsType[*waitError](err)
	now := r.svc.clock.Now()

	switch {
	case err == nil:
		return out, nil
	case !borrowed || errors.Is(err, errOtherAccount):
		r.svc.logger.InfoContext(ctx, "the GitHub token isn't the account's whose notifications are marked done; "+
			"the bulk mark waits")

		return r.pause(ctx, BulkPauseOtherAccount, time.Time{}, nil)
	case ctx.Err() != nil:
		return bulkOutcome{next: now.Add(FailedRetry)}, err
	case waiting || github.IsBadCredentials(err):
		return r.pause(ctx, BulkPauseFailing, now.Add(FailedRetry), err)
	default:
		return bulkOutcome{next: now.Add(FailedRetry)}, err
	}
}

// sends marks threads with tok, one at a time, until the lane refuses the next, a step
// stops the run, or maxSendsPerRun went.
func (r *bulkRun) sends(ctx context.Context, tok github.Token) (bulkOutcome, error) {
	for range maxSendsPerRun {
		step, err := r.step(ctx, tok)
		if err != nil || !step.more {
			return step.out, err
		}
	}

	return bulkOutcome{next: r.svc.clock.Now()}, nil
}

// step marks the next thread, once the job, read again, still runs: the user may have
// stopped it.
func (r *bulkRun) step(ctx context.Context, tok github.Token) (bulkStep, error) {
	b, err := r.svc.loadBulk(ctx)
	if err != nil {
		return bulkStep{out: bulkOutcome{next: r.svc.clock.Now().Add(FailedRetry)}}, err
	}

	if !b.running() || !b.StartedAt.Equal(r.b.StartedAt) {
		r.b = b

		return bulkStep{}, nil
	}

	r.b = b

	id, step, err := r.next(ctx, tok)
	if id == 0 {
		return step, err
	}

	return r.mark(ctx, tok, id)
}

// next returns the next thread to mark (0 for none, with what the run comes to): the first
// left on the page read last, while it stands, or else on a page read now at the cursor.
// A page with none in scope moves the cursor past it, and a short one ends the job; after
// maxBulkPages such pages, the next run goes on after bulkPagesPause.
func (r *bulkRun) next(ctx context.Context, tok github.Token) (int64, bulkStep, error) {
	now := r.svc.clock.Now()

	for range maxBulkPages {
		if id, found, step, err := r.nextOnPage(ctx, tok, now); found {
			return id, step, err
		}
	}

	return 0, bulkStep{out: bulkOutcome{next: now.Add(bulkPagesPause)}}, nil
}

// nextOnPage returns the next thread to mark on the page read last, or on the page at the
// cursor read now, with what the run comes to when there is none; found is false when the
// page had none in scope and the cursor moved past it, so the next page is read.
func (r *bulkRun) nextOnPage(
	ctx context.Context, tok github.Token, now time.Time,
) (id int64, found bool, step bulkStep, err error) {
	if id, err = r.cached(ctx, now); id != 0 || err != nil {
		return id, true, bulkStep{}, err
	}

	got, listErr := r.svc.client.ListThreads(ctx, tok, github.ThreadQuery{Page: 1, Before: r.b.cursor()})
	if listErr != nil {
		step, err = r.listFailed(ctx, listErr, now)

		return 0, true, step, err
	}

	switch page := r.pageOf(got, now); {
	case len(page.targets) > 0:
		r.svc.bulkPage = page

		return page.targets[0], true, bulkStep{}, nil
	case !page.full:
		return 0, true, bulkStep{}, r.end(ctx, "")
	default:
		if err = r.moveCursor(ctx, pastPage(page.oldest, r.b.cursor())); err != nil {
			return 0, true, bulkStep{out: bulkOutcome{next: now.Add(FailedRetry)}}, err
		}

		return 0, false, bulkStep{}, nil
	}
}

// cached returns the first thread left on the page read last, while it stands for this
// job at its cursor; 0 when there is none, and the page is read again. A full page whose
// threads were all marked moves the cursor past them first, to its oldest update.
func (r *bulkRun) cached(ctx context.Context, now time.Time) (int64, error) {
	p := r.svc.bulkPage
	if p == nil || !p.job.Equal(r.b.StartedAt) || !p.cursor.Equal(r.b.cursor()) || now.Sub(p.readAt) >= bulkPageFor {
		r.svc.bulkPage = nil

		return 0, nil
	}

	if len(p.targets) > 0 {
		return p.targets[0], nil
	}

	r.svc.bulkPage = nil

	if !p.full || p.oldest.IsZero() {
		return 0, nil
	}

	if past := p.oldest.Add(time.Second); past.Before(r.b.cursor()) {
		return 0, r.moveCursor(ctx, past)
	}

	return 0, nil
}

// pageOf is got, a page of the listing read at now, as the run marks it: the threads on
// it the job marks, in order.
func (r *bulkRun) pageOf(got *github.ThreadPage, now time.Time) *bulkPage {
	page := &bulkPage{
		job: r.b.StartedAt, cursor: r.b.cursor(), readAt: now,
		full: len(got.Threads)+got.Dropped >= github.ThreadsPerPage,
	}

	page.oldest, _ = oldestUpdate(got.Threads)

	for i := range got.Threads {
		t := &got.Threads[i]
		if r.b.marks(t.ID, t.Repo.Owner, t.Repo.Repo, t.UpdatedAt) {
			page.targets = append(page.targets, t.ID)
		}
	}

	return page
}

// pastPage is the cursor past a full page none of whose threads the job marks, read at
// cursor: its oldest update and a second, which lists again the threads updated in the
// same second, or else, when that doesn't move it, a second before cursor.
func pastPage(oldest, cursor time.Time) time.Time {
	if past := oldest.Add(time.Second); !oldest.IsZero() && past.Before(cursor) {
		return past
	}

	return cursor.Add(-time.Second)
}

// mark marks the thread id done with tok, unless the mirror lists it as updated since the
// job's cutoff, which is new activity the user may not have seen, and records what GitHub
// answered. It holds sending while it does, as a queued clear's send does.
func (r *bulkRun) mark(ctx context.Context, tok github.Token, id int64) (bulkStep, error) {
	fresh, err := r.updatedSince(ctx, id)
	if err != nil {
		return bulkStep{out: bulkOutcome{next: r.svc.clock.Now().Add(FailedRetry)}}, err
	}

	if fresh {
		r.dropTarget(id)

		return goOnBulk, nil
	}

	r.svc.sending.Lock()
	defer r.svc.sending.Unlock()

	return r.after(ctx, id, r.svc.client.MarkThreadDoneInBulk(ctx, tok, id))
}

// updatedSince reports whether the mirror lists the thread id as updated at or after the
// job's cutoff.
func (r *bulkRun) updatedSince(ctx context.Context, id int64) (bool, error) {
	row, err := r.svc.q.GetDecideThread(ctx, queries.GetDecideThreadParams{AccountLogin: r.b.Account, ThreadID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("read a GitHub notification to mark done: %w", err)
	}

	return !row.UpdatedAt.Before(r.b.Before), nil
}

// after records what marking the thread id done came to (err), and says what the run does
// next: GitHub taking it, or having no such thread, counts it done; the lane refusing it
// waits as long as the lane says; any other failure is tried again FailedRetry on, and given
// up on after MaxAttempts.
func (r *bulkRun) after(ctx context.Context, id int64, err error) (bulkStep, error) {
	now := r.svc.clock.Now()

	switch {
	case err == nil || errors.Is(err, github.ErrThreadNotFound):
		return r.recordDone(ctx, id, err != nil, now)
	case choreLaneRefused(err):
		return r.laneRefused(ctx, err, now)
	case errors.Is(err, github.ErrChoreRefused):
		r.svc.logger.WarnContext(ctx, "the GitHub client marks no notifications done in bulk", slog.Any("error", err))

		return bulkStep{}, r.end(ctx, uierr.Message(markFailure(err, threadrules.ActionDone)))
	case ctx.Err() != nil, github.IsBadCredentials(err):
		return bulkStep{out: bulkOutcome{next: now.Add(FailedRetry)}}, err
	}

	return r.failedMark(ctx, id, err, now)
}

// recordDone records that GitHub took the mark of the thread id, or had no such thread
// (gone), at now, in one transaction: the thread leaves the mirror, a clear queued for it
// is settled, and the job counts it, out of any pause. It tells the window how far the job
// got.
func (r *bulkRun) recordDone(ctx context.Context, id int64, gone bool, now time.Time) (bulkStep, error) {
	b, _, err := r.svc.changeBulk(ctx, func(q *queries.Queries, b *bulkState) (bool, error) {
		if err := markInMirror(ctx, q, r.b.Account, id, threadrules.ActionDone, gone); err != nil {
			return false, err
		}

		if err := settleQueuedFor(ctx, q, r.b.Account, []marked{{id: id, gone: gone}}, now); err != nil {
			return false, err
		}

		if !b.StartedAt.Equal(r.b.StartedAt) {
			return false, nil
		}

		b.Done++
		b.Pause, b.NextAt, b.LastError = BulkPauseNone, time.Time{}, ""

		if b.Attempt.Thread == id {
			b.Attempt = bulkAttempt{}
		}

		return true, nil
	})
	if err != nil {
		return bulkStep{out: bulkOutcome{next: now.Add(FailedRetry)}}, fmt.Errorf("record a GitHub notification marked done: %w", err)
	}

	r.b = b
	r.dropTarget(id)
	r.progress(ctx)

	return goOnBulk, nil
}

// laneRefused stops the run until the bulk lane lets the next mark go, at no cost to the
// thread: a pause in the job when GitHub asked the app to wait, or the budget is down to
// its reserve, and none for the lane's own pace.
func (r *bulkRun) laneRefused(ctx context.Context, err error, now time.Time) (bulkStep, error) {
	next := now.Add(max(budget.RetryIn(err), time.Millisecond))

	var pause BulkPause

	switch {
	case errors.Is(err, budget.ErrWritesHeld):
		pause = BulkPauseHeld
	case errors.Is(err, budget.ErrBudgetReserved):
		pause = BulkPauseReserve
	default:
		return bulkStep{out: bulkOutcome{next: next}}, nil
	}

	out, pauseErr := r.pause(ctx, pause, next, nil)

	return bulkStep{out: out}, pauseErr
}

// failedMark records a failed mark of the thread id at now: the job waits FailedRetry as
// failing, or, after MaxAttempts in a row, gives the thread up and goes on, stopping once
// it gave up on maxBulkSkipped.
func (r *bulkRun) failedMark(ctx context.Context, id int64, cause error, now time.Time) (bulkStep, error) {
	message := uierr.Message(markFailure(cause, threadrules.ActionDone))
	gaveUp := false

	r.svc.logger.WarnContext(ctx, "marking a GitHub notification done in bulk failed", slog.Int64("thread", id),
		slog.Any("error", cause))

	b, _, err := r.svc.changeBulk(ctx, func(_ *queries.Queries, b *bulkState) (bool, error) {
		if !b.running() || !b.StartedAt.Equal(r.b.StartedAt) {
			return false, nil
		}

		b.LastError = message
		gaveUp = b.failed(id, now)

		return true, nil
	})
	if err != nil {
		return bulkStep{out: bulkOutcome{next: now.Add(FailedRetry)}}, err
	}

	r.b = b

	if !gaveUp {
		return bulkStep{out: bulkOutcome{next: now.Add(FailedRetry)}}, nil
	}

	r.dropTarget(id)
	r.progress(ctx)

	if !b.running() {
		r.svc.logger.InfoContext(ctx, "stopped marking GitHub notifications done in bulk: GitHub refused too many",
			slog.Int("done", b.Done), slog.Int("failed", b.Failed))

		return bulkStep{}, nil
	}

	return goOnBulk, nil
}

// failed counts a failed mark of the thread id at now, and reports whether the job gives
// it up: after MaxAttempts in a row, when it goes on without a pause, or ends once it gave
// up on maxBulkSkipped. Otherwise it waits FailedRetry, failing.
func (b *bulkState) failed(id int64, now time.Time) bool {
	if b.Attempt.Thread != id {
		b.Attempt = bulkAttempt{Thread: id}
	}

	b.Attempt.Count++

	if b.Attempt.Count < MaxAttempts {
		b.Pause, b.NextAt = BulkPauseFailing, now.Add(FailedRetry)

		return false
	}

	b.Failed++
	b.Skipped = append(b.Skipped, id)
	b.Attempt, b.Pause, b.NextAt = bulkAttempt{}, BulkPauseNone, time.Time{}

	if len(b.Skipped) >= maxBulkSkipped {
		b.EndedAt = now
		b.LastError = uierr.Message(uierr.New("GitHub refused too many of them, so marking them done stopped.", errBulkGaveUp))
	}

	return true
}

// listFailed pauses the job as failing after a listing GitHub failed, tried again
// FailedRetry on; a token GitHub refused goes back to be borrowed again.
func (r *bulkRun) listFailed(ctx context.Context, err error, now time.Time) (bulkStep, error) {
	if github.IsBadCredentials(err) || ctx.Err() != nil {
		return bulkStep{out: bulkOutcome{next: now.Add(FailedRetry)}}, err
	}

	r.svc.logger.WarnContext(ctx, "reading GitHub notifications to mark done in bulk failed", slog.Any("error", err))

	out, pauseErr := r.pause(ctx, BulkPauseFailing, now.Add(FailedRetry),
		uierr.New("Couldn't read your GitHub notifications: "+uierr.Message(err), err))

	return bulkStep{out: out}, pauseErr
}

// pause has the job wait for pause until next, zero for a pass, with cause's words as its
// last error, when it has one, and returns when the next run is due.
func (r *bulkRun) pause(ctx context.Context, pause BulkPause, next time.Time, cause error) (bulkOutcome, error) {
	b, _, err := r.svc.changeBulk(ctx, func(_ *queries.Queries, b *bulkState) (bool, error) {
		if !b.running() || !b.StartedAt.Equal(r.b.StartedAt) {
			return false, nil
		}

		b.Pause, b.NextAt = pause, next

		if cause != nil {
			b.LastError = uierr.Message(cause)
		}

		return true, nil
	})
	if err != nil {
		return bulkOutcome{next: r.svc.clock.Now().Add(FailedRetry)}, err
	}

	r.b = b

	return bulkOutcome{next: next}, nil
}

// moveCursor moves the job's cursor to at.
func (r *bulkRun) moveCursor(ctx context.Context, at time.Time) error {
	b, _, err := r.svc.changeBulk(ctx, func(_ *queries.Queries, b *bulkState) (bool, error) {
		if !b.running() || !b.StartedAt.Equal(r.b.StartedAt) {
			return false, nil
		}

		b.Cursor = at.UTC()

		return true, nil
	})
	if err != nil {
		return err
	}

	r.b = b

	return nil
}

// end ends the job, with message as its last error when it has one: nothing it marks is
// left on GitHub, or it can't mark any.
func (r *bulkRun) end(ctx context.Context, message string) error {
	now := r.svc.clock.Now()

	b, ended, err := r.svc.changeBulk(ctx, func(_ *queries.Queries, b *bulkState) (bool, error) {
		if !b.running() || !b.StartedAt.Equal(r.b.StartedAt) {
			return false, nil
		}

		b.EndedAt, b.Pause, b.NextAt = now.UTC(), BulkPauseNone, time.Time{}

		if message != "" {
			b.LastError = message
		}

		return true, nil
	})
	if err != nil {
		return err
	}

	r.b = b

	if ended {
		r.svc.logger.InfoContext(ctx, "marked GitHub notifications done in bulk", slog.Int("done", b.Done),
			slog.Int("failed", b.Failed))
	}

	return nil
}

// dropTarget takes the thread id off the page read last.
func (r *bulkRun) dropTarget(id int64) {
	if p := r.svc.bulkPage; p != nil {
		p.targets = slices.DeleteFunc(p.targets, func(t int64) bool { return t == id })
	}
}

// progress tells the window how far the job got: its counts, and how many in its scope
// the mirror still lists. A failure to count is logged, and tells nothing.
func (r *bulkRun) progress(ctx context.Context) {
	rows, err := r.svc.q.ListThreads(ctx, r.b.Account)
	if err != nil {
		r.svc.logger.DebugContext(ctx, "counting the GitHub notifications left to mark done failed", slog.Any("error", err))

		return
	}

	r.svc.events.Emit(event.GHThreadsBulk{Done: r.b.Done, Failed: r.b.Failed, Left: r.b.left(rows), More: r.truncated})
}

// untilBulk says how long until the next bulk run is due, on the Service's clock, which the
// Syncer waits on the real one; ok is false when none waits on a time.
func (s *Service) untilBulk() (time.Duration, bool) {
	s.mu.Lock()
	at := s.bulkAt
	s.mu.Unlock()

	if at.IsZero() {
		return 0, false
	}

	return at.Sub(s.clock.Now()), true
}

// bulkBy has the next bulk run go by at, unless one is due sooner, and moves the
// generation on, so a run going on keeps it (planBulk).
func (s *Service) bulkBy(at time.Time) {
	s.mu.Lock()
	s.bulkGen++
	s.bulkAt = earliest(s.bulkAt, at)
	s.mu.Unlock()
}

// bulkOff has no bulk run come due on a time, as when the job stopped.
func (s *Service) bulkOff() {
	s.mu.Lock()
	s.bulkGen++
	s.bulkAt = time.Time{}
	s.mu.Unlock()
}

// bulkGeneration is how many times bulkBy or bulkOff was called (see sendGeneration).
func (s *Service) bulkGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.bulkGen
}

// planBulk records when the next bulk run is due, as a bulk run did left it: FailedRetry
// on for one that stopped before it said, a panic included. A job started while the run
// went on (bulkBy, which moves the generation on from gen) is never put off.
func (s *Service) planBulk(req passRequest, did ran, gen uint64) {
	if !req.bulk {
		return
	}

	at := s.clock.Now().Add(FailedRetry)
	if did.bulk != nil {
		at = did.bulk.next
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bulkGen != gen {
		at = earliest(at, s.bulkAt)
	}

	s.bulkAt = at
}

// armBulk has a bulk run come due now after a pass, when the job runs and waits on no
// time: after a restart, which leaves it unfinished in its preference, and after a pause
// that waits for a pass (another account's token, reading turned off).
func (s *Service) armBulk(ctx context.Context) {
	if _, ok := s.untilBulk(); ok {
		return
	}

	b, err := s.loadBulk(ctx)
	if err != nil {
		s.logger.DebugContext(ctx, "reading the bulk mark of GitHub notifications failed", slog.Any("error", err))

		return
	}

	if b.running() {
		s.bulkBy(s.clock.Now())
	}
}

// tellAfter tells the window what the run changed (tell), but for a bulk run that changed
// only its counts within bulkTellEvery of the last time one told: its progress events say
// those.
func (s *Service) tellAfter(ctx context.Context, req passRequest, did ran, before *Snapshot) {
	now := s.clock.Now()

	if req.bulk && did.bulk != nil && !did.bulk.tell && now.Sub(s.bulkToldAt) < bulkTellEvery {
		return
	}

	s.bulkToldAt = now
	s.tell(ctx, before)
}
