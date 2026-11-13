package github

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// GitHub reports a budget on every response, REST and GraphQL alike, in these headers:
// the budget the request counted against, its size, what is left and used after the
// request, and when it refills, as a Unix time in seconds.
const (
	headerRateResource  = "X-Ratelimit-Resource"
	headerRateLimit     = "X-Ratelimit-Limit"
	headerRateRemaining = "X-Ratelimit-Remaining"
	headerRateUsed      = "X-Ratelimit-Used"
	headerRateReset     = "X-Ratelimit-Reset"
)

// graphQLPath is the GraphQL endpoint, whose requests cost what their query reports.
const graphQLPath = "/graphql"

// Budgets GitHub names in a reading's Resource.
const (
	ResourceGraphQL = "graphql"
	ResourceCore    = "core"
	ResourceSearch  = "search"
)

// RateReading is one budget as GitHub reported it with a response.
type RateReading struct {
	// Resource names the budget: ResourceGraphQL, ResourceCore, ResourceSearch, or
	// another GitHub keeps.
	Resource string

	Limit     int
	Remaining int
	Used      int

	// ResetAt is when the budget refills in full.
	ResetAt time.Time
}

// RateObserver hears every budget reading the client's responses carry, with what the
// request spent from that budget: a GraphQL query's reported cost, one for a REST call,
// and nothing for GET /rate_limit, which is free. It must return promptly; requests sent
// side by side call it concurrently.
type RateObserver interface {
	ObserveRate(reading RateReading, spent int)
}

// readingFrom reads a response's rate-limit headers; ok is false when it has none, as an
// answer from anywhere but GitHub's API.
func readingFrom(h http.Header) (RateReading, bool) {
	resource := h.Get(headerRateResource)
	limit, errLimit := strconv.Atoi(h.Get(headerRateLimit))
	remaining, errRemaining := strconv.Atoi(h.Get(headerRateRemaining))
	used, errUsed := strconv.Atoi(h.Get(headerRateUsed))
	reset, errReset := strconv.ParseInt(h.Get(headerRateReset), 10, 64)

	if resource == "" || limit <= 0 || errLimit != nil || errRemaining != nil || errUsed != nil || errReset != nil {
		return RateReading{}, false
	}

	return RateReading{
		Resource: resource, Limit: limit, Remaining: remaining, Used: used, ResetAt: time.Unix(reset, 0).UTC(),
	}, true
}

// observe tells the client's observer, if any, about a response's reading and what the
// request spent.
func (c *Client) observe(h http.Header, spent int) {
	if c.Rates == nil {
		return
	}

	if reading, ok := readingFrom(h); ok {
		c.Rates.ObserveRate(reading, spent)
	}
}

// observeRates tells the client's observer about every budget GET /rate_limit reported,
// none of which the call spent.
func (c *Client) observeRates(limits *RateLimits) {
	if c.Rates == nil {
		return
	}

	for resource, r := range map[string]Rate{
		ResourceGraphQL: limits.GraphQL, ResourceCore: limits.Core, ResourceSearch: limits.Search,
	} {
		if r.Limit > 0 {
			c.Rates.ObserveRate(RateReading{
				Resource: resource, Limit: r.Limit, Remaining: r.Remaining, Used: r.Used, ResetAt: r.ResetAt,
			}, 0)
		}
	}
}

// restSpend is what a REST call to path spends: one point, except the rate limit endpoint,
// which is free. GraphQL requests are counted by their query's cost instead (queryCost).
func restSpend(path string) int {
	if path == graphQLPath || path == rateLimitPath {
		return 0
	}

	return 1
}

// minQueryCost is the least a GraphQL query GitHub ran costs.
const minQueryCost = 1

// queryCost reads the cost a GraphQL response reports for its query, or 0 when it reports
// none, as a query GitHub refused before running it.
func queryCost(raw []byte) int {
	var body struct {
		Data *struct {
			RateLimit *struct {
				Cost int `json:"cost"`
			} `json:"rateLimit"`
		} `json:"data"`
	}

	if json.Unmarshal(raw, &body) != nil || body.Data == nil || body.Data.RateLimit == nil {
		return 0
	}

	return body.Data.RateLimit.Cost
}
