import type { InboxSnapshot } from '../platform'
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
// full. With no list below it, the party is the whole page, and shows in full. Otherwise
// it shows in full when the inbox emptied while the user watched (earned), the reward for
// getting there, and as a strip when they arrive to find it empty, leaving the page to
// the lists of what was set aside. Once the user folds it or runs it again, it stays as
// they left it.
export function partyCompact(fold: PartyFold, earned: boolean, setAside: boolean): boolean {
  if (!setAside) {
    return false
  }

  return fold === 'folded' || (fold === 'auto' && !earned)
}

// earnedQuip greets an inbox emptied while you watched.
export const earnedQuip = 'Inbox zero is for amateurs. You hit nil.'

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
  return quips[Math.min(Math.floor(random() * quips.length), quips.length - 1)] ?? earnedQuip
}

// nextQuip is the quip after the one shown; the earned one leads into the list.
export function nextQuip(current: string): string {
  return quips[(quips.indexOf(current) + 1) % quips.length] ?? earnedQuip
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

// arrivalBurst and earnedBurst are how much confetti opening the inbox at nil and emptying
// it throw; maxBurst caps a replay.
export const arrivalBurst = 70
export const earnedBurst = 180
export const maxBurst = 320

// replayBurst is how much confetti a replay throws: more with every run, to a limit.
export function replayBurst(runs: number): number {
  return Math.min(110 + runs * 35, maxBurst)
}
