package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The issue sheet reads an issue with three queries, each about one point:
//
//   - The view (FetchIssueView): what the item query reads (ItemFields and DetailFields), so
//     the inbox card is rewritten from the same answer, and IssueViewFields: the
//     description, why it is closed, whether its conversation is locked, what the viewer may
//     do, its type, milestone, parent, duplicate, assignees, participants, sub-issues, the
//     pull requests that close it, its reactions, its newest comment, and the newest page of
//     its timeline. Every nested list is asked for by size, and nothing nests a list inside
//     the timeline or the closing pull requests, so GitHub prices the whole read at one
//     point (measured, with and without the projects).
//   - The state (FetchIssueState): whether it is still open, whether the viewer may close
//     it, and its newest comments, which a close reads first so it never closes over a
//     comment the user hasn't seen.
//   - An earlier page of the timeline (FetchTimeline), read only when the user asks. The
//     review sheet reads a pull request's earlier pages with the same query.
//
// GitHub's timeline pages run oldest first: the newest page is the last one, and its
// startCursor reads the page before it. Its totalCount counts every item of the asked
// types in the whole timeline, the same on every page.

// The timeline's page sizes: the sheet reads the newest timelinePage items, and, when that
// answer is larger than the client reads (a hundred long comments can be), the newest
// timelineSmallPage instead.
const (
	timelinePage      = 100
	timelineSmallPage = 20
)

// timelineItemTypes are the timeline items the issue sheet reads: the comments, and the
// events a conversation is followed by. The view and the earlier pages ask for the same
// ones, so a page read later joins the first without a gap.
const timelineItemTypes = `[ISSUE_COMMENT, CROSS_REFERENCED_EVENT, CONNECTED_EVENT, DISCONNECTED_EVENT,
      CLOSED_EVENT, REOPENED_EVENT, ASSIGNED_EVENT, UNASSIGNED_EVENT, LABELED_EVENT, UNLABELED_EVENT,
      RENAMED_TITLE_EVENT, MARKED_AS_DUPLICATE_EVENT]`

// pullTimelineItemTypes are the timeline items the review sheet reads of a pull request:
// the issue sheet's, and the reviews and the events only a pull request has. Its commits are
// left out, so a busy branch never crowds the comments out of the page.
const pullTimelineItemTypes = `[ISSUE_COMMENT, CROSS_REFERENCED_EVENT, CONNECTED_EVENT, DISCONNECTED_EVENT,
      CLOSED_EVENT, REOPENED_EVENT, ASSIGNED_EVENT, UNASSIGNED_EVENT, LABELED_EVENT, UNLABELED_EVENT,
      RENAMED_TITLE_EVENT, MARKED_AS_DUPLICATE_EVENT, PULL_REQUEST_REVIEW, MERGED_EVENT,
      HEAD_REF_FORCE_PUSHED_EVENT, REVIEW_REQUESTED_EVENT, READY_FOR_REVIEW_EVENT, CONVERT_TO_DRAFT_EVENT]`

// issueViewQuery reads the issue sheet's view of one issue, and issueViewProjectsQuery the
// same with the projects it is in (see DetailOptions).
const (
	issueViewQuery         = issueViewQueryHead + detailFieldsFragment + issueViewFragments
	issueViewProjectsQuery = issueViewQueryHead + detailFieldsProjectsFragment + issueViewFragments
)

// issueViewQueryHead is the view query without its DetailFields fragment.
const issueViewQueryHead = `query IssueView($id: ID!, $last: Int!) {
  node(id: $id) { ...ItemFields ...DetailFields ...IssueViewFields }
  rateLimit { cost remaining resetAt limit used }
}
` + itemFieldsFragment + repoFieldsFragment

// issueViewFragments are what the view reads beyond an inbox card. The newest comment is
// aliased, since DetailFields asks for the comments' count under their own name, and so
// are the sub-issues and the closing pull requests, for what they are to the sheet.
const issueViewFragments = `
fragment IssueViewFields on Issue {
  body lastEditedAt stateReason closedAt locked activeLockReason
  viewerCanClose viewerCanReopen viewerCanUpdate viewerDidAuthor
  issueType { name color description }
  milestone { title url dueOn state progressPercentage }
  duplicateOf { ...RefFields }
  parent { ...RefFields }
  assignees(first: 10) { totalCount nodes { login } }
  participants(first: 10) { totalCount nodes { login } }
  subIssueList: subIssues(first: 25) { nodes { ...RefFields } }
  fixes: closedByPullRequestsReferences(first: 10, includeClosedPrs: true) {
    totalCount
    nodes { ...RefFields baseRefName mergedAt reviewDecision statusCheckRollup { state } }
  }
  reactionGroups { content reactors { totalCount } }
  lastComment: comments(last: 1) { nodes { id createdAt viewerDidAuthor author { login } } }
  timeline: timelineItems(last: $last, itemTypes: ` + timelineItemTypes + `) { ...TimelineFields }
}
` + timelineFieldsFragment + refFieldsFragment + assigneeFieldsFragment

// timelineFieldsFragment is what the issue sheet reads of each timeline item, and
// pullTimelineFieldsFragment what the review sheet reads of a pull request's: the same
// items (timelineNodes), and the reviews and events only a pull request has
// (pullTimelineNodes). A pull request's totalCount counts every item of its timeline,
// whatever the types asked for (measured in November 2026: 25 where 11 were of the types
// asked for), so its page is counted by filteredCount, which counts the items of the types
// asked for up to the page's end: on the newest page, the whole timeline's.
const (
	timelineFieldsFragment = `
fragment TimelineFields on IssueTimelineItemsConnection {
  totalCount
  pageInfo { hasPreviousPage startCursor }
  nodes {` + timelineNodes + `  }
}
`
	pullTimelineFieldsFragment = `
fragment PullTimelineFields on PullRequestTimelineItemsConnection {
  totalCount filteredCount
  pageInfo { hasPreviousPage startCursor }
  nodes {` + timelineNodes + pullTimelineNodes + `  }
}
`
)

// timelineNodes selects each timeline item both sheets read. Fields that more than one item
// type has share a type and nullability, as GitHub requires of fields merged under one
// name; a rename's subject, which would clash with a connection's, is never asked for.
const timelineNodes = `
    __typename
    ... on IssueComment {
      id url createdAt lastEditedAt body authorAssociation isMinimized minimizedReason viewerDidAuthor
      author { __typename login }
    }
    ... on CrossReferencedEvent {
      id createdAt willCloseTarget isCrossRepository actor { __typename login } source { ...RefFields }
    }
    ... on ConnectedEvent {
      id createdAt isCrossRepository actor { __typename login } source { ...RefFields } subject { ...RefFields }
    }
    ... on DisconnectedEvent {
      id createdAt isCrossRepository actor { __typename login } source { ...RefFields } subject { ...RefFields }
    }
    ... on ClosedEvent {
      id createdAt stateReason actor { __typename login }
      closer { __typename ...RefFields ... on Commit { abbreviatedOid url } }
      duplicateOf { ...RefFields }
    }
    ... on ReopenedEvent { id createdAt stateReason actor { __typename login } }
    ... on AssignedEvent { id createdAt actor { __typename login } assignee { ...AssigneeFields } }
    ... on UnassignedEvent { id createdAt actor { __typename login } assignee { ...AssigneeFields } }
    ... on LabeledEvent { id createdAt actor { __typename login } label { name color } }
    ... on UnlabeledEvent { id createdAt actor { __typename login } label { name color } }
    ... on RenamedTitleEvent { id createdAt actor { __typename login } previousTitle currentTitle }
    ... on MarkedAsDuplicateEvent {
      id createdAt isCrossRepository actor { __typename login } canonical { ...RefFields } duplicate { ...RefFields }
    }
`

// pullTimelineNodes selects the reviews and events only a pull request's timeline has. A
// review reads its first line comments, each with the end of the diff hunk it is on; a
// merge's commit is aliased, since a review's commit is asked for with other fields.
const pullTimelineNodes = `    ... on PullRequestReview {
      id url createdAt submittedAt lastEditedAt body state authorAssociation viewerDidAuthor
      author { __typename login }
      commit { oid }
      lineComments: comments(first: 20) {
        totalCount
        nodes { id url createdAt body path line originalLine startLine originalStartLine outdated diffHunk }
      }
    }
    ... on MergedEvent { id url createdAt actor { __typename login } mergeCommit: commit { abbreviatedOid } mergeRefName }
    ... on HeadRefForcePushedEvent {
      id createdAt actor { __typename login } beforeCommit { abbreviatedOid } afterCommit { abbreviatedOid }
    }
    ... on ReviewRequestedEvent {
      id createdAt actor { __typename login }
      requestedReviewer {
        __typename
        ... on User { login }
        ... on Bot { login }
        ... on Mannequin { login }
        ... on Team { combinedSlug }
      }
    }
    ... on ReadyForReviewEvent { id createdAt actor { __typename login } }
    ... on ConvertToDraftEvent { id createdAt actor { __typename login } }
`

// refFieldsFragment names a pull request or issue another refers to. The two states are
// aliased, as ItemFields does, since they are different enum types.
const refFieldsFragment = `
fragment RefFields on ReferencedSubject {
  __typename
  ... on Issue { number title url issueState: state stateReason repository { nameWithOwner } }
  ... on PullRequest { number title url prState: state isDraft merged repository { nameWithOwner } }
}
`

// assigneeFieldsFragment names whoever an assignment was made to or taken from.
const assigneeFieldsFragment = `
fragment AssigneeFields on Assignee {
  __typename
  ... on User { login }
  ... on Bot { login }
  ... on Mannequin { login }
  ... on Organization { login }
}
`

// issueStateQuery reads what a close of the issue must check first, as of now: whether it
// is still open, whether the viewer may close it, and its newest comments, oldest first,
// each with whether the viewer wrote it. It costs one point.
const issueStateQuery = `query IssueState($id: ID!) {
  node(id: $id) {
    __typename
    ... on Issue {
      issueState: state stateReason viewerCanClose
      recent: comments(last: 10) { totalCount nodes { id createdAt viewerDidAuthor author { login } } }
    }
  }
  rateLimit { cost remaining resetAt limit used }
}
`

// timelineQuery reads the page of an issue's or a pull request's timeline before a cursor,
// with the item's own number and repository, which tell it apart from the other side of a
// connection, and a pull request's head, which tells a review of an earlier commit. Each
// kind's page is aliased, since the two connections are different types. It costs one
// point for either kind (measured).
const timelineQuery = `query Timeline($id: ID!, $before: String!, $last: Int!) {
  node(id: $id) {
    __typename
    ... on Issue {
      number repository { nameWithOwner }
      issueTimeline: timelineItems(last: $last, before: $before, itemTypes: ` + timelineItemTypes + `) {
        ...TimelineFields
      }
    }
    ... on PullRequest {
      number repository { nameWithOwner } headRefOid
      pullTimeline: timelineItems(last: $last, before: $before, itemTypes: ` + pullTimelineItemTypes + `) {
        ...PullTimelineFields
      }
    }
  }
  rateLimit { cost remaining resetAt limit used }
}
` + timelineFieldsFragment + pullTimelineFieldsFragment + refFieldsFragment + assigneeFieldsFragment

// The timeline items the sheets read, by GitHub's type names: both sheets' first, then
// those only a pull request's timeline has.
const (
	TimelineComment           = "IssueComment"
	TimelineCrossReferenced   = "CrossReferencedEvent"
	TimelineConnected         = "ConnectedEvent"
	TimelineDisconnected      = "DisconnectedEvent"
	TimelineClosed            = "ClosedEvent"
	TimelineReopened          = "ReopenedEvent"
	TimelineAssigned          = "AssignedEvent"
	TimelineUnassigned        = "UnassignedEvent"
	TimelineLabeled           = "LabeledEvent"
	TimelineUnlabeled         = "UnlabeledEvent"
	TimelineRenamed           = "RenamedTitleEvent"
	TimelineMarkedAsDuplicate = "MarkedAsDuplicateEvent"

	TimelineReview           = "PullRequestReview"
	TimelineMerged           = "MergedEvent"
	TimelineForcePushed      = "HeadRefForcePushedEvent"
	TimelineReviewRequested  = "ReviewRequestedEvent"
	TimelineReadyForReview   = "ReadyForReviewEvent"
	TimelineConvertedToDraft = "ConvertToDraftEvent"
)

// A review's state, as GitHub's PullRequestReviewState says.
const (
	ReviewStateApproved         = "APPROVED"
	ReviewStateChangesRequested = "CHANGES_REQUESTED"
	ReviewStateCommented        = "COMMENTED"
	ReviewStateDismissed        = "DISMISSED"
	ReviewStatePending          = "PENDING"
)

// Why an issue is closed, or was reopened, as GitHub's stateReason says.
const (
	StateReasonCompleted  = "COMPLETED"
	StateReasonNotPlanned = "NOT_PLANNED"
	StateReasonDuplicate  = "DUPLICATE"
	StateReasonReopened   = "REOPENED"
)

var (
	// ErrIssueGone is returned for a read of an earlier page of an issue GitHub can no
	// longer find: it was deleted, or the token can no longer see it.
	ErrIssueGone = errors.New("GitHub can't find the issue")

	// ErrPullGone is returned for a read of an earlier page of a pull request GitHub can no
	// longer find.
	ErrPullGone = errors.New("GitHub can't find the pull request")

	// ErrInvalidCursor is returned for a timeline cursor GitHub could not have given: an
	// empty one, or one longer than its cursors run.
	ErrInvalidCursor = errors.New("invalid timeline cursor")
)

// IssueView is what FetchIssueView read about one issue.
type IssueView struct {
	// Update is the item and its card's details, as FetchItem reads them. When
	// Update.Found is false, GitHub could not resolve the ID and the rest is zero.
	Update ItemUpdate

	// Body is the description, as markdown, and LastEditedAt when it was last edited;
	// zero when it never was.
	Body         string
	LastEditedAt time.Time

	// StateReason is why it is closed (StateReasonCompleted, StateReasonNotPlanned, or
	// StateReasonDuplicate), StateReasonReopened for one reopened, and "" for one never
	// closed. ClosedAt is when it was last closed; zero while it is open.
	StateReason string
	ClosedAt    time.Time

	// Locked reports a locked conversation, which only people with write access can comment
	// on, and LockReason why: OFF_TOPIC, TOO_HEATED, RESOLVED, SPAM, or "".
	Locked     bool
	LockReason string

	// What GitHub lets the viewer do now: close it, reopen it, and edit it (its labels and
	// assignees included); and whether the viewer opened it.
	ViewerCanClose  bool
	ViewerCanReopen bool
	ViewerCanUpdate bool
	ViewerDidAuthor bool

	// Type is the issue's type, zero when it has none.
	Type IssueType

	// Milestone is the issue's milestone, nil when it is in none.
	Milestone *Milestone

	// DuplicateOf is the issue it was closed as a duplicate of, and Parent the issue it is
	// a sub-issue of; nil for none, or one the viewer cannot see.
	DuplicateOf *ItemRef
	Parent      *ItemRef

	// Assignees and Participants are the first ten of each, and how many there are.
	Assignees    People
	Participants People

	// SubIssues are its first 25 sub-issues; Update.Details.SubIssues counts them all.
	SubIssues []ItemRef

	// Fixes are the first ten pull requests that close it once merged, closed ones
	// included, and FixCount how many there are that the viewer can see.
	Fixes    []LinkedPull
	FixCount int

	// Reactions counts the description's reactions, each kind with any.
	Reactions []Reaction

	// LatestComment is its newest comment, nil when it has none.
	LatestComment *CommentMark

	// Timeline is the newest page of its timeline.
	Timeline TimelinePage
}

// IssueType is an issue's type, such as Bug.
type IssueType struct {
	Name        string
	Description string

	// Color is the type's color as "#rrggbb", or "" for one this build doesn't know.
	Color string
}

// Milestone is the milestone an issue is in.
type Milestone struct {
	Title string
	URL   string

	// DueOn is the day it is due, at midnight UTC; zero when it has no due date.
	DueOn time.Time

	// State is OPEN or CLOSED.
	State string

	// Progress is how far along it is, as a percentage of its issues and pull requests
	// closed.
	Progress float64
}

// ItemRef is a pull request or issue another names: a sub-issue, a parent, a duplicate,
// a fix, or the other side of a timeline event.
type ItemRef struct {
	IsPR bool

	// Repo is its repository as "owner/name".
	Repo   string
	Number int
	Title  string
	URL    string

	// State is StateOpen, StateClosed, or StateMerged.
	State string

	// StateReason is a closed issue's reason (see the StateReason values), "" otherwise.
	StateReason string

	// IsDraft reports a draft pull request.
	IsDraft bool
}

// LinkedPull is a pull request that closes an issue once it merges: one whose description
// says so, or one someone linked to the issue by hand.
type LinkedPull struct {
	Ref ItemRef

	// Base is the branch it merges into, and MergedAt when it merged; zero while it hasn't.
	Base     string
	MergedAt time.Time

	// ReviewDecision is APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or "".
	ReviewDecision string

	// Checks is its head commit's overall result: SUCCESS, FAILURE, ERROR, PENDING,
	// EXPECTED, or "" for none.
	Checks string
}

// People are the first few logins of an issue's assignees or participants, and how many
// there are in all.
type People struct {
	Logins []string
	Total  int
}

// Reaction counts one kind of reaction.
type Reaction struct {
	// Content is GitHub's name for the reaction: THUMBS_UP, THUMBS_DOWN, LAUGH, HOORAY,
	// CONFUSED, HEART, ROCKET, or EYES.
	Content string
	Count   int
}

// CommentMark is a comment as a close checks it: its ID, when it was written, by whom,
// and whether by the viewer.
type CommentMark struct {
	ID        string
	CreatedAt time.Time
	Author    string
	Mine      bool
}

// TimelinePage is one page of an issue's or a pull request's timeline, oldest first.
type TimelinePage struct {
	Items []TimelineItem

	// HasEarlier reports items before the page, which Before, the page's cursor, reads.
	HasEarlier bool
	Before     string

	// Total counts every item of the types the sheet reads in the whole timeline; on an
	// earlier page of a pull request's, those up to the page's end.
	Total int

	// RateLimit is the GraphQL budget after the read that brought the page.
	RateLimit QueryRate
}

// TimelineItem is one comment, review, or event of an issue's or a pull request's
// timeline. Which fields are set depends on its Type, one of the Timeline values.
type TimelineItem struct {
	Type      string
	ID        string
	CreatedAt time.Time

	// Actor is who wrote the comment or made the event, "" when GitHub doesn't say, as for
	// a deleted account, and ActorType its GraphQL type: User, Bot, or another.
	Actor     string
	ActorType string

	// A comment's or a review's text, page, its author's relationship to the repository,
	// when it was last edited (zero when never), and whether the viewer wrote it. A merge's
	// page is its URL too.
	Body        string
	URL         string
	Association string
	EditedAt    time.Time
	Mine        bool

	// Minimized reports a comment someone folded away, and MinimizedReason why, as an
	// upper-case word: ABUSE, OFF_TOPIC, OUTDATED, RESOLVED, DUPLICATE, SPAM, or "".
	Minimized       bool
	MinimizedReason string

	// Ref is the other item an event names: the pull request or issue that mentioned this
	// one, the other side of a connection, the pull request or issue that closed it, or the
	// issue it was marked a duplicate of.
	Ref *ItemRef

	// WillClose reports a mention that closes this issue once it merges, and CrossRepo one
	// from another repository.
	WillClose bool
	CrossRepo bool

	// Commit is the short SHA of the commit that closed it, whose page URL holds, or of a
	// merge's commit; and DuplicateOf the issue a close as a duplicate names.
	Commit      string
	DuplicateOf *ItemRef

	// StateReason is a close's or a reopen's reason (see the StateReason values).
	StateReason string

	// Label is a label added or taken off, and Assignee who was assigned or unassigned.
	Label    *Label
	Assignee string

	// PreviousTitle and CurrentTitle are a rename's titles.
	PreviousTitle string
	CurrentTitle  string

	// ReviewState is a review's state (see the ReviewState values), and Outdated reports one
	// left on an earlier commit than the pull request's head as of the read. LineComments are
	// its first line comments, and LineCommentCount how many it has in all.
	ReviewState      string
	Outdated         bool
	LineComments     []ReviewLineComment
	LineCommentCount int

	// Reviewer is whom a review was requested of: a user's login, or a team as "org/team".
	Reviewer string

	// Branch is the branch a merge went into.
	Branch string

	// BeforeCommit and AfterCommit are a force push's short SHAs, before and after it.
	BeforeCommit string
	AfterCommit  string
}

// ReviewLineComment is one of a review's line comments.
type ReviewLineComment struct {
	ID        string
	URL       string
	CreatedAt time.Time
	Body      string

	// Path is the file it is on, and Line the line it ends on, StartLine the one it starts
	// on for one over several lines (else 0), as the diff it was left on numbers them when
	// it is Outdated: the lines it was on have changed since.
	Path      string
	Line      int
	StartLine int
	Outdated  bool

	// DiffHunk is the diff hunk it was left on, up to its line, as GitHub gives it.
	DiffHunk string
}

// IssueState is what FetchIssueState read about an issue, as of now.
type IssueState struct {
	// Found is false when GitHub could not resolve the ID; the rest is zero then.
	Found bool

	// State is StateOpen or StateClosed, and StateReason why (see the StateReason values).
	State       string
	StateReason string

	// CanClose says whether GitHub lets the viewer close it.
	CanClose bool

	// Comments counts its comments, and Recent are the newest ten, oldest first.
	Comments int
	Recent   []CommentMark
}

// FetchIssueView reads what the issue sheet shows about the issue with node ID id, as of
// now, with what opts adds to its card's details, with one GraphQL request. When GitHub
// refuses the projects opts asks for, for want of a scope, it reads it again without them,
// and when the answer is larger than the client reads, again with a smaller page of the
// timeline.
func (c *Client) FetchIssueView(ctx context.Context, tok Token, id string, opts DetailOptions) (*IssueView, error) {
	view, err := c.fetchIssueView(ctx, tok, id, opts)

	return view, Redact(err, tok)
}

// FetchIssueState reads whether the issue with node ID id is still open, whether the viewer
// may close it, and its newest comments, as of now, with one GraphQL request of one point.
// A close reads it first.
func (c *Client) FetchIssueState(ctx context.Context, tok Token, id string) (*IssueState, error) {
	state, err := c.fetchIssueState(ctx, tok, id)

	return state, Redact(err, tok)
}

// FetchTimeline reads the page of the timeline of the issue or pull request (kind) with
// node ID id before the cursor before, with one GraphQL request. An issue GitHub can no
// longer find returns ErrIssueGone, and a pull request ErrPullGone.
func (c *Client) FetchTimeline(ctx context.Context, tok Token, kind item.Kind, id, before string) (*TimelinePage, error) {
	page, err := c.fetchTimeline(ctx, tok, kind, id, before)

	return page, Redact(err, tok)
}

// issueViewQuery returns the issue view query that reads what o asks for.
func (o DetailOptions) issueViewQuery() string {
	if o.Projects {
		return issueViewProjectsQuery
	}

	return issueViewQuery
}

// fetchIssueView reads the view, again without the projects when GitHub refuses them for
// want of a scope.
func (c *Client) fetchIssueView(ctx context.Context, tok Token, id string, opts DetailOptions) (*IssueView, error) {
	view, err := c.issueViewPaged(ctx, tok, id, opts)
	if !opts.Projects || !refusedForScope(err) {
		return view, err
	}

	if view, err = c.issueViewPaged(ctx, tok, id, DetailOptions{}); err != nil {
		return nil, err
	}

	view.Update.ProjectsRefused = true

	return view, nil
}

// issueViewPaged sends the view query, with a smaller page of the timeline should the
// answer be too large (paged).
func (c *Client) issueViewPaged(ctx context.Context, tok Token, id string, opts DetailOptions) (*IssueView, error) {
	return paged(func(last int) (*IssueView, error) {
		return c.sendIssueView(ctx, tok, id, opts, last)
	})
}

// paged sends a read of the newest timelinePage items of a timeline, and sends it again
// for the newest timelineSmallPage when the answer is larger than the client reads, as a
// hundred long comments can make it.
func paged[T any](send func(last int) (T, error)) (T, error) {
	got, err := send(timelinePage)
	if errors.Is(err, ErrResponseTooLarge) {
		return send(timelineSmallPage)
	}

	return got, err
}

// sendIssueView sends the view query, with what opts adds and the newest last items of the
// timeline, and decodes the answer.
func (c *Client) sendIssueView(ctx context.Context, tok Token, id string, opts DetailOptions, last int) (*IssueView, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	req := graphQLRequest{Query: opts.issueViewQuery(), Variables: map[string]any{"id": id, "last": last}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "issue view", req, unlessData)
	if err != nil {
		return nil, err
	}

	update, err := itemUpdate(resp)
	if err != nil {
		return nil, err
	}

	update.ProjectsRefused = opts.Projects && scopeRefused(resp.Errors)

	view := &IssueView{Update: *update}
	if !update.Found {
		return view, nil
	}

	if update.Item.IsPR {
		return nil, fmt.Errorf("%w: the ID is not an issue", ErrMalformedResponse)
	}

	var fields issueViewNode
	if err = json.Unmarshal(resp.Data.Node, &fields); err != nil {
		return nil, fmt.Errorf("%w: decode the issue view: %w", ErrMalformedResponse, err)
	}

	self := ItemRef{Repo: update.Item.RepoOwner + "/" + update.Item.RepoName, Number: update.Item.Number}
	fields.fill(view, self)
	view.Timeline.RateLimit = update.RateLimit

	return view, nil
}

// fetchIssueState sends the state query and decodes the answer.
func (c *Client) fetchIssueState(ctx context.Context, tok Token, id string) (*IssueState, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	req := graphQLRequest{Query: issueStateQuery, Variables: map[string]any{"id": id}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "issue state", req, unlessData)
	if err != nil {
		return nil, err
	}

	if gone, goneErr := goneNode(resp); gone || goneErr != nil {
		if goneErr != nil {
			return nil, goneErr
		}

		return &IssueState{}, nil
	}

	var node struct {
		TypeName       string `json:"__typename"`
		IssueState     string `json:"issueState"`
		StateReason    string `json:"stateReason"`
		ViewerCanClose bool   `json:"viewerCanClose"`
		Recent         struct {
			TotalCount int                   `json:"totalCount"`
			Nodes      []*graphQLCommentMark `json:"nodes"`
		} `json:"recent"`
	}

	if err = json.Unmarshal(resp.Data.Node, &node); err != nil {
		return nil, fmt.Errorf("%w: decode the issue state: %w", ErrMalformedResponse, err)
	}

	if node.TypeName != typeIssue || node.IssueState == "" {
		return nil, fmt.Errorf("%w: the ID is not an issue", ErrMalformedResponse)
	}

	return &IssueState{
		Found: true, State: node.IssueState, StateReason: node.StateReason, CanClose: node.ViewerCanClose,
		Comments: max(node.Recent.TotalCount, 0), Recent: commentMarks(node.Recent.Nodes),
	}, nil
}

// maxCursorBytes caps a timeline cursor: GitHub's are a few dozen bytes.
const maxCursorBytes = 200

// fetchTimeline reads the page before the cursor, with a smaller page should the answer be
// too large (paged).
func (c *Client) fetchTimeline(ctx context.Context, tok Token, kind item.Kind, id, before string) (*TimelinePage, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	if !kind.Valid() {
		return nil, fmt.Errorf("%w: a timeline of a %q", ErrInvalidItemRef, kind)
	}

	if before == "" || len(before) > maxCursorBytes {
		return nil, fmt.Errorf("%w: %q", ErrInvalidCursor, before)
	}

	return paged(func(last int) (*TimelinePage, error) {
		return c.sendTimeline(ctx, tok, kind, id, before, last)
	})
}

// sendTimeline sends the timeline query for the last items before the cursor, and decodes
// the answer, which must be about an item of kind.
func (c *Client) sendTimeline(
	ctx context.Context, tok Token, kind item.Kind, id, before string, last int,
) (*TimelinePage, error) {
	req := graphQLRequest{Query: timelineQuery, Variables: map[string]any{"id": id, "before": before, "last": last}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "timeline", req, unlessData)
	if err != nil {
		return nil, err
	}

	if gone, goneErr := goneNode(resp); gone || goneErr != nil {
		if goneErr != nil {
			return nil, goneErr
		}

		return nil, timelineGone(kind)
	}

	page, err := timelineOf(resp.Data.Node, kind)
	if err != nil {
		return nil, err
	}

	page.RateLimit = resp.Data.RateLimit.queryRate()

	return page, nil
}

// timelineGone is the error for a read of an earlier page of an item of kind GitHub can no
// longer find.
func timelineGone(kind item.Kind) error {
	if kind == item.KindPR {
		return uierr.New("GitHub can't find the pull request any more, or your account can no longer see it.",
			ErrPullGone)
	}

	return uierr.New("GitHub can't find the issue any more, or your account can no longer see it.", ErrIssueGone)
}

// timelineOf decodes the timeline query's node, which must be an item of kind, into its page.
func timelineOf(raw json.RawMessage, kind item.Kind) (*TimelinePage, error) {
	var node struct {
		TypeName   string `json:"__typename"`
		Number     int    `json:"number"`
		HeadRefOID string `json:"headRefOid"`
		Repository *struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
		IssueTimeline graphQLTimeline `json:"issueTimeline"`
		PullTimeline  graphQLTimeline `json:"pullTimeline"`
	}

	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("%w: decode the timeline: %w", ErrMalformedResponse, err)
	}

	want := typeIssue
	if kind == item.KindPR {
		want = typePullRequest
	}

	if node.TypeName != want || node.Repository == nil {
		return nil, fmt.Errorf("%w: the ID is not a %s", ErrMalformedResponse, kind.Noun())
	}

	self := ItemRef{IsPR: kind == item.KindPR, Repo: node.Repository.NameWithOwner, Number: node.Number}
	timeline := node.IssueTimeline

	if kind == item.KindPR {
		timeline = node.PullTimeline
	}

	page := timeline.page(self, node.HeadRefOID)

	return &page, nil
}

// issueViewNode is the IssueViewFields part of a view's answer. GitHub's nulls decode to
// zero values.
type issueViewNode struct {
	Body             string    `json:"body"`
	LastEditedAt     time.Time `json:"lastEditedAt"`
	StateReason      string    `json:"stateReason"`
	ClosedAt         time.Time `json:"closedAt"`
	Locked           bool      `json:"locked"`
	ActiveLockReason string    `json:"activeLockReason"`
	ViewerCanClose   bool      `json:"viewerCanClose"`
	ViewerCanReopen  bool      `json:"viewerCanReopen"`
	ViewerCanUpdate  bool      `json:"viewerCanUpdate"`
	ViewerDidAuthor  bool      `json:"viewerDidAuthor"`
	IssueType        *struct {
		Name        string `json:"name"`
		Color       string `json:"color"`
		Description string `json:"description"`
	} `json:"issueType"`
	Milestone    *graphQLMilestone `json:"milestone"`
	DuplicateOf  *graphQLRef       `json:"duplicateOf"`
	Parent       *graphQLRef       `json:"parent"`
	Assignees    graphQLPeople     `json:"assignees"`
	Participants graphQLPeople     `json:"participants"`
	SubIssueList struct {
		Nodes []*graphQLRef `json:"nodes"`
	} `json:"subIssueList"`
	Fixes struct {
		TotalCount int           `json:"totalCount"`
		Nodes      []*graphQLFix `json:"nodes"`
	} `json:"fixes"`
	ReactionGroups []*graphQLReactionGroup `json:"reactionGroups"`
	LastComment    struct {
		Nodes []*graphQLCommentMark `json:"nodes"`
	} `json:"lastComment"`
	Timeline graphQLTimeline `json:"timeline"`
}

// fill copies the view's fields into v, telling the issue self apart from the other side
// of a connection.
func (n *issueViewNode) fill(v *IssueView, self ItemRef) {
	v.Body = n.Body
	v.LastEditedAt = n.LastEditedAt.UTC()
	v.StateReason = n.StateReason
	v.ClosedAt = n.ClosedAt.UTC()
	v.Locked = n.Locked
	v.LockReason = n.ActiveLockReason
	v.ViewerCanClose = n.ViewerCanClose
	v.ViewerCanReopen = n.ViewerCanReopen
	v.ViewerCanUpdate = n.ViewerCanUpdate
	v.ViewerDidAuthor = n.ViewerDidAuthor
	v.Milestone = n.Milestone.milestone()
	v.DuplicateOf = n.DuplicateOf.itemRef()
	v.Parent = n.Parent.itemRef()
	v.Assignees = n.Assignees.people()
	v.Participants = n.Participants.people()
	v.SubIssues = itemRefs(n.SubIssueList.Nodes)
	v.Fixes = fixes(n.Fixes.Nodes)
	v.FixCount = shownCount(n.Fixes.TotalCount, len(n.Fixes.Nodes), len(v.Fixes))
	v.Reactions = reactionsOf(n.ReactionGroups)
	v.Timeline = n.Timeline.page(self, "")

	if t := n.IssueType; t != nil && t.Name != "" {
		v.Type = IssueType{Name: t.Name, Description: t.Description, Color: projectColor(t.Color)}
	}

	if marks := commentMarks(n.LastComment.Nodes); len(marks) > 0 {
		v.LatestComment = &marks[len(marks)-1]
	}
}

// graphQLReactionGroup counts one kind of reaction.
type graphQLReactionGroup struct {
	Content  string       `json:"content"`
	Reactors graphQLCount `json:"reactors"`
}

// reactionsOf counts a description's reactions, leaving out the kinds with none.
func reactionsOf(groups []*graphQLReactionGroup) []Reaction {
	out := []Reaction{}

	for _, g := range groups {
		if g != nil && g.Content != "" && g.Reactors.TotalCount > 0 {
			out = append(out, Reaction{Content: g.Content, Count: g.Reactors.TotalCount})
		}
	}

	return out
}

// graphQLMilestone is an issue's milestone.
type graphQLMilestone struct {
	Title              string    `json:"title"`
	URL                string    `json:"url"`
	DueOn              time.Time `json:"dueOn"`
	State              string    `json:"state"`
	ProgressPercentage float64   `json:"progressPercentage"`
}

// milestone converts the milestone, nil for none.
func (m *graphQLMilestone) milestone() *Milestone {
	if m == nil || m.Title == "" {
		return nil
	}

	return &Milestone{
		Title: m.Title, URL: m.URL, DueOn: m.DueOn.UTC(), State: m.State, Progress: min(max(m.ProgressPercentage, 0), 100),
	}
}

// graphQLPeople is the first few of an issue's assignees or participants.
type graphQLPeople struct {
	TotalCount int `json:"totalCount"`
	Nodes      []*struct {
		Login string `json:"login"`
	} `json:"nodes"`
}

// people converts the logins, skipping nulls.
func (p graphQLPeople) people() People {
	out := People{Logins: []string{}, Total: max(p.TotalCount, 0)}

	for _, n := range p.Nodes {
		if n != nil && n.Login != "" {
			out.Logins = append(out.Logins, n.Login)
		}
	}

	out.Total = max(out.Total, len(out.Logins))

	return out
}

// graphQLRef is a pull request or issue another names (the RefFields fragment), or, as a
// close's closer, a commit or a project, which carry no number.
type graphQLRef struct {
	TypeName    string `json:"__typename"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	IssueState  string `json:"issueState"`
	PRState     string `json:"prState"`
	StateReason string `json:"stateReason"`
	IsDraft     bool   `json:"isDraft"`
	Repository  *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	AbbreviatedOID string `json:"abbreviatedOid"`
}

// itemRef converts a pull request or issue; nil for null and anything else.
func (r *graphQLRef) itemRef() *ItemRef {
	if r == nil || r.Repository == nil || r.Repository.NameWithOwner == "" || r.Number <= 0 {
		return nil
	}

	ref := ItemRef{Repo: r.Repository.NameWithOwner, Number: r.Number, Title: r.Title, URL: r.URL}

	switch r.TypeName {
	case typeIssue:
		ref.State, ref.StateReason = r.IssueState, r.StateReason
	case typePullRequest:
		ref.IsPR, ref.State, ref.IsDraft = true, r.PRState, r.IsDraft
	default:
		return nil
	}

	return &ref
}

// is reports whether r names the item self, a pull request or an issue, which a
// connection's two sides tell apart.
func (r *ItemRef) is(self ItemRef) bool {
	return r != nil && r.IsPR == self.IsPR && r.Number == self.Number && strings.EqualFold(r.Repo, self.Repo)
}

// itemRefs converts the pull requests and issues, skipping nulls and anything else.
func itemRefs(nodes []*graphQLRef) []ItemRef {
	out := []ItemRef{}

	for _, n := range nodes {
		if ref := n.itemRef(); ref != nil {
			out = append(out, *ref)
		}
	}

	return out
}

// shownCount is how many of a connection's totalCount the viewer can see, from the nodes
// GitHub returned and those kept: a node it returned as null is one the token can't read,
// which GitHub counts but the viewer can't open, so it isn't counted. It is never fewer
// than were kept.
func shownCount(total, returned, kept int) int {
	return max(total-(returned-kept), kept)
}

// graphQLFix is a pull request that closes an issue.
type graphQLFix struct {
	graphQLRef

	BaseRefName       string    `json:"baseRefName"`
	MergedAt          time.Time `json:"mergedAt"`
	ReviewDecision    string    `json:"reviewDecision"`
	StatusCheckRollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

// fixes converts the closing pull requests, skipping nulls and anything but a pull
// request.
func fixes(nodes []*graphQLFix) []LinkedPull {
	out := []LinkedPull{}

	for _, n := range nodes {
		if n == nil {
			continue
		}

		ref := n.itemRef()
		if ref == nil || !ref.IsPR {
			continue
		}

		fix := LinkedPull{Ref: *ref, Base: n.BaseRefName, MergedAt: n.MergedAt.UTC(), ReviewDecision: n.ReviewDecision}
		if n.StatusCheckRollup != nil {
			fix.Checks = n.StatusCheckRollup.State
		}

		out = append(out, fix)
	}

	return out
}

// graphQLCommentMark is a comment asked only for who wrote it and when.
type graphQLCommentMark struct {
	ID              string        `json:"id"`
	CreatedAt       time.Time     `json:"createdAt"`
	ViewerDidAuthor bool          `json:"viewerDidAuthor"`
	Author          *graphQLActor `json:"author"`
}

// commentMarks converts the comments, in GitHub's order, skipping nulls.
func commentMarks(nodes []*graphQLCommentMark) []CommentMark {
	out := []CommentMark{}

	for _, n := range nodes {
		if n == nil || n.ID == "" {
			continue
		}

		mark := CommentMark{ID: n.ID, CreatedAt: n.CreatedAt.UTC(), Mine: n.ViewerDidAuthor}
		if n.Author != nil {
			mark.Author = n.Author.Login
		}

		out = append(out, mark)
	}

	return out
}

// graphQLTimeline is a page of an issue's timeline (the TimelineFields fragment), or of a
// pull request's (PullTimelineFields).
type graphQLTimeline struct {
	TotalCount    int  `json:"totalCount"`
	FilteredCount *int `json:"filteredCount"`
	PageInfo      struct {
		HasPreviousPage bool   `json:"hasPreviousPage"`
		StartCursor     string `json:"startCursor"`
	} `json:"pageInfo"`
	Nodes []*graphQLTimelineNode `json:"nodes"`
}

// page converts the page, skipping nulls and items of types the sheets don't read. self is
// the item the timeline is of, and head a pull request's head commit as of the read, which
// tells a review of an earlier one ("" for an issue).
func (t graphQLTimeline) page(self ItemRef, head string) TimelinePage {
	page := TimelinePage{Items: []TimelineItem{}, HasEarlier: t.PageInfo.HasPreviousPage, Before: t.PageInfo.StartCursor}

	for _, n := range t.Nodes {
		if it, ok := n.item(self, head); ok {
			page.Items = append(page.Items, it)
		}
	}

	if !page.HasEarlier {
		page.Before = ""
	}

	total := t.TotalCount
	if t.FilteredCount != nil {
		total = *t.FilteredCount
	}

	page.Total = max(total, len(page.Items))

	return page
}

// graphQLTimelineNode is one timeline item. Which fields are set depends on its type; the
// others decode to zero values.
type graphQLTimelineNode struct {
	TypeName          string        `json:"__typename"`
	ID                string        `json:"id"`
	CreatedAt         time.Time     `json:"createdAt"`
	Actor             *graphQLActor `json:"actor"`
	Author            *graphQLActor `json:"author"`
	URL               string        `json:"url"`
	LastEditedAt      time.Time     `json:"lastEditedAt"`
	Body              string        `json:"body"`
	AuthorAssociation string        `json:"authorAssociation"`
	IsMinimized       bool          `json:"isMinimized"`
	MinimizedReason   string        `json:"minimizedReason"`
	ViewerDidAuthor   bool          `json:"viewerDidAuthor"`
	WillCloseTarget   bool          `json:"willCloseTarget"`
	IsCrossRepository bool          `json:"isCrossRepository"`
	Source            *graphQLRef   `json:"source"`
	Subject           *graphQLRef   `json:"subject"`
	Closer            *graphQLRef   `json:"closer"`
	DuplicateOf       *graphQLRef   `json:"duplicateOf"`
	Canonical         *graphQLRef   `json:"canonical"`
	Duplicate         *graphQLRef   `json:"duplicate"`
	StateReason       string        `json:"stateReason"`
	Assignee          *graphQLActor `json:"assignee"`
	Label             *struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"label"`
	PreviousTitle string `json:"previousTitle"`
	CurrentTitle  string `json:"currentTitle"`

	// A pull request's own: a review's, a merge's, a force push's, and a review request's.
	SubmittedAt  time.Time            `json:"submittedAt"`
	State        string               `json:"state"`
	Commit       *graphQLCommit       `json:"commit"`
	LineComments *graphQLLineComments `json:"lineComments"`
	MergeCommit  *graphQLShortCommit  `json:"mergeCommit"`
	MergeRefName string               `json:"mergeRefName"`
	BeforeCommit *graphQLShortCommit  `json:"beforeCommit"`
	AfterCommit  *graphQLShortCommit  `json:"afterCommit"`
	Requested    *graphQLReviewer     `json:"requestedReviewer"`
}

// graphQLShortCommit is a commit asked only for its short SHA.
type graphQLShortCommit struct {
	AbbreviatedOID string `json:"abbreviatedOid"`
}

// short returns the commit's short SHA, "" for a nil commit.
func (c *graphQLShortCommit) short() string {
	if c == nil {
		return ""
	}

	return c.AbbreviatedOID
}

// graphQLReviewer is whom a review was requested of: a user, a bot, a mannequin, or a team.
type graphQLReviewer struct {
	TypeName     string `json:"__typename"`
	Login        string `json:"login"`
	CombinedSlug string `json:"combinedSlug"`
}

// name is the reviewer's login, or a team's "org/team"; "" for none.
func (r *graphQLReviewer) name() string {
	switch {
	case r == nil:
		return ""
	case r.TypeName == "Team":
		return r.CombinedSlug
	default:
		return r.Login
	}
}

// graphQLLineComments are a review's first line comments, and how many it has.
type graphQLLineComments struct {
	TotalCount int                   `json:"totalCount"`
	Nodes      []*graphQLLineComment `json:"nodes"`
}

// graphQLLineComment is one of a review's line comments. Its line numbers are null once
// the lines it was on changed (outdated), when the original ones say where it was.
type graphQLLineComment struct {
	ID                string    `json:"id"`
	URL               string    `json:"url"`
	CreatedAt         time.Time `json:"createdAt"`
	Body              string    `json:"body"`
	Path              string    `json:"path"`
	Line              *int      `json:"line"`
	OriginalLine      *int      `json:"originalLine"`
	StartLine         *int      `json:"startLine"`
	OriginalStartLine *int      `json:"originalStartLine"`
	Outdated          bool      `json:"outdated"`
	DiffHunk          string    `json:"diffHunk"`
}

// comments converts the line comments, skipping nulls, and returns how many there are in
// all.
func (c *graphQLLineComments) comments() ([]ReviewLineComment, int) {
	out := []ReviewLineComment{}
	if c == nil {
		return out, 0
	}

	for _, n := range c.Nodes {
		if n == nil || n.ID == "" {
			continue
		}

		out = append(out, ReviewLineComment{
			ID: n.ID, URL: n.URL, CreatedAt: n.CreatedAt.UTC(), Body: n.Body, Path: n.Path,
			Line: lineOf(n.Line, n.OriginalLine), StartLine: lineOf(n.StartLine, n.OriginalStartLine),
			Outdated: n.Outdated, DiffHunk: n.DiffHunk,
		})
	}

	return out, shownCount(c.TotalCount, len(c.Nodes), len(out))
}

// lineOf is a line comment's line: the current one, else the original, else 0.
func lineOf(current, original *int) int {
	for _, l := range []*int{current, original} {
		if l != nil && *l > 0 {
			return *l
		}
	}

	return 0
}

// item converts the timeline item; ok is false for null, one without an ID, a type the
// sheets don't read, a review still pending, and one that only comments and says nothing.
// self is the item the timeline is of, and head a pull request's head commit as of the
// read.
func (n *graphQLTimelineNode) item(self ItemRef, head string) (TimelineItem, bool) {
	if n == nil || n.ID == "" {
		return TimelineItem{}, false
	}

	out := TimelineItem{Type: n.TypeName, ID: n.ID, CreatedAt: n.CreatedAt.UTC()}
	out.Actor, out.ActorType = n.Actor.names()

	switch n.TypeName {
	case TimelineComment:
		n.comment(&out)
	case TimelineReview:
		return out, n.review(&out, head)
	case TimelineCrossReferenced, TimelineConnected, TimelineDisconnected, TimelineMarkedAsDuplicate:
		n.reference(&out, self)
	case TimelineClosed, TimelineReopened:
		n.stateChange(&out)
	case TimelineAssigned, TimelineUnassigned, TimelineLabeled, TimelineUnlabeled, TimelineRenamed:
		n.change(&out)
	case TimelineMerged, TimelineForcePushed, TimelineReviewRequested, TimelineReadyForReview,
		TimelineConvertedToDraft:
		n.pullEvent(&out)
	default:
		return TimelineItem{}, false
	}

	return out, true
}

// review fills in a review's fields, and reports whether the sheet shows it: not one still
// pending, which only its author sees, nor one that only comments and holds no text and no
// line comment, which says nothing.
func (n *graphQLTimelineNode) review(out *TimelineItem, head string) bool {
	n.comment(out)
	out.ReviewState = n.State
	out.LineComments, out.LineCommentCount = n.LineComments.comments()

	if !n.SubmittedAt.IsZero() {
		out.CreatedAt = n.SubmittedAt.UTC()
	}

	commit := n.Commit.oid()
	out.Outdated = head != "" && commit != "" && !strings.EqualFold(commit, head)

	switch {
	case n.State == ReviewStatePending:
		return false
	case n.State == ReviewStateCommented && strings.TrimSpace(n.Body) == "" && out.LineCommentCount == 0:
		return false
	default:
		return true
	}
}

// pullEvent fills in what a merge, a force push, or a review request names.
func (n *graphQLTimelineNode) pullEvent(out *TimelineItem) {
	switch n.TypeName {
	case TimelineMerged:
		out.Commit, out.Branch, out.URL = n.MergeCommit.short(), n.MergeRefName, n.URL
	case TimelineForcePushed:
		out.BeforeCommit, out.AfterCommit = n.BeforeCommit.short(), n.AfterCommit.short()
	case TimelineReviewRequested:
		out.Reviewer = n.Requested.name()
	}
}

// comment fills in a comment's fields.
func (n *graphQLTimelineNode) comment(out *TimelineItem) {
	out.Actor, out.ActorType = n.Author.names()
	out.Body = n.Body
	out.URL = n.URL
	out.Association = n.AuthorAssociation
	out.EditedAt = n.LastEditedAt.UTC()
	out.Mine = n.ViewerDidAuthor
	out.Minimized = n.IsMinimized

	if n.IsMinimized {
		out.MinimizedReason = strings.ReplaceAll(strings.ToUpper(strings.TrimSpace(n.MinimizedReason)), "-", "_")
	}
}

// reference fills in the other item a mention, a connection, or a duplicate mark names: a
// mention's source, and the side of a connection or a duplicate mark that isn't self. A
// mark names the issue self duplicates, or one marked a duplicate of self.
func (n *graphQLTimelineNode) reference(out *TimelineItem, self ItemRef) {
	out.CrossRepo = n.IsCrossRepository

	switch n.TypeName {
	case TimelineCrossReferenced:
		out.Ref, out.WillClose = n.Source.itemRef(), n.WillCloseTarget
	case TimelineMarkedAsDuplicate:
		out.Ref = otherSide(self, n.Canonical, n.Duplicate)
	default:
		out.Ref = otherSide(self, n.Subject, n.Source)
	}
}

// otherSide returns the first of a and b that names a pull request or issue other than
// self, or nil when neither does.
func otherSide(self ItemRef, a, b *graphQLRef) *ItemRef {
	for _, side := range []*graphQLRef{a, b} {
		if ref := side.itemRef(); ref != nil && !ref.is(self) {
			return ref
		}
	}

	return nil
}

// stateChange fills in a close's or a reopen's reason, and what closed it: a pull request
// or issue, or a commit.
func (n *graphQLTimelineNode) stateChange(out *TimelineItem) {
	out.StateReason = n.StateReason

	if n.TypeName != TimelineClosed {
		return
	}

	out.DuplicateOf = n.DuplicateOf.itemRef()

	if n.Closer == nil {
		return
	}

	if n.Closer.TypeName == "Commit" {
		out.Commit, out.URL = n.Closer.AbbreviatedOID, n.Closer.URL
	} else {
		out.Ref = n.Closer.itemRef()
	}
}

// change fills in an assignment's assignee, a label's name and color, or a rename's
// titles.
func (n *graphQLTimelineNode) change(out *TimelineItem) {
	switch n.TypeName {
	case TimelineAssigned, TimelineUnassigned:
		out.Assignee, _ = n.Assignee.names()
	case TimelineLabeled, TimelineUnlabeled:
		if n.Label != nil && n.Label.Name != "" {
			out.Label = &Label{Name: n.Label.Name, Color: labelColor(n.Label.Color)}
		}
	default:
		out.PreviousTitle, out.CurrentTitle = n.PreviousTitle, n.CurrentTitle
	}
}

// names returns the actor's login and type, both "" for none.
func (a *graphQLActor) names() (login, typeName string) {
	if a == nil {
		return "", ""
	}

	return a.Login, a.TypeName
}
