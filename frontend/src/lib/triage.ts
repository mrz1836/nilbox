import type { InboxItem, InboxList, InboxSnapshot, WakeReasons } from '../platform'
import { counted } from './format'

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

// reviewText names the review that ended a snooze.
const reviewText: Readonly<Record<string, string>> = {
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

const dayMs = 24 * 60 * 60 * 1000

// TriageTab is one of a list's tabs: the items in the list, those snoozed, and those
// dismissed.
export type TriageTab = 'inbox' | 'snoozed' | 'dismissed'

// TriagedList is one inbox list sorted into its tabs.
export interface TriagedList {
  // inbox keeps the list's order: the pinned items first, the most recently pinned first,
  // then the rest by newest activity.
  inbox: InboxItem[]
  // snoozed are soonest back first.
  snoozed: InboxItem[]
  // dismissed are the most recently dismissed first.
  dismissed: InboxItem[]
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

// isPinned reports whether an item is pinned to the top of its lists.
export function isPinned(item: InboxItem): boolean {
  return item.pinnedAt !== ''
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

// triageList sorts a list's items into its tabs at now.
export function triageList(list: InboxList, now: Date): TriagedList {
  const inbox: InboxItem[] = []
  const snoozed: InboxItem[] = []
  const dismissed: InboxItem[] = []

  for (const item of list.items ?? []) {
    if (isDismissed(item)) {
      dismissed.push(item)
    } else if (isSnoozed(item, now)) {
      snoozed.push(item)
    } else {
      inbox.push(item)
    }
  }

  snoozed.sort((a, b) => timeOf(a.snoozedUntil) - timeOf(b.snoozedUntil))
  dismissed.sort((a, b) => timeOf(b.dismissedAt) - timeOf(a.dismissedAt))

  const total = Math.max(list.total - snoozed.length - dismissed.length, inbox.length)

  return { inbox, snoozed, dismissed, total }
}

// findItem looks an item up in a snapshot's lists.
export function findItem(snapshot: InboxSnapshot, id: number): InboxItem | undefined {
  for (const list of [snapshot.reviewRequested, snapshot.authored, snapshot.assigned]) {
    const found = list.items?.find((item) => item.id === id)

    if (found !== undefined) {
      return found
    }
  }

  return undefined
}

// tabOf is the tab of a sorted list that holds an item, or null when none does.
export function tabOf(triaged: TriagedList, id: number): TriageTab | null {
  const tabs: readonly TriageTab[] = ['inbox', 'snoozed', 'dismissed']

  return tabs.find((tab) => triaged[tab].some((item) => item.id === id)) ?? null
}

// inboxCount counts the items in the inbox at now: those in any list's own tab, neither
// snoozed nor dismissed. An item in more than one list, such as a pull request you wrote
// and are assigned, counts once.
export function inboxCount(snapshot: InboxSnapshot, now: Date): number {
  const ids = new Set<number>()

  for (const list of [snapshot.reviewRequested, snapshot.authored, snapshot.assigned]) {
    for (const item of list.items ?? []) {
      if (!isDismissed(item) && !isSnoozed(item, now)) {
        ids.add(item.id)
      }
    }
  }

  return ids.size
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

// startOfDay is the local midnight that starts d's day.
function startOfDay(d: Date): number {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
}

// formatWhen names a time from now on the way people say it: "today, 3:42 PM",
// "tomorrow, 9:00 AM", "Sat, 3:42 PM" within the week, then "Thu, Nov 19", with the year
// when it is not this one. locale is for tests; the app uses the system's.
export function formatWhen(now: Date, when: Date, locale?: string): string {
  // Rounding absorbs the 23- and 25-hour days around a change of the clocks.
  const days = Math.round((startOfDay(when) - startOfDay(now)) / dayMs)
  const time = new Intl.DateTimeFormat(locale, { hour: 'numeric', minute: '2-digit' }).format(when)

  if (days <= 0) {
    return `today, ${time}`
  }

  if (days === 1) {
    return `tomorrow, ${time}`
  }

  if (days < 7) {
    return `${new Intl.DateTimeFormat(locale, { weekday: 'short' }).format(when)}, ${time}`
  }

  const year = when.getFullYear() === now.getFullYear() ? {} : { year: 'numeric' as const }

  return new Intl.DateTimeFormat(locale, { weekday: 'short', month: 'short', day: 'numeric', ...year }).format(when)
}

// formatFull names a time in full, for a tooltip: "Saturday, November 8, 2026 at 3:42 PM".
export function formatFull(iso: string, locale?: string): string {
  const ms = timeOf(iso)

  return Number.isNaN(ms) ? '' : new Intl.DateTimeFormat(locale, { dateStyle: 'full', timeStyle: 'short' }).format(ms)
}

// capitalize starts a phrase with a capital, for formatWhen at the start of a line.
export function capitalize(phrase: string): string {
  return phrase.charAt(0).toUpperCase() + phrase.slice(1)
}
