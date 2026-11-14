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

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// refusedMessage is what the window says while GitHub refuses the listing.
const refusedMessage = "Your GitHub token can't read notifications."

// passRequest is what a pass is asked to do beyond what is due.
type passRequest struct {
	// now sends the listing even while a refused one waits RefusedFor: CheckNow.
	now bool

	// timed is a pass the Syncer found due when the last pass had ended at lastEnded: once
	// it holds the running slot, it is skipped if another pass (CheckNow's) ended since,
	// which the Syncer then waits from.
	timed     bool
	lastEnded time.Time

	// check are pull requests and issues whose state is checked this pass, whenever it
	// was checked last, as long as a thread of the account's is about one: those the inbox
	// saw close or leave its lists.
	check []item.Ref

	// nudged is a run the Syncer was nudged to: it lists only when the mirror is older than
	// GitHub's poll interval (stale), and decides only the threads about refs and check, or
	// every thread when all is set (the rules changed). Any other pass lists and decides
	// every thread.
	nudged bool
	refs   []item.Ref
	all    bool

	// send is a run that only sends the queued clears that are due, when the Syncer's timer
	// says one is: it lists nothing and decides nothing anew.
	send bool
}

// decidesAll reports whether the run decides every thread, rather than those of its refs.
func (r passRequest) decidesAll() bool {
	return !r.nudged || r.all
}

// scope is the items whose threads the run decides, by key; nil decides every thread.
func (r passRequest) scope() map[string]bool {
	if r.decidesAll() {
		return nil
	}

	out := make(map[string]bool, len(r.refs)+len(r.check))
	for _, ref := range append(slices.Clone(r.refs), r.check...) {
		out[ref.Key()] = true
	}

	return out
}

// listing is what a pass's listing read, and the state it leaves.
type listing struct {
	// state is the state of the passes once the listing is stored.
	state syncState

	// threads are the threads read, each once, the newest first.
	threads []github.Thread

	// walkedAll is whether the listing read every page, to the last there is or the last
	// the app reads: the account's rows it didn't list are deleted.
	walkedAll bool

	// backedOff is a listing not sent, since GitHub refused the last one less than
	// RefusedFor ago: nothing is stored.
	backedOff bool

	// floor is the oldest update on the last page read, when the listing stopped at the
	// high-water mark short of the last page: it covered every thread updated after it.
	floor time.Time

	// pages is how many pages were read, and notModified whether page 1 was answered 304.
	pages       int
	notModified bool

	// seen are the IDs of threads, while the pages are read.
	seen map[int64]bool
}

// refusedError is GitHub refusing the listing for the account login (403 or 404).
type refusedError struct {
	login string
	err   error
}

func (e *refusedError) Error() string { return e.err.Error() }

func (e *refusedError) Unwrap() error { return e.err }

// runPass runs one pass, once any other has ended, then tells the window when what
// Snapshot shows changed, and works out when the next queued clear may be sent. A timed
// pass that finds another ended while it waited runs nothing. Every pass but a nudged one
// that didn't list, and a run that only sends, counts as the last pass for the Syncer's
// timer. Both are done however the run ends, a panic included, so a run that panics never
// has the Syncer run it again at once.
func (s *Service) runPass(ctx context.Context, req passRequest) (err error) {
	if err = ctx.Err(); err != nil {
		return fmt.Errorf("start a pass: %w", err)
	}

	select {
	case s.running <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait for the pass running: %w", ctx.Err())
	}

	defer func() { <-s.running }()

	if req.timed && !s.lastEnded().Equal(req.lastEnded) {
		return nil
	}

	var did ran

	gen := s.sendGeneration()
	defer func() { s.endRun(req, did, gen) }()

	before := s.shownBefore(ctx)
	did, err = s.pass(ctx, req)

	s.tell(ctx, before)

	return err
}

// endRun does what follows any run, however it ended: it records when the next queued
// clear may be sent (planSend), and, for a pass that listed or meant to, that it ended.
func (s *Service) endRun(req passRequest, did ran, gen uint64) {
	s.planSend(req, did, gen)

	if did.listed || (!req.nudged && !req.send) {
		s.passEnded()
	}
}

// shownBefore is what Snapshot showed at the end of the last pass, or else shows now: what
// a pass compares with what it leaves. It is nil when neither can be read.
func (s *Service) shownBefore(ctx context.Context) *Snapshot {
	if s.shown != nil {
		return s.shown
	}

	snap, err := s.Snapshot(ctx)
	if err != nil {
		return nil
	}

	return &snap
}

// ran is what one run did, for what follows it.
type ran struct {
	// listed is whether it sent the listing, or meant to.
	listed bool

	// sends is when the next queued clear may be sent, as the run's sends left it: nil when
	// it stopped before them.
	sends *sendOutcome
}

// planSend records when the next queued clear may be sent, as the run did left it. A run
// that stopped before its sends leaves it as it was, unless it was to send, which tries
// again FailedRetry on, a send that panicked included: the Syncer never runs one send
// after another that failed. A clear the user queued while the run went on (sendBy, which
// moves the generation on from gen, the one the run began at) is never put off: the
// earlier time stands.
func (s *Service) planSend(req passRequest, did ran, gen uint64) {
	var at time.Time

	switch {
	case did.sends != nil:
		at = did.sends.next
	case req.send:
		at = s.clock.Now().Add(FailedRetry)
	default:
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.sendGen != gen {
		at = earliest(at, s.sendAt)
	}

	s.sendAt = at
}

// tell reads what Snapshot shows now, keeps it for the next pass, and tells the window
// when it differs from before (the passes' times aside), or can't be read. A pass whose
// caller gave up (the app quitting, or the window no longer waiting on CheckNow) tells
// nothing, and the next compares with what was shown before it.
func (s *Service) tell(ctx context.Context, before *Snapshot) {
	after, err := s.Snapshot(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}

		s.shown = nil
		s.events.Emit(event.GHThreadsUpdated{})

		return
	}

	s.shown = &after

	if before == nil || !reflect.DeepEqual(compared(*before), compared(after)) {
		s.events.Emit(event.GHThreadsUpdated{})
	}
}

// pass reads the rules, then, while reading is on, lists the notifications (a nudged run
// only while the mirror is stale) and follows the listing: it checks the items' states,
// decides which threads clear, queues them, and sends the clears that are due (follow). A
// run that only sends goes straight to the sends. Each failure of the listing or the
// states is recorded as the status's last error before it is returned, but a pass the
// caller gave up on.
func (s *Service) pass(ctx context.Context, req passRequest) (ran, error) {
	now := s.clock.Now()

	st, err := s.loadState(ctx)
	if err != nil {
		return ran{}, err
	}

	s.keepPoll(st)

	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return ran{}, s.failed(ctx, st, fmt.Errorf("read the rules for GitHub notifications: %w", err))
	}

	if !rules.Read {
		return ran{sends: &sendOutcome{}}, s.readingOff(ctx, st, now)
	}

	if err = s.skipWhileOff(ctx, rules, now); err != nil {
		return ran{}, err
	}

	if req.send {
		out, sendErr := s.sendDue(ctx, rules, st)

		return ran{sends: &out}, sendErr
	}

	return s.listAndFollow(ctx, req, rules, st, now)
}

// listAndFollow lists the notifications, but for a nudged run while the mirror is fresh,
// then follows the listing (follow), once a pass has learned the account and while GitHub
// lets the token read them.
func (s *Service) listAndFollow(
	ctx context.Context, req passRequest, rules threadrules.Rules, st syncState, now time.Time,
) (ran, error) {
	out := ran{listed: !req.nudged || st.stale(now)}

	if out.listed {
		listed, onward, err := s.listAndKeep(ctx, req, st, now)
		if !onward {
			return out, err
		}

		st = listed
	}

	if st.Account == "" || st.backingOff(now) {
		return out, nil
	}

	sends, err := s.follow(ctx, req, rules, st, out.listed, now)
	out.sends = sends

	return out, err
}

// listAndKeep lists the notifications and stores what the listing read, and returns the
// state it leaves; ok is false when the pass goes no further: the listing failed (err says
// why, recorded as the status's last error), or it backed off from GitHub's refusal.
func (s *Service) listAndKeep(ctx context.Context, req passRequest, st syncState, now time.Time) (syncState, bool, error) {
	listed, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (*listing, error) {
		return s.list(ctx, tok, st, req.now, now)
	})
	if err != nil {
		return st, false, s.listFailed(ctx, st, err, now)
	}

	if listed.backedOff {
		return st, false, nil
	}

	if err = s.keep(ctx, listed, now); err != nil {
		return st, false, s.failed(ctx, st, err)
	}

	s.logger.DebugContext(ctx, "read the GitHub notifications", slog.Int("pages", listed.pages),
		slog.Int("threads", len(listed.threads)), slog.Bool("unchanged", listed.notModified),
		slog.Bool("walkedAll", listed.walkedAll))

	return listed.state, true, nil
}

// readingOff is a pass while reading is off: it sends nothing, skips every queued clear,
// and remembers that the mirror went stale, so the first pass once reading is back on walks
// every page, and counts as the first after the rules changed.
func (s *Service) readingOff(ctx context.Context, st syncState, now time.Time) error {
	if err := s.skipQueued(ctx, s.q, true, now); err != nil {
		return err
	}

	if st.WalkNext && st.RulesSeen == "" && st.Backlog == 0 && st.BacklogRules == "" {
		return nil
	}

	st.WalkNext, st.RulesSeen, st.Backlog, st.BacklogRules = true, "", 0, ""

	return saveState(ctx, s.prefs, st)
}

// failed records err as the status's last error, in the words the window shows, unless
// ctx ended, and returns it.
func (s *Service) failed(ctx context.Context, st syncState, err error) error {
	if ctx.Err() != nil {
		return err
	}

	st.LastError = uierr.Message(err)
	if saveErr := saveState(ctx, s.prefs, st); saveErr != nil {
		return errors.Join(err, saveErr)
	}

	return err
}

// listFailed records why the listing failed. GitHub refusing it marks access refused for
// the account the token belongs to, asks again only after RefusedFor, and has the next
// listing walk every page; any other failure is only the last error, and the next pass
// tries again.
func (s *Service) listFailed(ctx context.Context, st syncState, err error, now time.Time) error {
	refusal, ok := errors.AsType[*refusedError](err)
	if !ok {
		return s.failed(ctx, st, fmt.Errorf("read the GitHub notifications: %w", err))
	}

	if !strings.EqualFold(st.Account, refusal.login) {
		st = syncState{Account: refusal.login, PollSeconds: st.PollSeconds}
	}

	st.Access, st.AskAgainAt, st.WalkNext = AccessRefused, now.Add(RefusedFor), true

	return s.failed(ctx, st, uierr.New(refusedMessage, err))
}

// list learns the token's account, then reads the listing: page 1 with the stored ETag
// and on until the high-water mark, or every page when a walk is due. Unless now, a listing
// GitHub refused less than RefusedFor ago isn't sent. It stores nothing.
func (s *Service) list(ctx context.Context, tok github.Token, st syncState, now bool, at time.Time) (*listing, error) {
	login, err := s.viewer(ctx, tok)
	if err != nil {
		return nil, err
	}

	switched := !strings.EqualFold(st.Account, login)
	if switched {
		// Another account's ETag, mark, and access say nothing of this one's.
		st = syncState{Account: login, PollSeconds: st.PollSeconds}
	}

	if !now && st.backingOff(at) {
		return &listing{state: st, backedOff: true}, nil
	}

	read, err := s.readPages(ctx, tok, st, !s.walked || switched || st.walkDue(at))
	if err != nil {
		if github.NotificationsRefused(err) {
			return nil, &refusedError{login: login, err: err}
		}

		return nil, err
	}

	read.state = advance(st, read, login, at)

	return read, nil
}

// backingOff reports whether GitHub refused the listing less than RefusedFor before at.
func (st syncState) backingOff(at time.Time) bool {
	return st.Access == AccessRefused && at.Before(st.AskAgainAt)
}

// walkDue reports whether a walk of every page is due at: one was asked for, none was
// made, or the last was FullWalkEvery ago.
func (st syncState) walkDue(at time.Time) bool {
	return st.WalkNext || st.LastFullWalk.IsZero() || !at.Before(st.LastFullWalk.Add(FullWalkEvery))
}

// readPages reads the listing's pages: every one when full, and otherwise page 1 with st's
// ETag, then on until a page whose oldest thread is no newer than st's high-water mark. It
// stops at the last page there is, or the last the app reads.
func (s *Service) readPages(ctx context.Context, tok github.Token, st syncState, full bool) (*listing, error) {
	out := &listing{seen: map[int64]bool{}}
	mark := st.HighWater

	if full {
		// A walk reads on to the last page whatever it finds.
		mark = time.Time{}
	}

	for page := 1; page > 0; {
		q := github.ThreadQuery{Page: page}
		if page == 1 && !full {
			q.ETag = st.ETag
		}

		got, err := s.client.ListThreads(ctx, tok, q)
		if err != nil {
			return nil, fmt.Errorf("read page %d: %w", page, err)
		}

		page = out.add(page, got, mark)
	}

	return out, nil
}

// add takes in got, page of the listing, and returns the page to read next, 0 for none: a
// 304 ends the listing, as does the last page, or, past the zero mark, a page whose oldest
// thread is no newer than mark.
func (l *listing) add(page int, got *github.ThreadPage, mark time.Time) int {
	l.pages = page
	l.state.PollSeconds = max(l.state.PollSeconds, int(got.PollInterval/time.Second))

	if page == 1 {
		l.state.ETag = got.ETag
	}

	if got.NotModified {
		l.notModified = true

		return 0
	}

	l.addThreads(got.Threads)

	if got.NextPage == 0 {
		// The last page there is, or the last the app reads, which leaves threads out
		// when GitHub lists another.
		l.walkedAll, l.state.Truncated = true, got.Capped

		return 0
	}

	if oldest, ok := oldestUpdate(got.Threads); ok && !mark.IsZero() && !oldest.After(mark) {
		l.floor = oldest

		return 0
	}

	return got.NextPage
}

// addThreads adds threads, each once: a thread updated while the pages are read moves up,
// and one may show twice; the first, newer, read counts.
func (l *listing) addThreads(threads []github.Thread) {
	for _, t := range threads {
		if !l.seen[t.ID] {
			l.seen[t.ID] = true
			l.threads = append(l.threads, t)
		}
	}
}

// advance is st once read, a listing of login's notifications answered at, is stored: the
// listing's ETag and poll interval, a high-water mark at the newest thread read, and, for a
// walk of every page, its time and whether it stopped at the last page the app reads.
func advance(st syncState, read *listing, login string, at time.Time) syncState {
	next := st
	next.Account = login
	next.ETag = read.state.ETag
	next.LastPass = at
	next.Access, next.AskAgainAt, next.LastError = AccessOK, time.Time{}, ""

	if read.state.PollSeconds > 0 {
		next.PollSeconds = read.state.PollSeconds
	}

	if read.notModified {
		return next
	}

	newest := newestUpdate(read.threads)

	if !read.walkedAll {
		next.HighWater = later(st.HighWater, newest)

		return next
	}

	next.HighWater = newest
	next.LastFullWalk, next.Truncated, next.WalkNext = at, read.state.Truncated, false

	return next
}

// keep stores what a listing read, in one transaction: every thread read, seen at; the
// deletion of the account's threads it didn't list within the span it covered (dropUnseen);
// and the state of the passes.
func (s *Service) keep(ctx context.Context, l *listing, at time.Time) error {
	account := l.state.Account

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		ids, err := upsertThreads(ctx, q, account, l.threads, at)
		if err != nil {
			return err
		}

		if err = dropUnseen(ctx, q, l, ids, at); err != nil {
			return err
		}

		return saveState(ctx, s.prefs.WithTx(tx), l.state)
	})
	if err != nil {
		return fmt.Errorf("store the GitHub notifications: %w", err)
	}

	if l.walkedAll {
		s.walked = true
	}

	s.keepPoll(l.state)

	return nil
}

// upsertThreads stores threads as account's, seen at, and returns their IDs.
func upsertThreads(ctx context.Context, q *queries.Queries, account string, threads []github.Thread, at time.Time,
) ([]int64, error) {
	ids := make([]int64, 0, len(threads))

	for i := range threads {
		if err := q.UpsertThread(ctx, threadRow(account, &threads[i], at)); err != nil {
			return nil, fmt.Errorf("store a GitHub notification: %w", err)
		}

		ids = append(ids, threads[i].ID)
	}

	return ids, nil
}

// dropUnseen drops the account's threads that l, a listing GitHub answered with every page
// it meant to read, didn't list (ids are those it did) within the span it covered, which
// GitHub's newest-updated-first order bounds: every thread, for a listing that read to the
// last page (deleteUnseen, which also drops other accounts' threads unseen for
// OtherAccountsKept), and those updated strictly after its floor, for one that stopped at
// the high-water mark, since nothing newer can be on a later page. Such a thread was
// marked done on GitHub. A 304 covers nothing, and drops nothing.
//
// It shares a race with the walk: a thread updated while the pages are read can move above
// a page already read, and be dropped once; the next pass lists it again, since page 1
// changed.
func dropUnseen(ctx context.Context, q *queries.Queries, l *listing, ids []int64, at time.Time) error {
	account := l.state.Account

	switch {
	case l.walkedAll:
		return deleteUnseen(ctx, q, account, ids, at)
	case l.notModified || l.floor.IsZero() || len(ids) == 0:
		return nil
	}

	_, err := q.DeleteUnseenThreadsSince(ctx, queries.DeleteUnseenThreadsSinceParams{
		AccountLogin: account, After: l.floor.UTC(), Seen: ids,
	})
	if err != nil {
		return fmt.Errorf("drop the GitHub notifications that are done: %w", err)
	}

	return nil
}

// deleteUnseen drops account's threads a walk of every page didn't list (ids are those
// it did), and the threads of other accounts no pass listed for OtherAccountsKept.
func deleteUnseen(ctx context.Context, q *queries.Queries, account string, ids []int64, at time.Time) error {
	var err error

	if len(ids) == 0 {
		_, err = q.DeleteAccountThreads(ctx, account)
	} else {
		_, err = q.DeleteUnseenThreads(ctx, queries.DeleteUnseenThreadsParams{AccountLogin: account, Seen: ids})
	}

	if err != nil {
		return fmt.Errorf("drop the GitHub notifications that are done: %w", err)
	}

	_, err = q.PruneOtherAccounts(ctx, queries.PruneOtherAccountsParams{
		AccountLogin: account, Before: at.Add(-OtherAccountsKept),
	})
	if err != nil {
		return fmt.Errorf("drop other accounts' GitHub notifications: %w", err)
	}

	return nil
}

// threadRow is t as account's row, seen at.
func threadRow(account string, t *github.Thread, at time.Time) queries.UpsertThreadParams {
	row := queries.UpsertThreadParams{
		AccountLogin: account, ThreadID: t.ID, Reason: t.Reason, Unread: t.Unread, UpdatedAt: t.UpdatedAt.UTC(),
		SubjectType: t.SubjectType, SubjectTitle: t.Title, RepoOwner: t.Repo.Owner, RepoName: t.Repo.Repo,
		LatestCommentURL: t.LatestCommentURL, SeenAt: at.UTC(),
	}

	if !t.LastReadAt.IsZero() {
		read := t.LastReadAt.UTC()
		row.LastReadAt = &read
	}

	// Only a pull request or an issue names an item; any other subject keeps '' and 0.
	if t.Ref.Kind.Valid() && t.Ref.Number > 0 && t.Ref.Repo.Valid() {
		row.ItemKind, row.Number = string(t.Ref.Kind), int64(t.Ref.Number)
		row.RepoOwner, row.RepoName = t.Ref.Repo.Owner, t.Ref.Repo.Repo
	}

	return row
}

// oldestUpdate is the earliest update among threads; ok is false for none.
func oldestUpdate(threads []github.Thread) (time.Time, bool) {
	if len(threads) == 0 {
		return time.Time{}, false
	}

	oldest := threads[0].UpdatedAt
	for _, t := range threads[1:] {
		if t.UpdatedAt.Before(oldest) {
			oldest = t.UpdatedAt
		}
	}

	return oldest, true
}

// newestUpdate is the latest update among threads, zero for none.
func newestUpdate(threads []github.Thread) time.Time {
	var newest time.Time

	for _, t := range threads {
		newest = later(newest, t.UpdatedAt)
	}

	return newest
}

// later returns the later of a and b.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
