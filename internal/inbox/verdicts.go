package inbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
)

// The user's own verdict on a pull request (their latest review that approved it or
// requested changes) is read by the whole refresh's fourth search, and by a read of the
// pull request alone (a card's refresh, the read after an agent session stops working,
// the review sheet's), so a review an agent left with gh, or one the user left in a
// browser, reaches the card at once. Each row keeps the verdict with when it was submitted
// (viewer_review_at), so a read tells the verdict it holds from a newer one, and when the
// newest verdict seen was submitted (viewer_review_seen_at), which only rises, so each
// review is told once as the user's (OutcomeReviewed), whichever read sees it first.

// reviewWindow is how long after its submission a review the inbox sees, but didn't send,
// is still told as the user's: twice the longest refresh interval, so a review left in a
// browser while the inbox refreshes hourly, behind GitHub's search index, still counts,
// while one seen for the first time long after (an upgrade, a pull request back in the
// fourth search's page) tells nothing.
const reviewWindow = 2 * time.Hour

// verdictAt is when v was submitted, to the second, as GitHub reports it and the inbox
// stores it, so two reads of one review always compare equal; zero with none.
func verdictAt(v *github.Verdict) time.Time {
	if v == nil {
		return time.Time{}
	}

	return v.SubmittedAt.UTC().Truncate(time.Second)
}

// newlySubmitted reports whether v is a verdict to tell as the user's review: submitted
// after the newest one seen (seen; nil before any), and within reviewWindow of now.
func newlySubmitted(v *github.Verdict, seen *time.Time, now time.Time) bool {
	at := verdictAt(v)

	switch {
	case at.IsZero():
		return false
	case seen != nil && !at.After(seen.UTC().Truncate(time.Second)):
		return false
	default:
		return now.Sub(at) <= reviewWindow
	}
}

// reviewedOutcome is what a read that found the user's verdict v newly submitted tells
// (see newlySubmitted): a review of the user's on the pull request ref, of the account
// with login, at GitHub's time, so the moments it records are as old as the review.
func reviewedOutcome(v *github.Verdict, login string, ref item.Ref) []Outcome {
	if v == nil {
		return nil
	}

	at := verdictAt(v)

	return []Outcome{{
		Kind: OutcomeReviewed, Login: login, Ref: ref, At: at,
		GitHub: GitHubFact{State: v.State, Commit: v.Commit, At: at},
	}}
}

// readReviewSeen reads, before a whole refresh stores reviewed (the fourth search's pull
// requests), when the newest verdict of the user's seen on each that holds a verdict
// recent enough to tell was submitted, by node ID: one read of the few reviewed lately,
// none when no verdict is recent.
func (w *writer) readReviewSeen(ctx context.Context, reviewed []github.Item) (map[string]*time.Time, error) {
	ids := make([]string, 0, len(reviewed))

	for _, it := range reviewed {
		if newlySubmitted(it.Verdict, nil, w.now) && it.NodeID != "" {
			ids = append(ids, it.NodeID)
		}
	}

	seen := make(map[string]*time.Time, len(ids))
	if len(ids) == 0 {
		return seen, nil
	}

	rows, err := w.q.ListViewerReviewSeen(ctx, queries.ListViewerReviewSeenParams{AccountID: w.accountID, NodeIds: ids})
	if err != nil {
		return nil, fmt.Errorf("read the verdicts seen: %w", err)
	}

	for _, r := range rows {
		seen[r.NodeID] = r.ViewerReviewSeenAt
	}

	return seen, nil
}

// reviewedOutcomes is what a whole refresh tells of reviewed, the fourth search's pull
// requests, once it stored them: a review of the user's for each whose verdict was newly
// submitted since the newest seen before the refresh (seen, see readReviewSeen), as one
// left in a browser, which no read of the pull request alone saw first.
func reviewedOutcomes(reviewed []github.Item, seen map[string]*time.Time, login string, now time.Time) []Outcome {
	var told []Outcome

	for _, it := range reviewed {
		if newlySubmitted(it.Verdict, seen[it.NodeID], now) {
			told = append(told, reviewedOutcome(it.Verdict, login, it.Ref())...)
		}
	}

	return told
}

// readsVerdict reports whether u, an item's own read, read the user's own verdict on a
// pull request: GitHub was asked for it, and the token it was read with belongs to the
// account the item is stored for, since GitHub says which verdict is the viewer's by the
// token. A read whose token's account isn't known yet (see Service.viewers) reads none,
// and the next whole refresh reads it.
func (u *itemUpdate) readsVerdict() bool {
	return u.update != nil && u.update.VerdictRead && u.update.Item.IsPR &&
		u.viewer != "" && strings.EqualFold(u.viewer, u.login)
}

// noteReadVerdict stores the user's verdict a read of the pull request with id alone found
// (it.Verdict, nil with none), in the write's transaction, after what the read stored of
// the item:
//
//   - A verdict that is the one stored (the same state, submitted at the same second) is
//     left as stored, so a commit the whole refresh moved onto the head, since the user
//     alone pushed since (coverOwnPushes), stays moved.
//   - A verdict older than the one stored is left too: a review the user left from the
//     inbox since the read went out, which stored its own.
//   - Any other is stored whole, its own commit included; none clears the one stored, as a
//     review dismissed since does.
//   - A pull request listed in Review requested only because it was pushed to since the
//     user requested changes leaves it when the verdict read is no longer pushed past (an
//     approval, a request for changes at its head, or none). A read never lists it there:
//     that takes the whole refresh, which asks who pushed.
//
// When the verdict was submitted rises to the read's, and the first time the write is
// applied (tellVerdict), a verdict newly submitted (see newlySubmitted) is kept to tell as
// the user's review, so a replay tells nothing again.
func (w *writer) noteReadVerdict(ctx context.Context, id int64, it github.Item) error {
	stored, err := w.q.GetViewerVerdict(ctx, id)
	if err != nil {
		return fmt.Errorf("read the verdict stored on %s: %w", it.Ref(), err)
	}

	v := it.Verdict

	if kept := keptVerdict(stored, v, id); changesVerdict(stored, kept) {
		if err = w.q.NoteViewerReview(ctx, kept); err != nil {
			return fmt.Errorf("record the verdict on %s: %w", it.Ref(), err)
		}
	}

	if !it.PushedSinceChanges() {
		if err = w.q.LeavePushed(ctx, id); err != nil {
			return fmt.Errorf("take %s out of Review requested: %w", it.Ref(), err)
		}
	}

	if w.tellVerdict && newlySubmitted(v, stored.ViewerReviewSeenAt, w.now) {
		w.reviewed = v
	}

	return nil
}

// keptVerdict is what the item with id keeps of the verdict v a read found, given the one
// stored (see noteReadVerdict), with when the newest verdict seen was submitted raised to
// v's (NoteViewerReview never lowers it).
func keptVerdict(stored queries.GetViewerVerdictRow, v *github.Verdict, id int64) queries.NoteViewerReviewParams {
	at, storedAt := verdictAt(v), secondOf(stored.ViewerReviewAt)
	kept := queries.NoteViewerReviewParams{
		ViewerReviewState: stored.ViewerReviewState, ViewerReviewCommit: stored.ViewerReviewCommit,
		ReviewedAt: stored.ViewerReviewAt, SeenAt: timeOrNil(at), ID: id,
	}

	switch {
	case v == nil:
		kept.ViewerReviewState, kept.ViewerReviewCommit, kept.ReviewedAt = "", "", nil
	case !storedAt.IsZero() && storedAt.Equal(at) && stored.ViewerReviewState == v.State:
	case !storedAt.IsZero() && storedAt.After(at):
	default:
		kept.ViewerReviewState, kept.ViewerReviewCommit, kept.ReviewedAt = v.State, v.Commit, timeOrNil(at)
	}

	return kept
}

// changesVerdict reports whether writing kept changes what stored holds: the verdict, its
// commit, when it was submitted, or, raised, when the newest verdict seen was.
func changesVerdict(stored queries.GetViewerVerdictRow, kept queries.NoteViewerReviewParams) bool {
	seen := secondOf(stored.ViewerReviewSeenAt)

	return kept.ViewerReviewState != stored.ViewerReviewState ||
		kept.ViewerReviewCommit != stored.ViewerReviewCommit ||
		!secondOf(kept.ReviewedAt).Equal(secondOf(stored.ViewerReviewAt)) ||
		(kept.SeenAt != nil && (seen.IsZero() || secondOf(kept.SeenAt).After(seen)))
}

// secondOf is t to the second, in UTC; zero for nil.
func secondOf(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return t.UTC().Truncate(time.Second)
}
