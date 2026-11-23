# What Autopilot may change on GitHub

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [The GitHub step](../../architecture/autopilot/github-step.md)

A routine's steps change on GitHub only what the review and issue sheets already change for you,
and only these five:

| Step or action | What it sends | How far it can be taken back |
|---|---|---|
| **Ask Dependabot to rebase** | One `@dependabot rebase` comment on a Dependabot pull request behind its base or in conflict, as the review sheet's | Not at all: Dependabot force-pushes its branch |
| **Update branch** | Merges the base branch into the pull request's branch, as the review sheet's | Not at all: the merge commit stays |
| **Comment one of my replies** | One of your comment replies, filled in for the item: on a pull request, as the review sheet's **Comment** (a review that only comments, so on one asking for your review it counts as your review), on an issue, as the issue sheet's | The comment can be deleted, but GitHub has told people by then |
| **Add or remove labels** | The labels added and taken off, as the issue sheet's edit, for pull requests too; only what changes the labels it has | By hand. A label can start a repository's label-triggered workflows |
| **Mark the GitHub notification done** | Clears the item's GitHub notifications, as **Done** on the Notifications page does, by your [rules](../github-notifications.md) (done or read) | On GitHub, where a notification marked done can be moved back |

Marking a notification done changes only your own notifications, so it runs at once with the
local steps, in any mode. The four actions follow the routine's mode: in Ask first, the first
one waits for your OK (**Send it**), and the rest go once you've said yes. Each one:

- **goes through the sheet's own service:** the item's sheet reads it from GitHub right before
  the action (one point), the routine is decided again on what that read found, and the sheet
  must still offer the action. One it no longer offers (*It's up to date with main, so
  there's nothing to rebase.*) closes the match, with the sheet's reason. One it offers with
  warnings, such as updating a Dependabot branch, waits for you with the warnings, and **Send
  anyway** accepts exactly those; a routine never accepts a warning itself.
- **is pinned to the head that read found:** a push in between sends nothing, counts as no
  try, and the action is read and decided again first.
- **is a run of its own,** recorded before it is sent, so a quit never sends one twice: one the
  app quit while sending waits for you, worded so you check the item on GitHub first (**Send
  it again**). A step's actions together are one try.
- **is paced below you:** at most **GitHub actions an hour** (Settings → Autopilot, 120 by
  default, up to 300) and a sixth of that a minute, under what the app sends GitHub (400 an
  hour, itself under GitHub's 500 for writes), so your own clicks always have room, and at most
  **GitHub actions a day**. When GitHub, or the app's own pace, asks to wait, the actions wait until then,
  as **⚡ Queued**.
- **waits for your OK on an outside contributor's item,** in every mode, since what it says is
  visible to them.

The first time a routine with a GitHub action is switched on, leaves Dry run, or changes, it queues
at most 25 of what it matches; past that it queues none, and they wait for you as a backlog
(**Matched at once**, in [Needs you](needs-you.md)), whose **Run on them** sends them all at the
pace above. A GitHub action never merges, approves, requests changes, closes, or approves workflow
runs, and a test fails the build if one could reach any of them: merging and posting a review are
steps of their own, each opt-in and behind its own guards ([Merge it and Post the agent's
review](merge-and-post-review.md)), which never close anything or approve workflow runs, and merge
past GitHub's rules only your own signed pull request that lacks only an approval from someone else,
as a routine you confirmed allows it while **Let routines merge your own pull requests past the
rules** is on. A reply that approves (*LGTM*, *Update OK*) or requests changes is refused where the
routine is built and again right before each action; one your library changed since holds the action
back (under **Can't start yet** in Needs you) and says why, reading nothing from GitHub.
