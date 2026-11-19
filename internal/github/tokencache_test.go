package github

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// waitTimeout bounds every wait in these tests.
const waitTimeout = 5 * time.Second

// borrowedAt is when the token cache tests start.
func borrowedAt() time.Time { return time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC) }

// cachingSource returns a TokenSource over a runner that prints fakeToken, with a fake
// clock, and the runner and clock.
func cachingSource(opts ...TokenOption) (*TokenSource, *testutil.Runner, *testutil.Clock) {
	runner := &testutil.Runner{Stdout: fakeToken + "\n"}
	clk := testutil.NewClock(borrowedAt())
	src := NewTokenSource(runner, testutil.Finder{Path: fakeGHPath}, append([]TokenOption{WithTokenClock(clk)}, opts...)...)

	return src, runner, clk
}

// borrow asks src for a token that must come.
func borrow(t *testing.T, src *TokenSource) Token {
	t.Helper()

	tok, err := src.Token(t.Context())
	require.NoError(t, err)
	require.Equal(t, fakeToken, tok.Reveal())

	return tok
}

func TestTokenSourceKeepsTheTokenForTheTTL(t *testing.T) {
	t.Parallel()

	src, runner, clk := cachingSource()

	borrow(t, src)
	clk.Advance(TokenTTL - time.Second)
	borrow(t, src)
	assert.Len(t, runner.Calls(), 1, "gh ran once while the token was young")

	clk.Advance(time.Second)
	borrow(t, src)
	assert.Len(t, runner.Calls(), 2, "gh ran again once the token was TokenTTL old")
}

func TestTokenSourceSharesOneGHRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		started := make(chan struct{}, 8)
		release := make(chan struct{})
		runner := &testutil.Runner{Script: func(context.Context, []string) ([]byte, []byte, error) {
			started <- struct{}{}

			<-release

			return []byte(fakeToken + "\n"), nil, nil
		}}
		src := NewTokenSource(runner, testutil.Finder{Path: fakeGHPath})

		const callers = 5

		var wg sync.WaitGroup

		tokens := make([]Token, callers)
		errs := make([]error, callers)

		for i := range callers {
			wg.Go(func() { tokens[i], errs[i] = src.Token(t.Context()) })
		}

		select {
		case <-started:
		case <-time.After(waitTimeout):
			require.FailNow(t, "gh never ran")
		}

		synctest.Wait() // every other caller has joined the run in flight and is waiting on it
		close(release)
		wg.Wait()

		for i := range callers {
			require.NoError(t, errs[i])
			assert.Equal(t, fakeToken, tokens[i].Reveal())
		}

		assert.Len(t, runner.Calls(), 1, "every caller shared one gh run")
	})
}

func TestTokenSourceForgetsOnlyTheTokenRefused(t *testing.T) {
	t.Parallel()

	src, runner, _ := cachingSource()
	held := borrow(t, src)

	src.Forget(Token{})
	src.Forget(NewToken("another-token-not-real"))
	borrow(t, src)
	assert.Len(t, runner.Calls(), 1, "forgetting another token keeps the one held")

	src.Forget(held)
	borrow(t, src)
	assert.Len(t, runner.Calls(), 2, "the refused token is borrowed again")
}

func TestTokenSourceClear(t *testing.T) {
	t.Parallel()

	src, runner, _ := cachingSource()
	borrow(t, src)

	src.Clear()
	borrow(t, src)
	assert.Len(t, runner.Calls(), 2)
}

func TestTokenSourceDoesNotKeepAFailure(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{Err: errFakeExit}
	src := NewTokenSource(runner, testutil.Finder{Path: fakeGHPath})

	for range 2 {
		_, err := src.Token(t.Context())
		require.ErrorIs(t, err, ErrNotLoggedIn)
	}

	assert.Len(t, runner.Calls(), 2, "a failed borrow is tried again")
}

func TestTokenSourceDropsTheTokenWhenGHsAccountChanges(t *testing.T) {
	t.Parallel()

	hosts := filepath.Join(t.TempDir(), "hosts.yml")
	src, runner, _ := cachingSource(WithHostsFile(hosts))

	borrow(t, src)
	borrow(t, src)
	assert.Len(t, runner.Calls(), 1, "no hosts file is no change")

	testutil.WriteFile(t, hosts, "github.com:\n    user: octo-user\n")
	borrow(t, src)
	assert.Len(t, runner.Calls(), 2, "gh logging in drops the token")

	borrow(t, src)
	assert.Len(t, runner.Calls(), 2, "an unchanged hosts file keeps it")

	testutil.WriteFile(t, hosts, "github.com:\n    user: octo-other\n")
	require.NoError(t, os.Chtimes(hosts, borrowedAt(), borrowedAt().Add(time.Hour)))
	borrow(t, src)
	assert.Len(t, runner.Calls(), 3, "gh auth switch drops the token")

	require.NoError(t, os.Remove(hosts))
	borrow(t, src)
	assert.Len(t, runner.Calls(), 4, "gh logging out drops the token")
}

func TestGHHostsFile(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")

	t.Setenv("GH_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	assert.Equal(t, filepath.Join(home, ".config", "gh", "hosts.yml"), GHHostsFile(home))

	t.Setenv("XDG_CONFIG_HOME", "/srv/config")
	assert.Equal(t, filepath.Join("/srv/config", "gh", "hosts.yml"), GHHostsFile(home))

	t.Setenv("GH_CONFIG_DIR", "/srv/gh")
	assert.Equal(t, filepath.Join("/srv/gh", "hosts.yml"), GHHostsFile(home))
}
