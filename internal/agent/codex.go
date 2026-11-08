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
	codexReadOnly       = "read-only"
	codexApproveOnAsk   = "on-request"
	codexProjectsPrefix = "projects."
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
		PlanLabel:    "Read-only",
		SessionNames: false,
		AuthFile:     "auth.json",
		AuthEnv:      []string{"OPENAI_API_KEY", "CODEX_API_KEY"},
		ModelsCache:  "models_cache.json",
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
	case key == "sandbox_mode", key == "approval_policy":
		return reasonPermissions
	case key == "projects" || strings.HasPrefix(key, codexProjectsPrefix):
		return "can't be added: " + reasonTrust
	default:
		return ""
	}
}

func (codex) configFlags() []string { return []string{"-c", codexConfig} }

func (codex) args(o Options) []string {
	var args []string

	if o.Model != "" {
		args = append(args, codexModel, o.Model)
	}

	if o.Effort != "" {
		// The value is read as TOML, so it is quoted as a TOML string.
		args = append(args, codexConfig, codexEffortKey+`="`+o.Effort+`"`)
	}

	switch o.Access.Kind {
	case AccessFull:
		args = append(args, codexBypass)
	case AccessMode:
		args = append(args, codexSandbox, o.Access.Mode)
	case AccessPlan, AccessPlanAllowFull:
		// Codex has no plan mode: a read-only sandbox that asks before anything else is
		// the nearest, and it cannot switch to full access later.
		args = append(args, codexSandbox, codexReadOnly, codexApproval, codexApproveOnAsk)
	case AccessDefault:
	}

	for _, dir := range o.AddDirs {
		args = append(args, codexAddDirPrefix+dir)
	}

	return append(append(args, o.Extra...), endOfFlags)
}

func (codex) promptLast() bool { return true }

func (codex) modelHint() string {
	return `A model is one word, such as "gpt-6-sol", that doesn't start with "-".`
}
