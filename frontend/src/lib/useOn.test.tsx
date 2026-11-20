import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { emit, listening, platform } from '../test/platform'
import { useOnAutopilot } from './useAutopilot'
import { useOn } from './useOn'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useOn', () => {
  it('hands the listener each event’s data from the moment it mounts until it unmounts', () => {
    const heard = vi.fn()

    const { unmount } = renderHook(() => {
      useOn('scriptruns:progress', heard)
    })

    expect(platform.on).toHaveBeenCalledOnce()
    expect(listening('scriptruns:progress')).toBe(1)

    act(() => {
      emit('scriptruns:progress', { id: 7 })
    })

    expect(heard).toHaveBeenCalledExactlyOnceWith({ id: 7 })

    unmount()

    expect(listening('scriptruns:progress')).toBe(0)
  })

  it('calls the latest listener rendered, without subscribing again', () => {
    const first = vi.fn()
    const second = vi.fn()

    const { rerender } = renderHook(
      ({ listener }: { listener: () => void }) => {
        useOn('sessions:updated', listener)
      },
      { initialProps: { listener: first } },
    )

    rerender({ listener: second })

    act(() => {
      emit('sessions:updated')
    })

    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledOnce()
    expect(platform.on).toHaveBeenCalledOnce()
    expect(listening('sessions:updated')).toBe(1)
  })

  it('listens only while enabled', () => {
    const heard = vi.fn()

    const { rerender, unmount } = renderHook(
      ({ enabled }: { enabled: boolean }) => {
        useOn('hooks:updated', heard, enabled)
      },
      { initialProps: { enabled: false } },
    )

    expect(platform.on).not.toHaveBeenCalled()
    expect(listening('hooks:updated')).toBe(0)

    rerender({ enabled: true })

    expect(listening('hooks:updated')).toBe(1)

    act(() => {
      emit('hooks:updated')
    })

    expect(heard).toHaveBeenCalledOnce()

    rerender({ enabled: false })

    expect(listening('hooks:updated')).toBe(0)

    rerender({ enabled: true })
    unmount()

    expect(listening('hooks:updated')).toBe(0)
    expect(platform.on).toHaveBeenCalledTimes(2)
  })

  it('moves to an event named anew', () => {
    const heard = vi.fn()

    const { rerender } = renderHook(
      ({ event }: { event: 'review:progress' | 'issue:progress' }) => {
        useOn(event, heard)
      },
      { initialProps: { event: 'review:progress' } },
    )

    rerender({ event: 'issue:progress' })

    expect(listening('review:progress')).toBe(0)
    expect(listening('issue:progress')).toBe(1)

    act(() => {
      emit('issue:progress', { itemId: 3, step: 'Merging' })
    })

    expect(heard).toHaveBeenCalledExactlyOnceWith({ itemId: 3, step: 'Merging' })
  })
})

describe('useOnAutopilot', () => {
  it('calls the latest reload on every change, with nothing', () => {
    const first = vi.fn()
    const second = vi.fn()

    const { rerender, unmount } = renderHook(
      ({ reload }: { reload: () => void }) => {
        useOnAutopilot(reload)
      },
      { initialProps: { reload: first } },
    )

    rerender({ reload: second })

    act(() => {
      emit('autopilot:updated')
    })

    expect(first).not.toHaveBeenCalled()
    expect(second).toHaveBeenCalledExactlyOnceWith()

    unmount()

    expect(listening('autopilot:updated')).toBe(0)
  })
})
