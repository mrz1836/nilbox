package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEveryAgentNamesItsReadinessCommands(t *testing.T) {
	t.Parallel()

	for _, a := range All() {
		assert.NotEmpty(t, a.VersionArgs, a.ID)
		assert.NotEmpty(t, a.SignInArgs, a.ID)
		assert.NotEmpty(t, a.LoginArgs, a.ID)
		assert.NotEmpty(t, a.KeyEnv, a.ID)
		assert.NotEmpty(t, a.StatusPart, a.ID)
	}
}

func TestParseVersion(t *testing.T) {
	t.Parallel()

	for out, want := range map[string]string{
		"2.1.325 (Claude Code)\n":  "2.1.325",
		"codex-cli 0.163.0\n":      "0.163.0",
		"codex-cli 0.164.0-beta.1": "0.164.0-beta.1",
		"no version here":          "",
		"":                         "",
	} {
		assert.Equal(t, want, ParseVersion([]byte(out)), out)
	}
}

func TestParseSignInClaude(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		out    string
		exitOK bool
		want   SignIn
	}{
		{
			name: "a Max subscription through the browser",
			out: `{"loggedIn": true, "authMethod": "claude.ai", "apiProvider": "firstParty", ` +
				`"email": "someone@example.com", "orgId": "x", "subscriptionType": "max"}`,
			exitOK: true,
			want:   SignIn{State: SignedIn, Method: "claude.ai", Subscription: "max"},
		},
		{
			name:   "an OAuth sign-in that expires",
			out:    `{"loggedIn": true, "authMethod": "oauth_token", "loginExpiresAt": "2026-11-20T10:00:00Z"}`,
			exitOK: true,
			want: SignIn{
				State: SignedIn, Method: "oauth_token",
				ExpiresAt: new(time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)),
			},
		},
		{name: "signed out", out: `{"loggedIn": false}`, exitOK: false, want: SignIn{State: SignedOut}},
		{name: "text, logged in", out: "Logged in.", exitOK: true, want: SignIn{State: SignedIn}},
		{name: "text, not logged in", out: "Not logged in. Run claude auth login", exitOK: false, want: SignIn{State: SignedOut}},
		{name: "nothing, failed", out: "", exitOK: false, want: SignIn{State: SignedOut}},
		{name: "nothing it reads", out: "something new", exitOK: true, want: SignIn{State: SignInUnknown}},
		{
			name: "a long method is dropped, never an email", out: `{"loggedIn": true, "authMethod": "a@b.c"}`,
			exitOK: true, want: SignIn{State: SignedIn},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := ParseSignIn(Claude, []byte(tc.out), tc.exitOK)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseSignInCodex(t *testing.T) {
	t.Parallel()

	got := ParseSignIn(Codex, []byte("Logged in using ChatGPT\n"), true)
	assert.Equal(t, SignIn{State: SignedIn, Method: "chatgpt"}, got)

	got = ParseSignIn(Codex, []byte("Logged in using an API key - sk-***abcd\n"), true)
	assert.Equal(t, SignIn{State: SignedIn, Method: "apiKey"}, got)

	got = ParseSignIn(Codex, []byte("Not logged in\n"), false)
	assert.Equal(t, SignedOut, got.State)

	assert.Equal(t, SignInUnknown, ParseSignIn("other", []byte("Logged in"), true).State)
}

// TestParseSignInCodexRecorded reads what codex-cli 0.163.0's `login status` wrote, to
// stderr alone, recorded and sanitized.
func TestParseSignInCodexRecorded(t *testing.T) {
	t.Parallel()

	in, err := os.ReadFile(filepath.Join("testdata", "codex", "login-status-signed-in.stderr.txt"))
	require.NoError(t, err)

	out, err := os.ReadFile(filepath.Join("testdata", "codex", "login-status-signed-out.stderr.txt"))
	require.NoError(t, err)

	// What core hands the parser: stdout (empty), a newline, then stderr.
	assert.Equal(t, SignIn{State: SignedIn, Method: "chatgpt"}, ParseSignIn(Codex, append([]byte("\n"), in...), true))
	assert.Equal(t, SignedOut, ParseSignIn(Codex, append([]byte("\n"), out...), false).State,
		"it exits 1 when signed out")
}

func TestParseSignInClaudeIgnoresWhatFollowsItsJSON(t *testing.T) {
	t.Parallel()

	got := ParseSignIn(Claude, []byte(`{"loggedIn": true, "subscriptionType": "max"}`+"\n"+"a warning on stderr\n"), true)
	assert.Equal(t, SignIn{State: SignedIn, Subscription: "max"}, got)
}

func TestSignInKeepsNoKeyText(t *testing.T) {
	t.Parallel()

	got := ParseSignIn(Codex, []byte("Logged in using an API key - sk-proj-secret\n"), true)
	require.Equal(t, SignedIn, got.State)
	assert.NotContains(t, got.Method+got.Subscription, "sk-")
}
