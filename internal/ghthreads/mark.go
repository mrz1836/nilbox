package ghthreads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// MaxPaceWait is the longest a mark the user clicked waits, in all, for the chore lane's
	// pace before it gives up and says so; a wait the lane asks for past what is left fails
	// at once.
	MaxPaceWait = 5 * time.Second

	// minPaceWait is the shortest wait for the lane's pace, when it says none.
	minPaceWait = 50 * time.Millisecond
)

// errMarkOtherAccount is a mark refused because the GitHub token now belongs to another
// account than the one whose notifications the window shows.
var errMarkOtherAccount = uierr.New(
	"Your GitHub token is for another account now. Check again to read its notifications.", errOtherAccount,
)

// MarkDone marks the threads with ids done on GitHub at the user's click, the panel's Done,
// which takes them out of the user's inbox there: any thread the mirror lists for the
// account the last pass read, whatever its subject and whatever the rules say clearing
// does, and, for one about a pull request or an issue, every thread the mirror lists about
// that item, since the panel's line stands for the item. Each one GitHub took, or no longer
// had, leaves the mirror, and a clear queued for it is settled. With reading off, or before
// a pass read an account, it sends nothing. It returns the Snapshot.
func (s *Service) MarkDone(ctx context.Context, ids []int64) (Snapshot, error) {
	return s.mark(ctx, ids, threadrules.ActionDone)
}

// MarkRead marks the threads with ids read on GitHub as the user opens them from the panel,
// as GitHub's own inbox does, and in the mirror: any unread thread it lists for the account
// the last pass read, whatever its subject, and, for one about a pull request or an issue,
// every unread thread it lists about that item. With reading off, or before a pass read an
// account, it sends nothing. It returns the Snapshot.
func (s *Service) MarkRead(ctx context.Context, ids []int64) (Snapshot, error) {
	return s.mark(ctx, ids, threadrules.ActionRead)
}

// mark does action to the account's threads among ids, then returns the Snapshot. A mark
// that fails stops the rest, and its error says why in words; the marks before it stand.
func (s *Service) mark(ctx context.Context, ids []int64, action threadrules.Action) (Snapshot, error) {
	if len(ids) > 0 {
		if err := s.markAndRecord(ctx, ids, action); err != nil {
			return Snapshot{}, err
		}
	}

	return s.Snapshot(ctx)
}

// marked is a thread GitHub took a mark for, or no longer had (gone).
type marked struct {
	id   int64
	gone bool
}

// markAndRecord sends the marks to GitHub at once, a pass running or not, then records
// what GitHub took in the mirror holding the pass slot, so a pass that listed the threads
// before GitHub took the marks stores its listing first and the marks land on top.
func (s *Service) markAndRecord(ctx context.Context, ids []int64, action threadrules.Action) error {
	account, targets, err := s.markPlan(ctx, ids, action)
	if err != nil || len(targets) == 0 {
		return err
	}

	took, sendErr := s.sendMarks(ctx, account, targets, action)
	if len(took) == 0 {
		return sendErr
	}

	if err = s.recordMarks(ctx, account, took, action); err != nil {
		return errors.Join(sendErr, err)
	}

	return sendErr
}

// markPlan returns the account the last pass read and the threads to mark for it among
// ids (markTargets); none while reading is off, or before a pass read an account.
func (s *Service) markPlan(ctx context.Context, ids []int64, action threadrules.Action) (string, []int64, error) {
	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return "", nil, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	st, err := s.loadState(ctx)
	if err != nil || !rules.Read || st.Account == "" {
		return "", nil, err
	}

	targets, err := s.markTargets(ctx, st.Account, ids, action)

	return st.Account, targets, err
}

// markTargets returns, each once, in order, the threads the mirror lists for account among
// ids, each one about a pull request or an issue standing for every thread the mirror
// lists about that item (standsFor); to mark them read, only the unread ones.
func (s *Service) markTargets(
	ctx context.Context, account string, ids []int64, action threadrules.Action,
) ([]int64, error) {
	byItem, err := s.threadsByItem(ctx, account)
	if err != nil {
		return nil, err
	}

	var targets []int64

	for _, id := range slices.Compact(slices.Sorted(slices.Values(ids))) {
		got, err := s.q.GetDecideThread(ctx, queries.GetDecideThreadParams{AccountLogin: account, ThreadID: id})
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("read a GitHub notification to mark: %w", err)
		}

		row := decideRow(got)
		targets = append(targets, standsFor(&row, byItem, action)...)
	}

	return slices.Compact(slices.Sorted(slices.Values(targets))), nil
}

// threadsByItem returns account's threads about a pull request or an issue, by the item's
// key.
func (s *Service) threadsByItem(ctx context.Context, account string) (map[string][]decideRow, error) {
	rows, err := s.q.ListDecideThreads(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("read the GitHub notifications to mark: %w", err)
	}

	byItem := map[string][]decideRow{}

	for i := range rows {
		key := rowItem(&rows[i]).Key()
		byItem[key] = append(byItem[key], rows[i])
	}

	return byItem, nil
}

// standsFor returns the threads row stands for: every thread about its item (byItem), or
// row alone, about no item; to mark them read, only the unread ones.
func standsFor(row *decideRow, byItem map[string][]decideRow, action threadrules.Action) []int64 {
	same, ok := byItem[rowItem(row).Key()]
	if !ok {
		same = []decideRow{*row}
	}

	var out []int64

	for i := range same {
		if action != threadrules.ActionRead || same[i].Unread {
			out = append(out, same[i].ThreadID)
		}
	}

	return out
}

// sendMarks marks targets on GitHub with a token it checks is account's, and returns the
// threads GitHub took, in order, with the error that stopped the rest.
func (s *Service) sendMarks(
	ctx context.Context, account string, targets []int64, action threadrules.Action,
) ([]marked, error) {
	var took []marked

	_, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (struct{}, error) {
		login, err := s.viewer(ctx, tok)
		if err != nil {
			return struct{}{}, err
		}

		if !strings.EqualFold(login, account) {
			return struct{}{}, errMarkOtherAccount
		}

		// A try with a token borrowed again goes on where the last one stopped.
		for _, id := range targets[len(took):] {
			gone, err := s.markOne(ctx, tok, id, action)
			if err != nil {
				return struct{}{}, err
			}

			took = append(took, marked{id: id, gone: gone})
		}

		return struct{}{}, nil
	})

	return took, err
}

// markOne sends action for the thread id through the chore lane, waiting out its pace for
// up to MaxPaceWait in all, and reports whether GitHub no longer had the thread (gone).
func (s *Service) markOne(ctx context.Context, tok github.Token, id int64, action threadrules.Action) (bool, error) {
	waited := time.Duration(0)

	for {
		err := s.sendMark(ctx, tok, id, action)

		switch {
		case err == nil:
			return false, nil
		case errors.Is(err, github.ErrThreadNotFound):
			return true, nil
		case errors.Is(err, budget.ErrWritePace):
			wait := max(budget.RetryIn(err), minPaceWait)
			if waited+wait > MaxPaceWait {
				return false, markFailure(err, action)
			}

			waited += wait

			if sleepErr := s.paceSleep(ctx, wait); sleepErr != nil {
				return false, fmt.Errorf("wait for the pace of changes to GitHub: %w", sleepErr)
			}
		default:
			return false, markFailure(err, action)
		}
	}
}

// sendMark marks the thread id done or read on GitHub, through the chore lane: the user's
// mark and a queued clear alike.
func (s *Service) sendMark(ctx context.Context, tok github.Token, id int64, action threadrules.Action) error {
	if action == threadrules.ActionRead {
		return s.client.MarkThreadRead(ctx, tok, id)
	}

	return s.client.MarkThreadDone(ctx, tok, id)
}

// recordMarks records the threads GitHub took in the mirror, holding the pass slot, in one
// transaction (markInMirror; a thread marked done also settles a clear queued for it, as
// no longer listed), then tells the window what changed.
func (s *Service) recordMarks(ctx context.Context, account string, took []marked, action threadrules.Action) error {
	select {
	case s.running <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("wait for the pass running: %w", ctx.Err())
	}

	defer func() { <-s.running }()

	before := s.shownBefore(ctx)
	now := s.clock.Now()

	s.sending.Lock()

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, m := range took {
			if err := markInMirror(ctx, q, account, m.id, action, m.gone); err != nil {
				return err
			}
		}

		if action == threadrules.ActionRead {
			return nil
		}

		return settleQueuedFor(ctx, q, account, took, now)
	})

	s.sending.Unlock()

	if err != nil {
		return fmt.Errorf("record the marked GitHub notifications: %w", err)
	}

	s.logger.InfoContext(ctx, "the user marked GitHub notifications", slog.String("as", string(action)),
		slog.Int("count", len(took)))
	s.tell(ctx, before)

	return nil
}

// markInMirror records through q that GitHub took action for account's thread id, or no
// longer had it (gone): a thread marked done, or gone, leaves the mirror, and one marked
// read stays, read. The user's marks and the queued clears both record theirs here.
func markInMirror(
	ctx context.Context, q *queries.Queries, account string, id int64, action threadrules.Action, gone bool,
) error {
	if action == threadrules.ActionRead && !gone {
		return q.SetThreadRead(ctx, queries.SetThreadReadParams{AccountLogin: account, ThreadID: id})
	}

	return q.DeleteThread(ctx, queries.DeleteThreadParams{AccountLogin: account, ThreadID: id})
}

// settleQueuedFor settles, through q, at now, every clear queued for one of the threads
// GitHub took a Done for, as no longer listed: the thread is out of the inbox already.
func settleQueuedFor(ctx context.Context, q *queries.Queries, account string, took []marked, now time.Time) error {
	clears, err := q.ListClearMarks(ctx, account)
	if err != nil {
		return fmt.Errorf("read the queued clears of GitHub notifications: %w", err)
	}

	for _, c := range clears {
		done := slices.ContainsFunc(took, func(m marked) bool { return m.id == c.ThreadID })
		if !done || ClearState(c.State) != ClearQueued {
			continue
		}

		if err = settleWith(ctx, q, c.ID, ClearSkipped, now, "", SkipNotListed); err != nil {
			return err
		}
	}

	return nil
}

// markFailure words a mark GitHub, or the chore lane, refused.
func markFailure(err error, action threadrules.Action) error {
	as := "done"
	if action == threadrules.ActionRead {
		as = "read"
	}

	switch {
	case errors.Is(err, budget.ErrWritesHeld):
		return uierr.New("GitHub asked for a pause before the next change. Try again in a minute.", err)
	case errors.Is(err, budget.ErrWritePace):
		return uierr.New("That's as many changes to GitHub as go in a while. Try again in a minute.", err)
	case errors.Is(err, budget.ErrBudgetReserved):
		return uierr.New("The GitHub budget is down to its reserve. Try again once it refills.", err)
	case errors.Is(err, github.ErrChoreRefused):
		return uierr.New("Notifications can't be changed on GitHub from here.", err)
	}

	return uierr.New(fmt.Sprintf("GitHub didn't mark it %s: %s", as, uierr.Message(err)), err)
}
