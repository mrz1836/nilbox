package github

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// observed is one ObserveRate call.
type observed struct {
	reading RateReading
	spent   int
}

// recordRates is a RateObserver that keeps every call.
type recordRates struct {
	mu  sync.Mutex
	got []observed
}

func (r *recordRates) ObserveRate(reading RateReading, spent int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.got = append(r.got, observed{reading: reading, spent: spent})
}

func (r *recordRates) all() []observed {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]observed(nil), r.got...)
}

// exampleRepo is the repository the rate tests ask about.
const exampleRepo = "example-repo"

// rateResetAt is the reset time the rate headers below report.
var rateResetAt = time.Unix(1794165452, 0).UTC() //nolint:gochecknoglobals // a fixed test value

// setRateHeaders writes a budget's headers on w, as GitHub does on every API response.
func setRateHeaders(w http.ResponseWriter, resource string, remaining, used int) {
	w.Header().Set("X-Ratelimit-Resource", resource)
	w.Header().Set("X-Ratelimit-Limit", "5000")
	w.Header().Set("X-Ratelimit-Remaining", strconv.Itoa(remaining))
	w.Header().Set("X-Ratelimit-Used", strconv.Itoa(used))
	w.Header().Set("X-Ratelimit-Reset", strconv.FormatInt(rateResetAt.Unix(), 10))
}

// observingClient is a test client with a recorder for its budget readings.
func observingClient(t *testing.T, handler http.HandlerFunc) (*Client, *recordRates) {
	t.Helper()

	rates := &recordRates{}
	client := newTestClient(t, handler)
	client.Rates = rates

	return client, rates
}

func TestReadingFrom(t *testing.T) {
	t.Parallel()

	full := http.Header{}
	full.Set("X-Ratelimit-Resource", "graphql")
	full.Set("X-Ratelimit-Limit", "5000")
	full.Set("X-Ratelimit-Remaining", "4887")
	full.Set("X-Ratelimit-Used", "113")
	full.Set("X-Ratelimit-Reset", "1794165452")

	reading, ok := readingFrom(full)
	require.True(t, ok)
	assert.Equal(t, RateReading{
		Resource: "graphql", Limit: 5000, Remaining: 4887, Used: 113, ResetAt: rateResetAt,
	}, reading)

	for name, header := range map[string]string{
		"X-Ratelimit-Resource": "", "X-Ratelimit-Limit": "0", "X-Ratelimit-Remaining": "many",
		"X-Ratelimit-Used": "", "X-Ratelimit-Reset": "soon",
	} {
		broken := full.Clone()
		broken.Set(name, header)

		_, ok := readingFrom(broken)
		assert.False(t, ok, "a bad %s is no reading", name)
	}
}

func TestAGraphQLQueryReportsItsCost(t *testing.T) {
	t.Parallel()

	client, rates := observingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		setRateHeaders(w, ResourceGraphQL, 4985, 15)
		_, _ = io.WriteString(w, plainIssue)
	})

	_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID}, DetailOptions{})
	require.NoError(t, err)

	reading := RateReading{Resource: ResourceGraphQL, Limit: 5000, Remaining: 4985, Used: 15, ResetAt: rateResetAt}
	assert.Equal(t, []observed{{reading: reading, spent: 0}, {reading: reading, spent: 1}}, rates.all(),
		"the response is read once as it arrives, then spent by the cost its query reports")
}

func TestARESTCallSpendsOneAndARefusalStillCounts(t *testing.T) {
	t.Parallel()

	client, rates := observingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		setRateHeaders(w, ResourceCore, 4970, 30)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"Not Found"}`)
	})

	ref := item.Ref{Repo: repo.Name{Owner: filesOwner, Repo: exampleRepo}, Number: 7, Kind: item.KindPR}
	_, err := client.FetchPullHead(t.Context(), NewToken(fakeToken), ref)
	require.ErrorIs(t, err, ErrUnexpectedStatus)

	assert.Equal(t, []observed{{
		reading: RateReading{Resource: ResourceCore, Limit: 5000, Remaining: 4970, Used: 30, ResetAt: rateResetAt},
		spent:   1,
	}}, rates.all())
}

func TestTheRateLimitEndpointIsFreeAndReportsEveryBudget(t *testing.T) {
	t.Parallel()

	body := fixture(t, "rate_limit_response.json")
	client, rates := observingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		setRateHeaders(w, ResourceCore, 4988, 12)
		_, _ = w.Write(body)
	})

	_, err := client.FetchRateLimits(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)

	spent := map[string]int{}

	for _, o := range rates.all() {
		spent[o.reading.Resource] += o.spent
	}

	assert.Equal(t, map[string]int{ResourceCore: 0, ResourceGraphQL: 0, ResourceSearch: 0}, spent)
}

func TestAResponseWithoutRateHeadersIsNotObserved(t *testing.T) {
	t.Parallel()

	client, rates := observingClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, plainIssue)
	})

	_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID}, DetailOptions{})
	require.NoError(t, err)
	assert.Empty(t, rates.all())
}

func TestAClientWithoutAnObserverStillWorks(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		setRateHeaders(w, ResourceGraphQL, 4985, 15)
		_, _ = io.WriteString(w, plainIssue)
	})

	_, err := client.FetchDetails(t.Context(), NewToken(fakeToken), []string{issueID}, DetailOptions{})
	require.NoError(t, err)
}

func TestQueryCost(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 4, queryCost([]byte(`{"data":{"rateLimit":{"cost":4}}}`)))
	assert.Zero(t, queryCost([]byte(`{"data":{"viewer":{}}}`)), "a query that asks for no cost")
	assert.Zero(t, queryCost([]byte(refusedProjects)), "a query refused before it ran")
	assert.Zero(t, queryCost([]byte(`not json`)))
}

func TestTheStatesQueryAsksForItsCost(t *testing.T) {
	t.Parallel()

	query, err := statesQuery([]item.Ref{{Repo: repo.Name{Owner: filesOwner, Repo: exampleRepo}, Number: 7}})
	require.NoError(t, err)
	assert.Contains(t, query, "rateLimit { cost }")
}

func TestLaterReadings(t *testing.T) {
	t.Parallel()

	reset := time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC)
	early := QueryRate{Used: 10, Remaining: 4990, ResetAt: reset}
	late := QueryRate{Used: 14, Remaining: 4986, ResetAt: reset}
	next := QueryRate{Used: 1, Remaining: 4999, ResetAt: reset.Add(time.Hour)}

	assert.True(t, late.Later(early), "more used in the same window")
	assert.False(t, early.Later(late))
	assert.True(t, next.Later(late), "a later window, whatever it used")
	assert.False(t, late.Later(next))
	assert.False(t, late.Later(late))
}
