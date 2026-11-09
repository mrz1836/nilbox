package launcher

import (
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/profiles"
)

// Permission summaries reported back to the UI after a launch. A summary that names the
// agent's planning mode uses its PlanLabel, as in "plan mode" or "read-only".
const (
	summaryOutside    = " (outside contributor)"
	summaryPlanFull   = ", full access available after the plan"
	summaryFull       = "full access"
	summaryDefault    = "the agent's default permissions"
	summaryModePrefix = "permission mode "
)

// permission is the outcome of resolvePermission: the access to start with and a short
// human-readable summary of what it means.
type permission struct {
	access  agent.Access
	summary string
}

// resolvePermission decides what access a launch starts with. It is the only place a
// launch's permissions are chosen; the agent spec turns them into its own flags.
//
// An item is effectively trusted when it is trusted or the user ticked the
// outside-contributor override. The override only lifts the outside-contributor
// restriction: the profile's own plan and permission settings still apply, and it does
// nothing for an item that is already trusted.
//
//   - not effectively trusted: planning only (Claude's plan mode, Codex's read-only
//     sandbox), and nothing else
//   - the profile's plan mode is on: planning only, and for Claude, with full access
//     available after the plan when the profile has full access
//   - the profile has full access: full access
//   - the profile sets a mode: that mode
//   - otherwise: no flag, so the agent's defaults apply
//
// Full access is never combined with a mode. An unknown permission mode on the profile
// is an error, whatever the item's trust.
func resolvePermission(a agent.Agent, trusted, override bool, p profiles.Profile) (permission, error) {
	if err := validatePermissionMode(a, p.PermissionMode); err != nil {
		return permission{}, err
	}

	plan := strings.ToLower(a.PlanLabel)

	if !trusted && !override {
		return permission{access: agent.Access{Kind: agent.AccessPlan}, summary: plan + summaryOutside}, nil
	}

	full := p.PermissionMode == profiles.PermissionFull

	switch {
	case p.PlanMode && full && a.ID == agent.Claude:
		return permission{access: agent.Access{Kind: agent.AccessPlanAllowFull}, summary: plan + summaryPlanFull}, nil
	case p.PlanMode:
		return permission{access: agent.Access{Kind: agent.AccessPlan}, summary: plan}, nil
	case full:
		return permission{access: agent.Access{Kind: agent.AccessFull}, summary: summaryFull}, nil
	case p.PermissionMode != "":
		return permission{
			access:  agent.Access{Kind: agent.AccessMode, Mode: p.PermissionMode},
			summary: summaryModePrefix + p.PermissionMode,
		}, nil
	default:
		return permission{summary: summaryDefault}, nil
	}
}

// validatePermissionMode accepts "", "full", or one of the agent's permission modes.
func validatePermissionMode(a agent.Agent, mode string) error {
	if mode == "" || mode == profiles.PermissionFull || agent.ValidPermissionMode(a.ID, mode) {
		return nil
	}

	return fmt.Errorf("%w %q for %s", ErrInvalidPermissionMode, mode, a.Name)
}
