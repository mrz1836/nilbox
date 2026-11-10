import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { emptyDraft, nextCommentId, useReviewDrafts } from './reviewDrafts'

describe('useReviewDrafts', () => {
  it('keeps a draft per pull request until it is cleared', () => {
    const { result } = renderHook(() => useReviewDrafts())

    expect(result.current.get(7)).toBe(emptyDraft)

    act(() => {
      result.current.update(7, (d) => ({ ...d, body: 'Looks good', edited: true }))
    })

    expect(result.current.get(7)).toMatchObject({ body: 'Looks good', edited: true })
    expect(result.current.get(8)).toBe(emptyDraft)

    act(() => {
      result.current.clear(7)
    })

    expect(result.current.get(7)).toBe(emptyDraft)
  })

  it('numbers new comments past the last', () => {
    expect(nextCommentId(emptyDraft)).toBe(1)
    expect(
      nextCommentId({
        ...emptyDraft,
        comments: [
          { id: 4, path: 'a', side: 'RIGHT', line: 1, startLine: 0, body: 'x' },
          { id: 2, path: 'a', side: 'RIGHT', line: 2, startLine: 0, body: 'y' },
        ],
      }),
    ).toBe(5)
  })
})
