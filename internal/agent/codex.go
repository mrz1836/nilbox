package agent

import "strings"

// OpenAI Codex's flags. Every flag a launch passes it is spelled here.
const (
	codexModel          = "--model"
	codexConfig         = "--config"
	codexSandbox        = "--sandbox"
	codexApproval       = "--ask-for-approval"
	codexBypass         = "--dangerously-bypass-approvals-and-sandbox"
	codexAddDirPrefix   = "--add-dir="
	codexEffortKey      = "model_reasoning_effort"
	codexServiceTierKey = "service_tier"
	codexFastTier       = "fast"
	codexReadOnly       = "read-only"
	codexApproveOnAsk   = "on-request"
	codexProjectsPrefix = "projects."

	// codexResume is the command that resumes a session: `codex resume [flags] -- <id>`.
	codexResume = "resume"
)

// codex is OpenAI Codex. Its prompt is a positional argument that comes after every flag,
// behind "--", so a prompt can never be read as a flag or a subcommand.
type codex struct{}

func (codex) info() Agent {
	return Agent{
		ID:        Codex,
		Name:      "Codex",
		Command:   "codex",
		ConfigEnv: "CODEX_HOME",
		ConfigDir: ".codex",
		// The sign-in, the settings, and the session history.
		Markers:      []Marker{{Name: "auth.json"}, {Name: "config.toml"}, {Name: "sessions", Dir: true}},
		EffortLevels: effortChoices("low", "medium", "high", "xhigh", "max", "ultra"),
		Models:       []Choice{},
		PermissionModes: []Choice{
			{Value: codexReadOnly, Label: "Read-only"},
			{Value: "workspace-write", Label: "Writes in the workspace"},
			{Value: "danger-full-access", Label: "Full access, asks first"},
		},
		PlanLabel:       "Read-only",
		SessionNames:    false,
		FastMode:        true,
		ConfigOverrides: true,
		AuthFile:        "auth.json",
		AuthEnv:         []string{"OPENAI_API_KEY", "CODEX_API_KEY"},
		ModelsCache:     "models_cache.json",
	}
}

func (codex) reserved() []reservedFlag {
	return append(commonReserved(),
		reservedFlag{long: codexModel, short: "-m", reason: reasonModel},
		reservedFlag{long: codexSandbox, short: "-s", reason: reasonPermissions, access: true},
		reservedFlag{long: codexApproval, short: "-a", reason: reasonPermissions, access: true},
		reservedFlag{long: codexBypass, reason: reasonPermissions, access: true},
		reservedFlag{long: "--dangerously-bypass-hook-trust", reason: reasonPermissions, access: true},
		reservedFlag{long: "--approve-for-me", reason: reasonPermissions, access: true},
		reservedFlag{long: "--full-auto", reason: reasonPermissions, access: true},
		reservedFlag{long: "--yolo", reason: reasonPermissions, access: true},
		reservedFlag{long: "--cd", short: "-C", reason: "can't be added: sessions start in the item's folder"},
		reservedFlag{long: "--remote", reason: reasonTerminal},
	)
}

// reservedConfig refuses a -c override of a value the launch sets itself: the model, the
// effort, the sandbox and approvals, and any project's trust.
func (codex) reservedConfig(key string) string {
	switch {
	case key == "model":
		return reasonModel
	case key == codexEffortKey:
		return reasonEffort
	case key == codexServiceTierKey:
		return reasonFast
	case key == "sandbox_mode", key == "approval_policy", key == "sandbox_permissions",
		key == "sandbox_workspace_write" || strings.HasPrefix(key, "sandbox_workspace_write."):
		return reasonPermissions
	case key == "projects" || strings.HasPrefix(key, codexProjectsPrefix):
		return "can't be added: " + reasonTrust
	default:
		return ""
	}
}

func (codex) configFlags() []string { return []string{"-c", codexConfig} }

// commands lists Codex's commands and their aliases, as of codex-cli 0.163, with some
// earlier versions had. Any of them before the prompt would run that command, which reads
// none of the session's flags, its sandbox included.
func (codex) commands() []string {
	return []string{
		"a", "agents", "app", "app-server", "apply", "archive", "cloud", "cloud-tasks", "completion", "debug",
		"delete", "doctor", "e", "exec", "exec-server", "features", "fork", "generate-ts", "help", "login", "logout",
		"mcp", "mcp-server", "migrate-rollouts", "plugin", "proto", "queue", "remote-control", "responses-api-proxy",
		"resume", "review", "sandbox", "unarchive", "update",
	}
}

// args returns Codex's flags. Codex takes no session ID of its own, so SessionID is
// ignored; a resumed session runs the resume command, with every flag after it, and the
// session's ID last, after "--", where the prompt would go.
func (codex) args(o Options) []string {
	var args []string

	if o.ResumeID != "" {
		args = append(args, codexResume)
	}

	if o.Model != "" {
		args = append(args, codexModel, o.Model)
	}

	args = append(args, codexOverrides(o)...)
	args = append(args, codexAccess(o.Access)...)

	for _, dir := range o.AddDirs {
		args = append(args, codexAddDirPrefix+dir)
	}

	args = append(append(args, o.Extra...), endOfFlags)

	if o.ResumeID != "" {
		args = append(args, o.ResumeID)
	}

	return args
}

// codexOverrides returns the config overrides a launch passes: the effort level and Fast
// mode, which Codex has no flags for. Each value is read as TOML, so it is quoted as a
// TOML string.
func codexOverrides(o Options) []string {
	var args []string

	if o.Effort != "" {
		args = append(args, codexConfig, codexEffortKey+`="`+o.Effort+`"`)
	}

	if o.Fast {
		args = append(args, codexConfig, codexServiceTierKey+`="`+codexFastTier+`"`)
	}

	return args
}

// codexAccess returns the flags of a launch's permissions.
func codexAccess(a Access) []string {
	switch a.Kind {
	case AccessFull:
		return []string{codexBypass}
	case AccessMode:
		return []string{codexSandbox, a.Mode}
	case AccessPlan, AccessPlanAllowFull:
		// Codex has no plan mode: a read-only sandbox that asks before anything else is the
		// nearest, and it cannot switch to full access later.
		return []string{codexSandbox, codexReadOnly, codexApproval, codexApproveOnAsk}
	case AccessDefault:
		return nil
	default:
		return nil
	}
}

func (codex) promptLast() bool { return true }

func (codex) modelHint() string {
	return `A model is one word, such as "gpt-6-sol", that doesn't start with "-".`
}
