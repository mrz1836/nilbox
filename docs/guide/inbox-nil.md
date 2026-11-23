# Inbox nil

When every list is empty, the inbox throws a party in their place, above any list that still
holds what you snoozed, dismissed, or muted: **inbox == nil**, a chip that says what the moment
was worth, a one-liner, and a terminal that checks off each list, counts what you snoozed and
dismissed (with when the first snooze comes back), keeps your streak (`streak … 4 // days
running`, the `best` to beat, and `nil … 37 // and counting`), and panics, having nothing left
to do.

**Milestones.** The app keeps a small log of the days the inbox reached nil, on your Mac, in its
database (never in a backup; a reset clears it). Every day at nil, whether you emptied the inbox
or opened it to find it empty, carries your **streak** on; a day without one starts it again.
Emptying it while you watch, by triaging the last item or by a refresh that finds nothing left,
counts as a nil of yours, and can unlock an achievement, each once: a **3-, 7-, 14-, or 30-day
streak** and every 30 days after, your **10th, 25th, 50th, and 100th nil** and every 100th after,
your **First nil**, a **Clean sweep** of 10 or more items in one sitting, **Early bird** (5:00
to noon), **Friday finish**, **Weekend warrior**, and **Night owl** (22:00 to 5:00). A streak reached
on a day you only found the inbox empty unlocks with the next nil you earn. The chip shows what
unlocked (*Achievement unlocked · 7-day streak*, with *+1 more* if several did), or, for an
ordinary nil, its number (*Nil #37*) or the day of your streak (*Day 4 at nil*). The one-liner
fits the moment: the first nil you earn gets *Inbox zero is for amateurs. You hit nil.*, and
later ones turn through their own quips, or speak to a sweep, a streak, a Friday, a weekend, the
hour before standup, or the dark.

**Confetti.** Confetti flies from the **nil** on its own only when a nil unlocks an achievement,
and then at most once a day: the day it flew is in the log, so restarting the app doesn't throw
it again. Every other party plays one small flourish instead, never the same one twice in a row:
a glint sweeping across it, the mark hopping and turning, a hard RGB-split burst on **nil**, a
few code glyphs drifting up out of the mark, or the chip landing like a rubber stamp; and **go
run ./party** gives a little wiggle, twice. It runs the party again whenever you like, with more
confetti and a new one-liner each time.

**Celebrations** in Settings → Appearance sets how much of this you get. **Milestones**, the
default, is all of the above. **Quiet** keeps the chips and the flourishes, and throws confetti
only when you run the party again. **Off** shows the inbox at nil as a still strip, the mark, the
heading, and the one-liner, with no chip, flourish, or button; the log still keeps your streak.
Quiet and Off also skip the bursts of confetti that end the [first-run setup](first-run-setup.md)
and greet a [release on GitHub](release.md).

The party needs a successful refresh: an inbox that was never refreshed, or whose last refresh
failed, is unknown rather than empty. A list whose **N of M** count shows more on GitHub than
was fetched is not empty either. A list that holds something set aside stays below the party,
so its **Snoozed** and **Dismissed** tabs are a click away, and an **Undo** that brings an item
back ends it.

Above such a list, the party is a strip: **inbox == nil**, the chip, a one-liner, and **go run
./party**, with the lists in view below, whether you emptied the inbox or found it empty. **go
run ./party** in the strip runs the party in full. Turn a list to its **Snoozed**,
**Dismissed**, or **Muted** tab while the full party shows and it folds into the strip again,
the list staying where it was under the pointer; the chevron in the party's corner folds it too.
It stays the way you left it until you leave the inbox for another page and come back. With
nothing set aside, there's no list to make room for, and the party fills the page, unless
Celebrations is **Off**, which always shows the strip. With **Reduce motion** on (System Settings
→ Accessibility → Display), everything holds still and no confetti flies.
