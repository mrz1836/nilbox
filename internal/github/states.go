package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/item"
)

// statesBatch is the most items one states request asks about. Each is a single object,
// so a request costs one point whatever its size; the cap only keeps a request small.
const statesBatch = 50

// stateFieldsFragment is what the states query reads about a pull request or issue: just
// enough to tell whether it is done, and, for a pull request, the commit it ended on.
const stateFieldsFragment = `
fragment StateFields on IssueOrPullRequest {
  __typename
  ... on PullRequest { title url prState: state closedAt mergedAt headRefOid }
  ... on Issue { title url issueState: state closedAt }
}
`

// ErrInvalidItemRef is returned for an item.Ref that names no pull request or issue; it
// is never sent.
var ErrInvalidItemRef = errors.New("invalid pull request or issue reference")

// ItemState is what GitHub says about a pull request or issue as of now.
type ItemState struct {
	// PullRequest reports a pull request; false is an issue.
	PullRequest bool

	Title string
	URL   string

	// State is StateOpen, StateClosed, or StateMerged.
	State string

	// ClosedAt is when the item was merged or closed, or zero while it is open.
	ClosedAt time.Time

	// HeadOID is a pull request's head commit SHA; "" for an issue.
	HeadOID string
}

// FetchItemStates reads the state of each pull request or issue in refs, as of now, keyed
// by each ref's Key, so a ref finds its answer whatever its casing. A ref GitHub cannot
// resolve (a number that does not exist, a repository that is gone or that the token
// cannot see) is left out of the result. Any other error fails the whole call, so a
// passing problem never makes an item look done. A ref's Kind is not needed: the number
// alone picks the item.
func (c *Client) FetchItemStates(ctx context.Context, tok Token, refs []item.Ref) (map[string]ItemState, error) {
	states := make(map[string]ItemState, len(refs))

	for start := 0; start < len(refs); start += statesBatch {
		batch := refs[start:min(start+statesBatch, len(refs))]

		if err := c.fetchStates(ctx, tok, batch, states); err != nil {
			return nil, Redact(err, tok)
		}
	}

	return states, nil
}

// fetchStates sends one states query and adds its answers to states.
// fetchStates sends one states query and adds its answers to states. GitHub's not-found
// errors only leave a ref unanswered; any other error fails it.
func (c *Client) fetchStates(ctx context.Context, tok Token, refs []item.Ref, states map[string]ItemState) error {
	query, err := statesQuery(refs)
	if err != nil {
		return err
	}

	resp, _, err := postGraphQL[statesData](ctx, c, tok, "states", graphQLRequest{Query: query}, unlessNotFound)
	if err != nil {
		return err
	}

	for i, ref := range refs {
		if state, ok := (*resp.Data)[statesAlias(i)].state(); ok {
			states[ref.Key()] = state
		}
	}

	return nil
}

// statesQuery asks about each ref under its own alias. The repository names are written
// as JSON strings, which GraphQL reads the same way, so no name can break out of its
// argument.
func statesQuery(refs []item.Ref) (string, error) {
	var b strings.Builder

	b.WriteString("query ItemStates {\n")

	for i, ref := range refs {
		if ref.Number <= 0 {
			return "", fmt.Errorf("%w: number %d", ErrInvalidItemRef, ref.Number)
		}

		owner, err := json.Marshal(ref.Repo.Owner)
		if err != nil {
			return "", fmt.Errorf("encode the owner: %w", err)
		}

		name, err := json.Marshal(ref.Repo.Repo)
		if err != nil {
			return "", fmt.Errorf("encode the name: %w", err)
		}

		fmt.Fprintf(&b, "  %s: repository(owner: %s, name: %s) { item: issueOrPullRequest(number: %s) { ...StateFields } }\n",
			statesAlias(i), owner, name, strconv.Itoa(ref.Number))
	}

	b.WriteString("}\n")
	b.WriteString(stateFieldsFragment)

	return b.String(), nil
}

// statesAlias is the alias of the i-th ref in a states query.
func statesAlias(i int) string {
	return "r" + strconv.Itoa(i)
}

// statesData is the states query's data object: one aliased repository per ref, null
// when GitHub could not find it.
type statesData map[string]*statesRepo

// statesRepo is one aliased repository and the item asked about.
type statesRepo struct {
	Item *statesNode `json:"item"`
}

// statesNode is a pull request or an issue.
type statesNode struct {
	TypeName   string    `json:"__typename"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	PRState    string    `json:"prState"`
	IssueState string    `json:"issueState"`
	ClosedAt   time.Time `json:"closedAt"`
	MergedAt   time.Time `json:"mergedAt"`
	HeadRefOID string    `json:"headRefOid"`
}

// state converts the answer for one ref; ok is false when there is none.
func (r *statesRepo) state() (ItemState, bool) {
	if r == nil || r.Item == nil {
		return ItemState{}, false
	}

	n := r.Item
	out := ItemState{Title: n.Title, URL: n.URL, ClosedAt: n.ClosedAt.UTC()}

	switch n.TypeName {
	case typePullRequest:
		out.PullRequest, out.State, out.HeadOID = true, n.PRState, n.HeadRefOID
		if !n.MergedAt.IsZero() {
			out.ClosedAt = n.MergedAt.UTC()
		}
	case typeIssue:
		out.State = n.IssueState
	default:
		return ItemState{}, false
	}

	return out, out.State != ""
}
