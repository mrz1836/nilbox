import { describe, expect, it } from 'vitest'

import { replyDefaultsText, replySlots, replyUsageSummary } from './replies'

describe('replySlots', () => {
  it('offers an approval both approval defaults, and the others their own', () => {
    expect(replySlots('approve')).toEqual(['approve', 'approve_bots'])
    expect(replySlots('request_changes')).toEqual(['request_changes'])
    expect(replySlots('comment')).toEqual(['comment'])
    expect(replySlots('')).toEqual([])
  })
})

describe('replyUsageSummary', () => {
  it('says what a reply is the default for', () => {
    expect(replyUsageSummary([])).toBe('Not a default')
    expect(replyUsageSummary(['comment'])).toBe('Default for comments')
    expect(replyUsageSummary(['approve', 'approve_bots'])).toBe(
      'Default for approvals and approvals on bot pull requests',
    )
  })

  it('names the defaults for a sentence', () => {
    expect(replyDefaultsText(['request_changes'])).toBe('requests for changes')
  })
})
