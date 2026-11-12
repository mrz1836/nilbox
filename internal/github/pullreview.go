package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// pullReviewQuery reads what the review sheet shows about one pull request, by node ID,
// in one request of about one point: what the item query reads (ItemFields and
// DetailFields), so the inbox card is rewritten from the same answer, and ReviewFields:
// the description, the base branch, the merge state, what the viewer may do (close it
// included), the viewer's own latest review and pending reviews, auto-merge, the latest
// review of each reviewer with its words, the first commit's message, which holds
// Dependabot's metadata, and the head commit's checks, each with whether it is required.
//
// Fields DetailFields asks for with other arguments are aliased: GitHub refuses one name
// asked for twice with different arguments.
const pullReviewQuery = `query PullReview($id: ID!) {
  node(id: $id) { ...ItemFields ...DetailFields ...ReviewFields }
  rateLimit { cost remaining resetAt limit used }
}
` + itemFieldsFragment + repoFieldsFragment + detailFieldsFragment + reviewFieldsFragment

// reviewFieldsFragment is what the review sheet reads beyond an inbox card.
const reviewFieldsFragment = `
fragment ReviewFields on PullRequest {
  body baseRefName mergeStateStatus isMergeQueueEnabled
  viewerDidAuthor viewerCanUpdateBranch viewerCanEnableAutoMerge viewerCanMergeAsAdmin viewerCanClose
  viewerLatestReview { state commit { oid } }
  pendingReviews: reviews(states: [PENDING]) { totalCount }
  autoMergeRequest { mergeMethod enabledAt enabledBy { login } }
  reviewBodies: latestReviews(first: 50) {
    nodes { state body url submittedAt author { login } commit { oid } }
  }
  firstCommit: commits(first: 1) { nodes { commit { message } } }
  checkList: statusCheckRollup {
    contexts(first: 100) {
      nodes {
        __typename
        ... on CheckRun {
          name conclusion status detailsUrl isRequired(pullRequestId: $id)
          checkSuite { workflowRun { workflow { name } } }
        }
        ... on StatusContext { context state targetUrl isRequired(pullRequestId: $id) }
      }
    }
  }
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

	// Body is the description, as markdown.
	Body string

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

	// FirstCommitMessage is the first commit's message, "" when GitHub did not say.
	FirstCommitMessage string

	// Checks are the head commit's checks and statuses, the first 100.
	Checks []Check
}

// ReviewMark is a review's state and the commit it was left on.
type ReviewMark struct {
	// State is APPROVED, CHANGES_REQUESTED, COMMENTED, DISMISSED, or PENDING; "" for none.
	State string

	// Commit is the head commit the review was left on.
	Commit string
}

// Review is one reviewer's latest review.
type Review struct {
	Author string
	State  string
	Body   string
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
// id, as of now, with one GraphQL request.
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

// fetchPullReview sends the pull review query and decodes the answer.
func (c *Client) fetchPullReview(ctx context.Context, tok Token, id string) (*PullReview, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	req := graphQLRequest{Query: pullReviewQuery, Variables: map[string]any{"id": id}}

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

	fields.fill(review)

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
	Body                     string `json:"body"`
	BaseRefName              string `json:"baseRefName"`
	MergeStateStatus         string `json:"mergeStateStatus"`
	IsMergeQueueEnabled      bool   `json:"isMergeQueueEnabled"`
	ViewerDidAuthor          bool   `json:"viewerDidAuthor"`
	ViewerCanUpdateBranch    bool   `json:"viewerCanUpdateBranch"`
	ViewerCanEnableAutoMerge bool   `json:"viewerCanEnableAutoMerge"`
	ViewerCanMergeAsAdmin    bool   `json:"viewerCanMergeAsAdmin"`
	ViewerCanClose           bool   `json:"viewerCanClose"`
	ViewerLatestReview       *struct {
		State  string         `json:"state"`
		Commit *graphQLCommit `json:"commit"`
	} `json:"viewerLatestReview"`
	PendingReviews   graphQLCount      `json:"pendingReviews"`
	AutoMergeRequest *graphQLAutoMerge `json:"autoMergeRequest"`
	ReviewBodies     struct {
		Nodes []*struct {
			State       string         `json:"state"`
			Body        string         `json:"body"`
			URL         string         `json:"url"`
			SubmittedAt time.Time      `json:"submittedAt"`
			Author      *graphQLActor  `json:"author"`
			Commit      *graphQLCommit `json:"commit"`
		} `json:"nodes"`
	} `json:"reviewBodies"`
	FirstCommit struct {
		Nodes []*struct {
			Commit *struct {
				Message string `json:"message"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"firstCommit"`
	CheckList *struct {
		Contexts struct {
			Nodes []*graphQLCheck `json:"nodes"`
		} `json:"contexts"`
	} `json:"checkList"`
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

// fill copies the review fields into r.
func (n *reviewNode) fill(r *PullReview) {
	r.Body = n.Body
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
	r.FirstCommitMessage = n.firstCommitMessage()
	r.Checks = n.checks()

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
			Author: rev.Author.Login, State: rev.State, Body: rev.Body, URL: rev.URL, Commit: rev.Commit.oid(),
			SubmittedAt: rev.SubmittedAt.UTC(),
		})
	}

	return out
}

// firstCommitMessage is the first commit's message, or "".
func (n *reviewNode) firstCommitMessage() string {
	for _, c := range n.FirstCommit.Nodes {
		if c != nil && c.Commit != nil {
			return c.Commit.Message
		}
	}

	return ""
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
