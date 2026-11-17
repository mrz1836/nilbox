package autopilot

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The GitHub step sends the app's own writes, as D5 allows them: ask Dependabot to rebase,
// update the branch, comment one of the user's comment replies, add or remove labels, and,
// as a local step of its own, mark the item's GitHub notifications done. Each goes
// through the review or issue sheet's own service, which core adapts (GitHubActions),
// never the client: the sheet reads the item right before the action starts, as the
// engine's read before a start (the cycle after it decides the match again on what it
// stored), its own Decide must still offer the action, enabled and with no warning the
// user didn't accept, and it is pinned to the head that read found, so a push since sends
// nothing. Every action is its own run, recorded before it is sent, so a quit never sends
// one twice; the write gate paces them, and Autopilot takes at most a third of its edit
// lane (ActionsPerMinute, ActionsPerHour). A merge, an approval, a close, and approving
// workflow runs are none of them, and nothing here can name one.

// errNoAction is a GitHub job with no action to send.
var errNoAction = errors.New("the step has no such GitHub action")

// GitHubActions is what the GitHub step goes through: the review and issue sheets' own
// services, and the Notifications page's, which core adapts. Every method runs on a
// worker and may reach GitHub.
type GitHubActions interface {
	// Read reads the item again through its sheet (ReadAgain, one point), which rewrites
	// the inbox's card from the same answer: the read right before an action starts.
	Read(ctx context.Context, it ItemRef) error

	// Send sends one action to the item as its sheet would, decided again on the sheet's
	// last read and pinned to req.Head (a pull request's; "" for an issue), and says how it
	// went. An error is a failure after which GitHub may have taken the action, which is
	// never sent again on its own.
	Send(ctx context.Context, req GitHubSend) (GitHubSent, error)

	// MarkDone marks the item's GitHub notifications done (or read, as the user's rules
	// clear them) through the chore lane, as the Notifications page's Done does, and
	// returns how many it queued.
	MarkDone(ctx context.Context, it ItemRef) (int, error)

	// Preview says what an action would send to the item, built and not sent: the exact
	// comment, or the labels.
	Preview(ctx context.Context, it ItemRef, a spec.GitHubAction) (string, error)

	// Merge merges a pull request, or turns its auto-merge back on, through the review
	// sheet, only as its Decide, on the read right before, offers it and only at req.Head:
	// never past the rules, never one an agent's commits changed workflow, agent, or tooling
	// files in, and one GitHub holds for an approving review waits (GitHubNeedsReview).
	Merge(ctx context.Context, req MergeSend) (GitHubSent, error)

	// PostReview posts a review of a pull request through the review sheet, at req.Head,
	// with only the line comments its diff shows.
	PostReview(ctx context.Context, req ReviewSend) (GitHubSent, error)
}

// GitHubSend is one GitHub action to send.
type GitHubSend struct {
	Item ItemRef
	Head string

	Action spec.GitHubAction

	// Accept is the warnings the user accepted, as GitHubSent.Words worded them; "" for
	// none. An offer with other warnings waits for the user again.
	Accept string
}

// GitHubOutcome is how a GitHub action went.
type GitHubOutcome string

// The ways a GitHub action goes.
const (
	// GitHubSentIt is an action GitHub took; Words says what it did.
	GitHubSentIt GitHubOutcome = "sent"

	// GitHubNothing is an action with nothing to change, such as labels the item has
	// already: nothing was sent, and the step goes on.
	GitHubNothing GitHubOutcome = "nothing"

	// GitHubNotOffered is an action the item's sheet no longer offers; Words says why.
	GitHubNotOffered GitHubOutcome = "not_offered"

	// GitHubWarning is an action the sheet offers with warnings the user hasn't accepted;
	// Words holds them, which the user accepts by approving.
	GitHubWarning GitHubOutcome = "warning"

	// GitHubPushedSince is an action refused, with nothing sent, because the pull request
	// was pushed to since the head it was pinned to.
	GitHubPushedSince GitHubOutcome = "pushed_since"

	// GitHubUnread is an action its sheet couldn't read the item before, so nothing was
	// sent; Words says why. It is read and decided again, as one pushed to since is.
	GitHubUnread GitHubOutcome = "unread"

	// GitHubHeld is an action the write gate refused before anything was sent; Words says
	// why and RetryIn when the next may go.
	GitHubHeld GitHubOutcome = "held"

	// GitHubNeedsReview is a merge GitHub holds for an approving review, which only a
	// person gives: nothing was sent, and it waits for the user.
	GitHubNeedsReview GitHubOutcome = "needs_review"
)

// GitHubSent is how a GitHub action went.
type GitHubSent struct {
	Outcome GitHubOutcome
	Words   string
	RetryIn time.Duration
}

// githubDispatcher runs the GitHub step's actions, one a job, and Mark the GitHub
// notification done (catalog.DispatchGitHub).
type githubDispatcher struct {
	actions GitHubActions
}

// NewGitHub returns the GitHub dispatcher, which sends the GitHub step's actions, one
// job each, and marks an item's GitHub notifications done, through actions.
func NewGitHub(actions GitHubActions) Dispatcher {
	return githubDispatcher{actions: actions}
}

// itemReader is a dispatcher that reads the item right before its step starts, in place
// of the inbox's own single-item read: the GitHub step's, through the item's sheet, whose
// read rewrites the inbox's card too, so the sheet's Decide works from facts as fresh as
// the cycle's, and the read costs one point, not two.
type itemReader interface {
	ReadItem(ctx context.Context, job Job) error
}

// ReadItem reads job's item through its sheet.
func (g githubDispatcher) ReadItem(ctx context.Context, job Job) error {
	return g.actions.Read(ctx, job.Item)
}

// Dispatch sends job's GitHub action, or marks its item's notifications done.
func (g githubDispatcher) Dispatch(ctx context.Context, job Job) (Result, error) {
	if job.Step.MarkDone != nil {
		return g.markDone(ctx, job)
	}

	if job.Action < 0 || job.Action >= len(job.Step.GitHub) {
		return Result{}, fmt.Errorf("%w: %d of %d", errNoAction, job.Action, len(job.Step.GitHub))
	}

	got, err := g.actions.Send(ctx, GitHubSend{
		Item: job.Item, Head: job.Head, Action: job.Step.GitHub[job.Action], Accept: job.Accept,
	})
	if err != nil {
		return Result{}, err
	}

	return resultOf(got)
}

// resultOf is how a GitHub action went, in the engine's words.
func resultOf(got GitHubSent) (Result, error) {
	switch got.Outcome {
	case GitHubSentIt:
		return Result{Sent: true, Summary: got.Words}, nil
	case GitHubNothing:
		return Result{Summary: got.Words}, nil
	case GitHubNotOffered:
		return Result{NotOffered: textutil.Sentence(cmp.Or(got.Words, "Its sheet no longer offers it."))}, nil
	case GitHubWarning:
		return Result{Warning: cmp.Or(got.Words, "It comes with a warning.")}, nil
	case GitHubPushedSince:
		return Result{PushedSince: true, Summary: cmp.Or(got.Words, pushedSinceWords)}, nil
	case GitHubUnread:
		return Result{PushedSince: true, Summary: cmp.Or(got.Words, "It couldn't be read, so nothing was sent.")}, nil
	case GitHubHeld:
		return Result{Held: cmp.Or(got.Words, "GitHub actions are held for now."), RetryIn: got.RetryIn}, nil
	case GitHubNeedsReview:
		return Result{
			Wait: BlockReviewRequired,
			WaitWords: cmp.Or(got.Words, "GitHub wants an approving review before it merges this: approve it, "+
				"then let the merge go."),
		}, nil
	}

	return Result{}, fmt.Errorf("%w: %q", errNoAction, got.Outcome)
}

// pushedSinceWords say a GitHub action found its pull request pushed to.
const pushedSinceWords = "It was pushed to since it was read, so nothing was sent: it's read again first."

// markDone marks job's item's GitHub notifications done.
func (g githubDispatcher) markDone(ctx context.Context, job Job) (Result, error) {
	n, err := g.actions.MarkDone(ctx, job.Item)
	if err != nil {
		return Result{}, err
	}

	if n == 0 {
		return Result{Summary: "It had no GitHub notification to mark done."}, nil
	}

	return Result{Summary: "Marked " + textutil.Counted(n, "GitHub notification", "GitHub notifications") + " done."}, nil
}

// ActionPreview is what one of a draft's GitHub actions would send to one item, built and
// not sent: its step, its action, and the exact comment or labels, or why it can't be
// built for the item.
type ActionPreview struct {
	// Step is the step's place among the draft's steps, from 0, and Kind its kind: a
	// GitHub step, or Mark the GitHub notification done.
	Step int              `json:"step"`
	Kind catalog.StepKind `json:"kind"`

	// Action is the GitHub step's action, "" for Mark the GitHub notification done, and
	// Label its name.
	Action catalog.GitHubActionKind `json:"action"`
	Label  string                   `json:"label"`

	// Sends is what it would send: the exact comment, or what it does to the labels.
	Sends string `json:"sends"`

	// Problem says why it can't be built for the item, "" when it can.
	Problem string `json:"problem"`
}

// ErrNoGitHubStep is returned for a preview of the GitHub actions of a draft that has
// none.
var ErrNoGitHubStep = errors.New("the routine sends no GitHub action")

// PreviewActions says what the draft d's GitHub actions would send to the item itemID,
// built and not sent: each GitHub step's actions in order, with the exact comment or what
// it does to the labels, and Mark the GitHub notification done, each marked with why it
// can't be built for the item when it can't (a reply the library no longer has, or that
// approves). The draft needn't be saved, but must have no problem. It sends nothing to
// GitHub, and the list is never nil.
func (s *Service) PreviewActions(ctx context.Context, d Draft, itemID int64) ([]ActionPreview, error) {
	out := []ActionPreview{}

	sp, problems, err := s.loadDraft(ctx, d.Spec)
	if err != nil {
		return out, err
	}

	if len(problems) > 0 {
		return out, uierr.New("Fix the routine's problems first: "+problems[0].Message, &ProblemsError{
			err: &spec.ValidationError{Problems: problems},
		})
	}

	it, err := s.previewItem(ctx, itemID)
	if err != nil {
		return out, err
	}

	for i, st := range sp.Steps {
		out = append(out, s.previewStep(ctx, i, st, it)...)
	}

	if len(out) == 0 {
		return out, uierr.New("This routine sends nothing to GitHub, so there's nothing to preview.", ErrNoGitHubStep)
	}

	return out, nil
}

// previewItem returns the stored item itemID as a match keeps a copy of it.
func (s *Service) previewItem(ctx context.Context, itemID int64) (ItemRef, error) {
	if s.items == nil || s.github == nil {
		return ItemRef{}, uierr.New("Preview isn't available here.", ErrNoGitHubStep)
	}

	got, err := s.items.Facts(ctx, []int64{itemID}, s.clock.Now())
	if err != nil {
		return ItemRef{}, uierr.New("The item couldn't be read from the inbox.", err)
	}

	if len(got) == 0 {
		return ItemRef{}, uierr.New("That item isn't in the inbox any more.", ErrNoMatch)
	}

	return itemRefOf(got[0].Item), nil
}

// previewStep says what step st, at index i, would send to it: each of a GitHub step's
// actions, or Mark the GitHub notification done; nothing for any other step.
func (s *Service) previewStep(ctx context.Context, i int, st spec.Step, it ItemRef) []ActionPreview {
	if st.MarkDone != nil {
		info, _ := catalog.StepByKind(catalog.StepMarkDone)

		return []ActionPreview{{
			Step: i, Kind: catalog.StepMarkDone, Label: info.Label,
			Sends: "Marks its GitHub notifications done, as your rules clear them.",
		}}
	}

	out := make([]ActionPreview, 0, len(st.GitHub))

	for _, a := range st.GitHub {
		p := ActionPreview{Step: i, Kind: catalog.StepGitHub, Action: a.Kind()}

		if info, ok := catalog.GitHubActionByKind(a.Kind()); ok {
			p.Label = info.Label
		}

		sends, err := s.github.Preview(ctx, it, a)
		if err != nil {
			p.Problem = uierr.Message(err)
		} else {
			p.Sends = sends
		}

		out = append(out, p)
	}

	return out
}

// ApproveBacklog gives the user's OK to every match of the routine id waiting as a
// backlog, over its first pass's cap (Activity's Run on them), which then start once the
// limits let them, and returns them as they are now. One that waits for anything else is
// left as it is.
func (s *Service) ApproveBacklog(ctx context.Context, id int64) ([]Match, error) {
	e, err := s.engineOrErr()
	if err != nil {
		return []Match{}, err
	}

	var ids []int64

	err = e.do(ctx, func(ctx context.Context) error {
		var approveErr error

		ids, approveErr = e.approveBacklog(ctx, id)
		e.Wake()

		return approveErr
	})
	if err != nil {
		return []Match{}, err
	}

	return s.matchesByID(ctx, ids)
}

// approveBacklog gives the OK to every match of the routine id waiting as a backlog, and
// returns them.
func (e *Engine) approveBacklog(ctx context.Context, id int64) ([]int64, error) {
	open, err := e.ledger.open(ctx)
	if err != nil {
		return nil, uierr.New("Autopilot's history couldn't be read.", err)
	}

	var ids []int64

	for _, m := range open {
		if m.RoutineID != id || m.state() != StateWaiting || Blocker(m.BlockedBy) != BlockBacklog {
			continue
		}

		if err = e.approve(ctx, m.ID); err != nil {
			return ids, err
		}

		ids = append(ids, m.ID)
	}

	return ids, nil
}
