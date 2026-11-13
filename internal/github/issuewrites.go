package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Why the issue sheet closes an issue, as GitHub's REST API takes it. A close as a
// duplicate names its duplicate, which the sheet doesn't offer.
const (
	IssueReasonCompleted  = "completed"
	IssueReasonNotPlanned = "not_planned"
)

// IssueClosed is GitHub's answer to the close of an issue.
type IssueClosed struct {
	// Reason is what GitHub says the issue is closed as, in its REST words
	// (IssueReasonCompleted, IssueReasonNotPlanned, or "duplicate"), or "" when it doesn't
	// say. It is the reason asked for, unless someone closed the issue first: GitHub
	// ignores a reason unless the close changes the issue's state.
	Reason string
}

// CloseIssue closes the issue ref as reason, IssueReasonCompleted or
// IssueReasonNotPlanned, with one REST request (PATCH /repos/{owner}/{repo}/issues/{number}),
// and returns what GitHub says it is closed as. One GitHub left open returns
// ErrEditIgnored, and a refusal is worded as a close (closeError). Any other reason, and a
// ref whose kind is not an issue, are refused before anything is sent: the same path closes
// a pull request, which only ClosePull does. GitHub takes nothing to pin a close to, so a
// caller reads the issue first (FetchIssueState) and closes only over what the user saw.
func (c *Client) CloseIssue(ctx context.Context, tok Token, ref item.Ref, reason string) (*IssueClosed, error) {
	closed, err := c.closeIssue(ctx, tok, ref, reason)

	return closed, Redact(err, tok)
}

// closeIssue checks the issue and the reason, and sends the close.
func (c *Client) closeIssue(ctx context.Context, tok Token, ref item.Ref, reason string) (*IssueClosed, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	if ref.Kind != item.KindIssue {
		return nil, fmt.Errorf("%w: %s is not an issue", ErrInvalidItemRef, ref)
	}

	if reason != IssueReasonCompleted && reason != IssueReasonNotPlanned {
		return nil, fmt.Errorf("%w: close reason %q", ErrInvalidWrite, reason)
	}

	answer, err := c.closeItem(ctx, tok, ref, issuePath(ref), map[string]string{"state": "closed", "state_reason": reason})
	if err != nil {
		return nil, err
	}

	return &IssueClosed{Reason: strings.ToLower(answer.StateReason)}, nil
}

// closedItem is GitHub's answer to a close: the item's state, and an issue's reason.
type closedItem struct {
	State       string `json:"state"`
	StateReason string `json:"state_reason"`
}

// closeItem sends the close of ref to its REST path, with the fields change sets, and
// checks GitHub's answer: one that says it is still open is a close GitHub ignored.
func (c *Client) closeItem(ctx context.Context, tok Token, ref item.Ref, path string, change map[string]string) (closedItem, error) {
	body, err := json.Marshal(change)
	if err != nil {
		return closedItem{}, fmt.Errorf("encode the close: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodPatch, path, body)
	if err != nil {
		return closedItem{}, closeError(ref, fmt.Errorf("close %s: %w", ref, err))
	}

	var answer closedItem

	if err = json.Unmarshal(raw, &answer); err != nil {
		return closedItem{}, fmt.Errorf("%w: decode the close of %s: %w", ErrMalformedResponse, ref, err)
	}

	if !strings.EqualFold(answer.State, "closed") {
		return closedItem{}, uierr.New(fmt.Sprintf("GitHub did not close %s.", ref), fmt.Errorf("close %s: %w", ref, ErrEditIgnored))
	}

	return answer, nil
}
