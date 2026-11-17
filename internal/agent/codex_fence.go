package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// How Codex keeps the tools presets, as S6 measured them on codex-cli 0.163.0:
//
//   - Its sandbox is a real boundary: read-only writes nothing, and workspace-write writes
//     only in the worktree, the folders named writable, and TMPDIR, with the network off
//     unless turned on. It also lets /tmp be written unless told not to (exclude_slash_tmp,
//     checked locally on 0.163.0), which every fence tells it. It refuses .git and .codex inside a writable folder, but not
//     AGENTS.md, so the fence hook refuses that and every other agent and tooling file.
//   - A linked worktree can't commit until its own git folder and the clone's objects,
//     refs, and logs are writable (sandbox_workspace_write.writable_roots); the clone's
//     hooks and config never are.
//   - Execpolicy can't keep the exact push (prefix rules match --force, and git -C or
//     sh -c match nothing), but a PreToolUse hook can: the app's own executable in its hook
//     mode, passed as -c hooks.PreToolUse=[...], which runs only with
//     --dangerously-bypass-hook-trust (without it the hook silently doesn't run). A hook
//     with no matcher runs on every tool call: the shell's (Bash), apply_patch's, and
//     view_image's. Calls it refuses never appear in the --json stream, so the hook logs
//     them in the run's folder (fence.LogDenial).
//   - The push needs the network, which workspace-write turns on for every command: under
//     Edit, test, and push, a command reaches any host, where Claude Code's sandbox reaches
//     only GitHub and the registries. The hook refuses the network tools themselves.
//   - Codex reads files through its shell, so under Read only its reading commands run
//     (fence.Spec.ShellReads), its read-only sandbox refusing every write besides.
//   - GitHub tasks runs only gh, which Codex's sandbox cuts off from GitHub and gh's
//     sign-in, and outside a sandbox only the hook would hold the run in: Codex doesn't
//     keep it.
//   - A run taken over in the terminal (codex resume) keeps its sandbox and its writable
//     folders, with the network off and every escalation asked of the user
//     (--ask-for-approval on-request), but runs no hook: the hook bypass would also run,
//     unreviewed, the hooks of a branch whose folder the user trusts, and the interactive
//     resume can't leave the user's config unread.

// Codex's fence flags and config keys.
const (
	codexBypassHookTrust  = "--dangerously-bypass-hook-trust"
	codexWorkspaceWrite   = "workspace-write"
	codexApprovalKey      = "approval_policy"
	codexNever            = "never"
	codexWritableRootsKey = "sandbox_workspace_write.writable_roots"
	codexNetworkKey       = "sandbox_workspace_write.network_access"
	codexNoSlashTmpKey    = "sandbox_workspace_write.exclude_slash_tmp"
	codexWebSearchKey     = "web_search"
	codexWebSearchOff     = "disabled"
	codexHooksKey         = "hooks.PreToolUse"

	// codexHookTimeout bounds one call of the fence hook, in seconds.
	codexHookTimeout = 30
)

// codexFenceReviewed lists every flag a Codex fence's argv passes and every config key its
// overrides set, as checked against codexFloor. A test fails when a preset uses one not
// listed here, or no longer uses one that is.
func codexFenceReviewed() []string {
	return []string{
		// Flags.
		"--ask-for-approval", "--config", "--dangerously-bypass-approvals-and-sandbox",
		"--dangerously-bypass-hook-trust", "--sandbox",
		// Config keys.
		"approval_policy", "hooks.PreToolUse", "sandbox_workspace_write.exclude_slash_tmp",
		"sandbox_workspace_write.network_access", "sandbox_workspace_write.writable_roots", "web_search",
	}
}

// keeps: Codex keeps every preset but GitHub tasks only.
func (codex) keeps(p fence.Preset) (bool, string) {
	if p == fence.GitHubTasks {
		return false, "Codex can't keep GitHub tasks only: its sandbox cuts gh off from GitHub and its sign-in, and " +
			"outside a sandbox nothing but the fence's hook would hold the run in."
	}

	return true, ""
}

// fenceSpec: Codex has no file tools of its own, and reads through its shell.
func (codex) fenceSpec(s fence.Spec) fence.Spec {
	s.ShellReads = true

	return s
}

// fenceFloor is codexFloor.
func (codex) fenceFloor() string { return codexFloor }

// fenceNote: Codex's sandbox writes only in the worktree, its git folders, and the
// temporary folder, with the network on only for a push preset, for its one push.
func (codex) fenceNote(built fence.Fence) string {
	words := "Commands run in a sandbox that writes only in this worktree, its git folders, and the temporary folder"
	if built.Policy.Push != nil {
		words += "; use the network for the one push alone."
	} else {
		words += ", with no network."
	}

	return built.NoteWith(words)
}

// backgroundReserved lists the flags no extra argument of a background run sets: the
// headless run's own, the fence's, and those that would widen it (other providers, the
// features switched on or off, a profile's config layered on, extra directories, images,
// another worktree, live search).
func (codex) backgroundReserved() []reservedFlag {
	fenced := []string{
		"--json", "--output-schema", "--ignore-rules", "--ignore-user-config", "--ephemeral", "--strict-config",
		"--skip-git-repo-check", "--thread-source",
	}
	loosens := []string{
		"--enable", "--disable", "--oss", "--local-provider", "--add-dir", "--search", "--cyber-access-program",
		"--remote", "--remote-auth-token-env",
	}

	out := make([]reservedFlag, 0, len(fenced)+len(loosens)+3)

	for _, long := range fenced {
		out = append(out, reservedFlag{long: long, reason: reasonFence, access: true})
	}

	for _, long := range loosens {
		out = append(out, reservedFlag{long: long, reason: reasonLoosens, access: true})
	}

	return append(out,
		reservedFlag{long: "--output-last-message", short: "-o", reason: reasonFence, access: true},
		reservedFlag{long: "--profile", short: "-p", reason: reasonLoosens, access: true},
		reservedFlag{long: "--image", short: "-i", reason: reasonLoosens, access: true},
	)
}

// codexFence is what a Codex fence's own settings file records: the flags its fence
// passes, so the run's folder says what it ran under, and Take over finds it there.
type codexFence struct {
	Flags []string `json:"flags"`
}

// fenceFiles returns Codex's record of a fenced run's flags, worked out for its policy
// and hook: Codex reads its fence from the command line, not from a file.
func (codex) fenceFiles(f *Fence, built fence.Fence, program string) ([]fence.File, error) {
	worked := *f
	worked.Policy, worked.Hook = &built.Policy, program

	data, err := json.MarshalIndent(codexFence{Flags: codexFenceArgs(&worked)}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("write the fence's settings: %w", err)
	}

	return []fence.File{{Name: FenceSettingsName, Data: append(data, '\n')}}, nil
}

// codexFenceArgs returns the flags of a fenced run's access: the preset's sandbox (or the
// profile's own access), nobody asked (or, taken over, the user asked about the rest), the
// folders beside the worktree its commands may write, the network for the one push, no
// web search, and the fence hook.
func codexFenceArgs(f *Fence) []string {
	if f == nil {
		return nil
	}

	args, sandbox := codexPresetAccess(f)
	args = append(args, codexApprovalArgs(f)...)

	if sandbox == codexWorkspaceWrite && f.Policy != nil {
		args = append(args, codexWorkspaceArgs(f)...)
	}

	args = append(args, codexConfig, codexWebSearchKey+"="+tomlString(codexWebSearchOff))

	if f.Hook != "" && f.Policy != nil && !f.TakenOver {
		args = append(args, codexBypassHookTrust, codexConfig, codexHooksKey+"="+codexHook(f.Hook, runDirOf(f)))
	}

	return args
}

// codexApprovalArgs returns who a fenced run asks: no one in the background, and, taken
// over, the user about whatever the sandbox refuses (but under full access, which asks
// nothing).
func codexApprovalArgs(f *Fence) []string {
	switch {
	case !f.TakenOver:
		return []string{codexConfig, codexApprovalKey + "=" + tomlString(codexNever)}
	case codexFullAccess(f):
		return nil
	default:
		return []string{codexApproval, codexApproveOnAsk}
	}
}

// codexWorkspaceArgs returns a workspace-write run's folders its commands may write beside
// the worktree, and the network for a push preset's one push in the background.
func codexWorkspaceArgs(f *Fence) []string {
	var args []string

	if roots := codexWritable(*f.Policy); len(roots) > 0 {
		args = append(args, codexConfig, codexWritableRootsKey+"="+tomlStrings(roots))
	}

	args = append(args, codexConfig, codexNoSlashTmpKey+"=true")

	if f.Preset == fence.EditTestPush && f.Policy.Push != nil && !f.TakenOver {
		args = append(args, codexConfig, codexNetworkKey+"=true")
	}

	return args
}

// codexPresetAccess returns a preset's own sandbox flags, and the sandbox they choose ("" for
// Codex's own default or none at all): each fixed preset's, or the profile's own access, as
// a click's launch passes it.
func codexPresetAccess(f *Fence) ([]string, string) {
	switch f.Preset {
	case fence.ReadOnly, fence.GitHubTasks:
		return []string{codexSandbox, codexReadOnly}, codexReadOnly
	case fence.EditTest, fence.EditTestPush:
		return []string{codexSandbox, codexWorkspaceWrite}, codexWorkspaceWrite
	case fence.Profile:
		switch f.Base.Kind {
		case AccessFull:
			return []string{codexBypass}, ""
		case AccessMode:
			return []string{codexSandbox, f.Base.Mode}, f.Base.Mode
		case AccessPlan, AccessPlanAllowFull:
			return []string{codexSandbox, codexReadOnly}, codexReadOnly
		case AccessDefault, AccessFence:
			return nil, ""
		}
	}

	return nil, ""
}

// codexFullAccess reports the profile's own preset with full access, which asks for no
// approvals and has no sandbox.
func codexFullAccess(f *Fence) bool {
	return f.Preset == fence.Profile && f.Base.Kind == AccessFull
}

// codexWritable returns the folders beside the worktree a workspace-write run's commands
// may write: the worktree's own git folder and the clone's objects, refs, and logs, so a
// commit can be made (never the clone's hooks or config), and the sandbox check's proof
// folder.
func codexWritable(p fence.Policy) []string {
	var out []string

	if n := len(p.GitDirs); n > 0 {
		gitDir, common := p.GitDirs[0], p.GitDirs[n-1]
		if gitDir != common {
			out = append(out, gitDir)
		}

		for _, sub := range []string{"objects", "refs", "logs"} {
			out = append(out, filepath.Join(common, sub))
		}
	}

	if p.Canary != nil {
		out = append(out, filepath.Dir(p.Canary.Proof))
	}

	return out
}

// codexHook is the fence hook's config override value: one PreToolUse hook with no matcher,
// so it runs on every tool call, whose command is the app's own executable in its hook
// mode, for the run's folder.
func codexHook(program, runDir string) string {
	command := textutil.ShellQuote(program) + " " + textutil.ShellQuote(fence.HookArg(runDir))

	return `[{hooks=[{type="command",command=` + tomlString(command) + `,timeout=` + strconv.Itoa(codexHookTimeout) + `}]}]`
}

// runDirOf returns the run's folder, where the fence's settings are.
func runDirOf(f *Fence) string {
	return strings.TrimSuffix(f.Settings, "/"+FenceSettingsName)
}
