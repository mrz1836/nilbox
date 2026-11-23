# Agents in your terminal

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Agents in the terminal, readiness, and Needs your OK](../../architecture/autopilot/agents-in-the-terminal.md)

A routine's **Run a coding agent** step, **In my terminal**, opens your terminal on the item
exactly as a click on its launch button does: the same agent, profile, model, and effort as the
launch defaults resolve (the routine's own choices taking the launch menu's place), the same
prompt, worktree, holds, workspace trust, and session record, so the item's session pill
follows it as any session. The prompt can name the routine too: **.Routine**, **.Trigger** (what
made it run, *Checks fail*), **.Attempt** (which of its tries this is), and **.Unattended**
(false here: you are there to answer), beside the rest, and **.FailedChecks** names the checks
that failed on the head commit, from the last read of them. A terminal run has no end Nilbox can
see, so nothing follows it but a notice, and the step ends as the terminal opens. Ask first suits
it best: on Autopilot, the builder warns, it opens a terminal tab while you work.

Right before each start, in this order: an [outside contributor](../outside-contributors.md)'s
item takes an agent only where its org or repository allows it, planning only; this Mac must be
[ready](this-mac.md) for the routine; the step's prompt must still be in your library and fit
the item; while **Hold agent starts while their provider reports an outage** is on, the agent's
provider's status page must not report a major outage on the part it depends on (Claude Code's,
or Codex CLI's); the repository needs a clone, and the tools its files say it needs (a `go.mod`
needs `go`, a `pnpm-lock.yaml` `pnpm`, `magefiles/` `magex`, ...) on your login shell's PATH; and
only then is the item read again from GitHub (one point), so a start held back here spends
nothing. The launch itself then holds back what a click's would, and a little more:

- A pull request's files are checked for **every pull request you didn't open**, trusted bots'
  and members' included, whatever **Settings → Sensitive files** says: a trusted bot's pull
  request can still carry agent configuration, synced from another repository say. One that
  changes agent files, or whose files couldn't all be checked, waits for your OK.
- A session working in the item's worktree queues the start until it ends; a routine never
  starts beside one.
- A worktree with uncommitted changes waits for your OK: Nilbox never starts in your work in
  progress, nor cleans it for you.
