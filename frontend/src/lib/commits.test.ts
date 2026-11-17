import { describe, expect, it } from 'vitest'

import { issueComment, reviewCommit, yourReview } from '../test/fixtures'
import { rewritePush, sinceIndex, sinceWords, verdictWords } from './commits'

const a = 'a'.repeat(40)
const b = 'b'.repeat(40)
const c = 'c'.repeat(40)
const commits = [reviewCommit({ oid: a }), reviewCommit({ oid: b }), reviewCommit({ oid: c })]

describe('sinceIndex', () => {
  it('starts after the reviewed commit', () => {
    expect(sinceIndex(commits, yourReview({ commit: a, count: 2 }))).toBe(1)
    expect(sinceIndex(commits, yourReview({ commit: b }))).toBe(2)
    expect(sinceIndex(commits, yourReview({ commit: b.toUpperCase() })), 'whatever the case').toBe(2)
  })

  it('marks none at the head, without a review, or when the list lacks the commit', () => {
    expect(sinceIndex(commits, yourReview({ commit: c, since: '', count: 0 }))).toBe(-1)
    expect(sinceIndex(commits, yourReview({ commit: c }))).toBe(-1)
    expect(sinceIndex(commits, null)).toBe(-1)
    expect(sinceIndex(commits, yourReview({ commit: 'd'.repeat(40) }))).toBe(-1)
    expect(sinceIndex(commits, yourReview({ commit: a, since: 'rewritten' }))).toBe(-1)
  })
})

describe('sinceWords', () => {
  it('says what happened since the review', () => {
    expect(sinceWords(yourReview({ count: 3 }), 3)).toBe('3 commits since you requested changes')
    expect(sinceWords(yourReview({ state: 'APPROVED', count: 1 }), 3)).toBe('1 commit since you approved')
    expect(sinceWords(yourReview({ since: 'beyond', count: 0 }), 100)).toBe('100+ commits since you requested changes')
    expect(sinceWords(yourReview({ since: 'rewritten', count: 0 }), 3)).toBe(
      'The branch was rewritten since you requested changes',
    )
  })

  it('says nothing when nothing is known to have happened', () => {
    expect(sinceWords(yourReview({ since: '', count: 0 }), 3)).toBe('')
    expect(sinceWords(null, 3)).toBe('')
  })

  it('words each verdict', () => {
    expect(verdictWords({ state: 'APPROVED' })).toBe('you approved')
    expect(verdictWords({ state: 'CHANGES_REQUESTED' })).toBe('you requested changes')
  })
})

describe('rewritePush', () => {
  const before = issueComment({ id: 'HRFPE_0', kind: 'force_pushed', at: '2026-06-15T10:00:00Z' })
  const after = issueComment({ id: 'HRFPE_1', kind: 'force_pushed', at: '2026-06-15T11:00:00Z' })
  const comment = issueComment({ id: 'IC_9', at: '2026-06-15T11:30:00Z' })

  it('finds the force push after the review', () => {
    const rewritten = yourReview({ since: 'rewritten', at: '2026-06-15T10:30:00Z' })

    expect(rewritePush([before, comment, after], rewritten)?.id).toBe('HRFPE_1')
    expect(rewritePush([before, comment], rewritten)).toBeUndefined()
    expect(rewritePush([before], yourReview({ since: 'rewritten', at: '' }))?.id, 'any, with no time').toBe('HRFPE_0')
  })

  it('finds none unless the branch was rewritten', () => {
    expect(rewritePush([after], yourReview({ at: '2026-06-15T10:30:00Z' }))).toBeUndefined()
    expect(rewritePush([after], null)).toBeUndefined()
  })
})
