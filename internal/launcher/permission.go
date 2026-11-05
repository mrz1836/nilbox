package launcher

import (
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/profiles"
)

// Permission summaries reported back to the UI after a launch.
const (
	summaryOutsidePlan = "plan mode (outside contributor)"
	summaryPlan        = "plan mode"
	summaryPlanFull    = "plan mode, full access available after the plan"
	summaryFull        = "full access"
	summaryDefault     = "the agent's default permissions"
	summaryModePrefix  = "permission mode "
)

// permission is the outcome of resolvePermission: the flags to pass and a short
// human-readable summary of what they mean.
type permission struct {
	args    []string
	summary string
}

// permissionModes returns the values the agent accepts for --permission-mode.
func permissionModes() []string {
	return []string{"acceptEdits", "auto", "bypassPermissions", "manual", "dontAsk", modePlan}
}

// resolvePermission decides which permission flags a launch starts with. It is the only
// place permission flags are chosen.
//
// An item is effectively trusted when it is trusted or the user ticked the
// outside-contributor override. The override only lifts the outside-contributor
// restriction: the profile's own plan and permission settings still apply, and it does
// nothing for an item that is already trusted.
//
//   - not effectively trusted: --permission-mode plan, and nothing else
//   - the profile's plan mode is on: --permission-mode plan, plus
//     --allow-dangerously-skip-permissions when the profile has full access
//   - the profile has full access: --dangerously-skip-permissions
//   - the profile sets a mode: --permission-mode <mode>
//   - otherwise: no flag, so the agent's defaults apply
//
// --dangerously-skip-permissions is never combined with --permission-mode. An unknown
// permission mode on the profile is an error, whatever the item's trust.
func resolvePermission(trusted, override bool, p profiles.Profile) (permission, error) {
	if err := validatePermissionMode(p.PermissionMode); err != nil {
		return permission{}, err
	}

	if !trusted && !override {
		return permission{args: []string{flagPermissionMode, modePlan}, summary: summaryOutsidePlan}, nil
	}

	full := p.PermissionMode == profiles.PermissionFull

	switch {
	case p.PlanMode && full:
		return permission{
			args:    []string{flagPermissionMode, modePlan, flagAllowSkipPermissions},
			summary: summaryPlanFull,
		}, nil
	case p.PlanMode:
		return permission{args: []string{flagPermissionMode, modePlan}, summary: summaryPlan}, nil
	case full:
		return permission{args: []string{flagSkipPermissions}, summary: summaryFull}, nil
	case p.PermissionMode != "":
		return permission{
			args:    []string{flagPermissionMode, p.PermissionMode},
			summary: summaryModePrefix + p.PermissionMode,
		}, nil
	default:
		return permission{summary: summaryDefault}, nil
	}
}

// validatePermissionMode accepts "", "full", or one of the agent's permission modes.
func validatePermissionMode(mode string) error {
	if mode == "" || mode == profiles.PermissionFull || slices.Contains(permissionModes(), mode) {
		return nil
	}

	return fmt.Errorf("%w %q", ErrInvalidPermissionMode, mode)
}
