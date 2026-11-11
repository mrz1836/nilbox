package github

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/repo"
)

// The release queries ask about releaseBatch repositories each, and at most
// releaseParallel are in flight. Every size costs one point, but GitHub's time grows with
// it: measured, 50 repositories took up to 10.6 s and once ran into GitHub's ten-second
// limit, which it answers with a 502, while 10 took 1.6 s. A batch GitHub gives up on is
// halved (see sendHalving).
const (
	releaseBatch    = 15
	releaseParallel = statesParallel

	// releaseTagsMax is how many of a repository's newest tags the status query reads, and
	// releaseCommitsMax how many of the newest commits since a tag the compare query reads.
	releaseTagsMax    = 20
	releaseCommitsMax = 10
)

// GitHub's GraphQL names for the objects a tag can point at, and its error type for a node
// the token may not see, such as a repository of an organization that enforces SAML
// single sign-on the token was not authorized for.
const (
	typeCommit     = "Commit"
	typeTag        = "Tag"
	errorForbidden = "FORBIDDEN"
)

// releaseStatusFragment is what the status query reads of each repository, with
// releaseTagsMax in place of its verb: whether it is archived, the viewer's access, the
// default branch and its newest commit with that commit's check rollup, and the newest
// tags by commit date, each with what it points at and, for an annotated tag, what the tag
// object points at.
const releaseStatusFragment = `
fragment ReleaseStatusFields on Repository {
  isArchived viewerPermission
  defaultBranchRef {
    name
    target { oid ... on Commit { committedDate messageHeadline statusCheckRollup { state } } }
  }
  refs(refPrefix: "refs/tags/", first: %d, orderBy: {field: TAG_COMMIT_DATE, direction: DESC}) {
    nodes { name target { __typename oid ... on Tag { target { __typename oid } } } }
  }
}
`

// releaseCompareFragment is what the compare query reads of a comparison, with
// releaseCommitsMax in place of its verb: how far the head is ahead, and its newest
// commits.
const releaseCompareFragment = `
fragment ReleaseCompareFields on Comparison {
  aheadBy
  commits(last: %d) { totalCount nodes { oid messageHeadline committedDate } }
}
`

// ErrInvalidCompareRequest is returned for a CompareRequest without a tag or a branch; it
// is never sent.
var ErrInvalidCompareRequest = errors.New("invalid release comparison request")

// ReleaseStatus is what GitHub says about a repository a release is made from.
type ReleaseStatus struct {
	// Archived reports an archived repository, which takes no pushes.
	Archived bool

	// ViewerPermission is the viewer's access: ADMIN, MAINTAIN, WRITE, TRIAGE, or READ ("" when
	// GitHub did not say).
	ViewerPermission string

	// DefaultBranch is the default branch's name ("" for an empty repository).
	DefaultBranch string

	// Head is the default branch's newest commit; zero for an empty repository.
	Head ReleaseCommit

	// HeadChecks is the rollup state of Head's checks and statuses: SUCCESS, PENDING,
	// FAILURE, ERROR, or EXPECTED, and "" when it has none.
	HeadChecks string

	// Tags are the repository's newest tags by commit date, newest first, at most 20. Never
	// nil.
	Tags []ReleaseTag
}

// ReleaseTag is one of a repository's tags.
type ReleaseTag struct {
	// Name is the tag's name, as in v1.4.2.
	Name string

	// OID is the object the tag points at: the commit for a lightweight tag, the tag object
	// for an annotated one.
	OID string

	// CommitOID is the commit the tag marks: OID for a lightweight tag, and what the tag
	// object points at for an annotated one. It is "" when that is not a commit.
	CommitOID string
}

// CompareRequest asks how far a repository's branch is ahead of one of its tags.
type CompareRequest struct {
	Repo repo.Name

	// Tag is the tag's name, as in v1.4.2.
	Tag string

	// Branch is the branch's name, as in master.
	Branch string
}

// ReleaseCompare is how far a branch is ahead of a tag.
type ReleaseCompare struct {
	// AheadBy is how many commits the branch has that the tag does not.
	AheadBy int

	// TotalCommits is how many commits the comparison holds, which can exceed len(Commits).
	TotalCommits int

	// Commits are the newest of those commits, at most 10, as GitHub lists them. Never nil.
	Commits []ReleaseCommit
}

// ReleaseCommit is a commit as a release lists it.
type ReleaseCommit struct {
	// OID is the commit's SHA.
	OID string

	// Headline is the first line of its message.
	Headline string

	// CommittedAt is when it was committed, in UTC.
	CommittedAt time.Time
}

// FetchReleaseStatus reads, for each repository, what a release needs to know before it
// compares anything: whether it is archived, the viewer's access, the default branch and
// its newest commit, and its newest tags. The answers are keyed by each repository's Key,
// and a repository named twice is asked about once. A repository GitHub can't resolve, or
// won't show the token, is left out; any other error fails the whole call. The
// repositories are asked about 15 to a query, at most four queries at a time, each costing
// one point, which the budget hears. answered, when set, hears each query GitHub answers
// (see ReleaseAnswered).
func (c *Client) FetchReleaseStatus(
	ctx context.Context, tok Token, repos []repo.Name, answered ReleaseAnswered,
) (map[string]ReleaseStatus, error) {
	out, err := sendReleaseBatches(ctx, uniqueBy(repos, repo.Name.Key),
		func(ctx context.Context, batch []repo.Name) (map[string]ReleaseStatus, error) {
			return c.fetchReleaseStatus(ctx, tok, batch)
		}, answered)
	if err != nil {
		return nil, Redact(err, tok)
	}

	return out, nil
}

// FetchReleaseCompare reads, for each request, how far its branch is ahead of its tag,
// and the newest commits since the tag. It is the second round after FetchReleaseStatus,
// whose tags tell the caller which tag to compare with. The answers are keyed by each
// request's Repo.Key, so a repository is compared once: a later request for one already
// asked about is dropped. A repository, tag, or branch GitHub can't resolve, or a
// repository it won't show the token, is left out; any other error fails the whole call. A request without a tag or a branch fails the call
// before anything is sent. The batching, and answered, are FetchReleaseStatus's.
func (c *Client) FetchReleaseCompare(
	ctx context.Context, tok Token, reqs []CompareRequest, answered ReleaseAnswered,
) (map[string]ReleaseCompare, error) {
	for _, req := range reqs {
		if req.Tag == "" || req.Branch == "" {
			return nil, fmt.Errorf("%w: %s, tag %q, branch %q", ErrInvalidCompareRequest, req.Repo, req.Tag, req.Branch)
		}
	}

	out, err := sendReleaseBatches(ctx, uniqueBy(reqs, CompareRequest.key),
		func(ctx context.Context, batch []CompareRequest) (map[string]ReleaseCompare, error) {
			return c.fetchReleaseCompare(ctx, tok, batch)
		}, answered)
	if err != nil {
		return nil, Redact(err, tok)
	}

	return out, nil
}

// key is the key FetchReleaseCompare answers r under.
func (r CompareRequest) key() string {
	return r.Repo.Key()
}

// ReleaseAnswered hears a release query's progress: once for each batch GitHub answered,
// with how many of the call's repositories or requests it held, so a caller can count them
// off against all it asked about. It is called from the batches' own goroutines, perhaps
// at once, and must return promptly. A nil ReleaseAnswered hears nothing.
type ReleaseAnswered func(n int)

// releaseSend sends one release query about asks and returns its answers by key.
type releaseSend[A, T any] func(ctx context.Context, asks []A) (map[string]T, error)

// sendReleaseBatches sends asks in batches of releaseBatch, at most releaseParallel at a
// time, telling answered of each batch answered, and merges the answers. The first batch
// to fail stops the rest and fails the call.
func sendReleaseBatches[A, T any](
	ctx context.Context, asks []A, send releaseSend[A, T], answered ReleaseAnswered,
) (map[string]T, error) {
	batches := slices.Collect(slices.Chunk(asks, releaseBatch))
	answers := make([]map[string]T, len(batches))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(releaseParallel)

	for i, batch := range batches {
		g.Go(func() error {
			var err error

			answers[i], err = sendHalving(gctx, batch, send)
			if err == nil && answered != nil {
				answered(len(batch))
			}

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	out := make(map[string]T, len(asks))
	for _, answer := range answers {
		maps.Copy(out, answer)
	}

	return out, nil
}

// sendHalving sends one batch, and when GitHub gives up on it as too slow, sends each
// half in turn, halving again as needed, so the batch's slot never holds more than one
// request. A single ask GitHub gives up on fails like any other error.
func sendHalving[A, T any](ctx context.Context, batch []A, send releaseSend[A, T]) (map[string]T, error) {
	answer, err := send(ctx, batch)
	if err == nil || len(batch) == 1 || !tooSlow(ctx, err) {
		return answer, err
	}

	half := len(batch) / 2

	first, err := sendHalving(ctx, batch[:half], send)
	if err != nil {
		return nil, err
	}

	second, err := sendHalving(ctx, batch[half:], send)
	if err != nil {
		return nil, err
	}

	maps.Copy(first, second)

	return first, nil
}

// tooSlow reports whether err is a query that took too long to answer: GitHub stops a
// GraphQL query after about ten seconds and answers 502 (or a gateway's 504), and the HTTP
// client's own timeout ends a request still waiting for its answer. A request ctx ended
// is not: the caller gave up, or another batch failed. Nor is a connection that timed
// out before the query was sent, a dial or a TLS handshake: a smaller query wouldn't
// connect any sooner, so halving would only wait out the timeout again and again.
func tooSlow(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}

	if status, ok := errors.AsType[*StatusError](err); ok {
		return status.StatusCode == http.StatusBadGateway || status.StatusCode == http.StatusGatewayTimeout
	}

	if op, ok := errors.AsType[*net.OpError](err); ok && op.Op == "dial" {
		return false
	}

	if strings.Contains(err.Error(), "TLS handshake timeout") {
		return false
	}

	timeout, ok := errors.AsType[net.Error](err)

	return ok && timeout.Timeout()
}

// unlessHidden fails a release query on any GraphQL error but those that only hide one
// repository: GitHub's not-found, and a refusal that came with the other repositories'
// data, such as a repository behind SAML single sign-on the token was not authorized for.
// Either leaves that repository's alias null.
func unlessHidden(e graphQLError, hasData bool) bool {
	return e.Type != errorNotFound && (e.Type != errorForbidden || !hasData)
}

// uniqueBy returns items without those whose key an earlier one had, in order.
func uniqueBy[T any](items []T, key func(T) string) []T {
	seen := make(map[string]bool, len(items))

	return slices.DeleteFunc(slices.Clone(items), func(item T) bool {
		k := key(item)
		if seen[k] {
			return true
		}

		seen[k] = true

		return false
	})
}

// fetchReleaseStatus sends one status query and returns its answers by repository key.
func (c *Client) fetchReleaseStatus(ctx context.Context, tok Token, repos []repo.Name) (map[string]ReleaseStatus, error) {
	query, err := releaseStatusQuery(repos)
	if err != nil {
		return nil, err
	}

	resp, _, err := postGraphQL[releaseStatusData](ctx, c, tok, "release status", graphQLRequest{Query: query}, unlessHidden)
	if err != nil {
		return nil, err
	}

	out := make(map[string]ReleaseStatus, len(repos))

	for i, r := range repos {
		if status, ok := (*resp.Data)[batchAlias("r", i)].status(); ok {
			out[r.Key()] = status
		}
	}

	return out, nil
}

// fetchReleaseCompare sends one compare query and returns its answers by repository key.
func (c *Client) fetchReleaseCompare(
	ctx context.Context, tok Token, reqs []CompareRequest,
) (map[string]ReleaseCompare, error) {
	query, err := releaseCompareQuery(reqs)
	if err != nil {
		return nil, err
	}

	resp, _, err := postGraphQL[releaseCompareData](ctx, c, tok, "release compare", graphQLRequest{Query: query}, unlessHidden)
	if err != nil {
		return nil, err
	}

	out := make(map[string]ReleaseCompare, len(reqs))

	for i, req := range reqs {
		if compare, ok := (*resp.Data)[batchAlias("r", i)].compare(); ok {
			out[req.key()] = compare
		}
	}

	return out, nil
}

// releaseStatusQuery asks about each repository under its own alias. The repository is
// written by repoField, so no name can break out of its argument.
func releaseStatusQuery(repos []repo.Name) (string, error) {
	var b strings.Builder

	b.WriteString("query ReleaseStatus {\n")

	for i, r := range repos {
		field, err := repoField(batchAlias("r", i), r)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(&b, "  %s { ...ReleaseStatusFields }\n", field)
	}

	// The query's own cost, so the API budget counts it; the key is not an alias, so no
	// repository looks it up.
	b.WriteString("  rateLimit { cost }\n}\n")
	fmt.Fprintf(&b, releaseStatusFragment, releaseTagsMax)

	return b.String(), nil
}

// releaseCompareQuery compares each request's tag with its branch under its own alias.
// The repository is written by repoField and the tag and branch by graphQLString, so none
// can break out of its argument.
func releaseCompareQuery(reqs []CompareRequest) (string, error) {
	var b strings.Builder

	b.WriteString("query ReleaseCompare {\n")

	for i, req := range reqs {
		field, err := repoField(batchAlias("r", i), req.Repo)
		if err != nil {
			return "", err
		}

		tag, err := graphQLString("refs/tags/" + req.Tag)
		if err != nil {
			return "", err
		}

		branch, err := graphQLString(req.Branch)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(&b, "  %s { ref(qualifiedName: %s) { compare(headRef: %s) { ...ReleaseCompareFields } } }\n",
			field, tag, branch)
	}

	b.WriteString("  rateLimit { cost }\n}\n")
	fmt.Fprintf(&b, releaseCompareFragment, releaseCommitsMax)

	return b.String(), nil
}

// releaseStatusData is the status query's data object: one aliased repository each, null
// when GitHub could not find it or would not show it.
type releaseStatusData map[string]*releaseStatusRepo

// releaseStatusRepo is one aliased repository. GitHub's nulls decode to zero values.
type releaseStatusRepo struct {
	IsArchived       bool   `json:"isArchived"`
	ViewerPermission string `json:"viewerPermission"`
	DefaultBranchRef *struct {
		Name   string         `json:"name"`
		Target *releaseCommit `json:"target"`
	} `json:"defaultBranchRef"`
	Refs struct {
		Nodes []*releaseTagNode `json:"nodes"`
	} `json:"refs"`
}

// releaseCommit is a commit: a branch's target, or one of a comparison's commits.
type releaseCommit struct {
	OID               string    `json:"oid"`
	MessageHeadline   string    `json:"messageHeadline"`
	CommittedDate     time.Time `json:"committedDate"`
	StatusCheckRollup *struct {
		State string `json:"state"`
	} `json:"statusCheckRollup"`
}

// releaseTagNode is one tag and the object it points at.
type releaseTagNode struct {
	Name   string `json:"name"`
	Target *struct {
		TypeName string `json:"__typename"`
		OID      string `json:"oid"`

		// Target is what an annotated tag's object points at.
		Target *struct {
			TypeName string `json:"__typename"`
			OID      string `json:"oid"`
		} `json:"target"`
	} `json:"target"`
}

// status converts one repository's answer; ok is false when there is none.
func (r *releaseStatusRepo) status() (ReleaseStatus, bool) {
	if r == nil {
		return ReleaseStatus{}, false
	}

	out := ReleaseStatus{
		Archived: r.IsArchived, ViewerPermission: r.ViewerPermission, Tags: make([]ReleaseTag, 0, len(r.Refs.Nodes)),
	}

	if ref := r.DefaultBranchRef; ref != nil {
		out.DefaultBranch = ref.Name
		out.Head, out.HeadChecks = ref.Target.commit(), ref.Target.checks()
	}

	for _, n := range r.Refs.Nodes {
		if tag, ok := n.tag(); ok {
			out.Tags = append(out.Tags, tag)
		}
	}

	return out, true
}

// commit converts a commit; zero for none.
func (n *releaseCommit) commit() ReleaseCommit {
	if n == nil {
		return ReleaseCommit{}
	}

	return ReleaseCommit{OID: n.OID, Headline: n.MessageHeadline, CommittedAt: n.CommittedDate.UTC()}
}

// checks is the commit's check rollup state, "" when it has none.
func (n *releaseCommit) checks() string {
	if n == nil || n.StatusCheckRollup == nil {
		return ""
	}

	return n.StatusCheckRollup.State
}

// tag converts one tag, peeling an annotated tag to its commit; ok is false for a null or
// nameless node.
func (n *releaseTagNode) tag() (ReleaseTag, bool) {
	if n == nil || n.Name == "" {
		return ReleaseTag{}, false
	}

	out := ReleaseTag{Name: n.Name}

	t := n.Target
	if t == nil {
		return out, true
	}

	out.OID = t.OID

	switch {
	case t.TypeName == typeCommit:
		out.CommitOID = t.OID
	case t.TypeName == typeTag && t.Target != nil && t.Target.TypeName == typeCommit:
		out.CommitOID = t.Target.OID
	}

	return out, true
}

// releaseCompareData is the compare query's data object: one aliased repository each,
// null when GitHub could not find it or would not show it.
type releaseCompareData map[string]*releaseCompareRepo

// releaseCompareRepo is one aliased repository, its tag (null when there is no such tag),
// and the tag's comparison with the branch (null when there is no such branch).
type releaseCompareRepo struct {
	Ref *struct {
		Compare *struct {
			AheadBy int `json:"aheadBy"`
			Commits struct {
				TotalCount int              `json:"totalCount"`
				Nodes      []*releaseCommit `json:"nodes"`
			} `json:"commits"`
		} `json:"compare"`
	} `json:"ref"`
}

// compare converts one repository's comparison; ok is false when there is none.
func (r *releaseCompareRepo) compare() (ReleaseCompare, bool) {
	if r == nil || r.Ref == nil || r.Ref.Compare == nil {
		return ReleaseCompare{}, false
	}

	comparison := r.Ref.Compare
	out := ReleaseCompare{
		AheadBy: comparison.AheadBy, TotalCommits: comparison.Commits.TotalCount,
		Commits: make([]ReleaseCommit, 0, len(comparison.Commits.Nodes)),
	}

	for _, n := range comparison.Commits.Nodes {
		if n != nil && n.OID != "" {
			out.Commits = append(out.Commits, n.commit())
		}
	}

	return out, true
}
