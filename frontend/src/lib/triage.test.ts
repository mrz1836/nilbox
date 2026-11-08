import { describe, expect, it } from 'vitest'

import type { WakeReview } from '../platform'
import { item, list, snapshot } from '../test/fixtures'
import {
  capitalize,
  findItem,
  formatFull,
  formatWhen,
  inboxCount,
  isBackFromSnooze,
  isDismissed,
  isSnoozed,
  snoozeChoices,
  snoozeEnd,
  snoozeLength,
  snoozeShort,
  tabOf,
  triageList,
  wakeParts,
  wakeSummary,
} from './triage'

// Tests run in UTC (vitest.config.ts); now is a Monday.
const now = new Date('2026-06-15T12:00:00Z')

// plain folds the narrow and no-break spaces some locales put in times into plain ones.
function plain(text: string): string {
  return text.replace(/\s/g, ' ')
}

// at returns the RFC 3339 time hours after now (before, when negative).
function at(hours: number): string {
  return new Date(now.getTime() + hours * 3_600_000).toISOString()
}

describe('triage state', () => {
  it('tells dismissed, snoozed, and back-from-snooze items apart', () => {
    const dismissed = item({ dismissedAt: at(-2) })
    const snoozed = item({ snoozedUntil: at(5) })
    const back = item({ snoozedUntil: at(-3) })
    const longBack = item({ snoozedUntil: at(-25) })
    const plainItem = item()

    expect(isDismissed(dismissed)).toBe(true)
    expect(isDismissed(snoozed)).toBe(false)

    expect(isSnoozed(snoozed, now)).toBe(true)
    expect(isSnoozed(back, now)).toBe(false)
    expect(isSnoozed(plainItem, now)).toBe(false)

    expect(isBackFromSnooze(back, now)).toBe(true)
    expect(isBackFromSnooze(longBack, now)).toBe(false)
    expect(isBackFromSnooze(snoozed, now)).toBe(false)
    expect(isBackFromSnooze(plainItem, now)).toBe(false)
    expect(isBackFromSnooze(item({ snoozedUntil: at(-1), dismissedAt: at(-1) }), now)).toBe(false)
  })

  it('ends a snooze exactly when its time comes', () => {
    const snoozed = item({ snoozedUntil: now.toISOString() })

    expect(isSnoozed(snoozed, new Date(now.getTime() - 1))).toBe(true)
    expect(isSnoozed(snoozed, now)).toBe(false)
    expect(isBackFromSnooze(snoozed, now)).toBe(true)
  })
})

describe('triageList', () => {
  it('sorts a list into its tabs, soonest back and latest dismissed first', () => {
    const a = item({ id: 1 })
    const b = item({ id: 2, snoozedUntil: at(48) })
    const c = item({ id: 3, dismissedAt: at(-5) })
    const d = item({ id: 4, snoozedUntil: at(2) })
    const e = item({ id: 5, dismissedAt: at(-1) })
    const f = item({ id: 6, snoozedUntil: at(-2) })

    const triaged = triageList(list([a, b, c, d, e, f], 40), now)

    expect(triaged.inbox.map((i) => i.id)).toEqual([1, 6])
    expect(triaged.snoozed.map((i) => i.id)).toEqual([4, 2])
    expect(triaged.dismissed.map((i) => i.id)).toEqual([5, 3])
    expect(triaged.total).toBe(36)
  })

  it('never counts fewer than the inbox tab shows, and reads a missing list as empty', () => {
    const triaged = triageList(list([item({ id: 1 }), item({ id: 2, dismissedAt: at(-1) })], 0), now)

    expect(triaged.total).toBe(1)
    expect(triageList({ items: null, shown: 0, total: 0 }, now)).toEqual({
      inbox: [],
      snoozed: [],
      dismissed: [],
      total: 0,
    })
  })
})

describe('inboxCount', () => {
  it('counts what is in the inbox, each item once, leaving out what is snoozed or dismissed', () => {
    const mine = item({ id: 1 })
    const review = item({ id: 2 })
    const back = item({ id: 3, snoozedUntil: at(-2) })

    const counted = inboxCount(
      snapshot({
        reviewRequested: list([review, item({ id: 4, snoozedUntil: at(5) })]),
        authored: list([mine, back, item({ id: 5, dismissedAt: at(-1) })], 40),
        assigned: list([mine]),
      }),
      now,
    )

    expect(counted).toBe(3)
  })

  it('is 0 for an empty inbox, and reads a missing list as empty', () => {
    expect(inboxCount(snapshot(), now)).toBe(0)
    expect(inboxCount(snapshot({ authored: { items: null, shown: 0, total: 0 } }), now)).toBe(0)
  })
})

describe('snooze lengths', () => {
  it('names every choice', () => {
    expect(snoozeChoices.map(snoozeLength)).toEqual(['1 day', '2 days', '3 days', '1 week', '2 weeks', '30 days'])
    expect(snoozeChoices.map(snoozeShort)).toEqual(['1d', '2d', '3d', '1w', '2w', '30d'])
  })

  it('ends a snooze at the same time of day', () => {
    expect(snoozeEnd(now, 3).toISOString()).toBe('2026-06-18T12:00:00.000Z')
    expect(now.toISOString()).toBe('2026-06-15T12:00:00.000Z')
  })
})

describe('formatWhen', () => {
  it('names a time the way people say it', () => {
    expect(plain(formatWhen(now, new Date('2026-06-15T18:30:00Z'), 'en-US'))).toBe('today, 6:30 PM')
    expect(plain(formatWhen(now, new Date('2026-06-16T09:00:00Z'), 'en-US'))).toBe('tomorrow, 9:00 AM')
    expect(plain(formatWhen(now, new Date('2026-06-18T12:00:00Z'), 'en-US'))).toBe('Thu, 12:00 PM')
    expect(formatWhen(now, new Date('2026-06-22T12:00:00Z'), 'en-US')).toBe('Mon, Jun 22')
    expect(formatWhen(now, new Date('2027-01-04T12:00:00Z'), 'en-US')).toBe('Mon, Jan 4, 2027')
  })

  it('counts calendar days, not 24-hour spans', () => {
    const late = new Date('2026-06-15T23:30:00Z')

    expect(plain(formatWhen(late, new Date('2026-06-16T00:30:00Z'), 'en-US'))).toBe('tomorrow, 12:30 AM')
  })

  it('names a time in full for a tooltip', () => {
    expect(plain(formatFull('2026-06-18T12:00:00Z', 'en-US'))).toBe('Thursday, June 18, 2026 at 12:00 PM')
    expect(formatFull('', 'en-US')).toBe('')
  })

  it('capitalizes a phrase', () => {
    expect(capitalize('tomorrow, 9:00 AM')).toBe('Tomorrow, 9:00 AM')
    expect(capitalize('')).toBe('')
  })
})

describe('wake reasons', () => {
  it('names what ended a snooze early, most decisive first', () => {
    expect(wakeParts({ commits: 2, forcePushed: false, review: 'approved', comments: 3 })).toEqual([
      'approved',
      '2 new commits',
      '3 new comments',
    ])
    expect(wakeSummary({ commits: 1, forcePushed: false, review: 'changes_requested', comments: 1 })).toBe(
      'changes requested, 1 new commit, 1 new comment',
    )
    expect(wakeSummary({ commits: 0, forcePushed: true, review: 'reviewed', comments: 0 })).toBe(
      'new review, force-pushed',
    )
  })

  it('says nothing when nothing woke it', () => {
    expect(wakeSummary(null)).toBe('')
    expect(wakeSummary({ commits: 0, forcePushed: false, review: '', comments: 0 })).toBe('')
  })

  it('names nothing for a review it does not know', () => {
    // A review another build stored, outside the ones this build knows.
    const praised = 'praised' as unknown as WakeReview

    expect(wakeSummary({ commits: 2, forcePushed: false, review: praised, comments: 0 })).toBe('2 new commits')
  })
})

describe('findItem and tabOf', () => {
  const fresh = item({ id: 1 })
  const snoozed = item({ id: 2, snoozedUntil: '2026-06-18T12:00:00Z' })
  const dismissed = item({ id: 3, dismissedAt: '2026-06-15T09:00:00Z' })

  it('finds an item in any list', () => {
    const snap = snapshot({ authored: list([fresh]), assigned: list([snoozed]) })

    expect(findItem(snap, 2)).toBe(snoozed)
    expect(findItem(snap, 1)).toBe(fresh)
    expect(findItem(snap, 9)).toBeUndefined()
    expect(findItem(snapshot({ assigned: { items: null, shown: 0, total: 0 } }), 1)).toBeUndefined()
  })

  it('finds the tab that holds an item', () => {
    const triaged = triageList(list([fresh, snoozed, dismissed]), now)

    expect(tabOf(triaged, 1)).toBe('inbox')
    expect(tabOf(triaged, 2)).toBe('snoozed')
    expect(tabOf(triaged, 3)).toBe('dismissed')
    expect(tabOf(triaged, 9)).toBeNull()
  })
})
