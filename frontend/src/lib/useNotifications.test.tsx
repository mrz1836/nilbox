import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { sendEvent } from '../test/events'
import { notification } from '../test/fixtures'
import { useNotifications } from './useNotifications'

const platform = vi.hoisted(() => ({
  listNotifications: vi.fn(),
  markNotificationsRead: vi.fn(),
  clearNotifications: vi.fn(),
  on: vi.fn(),
}))

vi.mock('../platform', () => platform)

const stopListening = vi.fn()

// deferred is a call the test settles.
function deferred() {
  let resolve: () => void = () => undefined
  let reject: (err: unknown) => void = () => undefined
  const promise = new Promise<void>((res, rej) => {
    resolve = res
    reject = rej
  })

  return { promise, resolve, reject }
}

beforeEach(() => {
  platform.listNotifications.mockResolvedValue({ items: [notification()], unread: 1 })
  platform.markNotificationsRead.mockResolvedValue(undefined)
  platform.clearNotifications.mockResolvedValue(undefined)
  platform.on.mockReturnValue(stopListening)
})

describe('useNotifications', () => {
  it('loads the history, and again whenever it changes, until unmounted', async () => {
    const { result, unmount } = renderHook(() => useNotifications())

    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    platform.listNotifications.mockResolvedValue({
      items: [notification({ id: 2 }), notification({ read: true })],
      unread: 1,
    })
    act(() => {
      sendEvent(platform.on, 'notifications:updated')
    })

    await waitFor(() => {
      expect(result.current.log.items).toHaveLength(2)
    })

    unmount()
    expect(stopListening).toHaveBeenCalledOnce()
  })

  it('shows only the latest load’s answer', async () => {
    let answerFirst: (log: unknown) => void = () => undefined
    platform.listNotifications.mockReturnValueOnce(
      new Promise((resolve) => {
        answerFirst = resolve
      }),
    )

    const { result } = renderHook(() => useNotifications())
    platform.listNotifications.mockResolvedValue({ items: [], unread: 0 })
    act(() => {
      sendEvent(platform.on, 'notifications:updated')
    })

    await act(async () => {
      answerFirst({ items: [notification()], unread: 1 })
      await Promise.resolve()
    })

    expect(result.current.log).toEqual({ items: [], unread: 0 })
  })

  it('reports a history that could not be loaded', async () => {
    platform.listNotifications.mockRejectedValue(new Error('database is locked'))

    const { result } = renderHook(() => useNotifications())

    await waitFor(() => {
      expect(result.current.error).toBe('database is locked')
    })
  })

  it('marks everything read at once, and keeps it read once stored', async () => {
    const stored = deferred()
    platform.markNotificationsRead.mockReturnValue(stored.promise)
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    act(() => {
      result.current.markRead()
    })

    expect(result.current.log.unread).toBe(0)
    expect(result.current.log.items[0]?.read).toBe(true)
    expect(platform.markNotificationsRead).toHaveBeenCalledOnce()

    await act(async () => {
      stored.resolve()
      await stored.promise
    })

    expect(result.current.log.unread).toBe(0)
    expect(result.current.markError).toBe('')
  })

  it('puts the entries back to unread, and says why, when they cannot be marked read', async () => {
    const stored = deferred()
    platform.markNotificationsRead.mockReturnValue(stored.promise)
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    act(() => {
      result.current.markRead()
    })

    expect(result.current.log.unread).toBe(0)

    await act(async () => {
      stored.reject(new Error('database is locked'))
      await stored.promise.catch(() => undefined)
    })

    expect(result.current.log.unread).toBe(1)
    expect(result.current.log.items[0]?.read).toBe(false)
    expect(result.current.markError).toBe('database is locked')
  })

  it('clears the history', async () => {
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    await act(() => result.current.clear())

    expect(result.current.log).toEqual({ items: [], unread: 0 })

    platform.clearNotifications.mockRejectedValue(new Error('busy'))
    await expect(result.current.clear()).rejects.toThrow('busy')
  })
})
