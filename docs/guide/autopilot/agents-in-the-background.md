# Agents in the background

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Agents in the background](../../architecture/autopilot/agents-in-the-background.md), [Codex in the background](../../architecture/autopilot/codex-in-the-background.md), [`internal/agentrun/stream`](../../architecture/agentrun/stream.md)

A routine's **Run a coding agent** step, **In the background**, runs the agent headless in the
item's own worktree, with no terminal opened, held in by the step's fence: its tools preset
([Fences](fences.md)), its budget, and its time limit. Claude Code
and Codex both run there. A preset the step's agent can't keep (Codex's GitHub tasks only) isn't
offered for it, and the builder says why; one an inherited agent can't keep, for the item's
repository, leaves the start not ready.

- **Planned as a click's launch.** The same agent, profile, model, effort, and prompt as a terminal
  run, and the same checks right before the start ([Agents in your
  terminal](agents-in-your-terminal.md)), with the same holds: agent files in a pull request you
  didn't open wait for your OK, a session in the worktree queues the start, and uncommitted changes
  wait for your OK. The worktree is always the item's, made at the head the routine decided on, and
  one at another head waits for you. The prompt's **.Unattended** is true. An outside contributor's
  item runs read only whatever the fence says.
- **What it runs with.** Your login shell's environment, with the profile's own (the default
  profile's config folder is never named, and no API key signs the agent in unless **Use an API
  key when one is set** is on), without the variables an agent session sets for itself; no permission
  prompts, which no one would answer; the fence's flags and settings, worked out for the
  worktree as the run starts and kept in the run's own folder, and its line in the note the
  agent is given (what it may do, and for a sandboxed preset, the check to run first); the budget in dollars,
  which the agent keeps to itself, or in tokens, which Nilbox stops it at; and the time limit,
  30 minutes unless the step says. Claude Code 2.1.319 or later is needed, and Codex 0.163.0 or
  later; an older one leaves the routine not ready, saying so.
- **Codex** runs as `codex exec`, reading none of your Codex config (its MCP servers, plugins,
  notify program, and the folders it trusts), so a branch's own `.codex` config and hooks never
  load, and none of its rules files; its sign-in is the profile's. It keeps no budget of its own
  and reports tokens, never a cost: the builder says *Budget: not available for Codex in
  dollars*, a budget in tokens is checked as its run ends, the time limit always holds, a daily
  cap in dollars can't count it, and its runs stop when the app quits. Out of usage, it says so
  as Claude Code does, with when the limit resets from its own record of the thread.
- **How it ended** is the agent's own last word, never its exit status: *Finished:* with its
  notes, *Out of budget ($3.00).*, *Stopped after 30 minutes.*, *Out of usage: its plan's
  five-hour limit is reached. It resets Mon 15:04.*, or its error. What it cost is what the
  agent reports: on your plan rather than an API key, an estimate of what the API would have
  charged, which **Agent finished** says (*About $0.12, estimated.*). It tells you once when a
  run ends, done or failed, never for one you stopped. A run whose sandbox check shows its
  sandbox didn't hold is failed, whatever the agent says, and the files its commits changed are
  noted on the item, so a launch there waits for your OK when it wrote agent files.
- **Working**, at the top of Activity, lists each agent at work: its item, its routine, how
  long, its tokens so far, and its latest step, with **Watch**, which opens its run view, and
  **Stop**, and **Stop all** for every one (Settings → Autopilot has it too), which stops the
  routines' script runs too. The starts queued are under [Needs you](needs-you.md), each saying
  what it waits on.
- **The run view** opens beside Activity, from Working's **Watch**, an Activity line's **Show
  the run**, an **Agent finished** banner or notification, the menu bar's agent, the item's
  session panel, its launch button, or the line a review or issue sheet shows under its work
  line (*⚡ Fix failing checks pushed 4f2a9c1 · Fixed*). Its header says where the run stands,
  how long it took, its cost against its budget (≈ for an estimate on your plan; the budget
  alone until the agent reports), the agent, model, effort, worktree, and preset, its tokens in
  and out with its cache's apart, and the routine's steps with the one it is at marked. **Take
  over in iTerm2** (named for the terminal chosen in Settings), **Stop**, **Diff** (the
  worktree's changes since the run started, committed or not, read-only, with the files git
  doesn't track), **Show the worktree**, **View on GitHub**, and **Show the log file**. The
  agent's todo list is a live checklist on the left; on the right, the timeline: each command
  with ✓ or ✗ once it answers, the files it edited, its reads and searches collapsed, what it
  said, what the fence refused, and its end, each at the time the agent wrote it. A run that
  works shows from memory, as it goes; one that ended, or one from before a restart, reads its
  timeline from its log a page at a time, and **Show earlier events** reads the page before, so
  a long log never loads whole. Messages the app didn't know are counted, never shown. Once it
  ends, the view says how it came out: Nilbox's verdict from GitHub, the agent's own report or
  its last words, whether it needs a person, its turns, its time, its commit, and how many files
  it changed.
- **Take over** stops the agent while it works, waits for its end, and opens its conversation
  in the terminal chosen in Settings, in its worktree, with its profile (`claude --resume
  <session>`), as a launch does, held back as one is. It keeps the run's fence, but someone
  answers now: anything the fence doesn't allow asks you instead of being refused (its rules,
  its hook, and its sandbox still hold). **Use the profile's permissions for this session**,
  unticked, gives that one session the profile's own, under the trust rule as always, so an
  outside contributor's item still only plans. Background runs pass
  `--system-prompt-snapshot off`, so the unattended note isn't replayed on resume, and the
  resumed session is yours: no *in the background* mark, followed as any session is, and it
  holds a click on the item, and the routine's next start there, while it runs. The run's match
  closes as **Taken over**, counting as no try, and nothing says it stopped. A run whose fence is
  no longer kept (its history cleared) is taken over only with the profile's permissions. A
  Codex run is taken over as `codex resume <thread>`, under the run's sandbox again, asking you
  about whatever it refuses, with the network off, so a push asks first; its fence's hook
  doesn't follow it into the terminal, nor is its worktree trusted for it, so Codex asks you
  whether to trust the folder. Its first message tells it you've taken over, since its
  unattended note stays in the conversation.
- **Where it shows.** The item's session pill and panel follow the run, with **Watch**,
  **Stop**, and **Take over** in place of Show in terminal and Resume, and its launch button
  says **Watch**. The Worktrees view's row for its worktree says *⚡ <routine> · in the
  background* where its agent goes, and its drawer that the routine made it, so the cleanup
  never removes it; a click's launch there is held
  back, never started beside it: the dialog offers **Watch it** and **Take over** instead.
- **Quitting** while agents work asks first, as for any run ([Runs](../runs.md)), naming them
  (*2 agents are still working.*). **Stop and quit** stops each (the agent's own Control-C
  first, so it reports its end). **Let them finish** (**Let it finish** for one), offered while
  every run keeps to a budget of its own (Claude Code's), quits and leaves them working: they
  stay recorded as going, and the next start follows each again to its end and its verdict,
  stopping at once one found past its time limit. A Codex run keeps no budget of its own, so it
  always stops with the app, and the question says so. A confirmed **Restart to Update** lets
  such runs carry on the same way, and the new version follows them. A run a quit or a crash
  cut off is *interrupted*, and its match comes back under Needs you, with **Take over** in its
  session's panel. A process the
  agent left working in the worktree is ended with it. While **Keep the Mac awake while agents
  work** is on, the Mac stays awake for each run.
- **Its log** is kept in `script-runs/` and its prompt and settings in `agent-runs/` in the
  data folder, readable only by you, for **Run logs kept** (30 days), 200 runs and 1 GB at
  most; **Clear history…** removes those of the runs that ended. These runs aren't listed in
  Scripts → Runs.
- **Out of usage.** A run your plan's usage limit refused (*Out of usage: its plan's five-hour
  limit is reached.*) is no try: its profile rests until the limit resets (an hour when the
  limit doesn't say), and the step starts again on the next profile of its chain, its own
  profile first and then each of its **Fallbacks** in order, another agent's included. A
  fallback of the same agent keeps the step's model and effort; another agent's takes that
  agent's launch defaults, so a Claude Code profile out of usage can fall back to Codex. A
  fallback this Mac can't run in the background (one whose agent isn't installed, or can't keep
  the step's preset) is passed over, and only warns in [This Mac](this-mac.md) and the builder. With every profile resting,
  the start waits, *Queued: the default profile is out of usage until Mon 15:04*, asking GitHub
  nothing, and starts once the soonest rest ends; a restart remembers which profiles rest.
