package agent

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mrz1836/nilbox/internal/fence"
)

// AccessFence starts a background run under a routine's tools preset (Access.Fence): the
// preset brings its own permission mode and rules, whatever the profile says, and the
// profile's own preset starts from the access a click would (Fence.Base), within the
// fence's hard lines. Only the launcher's resolvePermission chooses it, below the trust
// rule, so an outside contributor's item still runs planning only.
const AccessFence AccessKind = "fence"

// FenceSettingsName is the file in a fenced run's folder that holds the agent's fence
// settings: its rules, its hook, and its sandbox.
const FenceSettingsName = "settings.json"

// reasonFence says why an extra flag can't be added to a background run.
const reasonFence = "is set by the routine's fence"

// reasonLoosens says why an extra flag that would widen a background run can't be added.
const reasonLoosens = "can't be added to a background run: it would loosen the routine's fence"

// ErrPresetNotKept is returned for a tools preset the agent can't keep in a background run.
var ErrPresetNotKept = errors.New("this agent can't keep the tools preset")

// Fence is a background run's access under a tools preset.
type Fence struct {
	// Preset is the routine's tools preset.
	Preset fence.Preset

	// Base is the access a click would start with, which only the profile's own preset
	// starts from; every other preset brings its own mode, so it is zero.
	Base Access

	// Settings is the absolute path of the settings file FenceFiles writes, in the run's
	// own folder, which the runner sets once it has one (Access.InRun).
	Settings string

	// TakenOver reports the run's conversation resumed in the user's terminal (Take over):
	// someone answers now, so each fixed preset asks about whatever its rules don't allow
	// rather than refusing it, and its rules, its hook, and its sandbox still hold. The
	// profile's own preset starts from the click's access either way.
	TakenOver bool
}

// TakenOver returns the access of a fenced run's conversation resumed in the user's
// terminal: the same fence, asking rather than refusing (Fence.TakenOver). Any other access
// is returned as it is.
func (a Access) TakenOver() Access {
	if a.Kind != AccessFence || a.Fence == nil {
		return a
	}

	f := *a.Fence
	f.TakenOver = true
	a.Fence = &f

	return a
}

// FenceAccess returns the access of a background run under a preset; base is the click's
// access, which only the profile's own preset keeps.
func FenceAccess(preset fence.Preset, base Access) Access {
	f := &Fence{Preset: preset}
	if preset == fence.Profile {
		f.Base = base
	}

	return Access{Kind: AccessFence, Fence: f}
}

// InRun returns the access with its fence's settings in the run's folder dir. Any other
// access is returned as it is.
func (a Access) InRun(dir string) Access {
	if a.Kind != AccessFence || a.Fence == nil {
		return a
	}

	f := *a.Fence
	f.Settings = filepath.Join(dir, FenceSettingsName)
	a.Fence = &f

	return a
}

// fenceKeeper is an agent spec that can keep the tools presets in a background run.
type fenceKeeper interface {
	// fenceFiles returns the agent's own files for a fenced run, beside the policy.
	fenceFiles(f *Fence, built fence.Fence, program string) ([]fence.File, error)

	// backgroundReserved lists the flags no extra argument of a background run sets: the
	// fence's own and those that would loosen it.
	backgroundReserved() []reservedFlag
}

// KeepsPreset reports whether the agent can keep a tools preset in a background run, and
// when it can't, why, as the builder says it.
func KeepsPreset(id ID, p fence.Preset) (bool, string) {
	s, err := specFor(id)
	if err != nil {
		return false, "This agent isn't one this build knows."
	}

	if !p.Valid() {
		return false, fmt.Sprintf("%q isn't a tools preset.", p)
	}

	if _, ok := s.(fenceKeeper); !ok {
		return false, s.info().Name + " can't keep a tools preset in a background run yet."
	}

	return true, ""
}

// validateFence checks an AccessFence for the agent with id.
func validateFence(id ID, f *Fence) error {
	if err := fenceShape(f); err != nil {
		return err
	}

	if ok, why := KeepsPreset(id, f.Preset); !ok {
		return fmt.Errorf("%w: %s", ErrPresetNotKept, why)
	}

	return validateAccess(id, f.Base)
}

// fenceShape checks a fence apart from its agent: its preset, its settings' path, and
// the base only the profile's own preset starts from.
func fenceShape(f *Fence) error {
	switch {
	case f == nil:
		return fmt.Errorf("%w: a fenced run with no fence", ErrInvalidAccess)
	case !f.Preset.Valid():
		return fmt.Errorf("%w %q", fence.ErrPreset, f.Preset)
	case f.Settings == "" || !filepath.IsAbs(f.Settings) || filepath.Clean(f.Settings) != f.Settings:
		return fmt.Errorf("%w: a fenced run's settings must be an absolute path in its folder", ErrInvalidAccess)
	case f.Base.Kind == AccessFence:
		return fmt.Errorf("%w: a fence can't start from another", ErrInvalidAccess)
	case f.Preset != fence.Profile && (f.Base.Kind != AccessDefault || f.Base.Mode != ""):
		return fmt.Errorf("%w: only the profile's own preset starts from the profile's access", ErrInvalidAccess)
	default:
		return nil
	}
}

// FenceFiles returns the files the agent's fence needs in the run's folder, beside the
// policy fence.WriteRun writes: for Claude Code, its settings, with the fence hook (the
// app's own executable, program, in its hook mode), the rules, attribution off, and for a
// sandboxed preset, the sandbox. The access must be AccessFence in the run's folder, and
// built the fence worked out for its preset.
func FenceFiles(id ID, a Access, built fence.Fence, program string) ([]fence.File, error) {
	if a.Kind != AccessFence {
		return nil, fmt.Errorf("%w: only a fenced run has fence files", ErrInvalidAccess)
	}

	if err := validateAccess(id, a); err != nil {
		return nil, err
	}

	if built.Policy.Preset != a.Fence.Preset {
		return nil, fmt.Errorf("%w: the fence was built for another preset", ErrInvalidAccess)
	}

	if !filepath.IsAbs(program) {
		return nil, fmt.Errorf("%w: the fence hook's program must be an absolute path", ErrInvalidAccess)
	}

	s, err := specFor(id)
	if err != nil {
		return nil, err
	}

	keeper, ok := s.(fenceKeeper)
	if !ok {
		return nil, ErrPresetNotKept
	}

	return keeper.fenceFiles(a.Fence, built, program)
}

// FencedRun is what a fenced background run needs beside its argv, whose access flags
// Args writes from the same AccessFence: its fence, the files its folder holds, and the
// line its unattended note gains.
type FencedRun struct {
	Fence fence.Fence
	Files []fence.File
	Note  string
}

// PrepareFence works a fenced run out for the agent with id: the fence its spec builds
// (fence.Build), under the access's preset and in the folder its settings are in, and the
// agent's own files for it (FenceFiles), whose hook is the app's own executable, program.
// The runner calls it as the run starts, under the worktree's lock, then Write.
func PrepareFence(id ID, a Access, s fence.Spec, program string) (FencedRun, error) {
	if a.Kind != AccessFence || a.Fence == nil {
		return FencedRun{}, fmt.Errorf("%w: only a fenced run has a fence", ErrInvalidAccess)
	}

	if s.Preset != a.Fence.Preset {
		return FencedRun{}, fmt.Errorf("%w: the run's spec names another preset than its access", ErrInvalidAccess)
	}

	if filepath.Join(s.RunDir, FenceSettingsName) != a.Fence.Settings {
		return FencedRun{}, fmt.Errorf("%w: the run's folder isn't the one its settings are in", ErrInvalidAccess)
	}

	built, err := fence.Build(s)
	if err != nil {
		return FencedRun{}, err
	}

	files, err := FenceFiles(id, a, built, program)
	if err != nil {
		return FencedRun{}, err
	}

	return FencedRun{Fence: built, Files: files, Note: built.AgentNote()}, nil
}

// Write writes the run's files into its folder (fence.WriteRun).
func (r FencedRun) Write(dir string) error {
	return fence.WriteRun(dir, r.Fence, r.Files...)
}

// CheckFenced checks the extra arguments of a background run, as CheckExtra checks every
// launch's, and also refuses each flag the routine's fence sets or that would loosen it:
// the tools and settings flags, extra directories, plugins and MCP servers, and the
// headless run's own (see ReservedFlagError).
func CheckFenced(id ID, args []string) error {
	if err := CheckExtra(id, args); err != nil {
		return err
	}

	s, err := specFor(id)
	if err != nil {
		return err
	}

	keeper, ok := s.(fenceKeeper)
	if !ok {
		return ErrPresetNotKept
	}

	list := keeper.backgroundReserved()

	for _, arg := range args {
		if flag, name, found := reservedIn(list, s.switches(), arg); found {
			return &ReservedFlagError{Flag: name, Reason: flag.reason, Access: flag.access}
		}
	}

	return nil
}

// reservedIn returns the flag of list an argument sets, as reserved does for a spec's own.
func reservedIn(list []reservedFlag, switches []string, arg string) (flag reservedFlag, name string, ok bool) {
	for _, flag = range list {
		if setsFlag(arg, flag.long) {
			return flag, flag.long, true
		}
	}

	for _, short := range shortOptions(arg, switches) {
		if i := slices.IndexFunc(list, func(f reservedFlag) bool { return f.short != "" && f.short == short }); i >= 0 {
			return list[i], short, true
		}
	}

	return reservedFlag{}, "", false
}
