import { act, render, renderHook, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { FIXED_NOW, fromNow, withClock } from '../test/clock'
import { createClock, useClock, useNow, useNowAs } from './useNow'

afterEach(() => {
  vi.useRealTimers()
})

// Reading shows the time the clock reads, as a component that shows the time would.
function Reading({ label }: { label: string }) {
  return <p aria-label={label}>{useNow().toISOString()}</p>
}

describe('useNow', () => {
  it('moves on every interval, rendering only what reads it', () => {
    vi.useFakeTimers({ now: FIXED_NOW })
    const clock = createClock(() => new Date(), 60_000)
    const { result } = renderHook(() => useNow(), { wrapper: withClock(clock) })

    expect(result.current.toISOString()).toBe('2026-06-15T12:00:00.000Z')

    act(() => {
      vi.advanceTimersByTime(59_999)
    })

    expect(result.current.toISOString()).toBe('2026-06-15T12:00:00.000Z')

    act(() => {
      vi.advanceTimersByTime(1)
    })

    expect(result.current.toISOString()).toBe('2026-06-15T12:01:00.000Z')
  })

  it('shares one interval among every reader, and stops it once none is left', () => {
    vi.useFakeTimers({ now: FIXED_NOW })
    const started = vi.spyOn(globalThis, 'setInterval')
    const stopped = vi.spyOn(globalThis, 'clearInterval')
    const clock = createClock(() => new Date(), 60_000)

    const view = render(
      <>
        <Reading label="first" />
        <Reading label="second" />
      </>,
      { wrapper: withClock(clock) },
    )

    expect(started).toHaveBeenCalledOnce()

    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    expect(screen.getByLabelText('first')).toHaveTextContent('2026-06-15T12:01:00.000Z')
    expect(screen.getByLabelText('second')).toHaveTextContent('2026-06-15T12:01:00.000Z')

    view.unmount()

    expect(stopped).toHaveBeenCalledOnce()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('reads the time afresh for the first reader after a pause, and not for a shorter one', () => {
    vi.useFakeTimers({ now: FIXED_NOW })
    const clock = createClock(() => new Date(), 60_000)
    const wrapper = withClock(clock)

    renderHook(() => useNow(), { wrapper }).unmount()

    vi.setSystemTime(fromNow(30_000))
    const { result: soon, unmount } = renderHook(() => useNow(), { wrapper })
    expect(soon.current.toISOString()).toBe('2026-06-15T12:00:00.000Z')
    unmount()

    vi.setSystemTime(fromNow(3_600_000))
    const { result: later } = renderHook(() => useNow(), { wrapper })
    expect(later.current.toISOString()).toBe('2026-06-15T13:00:00.000Z')
  })

  it('moves on when the time is read afresh, and stays put when it has not changed', () => {
    vi.useFakeTimers({ now: FIXED_NOW })
    const clock = createClock(() => new Date(), 60_000)
    const { result } = renderHook(() => ({ now: useNow(), clock: useClock() }), { wrapper: withClock(clock) })
    const first = result.current.now

    act(() => {
      expect(result.current.clock.read()).toEqual(first)
    })
    expect(result.current.now).toBe(first)

    vi.setSystemTime(fromNow(20_000))
    act(() => {
      result.current.clock.read()
    })

    expect(result.current.now.toISOString()).toBe('2026-06-15T12:00:20.000Z')
  })

  it('reads the window’s clock by default', () => {
    const { result } = renderHook(() => useNow())

    expect(Math.abs(result.current.getTime() - Date.now())).toBeLessThan(60_000)
  })
})

describe('useNowAs', () => {
  it('renders again only when what it shows changes', () => {
    vi.useFakeTimers({ now: FIXED_NOW })
    const clock = createClock(() => new Date(), 60_000)
    let times = 0

    const { result } = renderHook(
      () => {
        times += 1

        return useNowAs((now) => now.getUTCHours())
      },
      { wrapper: withClock(clock) },
    )

    expect(result.current).toBe(12)
    const first = times

    act(() => {
      vi.advanceTimersByTime(59 * 60_000)
    })

    expect(result.current).toBe(12)
    expect(times).toBe(first)

    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    expect(result.current).toBe(13)
    expect(times).toBe(first + 1)
  })
})
