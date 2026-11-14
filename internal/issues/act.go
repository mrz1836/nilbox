package issues

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/timeline"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	// ErrNotOffered is returned for an action the sheet does not offer for the issue now;
	// nothing is sent.
	ErrNotOffered = errors.New("the action isn't available for the issue now")

	// ErrNewActivity is a close stopped, after its comment was posted, by a comment someone
	// else wrote since the user opened the issue.
	ErrNewActivity = errors.New("someone commented since the user opened the issue")
)

// Act runs the action the user asked for on an issue, and returns what it came to. It runs
// only what the sheet offers now, decided again on what the sheet read. A close is pinned
// to the comments the user saw (the earlier of what the request says and what the read
// holds): every try first reads the issue's state and its newest comments, and closes
// nothing over a comment someone else wrote since (OutcomeNewActivity, with nothing sent)
// nor an issue someone closed first (OutcomeAlreadyClosed, which leaves the inbox); then it
// posts the user's text as a comment, once, and closes. A step GitHub made stands: once a
// close's comment is posted, nothing that fails after it is an error, but
// OutcomeCommentedNotClosed, so the sheet drops the text and never posts it twice.
func (s *Service) Act(ctx context.Context, req ActRequest) (Result, error) {
	card, c, err := s.load(ctx, req.ItemID)
	if err != nil {
		return Result{}, err
	}

	run, err := prepare(req, Decide(factsOf(card, c, false)))
	if err != nil {
		return Result{}, err
	}

	run.ref, run.nodeID, run.itemID, run.viewer, run.pin = card.ref(), card.NodeID, card.ID, card.Viewer, pinOf(req.Seen, c)
	run.step(s, "Checking your account")

	err = s.inbox.Act(ctx, req.ItemID, func(ctx context.Context, tok github.Token, _ Target) (Acted, error) {
		actErr := run.do(ctx, s, tok)
		s.settle(ctx, req.ItemID, run.acted.Closed)

		return run.acted, actErr
	})

	if err != nil && run.outcome == "" {
		return Result{}, err
	}

	if err != nil {
		// GitHub answered a step: the sheet must hear what it made, whatever failed after.
		run.failedAfterStep(ctx, s, err)
	}

	return run.result(), nil
}

// settle brings the sheet's cache in line with an action that may have reached GitHub,
// under the item's lock, whatever it came to: the cache of an issue closed, by the action
// or before it, is dropped, and any other's view marked out of date, since the action may
// have changed what it says, so the sheet opened again reads it rather than show the
// conversation as it was.
func (s *Service) settle(ctx context.Context, id int64, closed bool) {
	update := s.cache.expire
	if closed {
		update = s.cache.drop
	}

	if err := update(ctx, id); err != nil {
		s.logger.DebugContext(ctx, "could not update the cached issue after an action", "error", err)
	}
}

// prepare checks a request against the plan before anything is sent: the action is
// offered and enabled now, a comment has something to say, and the text a close posts
// first is something GitHub can take now.
func prepare(req ActRequest, plan Plan) (*action, error) {
	offer, ok := plan.offer(req.Action)
	text := textutil.HasText(req.Body)

	switch {
	case !ok:
		return nil, uierr.New("That isn't something the sheet can do with this issue.",
			fmt.Errorf("%w: %q", ErrNotOffered, req.Action))
	case !offer.Enabled:
		return nil, uierr.New(offer.Reason, fmt.Errorf("%w: %q: %s", ErrNotOffered, req.Action, offer.Reason))
	case req.Action == ActionComment && !text:
		return nil, uierr.New("Write something first.", fmt.Errorf("%w: %q needs text", ErrNotOffered, req.Action))
	case text && offer.CommentBlocked != "":
		return nil, uierr.New(offer.CommentBlocked,
			fmt.Errorf("%w: a comment before %q: %s", ErrNotOffered, req.Action, offer.CommentBlocked))
	default:
		return &action{kind: req.Action, body: req.Body, post: text}, nil
	}
}

// pinOf is the comments a close is pinned to: the earlier of asked, what the request says
// the user saw, and what the sheet's read holds, so a read that landed after the user
// looked never moves the pin past a comment they didn't see.
func pinOf(asked Seen, c *cached) Seen {
	if c == nil {
		return asked
	}

	read := Seen{Comments: c.view.Comments, Latest: c.view.Latest, LatestAt: c.view.LatestAt}
	askedAt, saw := parseStamp(asked.LatestAt)
	readAt, held := parseStamp(read.LatestAt)

	switch {
	case !saw:
		return asked // the user saw no comment at all
	case !held || readAt.Before(askedAt):
		return read
	default:
		return asked
	}
}

// newComment reports a comment someone other than viewer wrote after pin, and who wrote it,
// "" when the read can't tell. state holds the newest comments, oldest first: when every
// one of them is newer than pin and there are more, one before them may be too.
func newComment(pin Seen, state *github.IssueState, viewer string) (string, bool) {
	at, saw := parseStamp(pin.LatestAt)

	for i := len(state.Recent) - 1; i >= 0; i-- {
		c := &state.Recent[i]

		switch {
		case saw && (c.ID == pin.Latest || c.CreatedAt.Before(at)):
			return "", false // this comment, and every one before it, was there when the user looked
		case !c.Mine && !isViewer(c.Author, viewer):
			return cmp.Or(c.Author, "Someone"), true
		}
	}

	return "", state.Comments > len(state.Recent)
}

// action is one action as it runs: what was asked for, and what GitHub made happen so far,
// kept across a retry with a fresh token, so no step is ever made twice.
type action struct {
	kind Action

	// body is the user's text, and post reports that it says something: a comment's text,
	// or the text a close posts first.
	body string
	post bool

	// pin is the comments the user saw, which a close goes by.
	pin Seen

	ref    item.Ref
	nodeID string
	itemID int64
	viewer string

	// What GitHub made happen, and what it came to. by is who wrote the comment that
	// stopped a close ("" when the read can't tell), and closedAs what an issue someone
	// closed first is closed as.
	commented bool
	acted     Acted
	outcome   Outcome
	reason    string
	by        string
	closedAs  timeline.StateReason
	steps     []string
}

// step tells the window what the action is doing now.
func (a *action) step(s *Service, doing string) {
	s.events.Emit(event.IssueProgress{ItemID: a.itemID, Step: doing})
}

// do runs the action with tok, from where an earlier try left off.
func (a *action) do(ctx context.Context, s *Service, tok github.Token) error {
	if a.kind.closes() {
		return a.close(ctx, s, tok)
	}

	return a.comment(ctx, s, tok, "Commenting")
}

// comment posts the user's text as a comment, once, saying doing as it does.
func (a *action) comment(ctx context.Context, s *Service, tok github.Token, doing string) error {
	if a.commented {
		return nil
	}

	a.step(s, doing)

	if _, err := s.github.AddComment(ctx, tok, a.ref, a.body); err != nil {
		return err
	}

	a.commented, a.outcome = true, OutcomeCommented
	a.steps = append(a.steps, "Commented")

	return nil
}

// close closes the issue as the action says. GitHub takes nothing to pin a close to, so
// every try, a retry with a fresh token included, first reads the issue's state and its
// newest comments (check). The comment, if any, goes next, once, and the close last. Any
// failure is returned as it is: the inbox's Actor tries a refused token again, and once the
// comment is posted, failedAfterStep makes whatever failed after it the result.
func (a *action) close(ctx context.Context, s *Service, tok github.Token) error {
	a.step(s, "Checking for new comments since you opened it")

	state, err := s.github.FetchIssueState(ctx, tok, a.nodeID)
	if err != nil {
		return err
	}

	if stop, checkErr := a.check(state); stop || checkErr != nil {
		return checkErr
	}

	if a.post {
		if err = a.comment(ctx, s, tok, "Posting your comment"); err != nil {
			return err
		}
	}

	words := closedWords(a.kind)
	a.step(s, "Closing as "+words)

	closed, err := s.github.CloseIssue(ctx, tok, a.ref, restReason(a.kind))
	if err != nil {
		return err
	}

	a.acted.Closed = true

	if closedFirst(closed, a.kind, a.viewer) {
		a.outcome, a.closedAs = OutcomeAlreadyClosed, timeline.StateReason(strings.ToUpper(closed.Reason))

		return nil
	}

	a.outcome, a.acted.Own = OutcomeClosed, true
	a.steps = append(a.steps, "Closed as "+words)

	return nil
}

// check goes by the issue's state, read before a close: stop reports that the close must
// not go on, because someone closed the issue first (OutcomeAlreadyClosed) or, while
// nothing was posted, someone else commented since the pin (OutcomeNewActivity). The error
// is for a close that can't go at all, or, once the comment is posted, for a comment
// someone else wrote since.
func (a *action) check(state *github.IssueState) (bool, error) {
	switch {
	case !state.Found:
		return true, uierr.New("GitHub can't find the issue any more, or your account can no longer see it.",
			fmt.Errorf("%w: close %s", github.ErrIssueGone, a.ref))
	case state.State != github.StateOpen:
		a.acted.Closed, a.outcome, a.closedAs = true, OutcomeAlreadyClosed, timeline.StateReason(state.StateReason)

		return true, nil
	case !state.CanClose:
		return true, uierr.New("GitHub doesn't let your account close this issue.",
			fmt.Errorf("%w: close %s: GitHub says the viewer can't", ErrNotOffered, a.ref))
	}

	by, ok := newComment(a.pin, state, a.viewer)

	switch {
	case !ok:
		return false, nil
	case a.commented:
		// The comment went on an earlier try and stands; the close doesn't go.
		return true, uierr.New(newCommentSentence(by)+".", fmt.Errorf("close %s after the comment: %w", a.ref, ErrNewActivity))
	default:
		a.outcome, a.by, a.reason = OutcomeNewActivity, by, newCommentSentence(by)

		return true, nil
	}
}

// closedFirst reports a close GitHub answered as someone else's: one it says another
// account closed, or whose reason isn't the one asked, since GitHub keeps the reason of the
// close that came first.
func closedFirst(closed *github.IssueClosed, kind Action, viewer string) bool {
	return (closed.ClosedBy != "" && !isViewer(closed.ClosedBy, viewer)) ||
		(closed.Reason != "" && !strings.EqualFold(closed.Reason, restReason(kind)))
}

// failedAfterStep turns an error that came after a step GitHub made into the action's
// result, so the sheet hears what GitHub made, and never posts the text twice. A close
// whose comment GitHub took and that closed nothing is OutcomeCommentedNotClosed, with the
// error's words as its reason. Anything else GitHub answered (a comment that was all the
// action asked for, a close made or found made, a close stopped by a new comment) stands,
// and the error, which then came from the inbox's own bookkeeping, is only logged.
func (a *action) failedAfterStep(ctx context.Context, s *Service, err error) {
	if !a.kind.closes() || a.outcome != OutcomeCommented {
		s.logger.WarnContext(ctx, "an action's step failed after GitHub answered it", "outcome", a.outcome, "error", err)

		return
	}

	a.outcome, a.reason = OutcomeCommentedNotClosed, strings.TrimSuffix(uierr.Message(err), ".")
}

// restReason is the reason a close asks GitHub's REST API for.
func restReason(kind Action) string {
	if kind == ActionCloseNotPlanned {
		return github.IssueReasonNotPlanned
	}

	return github.IssueReasonCompleted
}

// closedWords says what a close closes the issue as.
func closedWords(kind Action) string {
	if kind == ActionCloseNotPlanned {
		return "not planned"
	}

	return "completed"
}

// newCommentSentence says who commented since the user opened the issue, without a full
// stop.
func newCommentSentence(by string) string {
	if by == "" {
		return "New comments came in since you opened it"
	}

	return by + " commented since you opened it"
}
