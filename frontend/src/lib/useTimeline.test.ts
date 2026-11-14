import { describe, expect, it } from 'vitest'

import { issueComment, issueTimeline } from '../test/fixtures'
import { joined } from './useTimeline'

// comments are count comments numbered from first, oldest first.
function comments(first: number, count: number) {
  return Array.from({ length: count }, (_, i) =>
    issueComment({ id: `IC_${String(first + i)}`, at: `2026-06-1${String(first + i)}T10:00:00Z` }),
  )
}

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
