package launcher

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/fence"
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
	summaryFenced     = "fenced: "
)

// ErrFenceNotKept is returned for a background run under a tools preset its agent can't
// keep, or with no run folder for the fence's settings.
var ErrFenceNotKept = errors.New("the routine's fence can't be kept")

// Fence is a background run's fence, as its request carries it: the routine's tools
// preset, and the run's own folder, where its settings and policy go.
type Fence struct {
	Preset fence.Preset
	RunDir string
}

// permission is the outcome of resolvePermission: the access to start with and a short
// human-readable summary of what it means.
type permission struct {
	access  agent.Access
	summary string
}

// resolvePermission decides what access a launch starts with. It is the only place a
// launch's permissions are chosen, a background run's fence included; the agent spec
// turns them into its own flags.
//
// An item is effectively trusted when it is trusted or the user ticked the
// outside-contributor override. The override only lifts the outside-contributor
// restriction: the profile's own plan and permission settings still apply, and it does
// nothing for an item that is already trusted.
//
// A click's launch (no fence):
//
//   - not effectively trusted: planning only (Claude's plan mode, Codex's read-only
//     sandbox), and nothing else
//   - the profile's plan mode is on: planning only, and for Claude, with full access
//     available after the plan when the profile has full access
//   - the profile has full access: full access
//   - the profile sets a mode: that mode
//   - otherwise: no flag, so the agent's defaults apply
//
// A background run (a fence):
//
//   - not effectively trusted: the Read only preset, whatever the fence says
//   - the profile's own preset: the access a click would start with, fenced
//   - any other preset: its own mode and rules; the profile's mode, plan mode, and full
//     access don't apply, so a profile with full access can't loosen it, and one with no
//     mode can't break it
//
// Full access is never combined with a mode. An unknown permission mode on the profile
// is an error, whatever the item's trust.
func resolvePermission(a agent.Agent, trusted, override bool, p profiles.Profile, f *Fence) (permission, error) {
	if err := validatePermissionMode(a, p.PermissionMode); err != nil {
		return permission{}, err
	}

	click := clickPermission(a, trusted, override, p)
	if f == nil {
		return click, nil
	}

	preset := f.Preset
	if !trusted && !override {
		preset = fence.ReadOnly
	}

	if ok, why := agent.KeepsPreset(a.ID, preset); !ok {
		return permission{}, fmt.Errorf("%w: %s", ErrFenceNotKept, why)
	}

	if !filepath.IsAbs(f.RunDir) {
		return permission{}, fmt.Errorf("%w: the run has no folder for its settings", ErrFenceNotKept)
	}

	summary := summaryFenced + preset.Label()

	switch {
	case !trusted && !override:
		summary += summaryOutside
	case preset == fence.Profile:
		summary += ", " + click.summary
	}

	return permission{access: agent.FenceAccess(preset, click.access).InRun(f.RunDir), summary: summary}, nil
}

// clickPermission is a click's access: the outside-contributor rule, then the profile's.
func clickPermission(a agent.Agent, trusted, override bool, p profiles.Profile) permission {
	plan := strings.ToLower(a.PlanLabel)

	if !trusted && !override {
		return permission{access: agent.Access{Kind: agent.AccessPlan}, summary: plan + summaryOutside}
	}

	full := p.PermissionMode == profiles.PermissionFull

	switch {
	case p.PlanMode && full && a.ID == agent.Claude:
		return permission{access: agent.Access{Kind: agent.AccessPlanAllowFull}, summary: plan + summaryPlanFull}
	case p.PlanMode:
		return permission{access: agent.Access{Kind: agent.AccessPlan}, summary: plan}
	case full:
		return permission{access: agent.Access{Kind: agent.AccessFull}, summary: summaryFull}
	case p.PermissionMode != "":
		return permission{
			access:  agent.Access{Kind: agent.AccessMode, Mode: p.PermissionMode},
			summary: summaryModePrefix + p.PermissionMode,
		}
	default:
		return permission{summary: summaryDefault}
	}
}

// validatePermissionMode accepts "", "full", or one of the agent's permission modes.
func validatePermissionMode(a agent.Agent, mode string) error {
	if mode == "" || mode == profiles.PermissionFull || agent.ValidPermissionMode(a.ID, mode) {
		return nil
	}

	return fmt.Errorf("%w %q for %s", ErrInvalidPermissionMode, mode, a.Name)
}
