// Package launcher starts a coding-agent session on a pull request or issue.
//
// A launch is built in layers so each OS-specific piece stays small:
//
//  1. BuildPlan turns an item, a profile, and a local clone into an OS-independent Plan:
//     the working directory, the agent binary and its arguments, and the environment.
//  2. A terminal.ScriptRenderer writes that plan as a script (POSIX sh today). The prompt
//     is kept in its own file and read by the script at run time, so prompt text never
//     appears on a command line and needs no shell quoting.
//  3. A terminal.Driver opens the script in the user's terminal app.
//
// Everything up to the terminal driver is pure and tested on every OS. The launcher
// never runs a state-changing git command in the clone: worktree launches use the
// agent's own --worktree flag, and the agent creates and owns the worktree.
package launcher

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/profiles"
)

// Agent command-line flags and environment names. Every flag the launcher passes is
// spelled here.
const (
	flagModel                = "--model"
	flagEffort               = "--effort"
	flagSkipPermissions      = "--dangerously-skip-permissions"
	flagAllowSkipPermissions = "--allow-dangerously-skip-permissions"
	flagPermissionMode       = "--permission-mode"
	flagAddDirPrefix         = "--add-dir="
	flagWorktree             = "--worktree"
	flagNamePrefix           = "--name="

	// modePlan is the --permission-mode value that starts a session in plan mode.
	modePlan = "plan"

	// envConfigDir points the agent at a profile's config directory. It is unset for
	// the default profile so the agent's own default login is used.
	envConfigDir = "CLAUDE_CONFIG_DIR"
)

// worktreePRHeadForm starts the --worktree argument of a pull request launched against a
// clone whose origin is the item's repository: "#<n>", which the agent names "pr-<n>"
// and bases on the pull request's head.
const worktreePRHeadForm = "#"

var (
	// ErrInvalidNumber is returned for an item number that is not positive.
	ErrInvalidNumber = errors.New("item number must be positive")

	// ErrRelativePath is returned when a path that must be absolute is not.
	ErrRelativePath = errors.New("path must be absolute")

	// ErrInvalidPermissionMode is returned for a profile permission mode the agent does
	// not accept.
	ErrInvalidPermissionMode = errors.New("unknown permission mode")

	// ErrInvalidEnvKey is returned for a profile environment variable with an unusable name.
	ErrInvalidEnvKey = errors.New("invalid environment variable name")

	// ErrEmptyArg is returned for an empty add-dir entry.
	ErrEmptyArg = errors.New("empty argument")

	// ErrNULByte is returned when a path, argument, or environment value contains a NUL
	// byte, which no script or process argument can carry.
	ErrNULByte = errors.New("value contains a NUL byte")

	// ErrConflictingExtraArg is returned when a profile's extra arguments try to set a
	// permission switch. Permissions come only from the launcher's own policy, so an
	// outside contributor's item can never be launched with more access than it allows.
	ErrConflictingExtraArg = errors.New("extra argument conflicts with the launch permission policy")
)

// permissionFlags returns the switches only resolvePermission may set.
func permissionFlags() []string {
	return []string{flagPermissionMode, flagSkipPermissions, flagAllowSkipPermissions}
}

// Item is the pull request or issue a session is launched on.
type Item struct {
	// Kind is the kind of item.
	Kind item.Kind

	// Number is the pull request or issue number.
	Number int

	// URL is the item's web address.
	URL string

	// Title is the item's title, as written by its author.
	Title string

	// Repo is the item's repository as "owner/name".
	Repo string

	// Branch is a pull request's head branch, or "" for an issue.
	Branch string

	// Trusted reports whether the item's author is trusted (see the trust package).
	Trusted bool
}

// Clone is the local working tree a session starts in.
type Clone struct {
	// Dir is the clone's absolute directory.
	Dir string

	// OriginIsItemRepo reports whether the clone's origin remote is the item's
	// repository. Only then can the agent fetch a pull request's head from origin.
	OriginIsItemRepo bool
}

// Request is everything BuildPlan needs to plan one launch.
type Request struct {
	// Item is the pull request or issue to launch on.
	Item Item

	// Profile supplies the model, effort, permissions, extra directories, extra
	// arguments, and environment.
	Profile profiles.Profile

	// Clone is the working tree to start in.
	Clone Clone

	// Worktree starts the session in an agent-created worktree.
	Worktree bool

	// FullAccessOverride lifts the outside-contributor restriction for this launch. The
	// profile's own plan and permission settings still apply.
	FullAccessOverride bool

	// ClaudePath is the agent binary's absolute path.
	ClaudePath string

	// PromptFile is the prompt file's absolute path, or "" when WriteLaunchFiles will
	// choose it.
	PromptFile string

	// SessionName names the session in the agent's prompt box, its resume list, and the
	// terminal's title, or "" to leave it unnamed.
	SessionName string
}

// Plan is an OS-independent description of one launch. The prompt is passed as the
// first argument after Binary, read from PromptFile when the script runs.
type Plan struct {
	// Kind and Number identify the item, for naming the launch files.
	Kind   item.Kind
	Number int

	// Dir is the directory the session starts in.
	Dir string

	// Binary is the agent binary's absolute path.
	Binary string

	// Args are the arguments that follow the prompt, one argv element each.
	Args []string

	// Env holds the environment variables to set.
	Env map[string]string

	// Unset lists environment variables to remove.
	Unset []string

	// PromptFile holds the prompt text the script passes as the first argument.
	PromptFile string

	// Worktree is the --worktree argument, or "" when the session starts in the clone.
	Worktree string

	// WorktreeOnPRHead reports whether the agent bases the worktree on the pull
	// request's head, so the session does not need to check the branch out.
	WorktreeOnPRHead bool

	// Permission is a short summary of the session's permissions, for the UI.
	Permission string
}

// Argv returns the full command line: the binary, the prompt, then Args. The prompt
// comes first because some agent flags (such as --add-dir) take several values and
// would otherwise swallow it.
func (p Plan) Argv(prompt string) []string {
	argv := make([]string, 0, len(p.Args)+2)
	argv = append(argv, p.Binary, prompt)

	return append(argv, p.Args...)
}

// BuildPlan plans a launch. It is pure: it reads no files and runs nothing.
//
// The arguments are, in order: --model, --effort, the permission flags chosen by the
// trust policy, one --add-dir=<dir> per extra directory, the profile's extra arguments
// verbatim, --name=<session name>, and --worktree <name> last. Extra arguments that try
// to set a permission switch are rejected with ErrConflictingExtraArg, and no plan is
// returned.
func BuildPlan(req Request) (Plan, error) {
	if err := validateRequest(req); err != nil {
		return Plan{}, err
	}

	perm, err := resolvePermission(req.Item.Trusted, req.FullAccessOverride, req.Profile)
	if err != nil {
		return Plan{}, err
	}

	args, err := profileArgs(req.Profile, perm.args)
	if err != nil {
		return Plan{}, err
	}

	env, unset, err := planEnv(req.Profile)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		Kind:       req.Item.Kind,
		Number:     req.Item.Number,
		Dir:        req.Clone.Dir,
		Binary:     req.ClaudePath,
		Env:        env,
		Unset:      unset,
		PromptFile: req.PromptFile,
		Permission: perm.summary,
	}

	if req.SessionName != "" {
		args = append(args, flagNamePrefix+req.SessionName)
	}

	if req.Worktree {
		plan.Worktree = worktreeArg(req.Item, req.Clone.OriginIsItemRepo)
		plan.WorktreeOnPRHead = strings.HasPrefix(plan.Worktree, worktreePRHeadForm)
		args = append(args, flagWorktree, plan.Worktree)
	}

	plan.Args = args

	if err = checkNUL(plan); err != nil {
		return Plan{}, err
	}

	return plan, nil
}

// worktreeArg returns the --worktree argument for an item. The same item always gets the
// same worktree, so launching again resumes it:
//
//   - a pull request whose repository is the clone's origin: "#<n>" (the agent names
//     the worktree "pr-<n>" and bases it on the pull request's head)
//   - any other item: its item.WorktreeName, "pr-<n>" or "issue-<n>"
func worktreeArg(it Item, originIsItemRepo bool) string {
	if it.Kind == item.KindPR && originIsItemRepo {
		return worktreePRHeadForm + strconv.Itoa(it.Number)
	}

	return item.WorktreeName(it.Kind, it.Number)
}

// script is what the plan's launch script runs, for a terminal.ScriptRenderer.
func (p Plan) script() terminal.Script {
	return terminal.Script{
		Dir: p.Dir, Binary: p.Binary, Args: p.Args, Env: p.Env, Unset: p.Unset, PromptFile: p.PromptFile,
	}
}

// worktreeDir is the name of the worktree directory the agent uses for the plan's
// --worktree argument (the "#<n>" form becomes "pr-<n>"), or "" without a worktree.
func (p Plan) worktreeDir() string {
	if p.Worktree == "" {
		return ""
	}

	return item.WorktreeName(p.Kind, p.Number)
}

// validateRequest checks the request's item and paths.
func validateRequest(req Request) error {
	if !req.Item.Kind.Valid() {
		return fmt.Errorf("%w: %q", item.ErrInvalidKind, req.Item.Kind)
	}

	if req.Item.Number <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidNumber, req.Item.Number)
	}

	paths := map[string]string{"clone directory": req.Clone.Dir, "agent binary": req.ClaudePath}
	if req.PromptFile != "" {
		paths["prompt file"] = req.PromptFile
	}

	for _, label := range slices.Sorted(maps.Keys(paths)) {
		if !filepath.IsAbs(paths[label]) {
			return fmt.Errorf("%w: %s %q", ErrRelativePath, label, paths[label])
		}
	}

	return nil
}

// profileArgs returns the model, effort, permission, add-dir, and extra arguments.
func profileArgs(p profiles.Profile, permArgs []string) ([]string, error) {
	args, err := modelEffortArgs(p)
	if err != nil {
		return nil, err
	}

	args = append(args, permArgs...)

	dirs, err := addDirArgs(p.AddDirs)
	if err != nil {
		return nil, err
	}

	if err = checkExtraArgs(p.ExtraArgs); err != nil {
		return nil, err
	}

	args = append(args, dirs...)

	return append(args, p.ExtraArgs...), nil
}

// modelEffortArgs returns the --model and --effort flags a profile asks for.
func modelEffortArgs(p profiles.Profile) ([]string, error) {
	var args []string

	if err := agent.ValidateModel(p.Model); err != nil {
		return nil, err
	}

	if p.Model != "" {
		args = append(args, flagModel, p.Model)
	}

	if err := agent.ValidateEffort(p.Effort); err != nil {
		return nil, err
	}

	if p.Effort == "" {
		return args, nil
	}

	return append(args, flagEffort, p.Effort), nil
}

// addDirArgs returns one --add-dir=<dir> argument per directory. The "=" form keeps the
// flag from consuming the arguments after it.
func addDirArgs(dirs []string) ([]string, error) {
	args := make([]string, 0, len(dirs))

	for _, dir := range dirs {
		if dir == "" {
			return nil, fmt.Errorf("%w: add-dir entry", ErrEmptyArg)
		}

		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("%w: add-dir %q", ErrRelativePath, dir)
		}

		args = append(args, flagAddDirPrefix+dir)
	}

	return args, nil
}

// checkExtraArgs rejects extra arguments that set a permission switch.
func checkExtraArgs(extra []string) error {
	for _, arg := range extra {
		if conflictsWithPermissions(arg) {
			return fmt.Errorf("%w: %q", ErrConflictingExtraArg, arg)
		}
	}

	return nil
}

// conflictsWithPermissions reports whether arg sets a permission switch, in either the
// "--flag value" or the "--flag=value" form.
func conflictsWithPermissions(arg string) bool {
	for _, flag := range permissionFlags() {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}

	return false
}

// planEnv returns the variables to set and to unset. A profile with a config directory
// sets CLAUDE_CONFIG_DIR to it, overriding any value in the profile's env; the default
// profile unsets CLAUDE_CONFIG_DIR so the agent's own default login is used.
func planEnv(p profiles.Profile) (map[string]string, []string, error) {
	env := make(map[string]string, len(p.Env)+1)
	for key, value := range p.Env {
		if !terminal.ValidEnvName(key) {
			return nil, nil, fmt.Errorf("%w: %q", ErrInvalidEnvKey, key)
		}

		env[key] = value
	}

	if p.ConfigDir == "" {
		delete(env, envConfigDir)

		return env, []string{envConfigDir}, nil
	}

	if !filepath.IsAbs(p.ConfigDir) {
		return nil, nil, fmt.Errorf("%w: profile config directory %q", ErrRelativePath, p.ConfigDir)
	}

	env[envConfigDir] = p.ConfigDir

	return env, nil, nil
}

// checkNUL rejects a plan whose directory, binary, arguments, or environment holds a
// NUL byte.
func checkNUL(p Plan) error {
	values := append([]string{p.Dir, p.Binary, p.PromptFile}, p.Args...)
	for _, v := range p.Env {
		values = append(values, v)
	}

	for _, v := range values {
		if strings.ContainsRune(v, 0) {
			return ErrNULByte
		}
	}

	return nil
}
