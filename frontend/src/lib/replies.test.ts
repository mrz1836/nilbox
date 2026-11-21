import { describe, expect, it, vi } from 'vitest'

import { replyDefaultsText, replyMenuItems, replySlots, replyUsageSummary } from './replies'

describe('replySlots', () => {
  it('offers an approval both approval defaults, a comment the comments’ and the nudge’s, and the others their own', () => {
    expect(replySlots('approve')).toEqual(['approve', 'approve_bots'])
    expect(replySlots('request_changes')).toEqual(['request_changes'])
    expect(replySlots('comment')).toEqual(['comment', 'nudge'])
    expect(replySlots('')).toEqual([])
  })
})

describe('replyUsageSummary', () => {
  it('says what a reply is the default for', () => {
    expect(replyUsageSummary([])).toBe('Not a default')
    expect(replyUsageSummary(['comment'])).toBe('Default for comments')
    expect(replyUsageSummary(['comment', 'nudge'])).toBe('Default for comments and nudges')
    expect(replyUsageSummary(['approve', 'approve_bots'])).toBe(
      'Default for approvals and approvals on bot pull requests',
    )
  })

  it('names the defaults for a sentence', () => {
    expect(replyDefaultsText(['request_changes'])).toBe('requests for changes')
  })
})

describe('replyMenuItems', () => {
  const replies = [
    { id: 4, nickname: 'Ping', name: 'Ask how it is going' },
    { id: 5, nickname: 'Thanks', name: 'Say thanks' },
  ]

  it('offers each reply, the default marked, then Manage replies… set apart', () => {
    const onPick = vi.fn<(replyId: number) => void>()
    const onManage = vi.fn()
    const items = replyMenuItems({ replies, error: '', none: 'None yet', fallback: 5, onPick, onManage })

    expect(items.map(({ label, hint, title, divided }) => ({ label, hint, title, divided }))).toEqual([
      { label: 'Ping', hint: undefined, title: 'Ask how it is going', divided: undefined },
      { label: 'Thanks', hint: 'default', title: 'Say thanks', divided: undefined },
      { label: 'Manage replies…', hint: undefined, title: undefined, divided: true },
    ])

    items[0]?.onSelect?.()
    expect(onPick).toHaveBeenCalledExactlyOnceWith(4)
    items[2]?.onSelect?.()
    expect(onManage).toHaveBeenCalledOnce()
  })

  it('says why there is nothing to pick, with no foot when the replies can’t be managed', () => {
    const onPick = vi.fn()

    expect(replyMenuItems({ replies: undefined, error: '', none: 'None yet', onPick })).toEqual([
      { label: 'Loading replies…', disabled: true },
    ])
    expect(replyMenuItems({ replies: undefined, error: 'the library is locked', none: 'None yet', onPick })).toEqual([
      { label: 'Couldn’t load the replies', title: 'the library is locked', disabled: true },
    ])
    expect(replyMenuItems({ replies: [], error: '', none: 'None yet', onPick })).toEqual([
      { label: 'None yet', disabled: true },
    ])
    expect(onPick).not.toHaveBeenCalled()
  })
})
