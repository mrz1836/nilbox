import { describe, expect, it } from 'vitest'

import type { NotificationKind } from '../platform'
import { notification } from '../test/fixtures'
import { bannerStatusText, entryRef, kindTitle, notificationKinds } from './notifications'

describe('notifications', () => {
  it('lists the kinds in the order Settings shows them', () => {
    expect(notificationKinds.map((k) => k.kind)).toEqual([
      'review_requested',
      'assigned',
      'woke',
      'session_needs_you',
      'session_finished',
    ])
  })

  it('titles each kind, and an unknown one plainly', () => {
    expect(kindTitle('review_requested')).toBe('Review requested')
    expect(kindTitle('assigned')).toBe('Assigned to you')
    expect(kindTitle('woke')).toBe('Back from snooze')
    // A kind another build stored, outside the ones this build knows.
    expect(kindTitle('mentions' as unknown as NotificationKind)).toBe('Notification')
  })

  it('names an entry’s item by repository and number', () => {
    expect(entryRef(notification())).toBe('example-repo #7')
    expect(entryRef(notification({ repo: 'no-owner', number: 3 }))).toBe('no-owner #3')
  })

  it('explains banners that cannot show, and says nothing when they do', () => {
    expect(bannerStatusText('on')).toBe('')
    expect(bannerStatusText('')).toBe('')
    expect(bannerStatusText('off')).toMatch(/System Settings › Notifications/)
    expect(bannerStatusText('unavailable')).toMatch(/packaged app/)
  })
})
