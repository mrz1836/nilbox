package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// statesBatch is the most items one states request asks about. Each is a single object,
// so a request costs one point whatever its size; the cap only keeps a request small.
// Requests are sent side by side, at most statesParallel at a time, like the details
// requests. Each ref is asked about under statesPrefix and its index, as in r0.
//
// The same requests carry FetchLinkStates' head asks, at most headsBatch each, under
// headsPrefix and their index, as in h0. Each is a connection of at most headsPerBranch
// pull requests, enough that the repository's own one isn't lost behind forks' pull requests
// of a branch of the same name, as main or a common name has, updated since; GitHub counts
// a connection as one call whatever its size, so a request full of them still costs a point.
const (
	statesBatch    = 50
	statesParallel = detailsParallel
	statesPrefix   = "r"

	headsBatch     = 50
	headsPerBranch = 20
	headsPrefix    = "h"
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

// headFields is what the states query reads about each pull request a head ask finds:
// what StateFields reads of a pull request, its number, and the repository its head
// branch is in, so a fork's branch of the same name is told apart.
const headFields = "number title url state closedAt mergedAt headRefOid headRefName headRepository { nameWithOwner }"

// The keys fetchStates answers under, before FetchLinkStates splits them: an item's under
// itemKey and its ref's Key, a head ask's under headKey and its HeadKey.
const (
	itemKey = "item\x00"
	headKey = "head\x00"
)

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

// HeadQuery asks which pull requests of a repository have a branch of a given name as
// their head.
type HeadQuery struct {
	Repo   repo.Name
	Branch string
}

// HeadPR is a pull request whose head branch had the name a HeadQuery asked about, as
// GitHub reports it now.
type HeadPR struct {
	// Number is the pull request's number in the repository asked about.
	Number int

	// State is what GitHub says about it, as FetchItemStates reads a pull request.
	State ItemState

	// HeadRepo is the repository its head branch is in, as owner/name, or "" when GitHub
	// doesn't say, as for a fork that was deleted.
	HeadRepo string
}

// HeadKey is the key FetchLinkStates answers a repository's branch under.
func HeadKey(r repo.Name, branch string) string {
	return r.Key() + "\x00" + branch
}

// FetchItemStates reads the state of each pull request or issue in refs, as of now, keyed
// by each ref's Key, so a ref finds its answer whatever its casing. A ref GitHub cannot
// resolve (a number that does not exist, a repository that is gone or that the token
// cannot see) is left out of the result. Any other error fails the whole call, so a
// passing problem never makes an item look done. A ref's Kind is not needed: the number
// alone picks the item. A ref that names no item fails the call before anything is sent.
func (c *Client) FetchItemStates(ctx context.Context, tok Token, refs []item.Ref) (map[string]ItemState, error) {
	states, _, err := c.FetchLinkStates(ctx, tok, refs, nil)

	return states, err
}

// FetchLinkStates reads the state of each pull request or issue in refs as FetchItemStates
// does and, in the same requests, the pull requests of each repository heads asks about
// whose head branch had its name: in any state, newest updated first, at most three, under
// HeadKey. Branches asked about beside items cost no request of their own, and branches
// alone one request per fifty. A name no pull request had is left out, and so is a
// repository GitHub cannot resolve; a name that is empty or not valid UTF-8 is never asked
// about, since a query could only ask about it rewritten, and so about another branch. Any
// other error fails the whole call, so a passing problem never links a worktree to the
// wrong pull request. Both maps are never nil.
func (c *Client) FetchLinkStates(ctx context.Context, tok Token, refs []item.Ref, heads []HeadQuery,
) (map[string]ItemState, map[string][]HeadPR, error) {
	heads = slices.DeleteFunc(slices.Clone(heads), func(h HeadQuery) bool {
		return h.Branch == "" || !utf8.ValidString(h.Branch)
	})

	count := max(batches(len(refs), statesBatch), batches(len(heads), headsBatch))
	queries := make([]statesRequest, 0, count)

	for i := range count {
		req := statesRequest{refs: chunk(refs, i, statesBatch), heads: chunk(heads, i, headsBatch)}

		query, err := statesQuery(req.refs, req.heads)
		if err != nil {
			return nil, nil, err
		}

		req.query = query
		queries = append(queries, req)
	}

	answers, err := fanOut(ctx, queries, statesParallel,
		func(ctx context.Context, req statesRequest) (map[string]statesAnswer, error) {
			return c.fetchStates(ctx, tok, req)
		})
	if err != nil {
		return nil, nil, Redact(err, tok)
	}

	states := make(map[string]ItemState, len(refs))
	prs := make(map[string][]HeadPR, len(heads))

	for key, answer := range answers {
		if ref, ok := strings.CutPrefix(key, itemKey); ok {
			states[ref] = answer.state
		} else if head, ok := strings.CutPrefix(key, headKey); ok {
			prs[head] = answer.heads
		}
	}

	return states, prs, nil
}

// batches is how many batches of at most size n things take.
func batches(n, size int) int {
	return (n + size - 1) / size
}

// chunk is the i-th batch of at most size of list, empty past its end.
func chunk[T any](list []T, i, size int) []T {
	return list[min(i*size, len(list)):min((i+1)*size, len(list))]
}

// statesRequest is one states query and the refs and head asks it asks about, in alias
// order.
type statesRequest struct {
	refs  []item.Ref
	heads []HeadQuery
	query string
}

// statesAnswer is one answer of a states query: an item's state, under itemKey and its
// ref's Key, or the pull requests a head ask found, under headKey and its HeadKey.
type statesAnswer struct {
	state ItemState
	heads []HeadPR
}

// fetchStates sends one states query and returns its answers (see statesAnswer). GitHub's
// not-found errors only leave a ref or a head ask unanswered; any other error fails it.
func (c *Client) fetchStates(ctx context.Context, tok Token, req statesRequest) (map[string]statesAnswer, error) {
	resp, _, err := postGraphQL[statesData](ctx, c, tok, "states", graphQLRequest{Query: req.query}, unlessNotFound)
	if err != nil {
		return nil, err
	}

	answers := make(map[string]statesAnswer, len(req.refs)+len(req.heads))

	for i, ref := range req.refs {
		if state, ok := (*resp.Data)[batchAlias(statesPrefix, i)].state(); ok {
			answers[itemKey+ref.Key()] = statesAnswer{state: state}
		}
	}

	for i, head := range req.heads {
		if prs := (*resp.Data)[batchAlias(headsPrefix, i)].pullRequests(); len(prs) > 0 {
			answers[headKey+HeadKey(head.Repo, head.Branch)] = statesAnswer{heads: prs}
		}
	}

	return answers, nil
}

// statesQuery asks about each ref under its own alias, and about each head ask under its
// own: the pull requests of its repository whose head branch had its name. The repository
// is written by repoField and the branch by graphQLString, so no name can break out of its
// argument.
func statesQuery(refs []item.Ref, heads []HeadQuery) (string, error) {
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

	for i, head := range heads {
		field, err := headField(batchAlias(headsPrefix, i), head)
		if err != nil {
			return "", err
		}

		b.WriteString("  " + field + "\n")
	}

	// The query's own cost, so the API budget counts it; the key is not an alias, so no ref
	// looks it up.
	b.WriteString("  rateLimit { cost }\n}\n")

	// GitHub refuses a fragment the query never spreads.
	if len(refs) > 0 {
		b.WriteString(stateFieldsFragment)
	}

	return b.String(), nil
}

// headField is the field that asks, under alias, about the pull requests of head's
// repository whose head branch had its name.
func headField(alias string, head HeadQuery) (string, error) {
	field, err := repoField(alias, head.Repo)
	if err != nil {
		return "", err
	}

	branch, err := graphQLString(head.Branch)
	if err != nil {
		return "", fmt.Errorf("encode a branch of %s: %w", head.Repo, err)
	}

	return fmt.Sprintf("%s { pulls: pullRequests(headRefName: %s, states: [OPEN, MERGED, CLOSED], first: %d, "+
		"orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { %s } } }", field, branch, headsPerBranch, headFields), nil
}

// statesData is the states query's data object: one aliased repository per ref or head
// ask, null when GitHub could not find it.
type statesData map[string]*statesRepo

// statesRepo is one aliased repository: the item a ref asked about, or the pull requests a
// head ask found.
type statesRepo struct {
	Item  *statesNode     `json:"item"`
	Pulls *headConnection `json:"pulls"`
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

// headConnection is the pull requests a head ask found.
type headConnection struct {
	Nodes []*headNode `json:"nodes"`
}

// headNode is one pull request a head ask found.
type headNode struct {
	Number     int       `json:"number"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	State      string    `json:"state"`
	ClosedAt   time.Time `json:"closedAt"`
	MergedAt   time.Time `json:"mergedAt"`
	HeadRefOID string    `json:"headRefOid"`
	HeadRef    string    `json:"headRefName"`

	HeadRepository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"headRepository"`
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

// pullRequests converts the answer for one head ask, in GitHub's order, leaving out a node
// that is null, or that names no pull request or no state; none when there is no answer.
// A merged pull request is dated by its merge, as state dates one.
func (r *statesRepo) pullRequests() []HeadPR {
	if r == nil || r.Pulls == nil {
		return nil
	}

	out := make([]HeadPR, 0, len(r.Pulls.Nodes))

	for _, n := range r.Pulls.Nodes {
		if n == nil || n.Number <= 0 || n.State == "" {
			continue
		}

		pr := HeadPR{Number: n.Number, State: ItemState{
			PullRequest: true, Title: n.Title, URL: n.URL, State: n.State, ClosedAt: n.ClosedAt.UTC(),
			HeadOID: n.HeadRefOID, HeadRef: n.HeadRef,
		}}

		if !n.MergedAt.IsZero() {
			pr.State.ClosedAt = n.MergedAt.UTC()
		}

		if n.HeadRepository != nil {
			pr.HeadRepo = n.HeadRepository.NameWithOwner
		}

		out = append(out, pr)
	}

	return out
}
