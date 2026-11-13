package github

import "sync"

// ViewerCache remembers the account the last token it was told of belongs to, so its holder
// asks GitHub who the token is (FetchViewer) only for a token it hasn't seen. The token
// stays in memory, as the TokenSource keeps it, and is only compared (Token.Same). The zero
// value is ready, and it is safe for concurrent use.
type ViewerCache struct {
	mu    sync.Mutex
	tok   Token
	login string
}

// Get returns the account tok belongs to, if tok is the token remembered.
func (c *ViewerCache) Get(tok Token) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.login == "" || !c.tok.Same(tok) {
		return "", false
	}

	return c.login, true
}

// Remember records that tok belongs to the account login, in place of the one remembered.
// A zero token or an empty login is not remembered.
func (c *ViewerCache) Remember(tok Token, login string) {
	if tok.IsZero() || login == "" {
		return
	}

	c.mu.Lock()
	c.tok, c.login = tok, login
	c.mu.Unlock()
}
