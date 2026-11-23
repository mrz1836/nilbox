# Building a routine

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Schedules, the digest, the script step, and a routine's week](../../architecture/autopilot/schedules.md), [The searches, and the builder's reads of the inbox](../../architecture/autopilot/searches.md), [`internal/autopilot/draft`](../../architecture/autopilot/draft.md), [The Autopilot tab](../../architecture/autopilot/autopilot-tab.md)

The builder is the routine itself, laid out top to bottom in the order it runs, each part edited
in place. **New routine** (or **Start from scratch** on the empty tab) starts one on your
account's pull requests, telling you when their checks fail, named "New routine", in the mode
new routines start in (Settings → Autopilot), shown beside its name; it starts off, and you
switch it on once it is saved. Clicking **New routine** again while you make one keeps what
you have.

- **The sentence** at the top says the routine in one sentence, as the list and the menu bar
  will (*When checks fail on pull requests by Dependabot in your account and acme-org, if a
  failed check's name contains security or codeql, run "Fix dep security" in the background.
  Once per head commit, at most 2 tries per pull request.*). It rewrites itself as you edit;
  while the routine has a problem, the last sentence that read stays.
- **WATCH** is what of your inbox it watches, by why each item is there (**Review requested**,
  **Opened by you**, **Assigned to you**, any of them ticked), and its
  [searches](routine-searches.md), up to five, each built from simple fields: **Pull
  requests**, **Issues**, or both, **by** whom, **in** where, and **More ▸** for who it
  involves, labels in and out, the branches, the state, and qualifiers typed by hand. Who and
  where are tokens, such as **[Dependabot ×]** and **[Your account ×]**: the field suggests the
  authors, bots, and owners your items came from, and takes anything else typed. Enter or a
  comma adds one (never while an input method is composing), the arrows pick a suggestion,
  Backspace in an empty field or a token's **×** takes one away. A name you type is an org
  (`acme-org`), a repository (`acme-org/api`), or as you write it (`user:octo-user`). A search
  of **Issues** offers only what an issue has, and switching one to Issues drops the branches,
  the state, and *Asking for your review*. Under the block, **the cost line** says what the
  searches add to each refresh, *About 1 point a refresh (0.2% of the budget)*, from the last
  test, and says so once the searches changed since.
- **WHEN** is its one trigger, with the trigger's own options at their defaults until you set
  them: wait until every check has finished, how long they settle, the first commit too, how
  many comments, how many quiet days, which label, or a schedule. **On a schedule** fires at a
  time of day by your Mac's own clock (**Every day**, **Every weekday**, or **Every week** on a
  day), on the minute's tick that reaches it, once a day: a time missed while the Mac slept
  runs once it wakes, the same day, never for a day gone by, and a change of the clock
  (daylight saving, setting it, a new time zone) keeps the time of day and never fires twice
  in one day. It tells you **For each match**, each item's Notify me on its own, or **Once,
  with the list**: one digest that counts what it matched by where each stands, *3 Dependabot
  pull requests need you · 2 failing, 1 waiting for review: acme-org/web #88, …*, its other
  steps still running on each item. A digest needs Notify me as its first step; an item that
  joins the watch later the same day gets a digest of its own.
- **IF** is its conditions, **All** or **Any** of them, each a field (grouped by Item,
  Repository, Pull request, Checks, Files, Dependency update, History), how it compares, and its
  value or values, with **+ Add a condition** and **+ Add a group** (one level deep). A field's
  values suggest what your items hold, such as the check names your pull requests have run,
  so you pick *🔒 Security Scans* rather than guess it; a field of choices takes only its
  choices.
- **THEN** is its steps, numbered, in the order they run, each with its own settings, **Only
  if** on a step before it (*only if it pushed*, *only if the checks passed*), **↑ ↓** to move
  it, and **🗑**. **+ Add a step** offers only what fits at the end: one agent step at most,
  **Wait for the checks** only after an agent step that may push, **Merge it** only after a
  wait on its push, or with no agent step in a routine that fires when **Checks pass**, **Post
  the agent's review** only after a read-only agent step, and only a notice after an agent
  step in your terminal. A Merge it with no agent step has two more ticks, **Merge past the
  rules when an approval is all it lacks** and **Also when it changes workflow, agent, or
  tooling files**, which [Merge it](merge-and-post-review.md) explains; with
  either, **Save** first asks you to confirm what the routine may do, a box for each.
- **The GitHub step** lists its actions in the order they are sent, each with **↑ ↓** and
  **🗑**, and **+ Add an action** offers what it hasn't and what fits the items: **Ask
  Dependabot to rebase** and **Update branch** for pull requests, **Comment one of my replies**
  (one of your library's comment replies that fits the items; never an approval's, such as
  *LGTM*, or a request for changes', since the step only comments), and **Add or remove
  labels**, with the labels to add and to take off.
- **Run one of my scripts** runs one of your own [Repo scripts](../repo-scripts.md), which it names
  by picking it (its command, and where it shows, on the step's line), in the item's clone,
  in the app, exactly as Repo scripts runs it, wherever the script itself runs. It follows the
  routine's mode, as a GitHub action does, with the routine's tries and runs a day, and takes
  no agent's slot. A script set to **Ask before running** waits for your OK every time it
  would run, on Autopilot too (*"Lint" asks before each run: run it?*), and your OK is for the
  script as you saw it: one you edited since asks again. A script that would bump a version or
  upgrade the dependencies, which Repo scripts points to Release or Deps, fails its step: a
  routine never runs one anyway. A script that doesn't show in the
  item's clone, or that you deleted, fails its step and says so; with no clone, or Scripts
  turned off, the match waits. A script you renamed since you picked it waits for you to pick
  it again (*The script picked as "Lint" is now "Lint all": pick it again in the routine.*),
  so a routine from another Mac, or from a backup, never runs another script that happens to
  have the same ID here. **Stop** stops it, as in Scripts → Runs.
- **The agent step** names its **Prompt** (a library prompt for the items' kind, or the item's
  own default prompt; **Edit in Prompts** opens the Prompts tab), **Where** it runs (**In the
  background**, which a new agent step starts with, or **In my terminal**, exactly a click's
  launch, which suits Ask first), and its launch on one line, *As a click would: Claude ·
  default · its default model · default effort*. **Change ▸** opens the agent, profile, model,
  and effort, each on **Inherit** until set, saying what it inherits at the app's level (a
  click takes each repository's and org's own launch defaults first), and the **Fallbacks**,
  up to three profiles to use when one is out of usage. **+ Per org** sets them for one org or
  repository at a time. Changing the agent drops a model and a profile, which are one agent's
  own, and an effort the new agent doesn't take. An agent left on **Inherit** follows the
  [launch defaults](../launch-defaults.md): switch a repository's from Claude Code to Codex and its
  routines' runs there switch with it, while each run already made keeps the agent it ran
  with. In the background, plan mode is set by Tools and the worktree is always the item's
  own, and the **Fence** holds the run in: its budget in dollars or tokens (default $3.00, or
  in tokens from 10,000 to 50 million), its time limit (default 30 minutes, 5 to 180), and its
  **Tools** preset, offered only as the step's agent keeps it (Codex keeps every preset but
  **GitHub tasks only**, and the builder says why), with extra commands to allow or deny. Each
  value set here has **Reset**.
- **LIMITS** are once per what (the trigger's by default), the tries per item (2), the runs a
  day (10), the wait between tries on one item (30 minutes), its **Active hours** (always, by
  default), and its own **Daily budget**, in dollars or tokens (off), each at its default until
  set; Settings → Autopilot's limits and daily cap still hold over every routine together.

## Problems and warnings

What keeps a routine from being saved shows where it is, in red: a search with no scope, a regex
that doesn't compile, a prompt your library doesn't have or that isn't for the items' kind, a
trigger that doesn't fit what it watches, a second agent step. The footer counts them (*Fix 2
problems to save.*). Warnings, in amber, never block saving: a search that matches more than 100
items checks only the newest 100; most of what it watches isn't in your inbox, so a snooze or a pin
won't change what you see; an owner none of your items came from, since GitHub answers a name it
doesn't know with nothing; and, on Autopilot, a terminal run or the profile's own permissions. As
you edit, the last reading stays until the next arrives, so nothing blinks out. A problem at a place
no part shows, as an imported routine can hold, is listed at the top.

## Saving

**Save** (or ⌘S, with Caps Lock on too, and never while a dialog or a sheet is open) stores the
routine once nothing keeps it; **Discard** puts back what was saved, and why a save failed shows in
the footer. Saving a change to what a routine that is on watches, fires on, checks, or fires once
per asks the [question](README.md) first, *3 pull requests match right now.* **Run on them too** ·
**Only what changes from now on**, and asks nothing only when nothing matches, nothing waits to be
told, and everything it watches was read; otherwise it asks all the same (*2 items may match right
now.*, *Not everything it watches could be read right now.*). Leaving a routine with unsaved changes
for another, Activity, or a new one asks first; your draft survives switching tabs.
