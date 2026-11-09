package agent

import (
	"errors"
	"fmt"
	"slices"
)

// AccessKind is what a session may do without asking, as the launcher's trust policy
// decides it. Each agent turns it into its own flags.
type AccessKind string

// Access kinds.
const (
	// AccessDefault passes no permission flag, so the agent's own settings apply.
	AccessDefault AccessKind = ""

	// AccessMode starts the session in one of the agent's permission modes (Access.Mode).
	AccessMode AccessKind = "mode"

	// AccessFull starts the session with full access: no prompts and no sandbox.
	AccessFull AccessKind = "full"

	// AccessPlan starts the session planning only: it reads, and changes nothing.
	AccessPlan AccessKind = "plan"

	// AccessPlanAllowFull starts the session planning, and lets it switch to full access
	// once the plan is accepted, where the agent can.
	AccessPlanAllowFull AccessKind = "plan-allow-full"
)

// Access is a launch's permissions: a kind, and for AccessMode, the mode.
type Access struct {
	Kind AccessKind
	Mode string
}

// ErrInvalidPermissionMode is returned for a permission mode the agent does not accept.
var ErrInvalidPermissionMode = errors.New("unknown permission mode")

// ErrInvalidAccess is returned for an access kind this build does not know.
var ErrInvalidAccess = errors.New("unknown access")

// Options are what a launch passes an agent, besides its prompt. The launcher has
// checked the directories and the extra arguments.
type Options struct {
	// Model and Effort are "" for the agent's own default.
	Model  string
	Effort string

	// Access is the session's permissions.
	Access Access

	// AddDirs are extra directories the session may use, as absolute paths.
	AddDirs []string

	// Extra are extra arguments, one argv element each, passed as they are.
	Extra []string

	// SessionName names the session, or is "". An agent without SessionNames ignores it.
	SessionName string
}

// Args returns the flags a launch passes the agent with id, in order: model, effort,
// access, extra directories, extra arguments, and session name. It refuses a model or
// effort level the agent would not accept, and a permission mode it does not have.
func Args(id ID, o Options) ([]string, error) {
	s, err := specFor(id)
	if err != nil {
		return nil, err
	}

	if err = ValidateModel(o.Model); err != nil {
		return nil, err
	}

	if err = ValidateEffort(id, o.Effort); err != nil {
		return nil, err
	}

	if err = validateAccess(id, o.Access); err != nil {
		return nil, err
	}

	return s.args(o), nil
}

// ValidPermissionMode reports whether mode is one of the agent's permission modes.
func ValidPermissionMode(id ID, mode string) bool {
	s, err := specFor(id)

	return err == nil && slices.Contains(values(s.info().PermissionModes), mode)
}

// validateAccess checks an Access for the agent with id.
func validateAccess(id ID, a Access) error {
	switch a.Kind {
	case AccessDefault, AccessFull, AccessPlan, AccessPlanAllowFull:
		return nil
	case AccessMode:
		if ValidPermissionMode(id, a.Mode) {
			return nil
		}

		return fmt.Errorf("%w %q", ErrInvalidPermissionMode, a.Mode)
	default:
		return fmt.Errorf("%w %q", ErrInvalidAccess, a.Kind)
	}
}
