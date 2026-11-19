import { describe, expect, it } from 'vitest'

import type { InboxItem } from '../platform'
import { details, item } from '../test/fixtures'
import { awaitsAuthor, nudgeable, nudgeDue, nudgeWords, quietSince, quietWords } from './nudge'

// waiting is someone else's pull request the user requested changes on, last active at
// 09:00 on June 15th, not pushed to since.
function waiting(overrides: Partial<InboxItem> = {}): InboxItem {
  return item({
    author: 'contributor-a',
    assigned: true,
    updatedAt: '2026-06-15T09:00:00Z',
    details: details({ viewerVerdict: 'changes', pushedSinceReview: false }),
    ...overrides,
  })
}

// at is a time on June 15th or later, by its ISO text.
const at = (iso: string) => new Date(iso)

describe('awaitsAuthor', () => {
  it('is a pull request someone else opened that waits on the changes the user requested', () => {
    expect(awaitsAuthor(waiting())).toBe(true)
  })

  it.each([
    ['the user’s own', { authored: true }],
    ['an issue', { kind: 'issue' as const }],
    ['one that asks for the user’s review', { reviewRequest: 'you' as const }],
    ['one pushed to since', { details: details({ viewerVerdict: 'changes', pushedSinceReview: true }) }],
    ['one the user approved', { details: details({ viewerVerdict: 'approved' }) }],
    ['one whose details are unread', { details: null }],
  ])('is not %s', (_, overrides: Partial<InboxItem>) => {
    expect(awaitsAuthor(waiting(overrides))).toBe(false)
  })

  it('leaves a bot’s pull request waiting, but nudges no bot', () => {
    expect(awaitsAuthor(waiting({ authorIsBot: true }))).toBe(true)
    expect(nudgeable(waiting({ authorIsBot: true }))).toBe(false)
    expect(nudgeable(waiting())).toBe(true)
  })
})

describe('quietSince', () => {
  it('is the later of the last activity and the nudge', () => {
    expect(quietSince(waiting())).toBe('2026-06-15T09:00:00Z')
    expect(quietSince(waiting({ nudgedAt: '2026-06-16T09:00:00Z' }))).toBe('2026-06-16T09:00:00Z')
    expect(quietSince(waiting({ nudgedAt: '2026-06-14T09:00:00Z' }))).toBe('2026-06-15T09:00:00Z')
  })
})

describe('nudgeDue', () => {
  it('is due once the author has been quiet for the threshold, not a minute before', () => {
    expect(nudgeDue(waiting(), at('2026-06-16T08:59:00Z'), 24)).toBe(false)
    expect(nudgeDue(waiting(), at('2026-06-16T09:00:00Z'), 24)).toBe(true)
  })

  it('waits again from a nudge', () => {
    const nudged = waiting({ nudgedAt: '2026-06-16T09:00:00Z' })

    expect(nudgeDue(nudged, at('2026-06-16T10:00:00Z'), 24)).toBe(false)
    expect(nudgeDue(nudged, at('2026-06-17T09:00:00Z'), 24)).toBe(true)
  })

  it('is never due while Off, for a bot, or for what waits on no author', () => {
    const late = at('2026-07-15T09:00:00Z')

    expect(nudgeDue(waiting(), late, 0)).toBe(false)
    expect(nudgeDue(waiting({ authorIsBot: true }), late, 24)).toBe(false)
    expect(nudgeDue(waiting({ authored: true }), late, 24)).toBe(false)
  })
})

describe('the words', () => {
  it('say how long it has been quiet, or since the nudge', () => {
    expect(quietWords(waiting(), at('2026-06-17T09:00:00Z'))).toBe('quiet for 2d')
    expect(quietWords(waiting({ nudgedAt: '2026-06-17T04:00:00Z' }), at('2026-06-17T09:00:00Z'))).toBe(
      'you nudged them 5h ago',
    )
    expect(quietWords(waiting({ nudgedAt: '2026-06-17T09:00:00Z' }), at('2026-06-17T09:00:10Z'))).toBe(
      'you nudged them just now',
    )
  })

  it('are a card’s only once a nudge is due, or the user nudged', () => {
    expect(nudgeWords(waiting(), at('2026-06-15T15:00:00Z'), 24)).toBe('')
    expect(nudgeWords(waiting(), at('2026-06-17T09:00:00Z'), 24)).toBe('quiet for 2d')
    expect(nudgeWords(waiting(), at('2026-06-17T09:00:00Z'), 0)).toBe('')
    expect(nudgeWords(waiting({ nudgedAt: '2026-06-17T04:00:00Z' }), at('2026-06-17T09:00:00Z'), 0)).toBe(
      'you nudged them 5h ago',
    )
    expect(nudgeWords(waiting({ authored: true }), at('2026-06-17T09:00:00Z'), 24)).toBe('')
  })
})
