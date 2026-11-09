package agent

import (
	"errors"
	"fmt"
	"regexp"
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

	// SessionID is the ID the app minted for a new session, or "". An agent without
	// SessionIDs ignores it.
	SessionID string

	// ResumeID is the agent's own ID of a session to resume instead of starting one, or
	// "". A session resumed takes no prompt.
	ResumeID string

	// Fast starts the session in the agent's Fast mode. An agent without FastMode
	// ignores it.
	Fast bool
}

// Args returns the flags a launch passes the agent with id, in order: model, effort,
// access, extra directories, extra arguments, session name, and the session to start or
// resume. It refuses a model or effort level the agent would not accept, a permission
// mode it does not have, and a session ID that is not one.
func Args(id ID, o Options) ([]string, error) {
	s, err := specFor(id)
	if err != nil {
		return nil, err
	}

	if err = ValidateModel(o.Model); err != nil {
		return nil, err
	}

	if err = validateSessionIDs(o); err != nil {
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

// ErrInvalidSessionID is returned for a session ID that is not a UUID, or for a launch
// that both starts a session with an ID and resumes one.
var ErrInvalidSessionID = errors.New("invalid session ID")

// sessionIDPattern is what a session ID looks like: a UUID in its 36-character form,
// with a version (1 to 8) and the standard variant, as Claude Code's sessions (version 4)
// and Codex's threads (version 7) are. Both agents read anything else they are asked to
// resume as a session's name, which could pick another session, and none of it can be
// read as a flag.
var sessionIDPattern = regexp.MustCompile(
	`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[1-8][0-9A-Fa-f]{3}-[89ABab][0-9A-Fa-f]{3}-[0-9A-Fa-f]{12}$`,
)

// ValidSessionID reports whether id can be passed as a session ID: a UUID (see
// sessionIDPattern).
func ValidSessionID(id string) bool {
	return sessionIDPattern.MatchString(id)
}

// validateSessionIDs checks the session a launch starts or resumes.
func validateSessionIDs(o Options) error {
	switch {
	case o.SessionID != "" && o.ResumeID != "":
		return fmt.Errorf("%w: a launch starts a session or resumes one, not both", ErrInvalidSessionID)
	case o.SessionID != "" && !ValidSessionID(o.SessionID):
		return fmt.Errorf("%w %q", ErrInvalidSessionID, o.SessionID)
	case o.ResumeID != "" && !ValidSessionID(o.ResumeID):
		return fmt.Errorf("%w %q", ErrInvalidSessionID, o.ResumeID)
	default:
		return nil
	}
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
