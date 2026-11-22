package autopilot

import (
	"context"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/trust"
)

// ReviewVerdict is what a read-only agent's review says of a pull request.
type ReviewVerdict string

// The verdicts of an agent's review, as its report spells them.
const (
	ReviewApprove        ReviewVerdict = "approve"
	ReviewRequestChanges ReviewVerdict = "request_changes"
	ReviewComment        ReviewVerdict = "comment"
)

// AgentReview is the review a read-only agent's report held: its verdict, its text, and its
// line comments, which Post the agent's review posts.
type AgentReview struct {
	Verdict  ReviewVerdict `json:"verdict"`
	Body     string        `json:"body"`
	Comments []ReviewLine  `json:"comments"`
}

// ReviewLine is one of a review's line comments: a line of a file's diff, or a range
// ending at it (StartLine), on a side ("LEFT", the base's; "RIGHT" or "", the head's).
type ReviewLine struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	StartLine int    `json:"startLine"`
	Side      string `json:"side"`
	Body      string `json:"body"`
}

// ReviewEvent is what Post the agent's review submits.
type ReviewEvent string

// The reviews Post the agent's review submits.
const (
	EventComment        ReviewEvent = "comment"
	EventRequestChanges ReviewEvent = "request_changes"
	EventApprove        ReviewEvent = "approve"
)

// ReviewSend is one review to post.
type ReviewSend struct {
	Item ItemRef

	// Head is the commit the agent read, which the review is pinned to.
	Head string

	Event    ReviewEvent
	Body     string
	Comments []ReviewLine

	// Accept is the warnings the user accepted, as GitHubSent.Words worded them.
	Accept string
}

// ReviewPost is a review a job posts: what is submitted, and, for an approval posted as a
// comment, why.
type ReviewPost struct {
	Event  ReviewEvent
	Review AgentReview
	Why    string
}

// PostAsComment is the user's choice, on an Ask first card, to post the agent's review as a
// comment whatever its verdict.
const PostAsComment = "comment"

// approveGuard is what decides whether a routine may approve a pull request: its author's
// trust, its file check, and whether a routine's run pushed to it.
type approveGuard struct {
	trusted, filesClean, pushedTo bool
}

// guardOf works out f's approve guard; pushedTo reports a routine's run that pushed to the
// item.
func guardOf(f facts.Facts, known, pushedTo bool) approveGuard {
	if !known {
		return approveGuard{pushedTo: pushedTo}
	}

	t := f.Value(catalog.FieldTrust)
	files := f.Value(catalog.FieldSensitive)

	return approveGuard{
		trusted:    t.Known() && (t.Text == string(trust.ClassMember) || t.Text == string(trust.ClassBot)),
		filesClean: files.Known() && !files.Partial && len(files.List) == 0,
		pushedTo:   pushedTo,
	}
}

// reviewEvent decides what Post the agent's review submits of rv, as st says: a comment,
// or its verdict, approving only when st allows it, the user didn't ask for a comment
// (postAs), and g holds; an approval held back posts as a comment, saying why. It is pure.
func reviewEvent(st spec.ReviewStep, rv AgentReview, g approveGuard, postAs string) (ReviewEvent, string) {
	if st.As != catalog.ReviewAsVerdict || postAs == PostAsComment {
		return EventComment, ""
	}

	switch rv.Verdict {
	case ReviewRequestChanges:
		return EventRequestChanges, ""
	case ReviewComment:
		return EventComment, ""
	case ReviewApprove:
	}

	switch {
	case !st.AllowApprove:
		return EventComment, "Posted as a comment: the routine doesn't allow approving."
	case !g.trusted:
		return EventComment, "Posted as a comment: a routine approves only a member's or a trusted bot's pull request."
	case !g.filesClean:
		return EventComment, "Posted as a comment: a routine approves only when every file was checked and none is sensitive."
	case g.pushedTo:
		return EventComment, "Posted as a comment: a routine never approves a pull request a routine pushed to."
	}

	return EventApprove, ""
}

// reviewOutcome is the Only if outcome a read-only agent's review gives its step.
func reviewOutcome(rv *AgentReview) catalog.Outcome {
	if rv == nil {
		return ""
	}

	switch rv.Verdict {
	case ReviewApprove:
		return catalog.OutcomeApprove
	case ReviewRequestChanges:
		return catalog.OutcomeRequestChanges
	case ReviewComment:
	}

	return ""
}

// reviewDispatcher runs Post the agent's review (catalog.DispatchReview).
type reviewDispatcher struct {
	actions GitHubActions
}

// ReadItem reads the item through its sheet, as the read right before the review.
func (d reviewDispatcher) ReadItem(ctx context.Context, job Job) error {
	return d.actions.Read(ctx, job.Item)
}

// Dispatch posts job's review.
func (d reviewDispatcher) Dispatch(ctx context.Context, job Job) (Result, error) {
	post := job.Review
	if post == nil || job.Head == "" {
		return Result{NotOffered: "The agent wrote no review to post."}, nil
	}

	got, err := d.actions.PostReview(ctx, ReviewSend{
		Item: job.Item, Head: job.Head, Event: post.Event, Body: post.Review.Body, Comments: post.Review.Comments,
		Accept: job.Accept,
	})
	if err != nil {
		return Result{}, err
	}

	res, err := resultOf(got)
	if err != nil || !res.Sent {
		return res, err
	}

	res.Summary = joinWords(res.Summary, post.Why)

	// The step records which review went, as the agent's own verdict names it.
	switch post.Event {
	case EventApprove:
		res.Outcome = catalog.OutcomeApprove
	case EventRequestChanges:
		res.Outcome = catalog.OutcomeRequestChanges
	case EventComment:
	}

	return res, nil
}

// joinWords joins two sentences, either of which may be "".
func joinWords(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}

	return a + " " + b
}

// agentReviewOf returns the review the agent step before step wrote, and the commit it read
// (its run's head before), nil when it wrote none.
func (p *pass) agentReviewOf(ctx context.Context, m *match, step int) (*AgentReview, string) {
	for i := min(step, len(m.steps)) - 1; i >= 0; i-- {
		sr := m.steps[i]
		if sr.Step.Agent == nil {
			continue
		}

		if sr.Review == nil || sr.RunID == 0 {
			return nil, ""
		}

		run, err := p.e.ledger.q.GetRun(ctx, sr.RunID)
		if err != nil {
			return nil, ""
		}

		return sr.Review, firstOf(run.HeadBefore, run.HeadAfter)
	}

	return nil, ""
}

// followUp fills in job for a step that follows a run, as the cycle starts it, and returns
// the commit it is pinned to: for Merge it, the commit its checks passed on, whether it
// turns auto-merge back on, and whether r, its routine, lets it past the rules where an
// approval is all it lacks (pastReview); for Post the agent's review, the commit the agent
// read and the review it posts, as the step and the guards on approving decide. It reports
// false for any other step.
func (p *pass) followUp(ctx context.Context, r *routineRun, m *match, sr *stepRun, job *Job) (string, bool) {
	switch {
	case sr.Step.Merge != nil:
		job.Head, job.AutoMerge = mergeHead(m, int(m.Step)), autoMergeBack(m, sr.Step.Merge.How)
		job.PastReview, job.Sensitive, job.PastReviewWhy = p.pastReview(r, m, sr)
		job.PastRulesOff = job.PastReviewWhy == PastRulesOffWords

		return job.Head, true
	case sr.Step.PostReview != nil:
		rv, head := p.agentReviewOf(ctx, m, int(m.Step))
		if rv == nil {
			return "", false
		}

		id := store.Deref(m.ItemID)
		f, known := p.facts[id]
		event, why := reviewEvent(*sr.Step.PostReview, *rv, guardOf(f, known, p.pushedTo(id)), sr.PostAs)
		job.Head, job.Review = head, &ReviewPost{Event: event, Review: *rv, Why: why}

		return head, true
	}

	return "", false
}

// noReview skips a Post the agent's review whose agent wrote no review, and reports whether
// it did: there is nothing to post.
func (p *pass) noReview(ctx context.Context, m *match, sr *stepRun) bool {
	if sr.Step.PostReview == nil {
		return false
	}

	if rv, _ := p.agentReviewOf(ctx, m, int(m.Step)); rv != nil {
		return false
	}

	sr.Status, sr.Note = stepSkipped, "The agent wrote no review to post."

	return true
}

// pushedTo reports an item a routine's run pushed to: one that recorded a commit.
func (p *pass) pushedTo(id int64) bool {
	for _, r := range p.runs[id] {
		if r.RunCommit != "" {
			return true
		}
	}

	return false
}
