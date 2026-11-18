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
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The review sheet reads a pull request and acts on it through an Actor, and the issue
// sheet an issue, which gives their work the inbox's own footing: the item's lock, so a
// read, an action, and the item's refresh never overlap; the check that the token belongs
// to the account whose inbox is shown, before anything changes on GitHub; and the write
// ordering of order.go, so what the inbox records survives a refresh that was already
// fetching. Each kind has its own methods, which refuse an item of the other kind, so the
// review sheet can never act on an issue nor the issue sheet on a pull request. What the
// work itself asks GitHub is the caller's, passed in as a function, which is why an Actor
// is not a service the window binds: the window cannot pass a function.

var (
	// ErrNotPull is returned when the item an action or a read is for is not a pull
	// request, or an action's pull request is in none of the inbox's lists.
	ErrNotPull = errors.New("the item is not a pull request in the inbox")

	// ErrNotIssue is returned when the item an action or a read is for is not an issue, or
	// an action's issue is in none of the inbox's lists.
	ErrNotIssue = errors.New("the item is not an issue in the inbox")
)

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
	// Reviewed reports a review GitHub recorded, at ReviewedAt (zero when it did not say),
	// and ReviewEvent which it was: github.ReviewApprove, github.ReviewRequestChanges, or
	// github.ReviewComment, which the text a merge or a close posts first is too. Any
	// review fulfills a request for one.
	Reviewed    bool
	ReviewedAt  time.Time
	ReviewEvent string

	// Merged reports a merge GitHub made, and Closed a close without one.
	Merged bool
	Closed bool

	// Rerequested reports the user's own review requested again, at the user's click, with
	// who the pull request asks now as GitHub answered (ReviewRequests), at GitHub's time of
	// the edit (RerequestedAt, zero when it did not say): the pull request asks the user
	// again, so it is back in Review requested.
	Rerequested    bool
	ReviewRequests github.ReviewRequests
	RerequestedAt  time.Time
}

// with returns what a and b made happen together, as two tries of one action do.
func (a Acted) with(b Acted) Acted {
	a.Reviewed = a.Reviewed || b.Reviewed
	a.ReviewedAt = later(a.ReviewedAt, b.ReviewedAt)
	a.ReviewEvent = strongerEvent(a.ReviewEvent, b.ReviewEvent)
	a.Merged = a.Merged || b.Merged
	a.Closed = a.Closed || b.Closed

	if b.Rerequested {
		a.ReviewRequests = b.ReviewRequests
	}

	a.Rerequested = a.Rerequested || b.Rerequested
	a.RerequestedAt = later(a.RerequestedAt, b.RerequestedAt)

	return a
}

// strongerEvent is the review two tries of one action submitted together: a decision (an
// approval or a request for changes) over a comment, and over none, b's of two alike.
func strongerEvent(a, b string) string {
	if b == "" || (decides(a) && !decides(b)) {
		return a
	}

	return b
}

// any reports whether the action changed anything the inbox records.
func (a Acted) any() bool {
	return a.Reviewed || a.Merged || a.Closed || a.Rerequested
}

// PullRead is what a caller's read of a pull request found: the item as GitHub answered,
// which rewrites the inbox card, and, when the read listed them, the files the pull
// request changes at that head, which serve as its sensitive-file check.
type PullRead struct {
	Update *github.ItemUpdate
	Files  *github.ChangedFiles
}

// IssueTarget is the issue an action or a read is for, as the inbox stored it.
type IssueTarget struct {
	ID     int64
	Ref    item.Ref
	NodeID string

	// Login is the account whose inbox lists the issue: the account every token an action
	// uses must belong to.
	Login string

	// UpdatedAt is the issue's last activity, as the inbox stored it.
	UpdatedAt time.Time

	// Assigned reports that the issue is assigned to the user, the only reason an issue is
	// in the inbox.
	Assigned bool

	// Projects says to read the issue's projects, as the last whole refresh could: a read
	// that rewrites the card without them would wipe its projects.
	Projects bool
}

// IssueRead is what a caller's read of an issue found: the item as GitHub answered, which
// rewrites the inbox card.
type IssueRead struct {
	Update *github.ItemUpdate
}

// IssueActed is what an action on an issue made happen on GitHub, for the inbox to record.
// An action that failed partway reports what GitHub made all the same.
type IssueActed struct {
	// Closed reports a close GitHub made, or found made: the issue leaves every list.
	Closed bool

	// Own reports that the close was the user's own and went through, not one someone
	// made first that the action found.
	Own bool
}

// Actor reads and acts on pull requests for the review sheet, and on issues for the issue
// sheet. It is safe for concurrent use.
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
//
// Once it has let go of the item, it tells the outcome sink what the action finished, even
// when the inbox could not record it: a merge, a close, or else an approval or a request
// for changes.
func (a *Actor) Act(
	ctx context.Context, id int64, act func(context.Context, github.Token, PullTarget) (Acted, error),
) (Snapshot, error) {
	s := a.s

	snap, done, err := s.actOn(ctx, id, item.KindPR, func(ctx context.Context, tok github.Token, row editRow) (Acted, error) {
		return act(ctx, tok, pullTargetOfRow(row))
	})

	s.settle(ctx, pullOutcomes(done.row, done.acted, s.now()))

	if done.acted.any() {
		s.tellChanged([]int64{id})
	}

	return snap, err
}

// ActIssue runs act on the issue with id, an issue assigned to the user, as Act runs an
// action on a pull request: under the item's lock, with a token of the account whose inbox
// is shown, which a token GitHub refuses is borrowed again for, once, so act must not
// repeat a step it made. A close act reports is recorded even when act also returns an
// error, and takes the issue out of every list at once, with no request. Once it has let
// go of the item, it tells the outcome sink of the close: as the user's own when act says
// so (IssueActed.Own), and otherwise as one it found made.
func (a *Actor) ActIssue(
	ctx context.Context, id int64, act func(context.Context, github.Token, IssueTarget) (IssueActed, error),
) (Snapshot, error) {
	s := a.s
	own := false

	snap, done, err := s.actOn(ctx, id, item.KindIssue, func(ctx context.Context, tok github.Token, row editRow) (Acted, error) {
		got, err := act(ctx, tok, issueTargetOfRow(row, false))
		own = own || (got.Closed && got.Own)

		return Acted{Closed: got.Closed}, err
	})

	s.settle(ctx, issueOutcomes(done.row, done.acted.Closed, own, s.now()))

	if done.acted.any() {
		s.tellChanged([]int64{id})
	}

	return snap, err
}

// editRow is an item as an edit reads it (GetEditItem).
type editRow = queries.GetEditItemRow

// actedOn is what an action made happen on the item whose row is row: nothing when it never
// ran.
type actedOn struct {
	row   editRow
	acted Acted
}

// actOn runs act on the item with id, of kind, listed in one of the inbox's lists, with a
// token of the account whose inbox is shown, holding the item's lock throughout, and
// records what act made happen (see Act). It returns that too, for the caller to tell
// once the item's lock is let go.
func (s *Service) actOn(
	ctx context.Context, id int64, kind item.Kind, act func(context.Context, github.Token, editRow) (Acted, error),
) (Snapshot, actedOn, error) {
	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, actedOn{}, err
	}

	defer unlock()

	row, err := s.targetRow(ctx, id, kind, true)
	if err != nil {
		return Snapshot{}, actedOn{}, err
	}

	done := actedOn{row: row}

	_, err = github.WithToken(ctx, s.tokens, func(tok github.Token) (struct{}, error) {
		if checkErr := s.checkViewer(ctx, tok, row.AccountLogin); checkErr != nil {
			return struct{}{}, checkErr
		}

		got, actErr := act(ctx, tok, row)
		done.acted = done.acted.with(got)

		return struct{}{}, actErr
	})

	if !done.acted.any() {
		snap, snapErr := s.Snapshot(ctx)

		return snap, done, errors.Join(err, snapErr)
	}

	if recordErr := s.recordActed(ctx, row, done.acted); recordErr != nil {
		err = errors.Join(err, uierr.New(
			"GitHub made the change, but the inbox could not record it. The next refresh catches it up.", recordErr,
		))
	}

	snap, snapErr := s.changed(ctx)

	return snap, done, errors.Join(err, snapErr)
}

// ReadPull runs read on the pull request with id, with a token, and stores what it found,
// as RefreshItem does with its own read: the card is rewritten from the answer, leaving
// Review requested once GitHub no longer asks the user to review it, or the pull
// request leaves the lists when it was merged, closed, or deleted since, and its files
// are stored as its sensitive-file check when one is due and they belong to the head the
// answer names. It returns the new snapshot. It never waits for a whole refresh, only for
// other work on the item, and a whole refresh that started later and stored the item
// makes it store nothing. Once it has let go of the item, it tells the outcome sink of a
// merge or a close the answer it stored shows.
func (a *Actor) ReadPull(
	ctx context.Context, id int64, read func(context.Context, github.Token, PullTarget) (PullRead, error),
) (Snapshot, error) {
	snap, told, err := a.s.readPull(ctx, id, read)
	a.s.settle(ctx, told)

	if err == nil {
		a.s.tellChanged([]int64{id})
	}

	return snap, err
}

// readPull is ReadPull under the item's lock. It returns what to tell once the lock is let
// go.
func (s *Service) readPull(
	ctx context.Context, id int64, read func(context.Context, github.Token, PullTarget) (PullRead, error),
) (Snapshot, []Outcome, error) {
	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, nil, err
	}

	defer unlock()

	row, err := s.targetRow(ctx, id, item.KindPR, false)
	if err != nil {
		return Snapshot{}, nil, err
	}

	target := pullTargetOfRow(row)

	var (
		seq    uint64
		viewer string
	)

	got, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (PullRead, error) {
		seq = s.startFetch() // each try numbers its own fetch
		viewer, _ = s.viewers.Get(tok)

		return read(ctx, tok, target)
	})
	if err != nil {
		return Snapshot{}, nil, err
	}

	if err = checkAnswer(got.Update, row); err != nil {
		return Snapshot{}, nil, err
	}

	files, err := s.readFiles(ctx, id, got)
	if err != nil {
		return Snapshot{}, nil, err
	}

	return s.storeRead(ctx, row, itemWrite{id: id, seq: seq, update: &itemUpdate{
		accountID: row.AccountID, login: row.AccountLogin, viewer: viewer, update: got.Update, files: files,
	}})
}

// checkAnswer refuses a read's answer that is missing, or that is about another item than
// the row it is for, so it never rewrites one card with another item's.
func checkAnswer(update *github.ItemUpdate, row editRow) error {
	if update == nil {
		return ErrIncompleteFetch
	}

	if update.Found && (update.Item.NodeID != row.NodeID || update.Item.IsPR != (item.Kind(row.Kind) == item.KindPR)) {
		return fmt.Errorf("%w: the answer is about another item than %d", ErrIncompleteFetch, row.ID)
	}

	return nil
}

// storeRead stores a read's answer as w says, for the item whose row is row, and returns
// the new snapshot, and the outcomes to tell: a verdict of the user's the answer found
// newly submitted, and a merge or a close the answer it stored shows.
func (s *Service) storeRead(ctx context.Context, row editRow, w itemWrite) (Snapshot, []Outcome, error) {
	did, err := s.storeItem(ctx, w)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("save the item: %w", err)
	}

	if !did.stored {
		snap, snapErr := s.Snapshot(ctx)

		return snap, nil, snapErr
	}

	told := reviewedOutcome(did.reviewed, row.AccountLogin, refOfRow(row))
	told = append(told, seenClosed(w, row.AccountLogin, refOfRow(row), s.now())...)
	s.noteMergedRead(w, refOfRow(row))
	snap, err := s.changed(ctx)

	return snap, told, err
}

// ReadIssue runs read on the issue with id, with a token, and stores what it found, as
// ReadPull does: the card is rewritten from the answer, its projects included when the
// target asked for them, or the issue leaves the lists when it was closed or deleted since.
// It holds the item's lock throughout, so read may store what it keeps of the answer, such
// as the sheet's own cache, knowing no action on the issue runs meanwhile. It returns the
// new snapshot. An issue in no list is read too: its sheet may still be open on it. Once
// it has let go of the item, it tells the outcome sink of a close the answer it stored
// shows.
func (a *Actor) ReadIssue(
	ctx context.Context, id int64, read func(context.Context, github.Token, IssueTarget) (IssueRead, error),
) (Snapshot, error) {
	snap, told, err := a.s.readIssue(ctx, id, read)
	a.s.settle(ctx, told)

	if err == nil {
		a.s.tellChanged([]int64{id})
	}

	return snap, err
}

// readIssue is ReadIssue under the item's lock. It returns what to tell once the lock is
// let go.
func (s *Service) readIssue(
	ctx context.Context, id int64, read func(context.Context, github.Token, IssueTarget) (IssueRead, error),
) (Snapshot, []Outcome, error) {
	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, nil, err
	}

	defer unlock()

	row, err := s.targetRow(ctx, id, item.KindIssue, false)
	if err != nil {
		return Snapshot{}, nil, err
	}

	last, err := s.projectAccess(ctx)
	if err != nil {
		return Snapshot{}, nil, err
	}

	target := issueTargetOfRow(row, readOptions(last).Projects)

	var seq uint64

	// The account is checked first: another account's token may not see the issue, and
	// its answer would take the issue out of the lists.
	got, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (IssueRead, error) {
		if checkErr := s.checkViewer(ctx, tok, row.AccountLogin); checkErr != nil {
			return IssueRead{}, checkErr
		}

		seq = s.startFetch() // each try numbers its own fetch

		return read(ctx, tok, target)
	})
	if err != nil {
		return Snapshot{}, nil, err
	}

	if err = checkAnswer(got.Update, row); err != nil {
		return Snapshot{}, nil, err
	}

	return s.storeRead(ctx, row, itemWrite{
		id: id, seq: seq, update: &itemUpdate{accountID: row.AccountID, update: got.Update, files: map[string]fileCheck{}},
	})
}

// PeekIssue runs peek on the issue with id, with a token, and stores nothing: a read the
// card has no part in, such as an earlier page of the issue's timeline. It holds the item's
// lock, as every other work on the issue does.
func (a *Actor) PeekIssue(
	ctx context.Context, id int64, peek func(context.Context, github.Token, IssueTarget) error,
) error {
	s := a.s

	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return err
	}

	defer unlock()

	row, err := s.targetRow(ctx, id, item.KindIssue, false)
	if err != nil {
		return err
	}

	target := issueTargetOfRow(row, false)

	_, err = github.WithToken(ctx, s.tokens, func(tok github.Token) (struct{}, error) {
		return struct{}{}, peek(ctx, tok, target)
	})

	return err
}

// PeekPull runs peek on the pull request with id, with a token, and stores nothing: a read
// the card has no part in, such as an earlier page of its conversation. It holds the item's
// lock, as every other work on the pull request does.
func (a *Actor) PeekPull(
	ctx context.Context, id int64, peek func(context.Context, github.Token, PullTarget) error,
) error {
	s := a.s

	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return err
	}

	defer unlock()

	row, err := s.targetRow(ctx, id, item.KindPR, false)
	if err != nil {
		return err
	}

	target := pullTargetOfRow(row)

	_, err = github.WithToken(ctx, s.tokens, func(tok github.Token) (struct{}, error) {
		return struct{}{}, peek(ctx, tok, target)
	})

	return err
}

// issueTargetOfRow converts a GetEditItem row of an issue; projects says to read its
// projects.
func issueTargetOfRow(row editRow, projects bool) IssueTarget {
	return IssueTarget{
		ID: row.ID, Ref: refOfRow(row), NodeID: row.NodeID, Login: row.AccountLogin,
		UpdatedAt: row.ItemUpdatedAt.UTC(), Assigned: row.ReasonAssigned, Projects: projects,
	}
}

// refOfRow names the item of a GetEditItem row.
func refOfRow(row editRow) item.Ref {
	return refOf(row.RepoOwner, row.RepoName, row.Number, item.Kind(row.Kind))
}

// targetRow reads the item with id, checking it belongs to the account whose inbox is
// shown, that it is of kind, and, when listed is set, that it is in one of the lists. An
// item of the other kind is refused with that kind's sentinel: ErrNotPull for a pull
// request's work, ErrNotIssue for an issue's.
func (s *Service) targetRow(ctx context.Context, id int64, kind item.Kind, listed bool) (editRow, error) {
	row, err := s.shownItem(ctx, id)
	if err != nil {
		return editRow{}, err
	}

	notKind := ErrNotPull
	if kind == item.KindIssue {
		notKind = ErrNotIssue
	}

	switch {
	case item.Kind(row.Kind) != kind || row.NodeID == "":
		return editRow{}, fmt.Errorf("%w (id %d)", notKind, id)
	case listed && !inTheInbox(row):
		return editRow{}, uierr.New(fmt.Sprintf("%s is no longer in your inbox.", refOfRow(row)),
			fmt.Errorf("%w (id %d)", notKind, id))
	default:
		return row, nil
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

// inTheInbox reports a row in one of the lists, or one a routine's search watches.
func inTheInbox(row editRow) bool {
	return row.ReasonReviewRequested || row.ReasonAuthored || row.ReasonAssigned || row.ReasonPushed || row.Watched
}

// pullTargetOfRow converts a GetEditItem row of a pull request.
func pullTargetOfRow(row editRow) PullTarget {
	return PullTarget{
		ID:     row.ID,
		Ref:    refOfRow(row),
		NodeID: row.NodeID, Login: row.AccountLogin, Head: store.Deref(row.HeadOID), UpdatedAt: row.ItemUpdatedAt.UTC(),
		ReviewRequested: row.ReasonReviewRequested || row.ReasonPushed, Authored: row.ReasonAuthored,
		Assigned: row.ReasonAssigned,
	}
}

// recordActed records what an action made happen on the item row is: a review takes it
// out of the Review requested list, stamped as an edit is (see UpsertItem), whether it
// asked or was pushed to since the user requested changes, and one that approves or
// requests changes is the user's verdict at the head they saw (see recordReview); the
// user's review requested again puts it back in Review requested (see recordRerequest); a
// merge or a close takes it out of every list, as an item write numbered now, after
// GitHub's answer, so a refresh that was fetching applies it again. A merge has the
// repository's other open pull requests on its base read again (see noteMerged).
func (s *Service) recordActed(ctx context.Context, row editRow, acted Acted) error {
	target := pullTargetOfRow(row)

	if acted.Reviewed {
		if err := s.recordReview(ctx, target, acted); err != nil {
			return fmt.Errorf("record the review: %w", err)
		}
	}

	if acted.Rerequested {
		if err := s.recordRerequest(ctx, target, acted); err != nil {
			return fmt.Errorf("record the review request: %w", err)
		}
	}

	return s.recordEnded(ctx, row, acted)
}

// recordRerequest records the user's review requested again on target, in one
// transaction: who the pull request asks now, as GitHub answered, and the pull request back
// in Review requested, as a request that arrived at GitHub's time of the edit, its left
// stamp cleared (RejoinReview), at once, before the next whole refresh's search catches up.
// Its pushed-since flag stays, so it still reads as pushed since the user's review, and,
// listed before, it tells no arrival. A refresh whose search hasn't caught up yet only
// lists it as pushed to since again, until the next one.
func (s *Service) recordRerequest(ctx context.Context, target PullTarget, acted Acted) error {
	stamp := later(acted.RerequestedAt.UTC(), target.UpdatedAt)

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if err := setReviewRequests(ctx, q, target.ID, acted.ReviewRequests); err != nil {
			return err
		}

		return q.RejoinReview(ctx, queries.RejoinReviewParams{ReviewRequestedAt: &stamp, ID: target.ID})
	})
}

// recordEnded takes the item row is out of every list when acted merged or closed it, as
// recordActed says, and has a merge followed (see noteMerged).
func (s *Service) recordEnded(ctx context.Context, row editRow, acted Acted) error {
	if !acted.Merged && !acted.Closed {
		return nil
	}

	state := github.StateClosed
	if acted.Merged {
		state = github.StateMerged
	}

	w := itemWrite{id: row.ID, seq: s.startFetch(), unlist: true, state: state}
	if _, err := s.storeItemWrite(ctx, w); err != nil {
		return fmt.Errorf("record the merge or close: %w", err)
	}

	if acted.Merged {
		s.noteMerged(row.NodeID, row.AccountID, refOfRow(row).Repo, store.Deref(row.BaseRef))
	}

	return nil
}

// recordReview records a review acted left on target, in one transaction: out of Review
// requested when it was in it, and, when it approves or requests changes, the user's
// verdict at the head they saw (NoteViewerReview), so the card stops saying it was pushed
// to since at once, before the next whole refresh reads the verdict. The verdict is stamped
// with when GitHub says it was submitted (now when it didn't say), as the newest seen too,
// so a later read of the pull request tells it no more: pullOutcomes tells it.
func (s *Service) recordReview(ctx context.Context, target PullTarget, acted Acted) error {
	stamp := later(acted.ReviewedAt.UTC(), target.UpdatedAt)

	submitted := acted.ReviewedAt.UTC().Truncate(time.Second)
	if acted.ReviewedAt.IsZero() {
		submitted = s.now().Truncate(time.Second)
	}

	return store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		if target.ReviewRequested {
			if err := q.LeaveReview(ctx, queries.LeaveReviewParams{ReviewLeftAt: &stamp, ID: target.ID}); err != nil {
				return err
			}
		}

		if state := verdictOf(acted.ReviewEvent); state != "" {
			return q.NoteViewerReview(ctx, queries.NoteViewerReviewParams{
				ViewerReviewState: state, ViewerReviewCommit: target.Head, ReviewedAt: &submitted,
				SeenAt: &submitted, ID: target.ID,
			})
		}

		return nil
	})
}

// verdictOf is the verdict a review's event leaves: APPROVED or CHANGES_REQUESTED, or ""
// for a review that only comments.
func verdictOf(event string) string {
	switch event {
	case github.ReviewApprove:
		return github.ReviewStateApproved
	case github.ReviewRequestChanges:
		return github.ReviewStateChangesRequested
	default:
		return ""
	}
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
