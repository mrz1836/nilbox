import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { deferred } from '../test/async'
import { at } from '../test/clock'
import { details, heldRun, item, review, reviewFile } from '../test/fixtures'
import { platform } from '../test/platform'
import {
  approvalRetries,
  approvalWait,
  cardKey,
  listMark,
  oldAfter,
  readFloor,
  runsStarted,
  unknownRetries,
  unknownWait,
  useReview,
} from './useReview'

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

  it('shows what the sheet read before, then what GitHub says', async () => {
    const answer = deferred<ReturnType<typeof review>>()
    platform.refreshReview.mockReturnValue(answer.promise)

    const { result } = renderHook(() => useReview(1))

    await waitFor(() => {
      expect(result.current.review?.stale).toBe(true)
    })
    expect(result.current.checking).toBe(true)

    act(() => {
      answer.resolve(review())
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

  it('builds the sheet again from what it read when the card changes, once a read under way answers', async () => {
    const answer = deferred<ReturnType<typeof review>>()
    platform.refreshReview.mockReturnValue(answer.promise)
    const newer = review({ counts: { ...review().counts, passed: 2, newer: true } })
    platform.rebuildReview.mockResolvedValue(newer)

    const { result, rerender } = renderHook(({ card }) => useReview(1, '', card), { initialProps: { card: 'a' } })

    rerender({ card: 'b' })
    rerender({ card: 'c' })
    await waitFor(() => {
      expect(result.current.review).not.toBeNull()
    })
    expect(platform.rebuildReview, 'a read under way is never cut short').not.toHaveBeenCalled()

    act(() => {
      answer.resolve(review())
    })

    await waitFor(() => {
      expect(result.current.review?.counts.passed).toBe(2)
    })
    expect(platform.rebuildReview, 'however often the card changed').toHaveBeenCalledExactlyOnceWith(1)
    expect(result.current.checking, 'a rebuild reads nothing').toBe(false)

    rerender({ card: 'c' })
    rerender({ card: 'd' })
    await waitFor(() => {
      expect(platform.rebuildReview).toHaveBeenCalledTimes(2)
    })
    expect(platform.refreshReview).toHaveBeenCalledOnce()
    expect(platform.readReviewAgain).not.toHaveBeenCalled()
  })

  describe('when the card’s checks move', () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    // moved is #7 rebuilt from a card whose checks moved since the sheet read it, at 11:59.
    const moved = review({ counts: { ...review().counts, passed: 2, newer: true }, moved: true })
    // still is #7 rebuilt from a card the inbox read again with nothing moved.
    const still = review({ counts: { ...review().counts, newer: true } })

    // open opens #7 on a clock standing at now, its card at 'a', and lets its first read
    // answer.
    async function open(now = new Date('2026-06-15T12:00:00Z')) {
      const view = renderHook(({ card }) => useReview(1, '', card), {
        initialProps: { card: 'a' },
        wrapper: at(now),
      })

      await settle()
      expect(platform.refreshReview).toHaveBeenCalledOnce()

      return view
    }

    it('reads GitHub once after the rebuild, and never for a card that moved nothing', async () => {
      platform.rebuildReview.mockResolvedValueOnce(still).mockResolvedValueOnce(moved).mockResolvedValue(still)
      const { result, rerender } = await open()

      rerender({ card: 'b' })
      await settle()
      expect(platform.rebuildReview).toHaveBeenCalledOnce()
      expect(platform.refreshReview, 'nothing moved: no request').toHaveBeenCalledOnce()

      rerender({ card: 'c' })
      await settle()
      expect(platform.rebuildReview).toHaveBeenCalledTimes(2)
      expect(platform.refreshReview, 'the checks moved: one read').toHaveBeenCalledTimes(2)
      expect(platform.readReviewAgain).not.toHaveBeenCalled()

      await wait(10 * readFloor)
      expect(platform.refreshReview).toHaveBeenCalledTimes(2)
      expect(result.current.review?.moved).toBe(false)
    })

    it('reads once more at most for however many card changes came during a read', async () => {
      const answer = deferred<ReturnType<typeof review>>()
      platform.rebuildReview.mockResolvedValueOnce(moved).mockResolvedValueOnce(moved).mockResolvedValue(still)
      const { rerender } = await open()

      platform.refreshReview.mockReturnValueOnce(answer.promise)
      rerender({ card: 'b' })
      await settle()
      expect(platform.refreshReview).toHaveBeenCalledTimes(2)

      for (const card of ['c', 'd', 'e', 'f']) {
        rerender({ card })
        await settle()
      }
      expect(platform.rebuildReview, 'a read under way is never cut short').toHaveBeenCalledOnce()
      expect(platform.refreshReview).toHaveBeenCalledTimes(2)

      await act(async () => {
        answer.resolve(review())
        await vi.advanceTimersByTimeAsync(0)
      })
      await settle()
      expect(platform.rebuildReview, 'the changes build it once').toHaveBeenCalledTimes(2)
      expect(platform.refreshReview, 'and that asks once').toHaveBeenCalledTimes(3)

      await wait(10 * readFloor)
      expect(platform.rebuildReview).toHaveBeenCalledTimes(2)
      expect(platform.refreshReview).toHaveBeenCalledTimes(3)
    })

    it('waits until 20 seconds after the last read, and a closed sheet never reads', async () => {
      platform.rebuildReview.mockResolvedValue(moved)
      const { rerender, unmount } = await open(new Date('2026-06-15T11:59:05Z'))

      rerender({ card: 'b' })
      await settle()
      expect(platform.rebuildReview).toHaveBeenCalledOnce()

      await wait(readFloor - 5000 - 1)
      expect(platform.refreshReview, 'read at 11:59:00, 5 seconds before').toHaveBeenCalledOnce()
      await wait(1)
      expect(platform.refreshReview).toHaveBeenCalledTimes(2)

      rerender({ card: 'c' })
      await settle()
      expect(platform.rebuildReview).toHaveBeenCalledTimes(2)
      unmount()

      await wait(10 * readFloor)
      expect(platform.refreshReview, 'the read waiting went with the sheet').toHaveBeenCalledTimes(2)
    })

    it('makes no read again after one that failed, until the card moves on', async () => {
      platform.rebuildReview.mockResolvedValue(moved)
      const { result, rerender } = await open()

      platform.refreshReview.mockRejectedValueOnce(new Error('GitHub is down'))
      rerender({ card: 'b' })
      await settle()
      expect(platform.refreshReview).toHaveBeenCalledTimes(2)
      expect(result.current.error).toBe('GitHub is down')

      await wait(10 * readFloor)
      expect(platform.refreshReview).toHaveBeenCalledTimes(2)

      rerender({ card: 'c' })
      await settle()
      expect(platform.refreshReview).toHaveBeenCalledTimes(3)
    })
  })

  describe('when the window comes back to the front', () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    // focus brings the window back to the front.
    async function focus() {
      await act(async () => {
        window.dispatchEvent(new Event('focus'))
        await vi.advanceTimersByTimeAsync(0)
      })
      await settle()
    }

    // open opens #7, last read at 11:59, on a clock standing at now, and lets its first read
    // answer.
    async function open(now: Date) {
      const view = renderHook(() => useReview(1), { wrapper: at(now) })

      await settle()
      expect(platform.refreshReview).toHaveBeenCalledOnce()

      return view
    }

    it('reads once when the last read is more than five minutes old', async () => {
      await open(new Date(Date.parse('2026-06-15T11:59:00Z') + oldAfter))

      await focus()
      expect(platform.refreshReview, 'one read, as Refresh').toHaveBeenCalledTimes(2)
      expect(platform.readReviewAgain).not.toHaveBeenCalled()

      await wait(10 * readFloor)
      expect(platform.refreshReview, 'and no more').toHaveBeenCalledTimes(2)
    })

    it('reads nothing within five minutes of the last read', async () => {
      await open(new Date(Date.parse('2026-06-15T11:59:00Z') + oldAfter - 1000))

      await focus()
      await focus()
      expect(platform.refreshReview).toHaveBeenCalledOnce()
    })

    it('reads nothing while a read is under way, nor once the sheet is closed', async () => {
      const answer = deferred<ReturnType<typeof review>>()
      platform.refreshReview.mockReturnValueOnce(answer.promise)
      const { result, unmount } = renderHook(() => useReview(1), { wrapper: at(new Date('2026-06-15T13:00:00Z')) })

      await settle()
      expect(result.current.checking).toBe(true)
      await focus()
      expect(platform.refreshReview, 'the read under way brings it up to date').toHaveBeenCalledOnce()

      await act(async () => {
        answer.resolve(review())
        await vi.advanceTimersByTimeAsync(0)
      })
      await settle()
      unmount()

      await focus()
      expect(platform.refreshReview, 'a closed sheet reads nothing').toHaveBeenCalledOnce()
    })
  })

  it('keys the card on what the inbox stores of it', () => {
    const card = item({ details: details() })

    expect(cardKey(undefined)).toBe('')
    expect(cardKey(card)).toBe(cardKey(item({ details: details() })))
    expect(cardKey(card)).not.toBe(cardKey({ ...card, details: details({ fetchedAt: '2026-06-15T12:01:00Z' }) }))
    expect(cardKey(card)).not.toBe(cardKey({ ...card, updatedAt: '2026-06-15T12:01:00Z' }))
    expect(cardKey(card)).not.toBe(cardKey({ ...card, isDraft: true }))
    expect(cardKey(card), 'its triage is not the pull request').toBe(
      cardKey({ ...card, pinnedAt: '2026-06-15T12:01:00Z' }),
    )
  })

  it('marks what the sheet read as being read, or as read then, once it is older than what it shows', () => {
    const now = new Date('2026-06-15T12:00:00Z')
    const counts = review().counts
    const recent = review({ fetchedAt: '2026-06-15T11:59:00Z' })
    const old = review({ fetchedAt: '2026-06-15T11:50:00Z' })
    const newer = review({ fetchedAt: '2026-06-15T11:59:00Z', counts: { ...counts, newer: true } })
    const moved = review({ fetchedAt: '2026-06-15T11:59:00Z', counts: { ...counts, newer: true }, moved: true })

    expect(listMark(review({ fetchedAt: '' }), now, true, true), 'never read: placeholders say so').toBe('current')
    expect(listMark(recent, now, true, true)).toBe('current')
    expect(listMark(old, now, true, false), `older than ${String(oldAfter)} ms`).toBe('reading')
    expect(listMark(newer, now, true, false), 'older than the inbox’s read').toBe('reading')
    expect(listMark(old, now, false, true), 'a read failed').toBe('old')
    expect(listMark(old, now, false, false), 'nothing newer says otherwise').toBe('current')
    expect(listMark(newer, now, false, false), 'the inbox found nothing moved').toBe('current')
    expect(listMark(moved, now, false, false), 'the counts moved past the list').toBe('old')
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
