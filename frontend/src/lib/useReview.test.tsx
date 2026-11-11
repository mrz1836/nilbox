import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { review, reviewFile } from '../test/fixtures'
import { platform } from '../test/platform'
import { unknownRetries, unknownWait, useReview } from './useReview'

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
})
