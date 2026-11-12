package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The review sheet reads a pull request and acts on it through an Actor, which gives its
// work the inbox's own footing: the item's lock, so a read, an action, and the item's
// refresh never overlap; the check that the token belongs to the account whose inbox is
// shown, before anything changes on GitHub; and the write ordering of order.go, so what
// the inbox records survives a refresh that was already fetching. What the work itself
// asks GitHub is the caller's, passed in as a function, which is why an Actor is not a
// service the window binds: the window cannot pass a function.

// ErrNotPull is returned when the item an action or a read is for is not a pull request,
// or an action's pull request is in none of the inbox's lists.
var ErrNotPull = errors.New("the item is not a pull request in the inbox")

// PullTarget is the pull request an action or a read is for, as the inbox stored it.
type PullTarget struct {
	ID     int64
	Ref    item.Ref
	NodeID string

	// Login is the account whose inbox lists the pull request: the account every token an
	// action uses must belong to.
	Login string

	// Head is the head commit the last refresh read, or "" before one has.
	Head string

	// UpdatedAt is the pull request's last activity, as the inbox stored it.
	UpdatedAt time.Time

	// ReviewRequested, Authored, and Assigned are the lists the pull request is in.
	ReviewRequested bool
	Authored        bool
	Assigned        bool
}

// Acted is what an action made happen on GitHub, for the inbox to record. An action that
// failed partway reports the steps GitHub made all the same.
type Acted struct {
	// Reviewed reports a review GitHub recorded, at ReviewedAt (zero when it did not say).
	// Any review fulfills a request for one.
	Reviewed   bool
	ReviewedAt time.Time

	// Merged reports a merge GitHub made, and Closed a close without one.
	Merged bool
	Closed bool
}

// with returns what a and b made happen together, as two tries of one action do.
func (a Acted) with(b Acted) Acted {
	a.Reviewed = a.Reviewed || b.Reviewed
	a.ReviewedAt = later(a.ReviewedAt, b.ReviewedAt)
	a.Merged = a.Merged || b.Merged
	a.Closed = a.Closed || b.Closed

	return a
}

// any reports whether the action changed anything the inbox records.
func (a Acted) any() bool {
	return a.Reviewed || a.Merged || a.Closed
}

// PullRead is what a caller's read of a pull request found: the item as GitHub answered,
// which rewrites the inbox card, and, when the read listed them, the files the pull
// request changes at that head, which serve as its sensitive-file check.
type PullRead struct {
	Update *github.ItemUpdate
	Files  *github.ChangedFiles
}

// Actor reads and acts on pull requests for the review sheet. It is safe for concurrent
// use.
type Actor struct {
	s *Service
}

// NewActor returns an Actor over the inbox s.
func NewActor(s *Service) *Actor {
	return &Actor{s: s}
}

// Act runs act on the pull request with id, a pull request in one of the inbox's lists,
// with a token of the account whose inbox is shown, and returns the new snapshot. It holds
// the item's lock throughout, and checks the token's account before act runs; a token
// GitHub refuses is borrowed again once, and act runs again with it, so act must not
// repeat a step it made. What act reports is recorded even when it also returns an error,
// since a step GitHub made stands: a review takes the pull request out of the Review
// requested list, and a merge or a close out of every list, at once and with no request.
func (a *Actor) Act(
	ctx context.Context, id int64, act func(context.Context, github.Token, PullTarget) (Acted, error),
) (Snapshot, error) {
	s := a.s

	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}

	defer unlock()

	target, err := s.pullTarget(ctx, id, true)
	if err != nil {
		return Snapshot{}, err
	}

	var acted Acted

	_, err = github.WithToken(ctx, s.tokens, func(tok github.Token) (struct{}, error) {
		if checkErr := s.checkViewer(ctx, tok, target.Login); checkErr != nil {
			return struct{}{}, checkErr
		}

		got, actErr := act(ctx, tok, target)
		acted = acted.with(got)

		return struct{}{}, actErr
	})

	if !acted.any() {
		snap, snapErr := s.Snapshot(ctx)

		return snap, errors.Join(err, snapErr)
	}

	if recordErr := s.recordActed(ctx, target, acted); recordErr != nil {
		err = errors.Join(err, uierr.New(
			"GitHub made the change, but the inbox could not record it. The next refresh catches it up.", recordErr,
		))
	}

	snap, snapErr := s.changed(ctx)

	return snap, errors.Join(err, snapErr)
}

// ReadPull runs read on the pull request with id, with a token, and stores what it found,
// as RefreshItem does with its own read: the card is rewritten from the answer, or the pull
// request leaves the lists when it was merged, closed, or deleted since, and its files
// are stored as its sensitive-file check when one is due and they belong to the head the
// answer names. It returns the new snapshot. It never waits for a whole refresh, only for
// other work on the item, and a whole refresh that started later and stored the item
// makes it store nothing.
func (a *Actor) ReadPull(
	ctx context.Context, id int64, read func(context.Context, github.Token, PullTarget) (PullRead, error),
) (Snapshot, error) {
	s := a.s

	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, err
	}

	defer unlock()

	target, accountID, err := s.pullTargetOf(ctx, id, false)
	if err != nil {
		return Snapshot{}, err
	}

	var seq uint64

	got, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (PullRead, error) {
		seq = s.startFetch() // each try numbers its own fetch

		return read(ctx, tok, target)
	})
	if err != nil {
		return Snapshot{}, err
	}

	if got.Update == nil {
		return Snapshot{}, ErrIncompleteFetch
	}

	files, err := s.readFiles(ctx, id, got)
	if err != nil {
		return Snapshot{}, err
	}

	wrote, err := s.storeItemWrite(ctx, itemWrite{
		id: id, seq: seq, update: &itemUpdate{accountID: accountID, update: got.Update, files: files},
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("save the pull request: %w", err)
	}

	if !wrote {
		return s.Snapshot(ctx)
	}

	return s.changed(ctx)
}

// pullTarget reads the pull request with id, checking it belongs to the account whose
// inbox is shown and, when listed is set, that it is in one of the lists.
func (s *Service) pullTarget(ctx context.Context, id int64, listed bool) (PullTarget, error) {
	target, _, err := s.pullTargetOf(ctx, id, listed)

	return target, err
}

// pullTargetOf is pullTarget that also returns the id of the account the pull request is
// stored for.
func (s *Service) pullTargetOf(ctx context.Context, id int64, listed bool) (PullTarget, int64, error) {
	row, err := s.shownItem(ctx, id)
	if err != nil {
		return PullTarget{}, 0, err
	}

	target := pullTargetOfRow(row)

	switch {
	case target.Ref.Kind != item.KindPR || target.NodeID == "":
		return PullTarget{}, 0, fmt.Errorf("%w (id %d)", ErrNotPull, id)
	case listed && !target.ReviewRequested && !target.Authored && !target.Assigned:
		return PullTarget{}, 0, uierr.New(fmt.Sprintf("%s is no longer in your inbox.", target.Ref),
			fmt.Errorf("%w (id %d)", ErrNotPull, id))
	default:
		return target, row.AccountID, nil
	}
}

// shownItem reads the item with id, which must be one of the signed-in account's: only
// its items are shown, so another's is not found.
func (s *Service) shownItem(ctx context.Context, id int64) (queries.GetEditItemRow, error) {
	row, err := s.q.GetEditItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return queries.GetEditItemRow{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	if err != nil {
		return queries.GetEditItemRow{}, fmt.Errorf("read the item: %w", err)
	}

	viewer, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil {
		return queries.GetEditItemRow{}, fmt.Errorf("read the signed-in account: %w", err)
	}

	if viewer == "" || !strings.EqualFold(viewer, row.AccountLogin) {
		return queries.GetEditItemRow{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	return row, nil
}

// pullTargetOfRow converts a GetEditItem row.
func pullTargetOfRow(row queries.GetEditItemRow) PullTarget {
	return PullTarget{
		ID: row.ID,
		Ref: item.Ref{
			Repo: repo.Name{Owner: row.RepoOwner, Repo: row.RepoName}, Number: int(row.Number), Kind: item.Kind(row.Kind),
		},
		NodeID: row.NodeID, Login: row.AccountLogin, Head: value(row.HeadOID), UpdatedAt: row.ItemUpdatedAt.UTC(),
		ReviewRequested: row.ReasonReviewRequested, Authored: row.ReasonAuthored, Assigned: row.ReasonAssigned,
	}
}

// recordActed records what an action made happen on target: a review takes it out of the
// Review requested list, stamped as an edit is (see UpsertItem), and a merge or a close
// takes it out of every list, as an item write numbered now, after GitHub's answer, so a
// refresh that was fetching applies it again.
func (s *Service) recordActed(ctx context.Context, target PullTarget, acted Acted) error {
	if acted.Reviewed && target.ReviewRequested {
		stamp := later(acted.ReviewedAt.UTC(), target.UpdatedAt)

		err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
			return s.q.WithTx(tx).LeaveReview(ctx, queries.LeaveReviewParams{ReviewLeftAt: &stamp, ID: target.ID})
		})
		if err != nil {
			return fmt.Errorf("record the review: %w", err)
		}
	}

	if acted.Merged || acted.Closed {
		if _, err := s.storeItemWrite(ctx, itemWrite{id: target.ID, seq: s.startFetch(), unlist: true}); err != nil {
			return fmt.Errorf("record the merge or close: %w", err)
		}
	}

	return nil
}

// readFiles returns the file check a read serves: the files it listed, when the item's
// files are checked, a check is due, and the files belong to the head the read's answer
// names. A check that is due but has no files to use waits for the next refresh.
func (s *Service) readFiles(ctx context.Context, id int64, got PullRead) (map[string]fileCheck, error) {
	update := got.Update
	if !update.Found || got.Files == nil || !strings.EqualFold(got.Files.Head, update.Details.HeadOID) {
		return map[string]fileCheck{}, nil
	}

	row, scope, err := s.loadCheckItem(ctx, id)
	if err != nil {
		return nil, err
	}

	last := map[string]filesStored{}
	if row.Stored != nil {
		last[update.Item.NodeID] = filesStored{HeadOID: row.Stored.Head, Error: row.Stored.Error}
	}

	if _, ok := scope.due(update.Item, update.Details.HeadOID, last); !ok {
		return map[string]fileCheck{}, nil
	}

	return map[string]fileCheck{update.Item.NodeID: {files: got.Files}}, nil
}
