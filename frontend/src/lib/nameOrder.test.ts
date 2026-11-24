import { describe, expect, it } from 'vitest'

import type { InboxItem } from '../platform'
import { item } from '../test/fixtures'
import { sortByAuthor, sortByName, sortByRepo } from './nameOrder'

const ids = (items: readonly InboxItem[]) => items.map((i) => i.id)

// card is item id, in repo by author, pinned when pinnedAt is set.
function card(id: number, repo: string, author: string, pinnedAt = ''): InboxItem {
  return item({ id, number: id, repo, author, pinnedAt })
}

describe('sortByName', () => {
  it('sorts A–Z by the name, ignoring case', () => {
    const items = [card(1, 'b', 'x'), card(2, 'C', 'x'), card(3, 'a', 'x'), card(4, 'B', 'x')]

    expect(ids(sortByName(items, (i) => i.repo))).toEqual([3, 1, 4, 2])
  })

  it('keeps the items of one name newest first, as they came', () => {
    const items = [card(1, 'web', 'x'), card(2, 'api', 'x'), card(3, 'web', 'x'), card(4, 'api', 'x')]

    expect(ids(sortByName(items, (i) => i.repo))).toEqual([2, 4, 1, 3])
  })

  it('orders the numbers in names by value', () => {
    const items = [card(1, 'repo-10', 'x'), card(2, 'repo-2', 'x'), card(3, 'repo-1', 'x')]

    expect(ids(sortByName(items, (i) => i.repo))).toEqual([3, 2, 1])
  })

  it('keeps the pinned items first, as they were', () => {
    const items = [
      card(1, 'zeta', 'x', '2026-06-15T10:00:00Z'),
      card(2, 'beta', 'x'),
      { ...card(3, 'omega', 'x'), repoRule: 'pinned' as const },
      card(4, 'alpha', 'x'),
    ]

    expect(ids(sortByName(items, (i) => i.repo))).toEqual([1, 3, 4, 2])
  })

  it('leaves the items it was given as they were', () => {
    const items = [card(1, 'b', 'x'), card(2, 'a', 'x')]

    sortByName(items, (i) => i.repo)

    expect(ids(items)).toEqual([1, 2])
  })
})

describe('sortByRepo and sortByAuthor', () => {
  const items = [
    card(1, 'acme-org/web', 'zoe'),
    card(2, 'acme-org/api', 'Bo'),
    card(3, 'example-org/api', 'ana'),
    card(4, 'acme-org/web', 'bo'),
  ]

  it('groups by repository, owner first', () => {
    expect(ids(sortByRepo(items))).toEqual([2, 1, 4, 3])
  })

  it('groups by author login', () => {
    expect(ids(sortByAuthor(items))).toEqual([3, 2, 4, 1])
  })
})
