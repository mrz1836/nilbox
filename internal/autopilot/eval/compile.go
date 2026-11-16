package eval

import (
	"regexp"
	"sync"

	"github.com/mrz1836/nilbox/internal/sensitive"
)

// maxCompiled caps each cache, so routines that come and go can't grow it without end;
// past it, a pattern is compiled each time it is used.
const maxCompiled = 4096

// compiled holds what compiling a pattern's text gave, so a regex or a path pattern is
// compiled once, not for every item of every cycle. It is safe for evaluations at once.
type compiled[T any] struct {
	compile func(string) (T, error)

	mu      sync.RWMutex
	entries map[string]compiledEntry[T]
}

// compiledEntry is one pattern's compilation: its value, and whether it compiled.
type compiledEntry[T any] struct {
	value T
	ok    bool
}

// newCompiled returns an empty cache that compiles with compile.
func newCompiled[T any](compile func(string) (T, error)) *compiled[T] {
	return &compiled[T]{compile: compile, entries: map[string]compiledEntry[T]{}}
}

// get returns text compiled, and false for text that doesn't compile.
func (c *compiled[T]) get(text string) (T, bool) {
	c.mu.RLock()
	e, hit := c.entries[text]
	c.mu.RUnlock()

	if hit {
		return e.value, e.ok
	}

	v, err := c.compile(text)
	e = compiledEntry[T]{value: v, ok: err == nil}

	c.mu.Lock()
	if len(c.entries) < maxCompiled {
		c.entries[text] = e
	}
	c.mu.Unlock()

	return e.value, e.ok
}

// regexes are the conditions' regexes, compiled to ignore case. A compiled regex is safe
// to match from evaluations at once.
//
//nolint:gochecknoglobals // a cache every evaluation shares
var regexes = newCompiled(func(s string) (*regexp.Regexp, error) { return regexp.Compile("(?i)" + s) })

// pathPatterns are the conditions' .gitignore-style patterns on changed paths, which only
// read once compiled.
//
//nolint:gochecknoglobals // a cache every evaluation shares
var pathPatterns = newCompiled(sensitive.CompilePattern)
