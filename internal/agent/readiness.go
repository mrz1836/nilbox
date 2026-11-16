package agent

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// What an agent's own commands say of this Mac, for Autopilot's readiness: its version
// (VersionArgs) and whether a profile is signed in (SignInArgs). Each agent's output is
// read by its own parser here; nothing else reads it. A sign-in keeps only whether the
// profile is signed in, how (a subscription's sign-in through the browser, or a key), the
// subscription's type, and when the sign-in expires: never the account's email, an
// organization, or a token.

// SignInState is what an agent's sign-in command says of a profile.
type SignInState string

// The states.
const (
	// SignedIn is a profile signed in, through the browser on a subscription or else.
	SignedIn SignInState = "signedIn"

	// SignedOut is a profile that isn't.
	SignedOut SignInState = "signedOut"

	// SignInUnknown is output the parser couldn't read: the command may have changed.
	SignInUnknown SignInState = "unknown"
)

// SignIn is a profile's sign-in, as its agent's sign-in command says it.
type SignIn struct {
	State SignInState `json:"state"`

	// Method is how it is signed in, in the agent's own word ("claude.ai", "chatgpt",
	// "apiKey"), "" when it doesn't say.
	Method string `json:"method"`

	// Subscription is the subscription's type ("max", "pro"), "" when it doesn't say.
	Subscription string `json:"subscription"`

	// ExpiresAt is when the sign-in expires, nil when it doesn't say.
	ExpiresAt *time.Time `json:"expiresAt"`
}

// versionPattern finds a version in what an agent's --version prints: "2.1.325 (Claude
// Code)", "codex-cli 0.163.0".
var versionPattern = regexp.MustCompile(`\b(\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?)\b`)

// ParseVersion returns the version an agent's VersionArgs printed, "" when there is none.
func ParseVersion(out []byte) string {
	return string(versionPattern.Find(out))
}

// ParseSignIn reads what the agent's SignInArgs printed, and whether the command exited
// cleanly. An agent this build doesn't know is unknown.
func ParseSignIn(id ID, out []byte, exitOK bool) SignIn {
	switch id {
	case Claude:
		return claudeSignIn(out, exitOK)
	case Codex:
		return codexSignIn(out, exitOK)
	case Inherit:
	}

	return SignIn{State: SignInUnknown}
}

// claudeStatus is the part of `claude auth status` (JSON by default) a sign-in keeps;
// whatever else it prints, the email and the organization among it, is never read into
// anything.
type claudeStatus struct {
	LoggedIn         *bool  `json:"loggedIn"`
	AuthMethod       string `json:"authMethod"`
	SubscriptionType string `json:"subscriptionType"`
	LoginExpiresAt   string `json:"loginExpiresAt"`
	ExpiresAt        string `json:"expiresAt"`
}

// claudeSignIn reads `claude auth status`: its JSON's loggedIn, else, as an older build's
// text, a line that says it is logged in, else a failed exit as signed out.
func claudeSignIn(out []byte, exitOK bool) SignIn {
	var st claudeStatus

	if start := bytes.IndexByte(out, '{'); start >= 0 && json.Unmarshal(out[start:], &st) == nil && st.LoggedIn != nil {
		if !*st.LoggedIn {
			return SignIn{State: SignedOut}
		}

		in := SignIn{State: SignedIn, Method: oneWord(st.AuthMethod), Subscription: oneWord(st.SubscriptionType)}
		in.ExpiresAt = expiry(st.LoginExpiresAt, st.ExpiresAt)

		return in
	}

	return textSignIn(out, exitOK, "")
}

// codexSignIn reads `codex login status`: "Logged in using ChatGPT", "Logged in using an
// API key - …", or "Not logged in", which it exits 1 with. What follows "using" is kept
// only as a word, and a key's text never is.
func codexSignIn(out []byte, exitOK bool) SignIn {
	text := strings.ToLower(string(out))

	method := ""

	switch {
	case strings.Contains(text, "chatgpt"):
		method = "chatgpt"
	case strings.Contains(text, "api key"):
		method = "apiKey"
	}

	return textSignIn(out, exitOK, method)
}

// textSignIn reads a sign-in command's text: a line saying it isn't logged in, one saying
// it is, else a failed exit as signed out and a clean one as unknown.
func textSignIn(out []byte, exitOK bool, method string) SignIn {
	text := strings.ToLower(string(out))

	switch {
	case strings.Contains(text, "not logged in"), strings.Contains(text, "not signed in"),
		strings.Contains(text, "logged out"):
		return SignIn{State: SignedOut}
	case strings.Contains(text, "logged in"), strings.Contains(text, "signed in"):
		return SignIn{State: SignedIn, Method: method}
	case !exitOK:
		return SignIn{State: SignedOut}
	default:
		return SignIn{State: SignInUnknown}
	}
}

// expiry reads the first of times that is a time, nil when none is.
func expiry(times ...string) *time.Time {
	for _, s := range times {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(s)); err == nil {
			return new(t.UTC())
		}
	}

	return nil
}

// oneWord keeps a short word the agent printed, "" for anything longer or with spaces.
func oneWord(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 40 || strings.ContainsAny(s, " \t\r\n@") {
		return ""
	}

	return s
}
