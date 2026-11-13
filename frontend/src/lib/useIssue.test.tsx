import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { Issue } from '../platform'
import { issue, issueComment, issueTimeline } from '../test/fixtures'
import { platform } from '../test/platform'
import { joined, useIssue } from './useIssue'

vi.mock('../platform', () => import('./../test/platform').then((fake) => fake.platform))

// comments are count comments numbered from first, oldest first.
function comments(first: number, count: number) {
  return Array.from({ length: count }, (_, i) =>
    issueComment({ id: `IC_${String(first + i)}`, at: `2026-06-1${String(first + i)}T10:00:00Z` }),
  )
}

describe('useIssue', () => {
  it('shows what the sheet read before, then what GitHub says', async () => {
    let answer: (is: Issue) => void = () => undefined
    platform.openIssue.mockResolvedValue(issue({ stale: true, fetchedAt: '', lastSeenAt: '2026-06-14T00:00:00Z' }))
    platform.refreshIssue.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )

    const { result } = renderHook(() => useIssue(1))

    await waitFor(() => {
      expect(result.current.issue?.stale).toBe(true)
    })
    expect(result.current.checking).toBe(true)
    expect(result.current.lastSeenAt).toBe('2026-06-14T00:00:00Z')

    act(() => {
      answer(issue({ lastSeenAt: '2026-06-15T00:00:00Z' }))
    })

    await waitFor(() => {
      expect(result.current.checking).toBe(false)
    })
    expect(result.current.issue?.stale).toBe(false)
    expect(result.current.lastSeenAt, 'the visit keeps the time it started from').toBe('2026-06-14T00:00:00Z')
  })

  it('reads GitHub again when asked, however recently it read', async () => {
    const { result } = renderHook(() => useIssue(1))

    await waitFor(() => {
      expect(result.current.issue).not.toBeNull()
    })
    expect(platform.readIssueAgain).not.toHaveBeenCalled()

    act(() => {
      result.current.reload()
    })

    await waitFor(() => {
      expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
    })
  })

  it('forces a read only of the issue the user asked it for', async () => {
    const { result, rerender } = renderHook(({ id }) => useIssue(id), { initialProps: { id: 1 } })

    await waitFor(() => {
      expect(result.current.issue).not.toBeNull()
    })

    act(() => {
      result.current.reload()
    })
    await waitFor(() => {
      expect(platform.readIssueAgain).toHaveBeenCalledWith(1)
    })

    rerender({ id: 2 })
    await waitFor(() => {
      expect(platform.refreshIssue).toHaveBeenCalledWith(2)
    })

    rerender({ id: 1 })
    await waitFor(() => {
      expect(platform.refreshIssue).toHaveBeenLastCalledWith(1)
    })
    expect(platform.readIssueAgain, 'stepping is no reload').toHaveBeenCalledOnce()
  })

  it('keeps a failed read of earlier entries to its own issue', async () => {
    let fail: (e: Error) => void = () => undefined
    platform.issueEarlier.mockReturnValueOnce(
      new Promise((_, reject) => {
        fail = reject
      }),
    )
    const { result, rerender } = renderHook(({ id }) => useIssue(id), { initialProps: { id: 1 } })

    await waitFor(() => {
      expect(result.current.issue).not.toBeNull()
    })

    let read: Promise<void> = Promise.resolve()
    act(() => {
      read = result.current.loadEarlier()
    })
    expect(result.current.loadingEarlier).toBe(true)

    rerender({ id: 2 })
    expect(result.current.loadingEarlier, 'another issue isn’t loading').toBe(false)

    await act(async () => {
      fail(new Error('GitHub is down'))
      await read
    })
    expect(result.current.earlierError, 'nor did it fail').toBe('')
  })

  it('never shows an answer about another issue', async () => {
    platform.openIssue.mockResolvedValue(issue({ id: 2 }))
    platform.refreshIssue.mockResolvedValue(issue({ id: 2 }))

    const { result } = renderHook(() => useIssue(1))

    await waitFor(() => {
      expect(platform.refreshIssue).toHaveBeenCalled()
    })
    expect(result.current.issue).toBeNull()
    expect(result.current.lastSeenAt).toBe('')
  })

  it('says why a read failed', async () => {
    platform.refreshIssue.mockRejectedValue(new Error('GitHub is down'))

    const { result } = renderHook(() => useIssue(1))

    await waitFor(() => {
      expect(result.current.error).toBe('GitHub is down')
    })
    expect(result.current.issue?.id, 'what the sheet read before stays').toBe(1)
  })

  it('reads the earlier pages and joins them to the read', async () => {
    const read = issue({ timeline: issueTimeline({ items: comments(5, 3), total: 9, before: 'c5' }) })
    platform.openIssue.mockResolvedValue(read)
    platform.refreshIssue.mockResolvedValue(read)
    platform.issueEarlier.mockResolvedValueOnce(issueTimeline({ items: comments(3, 2), total: 9, before: 'c3' }))
    platform.issueEarlier.mockResolvedValueOnce(issueTimeline({ items: comments(1, 2), total: 9, before: '' }))

    const { result } = renderHook(() => useIssue(1))

    await waitFor(() => {
      expect(result.current.issue?.timeline.items).toHaveLength(3)
    })

    await act(async () => {
      await result.current.loadEarlier()
    })

    expect(platform.issueEarlier).toHaveBeenLastCalledWith(1, 'c5')
    expect(result.current.issue?.timeline.items.map((it) => it.id)).toEqual(['IC_3', 'IC_4', 'IC_5', 'IC_6', 'IC_7'])
    expect(result.current.issue?.timeline.before).toBe('c3')

    await act(async () => {
      await result.current.loadEarlier()
    })

    expect(platform.issueEarlier).toHaveBeenLastCalledWith(1, 'c3')
    expect(result.current.issue?.timeline.items).toHaveLength(7)
    expect(result.current.issue?.timeline.before).toBe('')

    await act(async () => {
      await result.current.loadEarlier()
    })
    expect(platform.issueEarlier, 'nothing is left to read').toHaveBeenCalledTimes(2)
  })

  it('says why an earlier page could not be read', async () => {
    platform.issueEarlier.mockRejectedValueOnce(new Error('GitHub is down'))

    const { result } = renderHook(() => useIssue(1))

    await waitFor(() => {
      expect(result.current.issue).not.toBeNull()
    })

    await act(async () => {
      await result.current.loadEarlier()
    })

    expect(result.current.earlierError).toBe('GitHub is down')
    expect(result.current.loadingEarlier).toBe(false)

    await act(async () => {
      await result.current.loadEarlier()
    })
    expect(result.current.earlierError, 'a read that worked clears it').toBe('')
  })
})

describe('joined', () => {
  const earlier = { id: 1, anchor: 'IC_5', items: comments(1, 4), before: 'c1' }

  it('puts the earlier pages before the read they were read before', () => {
    const page = issueTimeline({ items: comments(5, 2), before: 'c5' })

    expect(joined(page, earlier).items.map((it) => it.id)).toEqual(['IC_1', 'IC_2', 'IC_3', 'IC_4', 'IC_5', 'IC_6'])
    expect(joined(page, earlier).before).toBe('c1')
    expect(joined(page, null)).toBe(page)
  })

  it('keeps them once newer entries pushed the read on, and drops them once it moved past them', () => {
    const pushed = issueTimeline({ items: comments(3, 4), before: 'c3' })

    expect(joined(pushed, earlier).items.map((it) => it.id)).toEqual(['IC_1', 'IC_2', 'IC_3', 'IC_4', 'IC_5', 'IC_6'])

    const past = issueTimeline({ items: comments(7, 2), before: 'c7' })
    expect(joined(past, earlier)).toBe(past)

    const empty = issueTimeline({ items: [], before: '' })
    expect(joined(empty, earlier)).toBe(empty)
  })
})
