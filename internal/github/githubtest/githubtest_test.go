package githubtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/github/githubtest"
)

var errSignedOut = errors.New("signed out")

// lend borrows a token from tokens and returns its value, or the error.
func lend(t *testing.T, ctx context.Context, tokens *githubtest.Tokens) (string, error) {
	t.Helper()

	tok, err := tokens.Token(ctx)

	return tok.Reveal(), err
}

func TestTokensLendInTurnAndKeepLendingTheLast(t *testing.T) {
	t.Parallel()

	tokens := githubtest.NewTokens("first-token-not-real", "second-token-not-real")

	for _, want := range []string{"first-token-not-real", "second-token-not-real", "second-token-not-real"} {
		got, err := lend(t, t.Context(), tokens)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	assert.Equal(t, 3, tokens.Loans())
}

func TestTokensGivenNoneLendTheFakeToken(t *testing.T) {
	t.Parallel()

	got, err := lend(t, t.Context(), githubtest.NewTokens())
	require.NoError(t, err)
	assert.Equal(t, githubtest.Token, got)
}

func TestTokensFailAsTheyAreTold(t *testing.T) {
	t.Parallel()

	tokens := githubtest.FailingTokens([]error{errSignedOut, nil})

	_, err := lend(t, t.Context(), tokens)
	require.ErrorIs(t, err, errSignedOut, "the first loan's own error")

	got, err := lend(t, t.Context(), tokens)
	require.NoError(t, err, "a nil error lends")
	assert.Equal(t, githubtest.Token, got)

	tokens.Fail(github.ErrGHNotFound)

	_, err = lend(t, t.Context(), tokens)
	require.ErrorIs(t, err, github.ErrGHNotFound, "every loan fails once Fail is set")

	tokens.Fail(nil)

	_, err = lend(t, t.Context(), tokens)
	require.NoError(t, err, "and lends again once it is cleared")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = lend(t, ctx, tokens)
	require.ErrorIs(t, err, context.Canceled, "a loan on a context that is done fails with its error")
	assert.Equal(t, 5, tokens.Loans(), "a failed loan counts")
}

func TestTokensSwapAndRecordWhatIsForgotten(t *testing.T) {
	t.Parallel()

	tokens := githubtest.NewTokens("old-token-not-real")
	tokens.Forget(github.NewToken("old-token-not-real"))
	tokens.Swap("new-token-not-real")

	got, err := lend(t, t.Context(), tokens)
	require.NoError(t, err)
	assert.Equal(t, "new-token-not-real", got, "a swapped token is lent from then on")
	assert.Equal(t, []string{"old-token-not-real"}, tokens.Forgot())
}

func TestCallsCountAndOrderTheRequests(t *testing.T) {
	t.Parallel()

	var calls githubtest.Calls

	assert.Empty(t, calls.Since("view"), "the zero value is ready")

	for _, name := range []string{"view", "comment", "view", "close", "label"} {
		calls.Add(name)
	}

	assert.Equal(t, 2, calls.Count("view"))
	assert.Zero(t, calls.Count("merge"))
	assert.Equal(t, 5, calls.Len())
	assert.Equal(t, []string{"close", "label"}, calls.Since("view"), "the requests after the last view")
	assert.Equal(t, []string{"label"}, calls.Since("view", "close"), "after the last of either")
	assert.Equal(t, []string{"view", "comment", "view", "close", "label"}, calls.Since("merge"),
		"every request when none of the names was made")
}

func TestBadCredentialsIsGitHubRefusingTheToken(t *testing.T) {
	t.Parallel()

	assert.True(t, github.IsBadCredentials(githubtest.BadCredentials()))
}
