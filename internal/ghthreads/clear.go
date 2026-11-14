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
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// Clearing. Once the user turns clearing on (threadrules' Auto), a pass that follows its
// listing decides, for every thread about a pull request or an issue, whether the work
// behind it is finished, against the moments that could say so: the user's own in the app,
// which the inbox tells the Settler of (a merge, a close, a review that decides, a
// dismissal, taking themselves off), and GitHub's own state (a merge or a close anywhere
// else), which counts only when read within StateFreshFor. threadrules.Decide guards each
// one: a thread updated after the moment and unread is held, unless every activity since
// was a bot's, which a check of its latest comment tells, once per update of the thread.
//
// A thread that clears is queued in the gh_thread_clears ledger before anything is sent,
// with the rule that chose it and what clearing does (done or read). A clear the user's
// own action chose waits the Undo hold (Hold, which follows the toast setting), so Keep
// can still undo it; one GitHub's state chose goes at once. The first pass after clearing
// turns on, or after the rules change, queues at most FirstCap by itself, and any later
// pass at most Cap: a pass over its cap queues none, and records how many it held back
// (the Backlog), for the user to review in the cleanup sheet (Preview, then Clear).
//
// The queue is sent one clear at a time through the GitHub client's chore lane, each
// checked again just before it goes (sendOne): clearing still on, reading still on,
// GitHub not refusing the listing, the token still the account's, the thread still listed
// at the update it was decided on, and the rules still clearing it. A clear queued more
// than QueuedFor ago is decided again instead. GitHub answering that it has no such thread
// counts as done; any other failure is tried again, MaxAttempts times at most, and a clear
// that failed, like one the user kept, isn't queued again on its own until its thread
// moves.

const (
	// StateFreshFor is how recently GitHub's state of an item must have been read for a
	// merge or a close it says to clear a thread: never a guess, and an item reopened
	// stops counting once its state is read again.
	StateFreshFor = time.Hour

	// MinHold is the least time a clear chosen by the user's own action in the app waits
	// before it is sent, and HoldPad the time it waits past the toast that offers its
	// Undo (Hold).
	MinHold = 15 * time.Second
	HoldPad = 3 * time.Second

	// FirstCap is the most clears the first pass after clearing turns on, or after the
	// rules change, queues by itself, and Cap the most any later pass does: more wait for
	// the user to review them.
	FirstCap = 25
	Cap      = 150

	// QueuedFor is how long a clear stays queued before it is decided again instead of
	// sent.
	QueuedFor = 24 * time.Hour

	// MaxAttempts is how many failed sends make a clear failed, and FailedRetry how long
	// after a failed send the next is tried.
	MaxAttempts = 3
	FailedRetry = time.Minute

	// MinNudgeListing is the least time since the last listing after which a nudged run
	// lists first, when GitHub's poll interval asks for less.
	MinNudgeListing = time.Minute

	// MomentsKept is how long a moment is kept after the app heard of it, and ClearsKept
	// how long a clear is kept after it settled.
	MomentsKept = 30 * 24 * time.Hour
	ClearsKept  = 30 * 24 * time.Hour

	// MaxRecent is how many settled clears Snapshot lists.
	MaxRecent = 50

	// MaxCommentChecks is the most latest-comment reads one pass sends to learn who acted
	// on the threads held back; the rest wait for the next pass.
	MaxCommentChecks = 150

	// maxSendsPerRun bounds the clears one run sends, should the chore lane never refuse.
	maxSendsPerRun = 200
)

// ClearState is where a clear stands in the ledger.
type ClearState string

// The states of a clear.
const (
	// ClearQueued is a clear waiting to be sent.
	ClearQueued ClearState = "queued"

	// ClearSent is a clear GitHub took, or one GitHub no longer had the thread of.
	ClearSent ClearState = "sent"

	// ClearKept is a clear the user undid (Keep), or one a restore after a dismissal took
	// back.
	ClearKept ClearState = "kept"

	// ClearFailed is a clear whose sends failed MaxAttempts times.
	ClearFailed ClearState = "failed"

	// ClearSkipped is a clear not sent, for its SkipReason.
	ClearSkipped ClearState = "skipped"
)

// SkipReason is why a clear was skipped, or a thread held back from clearing.
type SkipReason string

// The reasons a clear is skipped. Those threadrules.Decide gives keep its words.
const (
	// SkipNone is no reason: the clear wasn't skipped.
	SkipNone SkipReason = ""

	// SkipOff is reading the notifications, or clearing on its own, turned off.
	SkipOff = SkipReason(threadrules.ReasonOff)

	// SkipRuleOff is the trigger turned off, or a watched repository's thread left alone.
	SkipRuleOff = SkipReason(threadrules.ReasonRuleOff)

	// SkipStillOpen is an item GitHub no longer says is finished: reopened.
	SkipStillOpen = SkipReason(threadrules.ReasonStillOpen)

	// SkipNewActivity is a thread that changed after the moment, which the user may not
	// have seen.
	SkipNewActivity = SkipReason(threadrules.ReasonNewActivity)

	// SkipAlreadyRead is a thread already read while clearing marks one read.
	SkipAlreadyRead = SkipReason(threadrules.ReasonAlreadyRead)

	// SkipNotItem is a thread about neither a pull request nor an issue.
	SkipNotItem = SkipReason(threadrules.ReasonNotItem)

	// SkipNotListed is a thread the mirror no longer lists: done elsewhere, or dropped for
	// one pass by a listing that raced its update. A later pass queues it again if it
	// still clears.
	SkipNotListed SkipReason = "not_listed"

	// SkipChanged is a thread updated since the clear was decided: it was decided again.
	SkipChanged SkipReason = "changed"

	// SkipStale is a clear queued more than QueuedFor ago: it was decided again.
	SkipStale SkipReason = "stale"

	// SkipKept is a thread the user kept (Undo) at its update: it waits until it moves.
	SkipKept SkipReason = "kept"

	// SkipFailed is a thread whose clear failed MaxAttempts times at its update: it waits
	// until it moves, or the user clears it by hand.
	SkipFailed SkipReason = "failed"
)

// clearRun is what one run's clearing steps share: the rules as the run read them, the
// account whose threads it decides, its scope (nil for every thread), its time, the Undo
// hold, and whether a backlog counted under these rules is held for review.
type clearRun struct {
	rules       threadrules.Rules
	account     string
	scope       map[string]bool
	now         time.Time
	hold        time.Duration
	backlogHeld bool
}

// decide decides r against its item's moments, taking GitHub's word on a state of any age
// when anyAge.
func (c *clearRun) decide(in *decideInputs, r *decideRow, anyAge bool) (decision, bool) {
	return decideThread(c.rules, r, in.momentsOf(r), c.now, c.hold, anyAge)
}

// follow runs what follows a run's listing, for st's account, from one read of its
// threads, moments, and clears: it checks the state of the items asked and due (and of
// those GitHub's word, read too long ago, would clear), checks who acted after the moment
// on the threads held back, decides the threads in the run's scope and queues those that
// clear, and sends the queued clears that are due. A failure to check states is recorded as
// the status's last error, and the rest still runs.
func (s *Service) follow(
	ctx context.Context, req passRequest, rules threadrules.Rules, st syncState, listed bool, now time.Time,
) (*sendOutcome, error) {
	run := &clearRun{
		rules: rules, account: st.Account, scope: req.scope(), now: now, hold: s.hold(ctx),
		backlogHeld: st.backlogHeld(rules),
	}

	in, err := loadInputs(ctx, s.q, run.account)
	if err != nil {
		return nil, err
	}

	errs := s.checkFirst(ctx, run, &in, req, st, listed)
	if ctx.Err() != nil {
		return nil, errors.Join(errs...)
	}

	errs = append(errs, s.queue(ctx, run, &in, req.decidesAll()))

	if req.decidesAll() {
		errs = append(errs, s.prune(ctx, st.Account, now))
	}

	out, err := s.sendDue(ctx, rules, st)

	return &out, errors.Join(append(errs, err)...)
}

// checkFirst checks what deciding the run's threads needs read first: the states asked,
// due, and too old to count (staleRefs), reading the threads again only when a state was
// stored, then who acted after the moment on the threads held back. A failure to read who
// acted is logged, and deciding goes on without it.
func (s *Service) checkFirst(
	ctx context.Context, run *clearRun, in *decideInputs, req passRequest, st syncState, listed bool,
) []error {
	asked := append(slices.Clone(req.check), run.staleRefs(in)...)
	stored, err := s.checkStates(ctx, st, asked, req.nudged && !listed, run.now)
	errs := []error{err}

	if stored {
		errs = append(errs, in.reloadThreads(ctx, s.q, run.account))
	}

	if err = s.checkAfterMoments(ctx, run, in); err != nil {
		if ctx.Err() != nil {
			return append(errs, err)
		}

		s.logger.WarnContext(ctx, "checking who acted on GitHub notifications failed", slog.Any("error", err))
	}

	return errs
}

// skipWhileOff skips every queued clear the user didn't ask for while clearing on its own
// is off: clearing stops at once.
func (s *Service) skipWhileOff(ctx context.Context, rules threadrules.Rules, now time.Time) error {
	if rules.Auto {
		return nil
	}

	return s.skipQueued(ctx, s.q, false, now)
}

// skipQueued skips the queued clears as SkipOff: every one when all (reading turned off),
// else those the user didn't ask for (clearing on its own turned off).
func (s *Service) skipQueued(ctx context.Context, q *queries.Queries, all bool, now time.Time) error {
	at := now.UTC()
	params := queries.SkipQueuedClearsParams{SettledAt: &at, SkipReason: string(SkipOff)}

	var (
		skipped int64
		err     error
	)

	if all {
		skipped, err = q.SkipQueuedClears(ctx, params)
	} else {
		skipped, err = q.SkipQueuedAutoClears(ctx, queries.SkipQueuedAutoClearsParams(params))
	}

	if err != nil {
		return fmt.Errorf("skip the queued clears of GitHub notifications: %w", err)
	}

	if skipped > 0 {
		s.logger.InfoContext(ctx, "clearing GitHub notifications turned off; skipped the queued clears",
			slog.Int64("skipped", skipped))
	}

	return nil
}

// decideInputs are what deciding the account's threads reads: the threads, the moments by
// item, and the ledger's marks. A run reads them once, and again only what a step changed.
type decideInputs struct {
	threads []decideRow
	moments map[string][]moment
	marks   marks
}

// loadInputs reads what deciding account's threads reads, through q.
func loadInputs(ctx context.Context, q *queries.Queries, account string) (decideInputs, error) {
	threads, err := q.ListDecideThreads(ctx, account)
	if err != nil {
		return decideInputs{}, fmt.Errorf("read the GitHub notifications to clear: %w", err)
	}

	moments, err := q.ListMoments(ctx, account)
	if err != nil {
		return decideInputs{}, fmt.Errorf("read the moments that clear GitHub notifications: %w", err)
	}

	m, err := loadMarks(ctx, q, account)
	if err != nil {
		return decideInputs{}, err
	}

	return decideInputs{threads: threads, moments: momentsByItem(moments), marks: m}, nil
}

// loadMarks reads the marks of account's clears through q.
func loadMarks(ctx context.Context, q *queries.Queries, account string) (marks, error) {
	clears, err := q.ListClearMarks(ctx, account)
	if err != nil {
		return marks{}, fmt.Errorf("read the cleared GitHub notifications: %w", err)
	}

	return marksOf(clears), nil
}

// reloadThreads reads account's threads again through q, once a state check stored what
// GitHub said of their items.
func (in *decideInputs) reloadThreads(ctx context.Context, q *queries.Queries, account string) error {
	threads, err := q.ListDecideThreads(ctx, account)
	if err != nil {
		return fmt.Errorf("read the GitHub notifications to clear: %w", err)
	}

	in.threads = threads

	return nil
}

// momentsOf returns the moments of r's item.
func (in *decideInputs) momentsOf(r *decideRow) []moment {
	return in.moments[rowItem(r).Key()]
}

// eligible returns the threads in scope a clear could be queued for on its own (marks'
// eligible).
func (in *decideInputs) eligible(scope map[string]bool) []*decideRow {
	out := make([]*decideRow, 0, len(in.threads))

	for i := range in.threads {
		if r := &in.threads[i]; inScope(scope, r) && in.marks.eligible(r) {
			out = append(out, r)
		}
	}

	return out
}

// staleRefs returns the items, each once, whose state should be read again before the
// run's threads are decided: a thread would clear on GitHub's word that it was merged or
// closed, but that word was read more than StateFreshFor ago. Nothing is asked while
// clearing on its own is off, nor while a backlog counted under the run's rules is held
// for review: its clears aren't queued until the user reviews them, and reading their
// states again every hour would change nothing (queue counts them all the same).
func (c *clearRun) staleRefs(in *decideInputs) []item.Ref {
	if !c.rules.Auto || c.backlogHeld {
		return nil
	}

	seen := map[string]bool{}

	var out []item.Ref

	for _, r := range in.eligible(c.scope) {
		if ref := rowItem(r); !seen[ref.Key()] && c.clearsOnStaleWord(in, r) {
			seen[ref.Key()] = true
			out = append(out, ref)
		}
	}

	return out
}

// clearsOnStaleWord reports whether r would clear only on GitHub's word read too long ago.
func (c *clearRun) clearsOnStaleWord(in *decideInputs, r *decideRow) bool {
	if fresh, _ := c.decide(in, r, false); fresh.verdict.Kind == threadrules.VerdictClear {
		return false
	}

	old, ok := c.decide(in, r, true)

	return ok && old.verdict.Kind == threadrules.VerdictClear
}

// checkAfterMoments checks, for each of the run's threads that a moment holds back for its
// new activity, who acted on it after the moment, once per update of the thread, and stores
// it, in in too. It asks only while clearing on its own is on and a bot's activity counts
// as seen, when the answer could change the verdict; a person's activity stays until the
// thread is read, so a thread that holds one is never asked about again until then.
//
// It reads the thread's latest comment only (one REST request; none when the latest
// comment's URL is the subject's own, whoActed): a person's comment followed by a bot's
// before the next pass reads the thread is taken as a bot's. A person's seen once stays. A
// pass asks MaxCommentChecks at most, the most recently updated first, and stops at the
// first answer that says GitHub is limiting the app's requests, or when the run ends; the
// rest are asked by the next pass, since each is asked once per update.
func (s *Service) checkAfterMoments(ctx context.Context, run *clearRun, in *decideInputs) error {
	if !run.rules.Auto || !run.rules.BotActivity {
		return nil
	}

	checks := commentChecks{left: MaxCommentChecks}

	for _, r := range in.eligible(run.scope) {
		if !run.worthAsking(in, r) {
			continue
		}

		if asked, err := s.checkAfterMoment(ctx, run, r); checks.done(ctx, asked, err) {
			break
		}
	}

	if checks.left <= 0 {
		s.logger.DebugContext(ctx, "checked who acted on as many GitHub notifications as a pass does; the rest wait",
			slog.Int("checked", MaxCommentChecks))
	}

	return errors.Join(checks.errs...)
}

// commentChecks counts down the latest-comment reads a pass may still send, and gathers
// their failures.
type commentChecks struct {
	left int
	errs []error
}

// done records one check, which sent a request when asked, and went as err says; it reports
// whether the pass stops asking: it sent as many as it may, GitHub is limiting the app's
// requests (or the app's pace refused one), or the run ended.
func (c *commentChecks) done(ctx context.Context, asked bool, err error) bool {
	if asked {
		c.left--
	}

	if err != nil {
		c.errs = append(c.errs, err)

		if ctx.Err() != nil || github.RateLimited(err) || choreLaneRefused(err) {
			return true
		}
	}

	return c.left <= 0
}

// worthAsking reports whether who acted on r after its moment could change its verdict:
// a moment holds it back for its new activity, and it changed since the last check.
func (c *clearRun) worthAsking(in *decideInputs, r *decideRow) bool {
	d, ok := c.decide(in, r, true)

	return ok && d.verdict.Kind == threadrules.VerdictHold && afterMomentDue(r)
}

// checkAfterMoment reads who acted on r last, and stores it, on r too, checked as of the
// run's time (or r's update, should GitHub's clock be ahead). asked reports whether it sent
// a request.
func (s *Service) checkAfterMoment(ctx context.Context, run *clearRun, r *decideRow) (asked bool, err error) {
	who, asked, err := s.whoActed(ctx, r)
	if err != nil {
		return asked, err
	}

	checked := later(run.now, r.UpdatedAt).UTC()

	err = s.q.SetAfterMoment(ctx, queries.SetAfterMomentParams{
		AfterMoment: string(who), CheckedAt: &checked, AccountLogin: run.account, ThreadID: r.ThreadID,
	})
	if err != nil {
		return asked, fmt.Errorf("store who acted on a GitHub notification: %w", err)
	}

	r.AfterMoment, r.AfterMomentCheckedAt = string(who), &checked

	s.logger.DebugContext(ctx, "checked who acted on a GitHub notification", slog.Int64("thread", r.ThreadID),
		slog.String("title", r.SubjectTitle), slog.String("who", string(who)))

	return asked, nil
}

// afterMomentDue reports whether who acted on r after its moment is worth checking: it is
// unread, a person's activity isn't already known, and it updated since the last check.
func afterMomentDue(r *decideRow) bool {
	if !r.Unread || threadrules.AfterMoment(r.AfterMoment) == threadrules.AfterPerson {
		return false
	}

	return r.AfterMomentCheckedAt == nil || r.UpdatedAt.After(*r.AfterMomentCheckedAt)
}

// whoActed reads who acted on r last: unknown for no latest comment, or a URL that is no
// comment's; for the subject's own URL, as subjectActivity says, with no request; and
// otherwise the latest comment's author (one REST request, asked): a bot when GitHub's type
// for it is Bot, unknown when GitHub names none, and a person otherwise.
func (s *Service) whoActed(ctx context.Context, r *decideRow) (who threadrules.AfterMoment, asked bool, err error) {
	url := r.LatestCommentURL
	if url == "" {
		return threadrules.AfterUnknown, false, nil
	}

	if ref, ok := s.client.ParseSubject(url); ok {
		return subjectActivity(r, ref), false, nil
	}

	author, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (*github.CommentAuthor, error) {
		return s.client.FetchLatestComment(ctx, tok, url)
	})

	switch {
	case errors.Is(err, github.ErrInvalidCommentURL):
		return threadrules.AfterUnknown, false, nil
	case err != nil:
		return "", true, fmt.Errorf("read who wrote a GitHub notification's latest comment: %w", err)
	case author.Type == "Bot":
		return threadrules.AfterBot, true, nil
	case author.Type == "":
		return threadrules.AfterUnknown, true, nil
	default:
		return threadrules.AfterPerson, true, nil
	}
}

// subjectActivity is who acted on r last when GitHub gives the subject's own URL, ref, as
// its latest comment's: no comment, but an event. On an item merged or closed, that is the
// merge or the close itself, which counts as a bot's; on an open one it may be anyone's (a
// push, a request for review, an approval with no text, a reopening), so it counts as a
// person's, which holds the thread until it is read. Another item's URL tells nothing.
func subjectActivity(r *decideRow, ref item.Ref) threadrules.AfterMoment {
	if ref.Key() != rowItem(r).Key() {
		return threadrules.AfterUnknown
	}

	switch threadrules.State(r.SubjectState) {
	case threadrules.StateMerged, threadrules.StateClosed:
		return threadrules.AfterBot
	case threadrules.StateOpen, threadrules.StateUnknown:
		return threadrules.AfterPerson
	default:
		return threadrules.AfterPerson
	}
}

// queue decides the run's threads and queues the clears of those that clear, in one
// transaction, unless they are more than the run's cap: FirstCap for the first run under
// the rules (whose fingerprint differs from the one last stored), else Cap. Those that would
// clear but for a state read too long ago count toward it too. A run over its cap queues
// none. A run that decides every thread (all) records how many it held back as the
// Backlog, with the rules it counted them under, and, under its cap, the rules as seen.
// While clearing on its own is off, it queues nothing and forgets the rules seen. The
// ledger's marks are read again in the transaction, since the user's clicks may have
// queued or kept a clear since the run read them.
func (s *Service) queue(ctx context.Context, run *clearRun, in *decideInputs, all bool) error {
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		return s.queueIn(ctx, s.q.WithTx(tx), s.prefs.WithTx(tx), run, in, all)
	})
	if err != nil {
		return fmt.Errorf("decide which GitHub notifications clear: %w", err)
	}

	return nil
}

// queueIn is queue's work, through q and p, a transaction's queries and preferences.
func (s *Service) queueIn(
	ctx context.Context, q *queries.Queries, p *prefs.Store, run *clearRun, in *decideInputs, all bool,
) error {
	st, err := s.loadStateFrom(ctx, p)
	if err != nil {
		return err
	}

	if !run.rules.Auto {
		return recordRun(ctx, p, st, all, held{})
	}

	if in.marks, err = loadMarks(ctx, q, run.account); err != nil {
		return err
	}

	rows, stale := run.clears(in)
	seen := fingerprint(run.rules)

	limit := Cap
	if st.RulesSeen != seen {
		limit = FirstCap
	}

	if total := len(rows) + stale; total > limit {
		s.logger.InfoContext(ctx, "more GitHub notifications would clear than clear on their own; they wait for review",
			slog.Int("count", total), slog.Int("cap", limit))

		return recordRun(ctx, p, st, all, held{seen: st.RulesSeen, backlog: total, backlogRules: seen})
	}

	if err = s.insertClears(ctx, q, rows); err != nil {
		return err
	}

	return recordRun(ctx, p, st, all, held{seen: seen})
}

// clears decides the run's threads that could be queued, and returns the clears of those
// that clear, and how many more would but for a state read too long ago.
func (c *clearRun) clears(in *decideInputs) (rows []queries.InsertClearParams, stale int) {
	action := threadrules.DecodeAction(string(c.rules.Action))

	for _, r := range in.eligible(c.scope) {
		d, ok := c.decide(in, r, false)

		switch {
		case ok && d.verdict.Kind == threadrules.VerdictClear:
			rows = append(rows, clearParams(c.account, r, d.verdict.Trigger, action, d.sendAfter, c.now))
		case c.clearsOnStaleWord(in, r):
			stale++
		}
	}

	return rows, stale
}

// insertClears queues rows through q.
func (s *Service) insertClears(ctx context.Context, q *queries.Queries, rows []queries.InsertClearParams) error {
	for i := range rows {
		if _, err := q.InsertClear(ctx, rows[i]); err != nil {
			return fmt.Errorf("queue a GitHub notification's clear: %w", err)
		}

		s.logger.DebugContext(ctx, "queued a GitHub notification's clear", slog.Int64("thread", rows[i].ThreadID),
			slog.String("title", rows[i].Title), slog.String("trigger", rows[i].Trigger))
	}

	return nil
}

// held is what a run that decided every thread leaves of its caps: the fingerprint of the
// rules seen, and the clears it held back for review, with the rules it counted them under.
type held struct {
	seen         string
	backlog      int
	backlogRules string
}

// recordRun stores, for a run that decided every thread (all), what it held (h), through p,
// when that changed; a run that decided some threads stores nothing.
func recordRun(ctx context.Context, p *prefs.Store, st syncState, all bool, h held) error {
	if !all || (st.RulesSeen == h.seen && st.Backlog == h.backlog && st.BacklogRules == h.backlogRules) {
		return nil
	}

	st.RulesSeen, st.Backlog, st.BacklogRules = h.seen, h.backlog, h.backlogRules

	return saveState(ctx, p, st)
}

// clearParams is a queued clear of r for account, by trigger, doing action, decided at now
// and sent once sendAfter has passed.
func clearParams(
	account string, r *decideRow, trigger threadrules.Trigger, action threadrules.Action, sendAfter, now time.Time,
) queries.InsertClearParams {
	return queries.InsertClearParams{
		AccountLogin: account, ThreadID: r.ThreadID, RepoOwner: r.RepoOwner, RepoName: r.RepoName,
		ItemKind: r.ItemKind, Number: r.Number, Title: r.SubjectTitle, Reason: r.Reason, Trigger: string(trigger),
		Action: string(action), ThreadUpdatedAt: r.UpdatedAt.UTC(), QueuedAt: now.UTC(), SendAfter: sendAfter.UTC(),
	}
}

// prune drops the moments heard of MomentsKept ago and the clears settled ClearsKept ago,
// and skips another account's clears queued QueuedFor ago, which no token of theirs came
// back to send.
func (s *Service) prune(ctx context.Context, account string, now time.Time) error {
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if _, err := q.PruneMoments(ctx, now.Add(-MomentsKept).UTC()); err != nil {
			return err
		}

		settled := now.Add(-ClearsKept).UTC()
		if _, err := q.PruneClears(ctx, &settled); err != nil {
			return err
		}

		at := now.UTC()

		_, err := q.SkipOtherAccountsQueued(ctx, queries.SkipOtherAccountsQueuedParams{
			SettledAt: &at, SkipReason: string(SkipStale), AccountLogin: account, Before: now.Add(-QueuedFor).UTC(),
		})

		return err
	})
	if err != nil {
		return fmt.Errorf("prune the cleared GitHub notifications: %w", err)
	}

	return nil
}
