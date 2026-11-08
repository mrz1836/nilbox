// Package github talks to GitHub on the user's behalf.
//
// The token is borrowed from the GitHub CLI (gh auth token) and kept in memory only. A
// TokenSource hands the same token to every caller for up to ten minutes, so a refresh
// does not wait for gh each time, and callers that ask at once share one gh run. It asks
// gh again sooner when:
//
//   - GitHub refuses the token (HTTP 401, Bad credentials): WithToken forgets it and
//     retries the request once with a fresh one, so a token revoked or replaced by
//     gh auth login is picked up at once;
//   - gh's account changes: gh rewrites its hosts file on gh auth switch, login, and
//     logout, and a token borrowed before that is dropped;
//   - the app quits: Clear forgets the token.
//
// The token never reaches a log line, an error message, or the database: the Token type
// prints as [redacted] in every format, and every error that leaves this package is
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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// redactedText replaces the token wherever it would otherwise be printed.
const redactedText = "[redacted]"

// ghHost is the GitHub host the token is borrowed for.
const ghHost = "github.com"

// TokenTTL is how long a TokenSource keeps handing out the token it borrowed before it
// asks gh again.
const TokenTTL = 10 * time.Minute

// tokenKey names the one gh run callers share.
const tokenKey = "token"

// What the user is told when the GitHub CLI cannot lend a token.
const (
	ghMissingMessage = "The GitHub CLI (gh) was not found. Install it from https://cli.github.com, " +
		"then run gh auth login."
	loggedOutMessage = "The GitHub CLI is not signed in to github.com. Run gh auth login, then try again."
)

var (
	// ErrGHNotFound is returned when the GitHub CLI is not installed or not on the search path.
	ErrGHNotFound = errors.New("the GitHub CLI (gh) was not found: install the GitHub CLI from https://cli.github.com, then run gh auth login")

	// ErrNotLoggedIn is returned when the GitHub CLI has no token for github.com.
	ErrNotLoggedIn = errors.New("the GitHub CLI is not logged in to github.com: run gh auth login")
)

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

// TokenSource borrows the GitHub CLI's token for github.com and keeps it in memory for
// TokenTTL. It is safe for concurrent use.
type TokenSource struct {
	runner    execpath.Runner
	finder    execpath.Finder
	clock     clock.Clock
	hostsFile string

	// borrows runs gh once for every caller that asks while it runs.
	borrows flight.Group[Token]

	mu      sync.Mutex
	held    Token
	expires time.Time
	hosts   hostsStamp
}

// TokenOption configures a TokenSource.
type TokenOption func(*TokenSource)

// WithTokenClock sets the clock that ages the token. Without it the system clock does.
func WithTokenClock(c clock.Clock) TokenOption {
	return func(s *TokenSource) { s.clock = clock.Or(c) }
}

// WithHostsFile sets the GitHub CLI's hosts file (see GHHostsFile), which gh rewrites when
// its account changes; a token borrowed before a change is not handed out again. Without
// it no file is watched, and only the TTL, Forget, and Clear drop the token.
func WithHostsFile(path string) TokenOption {
	return func(s *TokenSource) { s.hostsFile = path }
}

// NewTokenSource returns a TokenSource that finds gh with finder and runs it with runner.
func NewTokenSource(runner execpath.Runner, finder execpath.Finder, opts ...TokenOption) *TokenSource {
	s := &TokenSource{runner: runner, finder: finder, clock: clock.System{}}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// GHHostsFile returns where the GitHub CLI records its signed-in accounts, as gh finds it:
// hosts.yml in GH_CONFIG_DIR when that is set, else in XDG_CONFIG_HOME/gh, else in
// home/.config/gh, which gh uses on macOS too.
func GHHostsFile(home string) string {
	const hosts = "hosts.yml"

	if dir := os.Getenv("GH_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, hosts)
	}

	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "gh", hosts)
	}

	return filepath.Join(home, ".config", "gh", hosts)
}

// Token returns the token borrowed last while it is younger than TokenTTL and gh's
// account has not changed since; otherwise it runs gh auth token, once for every caller
// asking meanwhile, and keeps what gh prints. It returns ErrGHNotFound when gh is missing
// and ErrNotLoggedIn when gh has no token, each with a message for the user (see
// uierr.Message). The command's output is never included in an error.
func (s *TokenSource) Token(ctx context.Context) (Token, error) {
	if tok, ok := s.cached(); ok {
		return tok, nil
	}

	return s.borrows.Do(ctx, tokenKey, s.borrow)
}

// Forget drops tok if it is the token held, so the next Token asks gh again. It leaves a
// token borrowed since in place, so callers refused with the same old token do not throw
// away the fresh one.
func (s *TokenSource) Forget(tok Token) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !tok.IsZero() && tok.Reveal() == s.held.Reveal() {
		s.dropLocked()
	}
}

// Clear drops the token held, whatever it is, as the app does when it quits.
func (s *TokenSource) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dropLocked()
}

// cached returns the token held while it may still be handed out, and drops it otherwise.
func (s *TokenSource) cached() (Token, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.held.IsZero() {
		return Token{}, false
	}

	if !s.clock.Now().Before(s.expires) || s.stampHosts() != s.hosts {
		s.dropLocked()

		return Token{}, false
	}

	return s.held, true
}

// dropLocked forgets the token held. The caller holds mu.
func (s *TokenSource) dropLocked() {
	s.held, s.expires, s.hosts = Token{}, time.Time{}, hostsStamp{}
}

// borrow runs gh auth token and holds what it prints. The hosts file is read first, so an
// account change while gh runs drops the token at the next Token.
func (s *TokenSource) borrow(ctx context.Context) (Token, error) {
	hosts := s.stampHosts()

	tok, err := s.run(ctx)
	if err != nil {
		return Token{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.held, s.expires, s.hosts = tok, s.clock.Now().Add(TokenTTL), hosts

	return tok, nil
}

// run runs gh auth token and returns the token it prints.
func (s *TokenSource) run(ctx context.Context) (Token, error) {
	path, err := s.finder.Find("gh")
	if err != nil {
		return Token{}, uierr.New(ghMissingMessage, fmt.Errorf("%w: %w", ErrGHNotFound, err))
	}

	stdout, _, err := s.runner.Run(ctx, path, "auth", "token", "--hostname", ghHost)
	if err != nil {
		return Token{}, tokenRunError(ctx, err)
	}

	value := strings.TrimSpace(string(stdout))
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return Token{}, errLoggedOut()
	}

	return NewToken(value), nil
}

// hostsStamp is the state of gh's hosts file: its size and modification time, or none
// when it is missing or not watched.
type hostsStamp struct {
	exists  bool
	size    int64
	modTime int64
}

// stampHosts reads the hosts file's state.
func (s *TokenSource) stampHosts() hostsStamp {
	if s.hostsFile == "" {
		return hostsStamp{}
	}

	info, err := os.Stat(s.hostsFile)
	if err != nil {
		return hostsStamp{}
	}

	return hostsStamp{exists: true, size: info.Size(), modTime: info.ModTime().UnixNano()}
}

// Tokens lends a token and takes back one GitHub refused. *TokenSource implements it.
type Tokens interface {
	Token(ctx context.Context) (Token, error)
	Forget(tok Token)
}

// WithToken runs call with a token from tokens. When GitHub refuses that token as bad
// credentials, which it does once the token is revoked or replaced, WithToken forgets it
// and runs call once more with a fresh one.
func WithToken[T any](ctx context.Context, tokens Tokens, call func(Token) (T, error)) (T, error) {
	tok, err := tokens.Token(ctx)
	if err != nil {
		var zero T

		return zero, err
	}

	got, err := call(tok)
	if !IsBadCredentials(err) {
		return got, err
	}

	tokens.Forget(tok)

	if tok, err = tokens.Token(ctx); err != nil {
		var zero T

		return zero, err
	}

	return call(tok)
}

// IsBadCredentials reports whether err is GitHub refusing the token itself: HTTP 401, or
// a Bad credentials message.
func IsBadCredentials(err error) bool {
	var status *StatusError

	return errors.As(err, &status) &&
		(status.StatusCode == http.StatusUnauthorized || strings.EqualFold(status.Message, "Bad credentials"))
}

// tokenRunError maps a failed gh run to an actionable error without its output.
func tokenRunError(ctx context.Context, err error) error {
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("borrow the GitHub CLI token: %w", ctx.Err())
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return uierr.New(ghMissingMessage, ErrGHNotFound)
	default:
		return errLoggedOut()
	}
}

// errLoggedOut is ErrNotLoggedIn with a message for the user.
func errLoggedOut() error {
	return uierr.New(loggedOutMessage, ErrNotLoggedIn)
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
