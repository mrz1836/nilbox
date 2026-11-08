import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { InboxSnapshot } from '../platform'
import { withClock } from '../test/clock'
import { item, list, snapshot } from '../test/fixtures'
import { createClock } from './useNow'
import { useTriage } from './useTriage'

afterEach(() => {
  vi.useRealTimers()
})

const inbox = snapshot({
  reviewRequested: list([item({ id: 1 }), item({ id: 2, number: 8, snoozedUntil: '2026-06-15T12:30:00Z' })]),
})

// triage renders useTriage over a clock that starts at noon, counting its renders.
function triage(initial: InboxSnapshot | null) {
  vi.useFakeTimers({ now: new Date('2026-06-15T12:00:00Z'), toFake: ['Date', 'setInterval', 'clearInterval'] })
  const renders = { count: 0 }
  const view = renderHook(
    ({ snapshot: current }: { snapshot: InboxSnapshot | null }) => {
      renders.count += 1

      return useTriage(current)
    },
    { initialProps: { snapshot: initial }, wrapper: withClock(createClock()) },
  )

  return { ...view, renders }
}

describe('useTriage', () => {
  it('is null until the inbox loads', () => {
    expect(triage(null).result.current).toBeNull()
  })

  it('sorts the inbox, and renders again only as a snooze ends', () => {
    const { result, renders } = triage(inbox)

    expect(result.current?.lists.reviewRequested.inbox.map((i) => i.id)).toEqual([1])
    expect(result.current?.count).toBe(1)

    const before = renders.count
    const sorted = result.current

    act(() => {
      vi.advanceTimersByTime(29 * 60_000)
    })

    expect(renders.count).toBe(before)
    expect(result.current).toBe(sorted)

    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    expect(renders.count).toBe(before + 1)
    expect(result.current?.lists.reviewRequested.inbox.map((i) => i.id)).toEqual([1, 2])
    expect(result.current?.count).toBe(2)
  })
})
