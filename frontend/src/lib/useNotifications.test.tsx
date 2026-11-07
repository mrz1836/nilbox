import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { notification } from '../test/fixtures'
import { useNotifications } from './useNotifications'

const platform = vi.hoisted(() => ({
  listNotifications: vi.fn(),
  markNotificationsRead: vi.fn(),
  clearNotifications: vi.fn(),
  onNotificationsUpdated: vi.fn(),
}))

vi.mock('../platform', () => platform)

const stopListening = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  platform.listNotifications.mockResolvedValue({ items: [notification()], unread: 1 })
  platform.markNotificationsRead.mockResolvedValue(undefined)
  platform.clearNotifications.mockResolvedValue(undefined)
  platform.onNotificationsUpdated.mockReturnValue(stopListening)
})

describe('useNotifications', () => {
  it('loads the history, and again whenever it changes, until unmounted', async () => {
    const { result, unmount } = renderHook(() => useNotifications())

    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    const reload = platform.onNotificationsUpdated.mock.calls[0]?.[0] as () => void
    platform.listNotifications.mockResolvedValue({
      items: [notification({ id: 2 }), notification({ read: true })],
      unread: 1,
    })
    act(reload)

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
    const reload = platform.onNotificationsUpdated.mock.calls[0]?.[0] as () => void
    platform.listNotifications.mockResolvedValue({ items: [], unread: 0 })
    act(reload)

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

  it('marks everything read at once, and reloads if that fails', async () => {
    const { result } = renderHook(() => useNotifications())
    await waitFor(() => {
      expect(result.current.log.unread).toBe(1)
    })

    act(() => {
      result.current.markRead()
    })

    expect(result.current.log.unread).toBe(0)
    expect(result.current.log.items?.[0]?.read).toBe(true)
    expect(platform.markNotificationsRead).toHaveBeenCalledOnce()

    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.markNotificationsRead.mockRejectedValue(new Error('busy'))

    act(() => {
      result.current.markRead()
    })

    await waitFor(() => {
      expect(platform.listNotifications).toHaveBeenCalledTimes(2)
    })
    expect(logged).toHaveBeenCalled()
    logged.mockRestore()
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
