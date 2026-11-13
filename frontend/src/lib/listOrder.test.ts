import { describe, expect, it } from 'vitest'

import type { InboxItem } from '../platform'
import { details, item, project } from '../test/fixtures'
import type { FilteredList } from './inboxFilter'
import { keptInPlace, ofKind, sheetCards, shownItems, shownOrder, statusRanking } from './listOrder'
import type { TriagedList } from './triage'

// issue is an issue numbered n, in a project at status, or in none.
function issue(n: number, status = ''): InboxItem {
  return item({
    id: n,
    number: n,
    kind: 'issue',
    details: details({ projects: status === '' ? [] : [project({ status, statusColor: '' })] }),
  })
}

const ids = (items: readonly InboxItem[]) => items.map((i) => i.id)

// triaged is a list whose own tab holds inbox, newest activity first, with one item in
// each of the other tabs.
function triaged(inbox: InboxItem[]): TriagedList {
  return { inbox, snoozed: [issue(90)], dismissed: [issue(91)], muted: [issue(92)], total: inbox.length }
}

const newestFirst = { byStatus: false, stored: [], kind: 'all' as const }
const byStatus = { byStatus: true, stored: [], kind: 'all' as const }

describe('shownItems', () => {
  const done = issue(1, 'Done')
  const backlog = issue(4, 'Backlog')
  const list = triaged([done, issue(2), issue(3, 'In progress'), backlog])

  it('keeps each tab in its own order while the list sorts by newest activity', () => {
    expect(ids(shownItems(list, 'inbox', undefined, newestFirst))).toEqual([1, 2, 3, 4])
    expect(ids(shownItems(list, 'snoozed', undefined, newestFirst))).toEqual([90])
    expect(ids(shownItems(list, 'dismissed', undefined, newestFirst))).toEqual([91])
    expect(ids(shownItems(list, 'muted', undefined, newestFirst))).toEqual([92])
  })

  it('orders the list’s own tab by project status, in the user’s order, and no other tab', () => {
    expect(ids(shownItems(list, 'inbox', undefined, byStatus))).toEqual([3, 4, 1, 2])
    const stored = ['Done', 'In progress', 'Backlog']

    expect(ids(shownItems(list, 'inbox', undefined, { byStatus: true, stored, kind: 'all' }))).toEqual([1, 3, 4, 2])
    expect(ids(shownItems(list, 'snoozed', undefined, byStatus))).toEqual([90])
  })

  it('shows only the kind the list shows on its own tab, sorted as it is, and every item on the others', () => {
    const pull = item({ id: 5, number: 5, kind: 'pr', details: details() })
    const mixed = triaged([done, pull, issue(3, 'In progress')])
    mixed.snoozed = [item({ id: 93, kind: 'pr' }), issue(90)]

    expect(ids(shownItems(mixed, 'inbox', undefined, { ...newestFirst, kind: 'pr' }))).toEqual([5])
    expect(ids(shownItems(mixed, 'inbox', undefined, { ...newestFirst, kind: 'issue' }))).toEqual([1, 3])
    expect(ids(shownItems(mixed, 'inbox', undefined, { ...byStatus, kind: 'issue' }))).toEqual([3, 1])
    expect(ids(shownItems(mixed, 'snoozed', undefined, { ...newestFirst, kind: 'issue' }))).toEqual([93, 90])
    expect(ids(ofKind(mixed.inbox, 'all'))).toEqual([1, 5, 3])
  })

  it('shows only the filter’s matches, ranked by the whole list’s statuses', () => {
    const matched: FilteredList = { inbox: [done, backlog], snoozed: [], dismissed: [], muted: [] }

    expect(ids(shownItems(list, 'inbox', matched, newestFirst))).toEqual([1, 4])
    expect(ids(shownItems(list, 'inbox', matched, byStatus))).toEqual([4, 1])
    expect(ids(shownItems(list, 'snoozed', matched, byStatus))).toEqual([])
  })
})

describe('shownOrder', () => {
  it('reads how the inbox sorts a list, and newest activity first while the order loads', () => {
    const order = { statusSorted: ['assigned' as const], statusOrder: ['Done'], kinds: { authored: 'issue' as const } }

    expect(shownOrder(order, 'assigned')).toEqual({ byStatus: true, stored: ['Done'], kind: 'all' })
    expect(shownOrder(order, 'authored')).toEqual({ byStatus: false, stored: ['Done'], kind: 'issue' })
    expect(shownOrder(null, 'assigned')).toEqual({ byStatus: false, stored: [], kind: 'all' })
  })
})

describe('statusRanking', () => {
  it('ranks the whole list’s statuses, the user’s order first', () => {
    const list = triaged([issue(1, 'Done'), issue(2, 'In progress')])

    expect(statusRanking(list, [])).toEqual(['In progress', 'Done'])
    expect(statusRanking(list, ['Done', 'In progress'])).toEqual(['Done', 'In progress'])
  })
})

describe('keptInPlace', () => {
  const all = [issue(1), issue(2), issue(3), issue(4)]

  it('keeps the shown cards as they are while they hold the sheet’s card, or the tab doesn’t', () => {
    const shown = [all[0], all[2]].filter((i) => i !== undefined)

    expect(keptInPlace(shown, all, 3)).toBe(shown)
    expect(keptInPlace(shown, all, 9)).toBe(shown)
  })

  it('puts the sheet’s card back where the tab places it among the shown cards', () => {
    const [one, two, three, four] = all

    expect(
      ids(
        keptInPlace(
          [one, four].filter((i) => i !== undefined),
          all,
          2,
        ),
      ),
    ).toEqual([1, 2, 4])
    expect(
      ids(
        keptInPlace(
          [two, three].filter((i) => i !== undefined),
          all,
          1,
        ),
      ),
    ).toEqual([1, 2, 3])
    expect(
      ids(
        keptInPlace(
          [one, two].filter((i) => i !== undefined),
          all,
          4,
        ),
      ),
    ).toEqual([1, 2, 4])
    expect(ids(keptInPlace([], all, 3))).toEqual([3])
  })
})

describe('sheetCards', () => {
  it('steps through the matches as shown, keeping the card the sheet shows', () => {
    const done = issue(1, 'Done')
    const doing = issue(3, 'In progress')
    const list = triaged([done, issue(2), doing])
    const matched: FilteredList = { inbox: [done], snoozed: [], dismissed: [], muted: [] }

    // Sorted by status, the card in progress comes first, and the sheet open on it keeps it.
    expect(ids(sheetCards(list, 'inbox', matched, byStatus, 3))).toEqual([3, 1])
    expect(ids(sheetCards(list, 'inbox', matched, newestFirst, 1))).toEqual([1])
    expect(ids(sheetCards(list, 'inbox', undefined, newestFirst, 2))).toEqual([1, 2, 3])
  })

  it('keeps a card of the kind the list leaves out in its place among the cards of either kind', () => {
    const pull = item({ id: 2, number: 2, kind: 'pr', details: details() })
    const list = triaged([issue(1), pull, issue(3)])

    expect(ids(sheetCards(list, 'inbox', undefined, { ...newestFirst, kind: 'issue' }, 1))).toEqual([1, 3])
    expect(ids(sheetCards(list, 'inbox', undefined, { ...newestFirst, kind: 'issue' }, 2))).toEqual([1, 2, 3])
  })
})
