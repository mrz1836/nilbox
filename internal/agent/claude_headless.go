package agent

import "strconv"

// Claude Code's flags for a background run, `claude -p`, as S1 found them on version
// 2.1.325. The fence's own flags (the preset's mode and tools, the settings and their
// sources, and no MCP server) are claude_fence.go's; these are the headless run's.
const (
	claudePrint                = "-p"
	claudeOutputFormat         = "--output-format"
	claudeStreamJSON           = "stream-json"
	claudeVerbose              = "--verbose"
	claudePermissionPrompts    = "--permission-prompts"
	claudeNoPrompts            = "none"
	claudeMaxBudget            = "--max-budget-usd"
	claudeJSONSchema           = "--json-schema"
	claudeAppendSystemPrompt   = "--append-system-prompt"
	claudeSystemPromptSnapshot = "--system-prompt-snapshot"
	claudeOff                  = "off"
)

// headless: Claude Code runs headless with `-p`, keeps to its own --max-budget-usd (so a
// run may outlive the app's quit), reports its cost, usage, and a structured report, and
// can be resumed. Its floor is the fence's (claudeFenceFloor), the version S1 set for
// every flag a background run passes.
func (claude) headless() Headless {
	return Headless{
		Background: true, Budget: true, Cost: true, Usage: true, Structured: true, Resume: true,
		MinVersion: claudeFenceFloor,
	}
}

// headlessArgs returns a background run's flags: the stream, nobody to answer, the
// session, the model and effort, the fence's access (claudeFenceArgs), the budget, the
// report's schema, the unattended note, and the extra arguments. --system-prompt-snapshot
// off keeps the note out of the conversation a later resume replays. The prompt comes on
// stdin.
func (claude) headlessArgs(o HeadlessOptions) []string {
	args := []string{
		claudePrint, claudeOutputFormat, claudeStreamJSON, claudeVerbose, claudePermissionPrompts, claudeNoPrompts,
		claudeSessionIDPrefix + o.SessionID,
	}

	if o.Model != "" {
		args = append(args, claudeModel, o.Model)
	}

	if o.Effort != "" {
		args = append(args, claudeEffort, o.Effort)
	}

	args = append(args, claudeAccess(o.Access)...)

	if o.BudgetUSD > 0 {
		args = append(args, claudeMaxBudget, strconv.FormatFloat(o.BudgetUSD, 'f', 2, 64))
	}

	if o.Schema != "" {
		args = append(args, claudeJSONSchema, o.Schema)
	}

	if o.SystemNote != "" {
		args = append(args, claudeAppendSystemPrompt, o.SystemNote)
	}

	args = append(args, claudeSystemPromptSnapshot, claudeOff)

	return append(args, o.Extra...)
}

// dropEnv names Claude Code's own variables, which a session it runs inside sets for its
// children: a background run started from such a shell would otherwise take them on.
func (claude) dropEnv() []string {
	return []string{"CLAUDECODE", "CLAUDE_CODE_*", "CLAUDE_EFFORT", "CLAUDE_PID", "CLAUDE_TMPDIR"}
}
