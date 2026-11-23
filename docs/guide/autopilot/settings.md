# Settings → Autopilot

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [The routines, kept](../../architecture/autopilot/routines-kept.md)

This section, marked **Beta**, shows while Autopilot (beta) is on in **Settings → Labs**.

| Setting | Default | What it does |
|---|---|---|
| **Autopilot (beta)**, in **Labs** | Off | Shows and runs Autopilot; off, it is hidden and runs nothing, and the routines stay as they are |
| **Pause Autopilot** | Off | No routine starts anything new; what is under way carries on |
| **New routines start in** | Ask first | The mode a routine you make starts in; a routine keeps its own once made |
| **Agents at once** | 2 | The most coding agents your routines will run at the same time, at most 3 |
| **Agent runs a day** | 20 | The most coding agent runs your routines will start in a day, together |
| **GitHub actions a day** | 500 | The most actions on GitHub your routines will send in a day, together, up to 2,000 |
| **GitHub actions an hour** | 120 | The most they send in an hour, 60 to 300, and a sixth of that a minute: GitHub allows about 500 writes an hour, which your own clicks and tools share |
| **Daily cap** | Off | The most your routines' coding agents will use in a day, in dollars ($1 to $500) or tokens (100,000 to 50 million); see [budgets and caps](budgets-and-caps.md) |
| **Keep the Mac awake while agents work** | Off | Keeps the Mac from going to sleep on its own while a routine's agent works |
| **Hold agent starts while their provider reports an outage** | On | Holds a routine's agent while its provider's status page reports a major outage |
| **Ask before catching up after** | 24 hours | After Nilbox was closed this long, a routine's agent and GitHub steps will wait for your OK; local steps never do |
| **History kept** | 90 days | How long what the routines did is kept; lowering it lowers the run logs with it |
| **Run logs kept** | 30 days | How long an agent run's log will be kept, never longer than the history |
| **Let routines merge your own pull requests past the rules** | Off | A routine whose Merge it allows it, and that you confirmed as you saved it, may merge your own signed pull request that lacks only an approval from someone else ([how](merge-and-post-review.md)); off, none does, and turning it off stops every such merge at once. Never in a backup, and Describe it can't propose it |
| **Clear history…** | | Removes now, after asking, what the routines did with items that are done with: gone, merged, or closed, or out of every list and search for longer than the history is kept. Your routines and what still waits stay, and it says *Nothing to clear.* when there was nothing |
| **Stop all** | | Stops at once every routine's coding agent working in the background, and every script a routine's step runs; the runs you started yourself carry on |
| **Describe it's agent**, under **Claude Code** | Inherit | The Claude Code profile, model, and effort a new [Describe it](describe-it.md) conversation starts with, each **Inherit** until you choose one; the profile inherits the launch defaults' Claude Code profile, and the model and effort that profile's own, which every turn runs with (a profile with none of its own runs Claude Code's default). A conversation keeps the agent it started with, and its **Change ▸** starts another |
| **Use Nilbox in your own Claude Code**, under **Claude Code** | | Each Claude Code profile, with whether the plugin is installed there and **Install** or **Remove**: the plugin lets a session of your own propose routines too ([how](your-own-claude-code.md)) |
| **Last checked** | | When Autopilot last checked the routines, how long it took, and what it found (*Last checked today, 11:59 AM · 34 ms · 7 matches · 1 start*), or that another copy runs it |

The limits hold every routine's coding agents, in the background and in your terminal, and its
[GitHub actions](changes-on-github.md). A limit delays a match and never loses
it: one over today's limit starts tomorrow, and a day runs from local midnight. Local steps keep
only to Pause Autopilot and a routine's own active hours. Backups carry these settings but **Let
routines merge your own pull requests past the rules**, which stays as it is here, and the
**Autopilot routines** category carries the routines, never your confirmation that one merges
past the rules (see [Backing up your setup](../backing-up.md)).
