package github

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchRateLimits(t *testing.T) {
	t.Parallel()

	body := fixture(t, "rate_limit_response.json")

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/rate_limit", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Content-Type"), "a GET has no body")

		_, _ = w.Write(body)
	})

	limits, err := client.FetchRateLimits(t.Context(), NewToken(fakeToken))
	require.NoError(t, err)

	reset := time.Date(2026, 1, 6, 13, 0, 0, 0, time.UTC)
	assert.Equal(t, Rate{Limit: 5000, Used: 12, Remaining: 4988, ResetAt: reset}, limits.Core)
	assert.Equal(t, Rate{Limit: 5000, Used: 1, Remaining: 4999, ResetAt: reset}, limits.GraphQL)
	assert.Equal(t, Rate{Limit: 30, Used: 1, Remaining: 29, ResetAt: time.Date(2026, 1, 6, 12, 1, 0, 0, time.UTC)}, limits.Search)
}

func TestFetchRateLimitsErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "not json", status: http.StatusOK, body: "nope", wantErr: ErrMalformedResponse},
		{name: "missing resource", status: http.StatusOK, body: `{"resources":{"core":{"limit":5000}}}`, wantErr: ErrMalformedResponse},
		{name: "server error", status: http.StatusInternalServerError, body: `{"message":"oops"}`, wantErr: ErrUnexpectedStatus},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			limits, err := client.FetchRateLimits(t.Context(), NewToken(fakeToken))
			require.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, limits)
		})
	}
}
