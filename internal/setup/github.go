package setup

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// GitHubState is where the GitHub CLI's sign-in stands, as the GitHub step shows it.
type GitHubState string

// Where the sign-in stands.
const (
	// GitHubReady is a sign-in whose token reads everything the inbox needs, or reports no
	// scopes to check (a fine-grained token).
	GitHubReady GitHubState = "ready"

	// GitHubNoCLI is the GitHub CLI not installed, or not where the app looks.
	GitHubNoCLI GitHubState = "no_cli"

	// GitHubSignedOut is the GitHub CLI installed but not signed in to github.com.
	GitHubSignedOut GitHubState = "signed_out"

	// GitHubMissingScopes is a token without the repo or read:org scope.
	GitHubMissingScopes GitHubState = "missing_scopes"

	// GitHubRefused is a token GitHub refused: revoked or expired.
	GitHubRefused GitHubState = "refused"

	// GitHubUnreachable is GitHub not answering, or answering in a way the app can't read.
	GitHubUnreachable GitHubState = "unreachable"
)

// ProjectAccess says whether the token reads GitHub Projects, which the inbox shows when it
// can.
type ProjectAccess string

// What the token says of GitHub Projects.
const (
	// ProjectsGranted is a token with read:project, or project, which includes it.
	ProjectsGranted ProjectAccess = "granted"

	// ProjectsMissing is a token that reports its scopes, neither of them among them.
	ProjectsMissing ProjectAccess = "missing"

	// ProjectsUnverified is a token that reports no scopes, or one the step didn't read.
	ProjectsUnverified ProjectAccess = "unverified"
)

// The commands the GitHub step offers, one for each state but ready and unreachable.
const (
	// FixInstall installs the GitHub CLI with Homebrew.
	FixInstall = "brew install gh"

	// FixSignIn signs the GitHub CLI in to github.com.
	FixSignIn = "gh auth login"

	// FixScopes grants the scopes the inbox needs.
	FixScopes = "gh auth refresh -s repo,read:org"
)

// GitHub is the GitHub step's check of the sign-in the app borrows from the GitHub CLI.
// No slice it holds is nil.
type GitHub struct {
	// State is where the sign-in stands.
	State GitHubState `json:"state"`

	// Login and Name are the account's login and display name, for a token GitHub took.
	Login string `json:"login"`
	Name  string `json:"name"`

	// Scopes are the scopes the token reports, in its order; empty for one that reports
	// none.
	Scopes []string `json:"scopes"`

	// Missing are the scopes the inbox needs that the token lacks, for GitHubMissingScopes.
	Missing []string `json:"missing"`

	// Unverified reports a token that reports no scopes, such as a fine-grained one, whose
	// access can't be checked: the inbox may miss some items.
	Unverified bool `json:"unverified"`

	// Projects says whether the token reads GitHub Projects.
	Projects ProjectAccess `json:"projects"`

	// CLIVersion is the GitHub CLI's version, or "" when it isn't installed or its version
	// can't be read.
	CLIVersion string `json:"cliVersion"`

	// Fix is the command that fixes the state, run in a terminal, or "" for none.
	Fix string `json:"fix"`

	// Message says what is wrong, in words the window shows, or "" when ready.
	Message string `json:"message"`
}

// GitHub checks the sign-in the app borrows from the GitHub CLI, with one GraphQL point
// when gh lends a token: who it belongs to and whether its scopes give the inbox what it
// needs. A problem is a state with a command that fixes it, never an error; only a check
// the window gave up on is.
func (s *Service) GitHub(ctx context.Context) (GitHub, error) {
	var (
		in  github.SignIn
		err error
	)

	if s.account == nil {
		err = github.ErrGHNotFound
	} else {
		in, err = s.account.SignIn(ctx)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return GitHub{}, ctxErr
	}

	out := classify(in, err)

	if s.cli != nil {
		if version, installed := s.cli.GHVersion(ctx); installed {
			out.CLIVersion = version
		}
	}

	if out.State == GitHubUnreachable {
		s.logger.InfoContext(ctx, "the setup could not check the GitHub sign-in", "error", err)
	}

	return out, nil
}

// classify works out the GitHub step's state from the sign-in read, or the error reading
// it returned.
func classify(in github.SignIn, err error) GitHub {
	out := GitHub{Scopes: []string{}, Missing: []string{}, Projects: ProjectsUnverified}

	switch {
	case err == nil:
		return signedIn(out, in)
	case errors.Is(err, github.ErrGHNotFound):
		out.State, out.Fix = GitHubNoCLI, FixInstall
	case errors.Is(err, github.ErrNotLoggedIn):
		out.State, out.Fix = GitHubSignedOut, FixSignIn
	case github.IsBadCredentials(err):
		out.State, out.Fix = GitHubRefused, FixSignIn
		out.Message = "GitHub refused the GitHub CLI's token: it was revoked or has expired. Sign in again."

		return out
	default:
		out.State = GitHubUnreachable
	}

	out.Message = uierr.Message(err)

	return out
}

// signedIn is the state of a sign-in GitHub took: ready, or missing the scopes the inbox
// needs.
func signedIn(out GitHub, in github.SignIn) GitHub {
	out.Login, out.Name = in.Login, in.Name
	out.Scopes = scopeList(in.Scopes)

	switch github.ProjectsScope(in.Scopes) {
	case github.ProjectScopeGranted:
		out.Projects = ProjectsGranted
	case github.ProjectScopeMissing:
		out.Projects = ProjectsMissing
	case github.ProjectScopeUnverified:
		out.Projects = ProjectsUnverified
	}

	warning, err := github.CheckScopes(in.Scopes)
	if missing, ok := errors.AsType[*github.MissingScopesError](err); ok {
		out.State, out.Fix = GitHubMissingScopes, FixScopes
		out.Missing = slices.Clone(missing.Missing)
		out.Message = "The GitHub CLI's token is missing the " + strings.Join(missing.Missing, " and ") +
			" scope, which the inbox needs to see private and organization repositories."

		return out
	}

	out.State = GitHubReady
	out.Unverified = warning != ""

	return out
}

// scopeList reads an X-OAuth-Scopes header into its scopes, in order, each once.
func scopeList(header string) []string {
	out := []string{}

	for scope := range strings.SplitSeq(header, ",") {
		if scope = strings.TrimSpace(scope); scope != "" && !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}

	return out
}
