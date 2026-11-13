package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The inbox edits GitHub only when the user asks, one item at a time, and only to take
// the user off something: off an item's assignees (Unassign), or off a pull request's
// requested reviewers (RemoveReviewRequest). Each edit is one REST request, which the
// GitHub client's WriteGate paces and the API budget counts, made with the token of the
// account whose inbox the user sees (see checkViewer). Once GitHub has made it, the item
// leaves that list at once, with no refresh, and stays out of it while GitHub's search
// index catches up (see UpsertItem's left stamps).

var (
	// ErrNotAssigned is returned for an edit that takes the user off the assignees of an
	// item not in the Assigned list.
	ErrNotAssigned = errors.New("the item is not in the assigned list")

	// ErrNotRequested is returned for an edit that takes the user off the reviewers of an
	// item not in the Review requested list.
	ErrNotRequested = errors.New("the item is not in the review-requested list")

	// ErrOtherAccount is returned for an edit while the GitHub CLI is signed in to another
	// account than the one whose inbox is shown.
	ErrOtherAccount = errors.New("the GitHub CLI is signed in to another account")
)

// Unassign takes the signed-in user off the assignees of an item in the Assigned list,
// on GitHub, and returns the new snapshot, in which the item has left that list. It is
// one REST request, paced and counted against the API budget, and it waits for nothing
// but other work on the same item.
func (s *Service) Unassign(ctx context.Context, id int64) (Snapshot, error) {
	return s.edit(ctx, id, editUnassign)
}

// RemoveReviewRequest takes the signed-in user off the reviewers a pull request in the
// Review requested list asks, on GitHub, and returns the new snapshot. The pull request
// leaves that list, unless it still asks a team for a review: GitHub's search counts a
// team the user is in, which only the next refresh can tell. A request made only through
// a team is left as it is, since GitHub takes a team's request off for every member or
// none. It is one REST request, paced and counted against the API budget, and it waits
// for nothing but other work on the same item.
func (s *Service) RemoveReviewRequest(ctx context.Context, id int64) (Snapshot, error) {
	return s.edit(ctx, id, editReview)
}

// editKind is which list an edit takes the user off.
type editKind int

const (
	editUnassign editKind = iota
	editReview
)

// verb names an edit in an error.
func (k editKind) verb() string {
	if k == editReview {
		return "remove the review request"
	}

	return "unassign"
}

// outcome is what an edit GitHub made tells.
func (k editKind) outcome() OutcomeKind {
	if k == editReview {
		return OutcomeUnrequested
	}

	return OutcomeUnassigned
}

// edit makes one edit to the item with id on GitHub, then stores what GitHub answered and
// returns the new snapshot. GitHub having made the edit, a failure to store it is
// returned, and the next refresh catches the inbox up. Once it has let go of the item, it
// tells the outcome sink of the edit GitHub made, stored or not.
func (s *Service) edit(ctx context.Context, id int64, kind editKind) (Snapshot, error) {
	snap, told, err := s.editLocked(ctx, id, kind)
	s.settle(ctx, told)

	return snap, err
}

// editLocked is edit under the item's lock. It returns what to tell once the lock is let
// go.
func (s *Service) editLocked(ctx context.Context, id int64, kind editKind) (Snapshot, []Outcome, error) {
	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%s: %w", kind.verb(), err)
	}

	defer unlock()

	target, err := s.editTarget(ctx, id, kind)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%s: %w", kind.verb(), err)
	}

	edited, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (*github.Edited, error) {
		if checkErr := s.checkViewer(ctx, tok, target.login); checkErr != nil {
			return nil, checkErr
		}

		if kind == editReview {
			return s.client.RemoveReviewRequest(ctx, tok, target.ref, target.login)
		}

		return s.client.RemoveAssignee(ctx, tok, target.ref, target.login)
	})
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("%s: %w", kind.verb(), err)
	}

	if edited == nil {
		return Snapshot{}, nil, fmt.Errorf("%s: %w", kind.verb(), ErrIncompleteFetch)
	}

	told := []Outcome{{Kind: kind.outcome(), Login: target.login, Ref: target.ref, At: s.now()}}

	if err = s.storeEdit(ctx, target, kind, edited); err != nil {
		return Snapshot{}, told, uierr.New(
			"GitHub made the change, but the inbox could not record it. The next refresh catches it up.",
			fmt.Errorf("%s: %w", kind.verb(), err),
		)
	}

	snap, err := s.changed(ctx)

	return snap, told, err
}

// editTarget is the item an edit is for, and the login it takes off.
type editTarget struct {
	id    int64
	ref   item.Ref
	login string

	// updatedAt is the item's last activity as the inbox stored it.
	updatedAt time.Time
}

// editTarget reads the item with id, and checks that the signed-in user is in the list
// the edit takes them off.
func (s *Service) editTarget(ctx context.Context, id int64, kind editKind) (editTarget, error) {
	row, err := s.shownItem(ctx, id)
	if err != nil {
		return editTarget{}, err
	}

	ref := refOfRow(row)

	if err = kind.check(ref, row); err != nil {
		return editTarget{}, err
	}

	return editTarget{id: id, ref: ref, login: row.AccountLogin, updatedAt: row.ItemUpdatedAt}, nil
}

// check makes sure the item ref, stored as row, is in the list the edit takes the user
// off.
func (k editKind) check(ref item.Ref, row queries.GetEditItemRow) error {
	switch {
	case k == editUnassign && !row.ReasonAssigned:
		return uierr.New(fmt.Sprintf("You are no longer assigned to %s.", ref), ErrNotAssigned)
	case k == editReview && (ref.Kind != item.KindPR || !row.ReasonReviewRequested):
		return uierr.New(fmt.Sprintf("%s no longer asks for your review.", ref), ErrNotRequested)
	default:
		return nil
	}
}

// storeEdit stores what GitHub answered an edit, in one transaction. Unassigned, the item
// leaves the Assigned list. Off its reviewers, a pull request keeps who it still asks to
// review, and leaves the Review requested list unless it still asks a team.
func (s *Service) storeEdit(ctx context.Context, target editTarget, kind editKind, edited *github.Edited) error {
	// The stamp is GitHub's time for the item after the edit; without one, the later of
	// the item's last activity and now.
	stamp := edited.UpdatedAt.UTC()
	if stamp.IsZero() {
		stamp = later(target.updatedAt.UTC(), s.now())
	}

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if kind == editUnassign {
			return q.LeaveAssigned(ctx, queries.LeaveAssignedParams{AssignLeftAt: &stamp, ID: target.id})
		}

		if err := setReviewRequests(ctx, q, target.id, edited.ReviewRequests); err != nil {
			return err
		}

		if len(edited.ReviewRequests.Teams) > 0 {
			return nil
		}

		return q.LeaveReview(ctx, queries.LeaveReviewParams{ReviewLeftAt: &stamp, ID: target.id})
	})
	if err != nil {
		return fmt.Errorf("save the change: %w", err)
	}

	return nil
}

// setReviewRequests rewrites who the pull request with id asks to review.
func setReviewRequests(ctx context.Context, q *queries.Queries, id int64, r github.ReviewRequests) error {
	users, teams, err := encodeReviewRequests(r)
	if err != nil {
		return err
	}

	err = q.SetReviewRequests(ctx, queries.SetReviewRequestsParams{ReviewUsers: users, ReviewTeams: teams, ItemID: id})
	if err != nil {
		return fmt.Errorf("save the review requests: %w", err)
	}

	return nil
}

// encodeReviewRequests encodes who a pull request asks to review for its two columns,
// each always an array.
func encodeReviewRequests(r github.ReviewRequests) (users, teams string, err error) {
	if users, err = store.EncodeJSON(r.Users); err != nil {
		return "", "", err
	}

	if teams, err = store.EncodeJSON(r.Teams); err != nil {
		return "", "", err
	}

	return users, teams, nil
}

// later returns the later of a and b.
func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
}

// checkViewer makes sure tok belongs to the account login, whose inbox the user sees, so
// an edit never takes another account off anything after gh auth switch. The account
// each token belongs to is remembered from the refreshes and checks made with it, so a
// check costs a request only for a token the inbox has not used yet.
func (s *Service) checkViewer(ctx context.Context, tok github.Token, login string) error {
	viewer, ok := s.viewers.get(tok)
	if !ok {
		var err error

		if viewer, err = s.client.FetchViewer(ctx, tok); err != nil {
			return fmt.Errorf("check the signed-in account: %w", err)
		}

		s.viewers.remember(tok, viewer)
	}

	if !strings.EqualFold(viewer, login) {
		return uierr.New(
			fmt.Sprintf("The GitHub CLI is signed in as %s, but the inbox shows %s's items. Refresh the inbox, then try again.",
				viewer, login),
			fmt.Errorf("%w: %s, not %s", ErrOtherAccount, viewer, login),
		)
	}

	return nil
}

// viewerCache remembers the account the last token the inbox used belongs to. The token
// stays in memory, as the TokenSource keeps it, and is only compared.
type viewerCache struct {
	mu    sync.Mutex
	tok   github.Token
	login string
}

// get returns the account tok belongs to, if tok is the token remembered.
func (c *viewerCache) get(tok github.Token) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.login == "" || !c.tok.Same(tok) {
		return "", false
	}

	return c.login, true
}

// remember records that tok belongs to the account login.
func (c *viewerCache) remember(tok github.Token, login string) {
	if tok.IsZero() || login == "" {
		return
	}

	c.mu.Lock()
	c.tok, c.login = tok, login
	c.mu.Unlock()
}
