import { describe, expect, it } from 'vitest'

import type { InboxItem } from '../platform'
import { details, item, project } from '../test/fixtures'
import { moveStatus, sortByStatus, statusesOf, statusOrder } from './statusOrder'

// issue is an issue numbered n, in a project at each status given.
function issue(n: number, ...statuses: string[]): InboxItem {
  return item({
    id: n,
    number: n,
    kind: 'issue',
    details: details({ projects: statuses.map((status) => project({ status, statusColor: '' })) }),
  })
}

const ids = (items: readonly InboxItem[]) => items.map((i) => i.id)

describe('statusOrder', () => {
  it('places statuses by what their names say: work under way first, work set aside and done last', () => {
    expect(
      statusOrder(
        [],
        ['Done', 'Deferred', 'Backlog', 'Ready', 'In progress', 'In review', 'Blocked', 'Needs design', 'Todo'],
      ),
    ).toEqual(['In progress', 'In review', 'Ready', 'Todo', 'Needs design', 'Backlog', 'Blocked', 'Deferred', 'Done'])
  })

  it('settles names that say two things', () => {
    expect(statusOrder([], ['Waiting for review', 'Not started', 'Ready for review', 'Waiting'])).toEqual([
      'Ready for review',
      'Waiting for review',
      'Not started',
      'Waiting',
    ])
  })

  it('keeps the user’s order, and places a new status after the last placed at its stage or earlier', () => {
    expect(statusOrder(['Deferred', 'In progress', 'Backlog'], ['Backlog', 'Ready', 'In progress'])).toEqual([
      'Deferred',
      'In progress',
      'Ready',
      'Backlog',
    ])
  })

  it('places a new status first when every placed one is at a later stage', () => {
    expect(statusOrder(['Done', 'Backlog'], ['In progress'])).toEqual(['In progress', 'Done', 'Backlog'])
  })

  it('matches names ignoring case and spaces, and keeps each once', () => {
    expect(statusOrder([' In Progress ', 'in progress'], ['IN PROGRESS', 'Ready', ' ready'])).toEqual([
      'In Progress',
      'Ready',
    ])
  })
})

describe('statusesOf', () => {
  it('lists each status once, as first met, with its first color', () => {
    const items = [
      item({ details: details({ projects: [project({ status: 'Ready', statusColor: '#218bff' })] }) }),
      item({
        id: 2,
        details: details({
          projects: [project({ status: 'ready', statusColor: '#000000' }), project({ status: '', statusColor: '' })],
        }),
      }),
      item({ id: 3, details: null }),
      item({ id: 4, details: details({ projects: [project({ status: 'In progress' })] }) }),
    ]

    expect(statusesOf(items)).toEqual([
      { name: 'Ready', color: '#218bff' },
      { name: 'In progress', color: '#bf8700' },
    ])
  })
})

describe('sortByStatus', () => {
  const order = ['In progress', 'Ready', 'Deferred']

  it('sorts by status in the order, keeping newest first within one, and the rest last', () => {
    const items = [issue(1, 'Deferred'), issue(2), issue(3, 'Ready'), issue(4, 'In progress'), issue(5, 'Ready')]

    expect(ids(sortByStatus(items, order))).toEqual([4, 3, 5, 1, 2])
  })

  it('puts items without details, or in a project without a status, with those without a project', () => {
    const noDetails = item({ id: 6, kind: 'issue', details: null })

    expect(ids(sortByStatus([issue(1, ''), noDetails, issue(3, 'Deferred')], order))).toEqual([3, 1, 6])
  })

  it('ranks an issue in several projects by its best status', () => {
    expect(ids(sortByStatus([issue(1, 'Ready'), issue(2, 'Deferred', 'In progress')], order))).toEqual([2, 1])
  })

  it('keeps the pinned items first, as they were', () => {
    const pinned = { ...issue(1, 'Deferred'), pinnedAt: '2026-06-15T10:00:00Z' }

    expect(ids(sortByStatus([pinned, issue(2, 'Ready'), issue(3, 'In progress')], order))).toEqual([1, 3, 2])
  })

  it('leaves the items it was given as they were', () => {
    const items = [issue(1, 'Deferred'), issue(2, 'In progress')]

    sortByStatus(items, order)

    expect(ids(items)).toEqual([1, 2])
  })
})

describe('moveStatus', () => {
  const order = ['In progress', 'Gone', 'Ready', 'Deferred']
  const shown = ['In progress', 'Ready', 'Deferred']

  it('swaps a status with its neighbor among those shown, keeping the others in place', () => {
    expect(moveStatus(order, shown, 'Ready', -1)).toEqual(['Ready', 'Gone', 'In progress', 'Deferred'])
    expect(moveStatus(order, shown, 'ready', 1)).toEqual(['In progress', 'Gone', 'Deferred', 'Ready'])
  })

  it('leaves the order alone at an end, or for a status not shown', () => {
    expect(moveStatus(order, shown, 'In progress', -1)).toEqual(order)
    expect(moveStatus(order, shown, 'Deferred', 1)).toEqual(order)
    expect(moveStatus(order, shown, 'Gone', 1)).toEqual(order)
  })
})
