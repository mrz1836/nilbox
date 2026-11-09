// Package launcher starts a coding-agent session (Claude Code or Codex) on a pull
// request or issue.
//
// A launch is built in layers so each OS-specific piece stays small:
//
//  1. Resolve works out the launch settings, the agent among them, from the launch menu's
//     choices, the prompt, the repository's and its owner's launch defaults, the profile,
//     and the app's settings.
//  2. A WorktreeMaker finds or makes the item's worktree (in Go, never in the script), and
//     BuildPlan turns the item, the profile, and the directory into an OS-independent
//     Plan: the agent binary and its arguments, and the environment.
//  3. A terminal.ScriptRenderer writes that plan as a script (POSIX sh today). The prompt
//     is kept in its own file and read by the script at run time, so prompt text never
//     appears on a command line and needs no shell quoting.
//  4. A terminal.Driver opens the script in the user's terminal app.
//
// Everything up to the terminal driver is pure and tested on every OS. The launcher
// itself never changes a clone: the script only changes into the directory and runs the
// agent, and only the worktrees package fetches and adds worktrees.
package launcher

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/profiles"
)

var (
	// ErrInvalidNumber is returned for an item number that is not positive.
	ErrInvalidNumber = errors.New("item number must be positive")

	// ErrRelativePath is returned when a path that must be absolute is not.
	ErrRelativePath = errors.New("path must be absolute")

	// ErrInvalidPermissionMode is returned for a profile permission mode the agent does
	// not accept.
	ErrInvalidPermissionMode = agent.ErrInvalidPermissionMode

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

// Clone is the local working tree a session starts in, or whose worktree it starts in.
type Clone struct {
	// Dir is the clone's absolute directory.
	Dir string

	// Remote is the clone's remote for the item's repository, such as "origin", or
	// "upstream" in a fork's clone.
	Remote string
}

// Request is everything BuildPlan needs to plan one launch.
type Request struct {
	// Item is the pull request or issue to launch on.
	Item Item

	// Agent is the coding agent to start.
	Agent agent.ID

	// Profile supplies the model, effort, permissions, extra directories, extra
	// arguments, and environment.
	Profile profiles.Profile

	// Dir is the absolute directory the session starts in: the item's worktree, or the
	// clone.
	Dir string

	// FullAccessOverride lifts the outside-contributor restriction for this launch. The
	// profile's own plan and permission settings still apply.
	FullAccessOverride bool

	// BinaryPath is the agent binary's absolute path.
	BinaryPath string

	// PromptFile is the prompt file's absolute path, or "" when WriteLaunchFiles will
	// choose it.
	PromptFile string

	// SessionName names the session in the agent's prompt box, its resume list, and the
	// terminal's title, or "" to leave it unnamed. An agent that cannot name sessions
	// ignores it.
	SessionName string

	// SessionID is the ID the app minted for the session, or "". An agent that takes no
	// session ID ignores it.
	SessionID string

	// ResumeID is the agent's own ID of a session to resume, or "" to start one. A
	// session resumed takes no prompt, and keeps the name it was started with.
	ResumeID string

	// Fast starts the session in the agent's Fast mode. An agent without one ignores it.
	Fast bool
}

// Plan is an OS-independent description of one launch. The prompt is passed first, or
// last for an agent whose prompt follows its flags, read from PromptFile when the script
// runs.
type Plan struct {
	// Kind and Number identify the item, for naming the launch files.
	Kind   item.Kind
	Number int

	// Agent is the agent the plan starts.
	Agent agent.ID

	// Dir is the directory the session starts in.
	Dir string

	// Binary is the agent binary's absolute path.
	Binary string

	// Args are the agent's arguments besides the prompt, one argv element each.
	Args []string

	// PromptLast reports that the prompt follows Args (which then end with "--") rather
	// than coming first.
	PromptLast bool

	// NoPrompt reports a session resumed rather than started: the agent is passed Args
	// alone, and no prompt.
	NoPrompt bool

	// Env holds the environment variables to set.
	Env map[string]string

	// Unset lists environment variables to remove.
	Unset []string

	// PromptFile holds the prompt text the script passes to the agent.
	PromptFile string

	// Permission is a short summary of the session's permissions, for the UI.
	Permission string
}

// Argv returns the full command line: the binary, then the prompt and Args, the prompt
// first unless PromptLast. Claude's prompt comes first because some of its flags (such as
// --add-dir) take several values and would otherwise swallow it; Codex's comes last,
// after "--", so it is never read as a flag or a subcommand. A NoPrompt plan passes Args
// alone, and prompt is ignored.
func (p Plan) Argv(prompt string) []string {
	argv := make([]string, 0, len(p.Args)+2)
	argv = append(argv, p.Binary)

	if p.NoPrompt {
		return append(argv, p.Args...)
	}

	if p.PromptLast {
		return append(append(argv, p.Args...), prompt)
	}

	argv = append(argv, prompt)

	return append(argv, p.Args...)
}

// BuildPlan plans a launch. It is pure: it reads no files and runs nothing.
//
// The agent spec writes the arguments, in order: the model, the effort, the permission
// flags the trust policy chose, one --add-dir=<dir> per extra directory, the profile's
// extra arguments verbatim, the session name where the agent takes one, and the session
// to start, where the agent takes its ID, or to resume. Extra
// arguments that try to set a permission switch are rejected with ErrConflictingExtraArg,
// and no plan is returned.
func BuildPlan(req Request) (Plan, error) {
	a, err := validateRequest(req)
	if err != nil {
		return Plan{}, err
	}

	perm, err := resolvePermission(a, req.Item.Trusted, req.FullAccessOverride, req.Profile)
	if err != nil {
		return Plan{}, err
	}

	dirs, err := addDirs(req.Profile.AddDirs)
	if err != nil {
		return Plan{}, err
	}

	if err = agent.CheckAccess(a.ID, req.Profile.ExtraArgs); err != nil {
		return Plan{}, fmt.Errorf("%w: %w", ErrConflictingExtraArg, err)
	}

	args, err := agent.Args(a.ID, agentOptions(a, req, perm, dirs))
	if err != nil {
		return Plan{}, err
	}

	env, unset, err := planEnv(a, req.Profile)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		Kind:       req.Item.Kind,
		Number:     req.Item.Number,
		Agent:      a.ID,
		Dir:        req.Dir,
		Binary:     req.BinaryPath,
		Args:       args,
		PromptLast: agent.PromptLast(a.ID),
		NoPrompt:   req.ResumeID != "",
		Env:        env,
		Unset:      unset,
		PromptFile: req.PromptFile,
		Permission: perm.summary,
	}

	if err = checkNUL(plan); err != nil {
		return Plan{}, err
	}

	return plan, nil
}

// agentOptions are what a launch passes the agent: the profile's choices, the access the
// policy chose, and the session to name, start, or resume, where the agent takes them.
func agentOptions(a agent.Agent, req Request, perm permission, dirs []string) agent.Options {
	opts := agent.Options{
		Model: req.Profile.Model, Effort: req.Profile.Effort, Access: perm.access, AddDirs: dirs,
		Extra: req.Profile.ExtraArgs, ResumeID: req.ResumeID, Fast: req.Fast && a.FastMode,
	}

	if a.SessionNames {
		opts.SessionName = req.SessionName
	}

	if a.SessionIDs && req.ResumeID == "" {
		opts.SessionID = req.SessionID
	}

	return opts
}

// script is what the plan's launch script runs, for a terminal.ScriptRenderer.
func (p Plan) script() terminal.Script {
	return terminal.Script{
		Dir: p.Dir, Binary: p.Binary, Args: p.Args, Env: p.Env, Unset: p.Unset, PromptFile: p.PromptFile,
		PromptLast: p.PromptLast, NoPrompt: p.NoPrompt,
	}
}

// validateRequest checks the request's item, agent, and paths, and returns the agent.
func validateRequest(req Request) (agent.Agent, error) {
	if !req.Item.Kind.Valid() {
		return agent.Agent{}, fmt.Errorf("%w: %q", item.ErrInvalidKind, req.Item.Kind)
	}

	if req.Item.Number <= 0 {
		return agent.Agent{}, fmt.Errorf("%w: %d", ErrInvalidNumber, req.Item.Number)
	}

	a, err := agent.Lookup(req.Agent)
	if err != nil {
		return agent.Agent{}, err
	}

	paths := map[string]string{"session directory": req.Dir, "agent binary": req.BinaryPath}
	if req.PromptFile != "" {
		paths["prompt file"] = req.PromptFile
	}

	for _, label := range slices.Sorted(maps.Keys(paths)) {
		if !filepath.IsAbs(paths[label]) {
			return agent.Agent{}, fmt.Errorf("%w: %s %q", ErrRelativePath, label, paths[label])
		}
	}

	return a, nil
}

// addDirs checks a profile's extra directories: each must be a non-empty absolute path.
func addDirs(dirs []string) ([]string, error) {
	for _, dir := range dirs {
		if dir == "" {
			return nil, fmt.Errorf("%w: add-dir entry", ErrEmptyArg)
		}

		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("%w: add-dir %q", ErrRelativePath, dir)
		}
	}

	return slices.Clone(dirs), nil
}

// planEnv returns the variables to set and to unset. A profile with a config directory
// sets the agent's config variable (CLAUDE_CONFIG_DIR, CODEX_HOME) to it, overriding any
// value in the profile's env; the default profile unsets it so the agent's own default
// login is used.
func planEnv(a agent.Agent, p profiles.Profile) (map[string]string, []string, error) {
	env := make(map[string]string, len(p.Env)+1)
	for key, value := range p.Env {
		if !terminal.ValidEnvName(key) {
			return nil, nil, fmt.Errorf("%w: %q", ErrInvalidEnvKey, key)
		}

		env[key] = value
	}

	if p.ConfigDir == "" {
		delete(env, a.ConfigEnv)

		return env, []string{a.ConfigEnv}, nil
	}

	if !filepath.IsAbs(p.ConfigDir) {
		return nil, nil, fmt.Errorf("%w: profile config directory %q", ErrRelativePath, p.ConfigDir)
	}

	env[a.ConfigEnv] = p.ConfigDir

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
