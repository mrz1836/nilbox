package agent

// Claude Code's flags. Every flag a launch passes it is spelled here.
const (
	claudeModel           = "--model"
	claudeEffort          = "--effort"
	claudeSkipPermissions = "--dangerously-skip-permissions"
	claudeAllowSkip       = "--allow-dangerously-skip-permissions"
	claudePermissionMode  = "--permission-mode"
	claudeAddDirPrefix    = "--add-dir="
	claudeNamePrefix      = "--name="

	// claudePlanMode is the --permission-mode value that starts a session in plan mode.
	claudePlanMode = "plan"
)

// claude is Claude Code. Its prompt comes first, since some of its flags (such as
// --add-dir) take several values and would otherwise swallow it.
type claude struct{}

func (claude) info() Agent {
	return Agent{
		ID:        Claude,
		Name:      "Claude",
		Command:   "claude",
		ConfigEnv: "CLAUDE_CONFIG_DIR",
		ConfigDir: ".claude",
		// The agent's state file, its settings, and its per-project session history. It
		// writes the first on its first run with the directory.
		Markers:      []Marker{{Name: ".claude.json"}, {Name: "settings.json"}, {Name: "projects", Dir: true}},
		EffortLevels: effortChoices("low", "medium", "high", "xhigh", "max"),
		Models: []Choice{
			{Value: "fable", Label: "Fable"},
			{Value: "opus", Label: "Opus"},
			{Value: "sonnet", Label: "Sonnet"},
			{Value: "haiku", Label: "Haiku"},
		},
		PermissionModes: []Choice{
			{Value: "acceptEdits", Label: "Accepts edits"},
			{Value: "auto", Label: "Auto"},
			{Value: "bypassPermissions", Label: "Full access"},
			{Value: "manual", Label: "Asks first"},
			{Value: "dontAsk", Label: "Doesn't ask"},
			{Value: claudePlanMode, Label: "Plans only"},
		},
		PlanLabel:    "Plan mode",
		SessionNames: true,
		AuthEnv:      []string{},
	}
}

func (claude) reserved() []reservedFlag {
	return append(commonReserved(),
		reservedFlag{long: claudeModel, reason: reasonModel},
		reservedFlag{long: claudeEffort, reason: reasonEffort},
		reservedFlag{long: claudePermissionMode, reason: reasonPermissions, access: true},
		reservedFlag{long: claudeSkipPermissions, reason: reasonPermissions, access: true},
		reservedFlag{long: claudeAllowSkip, reason: reasonPermissions, access: true},
		reservedFlag{long: "--name", short: "-n", reason: "can't be added: each session is named for you"},
		reservedFlag{long: "--print", short: "-p", reason: reasonTerminal},
		reservedFlag{long: "--background", reason: reasonTerminal},
		reservedFlag{long: "--bg", reason: reasonTerminal},
	)
}

func (claude) reservedConfig(string) string { return "" }

func (claude) configFlags() []string { return nil }

// commands lists none: Claude Code reads its prompt first, so no later word is read as a
// command.
func (claude) commands() []string { return nil }

func (claude) args(o Options) []string {
	var args []string

	if o.Model != "" {
		args = append(args, claudeModel, o.Model)
	}

	if o.Effort != "" {
		args = append(args, claudeEffort, o.Effort)
	}

	switch o.Access.Kind {
	case AccessFull:
		args = append(args, claudeSkipPermissions)
	case AccessMode:
		args = append(args, claudePermissionMode, o.Access.Mode)
	case AccessPlan:
		args = append(args, claudePermissionMode, claudePlanMode)
	case AccessPlanAllowFull:
		args = append(args, claudePermissionMode, claudePlanMode, claudeAllowSkip)
	case AccessDefault:
	}

	// The "=" form keeps --add-dir from taking the arguments after it as more directories.
	for _, dir := range o.AddDirs {
		args = append(args, claudeAddDirPrefix+dir)
	}

	args = append(args, o.Extra...)

	if o.SessionName != "" {
		args = append(args, claudeNamePrefix+o.SessionName)
	}

	return args
}

func (claude) promptLast() bool { return false }

func (claude) modelHint() string {
	return `A model is one word, such as "opus" or "sonnet", that doesn't start with "-".`
}
