# Launch defaults

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/launchdefaults`](../architecture/launchdefaults.md), [`internal/launcher`](../architecture/launcher.md)

**Settings → Repositories → Launch defaults** sets what launches use for an org's repositories,
or for one repository: the coding agent; a profile, a model, and extra flags for each agent; the
default prompt for pull requests, for issues, and for each situation a pull request can be in
(below); and the effort, plan mode, and worktree, which every agent shares. Each value can also
inherit, and the form shows what it inherits and from where. The per-agent values sit on a tab per agent, so a repository can launch on your work
account with either agent. The launch menu's **Save these choices as the default for** saves its
choices for the repository or the whole org in one click, and says when a saved choice won't
change this item's launches: a setting the chosen prompt sets itself, or, for the org, one the
repository sets itself.

The agent resolves first, in this order: the launch menu's switch, the repository's launch
defaults, its org's, the default agent under **Settings → Launching**, and last Claude. Every
other launch setting is then that agent's, resolving in this order, the first that is set
winning:

1. the launch menu's choice for this launch;
2. the chosen prompt's launch settings (the agent's model, the effort, plan mode, worktree);
3. the repository's launch defaults;
4. its org's launch defaults;
5. the profile's own settings (model, effort, plan mode);
6. the app's settings under **Settings → Launching** (the agent's default profile, the prompt
   for each kind and each situation, and the worktree default).

The effort is shared, so a level only one agent has, such as Codex's `ultra`, is skipped with a
warning when the other agent launches, and that agent's own default is used.

Extra flags add up instead: the profile's, then the org's, the repository's, and the prompt's,
each the launched agent's own. Flags the launcher sets itself can't be added this way, and a
value starting with `~` is refused (as in `~/x` or `--add-dir=~/x`), since nothing expands it.
Neither can `--`, after which the agent would read everything as the prompt. The flags are
checked layer by layer and then all together, since an option ending one layer would take the
next layer's first word as its value. Each agent reserves its own flags:

- **Claude Code:** `--model`, `--effort`, `--worktree` (`-w`), `--name` (`-n`), the permission
  flags (`--permission-mode`, `--dangerously-skip-permissions`,
  `--allow-dangerously-skip-permissions`), `--print` (`-p`), `--background` (`--bg`), and the
  flags that pick the conversation, since each launch starts its own
  [followed session](following-a-session.md): `--session-id`, `--resume` (`-r`), `--continue`
  (`-c`), `--fork-session`, `--from-pr`, and `--teleport`; and `--desktop`, `--cloud`, and
  `--environment`, which move the session out of your terminal. A short option counts wherever
  Claude Code reads one, so `-r<id>` and the combined `-cw` are refused too.
- **Codex:** `--model` (`-m`), the permission flags (`--sandbox` (`-s`), `--ask-for-approval`
  (`-a`), `--dangerously-bypass-approvals-and-sandbox`, `--dangerously-bypass-hook-trust`,
  `--approve-for-me`, `--full-auto`, and `--yolo`), `--worktree` (`-w`), `--cd` (`-C`), and
  `--remote`; a `-c`/`--config` override of `model`, `model_reasoning_effort`, `service_tier`,
  `sandbox_mode`, `approval_policy`, `sandbox_permissions`, `sandbox_workspace_write`, or any
  `projects.*` key, in every spelling Codex reads (`-c key=value`, `-ckey=value`,
  `--config=key=value`); and any of Codex's command names as a word of its own (`exec`, `e`,
  `review`, `resume`, `apply`, and the rest), which would run that command instead of a
  session, without the launch's sandbox. Other overrides, such as `-c model_verbosity=low`, and
  `-p` (a Codex config profile) are fine.

## A situation's prompt

A launch for the situation an inbox card names, a pull request whose checks fail, that conflicts
with its base, that a reviewer asked to change, or that you're asked to review again, takes the
repository's prompt for that situation, else its org's, else the app's, and with none of those set,
the pull request's prompt as above; the settings that prompt brings follow it. So an org's prompt
for pull requests doesn't beat the app's prompt for a situation: a review prompt is the wrong one to
fix checks with, and an org that wants its own sets its own for the situation. The app's prompt for
each starts on its starter (Fix checks, Resolve conflicts, Address review, Re-review). **Settings →
Launching** sets it in **Prompt when checks fail**, **Prompt when it has conflicts**, **Prompt when
changes are requested**, and **Prompt when you're asked to review again**, where **Same as pull
requests** leaves that situation the pull request's prompt. The form above has the same four, each
**Inherit** naming what it inherits: the org's prompt for the situation, else the app's, else *same
as pull requests*.

A default that no longer makes sense, such as a profile that was removed or an agent this
version doesn't know, is skipped with a warning in the launch menu. The outside-contributor rule
applies above everything.

The form's last row, **Autopilot on outside contributors' items** (**Inherit · Off · On (plan
mode only)**), says whether an [Autopilot](autopilot/README.md) routine may run a coding agent on an
[outside contributor](outside-contributors.md)'s item in that org or repository. A repository's
choice wins over its org's, and with neither it is off: there is no app-wide switch, so it is
always a deliberate choice for a place you know, and the form shows what Inherit means there.
On still means plan mode: a routine never lifts the outside-contributor rule. The choice alone
makes a row worth keeping, and backups and the copy beside the database keep it.

Autopilot's agent steps take a routine's own choices where the launch menu's go, so *Inherit* in
a routine means exactly what a click would use, the repository's and the org's launch defaults
included.
