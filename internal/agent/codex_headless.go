package agent

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Codex's flags for a background run, `codex exec`, as S6 found them on codex-cli 0.163.0.
// The fence's own flags (the sandbox, the approvals, the writable folders, the network,
// and the hook) are codex_fence.go's; these are the headless run's.
//
//   - The run reads the prompt on stdin ("-"), writes its events as JSON lines (--json),
//     and asks no one (approval_policy "never": exec has no --ask-for-approval). It is
//     never --approve-for-me, which refuses --sandbox and lets a model decide escalations.
//   - --ignore-user-config leaves the profile's config.toml unread: the user's MCP servers,
//     plugins, notify program, and the projects it trusts. Without that trust a branch's
//     own .codex/config.toml (its MCP server commands, its developer instructions) and its
//     .codex/hooks.json never load, which matters all the more under the hook bypass the
//     fence needs, since that bypass runs every enabled hook unreviewed. Checked locally on
//     0.163.0: in a trusted folder a branch's MCP command and its hooks.json hook ran, and
//     with --ignore-user-config neither did. The sign-in still comes from CODEX_HOME.
//   - --ignore-rules leaves every execpolicy .rules file unread, the user's and a branch's.
//   - The structured report is Codex's strict mode (--output-schema, StrictSchema), and
//     the unattended note its developer_instructions, which persist in the thread, so a
//     Take over says first that someone answers now (codexTakeOverNote).
//   - It reports tokens, never a cost, and keeps no budget of its own, so its runs never
//     outlive the app's quit.
const (
	codexExec             = "exec"
	codexJSON             = "--json"
	codexStdin            = "-"
	codexIgnoreUserConfig = "--ignore-user-config"
	codexIgnoreRules      = "--ignore-rules"
	codexOutputSchema     = "--output-schema"
	codexInstructionsKey  = "developer_instructions"

	// codexFloor is the oldest codex-cli whose flags and config keys every background run
	// uses, the version S6 tested and the only one: each flag and key a run passes is in
	// its help or was seen to take effect there (codexFenceReviewed).
	codexFloor = "0.163.0"
)

// ReportSchemaName is the file in a background run's folder that holds the report's
// schema, for an agent that reads it from a file (Codex's --output-schema).
const ReportSchemaName = "report-schema.json"

// codexTakeOverNote is the first message a background run's conversation gets when the
// user takes it over in the terminal: its unattended note persists in the thread, which
// no flag turns off.
const codexTakeOverNote = "You're no longer unattended: I've taken this conversation over in my terminal, so I can " +
	"answer questions and approve what you ask. Carry on from where you stopped."

// headless: Codex runs headless with `codex exec`, reports its tokens and a structured
// report, keeps no budget and reports no cost of its own (so a run never outlives the
// app's quit, and a daily cap in dollars can't count it), and can be resumed.
func (codex) headless() Headless {
	return Headless{
		Background: true, Budget: false, Cost: false, Usage: true, Structured: true, Resume: true, MinVersion: codexFloor,
	}
}

// headlessArgs returns a background run's flags: the exec command and its stream, no
// user config or rules, the model and effort, the fence's access (codexFenceArgs), the
// report's schema, the unattended note, the extra arguments, and the prompt on stdin. A
// budget in dollars has no flag: the runner's time limit and token budget still hold.
func (codex) headlessArgs(o HeadlessOptions) []string {
	args := []string{codexExec, codexJSON, codexIgnoreUserConfig, codexIgnoreRules}

	if o.Model != "" {
		args = append(args, codexModel, o.Model)
	}

	args = append(args, codexOverrides(Options{Effort: o.Effort})...)
	args = append(args, codexAccess(o.Access)...)

	if o.Schema != "" && o.Access.Fence != nil {
		args = append(args, codexOutputSchema, filepath.Join(filepath.Dir(o.Access.Fence.Settings), ReportSchemaName))
	}

	if o.SystemNote != "" {
		args = append(args, codexConfig, codexInstructionsKey+"="+tomlString(o.SystemNote))
	}

	return append(append(args, o.Extra...), endOfFlags, codexStdin)
}

// dropEnv names Codex's own variables (CODEX_*), which a session it runs inside sets for
// its children (CODEX_SANDBOX, CODEX_THREAD_ID, ...) or which steer it (CODEX_API_KEY,
// CODEX_URL, ...): a background run started from such a shell would otherwise take them
// on.
func (codex) dropEnv() []string { return []string{"CODEX_*"} }

// keptEnv names the variables of Codex's own a background run keeps: its profile's
// CODEX_HOME, which the run's plan sets or unsets, and CODEX_CA_CERTIFICATE, a corporate
// proxy's certificate the network needs.
func (codex) keptEnv() []string { return []string{"CODEX_HOME", "CODEX_CA_CERTIFICATE"} }

// schemaFile: Codex reads the report's schema from a file (--output-schema), in its strict
// form.
func (codex) schemaFile() bool { return true }

// tomlString writes s as a TOML basic string, as a config override's value is read.
func tomlString(s string) string {
	var b strings.Builder

	b.WriteByte('"')

	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				b.WriteString(`\u` + leftPad(strconv.FormatInt(int64(r), 16), 4))

				continue
			}

			b.WriteRune(r)
		}
	}

	b.WriteByte('"')

	return b.String()
}

// tomlStrings writes list as a TOML array of basic strings.
func tomlStrings(list []string) string {
	parts := make([]string, 0, len(list))
	for _, s := range list {
		parts = append(parts, tomlString(s))
	}

	return "[" + strings.Join(parts, ",") + "]"
}

// leftPad pads s with zeros to n characters.
func leftPad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}

	return s
}
