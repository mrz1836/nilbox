import { describe, expect, it } from 'vitest'

import type { NotificationKind } from '../platform'
import { notification } from '../test/fixtures'
import {
  appTypeOf,
  appTypeOrder,
  appTypes,
  bannerEntry,
  bannerStatusText,
  entryRef,
  entryTitle,
  entryWhere,
  filterEntries,
  kindTitle,
  notificationKinds,
  opensRun,
  opensUpdate,
  quickViewMax,
  startLists,
  unreadOf,
  updateBanner,
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
      'service_status',
      'update_ready',
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
    expect(kindTitle('service_status')).toBe('Service status')
    expect(kindTitle('update_ready')).toBe('New version')
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

  it('opens the update’s banner only from a new version’s entry', () => {
    const update = notification({ kind: 'update_ready', itemId: 0, itemKind: '', number: 0, repo: '', url: '' })

    expect(opensUpdate(update)).toBe(true)
    expect(opensUpdate(notification())).toBe(false)
    expect(entryWhere({ ...update, detail: 'Restart to update when you’re ready.' })).toBe(
      'Restart to update when you’re ready.',
    )
  })

  it('explains banners that cannot show, and says nothing when they do', () => {
    expect(bannerStatusText('on')).toBe('')
    expect(bannerStatusText('')).toBe('')
    expect(bannerStatusText('off')).toMatch(/System Settings › Notifications/)
    expect(bannerStatusText('unavailable')).toMatch(/packaged app/)
  })
})

describe('the history’s lists', () => {
  const review = notification({ id: 6 })
  const assigned = notification({ id: 5, kind: 'assigned', read: true })
  const woke = notification({ id: 4, kind: 'woke' })
  const released = notification({ id: 3, kind: 'release_published', itemId: 0, itemKind: '', read: true })
  const failed = notification({ id: 2, kind: 'run_failed', itemId: 0, itemKind: '', runId: 9 })
  const unknown = notification({ id: 1, kind: 'mentions' as unknown as NotificationKind })
  const status = notification({ id: 0, kind: 'service_status', itemId: 0, itemKind: '', read: true })
  const all = [review, assigned, woke, released, failed, unknown, status]

  it('opens on every entry of every type, and quick views twenty', () => {
    expect(startLists).toEqual({ app: { show: 'all', type: 'all' }, github: { show: 'all', type: 'all' } })
    expect(quickViewMax).toBe(20)
  })

  it('offers every type, each with its words', () => {
    expect(appTypeOrder).toEqual(['all', 'review', 'assigned', 'snooze', 'release', 'run', 'status', 'update'])
    expect(appTypeOrder.map((id) => appTypes[id].label)).toEqual([
      'All',
      'Reviews',
      'Assigned',
      'Snoozes',
      'Releases',
      'Runs',
      'Status',
      'Updates',
    ])

    for (const id of appTypeOrder) {
      expect(appTypes[id].none).not.toBe('')
    }
  })

  it('types each entry, a release’s three kinds together, and an unknown kind as none', () => {
    expect(all.map(appTypeOf)).toEqual(['review', 'assigned', 'snooze', 'release', 'run', '', 'status'])
    expect(appTypeOf(notification({ kind: 'release_done' }))).toBe('release')
    expect(appTypeOf(notification({ kind: 'release_failed' }))).toBe('release')
    expect(appTypeOf(notification({ kind: 'update_ready' }))).toBe('update')
  })

  it('shows the unread or all, of a type or of every type', () => {
    expect(filterEntries(all, 'all', 'all')).toEqual(all)
    expect(filterEntries(all, 'unread', 'all')).toEqual([review, woke, failed, unknown])
    expect(filterEntries(all, 'all', 'release')).toEqual([released])
    expect(filterEntries(all, 'unread', 'release')).toEqual([])
    expect(filterEntries(all, 'unread', 'run')).toEqual([failed])
    expect(filterEntries(all, 'all', 'status')).toEqual([status])
  })

  it('finds an item’s unread entries, and none for no item', () => {
    const items = [
      notification({ id: 3, itemId: 1 }),
      notification({ id: 2, itemId: 1, read: true }),
      notification({ id: 1, itemId: 2 }),
    ]

    expect(unreadOf(items, 1)).toEqual([3])
    expect(unreadOf(items, 3)).toEqual([])
    expect(unreadOf([notification({ itemId: 0 })], 0)).toEqual([])
  })

  it('tells the entry a banner stood for from one that took its id after the history was cleared', () => {
    const banner = { itemId: 1, url: 'https://github.com/acme-org/example-repo/pull/7', runId: 0 }

    expect(bannerEntry(notification(), banner)).toBe(true)
    expect(bannerEntry(notification({ itemId: 0 }), banner)).toBe(true)
    expect(bannerEntry(notification({ itemId: 2, url: 'https://github.com/acme-org/other/pull/1' }), banner)).toBe(
      false,
    )
    expect(bannerEntry(failed, { itemId: 0, url: '', runId: 9 })).toBe(true)
    expect(bannerEntry(failed, { itemId: 0, url: '', runId: 8 })).toBe(false)
    expect(bannerEntry(notification({ url: '' }), { itemId: 0, url: '', runId: 0 })).toBe(false)
  })

  it('tells a new version’s banner, which names nothing but its entry, by the entry’s kind', () => {
    const none = { itemId: 0, url: '', runId: 0 }
    const update = notification({ kind: 'update_ready', itemId: 0, itemKind: '', url: '' })

    expect(updateBanner(none)).toBe(true)
    expect(updateBanner({ ...none, itemId: 1 })).toBe(false)
    expect(updateBanner({ ...none, runId: 2 })).toBe(false)
    expect(updateBanner({ ...none, url: 'https://github.com/acme-org/sdk/releases' })).toBe(false)

    expect(bannerEntry(update, none)).toBe(true)
    // An entry of another kind that took the id after the history was cleared.
    expect(bannerEntry(notification({ kind: 'woke', itemId: 0, url: '' }), none)).toBe(false)
    // A banner about an item never matches the update's entry.
    expect(bannerEntry(update, { ...none, itemId: 1 })).toBe(false)
  })
})
