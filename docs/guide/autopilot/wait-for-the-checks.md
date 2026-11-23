# Closing the loop: Wait for the checks

[Docs](../../README.md) › [User guide](../../README.md#user-guide) › [Autopilot](README.md) · Design: [Verify, tries, the loop guard, and profile fallback](../../architecture/autopilot/verify-and-tries.md)

**Wait for the checks** follows an agent's push in the background: it comes after the agent
step (*only if it pushed*, as **+ Add a step** sets it), and its match is *Checking* until the
checks on the run's own commit say how they came out. Nothing is read twice: the checks
follower reads the pull request every round while its checks run (a routine with this step
follows the checks of what its searches return), and Nilbox reads it itself, a point at a time,
only when nothing did for two minutes, or while the push isn't seen yet.

| Verdict | When | Says |
|---|---|---|
| **Fixed** | Every check passes on its commit, once they settled (**Settle for**, 2 minutes) | *Fixed: checks pass on 4f2a9c1.* |
| **Still failing** | A check fails on it | *Still failing: CodeQL.*, naming up to three; on the routine's last try, *Gave up after 2 tries.* too |
| **Unknown** | No verdict within **Give up after** (2 hours), no check ran on it within ten minutes, or its workflow runs wait for a maintainer to approve them | *Unknown: the checks on 4f2a9c1 hadn't finished after 2 hours.* |
| **Overtaken** | Someone else pushed after it, a Dependabot rebase say | *Pushed 4f2a9c1, but someone else pushed after it, so its fix wasn't checked.* |
| **Push rejected** | It committed, but the branch moved while it worked, and it never force-pushes | *Its push was rejected: the branch moved while it worked.* |
| **No push** | It pushed nothing | *No push:* and the agent's own words |

The verdict is the step's and the run's, so the ⚡ chip says it (**⚡ Fixed**, **⚡ Still
failing**, **⚡ Overtaken**, **⚡ Push rejected**, **⚡ No push**, **⚡ Checks unknown**,
**⚡ Checking** while it waits), Activity's line leads with it, the run view's *How it ended*
says it in full (*Fixed: checks pass on 4f2a9c1.*), and the line the review and issue sheets
show under their work line ends with it (*⚡ Heal Dependabot pushed 4f2a9c1 · Fixed*, or
*· Checking* while it waits), opening the run. **Checks after a fix** tells you each verdict on a fix's checks once (fixed,
still failing, unknown, or overtaken).

**Only if** on a later step names how a step before it ended: *it pushed* or *it didn't push*
(the agent step), *the checks passed* or *the checks still fail* (the latest Wait for the
checks). A step whose Only if doesn't hold is skipped, and the next one runs, so *if the fix
passes, merge; if it still fails, tell me* is two steps. An unknown or overtaken verdict holds
neither.

## Tries

A routine's runs on an item count as its tries (**Tries per item**, 2), across occurrences: a fix
that still fails puts a new commit at the head, which the routine fires on again after **Wait
between tries**, until the last try, which says it gave up. A Dependabot rebase is a new occurrence,
never a new count, so a pull request rebased every day that the agent can't fix gets no run every
day. The next occurrence then closes *Gave up after 2 tries.* with **Try again**, which starts the
count over and opens it again; only you can. A run that was interrupted, stopped because its item
closed, or refused for usage counts as no try. While one of a routine's matches on an item works or
is checking, another of its matches there waits for it.

## The loop guard

A **New commits** routine never fires on a commit a routine's own run pushed, unless its trigger
says **Also on commits a routine pushed**. While an agent works on the item, a new head waits to be
told apart until its run ends and records what it pushed; someone else's push then fires it as
before.
