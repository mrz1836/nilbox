# GitHub notifications

[Docs](../README.md) › [User guide](../README.md#user-guide) · Design: [`internal/ghthreads`](../architecture/ghthreads.md), [`internal/threadrules`](../architecture/threadrules.md)

Nilbox keeps a copy of your GitHub inbox, the notifications GitHub keeps for you, read and
unread but not done, read from GitHub every five minutes, or as set in **Settings →
Refreshing**. **Settings → GitHub notifications → Read my GitHub notifications** turns it off; it
is on by default.

## The button

Beside the bell, GitHub's mark counts the unread notifications in your GitHub inbox, up to **99+**,
and shows nothing while none are unread. While GitHub refuses your token the notifications, it shows
an amber dot instead. Click it for the counts and when GitHub was last checked (*5 unread · 38 in
your GitHub inbox · checked 2m ago*), with a refresh button that checks GitHub now and turns while
it does; why they can't be read and when Nilbox asks again, with **Check again**; and the newest 20
unread, grouped by the pull request or issue each is about. Opening the panel reads Nilbox's own
copy: it asks GitHub nothing and marks nothing read. While automatic clearing is off, it offers
**Turn on automatic clearing**. **View all** opens the [Notifications page](notifications-page.md)'s
GitHub tab, which lists every notification, read ones too, and what clearing did. **Open on GitHub**
opens your GitHub inbox, and **Settings** its settings.

## Opening and Done

Click a notification to open it on GitHub, as GitHub's own inbox does, which marks it read there
too: a pull request or an issue on its page, a workflow run on its repository's Actions page, a
release on its Releases page, and anything else on its repository. Point at one, or move the focus
to it, and **Done** takes the place of its time: click it to mark the notification done on GitHub,
which takes it out of your GitHub inbox, whatever it is about and whatever **What clearing does**
says; Nilbox drops it at once. A notification on its way dims, and its **Done** turns until GitHub
has taken it; if GitHub refuses, the panel says why. Each goes through the same paced lane as
automatic clearing. Every link is built from the item's repository and number, or from the
repository's name and a fixed page; Nilbox opens no address GitHub sent.

## The settings

**Settings → GitHub notifications** also says what your GitHub inbox held at the last check, and
when, with **Check again**; that GitHub refuses the token, and when it is asked again; or why the
last check failed. Nilbox reads the newest 1,000, and says so when GitHub holds more. Turning
reading off hides the button at once, and turning reading or clearing on checks GitHub at once.

## Clearing

**Clear them automatically**, off by default and only while reading is on, clears a notification on
GitHub once the work behind it is done. Until you turn it on, Nilbox changes nothing in your GitHub
inbox. **What clearing does** chooses **Mark done**, which takes the notification out of your GitHub
inbox, or **Mark read**, which leaves it there, read, so the button's count stays and only its
accent goes; with **Mark read**, one you already read is left alone.

- **When.** Each moment under **When to clear** is on by default: a pull request merged, or
  closed without merging, and an issue closed, in Nilbox or anywhere else; a review of yours
  that approves or asks for changes (a comment alone doesn't count), in Nilbox or anywhere
  else, as an agent's `gh pr review` or one in your browser, once Nilbox sees it (the read
  after an agent's session stops working, a card's or the review sheet's refresh, or the next
  whole refresh) within two hours of it; an item you
  dismiss in Nilbox; and taking yourself off an item's assignees or reviewers in Nilbox. A
  review made elsewhere counts as your own action, at GitHub's time for it: a notification
  that moved more than a couple of minutes after the review stays, and, as what you do in
  Nilbox, it clears one you get only by watching the repository too. A
  merge or a close made elsewhere counts only on GitHub's own word, read within the last hour,
  so an item reopened stops counting at once. **Include repositories you only watch** clears a
  notification you get only by watching its repository once its item is merged or closed
  elsewhere; what you do in Nilbox clears one either way.
- **After a merge or a close.** GitHub tells of a merge or a close a little after it happens,
  and of what follows it (a bot's comment, a release noted on the pull request), and each brings
  a notification already cleared back to your inbox on GitHub. So for ten minutes after Nilbox
  learns an item merged or closed, or left your lists, as an auto-merge does, it reads your
  notifications again every two minutes and clears the item's notification as soon as it comes
  back, rather than at the next check. A read GitHub answers with "nothing new" costs nothing.
- **Nothing you haven't seen.** A notification that changed more than two minutes after the
  moment and is still unread is held back, since someone may have said something you haven't
  read. With **Count bot activity after it closes as seen**, it clears anyway when everything
  since was a bot's, which Nilbox tells from who wrote its latest comment: 1 REST point, read
  once each time the notification changes, at most 150 a check. An event with no comment on an
  open item, such as a push or a new request for your review, counts as a person's. A person's
  activity holds the notification until you read it on GitHub, and it clears on the next check.
- **Undo.** A notification cleared by something you did in Nilbox waits as long as the toast
  stays, plus 3 seconds (15 seconds at least), so the toast's **Undo** still takes it back:
  undoing a dismissal keeps its notification, which isn't cleared again on its own until it
  changes on GitHub. One cleared by a merge or a close made elsewhere goes as soon as a check
  finds it.
- **How many.** The first check after you turn clearing on, or change what clears, clears at
  most 25 on its own, and any later check at most 150. When more would clear, none is cleared
  on its own: they wait for you to review them.
- **Pace.** Notifications are cleared one at a time, at most one a second, 50 a minute, and 300
  an hour, in a lane of their own beside the [edits](item-menu.md), with
  the same reserve, and a pause GitHub asks for holds both. Just before each is sent, Nilbox
  checks again that clearing is still on, that GitHub still lists the notification as it was
  when it was chosen (one that changed is decided again), and that your `gh` token belongs to
  the account it was chosen for (another account's waits). Each one is recorded, sent or not.
  One GitHub no longer has counts as done; one that fails three times waits until the
  notification changes.
- **Never.** Nilbox never marks your whole inbox or a repository read, never unsubscribes from
  or mutes anything, never touches another account's notifications, and never clears one about
  a release, a discussion, a check suite, or anything else that isn't a pull request or an
  issue. [Mark all done](notifications-page.md#mark-all-done), which only you start, marks each notification done
  with a request of its own, as **Done** does: GitHub has no call that marks many done at once,
  and Nilbox sends none of those that mark many read.
