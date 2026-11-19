package github_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/github/githubtest"
)

var errFakeGitHub = errors.New("fake: GitHub is down")

// refused is GitHub's answer to a token it does not accept.
func refused() error {
	return github.Redact(fmt.Errorf("read the inbox: %w",
		&github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}), github.NewToken(githubtest.Token))
}

func TestWithTokenRetriesOnceWithAFreshToken(t *testing.T) {
	t.Parallel()

	tokens := githubtest.NewTokens("old-token-not-real", "new-token-not-real")

	var used []string

	got, err := github.WithToken(t.Context(), tokens, func(tok github.Token) (int, error) {
		used = append(used, tok.Reveal())
		if len(used) == 1 {
			return 0, refused()
		}

		return 7, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 7, got)
	assert.Equal(t, []string{"old-token-not-real", "new-token-not-real"}, used)
	assert.Equal(t, []string{"old-token-not-real"}, tokens.Forgot())
}

func TestWithTokenGivesUpAfterOneRetry(t *testing.T) {
	t.Parallel()

	tokens := githubtest.NewTokens()
	calls := 0

	_, err := github.WithToken(t.Context(), tokens, func(github.Token) (int, error) {
		calls++

		return 0, refused()
	})
	require.ErrorIs(t, err, github.ErrUnexpectedStatus)
	assert.Equal(t, 2, calls)
	assert.Equal(t, []string{githubtest.Token}, tokens.Forgot())
}

func TestWithTokenLeavesOtherFailuresAlone(t *testing.T) {
	t.Parallel()

	tokens := githubtest.NewTokens()
	calls := 0

	_, err := github.WithToken(t.Context(), tokens, func(github.Token) (int, error) {
		calls++

		return 0, errFakeGitHub
	})
	require.ErrorIs(t, err, errFakeGitHub)
	assert.Equal(t, 1, calls)
	assert.Empty(t, tokens.Forgot())
}

func TestWithTokenReturnsATokenFailure(t *testing.T) {
	t.Parallel()

	never := func(github.Token) (int, error) {
		require.FailNow(t, "nothing is asked without a token")

		return 0, nil
	}

	_, err := github.WithToken(t.Context(), githubtest.FailingTokens([]error{github.ErrNotLoggedIn}), never)
	require.ErrorIs(t, err, github.ErrNotLoggedIn)

	tokens := githubtest.FailingTokens([]error{nil, github.ErrGHNotFound})

	_, err = github.WithToken(t.Context(), tokens, func(github.Token) (int, error) { return 0, refused() })
	require.ErrorIs(t, err, github.ErrGHNotFound, "a failure to borrow the fresh token is returned")
}

func TestIsBadCredentials(t *testing.T) {
	t.Parallel()

	assert.True(t, github.IsBadCredentials(refused()))
	assert.True(t, github.IsBadCredentials(githubtest.BadCredentials()))
	assert.True(t, github.IsBadCredentials(&github.StatusError{StatusCode: http.StatusForbidden, Message: "Bad credentials"}))
	assert.False(t, github.IsBadCredentials(&github.StatusError{StatusCode: http.StatusForbidden, Message: "API rate limit exceeded"}))
	assert.False(t, github.IsBadCredentials(errFakeGitHub))
	assert.False(t, github.IsBadCredentials(nil))
}
