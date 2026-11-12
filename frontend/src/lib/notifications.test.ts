import { describe, expect, it } from 'vitest'

import type { NotificationKind } from '../platform'
import { notification } from '../test/fixtures'
import {
  bannerStatusText,
  entryRef,
  entryTitle,
  entryWhere,
  kindTitle,
  notificationKinds,
  opensRun,
} from './notifications'

describe('notifications', () => {
  it('lists the kinds in the order Settings shows them', () => {
    expect(notificationKinds.map((k) => k.kind)).toEqual([
      'review_requested',
      'assigned',
      'woke',
      'release_done',
      'release_published',
      'release_failed',
      'run_failed',
      'session_needs_you',
      'session_finished',
    ])
  })

  it('titles each kind, and an unknown one plainly', () => {
    expect(kindTitle('review_requested')).toBe('Review requested')
    expect(kindTitle('assigned')).toBe('Assigned to you')
    expect(kindTitle('woke')).toBe('Back from snooze')
    expect(kindTitle('release_done')).toBe('Released')
    expect(kindTitle('release_published')).toBe('Published')
    expect(kindTitle('release_failed')).toBe('Release workflow failed')
    expect(kindTitle('run_failed')).toBe('Run failed')
    // A kind another build stored, outside the ones this build knows.
    expect(kindTitle('mentions' as unknown as NotificationKind)).toBe('Notification')
  })

  it('names an entry’s item by repository and number', () => {
    expect(entryRef(notification())).toBe('example-repo #7')
    expect(entryRef(notification({ repo: 'no-owner', number: 3 }))).toBe('no-owner #3')
    expect(entryTitle(notification())).toBe('acme-org/example-repo #7')
  })

  it('names an entry about no item, as a release is, by its repository alone, never #0', () => {
    const release = notification({ kind: 'release_done', itemId: 0, itemKind: '', repo: 'acme-org/sdk', number: 0 })

    expect(entryRef(release)).toBe('sdk')
    expect(entryTitle(release)).toBe('acme-org/sdk')
    expect(entryTitle(notification({ repo: '' }))).toBeUndefined()
  })

  it('puts an entry’s item and what it says on one line, leaving out what is empty', () => {
    expect(entryWhere(notification())).toBe('example-repo #7')
    expect(entryWhere(notification({ detail: 'approved' }))).toBe('example-repo #7 · approved')

    const failed = notification({
      kind: 'run_failed',
      itemId: 0,
      itemKind: '',
      repo: '',
      number: 0,
      detail: 'It exited with status 1.',
      runId: 5,
    })

    expect(entryWhere(failed)).toBe('It exited with status 1.')
    expect(entryWhere({ ...failed, detail: '' })).toBe('')
  })

  it('opens a run only from a failed run’s entry that names one', () => {
    const failed = notification({ kind: 'run_failed', itemId: 0, itemKind: '', number: 0, runId: 5 })

    expect(opensRun(failed)).toBe(true)
    expect(opensRun({ ...failed, runId: 0 })).toBe(false)
    expect(opensRun(notification({ runId: 5 }))).toBe(false)
  })

  it('explains banners that cannot show, and says nothing when they do', () => {
    expect(bannerStatusText('on')).toBe('')
    expect(bannerStatusText('')).toBe('')
    expect(bannerStatusText('off')).toMatch(/System Settings › Notifications/)
    expect(bannerStatusText('unavailable')).toMatch(/packaged app/)
  })
})
