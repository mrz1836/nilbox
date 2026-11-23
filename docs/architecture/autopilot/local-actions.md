# Local actions: by a routine, Undo, and hands off

[Docs](../../README.md) › [Architecture](../../README.md#architecture) › [`internal/autopilot`](README.md) · Code: [`internal/autopilot`](../../../internal/autopilot) · Guide: [Snooze, dismiss, and pin: by a routine](../../guide/autopilot/snooze-dismiss-pin-by-a-routine.md)

## The effects (`effects.go`)

The triage dispatcher (`triageDispatcher`) runs a match's Pin, Snooze (its `days`, else three),
Dismiss, or Restore through `Triage.Apply`, the inbox's own triage for the routine
(`inbox.Routines.Triage`, see `internal/inbox`'s **A routine's triage**), whose record writes the
`autopilot_effects` row in the triage's own transaction, before its write: the match and step
(unique), the routine, the item, the action, and the item's local state before and after, as the
inbox writes them (JSON, opaque here), with `applied_at`. A step is a compare-and-set on the item's
local state as its cycle read it, before the item's facts (`EngineItems.LocalStates`, passed as
`Job.Local`), and its record checks, in the same transaction, that the routine isn't hands off the
item, so the user's own change, or reversal, while the step was on its way wins: `KeptChanged` ("Not
done: the item changed since this matched.", which also follows a snooze the wake rules ended or
another routine's step that landed first), or the routine left alone ("Left alone: you took back
what it did here.", the words that go with Let it act again). A step that changes nothing records
nothing, and its step's note says why ("You set it aside yourself, so it's left as you left it."). A
step a crash cut off after its change committed runs again on the next start (`stepUnderWay`), finds
its effect, and is done, so nothing changes twice. Its limits are the local steps' (`runLocal`): a
sign-out, Pause, and the routine's active hours hold it, the match staying queued (`signed_out`,
`paused`, `active_hours`); the caps, the tries, the agents at once, the daily cap, readiness, the
outside contributor rule, and a catch-up never do; its own limit is the routine's Once per.
`Match.Effects` lists each match's, with `CanUndo` (neither undone nor taken back, and the item
still as it left it, `Triage.AsLeft`; false in a copy without the engine).

**Undo** (`Undo(ctx, ids []int64) (Undone, error)`, by effect: a match's `Effects`, or a
grouped line's `ActivityGroup.Undo`) is a command the loop applies. It reads the effects newest
first, skips those undone, taken back, or of an item gone (`Skipped`), and hands the rest to
`Triage.Undo` in one transaction, whose record marks each effect it put back `undone_at` and
leaves its routine hands off the item (`autopilot_hands_off`, "You undid what it did."). An
item that changed since stays as it is (`Left`). `Words` says it for a toast ("Put back 5 items.
1 changed since, so it's left as it is."). A GitHub action has no Undo.

## Hands off

The user's own triage that takes back what a routine did reaches the engine through the outcomes
fan-out (`Outcome.Reversed`: the routine, the item, and what it did, which core's `toEngineOutcomes`
passes on), which tells the engine before the mirror of the GitHub notifications, whose record can
wait on a clear on its way to GitHub; `Engine.Outcomes` records it at once, in whichever copy of the
app the user did it, in one transaction: that part's effects of the routine on the item (the pin, or
the dismissal and the snooze) applied by the outcome's time are `reversed_at`, so a step that landed
after it keeps its Undo, and the routine leaves the item alone (`InsertHandsOff`, with the words:
"You brought back what it snoozed.", "You snoozed it again yourself.", "You unpinned what it
pinned.", ...). Setting aside again what a routine's Restore brought back is no reversal: only an
Undo leaves such a routine hands off the item. A routine evaluates no item it leaves alone, so it
records no match there; an open match of it on the item that hasn't started a run closes as
`hands_off` ("Left alone: you took back what it did here."), which opens again, if it never started
a step, once the user lets it act again (`LetActAgain`). `Explain` says so (`LeftAlone`), `Test`
marks such items (`TestItem.LeftAlone`), and `Matching` counts none of them.

## Activity's lines

Each `ActivityDay` holds, beside its matches, a line per routine and kind of local step among the
page's matches, on the local day each step ran (`ActivityGroup`: `Words` "Snoozed 6", the count, how
many were undone and taken back, the effects Undo would still put back, the matches it stands for,
their items, and when the last ran; `Key` names it across pages, whose matches may split a day). The
first page also says which items the routines leave alone (`ActivityPage.LeftAlone`, newest first,
each with the routine, the item's copy from its latest match, why, and since), for **Let it act
again**.

**The simulator** runs the triage steps through the inbox's real triage (`sim.Triage`, the
adapter core's `inboxTriage` is, over the app the scenario built), and its scenarios include
the *Snooze passing bot PRs* template as the template gallery holds it (the inbox reasons,
Checks pass, a trusted bot's patch or minor update: snooze three days, once per head commit,
waking by the user's rules, at once in Ask first, a record alone in Dry run), Undo after the
user changed some of the items, and hands off and **Let it act again**.

**The simulator** (`internal/autopilot/sim`, test support only tests import) runs the real
engine and ledger over a real database, inside a `testing/synctest` bubble: a scripted GitHub
(`World`: items whose heads, checks, and states a scenario changes) that the inbox's own
service reads, so every item goes through the inbox's store; a clock with sleep gaps (`Sleep`
jumps with no tick, `Advance` lets the ticks run); the fake `Runner` and fake dispatchers, but
the real notices dispatcher and problem notices over a fake sink (`Notices`, which records each
notice and can fail); and crash-and-restart points over the same data (`Crash`, `Begin`,
`Restart`), and a second copy on the same data folder (`Copy`). A scripted pull request's
failed checks can be named (`FailNamed`), as *Tell me about security failures* needs, whose
template the simulator loads exactly as the gallery holds it. Its pool keeps one connection,
so a wait for the database is one the bubble sees. Each scenario ends with the ledger it
expects.
