import { describe, expect, it } from 'vitest'

import { details, item, itemResolution, list, rate, snapshot } from '../test/fixtures'
import { same, shareResolutions, shareSnapshot } from './share'

// decoded is a value as the backend sends it again: the same data in new objects.
function decoded<T>(value: T): T {
  return structuredClone(value)
}

describe('same', () => {
  it('compares plain values', () => {
    expect(same(1, 1)).toBe(true)
    expect(same('a', 'a')).toBe(true)
    expect(same(null, null)).toBe(true)
    expect(same(Number.NaN, Number.NaN)).toBe(true)
    expect(same(1, '1')).toBe(false)
    expect(same(null, {})).toBe(false)
    expect(same({}, null)).toBe(false)
    expect(same(undefined, null)).toBe(false)
  })

  it('compares arrays and objects by what they hold', () => {
    expect(same(item({ details: details() }), decoded(item({ details: details() })))).toBe(true)
    expect(same([1, [2, 3]], [1, [2, 3]])).toBe(true)
    expect(same([1, 2], [1, 2, 3])).toBe(false)
    expect(same([1, 2], { 0: 1, 1: 2 })).toBe(false)
    expect(same({ a: 1 }, { a: 1, b: 2 })).toBe(false)
    expect(same({ a: 1, b: undefined }, { a: 1, c: undefined })).toBe(false)
    expect(same(item(), item({ title: 'Another title' }))).toBe(false)
  })
})

describe('shareSnapshot', () => {
  const one = item({ id: 1 })
  const two = item({ id: 2, number: 8 })
  const three = item({ id: 3, number: 9 })
  const was = snapshot({
    reviewRequested: list([one, two]),
    authored: list([three]),
    rate: { graphql: rate(), core: rate(), search: rate() },
  })

  it('takes everything as it is the first time', () => {
    expect(shareSnapshot(null, was)).toBe(was)
  })

  it('keeps the whole inbox when nothing changed', () => {
    expect(shareSnapshot(was, decoded(was))).toBe(was)
  })

  it('keeps each unchanged item, list, account, and reading, and takes what changed', () => {
    const next = decoded(was)
    next.reviewRequested.items[1] = { ...two, title: 'Renamed' }
    next.lastRefreshedAt = '2026-06-15T12:00:00Z'

    const shared = shareSnapshot(was, next)

    expect(shared).not.toBe(was)
    expect(shared.lastRefreshedAt).toBe('2026-06-15T12:00:00Z')
    expect(shared.reviewRequested).not.toBe(was.reviewRequested)
    expect(shared.reviewRequested.items[0]).toBe(one)
    expect(shared.reviewRequested.items[1]).toEqual({ ...two, title: 'Renamed' })
    expect(shared.authored).toBe(was.authored)
    expect(shared.assigned).toBe(was.assigned)
    expect(shared.account).toBe(was.account)
    expect(shared.rate).toBe(was.rate)
  })

  it('keeps an item that moved to another list, and a list whose count changed is new', () => {
    const next = decoded(snapshot({ ...was, reviewRequested: list([one]), assigned: list([two], 4) }))

    const shared = shareSnapshot(was, next)

    expect(shared.assigned.items[0]).toBe(two)
    expect(shared.assigned.total).toBe(4)
    expect(shared.reviewRequested.items).toEqual([one])
    expect(shared.reviewRequested).not.toBe(was.reviewRequested)
  })

  it('takes a list whose total changed though its items did not', () => {
    const next = decoded(was)
    next.authored.total = 40

    const shared = shareSnapshot(was, next)

    expect(shared.authored).not.toBe(was.authored)
    expect(shared.authored.items[0]).toBe(three)
  })
})

describe('shareResolutions', () => {
  const first = itemResolution({ itemId: 1 })
  const second = itemResolution({ itemId: 2 })
  const was = new Map([
    [1, first],
    [2, second],
  ])

  it('keeps the map when nothing changed', () => {
    expect(shareResolutions(was, decoded([first, second]))).toBe(was)
  })

  it('keeps each unchanged resolution in a new map when one changed', () => {
    const changed = itemResolution({ itemId: 2, problem: 'no clone root' })
    const shared = shareResolutions(was, decoded([first, changed]))

    expect(shared).not.toBe(was)
    expect(shared.get(1)).toBe(first)
    expect(shared.get(2)).toEqual(changed)
  })

  it('makes a new map when an item came or went', () => {
    const third = itemResolution({ itemId: 3 })

    expect(shareResolutions(was, [first]).size).toBe(1)

    const grown = shareResolutions(was, decoded([first, second, third]))
    expect(grown.size).toBe(3)
    expect(grown.get(2)).toBe(second)
  })
})
