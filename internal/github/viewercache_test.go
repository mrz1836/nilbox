package github

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestViewerCacheRemembersTheLastTokensAccount(t *testing.T) {
	t.Parallel()

	var c ViewerCache

	_, ok := c.Get(NewToken(fakeToken))
	assert.False(t, ok, "nothing remembered yet")

	c.Remember(NewToken(fakeToken), "octo-user")

	login, ok := c.Get(NewToken(fakeToken))
	assert.True(t, ok, "the same token, by its value")
	assert.Equal(t, "octo-user", login)

	_, ok = c.Get(NewToken("another-test-token-not-real"))
	assert.False(t, ok, "another token is asked about")

	c.Remember(NewToken("another-test-token-not-real"), "octo-other")

	_, ok = c.Get(NewToken(fakeToken))
	assert.False(t, ok, "only the last token is remembered")

	login, ok = c.Get(NewToken("another-test-token-not-real"))
	assert.True(t, ok)
	assert.Equal(t, "octo-other", login)
}

func TestViewerCacheIgnoresNothingToRemember(t *testing.T) {
	t.Parallel()

	var c ViewerCache

	c.Remember(NewToken(fakeToken), "octo-user")
	c.Remember(Token{}, "octo-other")
	c.Remember(NewToken("another-test-token-not-real"), "")

	login, ok := c.Get(NewToken(fakeToken))
	assert.True(t, ok, "a zero token or an empty login replaces nothing")
	assert.Equal(t, "octo-user", login)

	_, ok = c.Get(Token{})
	assert.False(t, ok, "a zero token belongs to no account")
}

func TestViewerCacheIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	var (
		c  ViewerCache
		wg sync.WaitGroup
	)

	for range 8 {
		wg.Go(func() {
			c.Remember(NewToken(fakeToken), "octo-user")

			if login, ok := c.Get(NewToken(fakeToken)); ok {
				assert.Equal(t, "octo-user", login)
			}
		})
	}

	wg.Wait()
}
