package github

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/item"
)

// statesBatch is the most items one states request asks about. Each is a single object,
// so a request costs one point whatever its size; the cap only keeps a request small.
// Requests are sent side by side, at most statesParallel at a time, like the details
// requests. Each ref is asked about under statesPrefix and its index, as in r0.
const (
	statesBatch    = 50
	statesParallel = detailsParallel
	statesPrefix   = "r"
)

// stateFieldsFragment is what the states query reads about a pull request or issue: just
// enough to tell whether it is done, and, for a pull request, the commit it ended on.
const stateFieldsFragment = `
fragment StateFields on IssueOrPullRequest {
  __typename
  ... on PullRequest { title url prState: state closedAt mergedAt headRefOid headRefName }
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

	// HeadRef is the name of a pull request's head branch, as in feature-x; "" for an
	// issue.
	HeadRef string
}

// FetchItemStates reads the state of each pull request or issue in refs, as of now, keyed
// by each ref's Key, so a ref finds its answer whatever its casing. A ref GitHub cannot
// resolve (a number that does not exist, a repository that is gone or that the token
// cannot see) is left out of the result. Any other error fails the whole call, so a
// passing problem never makes an item look done. A ref's Kind is not needed: the number
// alone picks the item. A ref that names no item fails the call before anything is sent.
func (c *Client) FetchItemStates(ctx context.Context, tok Token, refs []item.Ref) (map[string]ItemState, error) {
	var queries []statesRequest

	for batch := range slices.Chunk(refs, statesBatch) {
		query, err := statesQuery(batch)
		if err != nil {
			return nil, err
		}

		queries = append(queries, statesRequest{refs: batch, query: query})
	}

	answers := make([]map[string]ItemState, len(queries))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(statesParallel)

	for i, req := range queries {
		g.Go(func() error {
			var err error

			answers[i], err = c.fetchStates(gctx, tok, req)

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, Redact(err, tok)
	}

	states := make(map[string]ItemState, len(refs))
	for _, answer := range answers {
		maps.Copy(states, answer)
	}

	return states, nil
}

// statesRequest is one states query and the refs it asks about, in alias order.
type statesRequest struct {
	refs  []item.Ref
	query string
}

// fetchStates sends one states query and returns its answers by ref key. GitHub's
// not-found errors only leave a ref unanswered; any other error fails it.
func (c *Client) fetchStates(ctx context.Context, tok Token, req statesRequest) (map[string]ItemState, error) {
	resp, _, err := postGraphQL[statesData](ctx, c, tok, "states", graphQLRequest{Query: req.query}, unlessNotFound)
	if err != nil {
		return nil, err
	}

	states := make(map[string]ItemState, len(req.refs))

	for i, ref := range req.refs {
		if state, ok := (*resp.Data)[batchAlias(statesPrefix, i)].state(); ok {
			states[ref.Key()] = state
		}
	}

	return states, nil
}

// statesQuery asks about each ref under its own alias. The repository is written by
// repoField, so no name can break out of its argument.
func statesQuery(refs []item.Ref) (string, error) {
	var b strings.Builder

	b.WriteString("query ItemStates {\n")

	for i, ref := range refs {
		if ref.Number <= 0 {
			return "", fmt.Errorf("%w: number %d", ErrInvalidItemRef, ref.Number)
		}

		field, err := repoField(batchAlias(statesPrefix, i), ref.Repo)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(&b, "  %s { item: issueOrPullRequest(number: %s) { ...StateFields } }\n", field, strconv.Itoa(ref.Number))
	}

	// The query's own cost, so the API budget counts it; the key is not an alias, so no ref
	// looks it up.
	b.WriteString("  rateLimit { cost }\n}\n")
	b.WriteString(stateFieldsFragment)

	return b.String(), nil
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
	HeadRef    string    `json:"headRefName"`
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
		out.PullRequest, out.State, out.HeadOID, out.HeadRef = true, n.PRState, n.HeadRefOID, n.HeadRef
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
