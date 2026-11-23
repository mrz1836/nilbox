# When a routine misbehaves

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [The engine and the ledger](../../architecture/autopilot/engine-and-ledger.md), [Verify, tries, the loop guard, and profile fallback](../../architecture/autopilot/verify-and-tries.md)

- **It does something you didn't want.** **Pause Autopilot** (the tab's header, the menu bar,
  or Settings), and **Stop all** if agents are working. Open the routine's Activity to see each
  match and why, **Undo** what it changed on your Mac, put it back in **Dry run**, fix its
  conditions or limits, and **Test** before you turn it back on.
- **It seems to do nothing.** Settings → Autopilot's **Last checked** says whether the engine ran,
  and *Autopilot is running in another copy of Nilbox* means this one doesn't hold the lock. **This
  Mac** says what is missing. The routine's **Watching** list shows whether its search finds the
  item, and the item's **⋯** menu → **Autopilot ▸** → **Why didn't a routine run?** gives each
  condition with its value, and the ledger's view.
- **A run is stuck.** **Stop** asks the agent to stop, then SIGTERM, then SIGKILL. A run still
  marked working after a restart is followed again if its process lives, else *interrupted*,
  its match back under Needs you with **Take over**. Its worktree can be cleaned up in the
  Worktrees view once nothing runs in it.
- **The GitHub budget burns.** The budget menu shows Autopilot's share, and the routines'
  searches stop themselves below a quarter of the GraphQL budget. Watch fewer owners, or narrow
  the searches.
- **GitHub actions pile up.** **GitHub actions a day** and the write pace bound them, and a
  routine's first pass over 25 waits as a backlog. Lower the limit, or move the routine to Ask
  first.
- **Spend runs high.** Lower the run budget and the daily cap, move routines to a smaller
  model or a lower effort, and check each routine's **This week**.
- **An agent pushed a bad change.** It is on the pull request's own branch, and nothing merges
  it but a **Merge it** step you added, once its checks pass. Revert it on the branch, then
  tighten the routine: **Edit and test, no push**, or its prompt.
- **Run logs fill the disk.** Shorten **History kept** or **Run logs kept**, or **Clear
  history…**, which removes the ended runs' logs and keeps the routines.
