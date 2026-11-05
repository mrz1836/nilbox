package github

import (
	"errors"
	"strings"
)

// Scopes the inbox needs: repo to see private repositories, and read:org to see
// organization repositories and team review requests.
const (
	scopeRepo    = "repo"
	scopeReadOrg = "read:org"
)

// ErrMissingScopes matches a MissingScopesError with errors.Is.
var ErrMissingScopes = errors.New("the GitHub token is missing required scopes")

// ScopeWarning describes a scope check that passed without proof.
type ScopeWarning string

// ScopesUnverifiable is the warning for a token that reports no scopes, such as a
// fine-grained token. Requests still go ahead; results may be incomplete.
const ScopesUnverifiable ScopeWarning = "GitHub did not report this token's scopes " +
	"(a fine-grained token?), so the repo and read:org scopes can't be verified; " +
	"some items may be missing"

// MissingScopesError lists the required scopes the token lacks.
type MissingScopesError struct {
	Missing []string
}

// Error names the missing scopes and how to grant them.
func (e *MissingScopesError) Error() string {
	return "the GitHub token is missing the " + strings.Join(e.Missing, " and ") +
		" scope: run gh auth refresh -s repo,read:org"
}

// Is reports whether target is ErrMissingScopes.
func (e *MissingScopesError) Is(target error) bool {
	return target == ErrMissingScopes
}

// CheckScopes checks a response's X-OAuth-Scopes header for the scopes the inbox needs.
// An empty header (a token that reports no scopes) returns ScopesUnverifiable and no
// error. A header without repo, or without any of read:org, write:org, or admin:org,
// returns a *MissingScopesError.
func CheckScopes(header string) (ScopeWarning, error) {
	if strings.TrimSpace(header) == "" {
		return ScopesUnverifiable, nil
	}

	granted := make(map[string]bool)

	for _, scope := range strings.Split(header, ",") {
		granted[strings.TrimSpace(scope)] = true
	}

	var missing []string

	if !granted[scopeRepo] {
		missing = append(missing, scopeRepo)
	}

	// Broader organization scopes include read access.
	if !granted[scopeReadOrg] && !granted["write:org"] && !granted["admin:org"] {
		missing = append(missing, scopeReadOrg)
	}

	if len(missing) > 0 {
		return "", &MissingScopesError{Missing: missing}
	}

	return "", nil
}
