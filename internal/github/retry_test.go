package github

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

var errNotGitHubs = errors.New("save the inbox: database is locked")

func TestRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"none", nil, false},
		{"a server error", &StatusError{StatusCode: http.StatusInternalServerError}, true},
		{"a bad gateway", fmt.Errorf("read the item details: %w", &StatusError{StatusCode: http.StatusBadGateway}), true},
		{"unavailable", &StatusError{StatusCode: http.StatusServiceUnavailable}, true},
		{"too many requests", &StatusError{StatusCode: http.StatusTooManyRequests}, true},
		{"a rate limit's 403", &StatusError{StatusCode: http.StatusForbidden, Message: "API rate limit exceeded"}, true},
		{"asked to wait", &StatusError{StatusCode: http.StatusForbidden, RetryAfter: time.Minute}, true},
		{"a refused token", &StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}, false},
		{"forbidden", &StatusError{StatusCode: http.StatusForbidden, Message: "Resource not accessible"}, false},
		{"a missing resource", &StatusError{StatusCode: http.StatusNotFound}, false},
		{"a query refused for the rate limits", &queryError{limited: true}, true},
		{"a query refused for its scopes", &queryError{scopes: true}, false},
		{"a query GitHub turned down", &queryError{message: "Field 'nope' doesn't exist"}, false},
		{"an answer cut off", fmt.Errorf("read the GitHub API response: %w", io.ErrUnexpectedEOF), true},
		{"not GitHub's", errNotGitHubs, false},
		{"a malformed answer", ErrMalformedResponse, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Retryable(tt.err))
		})
	}
}

func TestRetryableReadsTheClientsOwnFailures(t *testing.T) {
	t.Parallel()

	t.Run("no answer", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.NotFoundHandler())
		client := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
		srv.Close()

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.Equal(t, unreachableMessage, uierr.Message(err))
		assert.True(t, Retryable(err), "a GitHub that can't be reached may be reached later")
	})

	t.Run("a server error", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.True(t, Retryable(err))
		assert.Zero(t, RetryAfter(err), "GitHub asked for no wait")
	})

	t.Run("a refused token", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		})

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.False(t, Retryable(err), "a refused token stays refused until the user signs in again")
	})

	t.Run("an answer cut off", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "1000")
			_, _ = w.Write([]byte(`{"data":`))
		})

		_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
		require.Error(t, err)
		assert.True(t, Retryable(err))
	})
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", strconv.Itoa(90))
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	_, err := client.FetchInbox(t.Context(), NewToken(fakeToken))
	require.Error(t, err)
	assert.Equal(t, 90*time.Second, RetryAfter(err), "what GitHub's Retry-After said")

	assert.Equal(t, secondaryLimitWait, RetryAfter(&StatusError{StatusCode: http.StatusTooManyRequests}),
		"a minute for a rate limit that said nothing")
	assert.Equal(t, secondaryLimitWait, RetryAfter(&queryError{limited: true}))
	assert.Zero(t, RetryAfter(&StatusError{StatusCode: http.StatusBadGateway}))
	assert.Zero(t, RetryAfter(errNotGitHubs))
}
