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

// Scopes that read GitHub Projects. They are optional: without one the inbox reads no
// projects. The project scope can change projects too, so it includes read access.
const (
	scopeReadProject = "read:project"
	scopeProject     = "project"
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

// ProjectScope is what a token's scopes say about reading GitHub Projects.
type ProjectScope int

const (
	// ProjectScopeUnverified is a token that reports no scopes, such as a fine-grained
	// token: only asking GitHub tells whether it reads projects.
	ProjectScopeUnverified ProjectScope = iota

	// ProjectScopeGranted is a token with the read:project scope, or the project scope,
	// which includes it.
	ProjectScopeGranted

	// ProjectScopeMissing is a token that reports its scopes, neither of them among them.
	ProjectScopeMissing
)

// CheckScopes checks a response's X-OAuth-Scopes header for the scopes the inbox needs.
// An empty header (a token that reports no scopes) returns ScopesUnverifiable and no
// error. A header without repo, or without any of read:org, write:org, or admin:org,
// returns a *MissingScopesError.
func CheckScopes(header string) (ScopeWarning, error) {
	granted, reported := grantedScopes(header)
	if !reported {
		return ScopesUnverifiable, nil
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

// ProjectsScope reads a response's X-OAuth-Scopes header for read access to GitHub
// Projects. An empty header is ProjectScopeUnverified.
func ProjectsScope(header string) ProjectScope {
	granted, reported := grantedScopes(header)

	switch {
	case !reported:
		return ProjectScopeUnverified
	case granted[scopeReadProject] || granted[scopeProject]:
		return ProjectScopeGranted
	default:
		return ProjectScopeMissing
	}
}

// grantedScopes reads an X-OAuth-Scopes header into a set. reported is false for an
// empty header: a token that reports no scopes.
func grantedScopes(header string) (granted map[string]bool, reported bool) {
	if strings.TrimSpace(header) == "" {
		return nil, false
	}

	granted = make(map[string]bool)

	for scope := range strings.SplitSeq(header, ",") {
		granted[strings.TrimSpace(scope)] = true
	}

	return granted, true
}
