# The Notifications page

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/event` and `internal/desktop`](../architecture/event-and-desktop.md), [`internal/ghthreads`](../architecture/ghthreads.md), [`internal/threadrules`](../architecture/threadrules.md)

The **Notifications** tab, beside **Inbox**, holds everything the bell and the GitHub button
are quick views of, on two tabs: **Nilbox**, the app's own notifications, and **GitHub**, your
[GitHub notifications](github-notifications.md). Each tab counts its unread. The page opens on
the tab you chose last, unless a quick view's **View all** asks for the other; the choice is
the window's memory, which a [backup](backing-up.md) never holds.

Each tab lists what it holds newest first, under a heading for each day (**Today**,
**Yesterday**, the weekday within the week, then the date), which stays at the top as its day
scrolls past. An unread entry carries a dot and a bolder title. **Show** picks **Unread** or
**All** (the default), and **Type** what kind; the toolbar's field [filters](filtering.md) the
list too. Long lists stay quick: only the rows near the screen are drawn.

## Nilbox

The whole history, up to the latest 1,000, in the local database (the `notifications` table),
whether or not a banner was shown. **Type** is **Reviews**, **Assigned**, **Snoozes**, **Releases**,
**Runs**, **Status**, **Updates**, or **Autopilot** (its five kinds together, offered while
Autopilot (beta) is on). Click an entry to open it as the bell does, which marks it read. Point at
one: an unread entry offers **✓ Done**, and a read one **Mark unread**, which puts it back on the
bell. **Mark all done** marks them all read, and **Clear history** forgets every notification, after
asking; banners and settings stay.

## GitHub

Every notification Nilbox keeps a copy of, read and unread, grouped by the pull request or issue
each is about, with the counts and when GitHub was last checked, a refresh button that checks it
now, **Open on GitHub**, and **Settings**. It says why GitHub refuses the token, why the last check
failed, and when GitHub holds more than the newest 1,000. **Type** is **Pull requests**, **Issues**,
**Releases**, **Actions** (workflow runs), or **Other**. A line whose item is merged or closed says
so. Click a line to open it on GitHub, which marks its unread notifications read there; point at it
for **Show in inbox** (while the inbox lists its item), **Mark read**, and **Done**. Each goes to
GitHub at once, one line at a time, through the same paced lane as clearing; [Mark all
done](#mark-all-done) clears a backlog. While Nilbox doesn't read your GitHub notifications, the tab
says so and offers **Turn on**, which checks GitHub straight after.

## Mark all done

GitHub's API marks notifications done one at a time, so a big backlog takes a while. **Mark
all done…**, at the right of the GitHub tab's toolbar and in the note that says GitHub holds more
than the newest 1,000, marks them done for you in the background, as GitHub's own **Mark as
done** does: each leaves your GitHub inbox, and one with new activity comes back on its own.
It's offered while Nilbox reads your notifications, GitHub lets the token read them, your
GitHub inbox isn't empty, and no bulk mark is running.

- **What.** The dialog opens at once, counting from the list the tab already holds. **Older
  than** picks **Any age** (the default), **1 day**, **1 week**, **1 month**, or **3 months**,
  and every repository is counted again at that age, the noisiest first (*412 notifications ·
  37 unread*); one with none that old leaves the list, and its box is remembered. Every
  repository starts ticked, or, when the toolbar's field names repositories (`repo:`), only
  those, so filtering to one repository and clicking does what GitHub's per-repository button
  does. **Select all** and **Select none** tick what's listed. With every repository ticked,
  everything that old is marked, notifications about no repository and those past the newest
  1,000 included; otherwise those of the repositories ticked, past the newest 1,000 too, which
  is why the final count may be higher. Anything updated after you click, or within the age
  you chose, stays.
- **How long.** It marks one notification every 1.5 seconds, at most 40 a minute and 1,200 an
  hour, in a lane of its own, so clicks and automatic clearing keep theirs: 1,000 take about 25
  minutes. The dialog says how long before you confirm. Each mark is one REST request, and so is
  each page of 50 it reads; a page it reads stands for a minute, and a notification Nilbox sees
  updated since you clicked is left alone.
- **While it runs.** A band under the tab's header says how far it got (*Marking done on GitHub:
  412 of 1,180 · about 19 minutes left*), with a bar, and why it waits, if it does: GitHub asked
  for a break, the API budget is down to its reserve until it refills, your `gh` token is for
  another account, reading is off, or GitHub isn't answering (it tries again a minute on). The
  list loses the marked notifications every few seconds. The toolbar's GitHub button shows the
  same line at the top of its panel, from any page. **Stop** stops at once: what was marked
  stays marked, the rest stay in your inbox. Quit while it runs, and it carries on where it
  left off when Nilbox opens again, at the same pace. One that fails three times is left in
  your inbox and counted; after 200 such, it stops.
- **When it ends.** A toast says so, from any page (*Marked 1,180 notifications done on
  GitHub*), with how many couldn't be marked, and the band says the same, with **GitHub's Done
  view**, until you dismiss it (**×**). There is no undo in Nilbox: GitHub's Done view moves any
  back to your inbox.

### Cleared

**Show → Cleared** shows what clearing did. **Clearing soon** lists each clear waiting to be sent,
why it clears, and when it goes (*in 12s*), with **Undo**, which keeps the notification. **Recently
cleared** lists the last 50 that settled and how: **Marked done**, **Marked read**, **Kept**,
**Couldn't clear**, or **Skipped**, with why; a click opens the item on GitHub. **GitHub's Done
view** opens GitHub's own list of what was marked done. While automatic clearing is off, it offers
to turn it on.
