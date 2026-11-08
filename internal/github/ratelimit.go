package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// rateLimitPath is the REST endpoint that reports every budget. Calling it is free.
const rateLimitPath = "/rate_limit"

// Rate is one API budget.
type Rate struct {
	Limit     int
	Used      int
	Remaining int
	ResetAt   time.Time
}

// RateLimits holds the budgets the rate gauge shows.
type RateLimits struct {
	Core    Rate
	GraphQL Rate
	Search  Rate
}

// fetchRateLimits calls GET /rate_limit and decodes the three budgets.
func (c *Client) fetchRateLimits(ctx context.Context, tok Token) (*RateLimits, error) {
	data, _, err := c.do(ctx, tok, http.MethodGet, rateLimitPath, nil)
	if err != nil {
		return nil, err
	}

	var resp restRateLimitResponse
	if err = json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("%w: decode the rate limit response: %w", ErrMalformedResponse, err)
	}

	res := resp.Resources
	if res.Core == nil || res.GraphQL == nil || res.Search == nil {
		return nil, fmt.Errorf("%w: the rate limit response is missing a resource", ErrMalformedResponse)
	}

	limits := &RateLimits{Core: res.Core.rate(), GraphQL: res.GraphQL.rate(), Search: res.Search.rate()}
	c.observeRates(limits)

	return limits, nil
}

// restRateLimitResponse is the GET /rate_limit body.
type restRateLimitResponse struct {
	Resources restRateResources `json:"resources"`
}

// restRateResources holds the budgets this app reads.
type restRateResources struct {
	Core    *restRate `json:"core"`
	GraphQL *restRate `json:"graphql"`
	Search  *restRate `json:"search"`
}

// restRate is one REST budget; Reset is a Unix time in seconds.
type restRate struct {
	Limit     int   `json:"limit"`
	Used      int   `json:"used"`
	Remaining int   `json:"remaining"`
	Reset     int64 `json:"reset"`
}

// rate converts the REST budget.
func (r *restRate) rate() Rate {
	return Rate{Limit: r.Limit, Used: r.Used, Remaining: r.Remaining, ResetAt: time.Unix(r.Reset, 0).UTC()}
}
