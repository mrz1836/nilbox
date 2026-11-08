package inbox

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/github"
)

// ProjectAccess says whether the inbox can read the GitHub projects an issue is in, as
// the last successful refresh found.
type ProjectAccess string

const (
	// ProjectAccessUnknown is before the first refresh.
	ProjectAccessUnknown ProjectAccess = ""

	// ProjectAccessOn is a token that reads projects: each issue's card shows the
	// projects it is in, with its status in each.
	ProjectAccessOn ProjectAccess = "on"

	// ProjectAccessNeedsScope is a token without the read:project scope, so no projects
	// are read. gh auth refresh -s read:project grants it, and the next refresh reads
	// them.
	ProjectAccessNeedsScope ProjectAccess = "needs_scope"
)

// decodeProjectAccess reads a stored ProjectAccess; anything else is unknown.
func decodeProjectAccess(stored string) ProjectAccess {
	switch access := ProjectAccess(stored); access {
	case ProjectAccessOn, ProjectAccessNeedsScope, ProjectAccessUnknown:
		return access
	default:
		return ProjectAccessUnknown
	}
}

// projectAccess returns what the last refresh found about reading projects.
func (s *Service) projectAccess(ctx context.Context) (ProjectAccess, error) {
	stored, _, err := s.prefs.Get(ctx, prefProjects)
	if err != nil {
		return ProjectAccessUnknown, fmt.Errorf("read whether projects can be read: %w", err)
	}

	return decodeProjectAccess(stored), nil
}

// readOptions are the details options for a request sent before the token's scopes are
// known: the projects are asked for when the last refresh read them.
func readOptions(last ProjectAccess) github.DetailOptions {
	return github.DetailOptions{Projects: last == ProjectAccessOn}
}

// readsProjects decides whether a refresh reads projects, once the token's scopes are
// known. A token that does not report its scopes is asked, unless GitHub refused it the
// last time.
func readsProjects(scope github.ProjectScope, last ProjectAccess) bool {
	switch scope {
	case github.ProjectScopeGranted:
		return true
	case github.ProjectScopeMissing:
		return false
	case github.ProjectScopeUnverified:
		return last != ProjectAccessNeedsScope
	default:
		return false
	}
}

// accessOf is what a refresh found: projects read when it asked for them and GitHub did
// not refuse them.
func accessOf(read github.DetailOptions, refused bool) ProjectAccess {
	if read.Projects && !refused {
		return ProjectAccessOn
	}

	return ProjectAccessNeedsScope
}
