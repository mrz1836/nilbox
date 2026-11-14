import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { githubThread, githubThreadList, githubThreads, queuedClear, settledClear } from '../test/fixtures'
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

  it('marks done and read on request, holding each until the mirror is read again after it', async () => {
    const { result } = renderHook(() => useGitHubThreads())

    await waitFor(() => {
      expect(result.current.snapshot).not.toBeNull()
    })

    let answer: (value: ReturnType<typeof githubThreads>) => void = () => undefined
    platform.markGitHubThreadsDone.mockReturnValueOnce(
      new Promise((resolve) => {
        answer = resolve
      }),
    )

    let done: ReturnType<typeof result.current.markDone> | undefined
    act(() => {
      done = result.current.markDone([1, 3])
    })

    expect([...result.current.finishing]).toEqual([1, 3])
    expect(result.current.reading.size).toBe(0)
    expect(platform.markGitHubThreadsDone).toHaveBeenCalledExactlyOnceWith([1, 3])

    // GitHub took them; the mirror, read again, shows them gone.
    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { count: 2 } }))
    act(() => {
      answer(githubThreads({ status: { count: 2 } }))
    })

    await waitFor(() => {
      expect(result.current.finishing.size).toBe(0)
    })
    expect(result.current.snapshot?.status.count).toBe(2)
    expect((await done)?.ok).toBe(true)

    let read: ReturnType<typeof result.current.markRead> | undefined
    act(() => {
      read = result.current.markRead([4])
    })

    expect([...result.current.reading]).toEqual([4])
    await waitFor(() => {
      expect(result.current.reading.size).toBe(0)
    })
    expect(platform.markGitHubThreadsRead).toHaveBeenCalledExactlyOnceWith([4])
    expect((await read)?.ok).toBe(true)
    expect(result.current.markError).toBe('')
  })

  it('says why a mark failed, reads the mirror again, and lets the words go on request', async () => {
    const { result } = renderHook(() => useGitHubThreads())

    await waitFor(() => {
      expect(result.current.snapshot).not.toBeNull()
    })

    const reads = platform.githubThreadsSnapshot.mock.calls.length
    platform.markGitHubThreadsDone.mockRejectedValueOnce(new Error('GitHub asked for a pause before the next change.'))

    let done: ReturnType<typeof result.current.markDone> | undefined
    act(() => {
      done = result.current.markDone([1])
    })

    await waitFor(() => {
      expect(result.current.finishing.size).toBe(0)
    })
    expect(result.current.markError).toBe('GitHub asked for a pause before the next change.')
    expect(platform.githubThreadsSnapshot.mock.calls.length).toBeGreaterThan(reads)
    expect((await done)?.ok).toBe(false)

    act(() => {
      result.current.dismissMarkError()
    })

    expect(result.current.markError).toBe('')
  })

  it('lists every notification only while listing, and again on each change and after each mark', async () => {
    platform.githubThreadsList.mockResolvedValue(githubThreadList({ threads: [githubThread()] }))

    const { result, rerender } = renderHook(({ listing }) => useGitHubThreads(listing), {
      initialProps: { listing: false },
    })

    await waitFor(() => {
      expect(result.current.snapshot).not.toBeNull()
    })
    expect(platform.githubThreadsList).not.toHaveBeenCalled()
    expect(result.current.list).toBeNull()

    act(() => {
      emit('ghthreads:updated')
    })
    expect(platform.githubThreadsList).not.toHaveBeenCalled()

    rerender({ listing: true })

    await waitFor(() => {
      expect(result.current.list?.threads).toHaveLength(1)
    })

    platform.githubThreadsList.mockResolvedValue(
      githubThreadList({ threads: [githubThread(), githubThread({ id: 1002, unread: false })] }),
    )
    act(() => {
      emit('ghthreads:updated')
    })

    await waitFor(() => {
      expect(result.current.list?.threads).toHaveLength(2)
    })

    const lists = platform.githubThreadsList.mock.calls.length

    act(() => {
      void result.current.markRead([1001])
    })
    await waitFor(() => {
      expect(result.current.reading.size).toBe(0)
    })
    expect(platform.githubThreadsList.mock.calls.length).toBeGreaterThan(lists)

    const checked = platform.githubThreadsList.mock.calls.length

    act(() => {
      void result.current.checkNow()
    })
    await waitFor(() => {
      expect(platform.githubThreadsList.mock.calls.length).toBeGreaterThan(checked)
    })

    // Listing no more, the list stays as it was and nothing asks again.
    rerender({ listing: false })

    const stopped = platform.githubThreadsList.mock.calls.length

    act(() => {
      emit('ghthreads:updated')
    })
    await waitFor(() => {
      expect(platform.githubThreadsSnapshot).toHaveBeenCalled()
    })
    expect(platform.githubThreadsList.mock.calls.length).toBe(stopped)
    expect(result.current.list?.threads).toHaveLength(2)
  })

  it('says why the list could not be read', async () => {
    platform.githubThreadsList.mockRejectedValue(new Error('database is locked'))

    const { result } = renderHook(() => useGitHubThreads(true))

    await waitFor(() => {
      expect(result.current.listError).toBe('database is locked')
    })
    expect(result.current.list).toBeNull()
  })

  it('takes queued clears back, and shows the mirror as Keep left it', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ queued: [queuedClear()] }))

    const { result } = renderHook(() => useGitHubThreads())

    await waitFor(() => {
      expect(result.current.snapshot?.queued).toHaveLength(1)
    })

    platform.keepGitHubThreads.mockResolvedValueOnce(githubThreads({ recent: [settledClear({ state: 'kept' })] }))

    let kept: Awaited<ReturnType<typeof result.current.keep>> | undefined
    await act(async () => {
      kept = await result.current.keep([1])
    })

    expect(platform.keepGitHubThreads).toHaveBeenCalledExactlyOnceWith([1])
    expect(kept?.ok).toBe(true)
    expect(result.current.snapshot?.queued).toEqual([])
    expect(result.current.snapshot?.recent).toHaveLength(1)

    platform.keepGitHubThreads.mockRejectedValueOnce(new Error('database is locked'))

    await act(async () => {
      kept = await result.current.keep([1])
    })

    expect(kept).toEqual({ ok: false, error: 'database is locked' })
    expect(result.current.snapshot?.recent).toHaveLength(1)
  })
})
