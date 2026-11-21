import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { NotificationLog } from '../platform'
import { deferred } from '../test/async'
import { notification } from '../test/fixtures'
import { listening, platform, send } from '../test/platform'
import { useNotifications } from './useNotifications'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

beforeEach(() => {
  platform.listNotifications.mockResolvedValue({ items: [notification()], unread: 1 })
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
    send('notifications:updated')

    await waitFor(() => {
      expect(result.current.log.items).toHaveLength(2)
    })

    expect(listening('notifications:updated')).toBe(1)
    unmount()
    expect(listening('notifications:updated')).toBe(0)
  })

  it('shows only the latest load’s answer', async () => {
    const first = deferred<NotificationLog>()
    platform.listNotifications.mockReturnValueOnce(first.promise)

    const { result } = renderHook(() => useNotifications())
    platform.listNotifications.mockResolvedValue({ items: [], unread: 0 })
    send('notifications:updated')

    await act(async () => {
      first.resolve({ items: [notification()], unread: 1 })
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
    platform.markAllNotificationsRead.mockReturnValue(stored.promise)
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    act(() => {
      result.current.markAllRead()
    })

    expect(result.current.log.unread).toBe(0)
    expect(result.current.log.items[0]?.read).toBe(true)
    expect(platform.markAllNotificationsRead).toHaveBeenCalledOnce()

    await act(async () => {
      stored.resolve()
      await stored.promise
    })

    expect(result.current.log.unread).toBe(0)
    expect(result.current.markError).toBe('')

    // With nothing unread, there is nothing to mark.
    act(() => {
      result.current.markAllRead()
    })

    expect(platform.markAllNotificationsRead).toHaveBeenCalledOnce()
  })

  it('puts the entries back to unread, and says why, when they cannot be marked read', async () => {
    const stored = deferred()
    platform.markAllNotificationsRead.mockReturnValue(stored.promise)
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    act(() => {
      result.current.markAllRead()
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

  it('marks entries read and unread one by one, at once', async () => {
    platform.listNotifications.mockResolvedValue({
      items: [notification({ id: 3 }), notification({ id: 2 }), notification({ id: 1, read: true })],
      unread: 2,
    })
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(2)
    })

    act(() => {
      result.current.markRead([3])
    })

    expect(result.current.log.items.map((n) => n.read)).toEqual([true, false, true])
    expect(result.current.log.unread).toBe(1)
    expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([3])

    await act(async () => {
      await Promise.resolve()
    })

    act(() => {
      result.current.markUnread([1, 3])
    })

    expect(result.current.log.items.map((n) => n.read)).toEqual([false, false, false])
    expect(result.current.log.unread).toBe(3)
    expect(platform.markNotificationsUnread).toHaveBeenCalledExactlyOnceWith([1, 3])

    await act(async () => {
      await Promise.resolve()
    })

    expect(result.current.log.unread).toBe(3)

    // An empty list stores nothing.
    act(() => {
      result.current.markRead([])
      result.current.markUnread([])
    })

    expect(platform.markNotificationsRead).toHaveBeenCalledOnce()
    expect(platform.markNotificationsUnread).toHaveBeenCalledOnce()
  })

  it('takes back a mark unread that cannot be stored', async () => {
    platform.listNotifications.mockResolvedValue({ items: [notification({ id: 1, read: true })], unread: 0 })
    const stored = deferred()
    platform.markNotificationsUnread.mockReturnValue(stored.promise)
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.items).toHaveLength(1)
    })

    act(() => {
      result.current.markUnread([1])
    })

    expect(result.current.log.unread).toBe(1)

    await act(async () => {
      stored.reject(new Error('busy'))
      await stored.promise.catch(() => undefined)
    })

    expect(result.current.log.unread).toBe(0)
    expect(result.current.markError).toBe('busy')
  })

  it('marks an item’s unread entries read, and nothing when it has none', async () => {
    platform.listNotifications.mockResolvedValue({
      items: [
        notification({ id: 4, itemId: 9 }),
        notification({ id: 3, itemId: 1, kind: 'woke' }),
        notification({ id: 2, itemId: 1 }),
        notification({ id: 1, itemId: 1, read: true }),
      ],
      unread: 3,
    })
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(3)
    })

    act(() => {
      result.current.markItemRead(1)
    })

    expect(platform.markNotificationsRead).toHaveBeenCalledExactlyOnceWith([3, 2])
    expect(result.current.log.unread).toBe(1)

    await act(async () => {
      await Promise.resolve()
    })

    act(() => {
      result.current.markItemRead(1)
      result.current.markItemRead(42)
      result.current.markItemRead(0)
    })

    expect(platform.markNotificationsRead).toHaveBeenCalledOnce()
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
