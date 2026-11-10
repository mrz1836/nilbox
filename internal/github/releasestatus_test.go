package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// releaseStatusBody answers the status query for releaseStatusRepos: a repository with a
// passing head and three tags (annotated, lightweight, and annotated on a tree) and a null
// tag node, an archived one the viewer can only read, with no checks and no tags, an empty
// one, one GitHub could not find, and one behind SAML single sign-on.
const releaseStatusBody = `{
  "data": {
    "r0": {
      "isArchived": false,
      "viewerPermission": "ADMIN",
      "defaultBranchRef": {
        "name": "master",
        "target": {"oid": "4444444444444444444444444444444444444444", "committedDate": "2026-11-09T15:04:05-05:00",
          "messageHeadline": "Add the release page", "statusCheckRollup": {"state": "SUCCESS"}}
      },
      "refs": {"nodes": [
        {"name": "v1.4.2", "target": {"__typename": "Tag", "oid": "2222222222222222222222222222222222222222",
          "target": {"__typename": "Commit", "oid": "1111111111111111111111111111111111111111"}}},
        {"name": "v1.4.1", "target": {"__typename": "Commit", "oid": "3333333333333333333333333333333333333333"}},
        {"name": "docs-snapshot", "target": {"__typename": "Tag", "oid": "5555555555555555555555555555555555555555",
          "target": {"__typename": "Tree", "oid": "6666666666666666666666666666666666666666"}}},
        null
      ]}
    },
    "r1": {
      "isArchived": true,
      "viewerPermission": "READ",
      "defaultBranchRef": {
        "name": "main",
        "target": {"oid": "7777777777777777777777777777777777777777", "committedDate": "2025-01-02T03:04:05Z",
          "messageHeadline": "Last words", "statusCheckRollup": null}
      },
      "refs": {"nodes": []}
    },
    "r2": {"isArchived": false, "viewerPermission": "WRITE", "defaultBranchRef": null, "refs": {"nodes": []}},
    "r3": null,
    "r4": null,
    "rateLimit": {"cost": 1}
  },
  "errors": [
    {"type": "NOT_FOUND", "path": ["r3"], "message": "Could not resolve to a Repository with the name 'acme-org/gone'."},
    {"type": "FORBIDDEN", "path": ["r4"], "message": "Resource protected by organization SAML enforcement."}
  ]
}`

// releaseStatusRepos are the repositories releaseStatusBody answers, in alias order.
func releaseStatusRepos() []repo.Name {
	return []repo.Name{
		{Owner: filesOwner, Repo: exampleRepo},
		{Owner: "Acme-Org", Repo: "Other-Repo"},
		{Owner: filesOwner, Repo: "empty-repo"},
		{Owner: filesOwner, Repo: "gone"},
		{Owner: filesOwner, Repo: "single-sign-on"},
	}
}

// releaseCompareBody answers the compare query for releaseCompareRequests: a branch three
// commits ahead with a null commit node, a tag that does not exist, a branch that does not
// exist, and a repository GitHub could not find.
const releaseCompareBody = `{
  "data": {
    "r0": {"ref": {"compare": {"aheadBy": 3, "commits": {"totalCount": 3, "nodes": [
      {"oid": "8888888888888888888888888888888888888888", "messageHeadline": "Fix the parser", "committedDate": "2026-11-07T10:00:00Z"},
      null,
      {"oid": "9999999999999999999999999999999999999999", "messageHeadline": "Add the release page",
        "committedDate": "2026-11-09T15:04:05-05:00"}
    ]}}}},
    "r1": {"ref": null},
    "r2": {"ref": {"compare": null}},
    "r3": null,
    "rateLimit": {"cost": 1}
  },
  "errors": [
    {"type": "NOT_FOUND", "path": ["r2", "ref", "compare"], "message": "Could not resolve to a Ref with the name 'gone'."},
    {"type": "NOT_FOUND", "path": ["r3"], "message": "Could not resolve to a Repository with the name 'acme-org/gone'."}
  ]
}`

// releaseCompareRequests are the requests releaseCompareBody answers, in alias order.
func releaseCompareRequests() []CompareRequest {
	return []CompareRequest{
		{Repo: repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}, Tag: "v1.4.2", Branch: "master"},
		{Repo: repo.Name{Owner: filesOwner, Repo: "untagged"}, Tag: "v0.0.1", Branch: "main"},
		{Repo: repo.Name{Owner: filesOwner, Repo: "branchless"}, Tag: "v2.0.0", Branch: "gone"},
		{Repo: repo.Name{Owner: filesOwner, Repo: "gone"}, Tag: "v1.0.0", Branch: "main"},
	}
}

// releaseRepos returns n repositories of the tests' owner, repo-0 onward.
func releaseRepos(n int) []repo.Name {
	repos := make([]repo.Name, n)
	for i := range repos {
		repos[i] = repo.Name{Owner: filesOwner, Repo: fmt.Sprintf("repo-%d", i)}
	}

	return repos
}

// readReleaseQuery reads the query a release request sends. It runs in a handler, so it
// reports a problem without stopping the test.
func readReleaseQuery(t *testing.T, r *http.Request) string {
	t.Helper()

	raw, err := io.ReadAll(r.Body)
	assert.NoError(t, err)

	var req graphQLRequest
	assert.NoError(t, json.Unmarshal(raw, &req))

	return req.Query
}

// releaseAliases counts the repositories a release query asks about.
func releaseAliases(query string) int {
	return strings.Count(query, "repository(")
}

// answerEach is a response that answers each of n aliases with node, at a cost of one
// point.
func answerEach(n int, node string) string {
	var b strings.Builder

	b.WriteString(`{"data": {`)

	for i := range n {
		fmt.Fprintf(&b, "%q: %s, ", releaseAlias(i), node)
	}

	b.WriteString(`"rateLimit": {"cost": 1}}}`)

	return b.String()
}

// releaseRound is one of the two release queries, for the tests they share.
type releaseRound struct {
	name string

	// node answers one repository.
	node string

	// fetch asks about n repositories and returns how many were answered.
	fetch func(ctx context.Context, c *Client, n int) (int, error)
}

// releaseRounds are both release queries.
func releaseRounds() []releaseRound {
	return []releaseRound{
		{
			name: "status",
			node: `{"isArchived": false, "viewerPermission": "ADMIN", "refs": {"nodes": []}}`,
			fetch: func(ctx context.Context, c *Client, n int) (int, error) {
				got, err := c.FetchReleaseStatus(ctx, NewToken(fakeToken), releaseRepos(n))

				return len(got), err
			},
		},
		{
			name: "compare",
			node: `{"ref": {"compare": {"aheadBy": 1, "commits": {"totalCount": 1, "nodes": []}}}}`,
			fetch: func(ctx context.Context, c *Client, n int) (int, error) {
				reqs := make([]CompareRequest, n)
				for i, r := range releaseRepos(n) {
					reqs[i] = CompareRequest{Repo: r, Tag: "v1.0.0", Branch: "master"}
				}

				got, err := c.FetchReleaseCompare(ctx, NewToken(fakeToken), reqs)

				return len(got), err
			},
		},
	}
}

func TestFetchReleaseStatusReadsEachRepository(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))

		query := readReleaseQuery(t, r)
		assert.True(t, strings.HasPrefix(query, "query ReleaseStatus {"), "a query, never a mutation")
		assert.Contains(t, query, `r0: repository(owner: "acme-org", name: "example-repo") { ...ReleaseStatusFields }`)
		assert.Contains(t, query, `r4: repository(owner: "acme-org", name: "single-sign-on") { ...ReleaseStatusFields }`)
		assert.Contains(t, query, "fragment ReleaseStatusFields on Repository {")
		assert.Contains(t, query, "isArchived viewerPermission")
		assert.Contains(t, query, "statusCheckRollup { state }")
		assert.Contains(t, query, `refs(refPrefix: "refs/tags/", first: 20, orderBy: {field: TAG_COMMIT_DATE, direction: DESC})`)
		assert.Contains(t, query, "... on Tag { target { __typename oid } }", "an annotated tag is peeled to what it marks")
		assert.Contains(t, query, "rateLimit { cost }", "the query reports its cost")

		_, _ = io.WriteString(w, releaseStatusBody)
	})

	repos := releaseStatusRepos()

	got, err := client.FetchReleaseStatus(t.Context(), NewToken(fakeToken), repos)
	require.NoError(t, err)
	require.Len(t, got, 3, "a repository GitHub can't find, or won't show, is left out")

	assert.Equal(t, ReleaseStatus{
		ViewerPermission: "ADMIN",
		DefaultBranch:    "master",
		Head: ReleaseCommit{
			OID:         sha1s("4"),
			Headline:    "Add the release page",
			CommittedAt: time.Date(2026, 11, 9, 20, 4, 5, 0, time.UTC),
		},
		HeadChecks: "SUCCESS",
		Tags: []ReleaseTag{
			{Name: "v1.4.2", OID: sha1s("2"), CommitOID: sha1s("1")},
			{Name: "v1.4.1", OID: sha1s("3"), CommitOID: sha1s("3")},
			{Name: "docs-snapshot", OID: sha1s("5")},
		},
	}, got[repos[0].Key()], "an annotated tag is read as its commit, and a tag of a tree as no commit")

	assert.Equal(t, ReleaseStatus{
		Archived:         true,
		ViewerPermission: "READ",
		DefaultBranch:    "main",
		Head: ReleaseCommit{
			OID: sha1s("7"), Headline: "Last words", CommittedAt: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		Tags: []ReleaseTag{},
	}, got["acme-org/other-repo"], "answers are keyed by the folded name")

	assert.Equal(t, ReleaseStatus{ViewerPermission: "WRITE", Tags: []ReleaseTag{}}, got[repos[2].Key()],
		"an empty repository has no default branch")
	assert.NotContains(t, got, repos[3].Key())
	assert.NotContains(t, got, repos[4].Key())
}

func TestFetchReleaseCompareReadsEachRepository(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query := readReleaseQuery(t, r)
		assert.True(t, strings.HasPrefix(query, "query ReleaseCompare {"), "a query, never a mutation")
		assert.Contains(t, query, `r0: repository(owner: "Acme-Org", name: "Example-Repo") { ref(qualifiedName: "refs/tags/v1.4.2") `+
			`{ compare(headRef: "master") { ...ReleaseCompareFields } } }`)
		assert.Contains(t, query, `r3: repository(owner: "acme-org", name: "gone")`)
		assert.Contains(t, query, "fragment ReleaseCompareFields on Comparison {")
		assert.Contains(t, query, "commits(last: 10) { totalCount nodes { oid messageHeadline committedDate } }")
		assert.Contains(t, query, "rateLimit { cost }", "the query reports its cost")

		_, _ = io.WriteString(w, releaseCompareBody)
	})

	reqs := releaseCompareRequests()

	got, err := client.FetchReleaseCompare(t.Context(), NewToken(fakeToken), reqs)
	require.NoError(t, err)
	require.Len(t, got, 1, "a tag, branch, or repository GitHub can't find is left out")

	assert.Equal(t, ReleaseCompare{
		AheadBy:      3,
		TotalCommits: 3,
		Commits: []ReleaseCommit{
			{OID: sha1s("8"), Headline: "Fix the parser", CommittedAt: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)},
			{OID: sha1s("9"), Headline: "Add the release page", CommittedAt: time.Date(2026, 11, 9, 20, 4, 5, 0, time.UTC)},
		},
	}, got["acme-org/example-repo"], "answers are keyed by the folded name, and a null commit is skipped")
}

func TestReleaseQueriesQuoteEveryValue(t *testing.T) {
	t.Parallel()

	hostile := repo.Name{Owner: `acme"org`, Repo: "x) { viewer { login } }"}

	status, err := releaseStatusQuery([]repo.Name{hostile})
	require.NoError(t, err)
	assert.Contains(t, status, `r0: repository(owner: "acme\"org", name: "x) { viewer { login } }") { ...ReleaseStatusFields }`,
		"a name stays inside its JSON string")
	assert.Equal(t, 1, releaseAliases(status))

	compare, err := releaseCompareQuery([]CompareRequest{{Repo: hostile, Tag: "v1\") { x }\n#", Branch: `main\"`}})
	require.NoError(t, err)
	assert.Contains(t, compare, `r0: repository(owner: "acme\"org", name: "x) { viewer { login } }") `+
		`{ ref(qualifiedName: "refs/tags/v1\") { x }\n#") { compare(headRef: "main\\\"") { ...ReleaseCompareFields } } }`,
		"a tag and a branch stay inside their JSON strings")
	assert.Equal(t, 1, releaseAliases(compare))
	assert.NotContains(t, compare, "\n#", "a newline is escaped, never written")
}

// repoNamePattern finds the repositories a release query asks about by name.
var repoNamePattern = regexp.MustCompile(`name: "(repo-\d+)"`)

// queryLog keeps the queries a test server was sent, in the order they arrived.
type queryLog struct {
	mu      sync.Mutex
	queries []string
}

// read reads r's query, keeps it, and returns how many repositories it asks about.
func (l *queryLog) read(t *testing.T, r *http.Request) int {
	t.Helper()

	query := readReleaseQuery(t, r)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.queries = append(l.queries, query)

	return releaseAliases(query)
}

// all returns the queries kept.
func (l *queryLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.queries)
}

// sizes returns how many repositories each query asked about.
func (l *queryLog) sizes() []int {
	queries := l.all()
	sizes := make([]int, 0, len(queries))

	for _, query := range queries {
		sizes = append(sizes, releaseAliases(query))
	}

	return sizes
}

// asked counts how many times the queries asked about each test repository, by name.
func (l *queryLog) asked() map[string]int {
	asked := map[string]int{}

	for _, query := range l.all() {
		for _, m := range repoNamePattern.FindAllStringSubmatch(query, -1) {
			asked[m[1]]++
		}
	}

	return asked
}

// answeringLog is a handler that keeps each query in log and answers every repository it
// asks about with node.
func answeringLog(t *testing.T, log *queryLog, node string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, answerEach(log.read(t, r), node))
	}
}

func TestReleaseRoundsAskFifteenRepositoriesAQuery(t *testing.T) {
	t.Parallel()

	for _, round := range releaseRounds() {
		t.Run(round.name, func(t *testing.T) {
			t.Parallel()

			var log queryLog

			client := newTestClient(t, answeringLog(t, &log, round.node))

			answered, err := round.fetch(t.Context(), client, 31)
			require.NoError(t, err)
			assert.Equal(t, 31, answered)

			sizes := log.sizes()
			slices.Sort(sizes)
			assert.Equal(t, []int{1, 15, 15}, sizes, "fifteen repositories a query")

			asked := log.asked()
			assert.Len(t, asked, 31)

			for name, times := range asked {
				assert.Equal(t, 1, times, "%s is asked about once", name)
			}
		})
	}
}

// flightGate holds a test server's queries until want of them are in flight together, and
// keeps the most it ever held at once.
type flightGate struct {
	want     int32
	inFlight atomic.Int32
	most     atomic.Int32
	full     chan struct{}
	fill     sync.Once
}

// newFlightGate returns a gate that opens once want queries are in flight.
func newFlightGate(want int32) *flightGate {
	return &flightGate{want: want, full: make(chan struct{})}
}

// enter counts a query in, then waits until the gate is open, or waitTimeout passes.
func (g *flightGate) enter() {
	now := g.inFlight.Add(1)

	for {
		if seen := g.most.Load(); now <= seen || g.most.CompareAndSwap(seen, now) {
			break
		}
	}

	if now >= g.want {
		g.fill.Do(func() { close(g.full) })
	}

	select {
	case <-g.full:
	case <-time.After(waitTimeout):
	}
}

// leave counts a query out. A handler leaves before it answers, so the query that takes
// its place is never counted alongside it.
func (g *flightGate) leave() {
	g.inFlight.Add(-1)
}

func TestReleaseRoundsSendAtMostFourQueriesAtOnce(t *testing.T) {
	t.Parallel()

	for _, round := range releaseRounds() {
		t.Run(round.name, func(t *testing.T) {
			t.Parallel()

			var log queryLog

			// The first four queries wait until all four are in flight together, which
			// happens only when they are sent side by side.
			gate := newFlightGate(4)

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				n := log.read(t, r)

				gate.enter()
				gate.leave()

				_, _ = io.WriteString(w, answerEach(n, round.node))
			})

			answered, err := round.fetch(t.Context(), client, 8*releaseBatch)
			require.NoError(t, err)
			assert.Equal(t, 8*releaseBatch, answered)
			assert.Len(t, log.all(), 8)
			assert.Equal(t, int32(4), gate.most.Load(), "four queries are in flight together, never more")
		})
	}
}

// halvingThreshold is the most repositories the halving tests' server answers in one
// query: more, and it gives up.
const halvingThreshold = 8

// releaseTimeout is the client timeout of the test that halves a query the client gave
// up on: long enough that a query the server answers at once never reaches it.
const releaseTimeout = time.Second

// giveUp is one way a query can take too long.
type giveUp struct {
	name string

	// answer ends a request the server won't answer.
	answer http.HandlerFunc

	// timeout is the client's, or 0 for the default.
	timeout time.Duration
}

// giveUps are the ways a query can take too long: GitHub's 502 and a gateway's 504, and
// the client's own timeout.
func giveUps() []giveUp {
	return []giveUp{
		{name: "502", answer: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"message": "Something went wrong while executing your query."}`)
		}},
		{name: "504", answer: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusGatewayTimeout)
		}},
		{name: "client timeout", timeout: releaseTimeout, answer: func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(waitTimeout):
			}
		}},
	}
}

// handler keeps each query in log, answers every repository of a query of at most
// halvingThreshold with node, and gives up on a larger one.
func (g giveUp) handler(t *testing.T, log *queryLog, node string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		if n := log.read(t, r); n <= halvingThreshold {
			_, _ = io.WriteString(w, answerEach(n, node))
		} else {
			g.answer(w, r)
		}
	}
}

func TestReleaseRoundsHalveAQueryGitHubGivesUpOn(t *testing.T) {
	t.Parallel()

	for _, round := range releaseRounds() {
		for _, g := range giveUps() {
			t.Run(round.name+"/"+g.name, func(t *testing.T) {
				t.Parallel()

				var log queryLog

				client := newTestClient(t, g.handler(t, &log, round.node))
				if g.timeout > 0 {
					client.HTTP.Timeout = g.timeout
				}

				answered, err := round.fetch(t.Context(), client, 15)
				require.NoError(t, err)
				assert.Equal(t, 15, answered, "every repository is answered")
				assert.Equal(t, []int{15, 7, 8}, log.sizes(), "the query is sent again in halves, one after the other")
			})
		}
	}
}

func TestReleaseRoundsFailWhenOneRepositoryIsStillTooSlow(t *testing.T) {
	t.Parallel()

	for _, round := range releaseRounds() {
		t.Run(round.name, func(t *testing.T) {
			t.Parallel()

			var log queryLog

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				_ = log.read(t, r)

				w.WriteHeader(http.StatusBadGateway)
			})

			_, err := round.fetch(t.Context(), client, 2)
			require.ErrorIs(t, err, ErrUnexpectedStatus)
			assert.Equal(t, []int{2, 1}, log.sizes(), "the first half fails the call; the second is never sent")
		})
	}
}

func TestReleaseRoundsDoNotHalveOtherFailures(t *testing.T) {
	t.Parallel()

	failures := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "a server error", status: http.StatusInternalServerError, wantErr: ErrUnexpectedStatus},
		{name: "a refusal", status: http.StatusForbidden, body: `{"message": "rate limited"}`, wantErr: ErrUnexpectedStatus},
		{
			name:    "a rate limit",
			body:    `{"data": null, "errors": [{"type": "RATE_LIMITED", "message": "slow down"}]}`,
			wantErr: ErrGraphQL,
		},
		{
			name:    "a refusal without data",
			body:    `{"data": null, "errors": [{"type": "FORBIDDEN", "message": "Resource not accessible"}]}`,
			wantErr: ErrGraphQL,
		},
		{
			name:    "an error beside the data",
			body:    `{"data": {"r0": null}, "errors": [{"type": "SERVICE_UNAVAILABLE", "message": "try later"}]}`,
			wantErr: ErrGraphQL,
		},
		{name: "a body that is not JSON", body: `<html>`, wantErr: ErrMalformedResponse},
	}

	for _, round := range releaseRounds() {
		for _, tt := range failures {
			t.Run(round.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				var requests atomic.Int32

				client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
					requests.Add(1)

					if tt.status != 0 {
						w.WriteHeader(tt.status)
					}

					_, _ = io.WriteString(w, tt.body)
				})

				answered, err := round.fetch(t.Context(), client, releaseBatch)
				require.ErrorIs(t, err, tt.wantErr)
				assert.Zero(t, answered)
				assert.Equal(t, int32(1), requests.Load(), "only a query GitHub gave up on is halved")
			})
		}
	}
}

func TestUnlessHidden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		hasData bool
		fatal   bool
	}{
		{name: "not found", kind: errorNotFound, hasData: true},
		{name: "not found without data", kind: errorNotFound},
		{name: "refused beside the data", kind: errorForbidden, hasData: true},
		{name: "refused without data", kind: errorForbidden, fatal: true},
		{name: "rate limited", kind: errorRateLimited, hasData: true, fatal: true},
		{name: "untyped", hasData: true, fatal: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.fatal, unlessHidden(graphQLError{Type: tt.kind, Message: "x"}, tt.hasData))
		})
	}
}

func TestReleaseRoundsReportTheirCost(t *testing.T) {
	t.Parallel()

	for _, round := range releaseRounds() {
		t.Run(round.name, func(t *testing.T) {
			t.Parallel()

			client, rates := observingClient(t, func(w http.ResponseWriter, r *http.Request) {
				n := releaseAliases(readReleaseQuery(t, r))

				setRateHeaders(w, ResourceGraphQL, 4980, 20)
				_, _ = io.WriteString(w, answerEach(n, round.node))
			})

			_, err := round.fetch(t.Context(), client, releaseBatch+1)
			require.NoError(t, err)

			reading := RateReading{Resource: ResourceGraphQL, Limit: 5000, Remaining: 4980, Used: 20, ResetAt: rateResetAt}
			assert.ElementsMatch(t, []observed{
				{reading: reading, spent: 0},
				{reading: reading, spent: 1},
				{reading: reading, spent: 0},
				{reading: reading, spent: 1},
			}, rates.all(), "the budget hears each query's reading, then its cost of one point")
		})
	}
}

func TestReleaseRoundsWithNothingToAskSendNothing(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("nothing is asked")
	})

	status, err := client.FetchReleaseStatus(t.Context(), NewToken(fakeToken), nil)
	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.Empty(t, status)

	compare, err := client.FetchReleaseCompare(t.Context(), NewToken(fakeToken), []CompareRequest{})
	require.NoError(t, err)
	assert.NotNil(t, compare)
	assert.Empty(t, compare)
}

func TestReleaseRoundsStopWhenTheContextEnds(t *testing.T) {
	t.Parallel()

	for _, round := range releaseRounds() {
		t.Run(round.name, func(t *testing.T) {
			t.Parallel()

			var log queryLog

			arrived := make(chan struct{}, 1)

			client := newTestClient(t, func(_ http.ResponseWriter, r *http.Request) {
				_ = log.read(t, r)

				arrived <- struct{}{}

				select {
				case <-r.Context().Done():
				case <-time.After(waitTimeout):
				}
			})

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			go func() {
				<-arrived
				cancel()
			}()

			answered, err := round.fetch(ctx, client, releaseBatch)
			require.ErrorIs(t, err, context.Canceled)
			assert.Zero(t, answered)
			assert.NotEqual(t, unreachableMessage, uierr.Message(err), "a caller that gave up is not told GitHub is down")
			assert.Len(t, log.all(), 1, "a canceled query is not halved")
		})
	}
}

func TestFetchReleaseCompareRefusesARequestWithoutATagOrBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bad  CompareRequest
	}{
		{name: "no tag", bad: CompareRequest{Repo: pullRef().Repo, Branch: "master"}},
		{name: "no branch", bad: CompareRequest{Repo: pullRef().Repo, Tag: "v1.0.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
				t.Error("nothing is sent")
			})

			reqs := make([]CompareRequest, 0, releaseBatch+1)
			for _, r := range releaseRepos(releaseBatch) {
				reqs = append(reqs, CompareRequest{Repo: r, Tag: "v1.0.0", Branch: "master"})
			}

			_, err := client.FetchReleaseCompare(t.Context(), NewToken(fakeToken), append(reqs, tt.bad))
			require.ErrorIs(t, err, ErrInvalidCompareRequest)
		})
	}
}

func TestReleaseRoundsAskAboutARepositoryOnce(t *testing.T) {
	t.Parallel()

	var log queryLog

	// One answer serves both queries: the status reads none of the comparison's fields.
	client := newTestClient(t, answeringLog(t, &log, `{"ref": {"compare": {"aheadBy": 2}}}`))

	example := repo.Name{Owner: filesOwner, Repo: exampleRepo}
	shouted := repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}

	status, err := client.FetchReleaseStatus(t.Context(), NewToken(fakeToken), []repo.Name{example, shouted})
	require.NoError(t, err)
	assert.Len(t, status, 1)

	compare, err := client.FetchReleaseCompare(t.Context(), NewToken(fakeToken), []CompareRequest{
		{Repo: shouted, Tag: "v1.0.0", Branch: "master"},
		{Repo: example, Tag: "v2.0.0", Branch: "master"},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]ReleaseCompare{
		example.Key(): {AheadBy: 2, Commits: []ReleaseCommit{}},
	}, compare)

	queries := log.all()
	require.Len(t, queries, 2)
	assert.Equal(t, []int{1, 1}, log.sizes(), "each query asks about the repository once")
	assert.Contains(t, queries[1], "refs/tags/v1.0.0", "the first request for a repository is kept")
}

// timeoutError is a net.Error that timed out.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestTooSlowHalvesOnlyASlowAnswer(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	assert.True(t, tooSlow(ctx, &StatusError{StatusCode: http.StatusBadGateway}))
	assert.True(t, tooSlow(ctx, &StatusError{StatusCode: http.StatusGatewayTimeout}))
	assert.False(t, tooSlow(ctx, &StatusError{StatusCode: http.StatusInternalServerError}))
	assert.True(t, tooSlow(ctx, &url.Error{Op: "Post", URL: "https://example.invalid/graphql", Err: timeoutError{}}),
		"the client's own timeout, waiting for the answer")

	dial := &url.Error{Op: "Post", URL: "https://example.invalid/graphql", Err: &net.OpError{Op: "dial", Net: "tcp", Err: timeoutError{}}}
	assert.False(t, tooSlow(ctx, dial), "a connection that never opened isn't a heavy query")

	tls := &url.Error{Op: "Post", URL: "https://example.invalid/graphql", Err: fmt.Errorf("net/http: TLS handshake timeout: %w", timeoutError{})}
	assert.False(t, tooSlow(ctx, tls))

	ended, cancel := context.WithCancel(ctx)
	cancel()
	assert.False(t, tooSlow(ended, &StatusError{StatusCode: http.StatusBadGateway}), "the caller gave up")
}
