package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// The routines' searches are packed into requests, side by side, rather than sent as one.
// GitHub resolves a request's aliases one after another and answers a request that runs
// past about 10 seconds with a 502: measured in November 2026, five searches of 295 results
// in one request failed that way every time, while a 100-result search alone took 2 to 4
// seconds with WatchFields. So searches are packed by how many results each returned last
// time (a new one counts as a full page), up to watchPackResults a request and
// watchPackSearches aliases, and at most watchParallel requests are in flight. Each
// request costs one point, whatever it holds.
const (
	watchPackResults  = 120
	watchPackSearches = 10
	watchParallel     = 4

	// watchPage is the most results one search returns: GitHub's own limit (first: 101 is
	// refused with EXCESSIVE_PAGINATION).
	watchPage = 100

	// watchPrefix starts each search's alias, as in w0.
	watchPrefix = "w"
)

// NewSearch is a WatchSearch's Last when the search has no answer yet: it is packed as a
// full page.
const NewSearch = -1

// watchFieldsFragment is what a routine's search reads about each result: what the inbox
// stores of an item (its own fields, as ItemFields reads them, with its state and when it
// closed), its head commit, its base branch, its checks' overall state, and its
// repository's identity and whether it is archived. It is lean on purpose: the checks'
// state is in it because finished checks don't move an item's update time, and the head
// commit and base branch cost nothing, while the merge state, the review decision,
// mergeable, and the repository's other fields cost GitHub time per result (measured in
// November 2026), so they come with the details, read by ID, only for the watched items
// that are new or changed.
const watchFieldsFragment = `
fragment WatchFields on SearchResultItem {
  __typename
  ... on PullRequest {
    id number title url createdAt updatedAt isDraft headRefName
    prState: state closedAt
    authorAssociation isCrossRepository
    author { __typename login }
    headRefOid baseRefName statusCheckRollup { state }
    repository { id name owner { login } isArchived }
  }
  ... on Issue {
    id number title url createdAt updatedAt
    issueState: state closedAt
    authorAssociation
    author { __typename login }
    repository { id name owner { login } isArchived }
  }
}
`

// ErrSearchFailed is a WatchResult's error when GitHub answered the request its search was
// in but not the search itself.
var ErrSearchFailed = errors.New("GitHub did not answer the search")

// WatchSearch is one routine search a refresh sends.
type WatchSearch struct {
	// Key identifies the search: the routines that share it ask for it once.
	Key string

	// Query is what GitHub is sent, its qualifiers checked by the routines' builder.
	Query string

	// Last is how many results it returned last time, at most a page, or NewSearch.
	Last int
}

// expected is how many results the search is packed as: its last count, a full page for
// a new one, and at least one, since even an empty search takes GitHub some time.
func (s WatchSearch) expected() int {
	if s.Last < 0 || s.Last > watchPage {
		return watchPage
	}

	return max(s.Last, 1)
}

// Watches is what FetchWatches read.
type Watches struct {
	// Results holds each search's answer, by key: one for every search asked.
	Results map[string]WatchResult

	// RateLimit is the GraphQL budget after the latest request; its Cost is the sum of
	// every request's cost.
	RateLimit QueryRate

	// Requests is how many requests were sent.
	Requests int
}

// WatchResult is one search's answer.
type WatchResult struct {
	// IssueCount is GitHub's total match count, which can be more than the page it
	// returned (Truncated).
	IssueCount int

	// Items are the pull requests and issues it returned, most recently updated first.
	Items []WatchItem

	// Err is why the search has no answer: the request it was in failed, GitHub failed it
	// alone, or its query can't be sent. Items is empty then.
	Err error

	// Partial reports an answer GitHub couldn't read in full: a result it failed alone is
	// left out of Items, or it sent an error that names no search, so an item missing from
	// Items may still match.
	Partial bool
}

// Truncated reports a search that matched more than the page it returned: the items past
// it are not in Items.
func (r WatchResult) Truncated() bool {
	return r.Err == nil && r.IssueCount > len(r.Items)
}

// WatchItem is one result of a routine's search: the item, with its repository read only
// in part (its node ID, owner, name, and whether it is archived), and what tells whether it
// changed since the last read.
type WatchItem struct {
	Item

	// HeadOID is a pull request's head commit, and BaseRef the branch it merges into.
	HeadOID string
	BaseRef string

	// ChecksState is the head commit's overall check result (SUCCESS, FAILURE, ERROR,
	// PENDING, or EXPECTED), "" with no checks and for an issue.
	ChecksState string
}

// FetchWatches sends the routines' searches, packed into requests sent side by side (see
// watchPackResults), each search under its own alias. A request that fails, as with a
// 502's HTML answer or GitHub's errors with no data, fails each of its searches; an error
// about one alias, or its list of results, fails that search alone; an error about one
// result leaves that result out, and its search Partial; and a query that can't be sent
// fails its search before anything is. Each one's error is in its result, and the others
// carry on. It returns an error only when the token was refused, so the caller can borrow
// a fresh one, or ctx ended.
func (c *Client) FetchWatches(ctx context.Context, tok Token, searches []WatchSearch) (*Watches, error) {
	watches, err := c.fetchWatches(ctx, tok, searches)

	return watches, Redact(err, tok)
}

// fetchWatches sends each pack on its own goroutine and gathers their answers.
func (c *Client) fetchWatches(ctx context.Context, tok Token, searches []WatchSearch) (*Watches, error) {
	sendable, refused := encodable(searches)
	packs := packWatches(sendable)
	answers := make([]*watchAnswer, len(packs), len(packs)+1)

	var g errgroup.Group

	g.SetLimit(watchParallel)

	for i, pack := range packs {
		g.Go(func() error {
			answers[i] = c.sendWatches(ctx, tok, pack)

			return nil
		})
	}

	_ = g.Wait() // each pack reports its own outcome

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := &Watches{Results: make(map[string]WatchResult, len(searches))}

	for _, a := range append(answers, refused) {
		if IsBadCredentials(a.err) {
			return nil, a.err
		}

		out.add(a)
	}

	return out, nil
}

// encodable returns the searches whose query can be written into a request, in order, and
// an answer that fails each of the others, which is never sent.
func encodable(searches []WatchSearch) ([]WatchSearch, *watchAnswer) {
	out := make([]WatchSearch, 0, len(searches))
	refused := &watchAnswer{results: map[string]WatchResult{}}

	for _, s := range searches {
		if _, err := graphQLString(s.Query); err != nil {
			refused.results[s.Key] = WatchResult{Err: fmt.Errorf("encode the search %q: %w", s.Key, err), Items: []WatchItem{}}

			continue
		}

		out = append(out, s)
	}

	return out, refused
}

// add merges one pack's answer: its searches' results, its request, and its cost.
func (w *Watches) add(a *watchAnswer) {
	for key, r := range a.results {
		w.Results[key] = r
	}

	if !a.sent {
		return
	}

	cost := w.RateLimit.Cost + a.rate.Cost
	if w.Requests == 0 || a.rate.Later(w.RateLimit) {
		w.RateLimit = a.rate
	}

	w.RateLimit.Cost = cost
	w.Requests++
}

// WatchRequests returns how many requests FetchWatches sends for searches that returned
// lasts results each last time (NewSearch for a new one), packed as it packs them.
func WatchRequests(lasts []int) int {
	searches := make([]WatchSearch, 0, len(lasts))
	for i, last := range lasts {
		searches = append(searches, WatchSearch{Key: batchAlias(watchPrefix, i), Last: last})
	}

	return len(packWatches(searches))
}

// packWatches packs the searches, each key once, in their order, into the requests
// FetchWatches sends: a new request once the next search would take one past
// watchPackResults or watchPackSearches.
func packWatches(searches []WatchSearch) [][]WatchSearch {
	var (
		packs [][]WatchSearch
		pack  []WatchSearch
		size  int
	)

	seen := make(map[string]bool, len(searches))

	for _, s := range searches {
		if s.Key == "" || seen[s.Key] {
			continue
		}

		seen[s.Key] = true

		if len(pack) > 0 && (size+s.expected() > watchPackResults || len(pack) == watchPackSearches) {
			packs, pack, size = append(packs, pack), nil, 0
		}

		pack = append(pack, s)
		size += s.expected()
	}

	if len(pack) > 0 {
		packs = append(packs, pack)
	}

	return packs
}

// watchesQuery writes the request for one pack: each search under its alias, in order.
// Each query is written with graphQLString, so none can break out of its argument.
func watchesQuery(pack []WatchSearch) (string, error) {
	var b strings.Builder

	b.WriteString("query Watches {\n")

	for i, s := range pack {
		query, err := graphQLString(s.Query)
		if err != nil {
			return "", fmt.Errorf("encode the search %q: %w", s.Key, err)
		}

		fmt.Fprintf(&b, "  %s: search(query: %s, type: ISSUE, first: %d) {\n", batchAlias(watchPrefix, i), query, watchPage)
		b.WriteString("    issueCount\n    nodes { ...WatchFields }\n  }\n")
	}

	b.WriteString("  rateLimit { cost remaining resetAt limit used }\n}\n")
	b.WriteString(watchFieldsFragment)

	return b.String(), nil
}

// watchAnswer is what one pack's request brought: each search's result, the request's
// budget reading, whether it reached GitHub at all, and the request's own error.
type watchAnswer struct {
	results map[string]WatchResult
	rate    QueryRate
	sent    bool
	err     error
}

// failAll fails every search of pack with err.
func failAll(pack []WatchSearch, err error) *watchAnswer {
	a := &watchAnswer{results: make(map[string]WatchResult, len(pack)), err: err}

	for _, s := range pack {
		a.results[s.Key] = WatchResult{Err: err, Items: []WatchItem{}}
	}

	return a
}

// sendWatches sends one pack's request and reads each search's answer.
func (c *Client) sendWatches(ctx context.Context, tok Token, pack []WatchSearch) *watchAnswer {
	query, err := watchesQuery(pack)
	if err != nil {
		return failAll(pack, err)
	}

	resp, _, err := postGraphQL[map[string]json.RawMessage](ctx, c, tok, "watches", graphQLRequest{Query: query}, unlessData)
	if err != nil {
		a := failAll(pack, err)
		_, answered := errors.AsType[*StatusError](err)
		a.sent = answered || errors.Is(err, ErrGraphQL) || errors.Is(err, ErrMalformedResponse)

		return a
	}

	data := *resp.Data
	faults := watchFaultsOf(resp.Errors)
	a := &watchAnswer{results: make(map[string]WatchResult, len(pack)), rate: aliasedRate(data), sent: true}

	for i, s := range pack {
		alias := batchAlias(watchPrefix, i)
		a.results[s.Key] = readWatch(data[alias], faults.of(alias))
	}

	return a
}

// watchFaults is what GitHub's errors beside a pack's data say about its searches, by
// alias: an error about a search itself or its list of results, which fails it, and the
// results one failed alone, by index, which are left out. An error that names no search
// makes every search of the pack partial.
type watchFaults struct {
	failed  map[string]error
	dropped map[string]map[int]bool
	anon    bool
}

// watchFault is what the errors say about one search.
type watchFault struct {
	err     error
	dropped map[int]bool
	partial bool
}

// watchFaultsOf sorts GitHub's errors by the search and result each names: a path that
// stops at a search's alias or its nodes (two elements at most) fails the search; a longer
// one names a result, by its index in nodes, which is dropped; one with no path names
// nothing.
func watchFaultsOf(errs []graphQLError) watchFaults {
	f := watchFaults{failed: map[string]error{}, dropped: map[string]map[int]bool{}}

	for _, e := range errs {
		alias := e.top()

		switch {
		case alias == "":
			f.anon = true
		case len(e.Path) <= 2:
			f.failed[alias] = errors.Join(f.failed[alias], fmt.Errorf("%w: %s", ErrSearchFailed, truncate(e.Message)))
		default:
			index, ok := e.Path[2].(float64)
			if !ok {
				f.failed[alias] = errors.Join(f.failed[alias], fmt.Errorf("%w: %s", ErrSearchFailed, truncate(e.Message)))

				continue
			}

			if f.dropped[alias] == nil {
				f.dropped[alias] = map[int]bool{}
			}

			f.dropped[alias][int(index)] = true
		}
	}

	return f
}

// of returns what the errors say about the search under alias.
func (f watchFaults) of(alias string) watchFault {
	return watchFault{err: f.failed[alias], dropped: f.dropped[alias], partial: f.anon || len(f.dropped[alias]) > 0}
}

// readWatch reads one search's answer, with what GitHub's errors say about it. An answer
// that is missing or null, or can't be read, fails the search; a result an error names is
// left out, and the answer is partial.
func readWatch(raw json.RawMessage, fault watchFault) WatchResult {
	if fault.err != nil {
		return WatchResult{Err: fault.err, Items: []WatchItem{}}
	}

	if isNull(raw) {
		return WatchResult{Err: fmt.Errorf("%w: no answer", ErrSearchFailed), Items: []WatchItem{}}
	}

	var s struct {
		IssueCount int          `json:"issueCount"`
		Nodes      []*watchNode `json:"nodes"`
	}

	if err := json.Unmarshal(raw, &s); err != nil || s.Nodes == nil {
		return WatchResult{
			Err:   fmt.Errorf("%w: %w: an answer it can't read", ErrSearchFailed, ErrMalformedResponse),
			Items: []WatchItem{},
		}
	}

	items := make([]WatchItem, 0, len(s.Nodes))

	for i, n := range s.Nodes {
		if fault.dropped[i] {
			continue
		}

		if it, ok := n.watchItem(); ok {
			items = append(items, it)
		}
	}

	return WatchResult{IssueCount: max(s.IssueCount, len(items)), Items: items, Partial: fault.partial}
}

// watchNode is one search result as WatchFields reads it: the item's own fields, which
// decode as an inbox search's do, and what tells whether it changed.
type watchNode struct {
	graphQLNode

	ClosedAt          time.Time `json:"closedAt"`
	HeadRefOID        string    `json:"headRefOid"`
	BaseRefName       string    `json:"baseRefName"`
	StatusCheckRollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

// watchItem converts a pull request or issue result; ok is false for anything else.
func (n *watchNode) watchItem() (WatchItem, bool) {
	if n == nil {
		return WatchItem{}, false
	}

	it, ok := n.item()
	if !ok || it.NodeID == "" {
		return WatchItem{}, false
	}

	it.ClosedAt = n.ClosedAt.UTC()
	out := WatchItem{Item: it}

	if it.IsPR {
		out.HeadOID, out.BaseRef = n.HeadRefOID, n.BaseRefName
		if n.StatusCheckRollup != nil {
			out.ChecksState = n.StatusCheckRollup.State
		}
	}

	return out, true
}
