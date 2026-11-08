import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Settings, Theme } from '../platform'
import { platform } from '../test/platform'
import type { Result } from './call'
import { useTheme } from './useTheme'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// deferred is a promise the test settles.
function deferred<T>() {
  let resolve: (value: T) => void = () => undefined
  let reject: (err: unknown) => void = () => undefined
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })

  return { promise, resolve, reject }
}

// fakeWindow is a window over a document of its own whose appearance the test controls,
// with view transitions that run their update at once and are counted.
function fakeWindow(dark = false) {
  const listeners = new Set<() => void>()
  const darkQuery = {
    matches: dark,
    addEventListener: (_type: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_type: string, listener: () => void) => listeners.delete(listener),
  }
  const doc = document.implementation.createHTMLDocument('')
  const startViewTransition = vi.fn((update: () => void) => {
    update()

    return { ready: Promise.resolve() }
  })

  Object.assign(doc, { startViewTransition })

  const matchMedia = (query: string) => (query === '(prefers-color-scheme: dark)' ? darkQuery : { matches: false })
  const win = { document: doc, matchMedia } as unknown as Window

  return {
    win,
    root: doc.documentElement,
    startViewTransition,
    listeners,
    setDark(next: boolean) {
      darkQuery.matches = next
      for (const listener of listeners) {
        listener()
      }
    },
  }
}

describe('useTheme', () => {
  it('pins the appearance before the stored theme loads, without a cross-fade', async () => {
    const stored = deferred<Theme>()
    platform.getTheme.mockReturnValue(stored.promise)
    const appearance = fakeWindow(true)

    const { result } = renderHook(() => useTheme(appearance.win))

    expect(result.current.theme).toBe('system')
    expect(result.current.dark).toBe(true)
    expect(appearance.root.dataset.theme).toBe('dark')
    expect(appearance.startViewTransition).not.toHaveBeenCalled()

    await act(async () => {
      stored.resolve('light')
      await stored.promise
    })

    expect(result.current.theme).toBe('light')
    expect(appearance.root.dataset.theme).toBe('light')
    expect(appearance.startViewTransition).toHaveBeenCalledOnce()
  })

  it('follows the appearance under System, cross-fading each change', async () => {
    const appearance = fakeWindow(false)
    const { result } = renderHook(() => useTheme(appearance.win))
    await waitFor(() => {
      expect(platform.getTheme).toHaveBeenCalledOnce()
    })

    act(() => {
      appearance.setDark(true)
    })

    expect(result.current.dark).toBe(true)
    expect(appearance.root.dataset.theme).toBe('dark')
    expect(appearance.startViewTransition).toHaveBeenCalledOnce()
  })

  it('keeps a Light or Dark theme when the appearance changes', async () => {
    platform.getTheme.mockResolvedValue('dark')
    const appearance = fakeWindow(false)
    const { result } = renderHook(() => useTheme(appearance.win))
    await waitFor(() => {
      expect(result.current.theme).toBe('dark')
    })

    act(() => {
      appearance.setDark(true)
      appearance.setDark(false)
    })

    expect(appearance.root.dataset.theme).toBe('dark')
  })

  it('switches to a chosen theme at once and stores it', async () => {
    const appearance = fakeWindow(false)
    const { result } = renderHook(() => useTheme(appearance.win))
    await waitFor(() => {
      expect(platform.getTheme).toHaveBeenCalledOnce()
    })

    let stored: Result<void> | undefined
    await act(async () => {
      stored = await result.current.chooseTheme('dark')
    })

    expect(stored).toEqual({ ok: true, data: undefined })
    expect(result.current.theme).toBe('dark')
    expect(appearance.root.dataset.theme).toBe('dark')
    expect(appearance.root.style.colorScheme).toBe('dark')
    expect(appearance.startViewTransition).toHaveBeenCalledOnce()
    expect(platform.setTheme).toHaveBeenCalledExactlyOnceWith('dark')
  })

  it('goes back to the theme it showed when a choice cannot be stored', async () => {
    platform.getTheme.mockResolvedValue('light')
    platform.setTheme.mockRejectedValue(new Error('database is locked'))
    const appearance = fakeWindow(false)
    const { result } = renderHook(() => useTheme(appearance.win))
    await waitFor(() => {
      expect(result.current.theme).toBe('light')
    })

    let failure: Result<void> | undefined
    await act(async () => {
      failure = await result.current.chooseTheme('dark')
    })

    expect(failure).toEqual({ ok: false, error: 'database is locked' })
    expect(result.current.theme).toBe('light')
    expect(appearance.root.dataset.theme).toBe('light')
  })

  it('keeps a newer choice when an older one fails to store, and stores them in order', async () => {
    const first = deferred<Settings>()
    platform.setTheme.mockReturnValueOnce(first.promise)
    const appearance = fakeWindow(false)
    const { result } = renderHook(() => useTheme(appearance.win))
    await waitFor(() => {
      expect(platform.getTheme).toHaveBeenCalledOnce()
    })

    let dark: Promise<Result<void>> | undefined
    act(() => {
      dark = result.current.chooseTheme('dark')
    })
    let light: Promise<Result<void>> | undefined
    act(() => {
      light = result.current.chooseTheme('light')
    })

    // The second store waits for the first.
    await waitFor(() => {
      expect(platform.setTheme).toHaveBeenCalledOnce()
    })
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(platform.setTheme).toHaveBeenCalledExactlyOnceWith('dark')

    await act(async () => {
      first.reject(new Error('database is locked'))
      expect(await dark).toEqual({ ok: false, error: 'database is locked' })
      expect(await light).toEqual({ ok: true, data: undefined })
    })

    expect(platform.setTheme).toHaveBeenLastCalledWith('light')
    expect(result.current.theme).toBe('light')
    expect(appearance.root.dataset.theme).toBe('light')
  })

  it('never lets a stored theme that loads late override a choice', async () => {
    const stored = deferred<Theme>()
    platform.getTheme.mockReturnValue(stored.promise)
    const appearance = fakeWindow(false)
    const { result } = renderHook(() => useTheme(appearance.win))

    await act(() => result.current.chooseTheme('dark'))
    await act(async () => {
      stored.resolve('light')
      await stored.promise
    })

    expect(result.current.theme).toBe('dark')
    expect(appearance.root.dataset.theme).toBe('dark')
  })

  it('logs a theme that could not be read and follows the appearance', async () => {
    const failure = new Error('database is locked')
    platform.getTheme.mockRejectedValue(failure)
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const appearance = fakeWindow(true)
    const { result } = renderHook(() => useTheme(appearance.win))

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not read the theme', failure)
    })
    expect(result.current.theme).toBe('system')
    expect(appearance.root.dataset.theme).toBe('dark')
  })

  it('stops following the appearance when unmounted', async () => {
    const appearance = fakeWindow(false)
    const { unmount } = renderHook(() => useTheme(appearance.win))
    await waitFor(() => {
      expect(platform.getTheme).toHaveBeenCalledOnce()
    })
    expect(appearance.listeners.size).toBe(1)

    unmount()

    expect(appearance.listeners.size).toBe(0)
  })
})
