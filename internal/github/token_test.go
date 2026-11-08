package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// fakeToken is a placeholder that matches no real token format.
const fakeToken = "test-token-not-real"

// fakeGHPath is where the fake finder says gh lives.
const fakeGHPath = "/opt/fake/bin/gh"

var (
	errFakeExit     = errors.New("exit status 1")
	errFakeNotFound = errors.New("gh: executable file not found")
)

func TestTokenSourceErrors(t *testing.T) {
	found := testutil.Finder{Path: fakeGHPath}

	cases := []struct {
		name    string
		finder  testutil.Finder
		runner  *testutil.Runner
		wantErr error
		want    string
	}{
		{name: "gh missing", finder: testutil.Finder{Err: errFakeNotFound}, runner: &testutil.Runner{}, wantErr: ErrGHNotFound},
		{
			name:    "not logged in",
			finder:  found,
			runner:  &testutil.Runner{Stderr: "no oauth token found for github.com", Err: errFakeExit},
			wantErr: ErrNotLoggedIn,
		},
		{name: "empty output", finder: found, runner: &testutil.Runner{Stdout: "\n"}, wantErr: ErrNotLoggedIn},
		{name: "output is not a token", finder: found, runner: &testutil.Runner{Stdout: "not a token"}, wantErr: ErrNotLoggedIn},
		{name: "gh vanished after lookup", finder: found, runner: &testutil.Runner{Err: exec.ErrNotFound}, wantErr: ErrGHNotFound},
		{name: "success", finder: found, runner: &testutil.Runner{Stdout: fakeToken + "\n"}, want: fakeToken},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := NewTokenSource(tc.runner, tc.finder).Token(t.Context())

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.True(t, tok.IsZero())

				// The user is told what to do, in a sentence, while the error keeps the
				// sentinel's text for the logs.
				want := map[error]string{ErrGHNotFound: ghMissingMessage, ErrNotLoggedIn: loggedOutMessage}[tc.wantErr]
				assert.Equal(t, want, uierr.Message(err))
				assert.Contains(t, err.Error(), tc.wantErr.Error())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, tok.Reveal())
			require.Len(t, tc.runner.Calls(), 1)
			assert.Equal(t, []string{fakeGHPath, "auth", "token", "--hostname", "github.com"}, tc.runner.Calls()[0])
		})
	}
}

func TestTokenSourceHints(t *testing.T) {
	assert.Contains(t, ErrGHNotFound.Error(), "https://cli.github.com")
	assert.Contains(t, ErrGHNotFound.Error(), "gh auth login")
	assert.Contains(t, ErrNotLoggedIn.Error(), "gh auth login")
}

func TestTokenSourceCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := NewTokenSource(&testutil.Runner{Err: errFakeExit}, testutil.Finder{Path: fakeGHPath}).Token(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTokenZeroValue(t *testing.T) {
	var tok Token

	assert.True(t, tok.IsZero())
	assert.Empty(t, tok.Reveal())
	assert.False(t, NewToken(fakeToken).IsZero())
}

// holder has a Token in both an exported and an unexported field.
type holder struct {
	Tok Token `json:"Tok"`
	tok Token
}

// formatted renders tok every way fmt can.
func formatted(tok Token) []string {
	verbs := []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%10s", "%-12v", "%T %v"}
	h := holder{Tok: tok, tok: tok}

	out := make([]string, 0, len(verbs)*5+4)

	for _, verb := range verbs {
		out = append(out,
			fmt.Sprintf(verb, tok),
			fmt.Sprintf(verb, &tok),
			fmt.Sprintf(verb, h),
			fmt.Sprintf(verb, []Token{tok}),
			fmt.Sprintf(verb, map[string]Token{"k": tok}),
		)
	}

	return append(out, fmt.Sprint(tok), fmt.Sprintln(tok, h), tok.String(), tok.GoString())
}

func TestTokenNeverAppearsInErrorsOrLogs(t *testing.T) {
	tok := NewToken(fakeToken)

	t.Run("fmt", func(t *testing.T) {
		for _, s := range formatted(tok) {
			assert.NotContains(t, s, fakeToken)
		}
	})

	t.Run("json", func(t *testing.T) {
		b, err := json.Marshal(holder{Tok: tok, tok: tok})
		require.NoError(t, err)
		assert.JSONEq(t, `{"Tok":"[redacted]"}`, string(b))

		b, err = tok.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, redactedText, string(b))
	})

	t.Run("slog", func(t *testing.T) {
		var buf bytes.Buffer

		for _, handler := range []slog.Handler{slog.NewJSONHandler(&buf, nil), slog.NewTextHandler(&buf, nil)} {
			logger := slog.New(handler)
			logger.Info("borrowed", "token", tok, "holder", holder{Tok: tok, tok: tok}, slog.Any("ptr", &tok))
		}

		assert.Contains(t, buf.String(), redactedText)
		assert.NotContains(t, buf.String(), fakeToken)
	})

	t.Run("failing gh run", func(t *testing.T) {
		runner := &testutil.Runner{Stdout: fakeToken, Stderr: "error: " + fakeToken, Err: errFakeExit}

		_, err := NewTokenSource(runner, testutil.Finder{Path: fakeGHPath}).Token(t.Context())
		require.Error(t, err)
		assert.NotContains(t, err.Error(), fakeToken)
	})

	t.Run("redact", func(t *testing.T) {
		inner := fmt.Errorf("%w: server said %s", ErrUnexpectedStatus, fakeToken)
		err := Redact(inner, tok)

		assert.NotContains(t, err.Error(), fakeToken)
		assert.Contains(t, err.Error(), redactedText)
		require.ErrorIs(t, err, ErrUnexpectedStatus)
		assert.NoError(t, errors.Unwrap(err), "the unredacted message is not reachable by unwrapping")
	})

	t.Run("api error echoing the token", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Bad credentials: Bearer ` + fakeToken + `"}`))
		})

		_, err := client.FetchInbox(t.Context(), tok)
		require.ErrorIs(t, err, ErrUnexpectedStatus)
		assert.NotContains(t, err.Error(), fakeToken)

		_, err = client.FetchRateLimits(t.Context(), tok)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), fakeToken)
	})

	t.Run("graphql error echoing the token", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(w, `{"errors":[{"message":"bad token %s"}]}`, fakeToken)
		})

		_, err := client.FetchInbox(t.Context(), tok)
		require.ErrorIs(t, err, ErrGraphQL)
		assert.NotContains(t, err.Error(), fakeToken)
	})
}

func TestRedactPassThrough(t *testing.T) {
	require.NoError(t, Redact(nil, NewToken(fakeToken)))

	err := fmt.Errorf("wrapped: %w", errFakeExit)
	assert.Same(t, err, Redact(err, Token{}), "an empty token leaves the error alone")
}

func TestRedactKeepsErrorsAs(t *testing.T) {
	err := Redact(fmt.Errorf("check: %w", &MissingScopesError{Missing: []string{scopeRepo}}), NewToken(fakeToken))

	var missing *MissingScopesError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, []string{scopeRepo}, missing.Missing)
}
