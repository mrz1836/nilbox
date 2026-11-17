// Package fence keeps an unattended agent run inside what its routine's tools preset
// promises, whichever agent runs it. A preset (Read only, GitHub tasks only, Edit and test,
// Edit test and push, The profile's own) is a promise about what the agent may do; each
// agent's spec in internal/agent turns it into that agent's own flags and rules, and this
// package holds the part that depends on no agent:
//
//   - Build works a run's Fence out from its Spec: the Policy the fence hook enforces on
//     every tool call, and for the edit presets the Sandbox the agent's own sandbox holds
//     its commands to (the folders they may write, the hosts they may reach, the secrets
//     they may not read).
//   - Decide is the policy itself, one pure function over a tool call: exactly one push
//     form, to the pull request's own branch on the clone's remote for the repository, for
//     a pull request whose branch is in the repository itself; no edit outside the worktree,
//     inside its git folder, or of an agent, workflow, or tooling file (internal/sensitive's
//     own patterns, so the two can't drift); none of the network tools; no merge, review,
//     close, release, or API call through gh; no commit carrying an AI co-author trailer;
//     and, for a sandboxed run, no command before the sandbox has been seen to hold.
//   - RunHook is the hook mode: the app's own executable, started by the agent before each
//     tool call with HookFlag, reads the call on stdin, decides it against the run folder's
//     PolicyFile, and answers in the agent's hook protocol. It reads nothing else, writes
//     nothing, and opens nothing of the app's, so cmd/nilbox runs it before anything reads a
//     setting or opens the data.
//   - Changed checks the files a run's commits changed against the sensitive categories,
//     so a run that touched agent files holds the next click on its item at that head.
//
// The fence is a guardrail, not a sandbox: it reads the command line, which a determined
// command can disguise. The boundaries are the agent's own: its permission mode, which
// denies what no rule allows, and its sandbox, which keeps a command's writes and network
// in bounds whatever the command line says.
package fence
