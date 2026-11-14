import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { githubThreads } from '../test/fixtures'
import { emit, listening, platform } from '../test/platform'
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
