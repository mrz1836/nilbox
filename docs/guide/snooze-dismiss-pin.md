# Snooze, dismiss, and pin

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/inbox`](../architecture/inbox.md), [`internal/reporules`](../architecture/reporules.md)

Each list has four tabs: the list itself with its count (**Review requested 14**), then
**Snoozed**, **Dismissed**, and **Muted**, each counted once it holds anything. Snoozing or
dismissing an item moves it out of its list's own tab, so the lists only show what still needs
you. A list with nothing in any of its four tabs hides until something arrives, and comes back
on its own tab.

- **Snooze** sets an item aside for a while. Hover a card and click the clock, which shows the
  length (**3d**), to snooze it for your default length; the arrow beside it offers 1 day, 2
  days, 3 days, 1 week, 2 weeks, or 30 days, each with the time the item comes back. The
  item waits in the **Snoozed** tab, soonest back first, then returns to its list on its own at
  the same time of day, even across a change of the clocks. For a day after that, its card's
  story says **back from snooze**. New activity can bring it back sooner; see below.
- **Dismiss** (the archive box) sets an item aside until you bring it back. It waits in the
  **Dismissed** tab, most recently dismissed first, however much it changes on GitHub.
- **Unsnooze** and **Restore**, on the cards in those tabs, put an item back in its list right
  away.
- **Pin** (the pushpin, at the right end of the card's buttons) keeps an item at the top of its
  list, above everything not pinned. With more than one pinned, the most
  recently pinned comes first; the rest keep their order by last activity. A pinned card
  shows its pin filled, in the accent color, without hovering; click it to unpin, and the item
  goes back to its place. A pin outlasts a snooze or a dismissal: the item waits in its tab,
  pin showing, and comes back pinned, so **Undo** leaves the pin as it was.

Every one of these is also in the card's [menu](item-menu.md), under
**Inbox actions**, and in the header of the [review sheet](reviewing-a-pull-request.md) and the
[issue sheet](reading-an-issue.md), always shown there. A snooze, a dismissal, a restore, or an
unmute from a sheet takes the card out of its tab, so the sheet moves on to the next card in the
list, or the one before it at the end, and closes once none is left; an unmute also moves past
the cards the same rule muted. A toast at the bottom of the window confirms each snooze or
dismissal, with **Undo**, over the sheet while one is open. The default length and the wake rules are under **Settings → Snoozing**, which the
snooze menu's **Snooze settings…** opens. Snoozes, dismissals, and pins are kept in the local database only:
GitHub never hears about them. The **N of M** count of a list leaves out the items set aside.

## One toast at a time

The window shows one toast at a time. One that answers your click shows at once; if another was
showing, that one waits behind it and gets its full time again when it comes back, so an **Undo** is
never lost. One you didn't ask for, such as a worktree cleanup's, an update's, or the Scripts tab's,
waits its turn. While more wait, the toast says which it is, as in **1 of 3**.

## Pinned and muted repositories and orgs

A rule can cover a whole repository, or an org (or a user) and every repository it owns. Set one
from a card's menu, under **Inbox actions → Pin or mute repository**, which offers the card's
repository and its org, or in **Settings → Repositories → Pinned and muted**, which lists every
rule, each org first and then its repositories, with **Pinned** or **Muted** to switch and **✕**
to take it off, above a field that adds one (an org such as `acme-org`, a repository such as
`acme-org/example-repo`, or a link to either on GitHub).

- **Pinned:** the repository's items count as pinned. They come right after the items you pinned
  yourself, newest activity first, and match `is:pinned`. Their cards show a filled pin, marked
  **Pinned with acme-org**, which a card doesn't unpin: the rule does.
- **Muted:** the repository's items wait in each list's **Muted** tab, whether or not they are
  also snoozed or dismissed. They count nowhere: not in the **Inbox** tab, the menu bar, or a
  list's **N of M**, and a new review request or assignment in a muted repository shows no
  banner. The inbox still reads them with every refresh, so they're current when you unmute, but
  nothing follows their running checks or checks their changed files. **Unmute**, on each card in
  the tab, takes the mute off the repository or org that set it.
- **Which rule wins:** a repository's own rule wins over its org's, so you can mute an org and pin
  one of its repositories. A target is pinned or muted, never both.
- **Release:** on **Scripts → Release**, a muted repository is listed in the folded **Muted**
  group, with **Unmute**, and GitHub isn't asked about it; a pinned one comes first in its group.
  Muting a listed repository regroups it without a request. Unmuting one the page never read
  reads that repository alone.

A toast confirms each change, with **Undo**. The rules live in your settings on this Mac;
GitHub never hears about them.

## Waking a snooze early

When you snooze an item, its activity as the last refresh read it is recorded: the head
commit, the number of commits and comments, its reviews, and whether it is a draft. Every
refresh compares the item with that record, and a snooze ends at once when the new activity
reaches any of your rules:

| Rule | Choices | Default |
|---|---|---|
| New commits | **Never**, **Any** (any push), **3+**, or **5+** new commits | Any |
| Reviews | **Never**, **Decisions** (an approval or a request for changes, or the review decision turning to either), or **Any** (also a review that only comments) | Decisions |
| New comments | **Never**, **Any**, **3+**, **5+**, or **10+** new comments | Never |
| Marked ready for review | **Never** or **Any** (a draft pull request taken out of draft) | Any |

**Any** push counts a force-push too, which keeps the commit count but moves the head, except
on a bot's pull request, where a force-push is usually a rebase. Comments include review
comments and bots' comments, since GitHub reports only their count; a higher threshold rides
out bot noise. Counts are measured from the moment you snoozed, so three comments over three
refreshes reach **3+**. A pull request turned into a draft never wakes a snooze, and a draft
snoozed before this rule existed still wakes when it's marked ready.

An item woken early comes back to its lists with its card's story saying what was new, most
decisive first, such as **back early: approved, 2 new commits**, **back early: changes
requested, force-pushed**, or **back early: marked ready for review**. Only the activity that
met a rule is named.
Dismissed items never wake, and a snooze that has ended is not woken again; snoozing an item
again starts a fresh record.
