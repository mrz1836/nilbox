// Package github talks to GitHub on the user's behalf.
//
// The token is borrowed from the GitHub CLI (gh auth token) on every refresh and held only
// in memory. It never reaches a log line, an error message, or the database: the Token
// type prints as [redacted] in every format, and every error that leaves this package is
// scrubbed of the token text.
//
// The inbox comes from one batched GraphQL request (three aliased searches plus the
// query's rate-limit cost); the REST rate-limit endpoint fills in the remaining budgets.
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os/exec"
	"strings"
)

// redactedText replaces the token wherever it would otherwise be printed.
const redactedText = "[redacted]"

// ghHost is the GitHub host the token is borrowed for.
const ghHost = "github.com"

var (
	// ErrGHNotFound is returned when the GitHub CLI is not installed or not on the search path.
	ErrGHNotFound = errors.New("the GitHub CLI (gh) was not found: install the GitHub CLI from https://cli.github.com, then run gh auth login")

	// ErrNotLoggedIn is returned when the GitHub CLI has no token for github.com.
	ErrNotLoggedIn = errors.New("the GitHub CLI is not logged in to github.com: run gh auth login")
)

// Runner runs a program with arguments and returns what it wrote. Implementations run the
// argv directly, never through a shell.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Finder resolves a program name to the absolute path of an executable.
type Finder interface {
	Find(name string) (string, error)
}

// Token is a GitHub API token. Its value is only reachable through Reveal; printing,
// logging, or encoding a Token always yields [redacted].
//
// The value sits behind a pointer so that even reflection-based printing of a struct
// holding a Token in an unexported field shows an address, not the token.
type Token struct {
	secret *string
}

// NewToken wraps a raw token value.
func NewToken(value string) Token {
	return Token{secret: &value}
}

// Reveal returns the raw token. Use it only to set the Authorization header.
func (t Token) Reveal() string {
	if t.secret == nil {
		return ""
	}

	return *t.secret
}

// IsZero reports whether the token is empty.
func (t Token) IsZero() bool {
	return t.Reveal() == ""
}

// String returns [redacted].
func (Token) String() string { return redactedText }

// GoString returns [redacted], so %#v never shows the token.
func (Token) GoString() string { return redactedText }

// Format prints [redacted] for every verb and flag.
func (Token) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, redactedText)
}

// MarshalJSON encodes the token as the string "[redacted]".
func (Token) MarshalJSON() ([]byte, error) {
	return []byte(`"` + redactedText + `"`), nil
}

// MarshalText encodes the token as [redacted].
func (Token) MarshalText() ([]byte, error) {
	return []byte(redactedText), nil
}

// LogValue makes slog record [redacted] for a token.
func (Token) LogValue() slog.Value {
	return slog.StringValue(redactedText)
}

// TokenSource borrows the GitHub CLI's token for github.com.
type TokenSource struct {
	runner Runner
	finder Finder
}

// NewTokenSource returns a TokenSource that finds gh with finder and runs it with runner.
func NewTokenSource(runner Runner, finder Finder) *TokenSource {
	return &TokenSource{runner: runner, finder: finder}
}

// Token runs gh auth token and returns the token it prints. It returns ErrGHNotFound when
// gh is missing and ErrNotLoggedIn when gh has no token. The command's output is never
// included in an error.
func (s *TokenSource) Token(ctx context.Context) (Token, error) {
	path, err := s.finder.Find("gh")
	if err != nil {
		return Token{}, fmt.Errorf("%w: %w", ErrGHNotFound, err)
	}

	stdout, _, err := s.runner.Run(ctx, path, "auth", "token", "--hostname", ghHost)
	if err != nil {
		return Token{}, tokenRunError(ctx, err)
	}

	value := strings.TrimSpace(string(stdout))
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return Token{}, ErrNotLoggedIn
	}

	return NewToken(value), nil
}

// tokenRunError maps a failed gh run to an actionable error without its output.
func tokenRunError(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("borrow the GitHub CLI token: %w", ctx.Err())
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return ErrGHNotFound
	default:
		return ErrNotLoggedIn
	}
}

// Redact returns err with every occurrence of the token's text replaced by [redacted].
// errors.Is and errors.As still see the original chain, but the unredacted message is not
// reachable through errors.Unwrap. A nil error or an empty token returns err unchanged.
func Redact(err error, tok Token) error {
	secret := tok.Reveal()
	if err == nil || secret == "" {
		return err
	}

	return &redactedError{err: err, secret: secret}
}

// redactedError hides a token inside another error's message.
type redactedError struct {
	err    error
	secret string
}

// Error returns the wrapped message with the token replaced.
func (e *redactedError) Error() string {
	return strings.ReplaceAll(e.err.Error(), e.secret, redactedText)
}

// Is reports whether the wrapped chain matches target.
func (e *redactedError) Is(target error) bool {
	return errors.Is(e.err, target)
}

// As finds the first error in the wrapped chain that matches target.
func (e *redactedError) As(target any) bool {
	return errors.As(e.err, target)
}
