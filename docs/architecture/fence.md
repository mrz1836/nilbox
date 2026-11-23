# internal/fence

[Docs](../README.md) › [Architecture](../README.md#architecture) · Code: [`internal/fence`](../../internal/fence) · Guide: [Fences: what a background agent may do](../guide/autopilot/fences.md), [What Autopilot never does](../guide/autopilot/never-does.md)

`internal/fence/` keeps an unattended agent run inside what its routine's tools preset promises
(`Preset`: `readOnly`, `githubTasks`, `editTest`, `editTestPush`, `profile`, the catalog's own
words, which core converts as they are), whichever agent runs it. Each agent's spec in
`internal/agent` turns a preset into its own flags and rules; this package holds the part that
depends on no agent. It is a leaf: it imports only `sensitive`, `paths`, and `textutil`
(`TestLeafImports`), and starts no process (`TestTheFenceRunsNothing`).

- **The fence.** `Build(Spec)` works a run's `Fence` out from its worktree (its links resolved,
  and the path as given kept as `WorktreeAs` when that differs), its git folders (read from the
  worktree's `.git` file and `commondir` through `paths.ReadHead`, `GitDirs`, when the spec
  leaves them out), its item, the pull request's head branch and the clone's remote for its
  repository, the repository's tools (`RepoTools`, from the marker files by `os.Lstat` alone),
  the routine's extra commands, and the run's, home, data, and config folders: the `Policy` the
  hook enforces, and for the edit presets (`Sandboxed`) the `Sandbox` the agent's own sandbox
  holds commands to. It refuses a folder that isn't absolute or holds what a rule can't name
  (`ErrSpec`), a tool or an extra command that would loosen a hard line (`ErrLoosens`: git and
  gh by name alone, the network tools, the shells, and any command the policy itself denies),
  and a push it can't make exact.
- **The exact push.** A push preset allows one push, `git push <remote> HEAD:refs/heads/<branch>`
  or with the short `HEAD:<branch>`, both exact, and only for a pull request whose branch is in
  the repository itself (`ErrCrossRepository` otherwise, which the engine's own
  `cross_repository` block keeps from starting), with its branch known (`ErrNoBranch`) and both
  names plain (`PlainName`, the prompts' rule: `textutil.ShellSafe` and no leading `-`;
  `PlainBranch` adds git's refname rules, so the refspec names that branch alone; `ErrNotPlain`).
  The profile's own preset starts with no push where one can't be made exact, rather than
  refusing. The exact fetch of the pull request's head, `git fetch <remote> refs/pull/<n>/head`,
  goes with a plain remote.
- **The policy.** `Decide(Policy, Call, FS)` is the one pure function every agent's hook feeds
  (its `FS` says what is on disk; nil reads nothing). It reads a Bash call's command line as a
  POSIX shell splits it (`shell.go`: each simple command with its assignments, quote-free words,
  and redirections, the commands inside substitutions, process substitutions, groups, function
  bodies, and unquoted here-documents included, and a word the shell expands at run time marked
  dynamic; a line it can't read is refused) and holds every simple command to the hard lines: no
  network tool, no command run as someone else, no shell handed `-c`, no `eval`, `env -S`, or
  `flock -c`; no program named by a variable or a glob; no `GIT_*`, `GH_*`, or `GITHUB_TOKEN`
  assignment; for git, none of the options that change where it runs or what config it reads
  (`-C`, `-c`, `--git-dir`, ...), none that run another program (`--upload-pack`, ...), only the
  preset's subcommands (the reads for every preset; the worktree's own git work for the edit
  presets), the listing subcommands only listing (no branch, tag, remote, worktree, reflog, or
  config change), no branch made by checkout or switch, every push but the exact one refused,
  and no commit whose message, on the line or in its `-F` file, carries an AI co-author
  trailer; for gh, never a merge, review, close, reopen, release, API call, or the account's
  own settings, and under the fixed presets only on its own in the line (its reads run outside
  the sandbox), only the preset's commands, GitHub tasks' writes only on the run's own item (its
  number or URL, `--repo` its repository, the flags each takes, `gh run rerun` only with
  `--failed`), and the edit presets' `gh pr checkout` of the item alone; under the wrappers
  (`xargs`, `env`, `timeout`, ...) and `find -exec`, every git, gh, or forbidden program among
  their words; and the paths a command names: none private (`ReadDeny`: the credentials under
  home, the app's data, the agent's config), and when the command writes (a `>` redirection, or
  a program that isn't one of the readers), none in git's folders or a denied category. A file
  tool writes only inside the worktree, its path and its links' target alike, never inside a
  `.git` or git's folders, never a file of the denied categories (`DeniedCategories`: agent,
  workflows, and tooling, from `internal/sensitive`'s own patterns, matched ignoring case,
  whatever the user turned off); a read tool reads nothing private. Web tools, MCP tools, and
  the tools that start work elsewhere or later are refused. The routine's own denies refuse a
  command by its first words. An agent's patch (Codex's `apply_patch`, as its own tool or a
  command) is decided as the writes of every file it adds, updates, deletes, or moves to
  (`PatchFiles`; a patch whose files it can't tell, as in a here-document, is refused), Codex's
  `view_image` as a read, and its `web_search` as a web tool. Under Read only, an agent that
  reads through its shell (`Policy.ShellReads`, which its spec sets: Codex) runs the commands
  that only read (the readers and `shellReaders`, git's reads, gh's reads, `find` without an
  action, `sed` without `-i`, no writing redirection), held besides by its own read-only
  sandbox.
- **The sandbox check.** A sandboxed run's policy holds a `Canary`: `touch <proof> <probe>`, whose
  proof is in the run folder's `ProofDir`, which the sandbox lets commands write, and whose probe
  is beside it, which it must refuse. Until the proof exists, every command but the check is
  refused, its reason giving the exact line; once the probe exists, the sandbox didn't hold, and
  every call is refused. `Sandboxed` reads the outcome for the run's end (held, leaked,
  unchecked, or none). Each agent's spec words the line the unattended note gains:
  `Fence.AgentNote` for Claude Code, `Fence.NoteWith` with its own sandbox's words for Codex.
- **The agent's rules.** `AllowedCommands`, `DeniedCommands`, and `DeniedPaths` work out, from the
  policy, the lists an agent's own rules name, so every agent and the hook hold the same ones:
  for a fixed preset whose mode refuses what no rule allows, the repository's tools, everyday
  commands, git's subcommands, gh's reads, the exact fetch and push, and the routine's own; the
  hard lines as denies, with no push deny for a push preset (it would beat the one allow); and
  the denied files at any depth they name, under each worktree path, and everything in git's
  folders. The `Sandbox` lists the folders a command may write beside the worktree (its git
  folders, the tools' caches under home, and the caches the run's environment names when they
  are safe, `envCaches`), the git folders' hooks, config, and info and the worktree's
  root-level denied files it may not, the secrets it may not read, the hosts it may reach
  (`Domains`: GitHub and the package registries), and the commands that run outside it as one
  plain command each: the exact push and fetch, the item's `gh pr checkout`, and gh's reads.
- **The hook mode.** `RunHook(args, stdin, stderr)` runs when the arguments hold `HookFlag`
  (`--fence-hook-v1=<run folder>`; any `--fence-hook` flag is the hook mode, so a build refuses
  a protocol version it doesn't know rather than start as the app). `cmd/nilbox`'s `main` calls
  it first in both builds, right after the desktop build's `updater.HandleHelperMode()`, before
  anything reads a setting, opens the data, starts logging, or touches the window. Protocol 1 is
  Claude Code's PreToolUse hook: the call's JSON on stdin (at most 32 MiB), the run folder's
  `PolicyFile` (`fence.json`, at most 1 MiB, a regular file, strict JSON of `PolicyVersion`), and
  a refusal as exit status 2 with its reason on stderr; a call let through says nothing and goes
  on to the agent's own rules. Codex's PreToolUse hook speaks the same protocol (codex-cli
  0.163.0: `tool_name`, `tool_input.command`, `cwd`). A call, or a policy, it can't read is
  refused. It writes nothing but each refusal, appended to the run folder's `DenialsFile`
  (`denials.jsonl`, `LogDenial`: only in a folder that is there, never through a link, at most
  1 MiB), since an agent's stream may never show a refused call (Codex's doesn't);
  `ReadDenials` counts them and keeps the last 20 for the run's outcome.
  `WriteRun` writes a run's folder: the policy and the agent's own files, 0600 and never through
  a link, in a 0700 folder, with the sandbox check's proof folder for a sandboxed run.
- **What changes GitHub.** `ChangesGitHub(line)` (`github.go`) reads a command line with the
  fence's own reader and reports one that changes something on GitHub: `gh pr` merge, review,
  close, reopen, comment, edit, ready, or update-branch; `gh issue` close, reopen, comment, or
  edit; `gh api` with a method other than GET, with fields and no method, or a GraphQL
  mutation; or `git push`, past git's own options and their values. It looks past the
  wrappers and into a shell's `-c` line, as Codex runs every command, to `maxShellDepth`, and a
  line it can't read is false. The sessions' observers call it to read an item again while
  its session still works; it decides nothing for the fence.
- **What a run changed.** `CheckChanged(head, files, cfg)` checks the files a run's commits
  changed against the sensitive categories, the denied ones always on (at most `MaxChanged`
  files; more make it incomplete), and `MergeChanged` adds that to an item's own file check.
  The inbox keeps the paths per item and head (`item_agent_changes`, `NoteAgentChanges`, the
  newest 20 heads, going with their item), at the head the run started from and its own last
  commit, and `AgentChanged` reads them back as a check with the user's categories; core's
  `agentFiles` is the launcher's `FileChecker` over the inbox's with them merged, so an agent
  file a run wrote holds the next launch on the item at either head as `HoldAgentFiles`, whoever
  opened it.

Claude Code keeps every preset (`internal/agent/claude_fence.go`). `agent.AccessFence` carries
the preset, for the profile's own preset the click's access it starts from (`Fence.Base`), and
the run folder's settings file (`Access.InRun`). Read only is `--restricted --tools
Read,Grep,Glob`; GitHub tasks drops every settings source (`--setting-sources ""`) with `--tools
Bash,Read`; the edit presets drop them with `--tools Bash,Read,Edit,Write,Grep,Glob`; each of
those runs in `dontAsk`, so a profile's full access can't loosen it and a profile without a mode
can't break it. The profile's own keeps the user's settings alone (`--setting-sources user`) with
the click's flags. Every one adds `--strict-mcp-config` and `--settings <run folder>/settings.json`,
whose Go types (`claudeSettings`) hold the rules, the fence hook as a command line (the app's own
executable and `HookArg`, each quoted as one word, never the exec form, whose flag a build that
doesn't know it would drop; 30 seconds, `onFailure: block`), hooks left on, attribution off (and
the older `includeCoAuthoredBy`), project MCP servers off, bypass mode off for a fixed preset,
and for the edit presets the sandbox (on, a hard failure when it can't start, no unsandboxed
retry, and no auto-allow, so the rules still decide; local binding, for a test's server). A test
checks every preset's settings against Claude Code's settings schema, recorded in
`internal/agent/testdata/claude-settings.schema.json`; another fails when a preset's flags or
settings keys change without `claudeFenceReviewed` and `claudeFenceFloor` (2.1.319,
`FenceFloor`) reviewed. `agent.Args` refuses a fenced run extra directories (`ErrFencedDirs`) and
every extra flag `CheckFenced` refuses (the fence's own, the headless run's, and those that would
loosen it, such as `--add-dir`, `--plugin-url`, and `--mcp-config`); `PrepareFence` builds a
run's fence and its files together, and `KeepsPreset` says which presets an agent keeps (each
spec's own `keeps`: Claude Code every one, Codex every one but GitHub tasks only, whose fence
is in [Codex in the background](autopilot/codex-in-the-background.md)). The launcher's `resolvePermission` chooses a background run's access from its
`Request.Fence`, below the trust rule: an untrusted item without the override runs Read only,
whatever the fence says, and a fenced plan takes none of the profile's extra directories.
