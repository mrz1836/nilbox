import { describe, expect, it } from 'vitest'

import type { WakeReview } from '../platform'
import { FIXED_NOW, fromNow } from '../test/clock'
import { item, list, snapshot } from '../test/fixtures'
import {
  applyTriage,
  findItem,
  isBackFromSnooze,
  isDismissed,
  isMuted,
  isPinned,
  isPinnedItself,
  isSnoozed,
  itemsByRef,
  snoozeChoices,
  snoozeEnd,
  snoozeLength,
  snoozeShort,
  tabOf,
  triageInbox,
  triageList,
  triageTime,
  wakeParts,
  wakeSummary,
} from './triage'

// Tests run in UTC (vitest.config.ts); FIXED_NOW is a Monday.

// at returns the RFC 3339 time hours after now (before, when negative).
function at(hours: number): string {
  return fromNow(hours * 3_600_000).toISOString()
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

    expect(isSnoozed(snoozed, FIXED_NOW)).toBe(true)
    expect(isSnoozed(back, FIXED_NOW)).toBe(false)
    expect(isSnoozed(plainItem, FIXED_NOW)).toBe(false)

    expect(isBackFromSnooze(back, FIXED_NOW)).toBe(true)
    expect(isBackFromSnooze(longBack, FIXED_NOW)).toBe(false)
    expect(isBackFromSnooze(snoozed, FIXED_NOW)).toBe(false)
    expect(isBackFromSnooze(plainItem, FIXED_NOW)).toBe(false)
    expect(isBackFromSnooze(item({ snoozedUntil: at(-1), dismissedAt: at(-1) }), FIXED_NOW)).toBe(false)
  })

  it('ends a snooze exactly when its time comes', () => {
    const snoozed = item({ snoozedUntil: FIXED_NOW.toISOString() })

    expect(isSnoozed(snoozed, fromNow(-1))).toBe(true)
    expect(isSnoozed(snoozed, FIXED_NOW)).toBe(false)
    expect(isBackFromSnooze(snoozed, FIXED_NOW)).toBe(true)
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

    const triaged = triageList(list([a, b, c, d, e, f], 40), FIXED_NOW)

    expect(triaged.inbox.map((i) => i.id)).toEqual([1, 6])
    expect(triaged.snoozed.map((i) => i.id)).toEqual([4, 2])
    expect(triaged.dismissed.map((i) => i.id)).toEqual([5, 3])
    expect(triaged.total).toBe(36)
  })

  it('sets a muted repository’s items apart in the Muted tab, whatever else they are, out of the total', () => {
    const a = item({ id: 1 })
    const muted = item({ id: 2, repoRule: 'muted' })
    const mutedSnoozed = item({ id: 3, repoRule: 'muted', snoozedUntil: at(4) })
    const mutedDismissed = item({ id: 4, repoRule: 'muted', dismissedAt: at(-1) })
    const pinned = item({ id: 5, repoRule: 'pinned' })

    const triaged = triageList(list([pinned, a, muted, mutedSnoozed, mutedDismissed], 20), FIXED_NOW)

    expect(triaged.inbox.map((i) => i.id)).toEqual([5, 1])
    expect(triaged.muted.map((i) => i.id)).toEqual([2, 3, 4])
    expect(triaged.snoozed).toEqual([])
    expect(triaged.dismissed).toEqual([])
    expect(triaged.total).toBe(17)
  })

  it('never counts fewer than the inbox tab shows', () => {
    const triaged = triageList(list([item({ id: 1 }), item({ id: 2, dismissedAt: at(-1) })], 0), FIXED_NOW)

    expect(triaged.total).toBe(1)
  })
})

describe('triageInbox', () => {
  it('sorts every list, and counts what is in the inbox once each, leaving out what is set aside', () => {
    const mine = item({ id: 1 })
    const review = item({ id: 2 })
    const back = item({ id: 3, snoozedUntil: at(-2) })
    const later = item({ id: 4, snoozedUntil: at(5) })
    const gone = item({ id: 5, dismissedAt: at(-1) })
    const inbox = snapshot({
      reviewRequested: list([review, later]),
      authored: list([mine, back, gone], 40),
      assigned: list([mine, later]),
    })

    const triaged = triageInbox(inbox, FIXED_NOW)

    expect(triaged.lists.reviewRequested).toEqual(triageList(inbox.reviewRequested, FIXED_NOW))
    expect(triaged.lists.authored).toEqual(triageList(inbox.authored, FIXED_NOW))
    expect(triaged.lists.assigned.inbox).toEqual([mine])
    expect(triaged.count).toBe(3)
    // An item set aside in two lists counts once.
    expect(triaged.snoozed).toBe(1)
    expect(triaged.dismissed).toBe(1)
    expect(triaged.nextBack).toEqual(new Date(at(5)))
  })

  it('counts no muted item in the inbox, and no muted snooze as the next back', () => {
    const triaged = triageInbox(
      snapshot({
        reviewRequested: list([item({ id: 1, repoRule: 'muted' }), item({ id: 2 })]),
        authored: list([item({ id: 1, repoRule: 'muted' }), item({ id: 3, repoRule: 'muted', snoozedUntil: at(1) })]),
      }),
      FIXED_NOW,
    )

    expect(triaged.count).toBe(1)
    expect(triaged.muted).toBe(2)
    expect(triaged.snoozed).toBe(0)
    expect(triaged.nextBack).toBeNull()
  })

  it('finds the soonest snooze among several', () => {
    const triaged = triageInbox(
      snapshot({
        reviewRequested: list([item({ id: 1, snoozedUntil: at(48) })]),
        authored: list([item({ id: 2, snoozedUntil: at(3) })]),
      }),
      FIXED_NOW,
    )

    expect(triaged.snoozed).toBe(2)
    expect(triaged.nextBack).toEqual(new Date(at(3)))
  })

  it('is empty for an empty inbox', () => {
    expect(triageInbox(snapshot(), FIXED_NOW)).toMatchObject({
      count: 0,
      snoozed: 0,
      dismissed: 0,
      muted: 0,
      nextBack: null,
    })
  })
})

describe('triageTime', () => {
  const inbox = snapshot({
    reviewRequested: list([item({ id: 1, snoozedUntil: at(-3) }), item({ id: 2, snoozedUntil: at(-1) })]),
    authored: list([item({ id: 3, snoozedUntil: at(2) }), item({ id: 4 })]),
  })

  it('is the latest snooze end already passed, which sorts the inbox as now does', () => {
    expect(triageTime(inbox, FIXED_NOW)).toBe(Date.parse(at(-1)))
    expect(triageInbox(inbox, new Date(triageTime(inbox, FIXED_NOW)))).toEqual(triageInbox(inbox, FIXED_NOW))
  })

  it('stays put until the clock passes the next snooze end', () => {
    const soon = fromNow(3_600_000)
    const past = new Date(Date.parse(at(2)))

    expect(triageTime(inbox, soon)).toBe(triageTime(inbox, FIXED_NOW))
    expect(triageTime(inbox, past)).toBe(Date.parse(at(2)))
  })

  it('is 0 when no snooze has ended', () => {
    expect(triageTime(inbox, new Date(at(-5)))).toBe(0)
    expect(triageTime(snapshot(), FIXED_NOW)).toBe(0)
  })
})

describe('snooze lengths', () => {
  it('names every choice', () => {
    expect(snoozeChoices.map(snoozeLength)).toEqual(['1 day', '2 days', '3 days', '1 week', '2 weeks', '30 days'])
    expect(snoozeChoices.map(snoozeShort)).toEqual(['1d', '2d', '3d', '1w', '2w', '30d'])
  })

  it('ends a snooze at the same time of day', () => {
    expect(snoozeEnd(FIXED_NOW, 3).toISOString()).toBe('2026-06-18T12:00:00.000Z')
    expect(FIXED_NOW.toISOString()).toBe('2026-06-15T12:00:00.000Z')
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

describe('applyTriage', () => {
  const pinned = item({ id: 1, title: 'Pinned', pinnedAt: '2026-06-14T12:00:00Z', updatedAt: '2026-06-10T12:00:00Z' })
  const newest = item({ id: 2, title: 'Newest', updatedAt: '2026-06-15T11:00:00Z' })
  const older = item({ id: 3, title: 'Older', updatedAt: '2026-06-15T09:00:00Z' })
  const oldest = item({ id: 4, title: 'Oldest', updatedAt: '2026-06-15T09:00:00Z' })
  const ids = (list: { items: { id: number }[] }) => list.items.map((i) => i.id)

  it('changes the item in every list that holds it, keeping its place', () => {
    const inbox = snapshot({ reviewRequested: list([newest, older]), assigned: list([older]) })

    const next = applyTriage(inbox, { id: 3, set: { dismissedAt: FIXED_NOW.toISOString() } })

    expect(ids(next.reviewRequested)).toEqual([2, 3])
    expect(findItem(next, 3)?.dismissedAt).toBe(FIXED_NOW.toISOString())
    expect(next.assigned.items[0]?.dismissedAt).toBe(FIXED_NOW.toISOString())
    expect(next.authored).toBe(inbox.authored)
  })

  it('moves a newly pinned item to the top, as the most recently pinned', () => {
    const inbox = snapshot({ reviewRequested: list([pinned, newest, older]) })

    const next = applyTriage(inbox, { id: 3, set: { pinnedAt: FIXED_NOW.toISOString() } })

    expect(ids(next.reviewRequested)).toEqual([3, 1, 2])
  })

  it('puts an unpinned item back among the rest by its activity, then its ID', () => {
    const inbox = snapshot({ reviewRequested: list([pinned, newest, oldest]) })

    expect(ids(applyTriage(inbox, { id: 1, set: { pinnedAt: '' } }).reviewRequested)).toEqual([2, 4, 1])

    const tied = snapshot({ reviewRequested: list([{ ...older, pinnedAt: FIXED_NOW.toISOString() }, newest, oldest]) })

    expect(ids(applyTriage(tied, { id: 3, set: { pinnedAt: '' } }).reviewRequested)).toEqual([2, 4, 3])
  })

  it('puts an item unpinned while its repository is pinned among its repository’s pins', () => {
    const repoPinned = item({ id: 5, updatedAt: '2026-06-15T10:00:00Z', repoRule: 'pinned' })
    const both = item({ ...older, id: 6, pinnedAt: FIXED_NOW.toISOString(), repoRule: 'pinned' })
    const inbox = snapshot({ reviewRequested: list([both, pinned, repoPinned, newest, oldest]) })

    expect(ids(applyTriage(inbox, { id: 6, set: { pinnedAt: '' } }).reviewRequested)).toEqual([1, 5, 6, 2, 4])
    expect(ids(applyTriage(inbox, { id: 5, set: { pinnedAt: FIXED_NOW.toISOString() } }).reviewRequested)).toEqual([
      5, 6, 1, 2, 4,
    ])
  })

  it('puts an unpinned item last when everything else lists before it', () => {
    const inbox = snapshot({ reviewRequested: list([{ ...oldest, id: 9, pinnedAt: FIXED_NOW.toISOString() }, newest]) })

    expect(ids(applyTriage(inbox, { id: 9, set: { pinnedAt: '' } }).reviewRequested)).toEqual([2, 9])
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
  })

  it('finds the tab that holds an item', () => {
    const triaged = triageList(list([fresh, snoozed, dismissed]), FIXED_NOW)

    expect(tabOf(triaged, 1)).toBe('inbox')
    expect(tabOf(triaged, 2)).toBe('snoozed')
    expect(tabOf(triaged, 3)).toBe('dismissed')
    expect(tabOf(triaged, 9)).toBeNull()
    expect(tabOf(triageList(list([item({ id: 4, repoRule: 'muted' })]), FIXED_NOW), 4)).toBe('muted')
  })
})

describe('isPinned and isMuted', () => {
  it('counts an item pinned with its repository as pinned, but not pinned itself', () => {
    expect(isPinned(item({ pinnedAt: at(-1) }))).toBe(true)
    expect(isPinnedItself(item({ pinnedAt: at(-1) }))).toBe(true)
    expect(isPinned(item({ repoRule: 'pinned' }))).toBe(true)
    expect(isPinnedItself(item({ repoRule: 'pinned' }))).toBe(false)
    expect(isPinned(item({ repoRule: 'muted' }))).toBe(false)
    expect(isPinned(item())).toBe(false)
  })

  it('tells a muted item', () => {
    expect(isMuted(item({ repoRule: 'muted' }))).toBe(true)
    expect(isMuted(item({ repoRule: 'pinned' }))).toBe(false)
    expect(isMuted(item())).toBe(false)
  })
})

describe('itemsByRef', () => {
  it('finds an item by its repository, in any case, and number, in the first list that holds it', () => {
    const issue = item({ id: 4, kind: 'issue', number: 12, repo: 'ACME-org/Example-Repo' })
    const pull = item({ id: 3, number: 12, repo: 'acme-org/api' })
    const refs = itemsByRef(
      snapshot({
        authored: list([pull]),
        assigned: list([issue, pull]),
      }),
    )

    expect(refs.get('acme-org/example-repo#12')).toEqual({ item: issue, list: 'assigned' })
    expect(refs.get('acme-org/api#12')).toEqual({ item: pull, list: 'authored' })
    expect(refs.has('acme-org/example-repo#13')).toBe(false)
  })
})
