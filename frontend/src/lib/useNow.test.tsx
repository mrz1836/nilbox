import { act, render, renderHook, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { createClock, useNow, type Clock } from './useNow'

afterEach(() => {
  vi.useRealTimers()
})

// Reading shows the time a clock reads, as a component that shows the time would.
function Reading({ clock, label }: { clock: Clock; label: string }) {
  return <p aria-label={label}>{useNow(clock).toISOString()}</p>
}

describe('useNow', () => {
  it('moves on every interval, rendering only what reads it', () => {
    vi.useFakeTimers({ now: new Date('2026-06-15T12:00:00Z') })
    const clock = createClock(() => new Date(), 60_000)
    const { result } = renderHook(() => useNow(clock))

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
    vi.useFakeTimers({ now: new Date('2026-06-15T12:00:00Z') })
    const started = vi.spyOn(globalThis, 'setInterval')
    const stopped = vi.spyOn(globalThis, 'clearInterval')
    const clock = createClock(() => new Date(), 60_000)

    const view = render(
      <>
        <Reading clock={clock} label="first" />
        <Reading clock={clock} label="second" />
      </>,
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
    vi.useFakeTimers({ now: new Date('2026-06-15T12:00:00Z') })
    const clock = createClock(() => new Date(), 60_000)

    renderHook(() => useNow(clock)).unmount()

    vi.setSystemTime(new Date('2026-06-15T12:00:30Z'))
    const { result: soon, unmount } = renderHook(() => useNow(clock))
    expect(soon.current.toISOString()).toBe('2026-06-15T12:00:00.000Z')
    unmount()

    vi.setSystemTime(new Date('2026-06-15T13:00:00Z'))
    const { result: later } = renderHook(() => useNow(clock))
    expect(later.current.toISOString()).toBe('2026-06-15T13:00:00.000Z')
  })

  it('reads the window’s clock by default', () => {
    const { result } = renderHook(() => useNow())

    expect(Math.abs(result.current.getTime() - Date.now())).toBeLessThan(60_000)
  })
})
