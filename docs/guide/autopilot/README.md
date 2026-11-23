# Autopilot

[Docs](../../README.md) › [User guide](../../README.md#user-guide) · Design: [`internal/autopilot`](../../architecture/autopilot/README.md), [The Autopilot tab](../../architecture/autopilot/autopilot-tab.md), [What runs next (`upcoming.go`)](../../architecture/autopilot/upcoming.md)

**In this folder:**

- [Building a routine](building-a-routine.md)
- [Testing a routine](testing-a-routine.md)
- [Templates and quick setup](templates-and-quick-setup.md)
- [Describe it](describe-it.md)
- [Use Nilbox in your own Claude Code](your-own-claude-code.md)
- [Export and import](export-and-import.md)
- [Agents in your terminal](agents-in-your-terminal.md)
- [Agents in the background](agents-in-the-background.md)
- [Closing the loop: Wait for the checks](wait-for-the-checks.md)
- [Merge it and Post the agent's review](merge-and-post-review.md)
- [Needs you](needs-you.md)
- [Snooze, dismiss, and pin: by a routine](snooze-dismiss-pin-by-a-routine.md)
- [What Autopilot may change on GitHub](changes-on-github.md)
- [Fences: what a background agent may do](fences.md)
- [Autopilot's notifications and the menu bar](notifications-and-menu-bar.md)
- [Settings → Autopilot](settings.md)
- [Budgets and caps](budgets-and-caps.md)
- [This Mac](this-mac.md)
- [What Autopilot never does](never-does.md)
- [Routine searches](routine-searches.md)
- [When a routine misbehaves](when-a-routine-misbehaves.md)

Autopilot watches your pull requests and issues and acts when something happens: a check
fails, a reviewer asks for changes, a bug comes in. Its routines can tell you, tidy your inbox,
or start a coding agent, always within limits you set.

A routine watches some items (your inbox's lists, or a [search](routine-searches.md) of its own),
waits for something to happen to one of them (checks pass, checks fail, it appears, ...), checks
its conditions, and then takes its steps. A routine's steps can pin, snooze, dismiss, or
restore the item ([by a routine](snooze-dismiss-pin-by-a-routine.md)), tell you about it
(**Notify me** and **Tell me**), send the few changes to GitHub the review and issue sheets
already send ([what Autopilot may change on GitHub](changes-on-github.md)),
start a coding agent on it in your terminal ([agents in your terminal](agents-in-your-terminal.md))
or in the background ([agents in the background](agents-in-the-background.md)), wait for the
checks on what that agent pushed ([closing the loop](wait-for-the-checks.md)),
and then, if you add the step, merge the fix or post the agent's review
([merge it and post the agent's review](merge-and-post-review.md)). Each routine
acts once per occurrence (once per head commit, say), across refreshes and restarts, and in
its mode:

| Mode | On a match |
|---|---|
| **Dry run** | Records what it would have done, and does nothing else |
| **Ask first** (where new routines start, unless you change it in [Settings](settings.md)) | Runs its local steps (pins, snoozes, notices) at once, and waits for your OK before anything else |
| **Autopilot** | Runs every step at once, within your limits |

Nothing changes a routine's mode but you. One copy of Nilbox on a data folder runs Autopilot at
a time; another copy shows what it does and starts nothing.

## Autopilot (beta)

Autopilot is in beta, so it is off on every install, an existing one included, until you turn it on
in **Settings → Labs**: **Autopilot (beta)**, *Routines that triage and fix pull requests for you.
Still being built.* While it is off, Nilbox shows none of it and runs none of it:

- the window has no Autopilot tab (a window on it goes to the inbox), no Autopilot section in
  Settings, no Autopilot banner switches in Settings → Notifications, no Autopilot type among
  the Notifications page's filters, no ⚡ chip or *by ⚡ routine* mark on a row, no
  **Autopilot ▸** in an item's ⋯ menu, and no Autopilot choice in the launch defaults (a
  stored one is kept); a notification that would open the Autopilot tab says *Autopilot is off.
  Turn it on in Settings → Labs to see this.* instead;
- the menu bar has no Autopilot group, and closing the window counts no routine: it quits as
  it would without Autopilot (with the menu bar icon showing, it hides, as before);
- the engine runs nothing: it takes no lock in the data folder, sends GitHub none of the
  routines' searches, has the checks follower follow none of their items, and refuses every
  action on its history, as a routine's **Test** does.

Your routines and their history stay as they are, each routine's own switch included. Turning
Autopilot on brings everything back as it was, marks its tab and its Settings heading
**Beta**, and starts it as a start of the app does: it reads the routines' searches at once,
and acts only on what they say then, asking first, as after a long gap, when it was off longer
than **Ask before catching up after** says. Turning it off starts nothing new from then on and
stops nothing: a step already handed over finishes, and the lock is let go once nothing it
started is still going. A backup carries the switch with the other settings.

## Start here

A routine earns your trust one mode at a time:

1. Turn on **Settings → Labs → Autopilot (beta)**, then open the **Autopilot** tab.
2. Pick a template, *Suggested for you* first ([templates](templates-and-quick-setup.md)),
   **Start from scratch** in the [builder](building-a-routine.md), or tell
   [Describe it](describe-it.md) what you want, and apply what Claude Code proposes.
3. Fill in its blanks, click **Test** to see what it matches on your items now and why
   ([testing a routine](testing-a-routine.md)), then **Save**. It is saved switched off.
4. Switch it on in **Dry run**: Activity lists each *Would run*, and nothing else happens.
5. Move it to **Ask first**: each match that would start an agent or send GitHub something
   waits under [Needs you](needs-you.md) for your **Run**.
6. Once its runs come out as you want, put it on **Autopilot**, within the
   [limits](settings.md) you set. **Pause Autopilot** stops every new start at once.

Coding agents run [in your terminal](agents-in-your-terminal.md), as a click does, or [in the
background](agents-in-the-background.md), headless, inside a [fence](fences.md). Either way they run
on the profile's own sign-in (a subscription such as Claude Max, through `claude auth login`), never
on an API key from your shell unless you turn on its agent's **Use an API key for Claude Code when
one is set** or **Use an API key for Codex when one is set** ([API
keys](../launching-a-session.md)). What a run costs is counted in tokens, the real measure; its
dollars are what the agent reports, an estimate of the API's price when you are on a subscription
([budgets and caps](budgets-and-caps.md)).

## The tab

**Autopilot** sits after **Notifications**, while Autopilot (beta) is on, marked **Beta**. Its count
is the agents working in the background (a run in your terminal is your session from its start),
with an amber dot while something waits for you and a red dot while a routine is broken or its
search fails; hover it for what they mean (*2 agents working · 1 needs you · 1 routine can’t run*).
A small accent dot before its name says Autopilot runs: a soft, steady glow while it is on, a
routine is switched on, and nothing pauses it, breathing while an agent works (still, with reduced
motion), and none while it runs nothing (paused, no routine on, another copy running them, or no one
signed in). While you have no routine, the tab explains Autopilot in the two sentences above, asks
*Tell Autopilot what you want* ([Describe it](describe-it.md)), then offers the
[templates](templates-and-quick-setup.md) that fit what Nilbox already knows (the first few,
*Suggested for you* first), **Start from scratch**, **Start from an item** (which says to use an
item's **⋯** menu, with **Show the inbox**), **Every template**, and **Import routines…**. Otherwise
it lists your routines on the left, under **Activity**, then **Describe it**, **New routine**, and
**Templates**, with **Import routines…**, **Export every routine…**, and **Autopilot settings** at
the bottom, and shows Activity, the routine you chose, Describe it, the gallery or a template's
quick setup, or the routine you are making on the right. **Pause Autopilot** sits in the header:
while paused, nothing new starts, a snooze or a pin included, and the button reads **Resume
Autopilot**.

## The routines

Each row shows the routine's name, a status line, and its switch. The status line says its mode and
what it is doing or did: *Autopilot · 2 waiting*, *Autopilot · 6 today*, *Dry run · 3 would run
today*, *Ask first · ran 2h ago*, *Dry run · would have run 2h ago*, *Autopilot · off*, counted from
the whole history, whatever Activity has loaded. *Today* turns over at midnight, and *ran* is when
it last ran, even while something newer waits; a routine's first look, which only notes what already
matched, counts in none of them. Or, instead, it says why the routine can't run: its last error, a
search GitHub refuses, or *Made by a newer Nilbox: update to edit it*, which has no switch. A
routine that belongs to another GitHub account than the one signed in is greyed, says *Belongs to
@other-login*, has no switch, and doesn't run. Its **⋯** menu holds:

- **Change with Claude…**, which opens [Describe it](describe-it.md) with the routine attached to
  your next message, as the button beside the routine's name does (off for another account's
  routine, or one this version can't read);
- **Duplicate**, a copy right after it, switched off, named "<name> copy";
- **Export…**, which saves it to a file ([export and import](export-and-import.md));
- **Dry run**, or **Leave Dry run**, which puts it back in Ask first (a routine with steps this
  version can only try in Dry run stays there);
- **Delete…**, which asks first. The routine stops and leaves the list; what it did stays in
  Activity, under its name, and the snoozes, pins, and dismissals it made stay as they are.

## Turning one on

Switching a routine on, or taking one that is on out of Dry run, first counts the items it matches
right now that it has no record of, reading any search of its own from GitHub unless it was read in
the last minute. When there are some, it asks: *3 pull requests match right now.* **Run on them
too**, or **Only what changes from now on**, which records them as already handled. It asks too when
some can't be told yet (*2 items may match right now.*), or when not everything it watches could be
read (the API budget low, a search GitHub didn't answer or that matched more than a page), saying
more may match than it counts. Only when there are none, and it read everything, does it ask
nothing. Escape asks nothing more and changes nothing. A change that can't be made says why in its
own words, as *Autopilot is running in another copy of Nilbox; make this change there.*

## Put it on Autopilot?

A routine in Ask first whose last five runs you approved all finished cleanly (every step done, its
agent pushed, its checks passed after) offers to go on Autopilot: its row says *Put it on
Autopilot?*, and the routine shows why, with **Put it on Autopilot** and **Not now**. It is a
suggestion alone: nothing ever changes a routine's mode but you. A run you skipped, or one that
didn't finish cleanly, starts the count again, and **Not now** waits for five more.

## A routine

Choosing a routine shows its switch, its mode (**Dry run · Ask first · Autopilot**, which asks the
question above when it leaves Dry run while on), while it is on when Autopilot runs it (as Activity
says it, below), and the routine itself, in the [builder](building-a-routine.md), with
[Test](testing-a-routine.md) below it. A routine that belongs to another account, or that a newer
Nilbox made, shows only the sentence it reads as. Below them:

- **Watching** lists the items its **Watch** finds now that its **If** counts, as the last
  refresh read them, newest activity first: an item its conditions rule out isn't listed
  (**Test** shows it, and why), while one that only waits on its **When**, such as a pull
  request whose checks still run, is. A condition Nilbox can't decide without reading GitHub,
  such as a check's name, doesn't rule an item out. The rows are the inbox's own rows: their
  signals, ⚡ chip, triage buttons, **⋯** menu, and launch button. Click a title to open its
  review or issue sheet, which steps through the list. This is where an item that is in no inbox list shows up, such as a Dependabot pull
  request a routine's search finds.
- **Activity** is that routine's own lines, as below.
- **This week** counts what its runs did in the last seven days: its agents' runs, the fixes
  among them (a push whose checks then passed), merges, reviews posted, GitHub actions GitHub
  took, and its scripts' runs, then what they spent, *about $1.20, as 2 runs estimated it ·
  1.2M tokens, 380K of them read from the cache*. The dollars are always *about*: an agent
  signed in with a subscription (Claude Max, say) reports what the same work would cost
  through the API, an estimate, and one with an API key what it was charged (*as 3 runs
  reported it, 2 by estimate* for both), while the tokens are what each run's stream
  reported, the real measure. A run in your terminal reports neither; the item's
  [session panel](../following-a-session.md) counts its session's tokens.

## When it runs

There is no schedule to set. Autopilot learns of what it acts on when Nilbox reads GitHub: your
inbox and your routines' searches every 5 minutes, and the checks still running on your pull
requests every 45 seconds (**Settings → Refreshing** sets both). Right after each read, every
minute, and at once when you change or switch a routine on, it checks every routine that is on, and
one acts as soon as what it waits for holds. Below Activity's title, a line says so as it goes: *●
Running · last check 20s ago · reads GitHub again in 3m*, with **Refresh now**, which reads the
inbox from GitHub at once, as the inbox's **Refresh** does; under it, how often it reads each, in
your settings' numbers. A routine's pane shows the first line while the routine is on. It says
*Starting* until the first read since Nilbox opened is in, and that no routine is on while none is;
the banners say what stops everything else.

**Activity** lists what the routines did, under a heading for each day, newest first:

| Line | Says | Can |
|---|---|---|
| A routine's local steps on a day, grouped | *Snoozed 6*, the items, the routine | **Undo**, which puts back each item that is still as the routine left it; one you changed since stays as you left it (*Put back 5 items. 1 changed since, so it's left as it is.*). Once all are taken back it says so |
| A Dry run's record | *Would run*, the item | **Open** |
| A notice | *Told you*, the item | **Open** |
| A fix whose push was checked | Its verdict, *Fixed: checks pass on 4f2a9c1.* ([closing the loop](wait-for-the-checks.md)) | **Open** |
| A routine that gave up | *Gave up*, *Gave up after 2 tries.* | **Try again**, which resets its tries on the item |
| A routine that stopped partway | Why it closed (*Closed: its step isn't offered*), the steps it ran (*Commented on #12*), and the one it couldn't send, with why | **Open** |
| An item a routine leaves alone | *Left alone*, why (*You brought back what it snoozed.*) | **Let it act again** |

**Filter** by routine (**Every routine**, or one) and by result (**Snoozes, pins, and
dismissals**, **Dry runs**, **Notices**, or **Worth knowing**: what was left alone, what gave
up, what failed, and what stopped partway). Earlier days load as you scroll to the end, or with **Show earlier days**.
**Open** shows the item, even when its routine is deleted: its card in the inbox while one of
your lists holds it, else its row in the Watching list of a routine that still lists it (the
line's own first), marked once, else its review or issue sheet while Nilbox still has it, else
its page on GitHub. A deleted routine's lines keep its name. Across the top, a banner says when
something stops everything: *Autopilot is paused. Nothing new starts.* with **Resume**;
*Autopilot is running in another copy of Nilbox. This one shows them and starts nothing.*; and
that no one is signed in to GitHub, so nothing starts.

## The ⚡ chip

An item a routine touched carries a chip after its session pill, on its inbox card and its Watching
row alike, saying what the newest of the routines' matches on it did: **⚡ Snoozed**, **⚡ Pinned**,
**⚡ Dismissed**, **⚡ Restored**, **⚡ Undone**, **⚡ Taken back**, **⚡ Would run**, **⚡ Told you**, or
**⚡ Queued** while Pause or a routine's active hours hold a step; what it last sent GitHub (**⚡
Asked rebase**, **⚡ Updated branch**, **⚡ Commented**, **⚡ Labeled**, **⚡ Marked done**); what the
checks on its agent's push came to (**⚡ Fixed**, **⚡ Still failing**, **⚡ Overtaken**, and the rest
of [the verdicts](wait-for-the-checks.md)); what a step after the run sent (**⚡ Merged**, **⚡
Auto-merge on**, **⚡ Approved**, **⚡ Changes requested**, **⚡ Reviewed**, from [Merge it and Post
the agent's review](merge-and-post-review.md)); one that waits or works says so (**⚡ Needs you**,
**⚡ Working**, **⚡ Checking**), and one that ended without acting says why, each reason in words of
its own (**⚡ Gave up**, **⚡ You skipped it**, **⚡ Skipped: an outside contributor's**, **⚡ Left
alone**). Click it for the item's history with the routines: each match, newest first, with its
routine, when, what it said, and each change it made with where it stands (*Still there*, *Changed
since*, *Undone*, *You took it back*), an **Undo** for one still there, and **Show in Activity** for
a match Activity lists, which opens its card in [Needs you](needs-you.md) while it waits.

## What runs next

Before a routine runs on an item, the chip says it will, dashed, in the accent's color, as something
still to come: **⚡ Runs when its checks pass** on your pull request whose checks still run, **⚡ Runs
in 2m** while they settle, counting down, **⚡ Runs in 3h** for a schedule due later today, counting
down too. It shows only when the routine will run by itself, with nothing more from anyone: it is on
and not in Dry run, its **If** counts the item for sure as the item will stand when its **When**
holds (*the checks' state is passing* counts a pull request whose checks still run, for a routine
that fires when they pass), its **When** is on its way (checks still running, or passed and
settling; checks that failed, waiting for the rest; a schedule due today; a quiet spell due within a
day), and it hasn't already run on that occurrence (a commit it ran on whose checks run again shows
nothing). A **When** that waits on something no one can foresee (a comment, a label, an approval, a
new commit) never shows, so a routine never marks everything it watches. While Autopilot is paused,
off, or run by another copy of Nilbox, no chip says so. What goes on now comes first: a match that
waits for you or works shows its own chip, and once the routine ran, the chip says what it did.
Click it for **Up next**: each routine that will run, how (*Runs by itself*, or *Asks you first*
when it is in Ask first with a step that asks, an agent's or one on GitHub, since local steps run by
themselves either way), when, and what it will do (*Then: Comment one of my replies, then Merge
it*), with **Open routine** and **Leave this one alone**, which tells it never to run on this item,
the line staying with **Undo**, and Activity's *Left alone* line letting it act again later. The
item's history follows below. It costs nothing: Autopilot notes what will run as it checks the
routines each minute, and the inbox reads it from memory.

## While a routine acts on an item, launching waits

Once a routine's match on an item is **⚡ Queued**, about to act, or works or checks its work, the
item's agent button and its **▾** wait, saying why on hover (*Merge syncs is about to merge this.
Launch an agent here once it has.*), so you don't start work on a pull request about to be merged.
Watching or taking over the routine's own agent is unaffected, and a match that waits for your OK,
or for something you fix, leaves the item to you. In **Needs you**, **Queued** says once, in full,
what every queued match waits on when it is the same for all (the hour's pace, say), and each row
names its pull request.
