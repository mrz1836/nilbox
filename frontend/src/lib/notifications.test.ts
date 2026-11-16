import { describe, expect, it } from 'vitest'

import type { NotificationKind } from '../platform'
import { autopilotAlert, autopilotTarget, notification } from '../test/fixtures'
import {
  allNotificationKinds,
  appTypeOf,
  appTypeOrder,
  appTypes,
  autopilotKinds,
  bannerEntry,
  bannerOpens,
  bannerStatusText,
  entryHeading,
  entryRef,
  entryTitle,
  entryWhere,
  filterEntries,
  inboxKinds,
  isAutopilotKind,
  kindTitle,
  notificationKinds,
  opensAutopilot,
  opensRun,
  opensUpdate,
  quickViewMax,
  sessionKinds,
  startLists,
  targetNamed,
  unreadOf,
  updateBanner,
} from './notifications'

// nothing is the Autopilot target of every banner that isn't Autopilot's.
const nothing = autopilotTarget()

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
      'autopilot_alert',
      'autopilot_needs_you',
      'autopilot_finished',
      'autopilot_checks',
      'autopilot_problem',
      'session_needs_you',
      'session_finished',
    ])
  })

  it('describes every kind the backend has, once, in exactly one of Settings’ groups', () => {
    expect([...notificationKinds.map((k) => k.kind)].sort()).toEqual([...allNotificationKinds].sort())
    expect(new Set(notificationKinds.map((k) => k.kind)).size).toBe(notificationKinds.length)
    expect([...inboxKinds, ...autopilotKinds, ...sessionKinds].map((k) => k.kind).sort()).toEqual(
      [...allNotificationKinds].sort(),
    )

    const unsaid = notificationKinds.filter(
      (k) => k.title === '' || k.label === '' || k.description === '' || kindTitle(k.kind) === 'Notification',
    )

    expect(unsaid.map((k) => k.kind)).toEqual([])
  })

  it('types every kind the history keeps, once, and the sessions’ none', () => {
    const sessions = sessionKinds.map((k) => k.kind)
    const untyped = allNotificationKinds.filter((kind) => appTypeOf(notification({ kind })) === '')
    const typedOften = (kind: NotificationKind) =>
      appTypeOrder.filter((id) => id !== 'all' && appTypes[id].kinds.includes(kind)).length

    expect(untyped).toEqual(sessions)
    expect(allNotificationKinds.filter((kind) => typedOften(kind) !== (sessions.includes(kind) ? 0 : 1))).toEqual([])
  })

  it('opens every kind’s entry as its kind says', () => {
    const opened = allNotificationKinds.map((kind) => {
      const entry = notification({
        kind,
        itemId: 0,
        itemKind: '',
        runId: 5,
        autopilot: autopilotTarget({ routineId: 3 }),
      })

      return [kind, bannerOpens(entry, { itemId: 0, url: '', runId: 5, autopilot: entry.autopilot }, true)]
    })

    expect(opened).toEqual(
      allNotificationKinds.map((kind) => [
        kind,
        kind === 'run_failed'
          ? 'run'
          : isAutopilotKind(kind)
            ? 'autopilot'
            : opensUpdate(notification({ kind }))
              ? 'update'
              : 'item',
      ]),
    )
  })

  it('heads an Autopilot alert by its routine, and every other entry by its kind', () => {
    expect(entryHeading(autopilotAlert())).toBe('Tell me about security failures')
    expect(entryHeading(autopilotAlert({ heading: '' }))).toBe('Autopilot alert')
    expect(entryHeading(notification())).toBe('Review requested')
    expect(kindTitle('autopilot_needs_you')).toBe('Needs your OK')
    expect(kindTitle('autopilot_finished')).toBe('Agent finished')
    expect(kindTitle('autopilot_checks')).toBe('Checks after a fix')
    expect(kindTitle('autopilot_problem')).toBe('Autopilot problem')
  })

  it('opens the Autopilot tab from Autopilot’s entries about no item alone', () => {
    expect(opensAutopilot(notification({ kind: 'autopilot_problem', itemId: 0, itemKind: '' }))).toBe(true)
    expect(opensAutopilot(autopilotAlert({ itemId: 0, itemKind: '' }))).toBe(true)
    // One about an item opens that item, as an arrival does, until the tab exists.
    expect(opensAutopilot(autopilotAlert())).toBe(false)
    expect(opensAutopilot(notification())).toBe(false)
    expect(opensRun(autopilotAlert({ runId: 4 }))).toBe(false)
    expect(targetNamed(nothing)).toBe(false)
    expect(targetNamed(autopilotTarget({ createdAt: '2026-06-15T11:54:00Z' }))).toBe(false)
    expect(targetNamed(autopilotTarget({ activity: true }))).toBe(true)
    expect(targetNamed(autopilotTarget({ runId: 2 }))).toBe(true)
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
    expect(appTypeOrder).toEqual([
      'all',
      'review',
      'assigned',
      'snooze',
      'release',
      'run',
      'status',
      'update',
      'autopilot',
    ])
    expect(appTypeOrder.map((id) => appTypes[id].label)).toEqual([
      'All',
      'Reviews',
      'Assigned',
      'Snoozes',
      'Releases',
      'Runs',
      'Status',
      'Updates',
      'Autopilot',
    ])

    for (const id of appTypeOrder) {
      expect(appTypes[id].none).not.toBe('')
    }
  })

  it('offers Autopilot’s five kinds as one type', () => {
    expect(appTypes.autopilot.kinds).toEqual([
      'autopilot_alert',
      'autopilot_needs_you',
      'autopilot_finished',
      'autopilot_checks',
      'autopilot_problem',
    ])
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
    const banner = { itemId: 1, url: 'https://github.com/acme-org/example-repo/pull/7', runId: 0, autopilot: nothing }

    expect(bannerEntry(notification(), banner)).toBe(true)
    expect(bannerEntry(notification({ itemId: 0 }), banner)).toBe(true)
    expect(bannerEntry(notification({ itemId: 2, url: 'https://github.com/acme-org/other/pull/1' }), banner)).toBe(
      false,
    )
    expect(bannerEntry(failed, { itemId: 0, url: '', runId: 9, autopilot: nothing })).toBe(true)
    expect(bannerEntry(failed, { itemId: 0, url: '', runId: 8, autopilot: nothing })).toBe(false)
    expect(bannerEntry(notification({ url: '' }), { itemId: 0, url: '', runId: 0, autopilot: nothing })).toBe(false)
  })

  it('tells a new version’s banner, which names nothing but its entry, by the entry’s kind', () => {
    const none = { itemId: 0, url: '', runId: 0, autopilot: nothing }
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

  it('opens what a clicked banner’s entry opens, by its kind', () => {
    const none = { itemId: 0, url: '', runId: 0, autopilot: nothing }
    const update = notification({ kind: 'update_ready', itemId: 0, itemKind: '', url: '' })
    const page = 'https://github.com/acme-org/sdk/releases/tag/v1.2.0'
    // An entry about a page that also names a run, which only a failed run's entry opens.
    const released = notification({ kind: 'release_done', itemId: 0, itemKind: '', url: page, runId: 9 })

    expect(bannerOpens(failed, { ...none, runId: 9 }, true)).toBe('run')
    expect(bannerOpens(update, none, true)).toBe('update')
    expect(bannerOpens(notification(), { ...none, itemId: 1 }, true)).toBe('item')
    expect(bannerOpens(released, { ...none, url: page, runId: 9 }, true)).toBe('item')
  })

  it('opens what a clicked banner names when the log doesn’t hold its entry, or holds another', () => {
    const none = { itemId: 0, url: '', runId: 0, autopilot: nothing }

    expect(bannerOpens(undefined, { ...none, runId: 9 }, true)).toBe('run')
    expect(bannerOpens(undefined, none, true)).toBe('update')
    expect(bannerOpens(undefined, { ...none, itemId: 1 }, true)).toBe('item')
    // A request that names no notification is never the update's.
    expect(bannerOpens(undefined, none, false)).toBe('item')
    // An entry of another kind that took the id after the history was cleared.
    expect(bannerOpens(notification({ kind: 'woke', itemId: 0, url: '' }), none, true)).toBe('update')
  })

  it('opens the Autopilot tab from an Autopilot banner, the entry it stood for or not', () => {
    const alert = autopilotAlert({ itemId: 0, itemKind: '', url: '' })
    const banner = { itemId: 0, url: '', runId: 0, autopilot: alert.autopilot }

    expect(updateBanner(banner)).toBe(false)
    expect(bannerEntry(alert, banner)).toBe(true)
    expect(bannerOpens(alert, banner, true)).toBe('autopilot')
    // The log hasn’t loaded it.
    expect(bannerOpens(undefined, banner, true)).toBe('autopilot')

    // An entry that took the id after the history was cleared, or a target made later.
    expect(bannerEntry(notification({ kind: 'woke', itemId: 0, url: '' }), banner)).toBe(false)
    expect(
      bannerEntry(alert, { ...banner, autopilot: { ...alert.autopilot, createdAt: '2026-06-16T08:00:00Z' } }),
    ).toBe(false)

    // The menu bar's row of what needs your OK names Activity, and no notification.
    expect(bannerOpens(undefined, { ...banner, autopilot: autopilotTarget({ activity: true }) }, false)).toBe(
      'autopilot',
    )
  })

  it('opens the item an Autopilot banner is about, the entry it stood for or not', () => {
    const alert = autopilotAlert()
    const banner = { itemId: 1, url: alert.url, runId: 0, autopilot: alert.autopilot }

    expect(bannerOpens(alert, banner, true)).toBe('item')
    // The log hasn’t loaded it.
    expect(bannerOpens(undefined, banner, true)).toBe('item')
    // Its item is no longer stored: its page opens.
    expect(bannerOpens(undefined, { ...banner, itemId: 0 }, true)).toBe('item')
  })

  it('marks an Autopilot entry about an item read as that item’s sheet opens', () => {
    const items = [autopilotAlert({ id: 4, itemId: 1 }), autopilotAlert({ id: 3, itemId: 2 }), notification({ id: 2 })]

    expect(unreadOf(items, 1)).toEqual([4, 2])
  })
})
