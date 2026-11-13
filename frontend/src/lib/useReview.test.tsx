import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { heldRun, review, reviewFile } from '../test/fixtures'
import { platform } from '../test/platform'
import { approvalRetries, approvalWait, runsStarted, unknownRetries, unknownWait, useReview } from './useReview'

vi.mock('../platform', () => import('./../test/platform').then((fake) => fake.platform))

// settle lets the answers already given reach the hook, on fake timers.
async function settle() {
  for (let i = 0; i < 3; i++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0)
    })
  }
}

// wait moves fake time on by ms, and lets what that starts settle.
async function wait(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
  await settle()
}

describe('useReview', () => {
  beforeEach(() => {
    platform.openReview.mockResolvedValue(review({ stale: true, fetchedAt: '' }))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows what the sheet read before, then what GitHub says', async () => {
    let answer: (r: ReturnType<typeof review>) => void = () => undefined
    platform.refreshReview.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )

    const { result } = renderHook(() => useReview(1))

    await waitFor(() => {
      expect(result.current.review?.stale).toBe(true)
    })
    expect(result.current.checking).toBe(true)

    act(() => {
      answer(review())
    })

    await waitFor(() => {
      expect(result.current.checking).toBe(false)
    })
    expect(result.current.review?.stale).toBe(false)
  })

  it('reads again while GitHub works out whether it can merge, a few times at most', async () => {
    vi.useFakeTimers()
    const unsure = review({ readiness: { state: 'unknown', sentence: 'Checking whether it can merge', needs: [] } })
    platform.refreshReview.mockResolvedValue(unsure)

    const { result } = renderHook(() => useReview(1))

    await settle()
    expect(platform.refreshReview).toHaveBeenCalledTimes(1)

    for (let n = 2; n <= 1 + unknownRetries; n++) {
      await wait(unknownWait)
      expect(platform.refreshReview).toHaveBeenCalledTimes(n)
    }

    await wait(10 * unknownWait)
    expect(platform.refreshReview).toHaveBeenCalledTimes(1 + unknownRetries)
    expect(result.current.review?.readiness.state).toBe('unknown')
  })

  it('stops reading again once GitHub knows', async () => {
    vi.useFakeTimers()
    platform.refreshReview
      .mockResolvedValueOnce(review({ readiness: { state: 'unknown', sentence: 'Checking', needs: [] } }))
      .mockResolvedValue(review())

    const { result } = renderHook(() => useReview(1))

    await settle()
    await wait(unknownWait)
    await wait(10 * unknownWait)
    expect(platform.refreshReview).toHaveBeenCalledTimes(2)
    expect(result.current.review?.readiness.state).toBe('ready')
  })

  it('reads again after an approval of held runs until their checks start, then stops', async () => {
    vi.useFakeTimers()
    const held = review({ unrun: [heldRun('CI', 101)] })
    const queued = review({
      unrun: [],
      checks: [{ name: 'lint', workflow: 'CI', state: 'QUEUED', outcome: 'pending', url: '', required: false }],
    })
    platform.refreshReview.mockResolvedValue(held)
    // GitHub hasn't made the checks yet on the first two reads; the third finds them queued.
    platform.readReviewAgain
      .mockResolvedValueOnce(held)
      .mockResolvedValueOnce(review({ unrun: [] }))
      .mockResolvedValue(queued)

    const { result } = renderHook(() => useReview(1))

    await settle()
    expect(result.current.following).toBe(false)

    act(() => {
      result.current.followRuns(1)
    })
    expect(result.current.following).toBe(true)
    await settle()
    expect(platform.readReviewAgain).toHaveBeenCalledTimes(1)
    expect(result.current.following).toBe(true)

    await wait(approvalWait)
    expect(platform.readReviewAgain).toHaveBeenCalledTimes(2)
    await wait(approvalWait)
    expect(platform.readReviewAgain).toHaveBeenCalledTimes(3)
    expect(result.current.following).toBe(false)
    expect(result.current.review?.checks[0]?.state).toBe('QUEUED')

    await wait(10 * approvalWait)
    expect(platform.readReviewAgain).toHaveBeenCalledTimes(3)
    expect(platform.refreshReview).toHaveBeenCalledTimes(1)
  })

  it('gives up reading again after an approval a few reads later', async () => {
    vi.useFakeTimers()
    const held = review({ unrun: [heldRun('CI', 101)] })
    platform.refreshReview.mockResolvedValue(held)
    platform.readReviewAgain.mockResolvedValue(held)

    const { result } = renderHook(() => useReview(1))

    await settle()
    act(() => {
      result.current.followRuns(1)
    })
    await settle()

    for (let n = 2; n <= 1 + approvalRetries; n++) {
      await wait(approvalWait)
      expect(platform.readReviewAgain).toHaveBeenCalledTimes(n)
    }

    await wait(10 * approvalWait)
    expect(platform.readReviewAgain).toHaveBeenCalledTimes(1 + approvalRetries)
    expect(result.current.following).toBe(false)
  })

  it('ends the reads after an approval when the user reads it again', async () => {
    vi.useFakeTimers()
    const held = review({ unrun: [heldRun('CI', 101)] })
    platform.refreshReview.mockResolvedValue(held)
    platform.readReviewAgain.mockResolvedValue(held)

    const { result } = renderHook(() => useReview(1))

    await settle()
    act(() => {
      result.current.followRuns(1)
    })
    await settle()

    act(() => {
      result.current.reload()
    })
    await settle()
    expect(result.current.following).toBe(false)

    await wait(10 * approvalWait)
    expect(platform.readReviewAgain).toHaveBeenCalledTimes(2)
  })

  it('counts approved runs as started once fewer are held and a check runs, or it is no longer open', () => {
    const running = [{ name: 'lint', workflow: 'CI', state: 'QUEUED', outcome: 'pending', url: '', required: false }]
    const two = [heldRun('CI', 101), heldRun('CodeQL', 102)]

    expect(runsStarted(review({ unrun: two, checks: running }), 2)).toBe(false)
    expect(runsStarted(review({ unrun: [] }), 2)).toBe(false)
    expect(runsStarted(review({ unrun: [], checks: running }), 2)).toBe(true)
    // GitHub refused one: the other started.
    expect(runsStarted(review({ unrun: [heldRun('CodeQL', 102)], checks: running }), 2)).toBe(true)
    expect(runsStarted(review({ unrun: [{ ...heldRun('CI', 101), approval: false }], checks: running }), 1)).toBe(true)
    expect(runsStarted(review({ state: 'MERGED', unrun: two }), 2)).toBe(true)
  })

  it('never shows another pull request', async () => {
    platform.openReview.mockResolvedValue(review({ id: 2 }))
    platform.refreshReview.mockResolvedValue(review({ id: 2 }))

    const { result } = renderHook(() => useReview(1))

    await waitFor(() => {
      expect(result.current.checking).toBe(false)
    })
    expect(result.current.review).toBeNull()
  })

  it('reports a failed read, and reads again on reload', async () => {
    platform.refreshReview.mockRejectedValueOnce(new Error('GitHub is down'))

    const { result } = renderHook(() => useReview(1))

    await waitFor(() => {
      expect(result.current.error).toBe('GitHub is down')
    })

    act(() => {
      result.current.reload()
    })

    await waitFor(() => {
      expect(result.current.error).toBe('')
    })
    // The user asked: the read reaches GitHub however recently it read.
    expect(platform.refreshReview).toHaveBeenCalledOnce()
    expect(platform.readReviewAgain).toHaveBeenCalledExactlyOnceWith(1)
  })

  it('reads the diffs Open left out', async () => {
    platform.refreshReview.mockResolvedValue(
      review({ files: [reviewFile({ path: 'big.go', patch: '', patchState: 'omitted' }), reviewFile()] }),
    )

    const { result } = renderHook(() => useReview(1))

    await waitFor(() => {
      expect(result.current.review?.files[0]?.patchState).toBe('omitted')
    })

    await act(() => result.current.loadPatches(['big.go']))

    expect(platform.reviewPatches).toHaveBeenCalledWith(1, review().head, ['big.go'])
    expect(result.current.review?.files[0]).toMatchObject({ patchState: '', patch: '@@ -1 +1 @@\n-a\n+b' })
    expect(result.current.loading.size).toBe(0)

    platform.reviewPatches.mockRejectedValueOnce(new Error('Reload it'))
    await act(() => result.current.loadPatches(['cache/cache.go']))
    expect(result.current.patchError).toBe('Reload it')

    await act(() => result.current.loadPatches([]))
    expect(platform.reviewPatches).toHaveBeenCalledTimes(2)
  })

  it('reads no diffs before the pull request', async () => {
    platform.openReview.mockReturnValue(new Promise(() => undefined))
    platform.refreshReview.mockReturnValue(new Promise(() => undefined))

    const { result } = renderHook(() => useReview(1))

    await act(() => result.current.loadPatches(['a.go']))
    expect(platform.reviewPatches).not.toHaveBeenCalled()
  })

  it('forces a read only of the pull request the user asked it for', async () => {
    const { result, rerender } = renderHook(({ id }) => useReview(id), { initialProps: { id: 1 } })

    await waitFor(() => {
      expect(result.current.review).not.toBeNull()
    })

    act(() => {
      result.current.reload()
    })
    await waitFor(() => {
      expect(platform.readReviewAgain).toHaveBeenCalledWith(1)
    })

    rerender({ id: 2 })
    await waitFor(() => {
      expect(platform.refreshReview).toHaveBeenCalledWith(2)
    })
    expect(platform.readReviewAgain, 'stepping is no reload').toHaveBeenCalledOnce()
  })
})
