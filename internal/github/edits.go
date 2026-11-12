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

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The app reads GitHub, and edits it only when the user asks, one item at a time: it takes
// the user off an item's assignees, or off a pull request's requested reviewers, and,
// from the review sheet, reviews, merges, turns on auto-merge, updates a branch, asks
// Dependabot to rebase, or closes a pull request (see review.go). Each edit is one
// request: a REST edit spends one point of the REST budget, a GraphQL mutation one point of
// the GraphQL budget. Every edit goes through the client's WriteGate first (edit for REST,
// mutate for GraphQL), so the app's edits stay within the user's budget and well clear of
// GitHub's own limits on writes, and an answer that asks the app to slow down holds the
// next ones.

// secondaryLimitWait is how long GitHub asks a client it slowed down for writing too fast
// to wait, when the answer does not say: at least a minute.
const secondaryLimitWait = time.Minute

// mutationCost is what a GraphQL mutation spends of the GraphQL budget: one point. A
// mutation cannot select rateLimit to report its own cost.
const mutationCost = 1

// errorRateLimited is GitHub's GraphQL error type for a request refused for its rate
// limits.
const errorRateLimited = "RATE_LIMITED"

var (
	// ErrEditIgnored is returned when GitHub accepted an edit but left the item as it was,
	// as it does for a change the token's account may not make.
	ErrEditIgnored = errors.New("GitHub did not make the change")

	// ErrInvalidLogin is returned for an edit about a login GitHub could not have issued.
	ErrInvalidLogin = errors.New("invalid GitHub login")

	// ErrPushedSince is returned when a pull request was pushed to after the head commit an
	// edit was pinned to: GitHub refused the edit, and nothing changed.
	ErrPushedSince = errors.New("the pull request was pushed to since it was read")

	// ErrMergeableNow is returned when GitHub refuses to turn on auto-merge because the pull
	// request can be merged right now.
	ErrMergeableNow = errors.New("the pull request can be merged now")

	// ErrNotMergeable matches a *NotMergeableError with errors.Is.
	ErrNotMergeable = errors.New("GitHub won't merge the pull request yet")

	// ErrOwnPull is returned for an approval or a request for changes on the user's own pull
	// request, which GitHub refuses.
	ErrOwnPull = errors.New("GitHub refuses a review of your own pull request")

	// ErrPendingReview is returned when the user already has a pending review on the pull
	// request, started on GitHub and not submitted, which blocks another.
	ErrPendingReview = errors.New("you have a pending review on the pull request")

	// ErrLineNotInDiff is returned when GitHub refuses a line comment on a line its diff does
	// not show.
	ErrLineNotInDiff = errors.New("a line comment is not on a line of the diff")
)

// NotMergeableError is GitHub refusing a merge for what the pull request still needs: a
// required review or check, a conflict, or a merge queue. It matches ErrNotMergeable.
type NotMergeableError struct {
	// Reason is GitHub's own words, as in "At least 1 approving review is required by
	// reviewers with write access."
	Reason string

	// MergeQueue reports a branch that merges only through its merge queue.
	MergeQueue bool

	// cause is GitHub's refusal, or nil.
	cause error
}

// Error says why GitHub won't merge.
func (e *NotMergeableError) Error() string {
	if e.Reason == "" {
		return ErrNotMergeable.Error()
	}

	return ErrNotMergeable.Error() + ": " + e.Reason
}

// Is reports whether target is ErrNotMergeable.
func (e *NotMergeableError) Is(target error) bool {
	return target == ErrNotMergeable
}

// Unwrap returns GitHub's refusal, so its status stays readable.
func (e *NotMergeableError) Unwrap() error {
	return e.cause
}

// WriteGate paces the edits the client sends GitHub. *budget.Tracker implements it. It
// must return promptly; edits sent side by side call it concurrently.
type WriteGate interface {
	// AllowWrite asks to send one edit now, spending from the budget resource names
	// (ResourceCore for a REST edit). It counts the edit and returns nil when it may go,
	// or returns an error that says why not and when to try again.
	AllowWrite(resource string) error

	// HoldWrites hears that GitHub asked the app to send no more edits for wait.
	HoldWrites(wait time.Duration)
}

// ReviewRequests is who a pull request asks to review.
type ReviewRequests struct {
	// Users are the logins of the people and bots asked, in GitHub's order.
	Users []string

	// Teams are the slugs of the teams asked, in GitHub's order.
	Teams []string
}

// Asks reports whether the pull request asks login to review by name, ignoring case. A
// team the login belongs to does not count.
func (r ReviewRequests) Asks(login string) bool {
	for _, user := range r.Users {
		if strings.EqualFold(user, login) {
			return true
		}
	}

	return false
}

// Edited is a pull request or issue as GitHub answered an edit to it.
type Edited struct {
	// UpdatedAt is the item's last update on GitHub, the edit included.
	UpdatedAt time.Time

	// ReviewRequests is who a pull request still asks to review, after
	// RemoveReviewRequest. It is zero after RemoveAssignee.
	ReviewRequests ReviewRequests
}

// RemoveAssignee takes login off the assignees of a pull request or issue, with one REST
// request (DELETE /repos/{owner}/{repo}/issues/{number}/assignees). GitHub answers with
// the item as it stands after the edit; when login is still one of its assignees, GitHub
// ignored the change, and ErrEditIgnored is returned.
func (c *Client) RemoveAssignee(ctx context.Context, tok Token, ref item.Ref, login string) (*Edited, error) {
	edited, err := c.removeAssignee(ctx, tok, ref, login)

	return edited, Redact(err, tok)
}

// RemoveReviewRequest takes login off the reviewers a pull request asks, with one REST
// request (DELETE /repos/{owner}/{repo}/pulls/{number}/requested_reviewers). GitHub
// answers with who the pull request still asks: when that is still login by name, GitHub
// ignored the change, and ErrEditIgnored is returned. A team login is in is not changed:
// GitHub takes a team's request off for every member or for none.
func (c *Client) RemoveReviewRequest(ctx context.Context, tok Token, ref item.Ref, login string) (*Edited, error) {
	edited, err := c.removeReviewRequest(ctx, tok, ref, login)

	return edited, Redact(err, tok)
}

// FetchViewer returns the login of the account tok belongs to, with a one-point GraphQL
// query, so an edit can be checked against the account whose inbox the user sees.
func (c *Client) FetchViewer(ctx context.Context, tok Token) (string, error) {
	login, err := c.fetchViewer(ctx, tok)

	return login, Redact(err, tok)
}

// viewerQuery reads the token's account, and the query's cost, which the budget counts.
const viewerQuery = `query Viewer {
  viewer { login }
  rateLimit { cost remaining resetAt limit used }
}`

// viewerData is the viewer query's data object.
type viewerData struct {
	Viewer graphQLViewer `json:"viewer"`
}

// fetchViewer sends the viewer query.
func (c *Client) fetchViewer(ctx context.Context, tok Token) (string, error) {
	resp, _, err := postGraphQL[viewerData](ctx, c, tok, "viewer", graphQLRequest{Query: viewerQuery}, anyError)
	if err != nil {
		return "", err
	}

	if resp.Data.Viewer.Login == "" {
		return "", fmt.Errorf("%w: the viewer has no login", ErrMalformedResponse)
	}

	return resp.Data.Viewer.Login, nil
}

// restLogin is a user or bot in a REST answer.
type restLogin struct {
	Login string `json:"login"`
}

// removeAssignee sends the edit and checks GitHub's answer.
func (c *Client) removeAssignee(ctx context.Context, tok Token, ref item.Ref, login string) (*Edited, error) {
	if err := validateEdit(ref, login); err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string][]string{"assignees": {login}})
	if err != nil {
		return nil, fmt.Errorf("encode the edit: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodDelete, issuePath(ref)+"/assignees", body)
	if err != nil {
		return nil, editError(ref, fmt.Errorf("unassign %s from %s: %w", login, ref, err))
	}

	var answer struct {
		UpdatedAt time.Time   `json:"updated_at"`
		Assignees []restLogin `json:"assignees"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %w", ErrMalformedResponse, ref, err)
	}

	for _, a := range answer.Assignees {
		if strings.EqualFold(a.Login, login) {
			return nil, uierr.New(
				fmt.Sprintf("GitHub kept you assigned to %s. Your account may not be allowed to change its assignees.", ref),
				fmt.Errorf("unassign %s from %s: %w", login, ref, ErrEditIgnored),
			)
		}
	}

	return &Edited{UpdatedAt: answer.UpdatedAt.UTC()}, nil
}

// removeReviewRequest sends the edit and checks GitHub's answer.
func (c *Client) removeReviewRequest(ctx context.Context, tok Token, ref item.Ref, login string) (*Edited, error) {
	if err := validateEdit(ref, login); err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string][]string{"reviewers": {login}})
	if err != nil {
		return nil, fmt.Errorf("encode the edit: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodDelete, pullPath(ref)+"/requested_reviewers", body)
	if err != nil {
		return nil, editError(ref, fmt.Errorf("remove %s from the reviewers of %s: %w", login, ref, err))
	}

	var answer restReviewAnswer
	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %w", ErrMalformedResponse, ref, err)
	}

	still := answer.requests()
	if still.Asks(login) {
		return nil, uierr.New(
			fmt.Sprintf("GitHub still asks you to review %s. Your account may not be allowed to change its reviewers.", ref),
			fmt.Errorf("remove %s from the reviewers of %s: %w", login, ref, ErrEditIgnored),
		)
	}

	return &Edited{UpdatedAt: answer.UpdatedAt.UTC(), ReviewRequests: still}, nil
}

// restReviewAnswer is GitHub's answer to an edit of a pull request's review requests: the
// pull request, as it stands after.
type restReviewAnswer struct {
	UpdatedAt          time.Time   `json:"updated_at"`
	RequestedReviewers []restLogin `json:"requested_reviewers"`
	RequestedTeams     []struct {
		Slug string `json:"slug"`
	} `json:"requested_teams"`
}

// requests lists who the pull request still asks to review, skipping empty entries.
func (a restReviewAnswer) requests() ReviewRequests {
	out := ReviewRequests{Users: []string{}, Teams: []string{}}

	for _, r := range a.RequestedReviewers {
		if r.Login != "" {
			out.Users = append(out.Users, r.Login)
		}
	}

	for _, t := range a.RequestedTeams {
		if t.Slug != "" {
			out.Teams = append(out.Teams, t.Slug)
		}
	}

	return out
}

// edit sends one edit through do, once the client's WriteGate, if any, allows it, and
// tells the gate when GitHub asks the app to slow down. An edit with no token is never
// sent, so it is not counted.
func (c *Client) edit(ctx context.Context, tok Token, method, path string, body []byte) ([]byte, error) {
	if c.Writes != nil && !tok.IsZero() {
		if err := c.Writes.AllowWrite(ResourceCore); err != nil {
			return nil, err
		}
	}

	raw, _, err := c.do(ctx, tok, method, path, body)

	if wait := writeHold(err); wait > 0 && c.Writes != nil {
		c.Writes.HoldWrites(wait)
	}

	return raw, err
}

// mutate sends one GraphQL mutation, named name in errors, once the client's WriteGate,
// if any, allows it, and decodes its data into T. It is edit's twin for GraphQL: the gate
// counts it against the GraphQL budget, it spends one point, and an answer that asks the
// app to slow down holds the next edits. Any GraphQL error fails it. A mutation with no
// token is never sent, so it is not counted.
func mutate[T any](ctx context.Context, c *Client, tok Token, name string, req graphQLRequest) (*T, error) {
	if !isMutation(req.Query) {
		return nil, fmt.Errorf("%w: the %s request is not a mutation", ErrMalformedResponse, name)
	}

	if c.Writes != nil && !tok.IsZero() {
		if err := c.Writes.AllowWrite(ResourceGraphQL); err != nil {
			return nil, err
		}
	}

	resp, _, err := sendGraphQL[T](ctx, c, tok, name, req, anyError, func([]byte) int { return mutationCost })

	if wait := writeHold(err); wait > 0 && c.Writes != nil {
		c.Writes.HoldWrites(wait)
	}

	if err != nil {
		return nil, err
	}

	return resp.Data, nil
}

// writeHold is how long GitHub asked the app to send no more edits, from its answer to
// one: what its Retry-After said, or a minute for a rate limit that did not say; 0 for
// any other answer.
func writeHold(err error) time.Duration {
	if failed, ok := errors.AsType[*queryError](err); ok && failed.limited {
		return secondaryLimitWait
	}

	status, ok := errors.AsType[*StatusError](err)
	if !ok {
		return 0
	}

	if status.RetryAfter > 0 {
		return status.RetryAfter
	}

	if rateLimited(status) {
		return secondaryLimitWait
	}

	return 0
}

// rateLimited reports an answer that refused a request for GitHub's rate limits rather
// than for the request itself.
func rateLimited(status *StatusError) bool {
	return status.StatusCode == http.StatusTooManyRequests ||
		(status.StatusCode == http.StatusForbidden && strings.Contains(strings.ToLower(status.Message), "rate limit"))
}

// editError gives a refused edit to ref a message the user can act on, keeping err's
// chain. A refusal it has no better words for, a token GitHub refused, and a request that
// got no answer keep what they say.
func editError(ref item.Ref, err error) error {
	status, ok := errors.AsType[*StatusError](err)
	if !ok || IsBadCredentials(err) {
		return err
	}

	switch {
	case rateLimited(status):
		return uierr.New("GitHub is limiting how fast your account can make changes. Try again in a minute.", err)
	case status.StatusCode == http.StatusNotFound:
		return uierr.New(fmt.Sprintf("GitHub could not find %s, or your account cannot see it.", ref), err)
	case status.StatusCode == http.StatusForbidden && status.Message != "":
		return uierr.New(fmt.Sprintf("GitHub refused the change to %s: %s", ref, sentence(status.Message)), err)
	case status.StatusCode == http.StatusForbidden:
		return uierr.New(fmt.Sprintf("GitHub refused the change to %s. Your account may not be allowed to make it.", ref), err)
	default:
		return err
	}
}

// writeError gives a refused review, merge, auto-merge, or branch update of ref a message
// the user can act on, and the sentinel that says what went wrong, keeping err's chain: a
// push since the head the write was pinned to (409, or a 422 about the expected head), a
// merge GitHub won't make yet (405, or a merge queue), a review of the user's own pull
// request, a pending review that blocks another, and a line comment off the diff. Any
// other refusal is worded by editError. A close has its own wording (closeError).
func writeError(ref item.Ref, err error) error {
	status, ok := errors.AsType[*StatusError](err)
	if !ok || IsBadCredentials(err) {
		return err
	}

	switch status.StatusCode {
	case http.StatusConflict:
		return pushedSince(ref, err)
	case http.StatusMethodNotAllowed:
		return notMergeable(ref, status.Message, err)
	case http.StatusUnprocessableEntity:
		return unprocessable(ref, status.Message, strings.ToLower(status.Message), err)
	default:
		return editError(ref, err)
	}
}

// unprocessable words a write of ref GitHub refused as unprocessable (422), from its
// message, and msg, the message in lower case.
func unprocessable(ref item.Ref, message, msg string, err error) error {
	switch {
	case strings.Contains(msg, "expected head"):
		return pushedSince(ref, err)
	case strings.Contains(msg, "merge queue"):
		return notMergeable(ref, message, err)
	case strings.Contains(msg, "your own pull request"):
		return uierr.New(fmt.Sprintf("GitHub doesn't let you approve or request changes on your own pull request, %s.", ref),
			fmt.Errorf("%w: %w", ErrOwnPull, err))
	case strings.Contains(msg, "pending review"):
		return uierr.New(fmt.Sprintf("You have a review of %s started on GitHub and not submitted. "+
			"Submit or discard it there, then try again.", ref), fmt.Errorf("%w: %w", ErrPendingReview, err))
	case strings.Contains(msg, "part of the diff") || strings.Contains(msg, "could not be resolved"):
		return uierr.New(fmt.Sprintf("GitHub refused a line comment on %s: a line it names isn't part of the diff.", ref),
			fmt.Errorf("%w: %w", ErrLineNotInDiff, err))
	case message != "":
		return uierr.New(fmt.Sprintf("GitHub refused the change to %s: %s", ref, sentence(message)), err)
	default:
		return err
	}
}

// pushedSince is a write GitHub refused because ref was pushed to after the head the write
// was pinned to.
func pushedSince(ref item.Ref, err error) error {
	return uierr.New(fmt.Sprintf("%s was pushed to since you opened it, so nothing was changed. "+
		"Reload it to see the new commits.", ref), fmt.Errorf("%w: %w", ErrPushedSince, err))
}

// notMergeable is a merge GitHub refused for what ref still needs, in GitHub's own words.
func notMergeable(ref item.Ref, reason string, err error) error {
	refused := &NotMergeableError{Reason: reason, MergeQueue: strings.Contains(strings.ToLower(reason), "merge queue"), cause: err}

	if reason == "" {
		return uierr.New(fmt.Sprintf("GitHub won't merge %s yet.", ref), refused)
	}

	return uierr.New(fmt.Sprintf("GitHub won't merge %s yet: %s", ref, sentence(reason)), refused)
}

// sentence ends GitHub's message with a full stop, unless it ends a sentence already.
func sentence(msg string) string {
	if strings.HasSuffix(msg, ".") || strings.HasSuffix(msg, "!") || strings.HasSuffix(msg, "?") {
		return msg
	}

	return msg + "."
}

// validateEdit checks an edit's item and login before anything is sent.
func validateEdit(ref item.Ref, login string) error {
	if err := validateRef(ref); err != nil {
		return err
	}

	if !repo.ValidOwner(login) {
		return fmt.Errorf("%w: %q", ErrInvalidLogin, login)
	}

	return nil
}

// issuePath is an issue's REST path, with its owner and name escaped. A pull request is
// an issue there too.
func issuePath(ref item.Ref) string {
	return "/repos/" + url.PathEscape(ref.Repo.Owner) + "/" + url.PathEscape(ref.Repo.Repo) + "/issues/" +
		strconv.Itoa(ref.Number)
}
