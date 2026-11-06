package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
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

// ErrInvalidItemRef is returned for an ItemRef whose number is not positive; it is never
// sent.
var ErrInvalidItemRef = errors.New("invalid pull request or issue reference")

// ItemRef names a pull request or issue by its repository and number. GitHub's numbers
// are shared by a repository's pull requests and issues, so the number alone picks one.
type ItemRef struct {
	Owner  string
	Name   string
	Number int
}

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

// FetchItemStates reads the state of each pull request or issue in refs, as of now. A ref
// GitHub cannot resolve (a number that does not exist, a repository that is gone or that
// the token cannot see) is left out of the result. Any other error fails the whole call,
// so a passing problem never makes an item look done.
func (c *Client) FetchItemStates(ctx context.Context, tok Token, refs []ItemRef) (map[ItemRef]ItemState, error) {
	states := make(map[ItemRef]ItemState, len(refs))

	for start := 0; start < len(refs); start += statesBatch {
		batch := refs[start:min(start+statesBatch, len(refs))]

		if err := c.fetchStates(ctx, tok, batch, states); err != nil {
			return nil, Redact(err, tok)
		}
	}

	return states, nil
}

// fetchStates sends one states query and adds its answers to states.
func (c *Client) fetchStates(ctx context.Context, tok Token, refs []ItemRef, states map[ItemRef]ItemState) error {
	query, err := statesQuery(refs)
	if err != nil {
		return err
	}

	body, err := json.Marshal(graphQLRequest{Query: query})
	if err != nil {
		return fmt.Errorf("encode the states query: %w", err)
	}

	raw, _, err := c.do(ctx, tok, http.MethodPost, "/graphql", body)
	if err != nil {
		return err
	}

	resp, err := decodeStates(raw)
	if err != nil {
		return err
	}

	for i, ref := range refs {
		if state, ok := resp.Data[statesAlias(i)].state(); ok {
			states[ref] = state
		}
	}

	return nil
}

// decodeStates reads a states response. GitHub's not-found errors only leave a ref
// unanswered; any other error fails it.
func decodeStates(raw []byte) (statesResponse, error) {
	var resp statesResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return statesResponse{}, fmt.Errorf("%w: decode the states response: %w", ErrMalformedResponse, err)
	}

	for _, e := range resp.Errors {
		if e.Type != errorNotFound {
			return statesResponse{}, (&graphQLResponse{Errors: resp.Errors}).err()
		}
	}

	if resp.Data == nil {
		return statesResponse{}, fmt.Errorf("%w: the states response has no data", ErrMalformedResponse)
	}

	return resp, nil
}

// statesQuery asks about each ref under its own alias. The repository names are written
// as JSON strings, which GraphQL reads the same way, so no name can break out of its
// argument.
func statesQuery(refs []ItemRef) (string, error) {
	var b strings.Builder

	b.WriteString("query ItemStates {\n")

	for i, ref := range refs {
		if ref.Number <= 0 {
			return "", fmt.Errorf("%w: number %d", ErrInvalidItemRef, ref.Number)
		}

		owner, err := json.Marshal(ref.Owner)
		if err != nil {
			return "", fmt.Errorf("encode the owner: %w", err)
		}

		name, err := json.Marshal(ref.Name)
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

// statesResponse is the states query's response body: one aliased repository per ref,
// null when GitHub could not find it.
type statesResponse struct {
	Data   map[string]*statesRepo `json:"data"`
	Errors []graphQLError         `json:"errors"`
}

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
