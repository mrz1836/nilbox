import { describe, expect, it } from 'vitest'

import type { NotificationKind } from '../platform'
import { githubThread, notification } from '../test/fixtures'
import {
  appNotificationSuggestions,
  filterAppNotifications,
  filterGitHubThreads,
  githubNotificationSuggestions,
  notificationHint,
  notificationSpec,
  reposNamed,
} from './notificationFilter'

describe('reposNamed', () => {
  const repos = ['acme-org/example-repo', 'acme-org/example-repo-docs', 'acme-org/other-repo']

  it('names the repositories the repo: filters keep, as the list keeps them', () => {
    expect(reposNamed('', repos)).toBeNull()
    expect(reposNamed('flaky test', repos)).toBeNull()
    expect(reposNamed('repo:acme-org/example-repo', repos)).toEqual(new Set(['acme-org/example-repo']))
    expect(reposNamed('repo:example', repos)).toEqual(new Set(['acme-org/example-repo', 'acme-org/example-repo-docs']))
    expect(reposNamed('repo:example -repo:docs', repos)).toEqual(new Set(['acme-org/example-repo']))
    expect(reposNamed('repo:ACME-ORG/OTHER-REPO', repos)).toEqual(new Set(['acme-org/other-repo']))
    expect(reposNamed('repo:nowhere', repos)).toEqual(new Set())
  })
})

describe('notificationSpec', () => {
  it('knows the repository, the type, the state, and GitHub’s reason, several states at once', () => {
    expect(notificationSpec).toEqual({ keys: ['repo', 'type', 'is', 'reason'], multi: ['is'] })
  })
})

describe('the app’s own history', () => {
  const review = notification({ id: 5 })
  const woke = notification({
    id: 4,
    kind: 'woke',
    read: true,
    title: 'Bump the parser',
    number: 8,
    detail: 'approved, 2 new commits',
  })
  const released = notification({
    id: 3,
    kind: 'release_done',
    itemId: 0,
    itemKind: '',
    repo: 'acme-org/sdk',
    number: 0,
    title: 'v1.4.3 is on GitHub',
    detail: 'Tagged a1b2c3d on master.',
  })
  const failed = notification({
    id: 2,
    kind: 'run_failed',
    itemId: 0,
    itemKind: '',
    repo: '',
    number: 0,
    title: 'A script failed in ~/projects/notes: lint',
    runId: 9,
  })
  const all = [review, woke, released, failed]

  // ids keeps the entries a query matches, by their ids, or null for one that filters
  // nothing.
  const ids = (query: string) => filterAppNotifications(all, query)?.map((n) => n.id) ?? null

  it('filters nothing for an empty query', () => {
    expect(ids('')).toBeNull()
    expect(ids('   ')).toBeNull()
  })

  it.each([
    ['widget', [5]],
    ['bump', [4]],
    ['acme-org/sdk', [3]],
    ['#8', [4]],
    ['7', [5]],
    ['back from snooze', [4]],
    ['approved', [4]],
    ['released', [3]],
    ['run failed', [2]],
    ['tagged', [3]],
    ['-widget', [4, 3, 2]],
    ['repo:sdk', [3]],
    ['repo:acme-org/example-repo', [5, 4]],
    ['type:release', [3]],
    ['type:review,run', [5, 2]],
    ['-type:snooze', [5, 3, 2]],
    ['is:unread', [5, 3, 2]],
    ['is:read', [4]],
    ['is:unread repo:example', [5]],
    ['reason:mention', []],
    ['-reason:mention', [5, 4, 3, 2]],
  ])('keeps for %j the entries %j', (query, want) => {
    expect(ids(query)).toEqual(want)
  })

  it('offers the unread, the types, and the repositories on an empty query', () => {
    const list = appNotificationSuggestions(all, '', 0)

    expect(list).toMatchObject({ label: 'Filters', auto: true, hint: notificationHint })
    expect(list.options.map((o) => [o.label, o.detail, o.count])).toEqual([
      ['Unread', 'is:unread', '3'],
      ['Type', 'type:', '4 types'],
      ['Repository', 'repo:', '2 repos'],
    ])
  })

  it('offers the values of the key being typed, counted among what the rest of the query keeps', () => {
    const values = (src: string) =>
      appNotificationSuggestions(all, src, src.length).options.map((o) => `${o.id} ${o.count}`)

    expect(values('type:')).toEqual(['type:release 1', 'type:review 1', 'type:run 1', 'type:snooze 1'])
    expect(values('is:unread repo:')).toEqual(['repo:acme-org/example-repo 1', 'repo:acme-org/sdk 1'])
    expect(values('is:')).toEqual(['is:unread 3', 'is:read 1'])
    expect(appNotificationSuggestions(all, 'repo:', 5)).toMatchObject({ label: 'Repositories' })
  })

  it('matches an entry of a kind this build doesn’t know by its words alone', () => {
    const mentions = notification({ id: 9, kind: 'mentions' as unknown as NotificationKind, title: 'Ping' })

    expect(filterAppNotifications([mentions], 'ping')?.map((n) => n.id)).toEqual([9])
    expect(filterAppNotifications([mentions], 'type:review')).toEqual([])
    expect(filterAppNotifications([mentions], '-type:review')?.map((n) => n.id)).toEqual([9])
  })

  it('leaves out Unread while nothing is', () => {
    const list = appNotificationSuggestions([woke], '', 0)

    expect(list.options.map((o) => o.label)).toEqual(['Type', 'Repository'])
  })
})

describe('the user’s GitHub notifications', () => {
  const pr = githubThread({ id: 1, reason: 'review_requested', state: 'OPEN' })
  const again = githubThread({ id: 2, reason: 'mention', unread: false, state: 'OPEN' })
  const merged = githubThread({
    id: 3,
    number: 9,
    title: 'Fix the parser',
    reason: 'author',
    unread: false,
    state: 'MERGED',
  })
  const closed = githubThread({
    id: 4,
    owner: 'acme-org',
    name: 'sdk',
    kind: 'issue',
    subjectType: 'Issue',
    number: 3,
    title: 'Crash on start',
    reason: 'assign',
    state: 'CLOSED',
  })
  const release = githubThread({
    id: 5,
    kind: '',
    number: 0,
    subjectType: 'Release',
    title: 'v1.4.3',
    reason: 'subscribed',
    state: '',
  })
  const workflow = githubThread({
    id: 6,
    owner: '',
    name: '',
    kind: '',
    number: 0,
    subjectType: 'CheckSuite',
    title: 'CI failed',
    reason: 'ci_activity',
    state: '',
  })
  const all = [pr, again, merged, closed, release, workflow]

  // lines keeps the lines a query matches, each by its notifications' ids, or null for one
  // that filters nothing.
  const lines = (query: string) => filterGitHubThreads(all, query)?.groups.map((g) => g.ids) ?? null

  it('filters nothing for an empty query', () => {
    expect(lines('')).toBeNull()
  })

  it.each([
    ['widget', [[1, 2]]],
    ['#9', [[3]]],
    ['sdk', [[4]]],
    ['mentioned', [[2]]],
    ['workflow run', [[6]]],
    ['release', [[5]]],
    ['is:unread', [[1], [4], [5], [6]]],
    ['is:read', [[2], [3]]],
    ['is:merged', [[3]]],
    ['is:closed', [[4]]],
    ['is:open is:unread', [[1]]],
    ['is:open', [[1, 2]]],
    ['type:pr', [[1, 2], [3]]],
    ['type:issue', [[4]]],
    ['type:actions', [[6]]],
    ['type:other', []],
    ['type:release,actions', [[5], [6]]],
    ['reason:assign', [[4]]],
    ['reason:mention', [[2]]],
    ['repo:acme-org/sdk', [[4]]],
    ['-repo:example-repo', [[4], [6]]],
  ])('keeps for %j the lines %j', (query, want) => {
    expect(lines(query)).toEqual(want)
  })

  it('reads a notification with no reason as having none', () => {
    const quiet = githubThread({ id: 7, reason: '', title: 'Quiet one' })

    expect(filterGitHubThreads([quiet], 'reason:mention')?.groups).toEqual([])
    expect(filterGitHubThreads([quiet], 'quiet')?.matches).toBe(1)
  })

  it('counts each line once, however many of its notifications match', () => {
    expect(filterGitHubThreads(all, 'widget')?.matches).toBe(1)
    expect(filterGitHubThreads(all, 'type:pr')?.matches).toBe(2)
  })

  it('offers the unread, the states, the types, the repositories, and the reasons on an empty query', () => {
    const list = githubNotificationSuggestions(all, '', 0)

    expect(list).toMatchObject({ label: 'Filters', auto: true, hint: notificationHint })
    expect(list.options.map((o) => [o.label, o.detail, o.count])).toEqual([
      ['Unread', 'is:unread', '4'],
      ['Merged', 'is:merged', '1'],
      ['Closed', 'is:closed', '1'],
      ['Type', 'type:', '4 types'],
      ['Repository', 'repo:', '2 repos'],
      ['Reason', 'reason:', '6 reasons'],
    ])
  })

  it('offers the reasons as GitHub writes them, and leaves out states no notification is in', () => {
    const values = (src: string) =>
      githubNotificationSuggestions([pr, again], src, src.length).options.map((o) => `${o.id} ${o.count}`)

    expect(values('reason:')).toEqual(['reason:mention 1', 'reason:review_requested 1'])
    expect(values('is:')).toEqual(['is:open 2', 'is:read 1', 'is:unread 1'])
    expect(githubNotificationSuggestions([pr], '', 0).options.map((o) => o.label)).toEqual([
      'Unread',
      'Type',
      'Repository',
      'Reason',
    ])
  })
})
