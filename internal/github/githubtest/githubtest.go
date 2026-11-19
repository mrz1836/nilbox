// Package githubtest holds the fakes of internal/github that the test suites share: a
// token source that never reaches GitHub, the bookkeeping of a fake GitHub's requests, and
// GitHub's refusal of a token. internal/testutil cannot hold them, as internal/github
// imports it. Only tests may import this package.
package githubtest

import (
	"context"
	"net/http"
	"sync"

	"github.com/mrz1836/nilbox/internal/github"
)

// Token is the token a Tokens lends when it was given none: it never reaches GitHub.
const Token = "test-token-not-real"

// Tokens is a token source that lends tokens in turn and records the ones it is told to
// forget. It is safe for concurrent use; a test reads it with Loans and Forgot once the
// calls it made have returned.
type Tokens struct {
	mu     sync.Mutex
	values []string
	errs   []error
	fail   error
	lent   int
	forgot []string
}

// NewTokens returns Tokens that lend values in turn, the last of them again once the rest
// are lent, or Token when there are none.
func NewTokens(values ...string) *Tokens {
	return &Tokens{values: values}
}

// FailingTokens returns Tokens whose loans fail in turn with errs, a nil error lending
// values as NewTokens does, and every loan after the last of errs lending a token.
func FailingTokens(errs []error, values ...string) *Tokens {
	return &Tokens{values: values, errs: errs}
}

// Token lends the next token. A loan counts even when it fails: on a context that is done,
// with its error; with the loan's own error from FailingTokens; or with the error Fail set.
func (f *Tokens) Token(ctx context.Context) (github.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := f.lent
	f.lent++

	switch {
	case ctx.Err() != nil:
		return github.Token{}, ctx.Err()
	case i < len(f.errs) && f.errs[i] != nil:
		return github.Token{}, f.errs[i]
	case f.fail != nil:
		return github.Token{}, f.fail
	case len(f.values) == 0:
		return github.NewToken(Token), nil
	default:
		return github.NewToken(f.values[min(i, len(f.values)-1)]), nil
	}
}

// Forget records tok as given back, as GitHub refused it.
func (f *Tokens) Forget(tok github.Token) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.forgot = append(f.forgot, tok.Reveal())
}

// Fail makes every loan from now on fail with err, or lend again for nil.
func (f *Tokens) Fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fail = err
}

// Swap lends value from now on, as gh auth switch does.
func (f *Tokens) Swap(value string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.values = []string{value}
}

// Loans returns how many tokens were asked for, the failed loans among them.
func (f *Tokens) Loans() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.lent
}

// Forgot returns the tokens given back, in order.
func (f *Tokens) Forgot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.forgot...)
}

// Calls counts a fake GitHub's requests by name and keeps their order. Its zero value is
// ready to use, and it is safe for concurrent use.
type Calls struct {
	mu     sync.Mutex
	counts map[string]int
	order  []string
}

// Add records a request named name.
func (c *Calls) Add(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.counts == nil {
		c.counts = map[string]int{}
	}

	c.counts[name]++
	c.order = append(c.order, name)
}

// Count returns how many requests named name were made.
func (c *Calls) Count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.counts[name]
}

// Len returns how many requests were made, whatever their names.
func (c *Calls) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.order)
}

// Since names the requests made after the last one named any of names, in order: every
// request when none of names was made.
func (c *Calls) Since(names ...string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	last := -1

	for i, name := range c.order {
		for _, n := range names {
			if name == n {
				last = i
			}
		}
	}

	return append([]string{}, c.order[last+1:]...)
}

// BadCredentials is GitHub refusing the token.
func BadCredentials() error {
	return &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
}
