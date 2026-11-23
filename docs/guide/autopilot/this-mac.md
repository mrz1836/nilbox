# This Mac

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Agents in the terminal, readiness, and Needs your OK](../../architecture/autopilot/agents-in-the-terminal.md)

Any Mac with Nilbox can run routines; each checks it has what its routines need. **This Mac**,
in Settings → Autopilot, lists each check, ✓, ⚠, or ✗, with what it found and its fix: GitHub's
sign-in (from the last refresh; **Check again** asks GitHub, for one point), `gh` and `git`,
each agent a routine runs (its version), each profile (signed in, by the agent's own
`claude auth status` or `codex login status` under the profile's folder, run without an API key
from your shell unless **Use an API key when one is set** is on), the terminal, the banners (a
warning only: the history fills without them), and the tools a routine **Requires**, all found
on your login shell's PATH, the one the agent runs with. A profile signed in through a
subscription is ready; a sign-in that expires within a week is a warning; nothing keeps the
account's email. A fix is a command to copy and run in a terminal (`CLAUDE_CONFIG_DIR=~/.claude-work
claude auth login`) or where to go: Nilbox never signs in for you. Below it, *Not ready here:*
names the routines this Mac can't run.

A routine is ready when each of its needs is ok or only a warning; what only its agent step's
**Fallbacks** need is only ever a warning, since the step runs on its own profile and a start
passes over a fallback this Mac can't run. One that isn't still decides
(Test and Dry run work), its row and builder say *Not ready on this Mac: ...*, the tab shows its
red dot, Activity says *This Mac can't run 2 of your routines* with **See what's missing**, and
you are told once per new problem. Its agent starts wait as *Can't start yet* until it is
fixed. The checks run as Autopilot starts (after the first paint, never on the cold start),
when a routine is saved or switched on, every ten minutes, and on **Check again**; right before
each start, the item's own agent and profile are checked again.
