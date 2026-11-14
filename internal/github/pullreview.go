package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// pullReviewQuery reads what the review sheet shows about one pull request, by node ID,
// in one request: what the item query reads (ItemFields and DetailFields), so the inbox
// card is rewritten from the same answer, and ReviewFields: the description, when it was
// last edited, and its reactions, the base branch, the merge state, what the viewer may do
// (close it included), the viewer's own latest review and pending reviews, auto-merge, each
// reviewer's latest review, the head commit's checks, each with whether it is required, its
// check suites, which name the workflow runs that ended without a check (as a fork's runs
// waiting for approval do) with each run's ID and page, the issues the pull request closes
// once it merges, and the newest page of its conversation: its comments, its reviews, each
// with its first line comments, and its events (PullTimelineFields).
//
// Fields DetailFields asks for with other arguments are aliased: GitHub refuses one name
// asked for twice with different arguments. The head commit's suites are asked for under
// DetailFields' own alias and arguments (headSuites), which GraphQL merges into one field,
// so they are read once whatever each fragment selects of them.
//
// GitHub prices the whole read at one point, the conversation's newest hundred items and
// each review's first twenty line comments included (measured in November 2026 on a pull
// request with reviews, line comments, force pushes, and review requests).
const pullReviewQuery = `query PullReview($id: ID!, $last: Int!) {
  node(id: $id) { ...ItemFields ...DetailFields ...ReviewFields }
  rateLimit { cost remaining resetAt limit used }
}
` + itemFieldsFragment + repoFieldsFragment + detailFieldsFragment + reviewFieldsFragment + pullTimelineFieldsFragment +
	refFieldsFragment + assigneeFieldsFragment

// reviewFieldsFragment is what the review sheet reads beyond an inbox card.
const reviewFieldsFragment = `
fragment ReviewFields on PullRequest {
  body lastEditedAt baseRefName mergeStateStatus isMergeQueueEnabled
  viewerDidAuthor viewerCanUpdateBranch viewerCanEnableAutoMerge viewerCanMergeAsAdmin viewerCanClose
  viewerLatestReview { state commit { oid } }
  pendingReviews: reviews(states: [PENDING]) { totalCount }
  autoMergeRequest { mergeMethod enabledAt enabledBy { login } }
  reviewBodies: latestReviews(first: 50) {
    nodes { state url submittedAt author { login } commit { oid } }
  }
  reactionGroups { content reactors { totalCount } }
  closes: closingIssuesReferences(first: 10) { totalCount nodes { ...RefFields } }
  checkList: statusCheckRollup {
    contexts(first: 100) {
      nodes {
        __typename
        ... on CheckRun {
          name conclusion status detailsUrl isRequired(pullRequestId: $id)
          checkSuite { id workflowRun { workflow { name } } }
        }
        ... on StatusContext { context state targetUrl isRequired(pullRequestId: $id) }
      }
    }
  }
  headSuites: commits(last: 1) {
    nodes {
      commit {
        checkSuites(first: 50) {
          nodes { id conclusion app { name } workflowRun { databaseId url workflow { name } } }
        }
      }
    }
  }
  timeline: timelineItems(last: $last, itemTypes: ` + pullTimelineItemTypes + `) { ...PullTimelineFields }
}
`

// mergeStateQuery reads, by node ID, what a merge must check first, as of now: the merge
// state, the head commit, whether the pull request is open, a draft, or merges through a
// queue, and whether the viewer may merge it past the rules. It costs one point.
const mergeStateQuery = `query MergeState($id: ID!) {
  node(id: $id) {
    __typename
    ... on PullRequest {
      mergeStateStatus headRefOid isDraft isMergeQueueEnabled prState: state viewerCanMergeAsAdmin
      autoMergeRequest { mergeMethod enabledAt enabledBy { login } }
    }
  }
  rateLimit { cost remaining resetAt limit used }
}
`

// A pull request's merge state, as GitHub's mergeStateStatus reports it.
const (
	// MergeStateClean can be merged now.
	MergeStateClean = "CLEAN"

	// MergeStateHasHooks can be merged now, and the repository runs pre-receive hooks.
	MergeStateHasHooks = "HAS_HOOKS"

	// MergeStateUnstable can be merged, though checks that are not required failed.
	MergeStateUnstable = "UNSTABLE"

	// MergeStateBlocked waits on a requirement: a review, a required check, or a rule.
	MergeStateBlocked = "BLOCKED"

	// MergeStateBehind is behind its base branch, which the repository requires it not be.
	MergeStateBehind = "BEHIND"

	// MergeStateDirty has conflicts with its base branch.
	MergeStateDirty = "DIRTY"

	// MergeStateUnknown is still being worked out by GitHub.
	MergeStateUnknown = "UNKNOWN"
)

// Check kinds: a GitHub Actions or app check run, or a commit status.
const (
	CheckKindRun    = "run"
	CheckKindStatus = "status"
)

// PullReview is what FetchPullReview read about one pull request.
type PullReview struct {
	// Update is the item and its card's details, as FetchItem reads them. When
	// Update.Found is false, GitHub could not resolve the ID and the rest is zero.
	Update ItemUpdate

	// Body is the description, as markdown, and LastEditedAt when it was last edited; zero
	// when it never was.
	Body         string
	LastEditedAt time.Time

	// Reactions counts the description's reactions, each kind with any.
	Reactions []Reaction

	// BaseRef is the branch the pull request merges into.
	BaseRef string

	// MergeState is one of the MergeState values.
	MergeState string

	// MergeQueue reports a base branch that merges only through its merge queue.
	MergeQueue bool

	// ViewerDidAuthor reports the viewer's own pull request.
	ViewerDidAuthor bool

	// ViewerCanUpdateBranch and ViewerCanEnableAutoMerge say whether the viewer may bring
	// the branch up to date with its base, and turn on auto-merge, as GitHub judges it now.
	ViewerCanUpdateBranch    bool
	ViewerCanEnableAutoMerge bool

	// ViewerCanBypass says whether GitHub lets the viewer merge past the branch's rules, as
	// a repository admin can, without waiting for what they require.
	ViewerCanBypass bool

	// ViewerCanClose says whether GitHub lets the viewer close the pull request (its
	// Closable viewerCanClose).
	ViewerCanClose bool

	// ViewerReview is the viewer's own latest review: its state and the commit it was left
	// on; zero when the viewer has not reviewed.
	ViewerReview ReviewMark

	// PendingReviews counts the viewer's reviews started on GitHub and not submitted.
	PendingReviews int

	// AutoMerge is the pull request's auto-merge, zero when it is off.
	AutoMerge AutoMerge

	// Reviews are each reviewer's latest review, in GitHub's order.
	Reviews []Review

	// Checks are the head commit's checks and statuses, the first 100.
	Checks []Check

	// Unrun are the head commit's check suites, of its first 50, that ended without any
	// check among Checks and without passing: a fork's workflow runs waiting for a
	// maintainer's approval, and runs that failed to start. GitHub counts them in the merge
	// state (UNSTABLE) though no check shows them.
	Unrun []UnrunSuite

	// Closes are the first ten issues the pull request closes once it merges, the ones its
	// description names and the ones linked to it by hand, and CloseCount how many there are
	// that the viewer can see.
	Closes     []ItemRef
	CloseCount int

	// Timeline is the newest page of its conversation.
	Timeline TimelinePage
}

// ReviewMark is a review's state and the commit it was left on.
type ReviewMark struct {
	// State is APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, or PENDING; "" for none.
	State string

	// Commit is the head commit the review was left on.
	Commit string
}

// Review is one reviewer's latest review; its text is in the conversation (Timeline).
type Review struct {
	Author string
	State  string
	URL    string
	Commit string

	// SubmittedAt is zero for a review not submitted.
	SubmittedAt time.Time
}

// Check is one check run or commit status on a pull request's head commit.
type Check struct {
	// Kind is CheckKindRun or CheckKindStatus.
	Kind string

	// Name is the check run's name, or the status's context.
	Name string

	// Workflow is the GitHub Actions workflow a check run belongs to, or "".
	Workflow string

	// State is a check run's conclusion once it completed, or else its status (QUEUED,
	// IN_PROGRESS, ...); a status's state (SUCCESS, FAILURE, ERROR, PENDING, EXPECTED).
	State string

	// Outcome is where the check is counted: CheckPassed, CheckFailed, CheckPending, or
	// CheckSkipped.
	Outcome string

	// URL is the check's page, "" when it has none.
	URL string

	// Required reports a check the base branch requires.
	Required bool
}

// UnrunSuite is a check suite that ended without a check: what ran it, and how it ended.
type UnrunSuite struct {
	// Name is the GitHub Actions workflow, or else the app, that ran it.
	Name string

	// Conclusion is ACTION_REQUIRED for a run waiting for approval, or how it otherwise
	// ended, a conclusion a check run with it fails on (checkRunOutcome), as FAILURE or
	// STARTUP_FAILURE.
	Conclusion string

	// RunID is the GitHub Actions workflow run's ID, which REST names it by (as
	// ApproveWorkflowRun does), and URL its page; 0 and "" for another app's suite.
	RunID int64
	URL   string
}

// SuiteAwaitsApproval is the conclusion of a suite whose run waits for a maintainer's
// approval, as a fork's first workflow runs do.
const SuiteAwaitsApproval = "ACTION_REQUIRED"

// Where a check is counted.
const (
	CheckPassed  = "passed"
	CheckFailed  = "failed"
	CheckPending = "pending"
	CheckSkipped = "skipped"
)

// MergeState is what FetchMergeState read about a pull request, as of now.
type MergeState struct {
	// Found is false when GitHub could not resolve the ID; the rest is zero then.
	Found bool

	// Status is one of the MergeState values.
	Status string

	// Head is the head commit's SHA.
	Head string

	// State is StateOpen, StateClosed, or StateMerged.
	State string

	IsDraft    bool
	MergeQueue bool
	AutoMerge  AutoMerge

	// CanBypass says whether GitHub lets the viewer merge past the branch's rules.
	CanBypass bool
}

// FetchPullReview reads what the review sheet shows about the pull request with node ID
// id, as of now, with one GraphQL request, and again with a smaller page of its
// conversation when the answer is larger than the client reads.
func (c *Client) FetchPullReview(ctx context.Context, tok Token, id string) (*PullReview, error) {
	review, err := c.fetchPullReview(ctx, tok, id)

	return review, Redact(err, tok)
}

// FetchMergeState reads the merge state of the pull request with node ID id, as of now,
// with one GraphQL request of one point. A merge reads it first, so it never merges what
// branch protection would block: GitHub's REST merge lets an admin through.
func (c *Client) FetchMergeState(ctx context.Context, tok Token, id string) (*MergeState, error) {
	state, err := c.fetchMergeState(ctx, tok, id)

	return state, Redact(err, tok)
}

// fetchPullReview sends the pull review query, with a smaller page of the conversation
// should the answer be too large (paged).
func (c *Client) fetchPullReview(ctx context.Context, tok Token, id string) (*PullReview, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	return paged(func(last int) (*PullReview, error) {
		return c.sendPullReview(ctx, tok, id, last)
	})
}

// sendPullReview sends the pull review query for the newest last items of the
// conversation, and decodes the answer.
func (c *Client) sendPullReview(ctx context.Context, tok Token, id string, last int) (*PullReview, error) {
	req := graphQLRequest{Query: pullReviewQuery, Variables: map[string]any{"id": id, "last": last}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "pull review", req, unlessData)
	if err != nil {
		return nil, err
	}

	update, err := itemUpdate(resp)
	if err != nil {
		return nil, err
	}

	review := &PullReview{Update: *update}
	if !update.Found {
		return review, nil
	}

	if !update.Item.IsPR {
		return nil, fmt.Errorf("%w: the ID is not a pull request", ErrMalformedResponse)
	}

	var fields reviewNode
	if err = json.Unmarshal(resp.Data.Node, &fields); err != nil {
		return nil, fmt.Errorf("%w: decode the pull review: %w", ErrMalformedResponse, err)
	}

	self := ItemRef{IsPR: true, Repo: update.Item.RepoOwner + "/" + update.Item.RepoName, Number: update.Item.Number}
	fields.fill(review, self, update.Details.HeadOID)
	review.Timeline.RateLimit = update.RateLimit

	return review, nil
}

// fetchMergeState sends the merge state query and decodes the answer.
func (c *Client) fetchMergeState(ctx context.Context, tok Token, id string) (*MergeState, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	req := graphQLRequest{Query: mergeStateQuery, Variables: map[string]any{"id": id}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "merge state", req, unlessData)
	if err != nil {
		return nil, err
	}

	if gone, goneErr := goneNode(resp); gone || goneErr != nil {
		if goneErr != nil {
			return nil, goneErr
		}

		return &MergeState{}, nil
	}

	var node struct {
		TypeName            string            `json:"__typename"`
		MergeStateStatus    string            `json:"mergeStateStatus"`
		HeadRefOID          string            `json:"headRefOid"`
		IsDraft             bool              `json:"isDraft"`
		IsMergeQueueEnabled bool              `json:"isMergeQueueEnabled"`
		PRState             string            `json:"prState"`
		CanMergeAsAdmin     bool              `json:"viewerCanMergeAsAdmin"`
		AutoMergeRequest    *graphQLAutoMerge `json:"autoMergeRequest"`
	}

	if err = json.Unmarshal(resp.Data.Node, &node); err != nil {
		return nil, fmt.Errorf("%w: decode the merge state: %w", ErrMalformedResponse, err)
	}

	if node.TypeName != typePullRequest || node.HeadRefOID == "" {
		return nil, fmt.Errorf("%w: the ID is not a pull request with a head", ErrMalformedResponse)
	}

	return &MergeState{
		Found: true, Status: node.MergeStateStatus, Head: node.HeadRefOID, State: node.PRState, IsDraft: node.IsDraft,
		MergeQueue: node.IsMergeQueueEnabled, AutoMerge: node.AutoMergeRequest.autoMerge(),
		CanBypass: node.CanMergeAsAdmin,
	}, nil
}

// reviewNode is the ReviewFields part of a pull review answer. GitHub's nulls decode to
// zero values.
type reviewNode struct {
	Body                     string    `json:"body"`
	LastEditedAt             time.Time `json:"lastEditedAt"`
	BaseRefName              string    `json:"baseRefName"`
	MergeStateStatus         string    `json:"mergeStateStatus"`
	IsMergeQueueEnabled      bool      `json:"isMergeQueueEnabled"`
	ViewerDidAuthor          bool      `json:"viewerDidAuthor"`
	ViewerCanUpdateBranch    bool      `json:"viewerCanUpdateBranch"`
	ViewerCanEnableAutoMerge bool      `json:"viewerCanEnableAutoMerge"`
	ViewerCanMergeAsAdmin    bool      `json:"viewerCanMergeAsAdmin"`
	ViewerCanClose           bool      `json:"viewerCanClose"`
	ViewerLatestReview       *struct {
		State  string         `json:"state"`
		Commit *graphQLCommit `json:"commit"`
	} `json:"viewerLatestReview"`
	PendingReviews   graphQLCount      `json:"pendingReviews"`
	AutoMergeRequest *graphQLAutoMerge `json:"autoMergeRequest"`
	ReviewBodies     struct {
		Nodes []*struct {
			State       string         `json:"state"`
			URL         string         `json:"url"`
			SubmittedAt time.Time      `json:"submittedAt"`
			Author      *graphQLActor  `json:"author"`
			Commit      *graphQLCommit `json:"commit"`
		} `json:"nodes"`
	} `json:"reviewBodies"`
	CheckList *struct {
		Contexts struct {
			Nodes []*graphQLCheck `json:"nodes"`
		} `json:"contexts"`
	} `json:"checkList"`
	HeadSuites graphQLHeadSuites `json:"headSuites"`
	Closes     struct {
		TotalCount int           `json:"totalCount"`
		Nodes      []*graphQLRef `json:"nodes"`
	} `json:"closes"`
	ReactionGroups []*graphQLReactionGroup `json:"reactionGroups"`
	Timeline       graphQLTimeline         `json:"timeline"`
}

// graphQLCommit is a commit asked only for its SHA.
type graphQLCommit struct {
	OID string `json:"oid"`
}

// oid returns the commit's SHA, "" for a nil commit.
func (c *graphQLCommit) oid() string {
	if c == nil {
		return ""
	}

	return c.OID
}

// graphQLCheck is one check run or commit status of a rollup. Which fields are set depends
// on the type; the others decode to zero values.
type graphQLCheck struct {
	TypeName   string `json:"__typename"`
	Name       string `json:"name"`
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
	DetailsURL string `json:"detailsUrl"`
	IsRequired bool   `json:"isRequired"`
	CheckSuite *struct {
		ID          string `json:"id"`
		WorkflowRun *struct {
			Workflow *struct {
				Name string `json:"name"`
			} `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
	Context   string `json:"context"`
	State     string `json:"state"`
	TargetURL string `json:"targetUrl"`
}

// graphQLSuite is one of the head commit's check suites. The details query asks only for
// its conclusion and its run's ID; the rest decode to zero values there.
type graphQLSuite struct {
	ID         string `json:"id"`
	Conclusion string `json:"conclusion"`
	App        *struct {
		Name string `json:"name"`
	} `json:"app"`
	WorkflowRun *struct {
		DatabaseID int64  `json:"databaseId"`
		URL        string `json:"url"`
		Workflow   *struct {
			Name string `json:"name"`
		} `json:"workflow"`
	} `json:"workflowRun"`
}

// runID is the suite's workflow run's ID, 0 for a nil suite or one without a run.
func (s *graphQLSuite) runID() int64 {
	if s == nil || s.WorkflowRun == nil {
		return 0
	}

	return max(s.WorkflowRun.DatabaseID, 0)
}

// heldRun reports a suite whose GitHub Actions workflow run waits for a maintainer's
// approval. An app's check run that ended ACTION_REQUIRED belongs to that app's suite,
// which has no workflow run, so it is never counted here as well as among failed checks.
func (s *graphQLSuite) heldRun() bool {
	return s != nil && s.Conclusion == SuiteAwaitsApproval && s.runID() > 0
}

// name is the workflow that ran the suite, or else its app, or "".
func (s *graphQLSuite) name() string {
	if w := s.WorkflowRun; w != nil && w.Workflow != nil && w.Workflow.Name != "" {
		return w.Workflow.Name
	}

	if s.App != nil {
		return s.App.Name
	}

	return ""
}

// unrun lists the head commit's check suites that ended without passing, a conclusion a
// check run with it fails on (a suite still queued has none, and one that passed, was
// skipped, or was neutral doesn't fail), and without a check among the rollup's contexts,
// skipping nulls. The suites are read from the head commit, not the rollup, so a fork's
// runs held for approval are listed while no check has run and the rollup is null.
func (n *reviewNode) unrun() []UnrunSuite {
	out := []UnrunSuite{}
	ran := n.suitesRan()

	for _, s := range n.HeadSuites.suites() {
		if s == nil || s.ID == "" || ran[s.ID] || checkRunOutcome(s.Conclusion) != outcomeFailed {
			continue
		}

		suite := UnrunSuite{Name: s.name(), Conclusion: s.Conclusion, RunID: s.runID()}
		if suite.RunID > 0 {
			suite.URL = s.WorkflowRun.URL
		}

		out = append(out, suite)
	}

	return out
}

// suitesRan holds the check suites of the rollup's check runs, by ID; none while the
// rollup is null.
func (n *reviewNode) suitesRan() map[string]bool {
	ran := map[string]bool{}
	if n.CheckList == nil {
		return ran
	}

	for _, c := range n.CheckList.Contexts.Nodes {
		if c != nil && c.CheckSuite != nil && c.CheckSuite.ID != "" {
			ran[c.CheckSuite.ID] = true
		}
	}

	return ran
}

// fill copies the review fields into r, telling the pull request self apart from the other
// side of a connection, and a review of an earlier commit than head.
func (n *reviewNode) fill(r *PullReview, self ItemRef, head string) {
	r.Body = n.Body
	r.LastEditedAt = n.LastEditedAt.UTC()
	r.Reactions = reactionsOf(n.ReactionGroups)
	r.Timeline = n.Timeline.page(self, head)
	r.BaseRef = n.BaseRefName
	r.MergeState = n.MergeStateStatus
	r.MergeQueue = n.IsMergeQueueEnabled
	r.ViewerDidAuthor = n.ViewerDidAuthor
	r.ViewerCanUpdateBranch = n.ViewerCanUpdateBranch
	r.ViewerCanEnableAutoMerge = n.ViewerCanEnableAutoMerge
	r.ViewerCanBypass = n.ViewerCanMergeAsAdmin
	r.ViewerCanClose = n.ViewerCanClose
	r.PendingReviews = max(n.PendingReviews.TotalCount, 0)
	r.AutoMerge = n.AutoMergeRequest.autoMerge()
	r.Reviews = n.reviews()
	r.Checks = n.checks()
	r.Unrun = n.unrun()
	r.Closes = itemRefs(n.Closes.Nodes)
	r.CloseCount = shownCount(n.Closes.TotalCount, len(n.Closes.Nodes), len(r.Closes))

	if v := n.ViewerLatestReview; v != nil {
		r.ViewerReview = ReviewMark{State: v.State, Commit: v.Commit.oid()}
	}
}

// reviews converts each reviewer's latest review, skipping nulls and reviews without an
// author.
func (n *reviewNode) reviews() []Review {
	out := make([]Review, 0, len(n.ReviewBodies.Nodes))

	for _, rev := range n.ReviewBodies.Nodes {
		if rev == nil || rev.Author == nil || rev.Author.Login == "" {
			continue
		}

		out = append(out, Review{
			Author: rev.Author.Login, State: rev.State, URL: rev.URL, Commit: rev.Commit.oid(),
			SubmittedAt: rev.SubmittedAt.UTC(),
		})
	}

	return out
}

// checks converts the head commit's checks, skipping nulls and other nodes.
func (n *reviewNode) checks() []Check {
	out := []Check{}
	if n.CheckList == nil {
		return out
	}

	for _, c := range n.CheckList.Contexts.Nodes {
		if check, ok := c.check(); ok {
			out = append(out, check)
		}
	}

	return out
}

// check converts one rollup context; ok is false for null and other nodes.
func (c *graphQLCheck) check() (Check, bool) {
	if c == nil {
		return Check{}, false
	}

	switch c.TypeName {
	case "CheckRun":
		state, counted := c.Conclusion, checkRunOutcome(c.Conclusion)
		if state == "" {
			state, counted = c.Status, outcomePending
		}

		check := Check{
			Kind: CheckKindRun, Name: c.Name, State: state, Outcome: counted.word(), URL: c.DetailsURL, Required: c.IsRequired,
		}

		if s := c.CheckSuite; s != nil && s.WorkflowRun != nil && s.WorkflowRun.Workflow != nil {
			check.Workflow = s.WorkflowRun.Workflow.Name
		}

		return check, check.Name != ""
	case "StatusContext":
		return Check{
			Kind: CheckKindStatus, Name: c.Context, State: c.State, Outcome: statusOutcome(c.State).word(),
			URL: c.TargetURL, Required: c.IsRequired,
		}, c.Context != ""
	default:
		return Check{}, false
	}
}

// word names an outcome the way Check.Outcome does.
func (o outcome) word() string {
	switch o {
	case outcomePassed:
		return CheckPassed
	case outcomeFailed:
		return CheckFailed
	case outcomeSkipped:
		return CheckSkipped
	case outcomePending:
		return CheckPending
	default:
		return CheckPending
	}
}

// Mergeable reports whether a merge state lets a merge through now without bypassing
// anything: CLEAN or HAS_HOOKS. UNSTABLE merges too, but only after the user accepts the
// failing checks, which the caller decides.
func Mergeable(status string) bool {
	return strings.EqualFold(status, MergeStateClean) || strings.EqualFold(status, MergeStateHasHooks)
}
