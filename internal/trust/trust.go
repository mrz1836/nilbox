// Package trust decides whether a pull request or issue comes from someone trusted with
// the repository. Launches on untrusted items start in plan mode, because their content
// (code, descriptions, comments) was written by someone outside the project.
package trust

import "strings"

// Author associations GitHub reports for an item's author. Only the first three are
// trusted.
const (
	AssociationOwner        = "OWNER"
	AssociationMember       = "MEMBER"
	AssociationCollaborator = "COLLABORATOR"
)

// Trusted reports whether an item's author is trusted: the author association must be
// OWNER, MEMBER, or COLLABORATOR, and a pull request must also come from a branch in the
// same repository, not a fork. Everything else, including bots (NONE) and unknown
// values, is untrusted.
func Trusted(assoc string, isPR, crossRepo bool) bool {
	switch strings.ToUpper(strings.TrimSpace(assoc)) {
	case AssociationOwner, AssociationMember, AssociationCollaborator:
		return !isPR || !crossRepo
	default:
		return false
	}
}
