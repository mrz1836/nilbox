# Codex in the background

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/agentrun`](../../../internal/agentrun), [`internal/autopilot`](../../../internal/autopilot) · Guide: [Agents in the background](../../guide/autopilot/agents-in-the-background.md)

Codex runs in the background through the same planner, fence, runner, run view, and Take over
as Claude Code: everything it does differently is its spec's (`internal/agent/codex_headless.go`,
`codex_fence.go`) and its parser's (`internal/agentrun/stream/codex.go`), as measured on
codex-cli 0.163.0, its version floor (`codexFloor`, `Headless.MinVersion`).

- **The run.** `codex exec --json --ignore-user-config --ignore-rules`, the model and
  `model_reasoning_effort`, the fence's flags, `--output-schema <run folder>/report-schema.json`,
  the unattended note as `developer_instructions`, the extra arguments, and `-- -`, the prompt
  on stdin. `--ignore-user-config` leaves the profile's `config.toml` unread (its MCP servers,
  plugins, `notify` program, and the projects it trusts; the sign-in still comes from
  `CODEX_HOME`), so a branch's own `.codex/config.toml` and `.codex/hooks.json`, which Codex
  loads only in a folder that config trusts, never load: checked locally, a trusted branch's MCP
  command and its `hooks.json` hook ran, the latter under the hook bypass the fence needs, and
  with the flag neither did (`TestABranchsCodexConfigCantRunACommandInABackgroundRun` replays
  it). `--ignore-rules` leaves every execpolicy file unread. A Codex run reads no trust
  (`Headless.ReadsTrust` false), so `Backgrounds.Trust` grants none, and a Take over under the
  fence grants none either: Codex asks the user. Never `--approve-for-me` (it refuses
  `--sandbox` and lets a model approve escalations) or `--ephemeral` (the thread must be kept to
  be taken over). The report is the strict form of the schema (`agent.StrictSchema`:
  `additionalProperties` false, every property required, the optional ones nullable), written
  by `agent.HeadlessFiles`, which `Backgrounds.Fenced` adds to the run's files. The
  environment drops every `CODEX_*` variable but `CODEX_HOME`, which the plan sets per profile,
  and `CODEX_CA_CERTIFICATE`.
- **The fence.** Codex keeps every preset but GitHub tasks only (its sandbox cuts gh off from
  GitHub and its sign-in, and outside one only the hook would hold the run in): `KeepsPreset`
  says so, and the builder offers it off with that reason (`agent.Agent.Background.Presets`).
  Read only is `--sandbox read-only`, and since Codex reads through its shell, the policy lets
  its reading commands run (`fence.Spec.ShellReads`, set by its spec). The edit presets are
  `--sandbox workspace-write`, with `sandbox_workspace_write.writable_roots` naming the
  worktree's own git folder and the clone's `objects`, `refs`, and `logs` (so it can commit,
  never touching the clone's hooks or config) and the sandbox check's proof folder, and `/tmp`
  kept out (`exclude_slash_tmp`: workspace-write lets `/tmp` be written by default, which the
  sandbox check caught); Edit, test, and push turns the sandbox's network on for its one push (`network_access`), which reaches
  any host: the hook refuses the network tools, but a test program isn't held to GitHub and the
  registries as Claude Code's sandbox holds it. The profile's own is a click's access. Every
  preset passes `approval_policy="never"`, `web_search="disabled"`, and the fence hook as
  `--config hooks.PreToolUse=[{hooks=[{type="command",command=...,timeout=30}]}]` with
  `--dangerously-bypass-hook-trust`, without which a `-c` hook silently doesn't run; a hook
  with no matcher runs on every call. The hook's protocol is Claude Code's (`--fence-hook-v1`),
  and the policy knows Codex's tools: `Bash`, `apply_patch` (its patch's files decided as
  writes, `fence.PatchFiles`, so `AGENTS.md`, which the sandbox lets through, is refused), and
  `view_image`. Codex's `--json` never shows a call its hook refused, so the hook logs each in
  the run's folder (`fence.LogDenial`, `denials.jsonl`), which the run's outcome counts
  (`Outcome.Refused`, `Refusals`). The fence's own record of its flags is the run folder's
  `settings.json` (`codexFence`), which Take over finds the fence by, as for Claude Code. The
  argv needs the fence worked out for the worktree, so `agent.Fence` carries its `Policy` and
  `Hook` once `Backgrounds.Fenced` has them (`Access.Worked`, `launcher.Fence.Policy`), and Take
  over reads the policy back from the run's folder. This argv was run, locally and once, by a
  real codex-cli 0.163.0 against a stand-in for its provider (no usage spent): the app's own
  binary ran as the hook on every shell call, held commands until the sandbox check, which
  wrote its proof and was refused its probe, refused `git push origin HEAD:other` and `curl`,
  logged both, and let a write in the worktree through; Codex took the strict schema and the
  note as written.
- **The stream** (`agentrun/stream/codex.go`): `thread.started` names the thread, which Codex picks,
  so the runner links the run's session to it as soon as it comes
  (`SessionUpdate.AgentSessionID`, `sessions.RunUpdate.AgentSessionID`); items' starts and ends
  are the tools and their results (a command shown without the login shell Codex wraps it in);
  the last `agent_message` is the final message and, as JSON, the report; `turn.completed` is
  the end, with its tokens (no cost: a dollar budget or cap can't count it), `turn.failed` an
  error, and one with `usage_limit_reached`, `quota_exceeded`, a workspace's credits depleted,
  or "usage limit" is out of usage. Which limit and when it resets come from the thread's
  rollout (`agentrun.LimitReader`, core's `runLimits` over `observe/codex.Limits`: the last
  token count's `rate_limit_reached_type` and its window), so the profile fallback rests the
  profile until then. A stream with neither end was cut off: stopped, interrupted, or failed
  without a word. Its tokens come only as the turn ends (`Headless.UsageLive` false), so a
  token budget is checked then, and the builder says so; a dollar budget has no flag
  (`Headless.Budget` false), so its runs never outlive the quit and the builder says *Budget:
  not available*. After a SIGKILL, the processes it left in the worktree are swept, as Claude
  Code's.
- **Take over** is `codex resume <thread>` in the worktree, under the run's sandbox and
  writable folders again (a resume keeps no sandbox of its own), `--ask-for-approval on-request`
  so whatever the sandbox refuses is asked, the network off so a push asks first, and no hook,
  whose bypass would also run a trusted branch's hooks unreviewed in the interactive session,
  which can't leave the user's config unread. Its `developer_instructions` persist in the
  thread, so the resume's first message tells it someone answers now (`Options.TakeOver`,
  `codexTakeOverNote`), with the fence or the profile's permissions alike.
