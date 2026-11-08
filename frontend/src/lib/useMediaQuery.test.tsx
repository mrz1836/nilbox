import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useMediaQuery } from './useMediaQuery'

// fakeMedia is a window whose media queries the test answers.
function fakeMedia(matches = false) {
  const listeners = new Set<() => void>()
  const list = {
    matches,
    addEventListener: (_type: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_type: string, listener: () => void) => listeners.delete(listener),
  }
  const matchMedia = vi.fn(() => list)
  const win = { matchMedia } as unknown as Window

  return {
    win,
    matchMedia,
    listeners,
    set(next: boolean) {
      list.matches = next
      for (const listener of listeners) {
        listener()
      }
    },
  }
}

describe('useMediaQuery', () => {
  it('reports whether the query matches, and follows each change', () => {
    const media = fakeMedia(true)
    const { result } = renderHook(() => useMediaQuery('(prefers-color-scheme: dark)', media.win))

    expect(result.current).toBe(true)
    expect(media.matchMedia).toHaveBeenCalledWith('(prefers-color-scheme: dark)')

    act(() => {
      media.set(false)
    })

    expect(result.current).toBe(false)
  })

  it('asks the window once for every component asking the same question, and stops listening', () => {
    const media = fakeMedia()
    const useReducedMotion = () => useMediaQuery('(prefers-reduced-motion: reduce)', media.win)
    const { unmount: unmountFirst } = renderHook(useReducedMotion)
    const { unmount: unmountSecond } = renderHook(useReducedMotion)

    expect(media.matchMedia).toHaveBeenCalledOnce()
    expect(media.listeners.size).toBe(2)

    unmountFirst()
    unmountSecond()

    expect(media.listeners.size).toBe(0)
  })

  it('never matches without matchMedia', () => {
    const { result } = renderHook(() => useMediaQuery('(prefers-color-scheme: dark)', {} as Window))

    expect(result.current).toBe(false)
  })
})
