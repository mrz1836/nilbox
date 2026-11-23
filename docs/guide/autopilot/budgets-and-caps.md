# Budgets and caps

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Agents in the background](../../architecture/autopilot/agents-in-the-background.md), [The engine and the ledger](../../architecture/autopilot/engine-and-ledger.md)

Three limits bound what the routines' agents spend, each set in dollars or in tokens:

- **A run's budget**, in the agent step's fence ($3.00 by default). In dollars, the agent keeps
  to it itself, as Claude Code does, and ends *Out of budget ($3.00).*; Codex keeps none, so a
  Codex step's budget is set in tokens. In tokens, Nilbox counts what the run reports as it goes
  and stops it there (Codex reports its tokens as its run ends, so its budget is checked then).
  The time limit (30 minutes by default) holds every run.
- **A routine's own Daily budget** (its limits) and the **Daily cap** in [Settings →
  Autopilot](settings.md), across every routine, both off by default. Before each start, today's
  use, plus the budgets of the runs still working, plus the new run's budget, must fit under each;
  otherwise the start waits (*It waits until tomorrow: it would go over today's cap (Daily cap, in
  Settings → Autopilot).*) and starts the next day, once. The day's counts word the same way: *It
  waits until tomorrow: Autopilot sent 50 GitHub actions today, the most Settings → Autopilot allows
  in a day.* A cap counts the runs budgeted in its own unit, so a dollar cap can't hold back a run
  budgeted in tokens, nor count a Codex run.
- **Tokens are the real measure.** Every run shows its tokens, in and out, with what it read from
  the cache apart, in the run view, Working, and a routine's **This week**. Its dollars are what the
  agent reports: on a subscription (Claude Max, say), what the same work would cost through the API,
  an estimate and never a bill, which Nilbox always words as *about* or *estimated*; with an API
  key, which a run uses only while its agent's **Use an API key for Claude Code when one is set** or
  **Use an API key for Codex when one is set** is on, what that key was charged. A run in your
  terminal reports neither, so no budget or cap counts it; the item's [session
  panel](../following-a-session.md) counts its session's tokens.
