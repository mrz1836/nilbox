# The item menu and edits on GitHub

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/inbox`](../architecture/inbox.md), [`internal/budget` and `internal/github`](../architecture/budget-and-github.md)

Every card has a **More actions** menu (**⋯**) at the end of its triage buttons, which shows as
you hover the card or move focus into it. It gathers everything you can do with the item:

- **Review…**, on a pull request, opens it in the [review sheet](reviewing-a-pull-request.md), and
  **View issue…**, on an issue, in the [issue sheet](reading-an-issue.md).
- **Open on GitHub** and **Copy link**.
- **Inbox actions**, a submenu with the card's triage buttons: **Refresh from GitHub**, **Pin to
  top** (or **Unpin**), **Mark as read** on an unread card or **Mark as unread** on one read
  (while [unread dots](inbox-cards.md) are on), **Pin or mute repository**, with a submenu that
  pins or mutes the card's repository or its whole org, **Snooze**, with a submenu of lengths,
  and **Dismiss**; on a card in the **Snoozed** or **Dismissed** tab, **Unsnooze** or
  **Restore** instead, and in the **Muted** tab, **Unmute**. The hover buttons stay, so the
  everyday ones are still one click away.
- **Autopilot**, a submenu, while [Autopilot (beta)](autopilot/README.md#autopilot-beta) is on:
  **Run a routine**, with a submenu of your routines that are on and act on the item's kind,
  which runs the one you choose on the item now (greyed while there is none, saying why on
  hover); **Make a routine from this…**, which opens the builder filled in from the item
  ([Templates and quick setup](autopilot/templates-and-quick-setup.md#quick-setup));
  **Describe a routine for this…**, which opens [Describe it](autopilot/describe-it.md#ways-in)
  with the item attached; and **Why didn't a routine run?**, which shows what a routine decides
  about the item now, condition by condition, asking GitHub nothing. Running one and asking
  why are in [Run a routine](autopilot/needs-you.md#run-a-routine).
- **On GitHub**, when they apply: **Nudge @author…** on a pull request waiting on its author
  (see [Nudge](inbox-cards.md)), **Unassign me** on an item assigned to you, and **Remove me as
  reviewer** on a pull request that asks for your review.

The keyboard works as in any Mac menu: the arrows move, → opens a submenu and ← leaves it,
Return chooses, and Escape closes the menu.

## Edits on GitHub

These are the only changes Nilbox makes on GitHub, and only when you choose one. Each is made with
your `gh` token, and GitHub's answer is checked before the card changes. A combined action, such as
**Approve & merge**, makes its changes one at a time and costs the sum of their rows, and so does a
merge that posts your text as a comment first (Merge when ready then reads the merge state before
the comment, 1 GraphQL point more). Nilbox never goes past branch protection on its own (only
**Merge past the rules** does, when you choose it, and an Autopilot routine you
[allow](autopilot/merge-and-post-review.md) on your own signed pull request that lacks only an
approval), and no agent ever approves, merges, or closes: every approval, merge, and close happens
only because you clicked. The one change Nilbox makes on its own is [clearing your GitHub
notifications](github-notifications.md), and only once you turn clearing on: it marks a notification
done, or read, when the work behind it is finished.

| Change | Where | Cost |
|---|---|---|
| Unassign me | The card's menu, or the issue sheet's | 1 REST point |
| Remove me as reviewer | The card's menu | 1 REST point |
| Nudge the author (a comment on the pull request) | A card's **Nudge**, its menu, or its details, after you read the comment | 1 REST point |
| Re-request my review | The review sheet, once its author pushed since you requested changes | 1 REST point |
| Approve (with line comments) | The review sheet | 1 GraphQL point to check nothing was pushed, then 1 REST point |
| Request changes, Comment (with line comments) | The review sheet | 1 REST point |
| Merge | The review sheet, after you confirm | 1–3 GraphQL points to read the merge state, then 1 REST point |
| Merge past the rules | The review sheet's menu, or its button on your own pull request when you turn that on, when GitHub lets you, after you confirm each rule it skips | 1–3 GraphQL points to read the merge state, then 1 REST point |
| Turn on auto-merge | The review sheet, after you confirm | 1 GraphQL point |
| Update branch | The review sheet | 1 REST point |
| Ask Dependabot to rebase (a `@dependabot rebase` comment) | The review sheet | 1 REST point |
| Approve workflows to run | The review sheet's bar or menu, after you confirm when it changes workflow files | 1 REST point per run, then up to 5 GraphQL points to read it again until their checks start (1 when GitHub approves none, taking them only on its site) |
| Close pull request | The review sheet's menu, after you confirm | 1 GraphQL point to check nothing was pushed, then 1 REST point (1 more for your comment) |
| Comment on an issue | The issue sheet | 1 REST point |
| Close an issue as completed, or as not planned | The issue sheet, after you confirm | 1 GraphQL point to check for new comments, then 1 REST point (1 more for your comment) |
| Add or remove labels | The issue sheet's **Labels**, with triage access | 1 GraphQL point to list the repository's labels (kept 10 minutes), then 1 REST point for all you add, and 1 for each you remove |
| Ask someone else or a team to review, or take their request off | **Reviewers** on the review sheet, with triage access | 1 REST point for everyone you ask, then 1 for everyone you take off; the picker reads who works on the repository for 1 GraphQL point at most weekly per repository, and 1 REST point more for an organization's teams |
| Assign or unassign someone else | **Assignees** on the review sheet or the issue sheet, with triage access | 1 REST point for everyone you unassign, then 1 for everyone you assign; the picker reads who can be assigned for 1 GraphQL point at most weekly per repository, and 1 a search only when nothing kept answers it |
| Mark a GitHub notification done, or read | On its own, once you turn on **Clear them automatically** in Settings → GitHub notifications | 1 REST point each, at most 50 a minute and 300 an hour, and 1 more for one that changed after the work was done, to read who commented last |

From the card's menu, once GitHub has taken you off, the item leaves that list (and the inbox,
when it is in no other), and a toast confirms it. When GitHub refuses, or answers without making
the change, as it does for an account that may not edit the item, the card stays as it was and
the toast says why. While an edit runs, the card fades and blurs under a note saying what
GitHub is doing ("Removing you as a reviewer on GitHub…") and takes no clicks, however slow
the network, its **⋯** turns into a spinner, and its other GitHub actions wait for it. A card
the item stays in comes back as it was.

- **Unassign me** takes you, and nobody else, off the item's assignees.
- **Remove me as reviewer** takes back the request for your review by name. A request made only
  to a team you are in can't be taken back for one member, since GitHub removes a team's request
  for every member or for none, so the menu names the team and leaves the item to you: dismiss it
  to set it aside. When a pull request asked both you and a team, you come off it, and the toast
  names the team it still asks.

GitHub's search can trail an edit by a minute or so, so the next refresh may still find the item
where you left it. Nilbox counts it as no longer assigned to you, or no longer asking for your
review, until the item changes on GitHub again: if someone assigns you again, or asks for your
review again, it comes back, with its notification, as usual. An item that no longer asks for
your review moves to **Yours** when you opened it or are assigned to it, and an item you are
no longer assigned to stays there while you opened it, without **assigned** in its byline; with
nothing else keeping it, it leaves the inbox.

## Whose account

An edit goes out only with the token of the account whose inbox you see. If you switched accounts
with `gh auth switch` since the last refresh, Nilbox asks you to refresh first, so it never takes
another account off anything. Checking a token the inbox hasn't used yet costs one GraphQL point;
after that, it is remembered.

## Within your budget

Each edit spends what the table says, which the [API budget gauge](window.md) counts like every
other request. Nilbox also keeps its own pace well under GitHub's limits on writes: at most 60 edits
a minute and 400 an hour, Autopilot's included, and none while the REST budget is down to its last
hundredth (50 of 5,000 points), which it leaves for `gh` and your agents. When GitHub asks it to
slow down, it waits as long as GitHub says. An edit held back for any of these sends nothing, and
the toast says why and when to try again.
