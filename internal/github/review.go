package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The review sheet's writes. Each is one edit the user asked for, sent through edit (REST)
// or mutate (GraphQL), so the WriteGate paces it and the API budget counts it, and each is
// pinned to the head commit the user saw: a review names it as its commit, a merge as the
// SHA the head must still be, auto-merge and a branch update as the head they expect. A
// push in between makes GitHub refuse the write, which comes back as ErrPushedSince. A
// close takes no head, so the sheet reads the head before it sends one.

// Review events a submitted review can carry.
const (
	ReviewApprove        = "APPROVE"
	ReviewRequestChanges = "REQUEST_CHANGES"
	ReviewComment        = "COMMENT"
)

// Merge methods, as GitHub's GraphQL API and viewerDefaultMergeMethod name them.
const (
	MergeMethodMerge  = "MERGE"
	MergeMethodSquash = "SQUASH"
	MergeMethodRebase = "REBASE"
)

// Sides of a diff a line comment can be on.
const (
	SideLeft  = "LEFT"
	SideRight = "RIGHT"
)

// Bounds GitHub puts on what a review sends.
const (
	// maxBodyRunes is the longest review, comment, or line comment GitHub takes.
	maxBodyRunes = 65536

	// maxLineComments caps the line comments one review sends.
	maxLineComments = 100
)

// ErrInvalidWrite is returned for a review, merge, or comment that cannot be sent as it
// stands; nothing is sent.
var ErrInvalidWrite = errors.New("the change to GitHub is not valid")

// ReviewInput is a review to submit.
type ReviewInput struct {
	// Event is ReviewApprove, ReviewRequestChanges, or ReviewComment.
	Event string

	// Body is the review's text; it may be empty for an approval.
	Body string

	// CommitID is the head commit the user reviewed, which the review is pinned to.
	CommitID string

	// Comments are the review's line comments.
	Comments []LineComment
}

// LineComment is one line comment of a review.
type LineComment struct {
	Path string

	// Line is the comment's last line, on Side (SideLeft or SideRight).
	Line int
	Side string

	// StartLine is a multi-line comment's first line, on StartSide; 0 for one line.
	StartLine int
	StartSide string

	Body string
}

// SubmittedReview is a review as GitHub answered its submission.
type SubmittedReview struct {
	ID int64

	// State is APPROVED, CHANGES_REQUESTED, or COMMENTED.
	State string

	URL      string
	CommitID string

	// SubmittedAt is when GitHub recorded it.
	SubmittedAt time.Time
}

// MergeInput is a merge to make.
type MergeInput struct {
	// SHA is the head commit the user saw; GitHub refuses the merge when the head moved.
	SHA string

	// Method is MergeMethodMerge, MergeMethodSquash, or MergeMethodRebase.
	Method string
}

// Merged is a merge as GitHub answered it.
type Merged struct {
	// SHA is the commit the merge made.
	SHA string
}

// BranchUpdate is GitHub's answer to a branch update, which it makes in the background.
type BranchUpdate struct {
	Message string
}

// Comment is a conversation comment as GitHub answered its creation.
type Comment struct {
	ID        int64
	URL       string
	CreatedAt time.Time
}

// SubmitReview submits a review of the pull request ref, pinned to in.CommitID, with one
// REST request (POST /repos/{owner}/{repo}/pulls/{number}/reviews). GitHub answers with
// the review; one left pending, which a review with an event never is, returns
// ErrEditIgnored.
func (c *Client) SubmitReview(ctx context.Context, tok Token, ref item.Ref, in ReviewInput) (*SubmittedReview, error) {
	review, err := c.submitReview(ctx, tok, ref, in)

	return review, Redact(err, tok)
}

// MergePull merges the pull request ref, if its head is still in.SHA, with one REST
// request (PUT /repos/{owner}/{repo}/pulls/{number}/merge). A head that moved returns
// ErrPushedSince, and a pull request GitHub won't merge yet a *NotMergeableError.
//
// For an admin, GitHub's REST merge goes through branch protection unasked, so a caller
// reads the merge state first (FetchMergeState) and merges only one that is mergeable.
func (c *Client) MergePull(ctx context.Context, tok Token, ref item.Ref, in MergeInput) (*Merged, error) {
	merged, err := c.mergePull(ctx, tok, ref, in)

	return merged, Redact(err, tok)
}

// EnableAutoMerge turns on auto-merge for the pull request ref, with node ID nodeID, with
// method, if its head is still expectedHead, with one GraphQL mutation. GitHub refuses it
// for a pull request it could merge right now, which returns ErrMergeableNow, and for a
// head that moved, which returns ErrPushedSince.
func (c *Client) EnableAutoMerge(
	ctx context.Context, tok Token, ref item.Ref, nodeID, method, expectedHead string,
) (*AutoMerge, error) {
	auto, err := c.enableAutoMerge(ctx, tok, ref, nodeID, method, expectedHead)

	return auto, Redact(err, tok)
}

// UpdateBranch merges the base branch into the pull request ref's branch, if its head is
// still expectedHead, with one REST request (PUT
// /repos/{owner}/{repo}/pulls/{number}/update-branch). GitHub makes the update in the
// background.
func (c *Client) UpdateBranch(ctx context.Context, tok Token, ref item.Ref, expectedHead string) (*BranchUpdate, error) {
	update, err := c.updateBranch(ctx, tok, ref, expectedHead)

	return update, Redact(err, tok)
}

// ApproveWorkflowRun approves the GitHub Actions workflow run with ID runID in the
// repository name, which GitHub holds until a maintainer approves it, as it does a fork's,
// with one REST request (POST /repos/{owner}/{repo}/actions/runs/{run_id}/approve). It
// takes write access to the repository. GitHub documents it for a public fork's first-time
// contributor, and may refuse a run its other approval settings hold, which is worded
// (approveError) with what GitHub says. A run takes no head: the caller approves only the
// runs it read on the head the user saw.
func (c *Client) ApproveWorkflowRun(ctx context.Context, tok Token, name repo.Name, runID int64) error {
	return Redact(c.approveWorkflowRun(ctx, tok, name, runID), tok)
}

// AddComment adds a comment to the conversation of the pull request or issue ref, with
// one REST request (POST /repos/{owner}/{repo}/issues/{number}/comments). The review sheet
// uses it only to ask Dependabot to rebase, and the issue sheet for the user's comment,
// whose refusals are worded as a comment's (commentError).
func (c *Client) AddComment(ctx context.Context, tok Token, ref item.Ref, body string) (*Comment, error) {
	comment, err := c.addComment(ctx, tok, ref, body)

	return comment, Redact(err, tok)
}

// ClosePull closes the pull request ref without merging it, with one REST request (PATCH
// /repos/{owner}/{repo}/pulls/{number}). GitHub answers with the pull request; one it left
// open returns ErrEditIgnored, and a refusal is worded as a close (closeError), never as a
// merge or a push. GitHub takes no head to pin a close to, so a caller reads the head
// first (FetchMergeState) and closes only what the user saw.
func (c *Client) ClosePull(ctx context.Context, tok Token, ref item.Ref) error {
	return Redact(c.closePull(ctx, tok, ref), tok)
}

// restReviewComment is a line comment as the reviews endpoint takes it.
type restReviewComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

// submitReview checks the review, sends it, and checks GitHub's answer.
func (c *Client) submitReview(ctx context.Context, tok Token, ref item.Ref, in ReviewInput) (*SubmittedReview, error) {
	if err := validateReview(ref, in); err != nil {
		return nil, err
	}

	payload := struct {
		CommitID string              `json:"commit_id"`
		Body     string              `json:"body,omitempty"`
		Event    string              `json:"event"`
		Comments []restReviewComment `json:"comments,omitempty"`
	}{CommitID: in.CommitID, Body: in.Body, Event: in.Event}

	for _, cm := range in.Comments {
		payload.Comments = append(payload.Comments, restReviewComment{
			Path: cm.Path, Line: cm.Line, Side: cm.Side, StartLine: cm.StartLine, StartSide: startSide(cm), Body: cm.Body,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode the review: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodPost, pullPath(ref)+"/reviews", body)
	if err != nil {
		return nil, writeError(ref, fmt.Errorf("review %s: %w", ref, err))
	}

	var answer struct {
		ID          int64     `json:"id"`
		State       string    `json:"state"`
		HTMLURL     string    `json:"html_url"`
		CommitID    string    `json:"commit_id"`
		SubmittedAt time.Time `json:"submitted_at"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode the review of %s: %w", ErrMalformedResponse, ref, err)
	}

	if answer.State == "" || answer.State == "PENDING" {
		return nil, uierr.New(fmt.Sprintf("GitHub kept your review of %s as a draft instead of submitting it.", ref),
			fmt.Errorf("review %s: %w", ref, ErrEditIgnored))
	}

	return &SubmittedReview{
		ID: answer.ID, State: answer.State, URL: answer.HTMLURL, CommitID: answer.CommitID,
		SubmittedAt: answer.SubmittedAt.UTC(),
	}, nil
}

// startSide is the side a multi-line comment starts on: its own when it names none, and
// "" for a one-line comment.
func startSide(cm LineComment) string {
	if cm.StartLine == 0 {
		return ""
	}

	if cm.StartSide == "" {
		return cm.Side
	}

	return cm.StartSide
}

// mergePull checks the merge, sends it, and checks GitHub's answer.
func (c *Client) mergePull(ctx context.Context, tok Token, ref item.Ref, in MergeInput) (*Merged, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	if err := validateHead(in.SHA); err != nil {
		return nil, err
	}

	if !validMethod(in.Method) {
		return nil, fmt.Errorf("%w: merge method %q", ErrInvalidWrite, in.Method)
	}

	body, err := json.Marshal(map[string]string{"sha": in.SHA, "merge_method": strings.ToLower(in.Method)})
	if err != nil {
		return nil, fmt.Errorf("encode the merge: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodPut, pullPath(ref)+"/merge", body)
	if err != nil {
		return nil, writeError(ref, fmt.Errorf("merge %s: %w", ref, err))
	}

	var answer struct {
		SHA     string `json:"sha"`
		Merged  bool   `json:"merged"`
		Message string `json:"message"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode the merge of %s: %w", ErrMalformedResponse, ref, err)
	}

	if !answer.Merged {
		return nil, uierr.New(fmt.Sprintf("GitHub did not merge %s: %s", ref, sentence(nonEmpty(answer.Message, "no reason given"))),
			fmt.Errorf("merge %s: %w", ref, ErrEditIgnored))
	}

	return &Merged{SHA: answer.SHA}, nil
}

// enableAutoMergeMutation turns on auto-merge, pinned to the head the user saw.
const enableAutoMergeMutation = `mutation EnableAutoMerge($id: ID!, $method: PullRequestMergeMethod!, $head: GitObjectID!) {
  enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: $method, expectedHeadOid: $head}) {
    pullRequest { autoMergeRequest { mergeMethod enabledAt enabledBy { login } } }
  }
}`

// enableAutoMergeData is the mutation's data object.
type enableAutoMergeData struct {
	EnablePullRequestAutoMerge *struct {
		PullRequest *struct {
			AutoMergeRequest *graphQLAutoMerge `json:"autoMergeRequest"`
		} `json:"pullRequest"`
	} `json:"enablePullRequestAutoMerge"`
}

// enableAutoMerge checks the request, sends the mutation, and checks GitHub's answer.
func (c *Client) enableAutoMerge(
	ctx context.Context, tok Token, ref item.Ref, nodeID, method, expectedHead string,
) (*AutoMerge, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	if nodeID == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	if err := validateHead(expectedHead); err != nil {
		return nil, err
	}

	if !validMethod(method) {
		return nil, fmt.Errorf("%w: merge method %q", ErrInvalidWrite, method)
	}

	req := graphQLRequest{
		Query: enableAutoMergeMutation, Variables: map[string]any{"id": nodeID, "method": method, "head": expectedHead},
	}

	data, err := mutate[enableAutoMergeData](ctx, c, tok, "auto-merge", req)
	if err != nil {
		return nil, autoMergeError(ref, fmt.Errorf("turn on auto-merge for %s: %w", ref, err))
	}

	if data.EnablePullRequestAutoMerge == nil || data.EnablePullRequestAutoMerge.PullRequest == nil {
		return nil, fmt.Errorf("%w: the auto-merge answer for %s has no pull request", ErrMalformedResponse, ref)
	}

	auto := data.EnablePullRequestAutoMerge.PullRequest.AutoMergeRequest.autoMerge()
	if !auto.On() {
		return nil, uierr.New(fmt.Sprintf("GitHub did not turn on auto-merge for %s.", ref),
			fmt.Errorf("turn on auto-merge for %s: %w", ref, ErrEditIgnored))
	}

	return &auto, nil
}

// autoMergeError words a refused auto-merge from GitHub's GraphQL errors, keeping err's
// chain: a pull request GitHub could merge now, a head that moved, a merge queue, and a
// repository that does not allow auto-merge. A refusal over REST is worded by writeError.
func autoMergeError(ref item.Ref, err error) error {
	failed, ok := errors.AsType[*queryError](err)
	if !ok {
		return writeError(ref, err)
	}

	msg := strings.ToLower(failed.message)

	switch {
	case mergeableNow(msg):
		return fmt.Errorf("%w: %w", ErrMergeableNow, err)
	case headMismatch(msg):
		return pushedSince(ref, err)
	case strings.Contains(msg, "merge queue"):
		return notMergeable(ref, failed.message, err)
	case strings.Contains(msg, "not allowed") || strings.Contains(msg, "not enabled"):
		return uierr.New(fmt.Sprintf("The repository of %s does not allow auto-merge.", ref), err)
	default:
		return uierr.New(fmt.Sprintf("GitHub refused to turn on auto-merge for %s: %s", ref, sentence(failed.message)), err)
	}
}

// mergeableNow reports GitHub's refusal of auto-merge for a pull request it could merge
// right now, from its message in lower case, as in "Pull request is in clean status".
func mergeableNow(msg string) bool {
	return strings.Contains(msg, "clean status") || strings.Contains(msg, "unstable status") ||
		strings.Contains(msg, "has_hooks status")
}

// headMismatch reports GitHub's refusal of a write pinned to a head that moved, from its
// message in lower case.
func headMismatch(msg string) bool {
	return strings.Contains(msg, "head") && (strings.Contains(msg, "expected") || strings.Contains(msg, "match"))
}

// updateBranch checks the request, sends it, and reads GitHub's answer.
func (c *Client) updateBranch(ctx context.Context, tok Token, ref item.Ref, expectedHead string) (*BranchUpdate, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	if err := validateHead(expectedHead); err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string]string{"expected_head_sha": expectedHead})
	if err != nil {
		return nil, fmt.Errorf("encode the branch update: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodPut, pullPath(ref)+"/update-branch", body)
	if err != nil {
		return nil, writeError(ref, fmt.Errorf("update the branch of %s: %w", ref, err))
	}

	var answer struct {
		Message string `json:"message"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode the branch update of %s: %w", ErrMalformedResponse, ref, err)
	}

	return &BranchUpdate{Message: answer.Message}, nil
}

// approveWorkflowRun checks the run and sends the approval. GitHub answers 201 with an
// empty object, which says nothing more.
func (c *Client) approveWorkflowRun(ctx context.Context, tok Token, name repo.Name, runID int64) error {
	if name.Owner == "" || name.Repo == "" || runID <= 0 {
		return fmt.Errorf("%w: workflow run %d of %q/%q", ErrInvalidWrite, runID, name.Owner, name.Repo)
	}

	run := workflowRun{repo: name, id: runID}
	path := "/repos/" + url.PathEscape(name.Owner) + "/" + url.PathEscape(name.Repo) + "/actions/runs/" +
		strconv.FormatInt(runID, 10) + "/approve"

	if _, err := c.edit(ctx, tok, http.MethodPost, path, nil); err != nil {
		return approveError(run, fmt.Errorf("approve %s: %w", run, err))
	}

	return nil
}

// workflowRun names a workflow run in a refusal's words.
type workflowRun struct {
	repo repo.Name
	id   int64
}

// String is "workflow run 123 of owner/name".
func (r workflowRun) String() string {
	return "workflow run " + strconv.FormatInt(r.id, 10) + " of " + r.repo.String()
}

// approveError gives a refused approval of run a message the user can act on, keeping err's
// chain (refusedAs): a run GitHub can't find was deleted or run again, and one it won't
// approve says why in GitHub's words, or, when GitHub says nothing, what an approval takes.
func approveError(run workflowRun, err error) error {
	return refusedAs(run, err, refusal{
		missed: "it wasn't approved; it may have been deleted or run again",
		verb:   "approve",
		unsaid: "approving takes write access to the repository, and works only on a run waiting for approval",
	})
}

// addComment checks the comment, sends it, and reads GitHub's answer.
func (c *Client) addComment(ctx context.Context, tok Token, ref item.Ref, body string) (*Comment, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	if err := validateBody(body, true); err != nil {
		return nil, err
	}

	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return nil, fmt.Errorf("encode the comment: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodPost, issuePath(ref)+"/comments", payload)
	if err != nil {
		return nil, commentError(ref, fmt.Errorf("comment on %s: %w", ref, err))
	}

	var answer struct {
		ID        int64     `json:"id"`
		HTMLURL   string    `json:"html_url"`
		CreatedAt time.Time `json:"created_at"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode the comment on %s: %w", ErrMalformedResponse, ref, err)
	}

	return &Comment{ID: answer.ID, URL: answer.HTMLURL, CreatedAt: answer.CreatedAt.UTC()}, nil
}

// closePull checks the pull request and sends the close (closeItem).
func (c *Client) closePull(ctx context.Context, tok Token, ref item.Ref) error {
	if err := validateRef(ref); err != nil {
		return err
	}

	_, err := c.closeItem(ctx, tok, ref, pullPath(ref), map[string]string{"state": "closed"})

	return err
}

// closeError gives a refused close of ref a message the user can act on, keeping err's
// chain. A close is neither a merge nor pinned to a head, so a refusal is worded as a close
// whatever its status, never as a merge GitHub won't make (405) or a push (409), and it
// carries neither ErrNotMergeable nor ErrPushedSince (refusedAs).
func closeError(ref item.Ref, err error) error {
	return refusedAs(ref, err, refusal{
		missed: "it wasn't closed", verb: "close", unsaid: "your account may not be allowed to close it",
	})
}

// refusal is how a write that is pinned to nothing and merges nothing, a close, an issue's
// comment, or a workflow run's approval, words GitHub's refusal: what didn't happen when GitHub couldn't find the item,
// what GitHub wouldn't do to it, and why, when GitHub says nothing.
type refusal struct {
	missed string
	verb   string
	unsaid string
}

// refusedAs gives a refused write of ref, the item or whatever else the write was to, the
// words w gives it, whatever its status: never a merge GitHub won't make (405) or a push
// (409). A rate limit, a token GitHub refused, and a request that got no answer keep what
// they say.
func refusedAs(ref fmt.Stringer, err error, w refusal) error {
	status, ok := errors.AsType[*StatusError](err)
	if !ok || IsBadCredentials(err) {
		return err
	}

	switch status.StatusCode {
	case http.StatusNotFound, http.StatusGone:
		return uierr.New(fmt.Sprintf("GitHub could not find %s, or your account cannot see it, so %s.", ref, w.missed), err)
	case http.StatusForbidden, http.StatusMethodNotAllowed, http.StatusConflict, http.StatusUnprocessableEntity:
		if rateLimited(status) {
			return editError(ref, err)
		}

		return uierr.New(fmt.Sprintf("GitHub wouldn't %s %s: %s", w.verb, ref, sentence(nonEmpty(status.Message, w.unsaid))), err)
	default:
		return editError(ref, err)
	}
}

// validateReview checks a review before anything is sent: its pull request, its event,
// the commit it is pinned to, its text, which a request for changes needs, and each line
// comment.
func validateReview(ref item.Ref, in ReviewInput) error {
	if err := validateRef(ref); err != nil {
		return err
	}

	switch in.Event {
	case ReviewApprove, ReviewRequestChanges, ReviewComment:
	default:
		return fmt.Errorf("%w: review event %q", ErrInvalidWrite, in.Event)
	}

	if err := validateHead(in.CommitID); err != nil {
		return err
	}

	needsBody := in.Event == ReviewRequestChanges || (in.Event == ReviewComment && len(in.Comments) == 0)
	if err := validateBody(in.Body, needsBody); err != nil {
		return err
	}

	if len(in.Comments) > maxLineComments {
		return fmt.Errorf("%w: %d line comments, more than %d", ErrInvalidWrite, len(in.Comments), maxLineComments)
	}

	for _, cm := range in.Comments {
		if err := validateLineComment(cm); err != nil {
			return err
		}
	}

	return nil
}

// validateLineComment checks one line comment's place and text.
func validateLineComment(cm LineComment) error {
	switch {
	case cm.Path == "":
		return fmt.Errorf("%w: a line comment names no file", ErrInvalidWrite)
	case cm.Line <= 0 || cm.StartLine < 0 || (cm.StartLine > 0 && cm.StartLine >= cm.Line):
		return fmt.Errorf("%w: line comment on lines %d to %d of %s", ErrInvalidWrite, cm.StartLine, cm.Line, cm.Path)
	case !validSide(cm.Side) || (cm.StartSide != "" && !validSide(cm.StartSide)):
		return fmt.Errorf("%w: line comment on side %q of %s", ErrInvalidWrite, cm.Side, cm.Path)
	default:
		return validateBody(cm.Body, true)
	}
}

// validateBody checks the text of a review or comment: not longer than GitHub takes, and
// not blank when required.
func validateBody(body string, required bool) error {
	if required && strings.TrimSpace(body) == "" {
		return uierr.New("Write something first: GitHub needs text for this.", fmt.Errorf("%w: no text", ErrInvalidWrite))
	}

	if utf8.RuneCountInString(body) > maxBodyRunes {
		return uierr.New(fmt.Sprintf("The text is longer than the %d characters GitHub takes.", maxBodyRunes),
			fmt.Errorf("%w: text too long", ErrInvalidWrite))
	}

	return nil
}

// validateHead checks that a commit a write is pinned to is a SHA: 40 hexadecimal
// characters, or 64 for a SHA-256 repository.
func validateHead(sha string) error {
	if len(sha) != 40 && len(sha) != 64 {
		return fmt.Errorf("%w: head commit %q", ErrInvalidWrite, sha)
	}

	for _, r := range sha {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return fmt.Errorf("%w: head commit %q", ErrInvalidWrite, sha)
		}
	}

	return nil
}

// validMethod reports a merge method GitHub knows.
func validMethod(method string) bool {
	return method == MergeMethodMerge || method == MergeMethodSquash || method == MergeMethodRebase
}

// validSide reports a side of a diff.
func validSide(side string) bool {
	return side == SideLeft || side == SideRight
}

// nonEmpty returns s, or fallback when s is blank.
func nonEmpty(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}

	return s
}
