import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { githubThreads, item, itemThreads } from '../test/fixtures'
import { emit, listening, platform } from '../test/platform'
import { tagOf } from './githubThreads'
import { useGitHubThreads } from './useGitHubThreads'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('useGitHubThreads', () => {
  it('reads the mirror, and again whenever the shell says it changed, until unmounted', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { count: 3, unread: 1 } }))

    const { result, unmount } = renderHook(() => useGitHubThreads())

    await waitFor(() => {
      expect(result.current.snapshot?.status.count).toBe(3)
    })

    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { count: 4, unread: 2 } }))
    act(() => {
      emit('ghthreads:updated')
    })

    await waitFor(() => {
      expect(result.current.snapshot?.status.count).toBe(4)
    })

    expect(platform.checkGitHubThreadsNow).not.toHaveBeenCalled()
    expect(listening('ghthreads:updated')).toBe(1)
    unmount()
    expect(listening('ghthreads:updated')).toBe(0)
  })

  it('keeps an item’s entry while its tag shows the same, so only a changed card renders again', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(
      githubThreads({ byItem: [itemThreads(), itemThreads({ number: 9, unread: 2 })] }),
    )

    const { result } = renderHook(() => useGitHubThreads())

    await waitFor(() => {
      expect(result.current.byItem.size).toBe(2)
    })

    const seven = tagOf(result.current.byItem, item())
    const nine = tagOf(result.current.byItem, item({ number: 9 }))

    platform.githubThreadsSnapshot.mockResolvedValue(
      githubThreads({ byItem: [itemThreads(), itemThreads({ number: 9, unread: 3 })] }),
    )
    act(() => {
      emit('ghthreads:updated')
    })

    await waitFor(() => {
      expect(tagOf(result.current.byItem, item({ number: 9 }))?.unread).toBe(3)
    })

    expect(tagOf(result.current.byItem, item())).toBe(seven)
    expect(tagOf(result.current.byItem, item({ number: 9 }))).not.toBe(nine)
  })

  it('reads the mirror again on request, from the database alone', async () => {
    const { result } = renderHook(() => useGitHubThreads())

    await waitFor(() => {
      expect(result.current.snapshot).not.toBeNull()
    })

    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { count: 9 } }))
    act(() => {
      result.current.reload()
    })

    await waitFor(() => {
      expect(result.current.snapshot?.status.count).toBe(9)
    })
    expect(platform.checkGitHubThreadsNow).not.toHaveBeenCalled()
  })

  it('checks GitHub on request and shows what it found, or why it couldn’t', async () => {
    const { result } = renderHook(() => useGitHubThreads())

    await waitFor(() => {
      expect(result.current.snapshot?.status.count).toBe(0)
    })

    platform.checkGitHubThreadsNow.mockResolvedValueOnce(githubThreads({ status: { count: 12, unread: 4 } }))

    let outcome: Awaited<ReturnType<typeof result.current.checkNow>> | undefined
    await act(async () => {
      outcome = await result.current.checkNow()
    })

    expect(outcome?.ok).toBe(true)
    expect(result.current.snapshot?.status.count).toBe(12)
    expect(result.current.checkError).toBe('')

    platform.checkGitHubThreadsNow.mockRejectedValueOnce(new Error('database is locked'))
    await act(async () => {
      outcome = await result.current.checkNow()
    })

    expect(outcome?.ok).toBe(false)
    expect(result.current.checkError).toBe('database is locked')
    expect(result.current.snapshot?.status.count).toBe(12)
  })
})
