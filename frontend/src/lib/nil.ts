import type { Celebrations, InboxSnapshot, NilMoment } from '../platform'
import { momentQuip } from './milestones'
import { inboxLists, isEmptyList, type TriagedInbox } from './triage'

// Inbox nil: the moment every list is empty, and the party the inbox throws for it.

// isInboxNil reports whether the inbox is truly empty, as triaged sorts it: refreshed at
// least once without an error, and nothing left in any list, counting what GitHub
// matched beyond the items it returned. A fresh install or a failed refresh is not nil,
// it is just unknown.
export function isInboxNil(snapshot: InboxSnapshot, triaged: TriagedInbox): boolean {
  if (snapshot.lastRefreshedAt === '' || snapshot.lastError !== '') {
    return false
  }

  return inboxLists.every((list) => triaged.lists[list].total === 0)
}

// NilStats is what the party reports was set aside to get there.
export type NilStats = Pick<TriagedInbox, 'snoozed' | 'dismissed' | 'nextBack'>

// listsShow reports whether a list still shows at nil, below the party: one that holds
// something snoozed, dismissed, or muted.
export function listsShow(triaged: TriagedInbox): boolean {
  return inboxLists.some((list) => !isEmptyList(triaged.lists[list]))
}

// PartyFold is how the user left the party on this visit to the inbox: as it came
// ('auto'), folded into its strip ('folded'), or run again in full from the strip ('open').
export type PartyFold = 'auto' | 'folded' | 'open'

// partyCompact reports whether the party shows as a strip above the lists rather than in
// full. With celebrations off it is always the strip. Otherwise, with no list below it, the
// party is the whole page, and shows in full; with one, it is a strip that leaves the page
// to the lists of what was set aside, until the user runs the party from it ('open').
export function partyCompact(fold: PartyFold, setAside: boolean, mode: Celebrations): boolean {
  if (mode === 'off') {
    return true
  }

  return setAside && fold !== 'open'
}

// firstQuip greets the first inbox the user ever emptied while they watched.
export const firstQuip = 'Inbox zero is for amateurs. You hit nil.'

// earnedQuips greet every other inbox emptied while the user watched, one after another,
// nil by nil (earnedQuip).
export const earnedQuips: readonly string[] = [
  'Cleared it yourself. The inbox never stood a chance.',
  'return nil // and mean it.',
  'Another inbox, garbage collected.',
  'err == nil. Carry on.',
  'Nothing left to review but your own brilliance.',
  'You emptied it. Every last one.',
  'Shipped, reviewed, done. The queue is a memory.',
  'defer celebrate()',
]

// earnedQuip is the quip for the user's count-th nil, the next of earnedQuips each time.
export function earnedQuip(count: number): string {
  return earnedQuips[Math.max(count, 0) % earnedQuips.length] ?? firstQuip
}

// openingQuip is the quip the party opens on, at now. An inbox found empty opens on a
// random one of quips. One the user emptied opens on firstQuip the first time, and with no
// moment to go on; on the quip about what it unlocked when it unlocked something; and
// otherwise as often on a quip about the moment (momentQuip), when one fits, as on the next
// of earnedQuips.
export function openingQuip(
  earned: boolean,
  moment: NilMoment | null,
  now: Date,
  random: () => number = Math.random,
): string {
  if (!earned) {
    return pickQuip(random)
  }

  if (moment === null || moment.unlocked.some((a) => a.kind === 'first')) {
    return firstQuip
  }

  const fits = momentQuip(moment, now, random)

  if (fits !== null && (moment.unlocked.length > 0 || random() < 0.5)) {
    return fits
  }

  return earnedQuip(moment.count)
}

// quips are the party's one-liners. Arriving at an empty inbox opens on a random one, and
// every replay moves to the next.
export const quips: readonly string[] = [
  'Zero items. Zero notifications. Zero regrets.',
  'The pull requests fear you.',
  'Every review reviewed. Every PR handled. Go touch grass.',
  'Status 204 No Content: the best status code there is.',
  'len(inbox) == 0, and the crowd goes wild.',
  'Even the linter has nothing to say.',
  'Merge conflicts: 0. Life conflicts: out of scope.',
  'Your backlog called. It had nothing to say.',
  'Somewhere, a 10x engineer just felt a disturbance.',
  'No blockers. No nits. Just vibes.',
  'You are the code review the bugs warned each other about.',
  'All green. Ship yourself a coffee.',
  'Go ahead, refresh. We dare you.',
  'This is what peak performance looks like. Screenshot it.',
]

// pickQuip picks the opening quip for an inbox found already empty.
export function pickQuip(random: () => number = Math.random): string {
  return quips[Math.min(Math.floor(random() * quips.length), quips.length - 1)] ?? firstQuip
}

// nextQuip is the quip after the one shown; any quip not in the list leads into it.
export function nextQuip(current: string): string {
  return quips[(quips.indexOf(current) + 1) % quips.length] ?? firstQuip
}

// replayLabels are the replay button's command, which escalates with every run.
export const replayLabels: readonly string[] = [
  'go run ./party',
  'go run ./party --again',
  'go run ./party --again --harder',
  'sudo go run ./party',
  'sudo go run ./party --force',
  'sudo rm -rf ./boredom',
]

// replayLabel is the replay button's command after runs replays.
export function replayLabel(runs: number): string {
  return replayLabels[Math.min(runs, replayLabels.length - 1)] ?? 'go run ./party'
}

// earnedBurst is how much confetti emptying the inbox throws; maxBurst caps a replay.
export const earnedBurst = 180
export const maxBurst = 320

// replayBurst is how much confetti a replay throws: more with every run, to a limit.
export function replayBurst(runs: number): number {
  return Math.min(110 + runs * 35, maxBurst)
}

// Flourish is the small show a party plays as it starts when it throws no confetti of its
// own: a glint sweeping across it, the mark hopping and turning, a hard RGB-split burst on
// "nil", code glyphs drifting up out of the mark, or the chip landing like a rubber stamp.
export type Flourish = 'glint' | 'hop' | 'glitch' | 'sparks' | 'stamp'

// flourishes are every flourish, and plainFlourishes those that need no chip to play on.
export const flourishes: readonly Flourish[] = ['glint', 'hop', 'glitch', 'sparks', 'stamp']
export const plainFlourishes: readonly Flourish[] = flourishes.filter((f) => f !== 'stamp')

// nextFlourish picks the flourish a party plays from choices, never last, the one the party
// before it played, so no two in a row are the same.
export function nextFlourish(
  last: Flourish | null,
  random: () => number = Math.random,
  choices: readonly Flourish[] = flourishes,
): Flourish {
  const fresh = choices.filter((f) => f !== last)
  const from = fresh.length > 0 ? fresh : choices

  return from[Math.min(Math.floor(random() * from.length), from.length - 1)] ?? 'glint'
}
