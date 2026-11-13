import type { InboxItem, InboxList, InboxSnapshot, ListKey, WakeReasons, WakeReview } from '../platform'
import { counted } from './format'
import { itemKey } from './itemWorktrees'

// Triage as the inbox shows it. The snapshot keeps every item in its lists with its
// triage state, and this module sorts each list into the tabs the inbox shows, against
// the window's clock: a snooze ends as the clock passes it, without the inbox being read
// again.

// snoozeChoices are the snooze lengths offered, in days: the Snooze menu's choices and
// the default's options in Settings.
export const snoozeChoices: readonly number[] = [1, 2, 3, 7, 14, 30]

// defaultSnoozeDays is the Snooze button's length until the settings have loaded. It
// matches the backend's default.
export const defaultSnoozeDays = 3

// commitThresholds and commentThresholds are the wake rules' choices in Settings: how
// many new commits or comments end a snooze early, 0 for never. One commit means any
// push, a force-push included.
export const commitThresholds: readonly number[] = [0, 1, 3, 5]
export const commentThresholds: readonly number[] = [0, 1, 3, 5, 10]

// reviewWakes are the choices for which reviews end a snooze early.
export const reviewWakes = ['never', 'decisions', 'any'] as const

// reviewText names the review that ended a snooze. None, or one this build does not
// know, as another build may have stored, names nothing.
const reviewText: Readonly<Partial<Record<WakeReview, string>>> = {
  changes_requested: 'changes requested',
  approved: 'approved',
  reviewed: 'new review',
}

// wakeParts lists the new activity that ended a snooze early, most decisive first: "changes
// requested", "2 new commits" or "force-pushed", then "3 new comments".
export function wakeParts(reasons: WakeReasons): string[] {
  const parts: string[] = []
  const review = reviewText[reasons.review]

  if (review !== undefined) {
    parts.push(review)
  }

  if (reasons.commits > 0) {
    parts.push(counted(reasons.commits, 'new commit', 'new commits'))
  } else if (reasons.forcePushed) {
    parts.push('force-pushed')
  }

  if (reasons.comments > 0) {
    parts.push(counted(reasons.comments, 'new comment', 'new comments'))
  }

  return parts
}

// wakeSummary is what ended a snooze early, in a few words: "approved, 2 new commits".
// It is "" when nothing did.
export function wakeSummary(reasons: WakeReasons | null): string {
  return reasons === null ? '' : wakeParts(reasons).join(', ')
}

// backMarkMs is how long an item back from a snooze is marked as such.
const backMarkMs = 24 * 60 * 60 * 1000

// TriageTab is one of a list's tabs: the items in the list, those snoozed, those
// dismissed, and those of a repository or org the user muted.
export type TriageTab = 'inbox' | 'snoozed' | 'dismissed' | 'muted'

// triageTabs are a list's tabs, in the order it shows them.
export const triageTabs: readonly TriageTab[] = ['inbox', 'snoozed', 'dismissed', 'muted']

// TriagedList is one inbox list sorted into its tabs.
export interface TriagedList {
  // inbox keeps the list's order: the items pinned themselves first, the most recently
  // pinned first, then those pinned with their repository or org, then the rest by newest
  // activity.
  inbox: InboxItem[]
  // snoozed are soonest back first.
  snoozed: InboxItem[]
  // dismissed are the most recently dismissed first.
  dismissed: InboxItem[]
  // muted are the items of a muted repository or org, whatever else they are, in the
  // list's order.
  muted: InboxItem[]
  // total is GitHub's match count less the items set apart, and never below the number
  // in the inbox tab.
  total: number
}

// timeOf reads a timestamp in milliseconds; an empty or bad one is NaN, which compares
// false with everything.
function timeOf(iso: string): number {
  return iso === '' ? Number.NaN : Date.parse(iso)
}

// isDismissed reports whether an item was dismissed.
export function isDismissed(item: InboxItem): boolean {
  return item.dismissedAt !== ''
}

// isPinnedItself reports whether the item itself is pinned to the top of its lists, which
// its pin button unpins.
export function isPinnedItself(item: InboxItem): boolean {
  return item.pinnedAt !== ''
}

// isPinned reports whether an item is pinned: itself, or with its repository or org. The
// items pinned themselves come first, then those pinned with their repository.
export function isPinned(item: InboxItem): boolean {
  return isPinnedItself(item) || item.repoRule === 'pinned'
}

// isMuted reports whether the item's repository or org is muted, which sets it apart in
// its lists' Muted tab, out of every count.
export function isMuted(item: InboxItem): boolean {
  return item.repoRule === 'muted'
}

// isSnoozed reports whether an item's snooze is still running at now.
export function isSnoozed(item: InboxItem, now: Date): boolean {
  return !isDismissed(item) && timeOf(item.snoozedUntil) > now.getTime()
}

// isBackFromSnooze reports whether an item's snooze ended within the last day, so its
// return can be pointed out.
export function isBackFromSnooze(item: InboxItem, now: Date): boolean {
  const since = now.getTime() - timeOf(item.snoozedUntil)

  return !isDismissed(item) && since >= 0 && since < backMarkMs
}

// triageList sorts a list's items into its tabs at now. An item of a muted repository or
// org is in the Muted tab whether or not it is snoozed or dismissed too.
export function triageList(list: InboxList, now: Date): TriagedList {
  const inbox: InboxItem[] = []
  const snoozed: InboxItem[] = []
  const dismissed: InboxItem[] = []
  const muted: InboxItem[] = []

  for (const item of list.items) {
    if (isMuted(item)) {
      muted.push(item)
    } else if (isDismissed(item)) {
      dismissed.push(item)
    } else if (isSnoozed(item, now)) {
      snoozed.push(item)
    } else {
      inbox.push(item)
    }
  }

  snoozed.sort((a, b) => timeOf(a.snoozedUntil) - timeOf(b.snoozedUntil))
  dismissed.sort((a, b) => timeOf(b.dismissedAt) - timeOf(a.dismissedAt))

  const total = Math.max(list.total - snoozed.length - dismissed.length - muted.length, inbox.length)

  return { inbox, snoozed, dismissed, muted, total }
}

// findItem looks an item up in a snapshot's lists.
export function findItem(snapshot: InboxSnapshot, id: number): InboxItem | undefined {
  for (const list of [snapshot.reviewRequested, snapshot.authored, snapshot.assigned]) {
    const found = list.items.find((item) => item.id === id)

    if (found !== undefined) {
      return found
    }
  }

  return undefined
}

// ListedItem is an item of the inbox and the first of its lists that holds it.
export interface ListedItem {
  item: InboxItem
  list: ListKey
}

// itemsByRef maps the inbox's items by itemKey, their repository in any case and their
// number, each to the first list that holds it in the inbox's order.
export function itemsByRef(snapshot: InboxSnapshot): ReadonlyMap<string, ListedItem> {
  const out = new Map<string, ListedItem>()

  for (const list of ['reviewRequested', 'authored', 'assigned'] as const) {
    for (const item of snapshot[list].items) {
      const key = itemKey(item.repo, item.number)

      if (!out.has(key)) {
        out.set(key, { item, list })
      }
    }
  }

  return out
}

// TriageChange is a triage action as the inbox shows it at once, before the backend's
// answer: the triage fields it gives an item.
export interface TriageChange {
  id: number
  set: Partial<Pick<InboxItem, 'dismissedAt' | 'snoozedUntil' | 'wokeBy' | 'pinnedAt'>>
}

// listsNewer reports whether a lists before b among the items not pinned: newest activity
// first, then the higher ID, as the backend orders them.
function listsNewer(a: InboxItem, b: InboxItem): boolean {
  const at = timeOf(a.updatedAt)
  const bt = timeOf(b.updatedAt)

  return at === bt ? a.id > b.id : at > bt
}

// pinRank is where an item comes in its lists: 0 pinned itself, 1 pinned with its
// repository or org, 2 neither.
function pinRank(item: InboxItem): number {
  if (isPinnedItself(item)) {
    return 0
  }

  return item.repoRule === 'pinned' ? 1 : 2
}

// placeItem puts an item back into a list it was taken out of at index. A newly pinned
// item goes first, as the most recently pinned; a newly unpinned one goes among the items
// of its rank (those pinned with their repository, or the rest), by its activity; any
// other keeps its place.
function placeItem(rest: readonly InboxItem[], next: InboxItem, was: InboxItem, index: number): InboxItem[] {
  let at = index

  if (isPinnedItself(next) && !isPinnedItself(was)) {
    at = 0
  } else if (!isPinnedItself(next) && isPinnedItself(was)) {
    const rank = pinRank(next)
    const after = rest.findIndex(
      (other) => pinRank(other) > rank || (pinRank(other) === rank && listsNewer(next, other)),
    )

    at = after < 0 ? rest.length : after
  }

  return [...rest.slice(0, at), next, ...rest.slice(at)]
}

// applyTriage returns the snapshot with a triage change made to the item in every list
// that holds it, ordered as the backend will order it.
export function applyTriage(snapshot: InboxSnapshot, change: TriageChange): InboxSnapshot {
  const apply = (list: InboxList): InboxList => {
    const index = list.items.findIndex((item) => item.id === change.id)
    const was = list.items[index]

    if (was === undefined) {
      return list
    }

    const rest = list.items.filter((_, i) => i !== index)

    return { ...list, items: placeItem(rest, { ...was, ...change.set }, was, index) }
  }

  return {
    ...snapshot,
    reviewRequested: apply(snapshot.reviewRequested),
    authored: apply(snapshot.authored),
    assigned: apply(snapshot.assigned),
  }
}

// tabOf is the tab of a sorted list that holds an item, or null when none does.
export function tabOf(triaged: TriagedList, id: number): TriageTab | null {
  return triageTabs.find((tab) => triaged[tab].some((item) => item.id === id)) ?? null
}

// TriagedInbox is the whole inbox sorted into its tabs at one time. Everything in the
// window that shows triage (the lists, the Inbox tab's count, the party at nil) reads
// this one sorting.
export interface TriagedInbox {
  lists: Readonly<Record<ListKey, TriagedList>>
  // count is how many items are in the inbox: in any list's own tab, not muted, snoozed,
  // or dismissed. An item in more than one list, such as a pull request you wrote and are
  // assigned, counts once.
  count: number
  // snoozed, dismissed, and muted count the items set aside, each once however many lists
  // it is in.
  snoozed: number
  dismissed: number
  muted: number
  // nextBack is when the soonest snooze ends, or null when nothing is snoozed.
  nextBack: Date | null
}

// triageInbox sorts every list of a snapshot into its tabs at now, and counts the inbox.
export function triageInbox(snapshot: InboxSnapshot, now: Date): TriagedInbox {
  const lists = {
    reviewRequested: triageList(snapshot.reviewRequested, now),
    authored: triageList(snapshot.authored, now),
    assigned: triageList(snapshot.assigned, now),
  }
  const inbox = new Set<number>()
  const snoozed = new Map<number, number>()
  const dismissed = new Set<number>()
  const muted = new Set<number>()

  for (const triaged of Object.values(lists)) {
    for (const item of triaged.inbox) {
      inbox.add(item.id)
    }

    for (const item of triaged.snoozed) {
      snoozed.set(item.id, timeOf(item.snoozedUntil))
    }

    for (const item of triaged.dismissed) {
      dismissed.add(item.id)
    }

    for (const item of triaged.muted) {
      muted.add(item.id)
    }
  }

  return {
    lists,
    count: inbox.size,
    snoozed: snoozed.size,
    dismissed: dismissed.size,
    muted: muted.size,
    nextBack: snoozed.size === 0 ? null : new Date(Math.min(...snoozed.values())),
  }
}

// triageTime is a time the inbox sorts into the same tabs at as at now: the latest
// snooze end that now has passed, or 0 when none has. The sorting changes only as the
// clock passes the end of a snooze, so a tick that passes none leaves this time, and
// the sorting, as they were.
export function triageTime(snapshot: InboxSnapshot, now: Date): number {
  const at = now.getTime()
  let passed = 0

  for (const list of [snapshot.reviewRequested, snapshot.authored, snapshot.assigned]) {
    for (const item of list.items) {
      const end = timeOf(item.snoozedUntil)

      if (end <= at && end > passed) {
        passed = end
      }
    }
  }

  return passed
}

// snoozeLength names a snooze length: "1 day", "3 days", "1 week", "2 weeks".
export function snoozeLength(days: number): string {
  if (days % 7 === 0) {
    const weeks = days / 7

    return weeks === 1 ? '1 week' : `${String(weeks)} weeks`
  }

  return days === 1 ? '1 day' : `${String(days)} days`
}

// snoozeShort is a snooze length in a few characters: "1d", "3d", "1w", "2w".
export function snoozeShort(days: number): string {
  return days % 7 === 0 ? `${String(days / 7)}w` : `${String(days)}d`
}

// snoozeEnd is when a snooze of days started at now ends: the same time of day, days
// later, as the backend works it out.
export function snoozeEnd(now: Date, days: number): Date {
  const end = new Date(now)
  end.setDate(end.getDate() + days)

  return end
}
