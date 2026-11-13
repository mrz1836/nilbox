package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// The inbox tells an OutcomeSink what the user finished from it, and which items it saw
// close or leave its lists, so work that follows the user's items elsewhere hears of it
// without asking GitHub again. Each method works out what to tell while it holds its
// locks, and tells the sink once it has let go of them, on the call's own goroutine, so
// the sink may call the inbox back. A whole refresh's outcomes are told by the caller of
// Refresh once the refresh's flight has returned, never inside it, so a sink that refreshes
// the inbox starts a refresh of its own instead of waiting for the one that told it. The
// sink's context is the call's, never canceled: what GitHub made is told even when the
// caller gave up waiting. While no sink listens, nothing is read or worked out for one.

// OutcomeKind is what happened to an item.
type OutcomeKind string

// Kinds of outcome.
const (
	// OutcomeMerged is a pull request the user merged from the inbox.
	OutcomeMerged OutcomeKind = "merged"

	// OutcomeClosed is a pull request or an issue the user closed from the inbox.
	OutcomeClosed OutcomeKind = "closed"

	// OutcomeReviewed is a pull request the user approved or asked changes of from the
	// inbox, with no merge or close in the same action. A comment is not a review here.
	OutcomeReviewed OutcomeKind = "reviewed"

	// OutcomeDismissed is an item the user dismissed that wasn't, and OutcomeRestored a
	// dismissed or snoozed one the user brought back.
	OutcomeDismissed OutcomeKind = "dismissed"
	OutcomeRestored  OutcomeKind = "restored"

	// OutcomeUnassigned is an item the user took themselves off the assignees of, and
	// OutcomeUnrequested a pull request they took themselves off the reviewers of, once
	// GitHub made the edit.
	OutcomeUnassigned  OutcomeKind = "unassigned"
	OutcomeUnrequested OutcomeKind = "unrequested"

	// OutcomeSeenClosed is an item a read of the inbox's, or an action on it, found merged
	// or closed: by someone else, or outside the inbox. A read after the user's own merge
	// or close from the inbox tells it too. A whole refresh tells it only of an item the
	// lists held before it.
	OutcomeSeenClosed OutcomeKind = "seen_closed"

	// OutcomeUnlisted is an item a whole refresh found in none of the lists it was in,
	// with no sign of why. A list GitHub cut short tells none, since its items past the
	// page may still be in it, and an item whose repository was archived tells none.
	OutcomeUnlisted OutcomeKind = "unlisted"
)

// Outcome is what happened to one item.
type Outcome struct {
	Kind OutcomeKind

	// Login is the account whose inbox lists the item: the one its token belongs to.
	Login string

	// Ref names the item.
	Ref item.Ref

	// At is when it happened: when the inbox learned of it, or the review's own time.
	At time.Time
}

// OutcomeSink is told about outcomes, synchronously, once the inbox has let go of every
// lock it held to work them out, and outside any refresh's flight. Its context carries the
// values of the call that found them, but is never canceled.
type OutcomeSink interface {
	Settle(ctx context.Context, outcomes []Outcome)
}

// settle tells the sink about outcomes, if there are any and a sink, with a context that
// can't be canceled. The caller holds no lock of the inbox's.
func (s *Service) settle(ctx context.Context, outcomes []Outcome) {
	if s.outcomes != nil && len(outcomes) > 0 {
		s.outcomes.Settle(context.WithoutCancel(ctx), outcomes)
	}
}

// heldOutcomes holds what whole refreshes found, until a caller of Refresh tells it, once,
// after the refresh's flight has returned. What no caller was there to tell waits for the
// next.
type heldOutcomes struct {
	mu       sync.Mutex
	outcomes []Outcome
}

// hold keeps outcomes for the next caller back from a refresh.
func (h *heldOutcomes) hold(outcomes []Outcome) {
	if len(outcomes) == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.outcomes = append(h.outcomes, outcomes...)
}

// take returns what is held and forgets it, so it is told once.
func (h *heldOutcomes) take() []Outcome {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := h.outcomes
	h.outcomes = nil

	return out
}

// refOf names an item stored as owner/name#number, of kind.
func refOf(owner, name string, number int64, kind item.Kind) item.Ref {
	return item.Ref{Repo: repo.Name{Owner: owner, Repo: name}, Number: int(number), Kind: kind}
}

// decides reports whether a review's event is a decision, an approval or a request for
// changes, rather than a comment.
func decides(event string) bool {
	return event == github.ReviewApprove || event == github.ReviewRequestChanges
}

// pullOutcomes is what an action on the pull request whose row is row tells, when it made
// acted happen: a merge, a close, or else a review that decided, at the review's own time
// when GitHub gave one. A COMMENT review, such as the text a merge or a close posts first,
// tells nothing.
func pullOutcomes(row editRow, acted Acted, now time.Time) []Outcome {
	out := Outcome{Login: row.AccountLogin, Ref: refOfRow(row), At: now}

	switch {
	case acted.Merged:
		out.Kind = OutcomeMerged
	case acted.Closed:
		out.Kind = OutcomeClosed
	case acted.Reviewed && decides(acted.ReviewEvent):
		out.Kind = OutcomeReviewed

		if !acted.ReviewedAt.IsZero() {
			out.At = acted.ReviewedAt.UTC()
		}
	default:
		return nil
	}

	return []Outcome{out}
}

// issueOutcomes is what an action on the issue whose row is row tells, when it closed it
// or found it closed: the user's own close (own), or one made first, which the action only
// saw.
func issueOutcomes(row editRow, closed, own bool, now time.Time) []Outcome {
	if !closed {
		return nil
	}

	kind := OutcomeSeenClosed
	if own {
		kind = OutcomeClosed
	}

	return []Outcome{{Kind: kind, Login: row.AccountLogin, Ref: refOfRow(row), At: now}}
}

// seenClosed is what a stored read of one item tells: the item merged or closed, when its
// answer says so. An item GitHub no longer finds, or whose repository was archived, leaves
// the lists too, but was not seen to close.
func seenClosed(w itemWrite, login string, ref item.Ref, now time.Time) []Outcome {
	if w.update == nil || !w.update.update.Found || !w.update.update.Item.Closed() {
		return nil
	}

	return []Outcome{{Kind: OutcomeSeenClosed, Login: login, Ref: ref, At: now}}
}

// triageItem is an item as a change to its triage tells it, with its triage before the
// change.
type triageItem = queries.GetTriageItemRow

// triageBefore reads the item with id, in the transaction of a change to its triage and
// before it, for what the change tells. It reads nothing while no sink listens, and
// reports false then, or when no item has id.
func (s *Service) triageBefore(ctx context.Context, q *queries.Queries, id int64) (triageItem, bool, error) {
	if s.outcomes == nil {
		return triageItem{}, false, nil
	}

	row, err := q.GetTriageItem(ctx, id)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return triageItem{}, false, nil
	case err != nil:
		return triageItem{}, false, fmt.Errorf("read the item's triage: %w", err)
	default:
		return row, true, nil
	}
}

// setAside reports whether the item t was dismissed, or snoozed past now.
func setAside(t triageItem, now time.Time) bool {
	return t.DismissedAt != nil || (t.SnoozedUntil != nil && t.SnoozedUntil.After(now))
}

// triageOutcome is what a change to the item t's triage tells, as kind, at.
func triageOutcome(kind OutcomeKind, t triageItem, at time.Time) []Outcome {
	ref := refOf(t.RepoOwner, t.RepoName, t.Number, item.Kind(t.Kind))

	return []Outcome{{Kind: kind, Login: t.AccountLogin, Ref: ref, At: at}}
}

// listedRow is an item the lists held before a refresh rewrote them.
type listedRow = queries.ListListedRow

// refreshOutcomes is what a whole refresh that stored inbox tells, for its account, of the
// items the lists held before it (before; nothing when there is none): each one it found
// merged or closed (closed, the items dropClosed took out of the lists), then each one it
// saved in no list, found neither closed nor archived (archived), and that was in no list
// GitHub cut short this time, since it may still be in one, past the page. It works out
// nothing while no sink listens. The caller has saved the items (w.saved).
func (w *writer) refreshOutcomes(inbox *github.Inbox, closed, archived []github.Item, before *listedBefore) []Outcome {
	if !w.tellOutcomes || before == nil {
		return nil
	}

	login := inbox.Viewer.Login
	wasClosed, wasArchived, listed := keysOf(closed), keysOf(archived), w.savedListed()
	cut := cutShort{
		reviewRequested: truncated(inbox.ReviewRequested), authored: truncated(inbox.Authored),
		assigned: truncated(inbox.Assigned),
	}

	var seen, unlisted []Outcome

	for _, r := range before.rows {
		ref := refOf(r.RepoOwner, r.RepoName, r.Number, item.Kind(r.Kind))

		switch {
		case wasClosed.has(r, ref):
			seen = append(seen, Outcome{Kind: OutcomeSeenClosed, Login: login, Ref: ref, At: w.now})
		case listed[r.ID], wasArchived.has(r, ref), cut.mayHold(r):
		default:
			unlisted = append(unlisted, Outcome{Kind: OutcomeUnlisted, Login: login, Ref: ref, At: w.now})
		}
	}

	return append(seen, unlisted...)
}

// itemKeys holds items a refresh read by node ID and by name, so a stored row finds the
// one it is whichever of the two it shares.
type itemKeys map[string]bool

// keysOf returns the keys of items.
func keysOf(items []github.Item) itemKeys {
	keys := make(itemKeys, 2*len(items))

	for _, it := range items {
		keys[it.Ref().Key()] = true

		if it.NodeID != "" {
			keys[it.NodeID] = true
		}
	}

	return keys
}

// has reports whether the stored row r, named ref, is one of the items.
func (k itemKeys) has(r listedRow, ref item.Ref) bool {
	return k[ref.Key()] || (r.NodeID != "" && k[r.NodeID])
}

// savedListed returns the items the writer saved in a list, by id: their lists as stored,
// so a list the user left from the inbox stays left.
func (w *writer) savedListed() map[int64]bool {
	listed := make(map[int64]bool, len(w.saved))

	for _, s := range w.saved {
		if e := s.entry; e.reviewRequested || e.authored || e.assigned {
			listed[s.id] = true
		}
	}

	return listed
}

// cutShort is which of a refresh's lists GitHub cut short: it matched more items than it
// returned.
type cutShort struct {
	reviewRequested bool
	authored        bool
	assigned        bool
}

// mayHold reports whether an item a refresh did not return may still be in one of the
// lists it was in, past the page GitHub returned.
func (c cutShort) mayHold(r listedRow) bool {
	return (r.ReasonReviewRequested && c.reviewRequested) || (r.ReasonAuthored && c.authored) ||
		(r.ReasonAssigned && c.assigned)
}

// truncated reports a list GitHub matched more items for than it returned.
func truncated(list github.SearchResult) bool {
	return list.IssueCount > len(list.Items)
}
