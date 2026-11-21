import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { githubThread, queuedClear, settledClear, threadsStatus } from '../test/fixtures'
import {
  askAgainText,
  byHandText,
  clearsIn,
  clearTriggers,
  clearURL,
  clearWhere,
  doneURL,
  filterThreads,
  githubTypeOf,
  githubTypeOrder,
  githubTypes,
  inboxURL,
  settledText,
  shownSettled,
  stateWords,
  triggerText,
  buttonLabel,
  checkedText,
  countsText,
  groupURL,
  groupWhere,
  itemURL,
  reasonsText,
  reasonText,
  refusal,
  summaryText,
  threadGroups,
  threadItem,
} from './githubThreads'

describe('threadItem and itemURL', () => {
  it('builds a pull request’s and an issue’s link from their parts', () => {
    const pr = threadItem({ owner: 'acme-org', name: 'example-repo', kind: 'pr', number: 7 })
    const issue = threadItem({ owner: 'acme-org', name: 'example.repo_2', kind: 'issue', number: 12 })

    expect(pr).not.toBeNull()
    expect(issue).not.toBeNull()
    expect(pr === null ? '' : itemURL(pr)).toBe('https://github.com/acme-org/example-repo/pull/7')
    expect(issue === null ? '' : itemURL(issue)).toBe('https://github.com/acme-org/example.repo_2/issues/12')
  })

  it.each([
    ['no kind, as a release has', { kind: '' as const }],
    ['no number', { number: 0 }],
    ['a number that is no whole number', { number: 1.5 }],
    ['no owner', { owner: '' }],
    ['an owner that climbs out of the path', { owner: '..' }],
    ['a name with a slash', { name: 'example-repo/../../evil' }],
    ['a name with a query', { name: 'example-repo?x=1' }],
    ['a name with a space', { name: 'example repo' }],
  ])('takes a thread with %s as about no item', (_, change) => {
    expect(threadItem({ owner: 'acme-org', name: 'example-repo', kind: 'pr', number: 7, ...change })).toBeNull()
  })
})

describe('reasonText', () => {
  it.each([
    ['review_requested', 'Review requested'],
    ['assign', 'Assigned'],
    ['mention', 'Mentioned'],
    ['team_mention', 'Team mentioned'],
    ['author', 'You opened it'],
    ['comment', 'You commented'],
    ['subscribed', 'Watching'],
    ['state_change', 'You changed its state'],
    ['ci_activity', 'Workflow run'],
    ['manual', 'Subscribed'],
    ['security_alert', 'Security alert'],
    ['invitation', 'Invitation'],
    ['approval_requested', 'Approval requested'],
  ])('words %s as %j', (reason, words) => {
    expect(reasonText(reason)).toBe(words)
  })

  it('shows a reason it does not know as GitHub wrote it, and never one inherited by every object', () => {
    expect(reasonText('member_feature_requested')).toBe('member_feature_requested')
    expect(reasonText('constructor')).toBe('constructor')
  })

  it('words each reason once, in order', () => {
    expect(reasonsText(['mention', 'review_requested', 'mention', ''])).toBe('Mentioned, Review requested')
    expect(reasonsText([])).toBe('')
  })
})

describe('the counts', () => {
  it('says what the GitHub inbox holds, the unread first, as the badge counts them', () => {
    expect(countsText(threadsStatus({ count: 38, unread: 5 }))).toBe('5 unread · 38 in your GitHub inbox')
    expect(countsText(threadsStatus({ count: 38 }))).toBe('38 in your GitHub inbox')
    expect(countsText(threadsStatus({ count: 1234, unread: 1000 }))).toBe('1,000 unread · 1,234 in your GitHub inbox')
    expect(countsText(threadsStatus())).toBe('Nothing in your GitHub inbox')
    expect(countsText(threadsStatus({ lastPassAt: '' }))).toBe('Not read from GitHub yet')
  })

  it('names the button with what its badge says', () => {
    expect(buttonLabel(threadsStatus())).toBe('GitHub notifications')
    expect(buttonLabel(threadsStatus({ count: 38 }))).toBe('GitHub notifications, 38 in your inbox')
    expect(buttonLabel(threadsStatus({ count: 38, unread: 5 }))).toBe(
      'GitHub notifications, 5 unread, 38 in your inbox',
    )
    expect(buttonLabel(threadsStatus({ access: 'refused', count: 38 }))).toBe('GitHub notifications, can’t be read')
  })

  it('sums the panel up: the counts, then when GitHub was checked, or that it is being checked', () => {
    const status = threadsStatus({ count: 38, unread: 5, lastPassAt: '2026-06-15T11:58:00Z' })

    expect(summaryText(FIXED_NOW, status, false)).toBe('5 unread · 38 in your GitHub inbox · checked 2m ago')
    expect(summaryText(FIXED_NOW, status, true)).toBe('5 unread · 38 in your GitHub inbox · checking…')
    expect(summaryText(FIXED_NOW, threadsStatus({ lastPassAt: '' }), false)).toBe('Not read from GitHub yet')
  })

  it('says when the inbox was last checked', () => {
    expect(checkedText(FIXED_NOW, threadsStatus({ count: 38, lastPassAt: '2026-06-15T11:58:00Z' }))).toBe(
      '38 in your GitHub inbox · checked 2m ago',
    )
    expect(checkedText(FIXED_NOW, threadsStatus({ lastPassAt: '2026-06-15T12:00:00Z' }))).toBe(
      'Nothing in your GitHub inbox · checked just now',
    )
  })

  it('says why GitHub refuses them, and when it is asked again', () => {
    const refused = threadsStatus({ access: 'refused', askAgainAt: '2026-06-15T17:00:00Z', lastError: '' })

    expect(refusal(refused)).toBe('Your GitHub token can’t read notifications.')
    expect(refusal({ ...refused, lastError: "Your GitHub token can't read notifications." })).toBe(
      "Your GitHub token can't read notifications.",
    )
    expect(askAgainText(FIXED_NOW, refused)).toBe('Asks GitHub again in 5h.')
    expect(askAgainText(FIXED_NOW, { ...refused, askAgainAt: '2026-06-15T11:00:00Z' })).toBe(
      'Asks GitHub again at the next check.',
    )
    expect(askAgainText(FIXED_NOW, { ...refused, askAgainAt: '' })).toBe('')
  })
})

describe('threadGroups', () => {
  it('groups the unread by the item each is about, in the order of each item’s newest', () => {
    const groups = threadGroups([
      githubThread({ id: 1, reason: 'mention', updatedAt: '2026-06-15T11:58:00Z' }),
      githubThread({ id: 2, number: 9, kind: 'issue', subjectType: 'Issue', title: 'Fix the parser' }),
      githubThread({ id: 3, reason: 'review_requested', updatedAt: '2026-06-15T11:00:00Z' }),
      githubThread({ id: 4, reason: 'mention' }),
    ])

    expect(groups.map((g) => [g.key, g.count, g.ids, g.reasons, g.updatedAt])).toEqual([
      ['acme-org/example-repo#7', 3, [1, 3, 4], ['mention', 'review_requested'], '2026-06-15T11:58:00Z'],
      ['acme-org/example-repo#9', 1, [2], ['review_requested'], '2026-06-15T11:55:00Z'],
    ])
    expect(groups[0]?.item).toEqual({ owner: 'acme-org', name: 'example-repo', kind: 'pr', number: 7 })
    expect(groups[1] === undefined ? '' : groupWhere(groups[1])).toBe('example-repo #9')
  })

  it('keeps each notification about no item apart, named by its repository and subject', () => {
    const groups = threadGroups([
      githubThread({ id: 5, kind: '', number: 0, subjectType: 'Release', title: 'v1.4.3', reason: 'subscribed' }),
      githubThread({ id: 6, kind: '', number: 0, subjectType: 'Release', title: 'v1.4.2', reason: 'subscribed' }),
      githubThread({ id: 7, owner: '', name: '', kind: '', number: 0, subjectType: 'Unknown', title: 'Something' }),
    ])

    expect(groups.map((g) => [g.key, g.item, groupWhere(g)])).toEqual([
      ['thread-5', null, 'acme-org/example-repo · Release'],
      ['thread-6', null, 'acme-org/example-repo · Release'],
      ['thread-7', null, 'Unknown'],
    ])
  })
})

describe('groupURL', () => {
  // urlOf is where a line about thread opens.
  const urlOf = (thread: Parameters<typeof githubThread>[0]) => {
    const [group] = threadGroups([githubThread(thread)])

    if (group === undefined) {
      throw new Error('no line')
    }

    return groupURL(group)
  }

  it('opens an item on its own page', () => {
    expect(urlOf({})).toBe('https://github.com/acme-org/example-repo/pull/7')
    expect(urlOf({ kind: 'issue', number: 9, subjectType: 'Issue' })).toBe(
      'https://github.com/acme-org/example-repo/issues/9',
    )
  })

  it('opens one about no item on the page of its repository it belongs on', () => {
    const about = (subjectType: string) => urlOf({ kind: '', number: 0, subjectType })

    expect(about('CheckSuite')).toBe('https://github.com/acme-org/example-repo/actions')
    expect(about('Release')).toBe('https://github.com/acme-org/example-repo/releases')
    expect(about('Discussion')).toBe('https://github.com/acme-org/example-repo/discussions')
    expect(about('RepositoryDependabotAlertsThread')).toBe(
      'https://github.com/acme-org/example-repo/security/dependabot',
    )
    expect(about('SomethingNew')).toBe('https://github.com/acme-org/example-repo')
  })

  it('opens the GitHub inbox for a repository no link could hold', () => {
    const about = (owner: string, name: string) => urlOf({ owner, name, kind: '', number: 0, subjectType: 'Release' })

    expect(about('', '')).toBe('https://github.com/notifications')
    expect(about('acme-org', '..')).toBe('https://github.com/notifications')
    expect(about('acme org', 'example-repo')).toBe('https://github.com/notifications')
    expect(about('acme-org', 'example-repo/../../evil')).toBe('https://github.com/notifications')
  })
})

describe('threadGroups with read ones', () => {
  it('counts each line’s unread apart from its notifications, and takes its item’s state', () => {
    const groups = threadGroups([
      githubThread({ id: 1, unread: false, state: '' }),
      githubThread({ id: 2, unread: true, state: 'MERGED' }),
      githubThread({ id: 3, unread: true, state: 'OPEN' }),
      githubThread({ id: 4, number: 9, unread: false, state: 'CLOSED' }),
    ])

    expect(groups.map((g) => [g.count, g.ids, g.unread, g.unreadIds, g.state, g.threads.map((t) => t.id)])).toEqual([
      [3, [1, 2, 3], 2, [2, 3], 'MERGED', [1, 2, 3]],
      [1, [4], 0, [], 'CLOSED', [4]],
    ])
  })
})

describe('stateWords', () => {
  it('chips a merged or closed item, and nothing else', () => {
    expect(stateWords('MERGED')).toBe('Merged')
    expect(stateWords('CLOSED')).toBe('Closed')
    expect(stateWords('OPEN')).toBe('')
    expect(stateWords('')).toBe('')
  })
})

describe('the GitHub tab’s types', () => {
  it('offers every type, each with its words', () => {
    expect(githubTypeOrder.map((id) => githubTypes[id].label)).toEqual([
      'All',
      'Pull requests',
      'Issues',
      'Releases',
      'Actions',
      'Other',
    ])

    for (const id of githubTypeOrder) {
      expect(githubTypes[id].none).not.toBe('')
    }
  })

  it.each([
    [{ kind: 'pr' as const, subjectType: 'PullRequest' }, 'pr'],
    [{ kind: '' as const, subjectType: 'PullRequest' }, 'pr'],
    [{ kind: 'issue' as const, subjectType: 'Issue' }, 'issue'],
    [{ kind: '' as const, subjectType: 'Issue' }, 'issue'],
    [{ kind: '' as const, subjectType: 'Release' }, 'release'],
    [{ kind: '' as const, subjectType: 'CheckSuite' }, 'actions'],
    [{ kind: '' as const, subjectType: 'WorkflowRun' }, 'actions'],
    [{ kind: '' as const, subjectType: 'Discussion' }, 'other'],
    [{ kind: '' as const, subjectType: 'RepositoryVulnerabilityAlert' }, 'other'],
  ])('types %j as %s', (thread, want) => {
    expect(githubTypeOf(thread)).toBe(want)
  })

  it('shows the unread or all, of a type or of every type, and no notification while Cleared shows', () => {
    const pr = githubThread({ id: 1 })
    const readIssue = githubThread({ id: 2, kind: 'issue', subjectType: 'Issue', unread: false })
    const release = githubThread({ id: 3, kind: '', number: 0, subjectType: 'Release' })
    const all = [pr, readIssue, release]

    expect(filterThreads(all, 'all', 'all')).toEqual(all)
    expect(filterThreads(all, 'unread', 'all')).toEqual([pr, release])
    expect(filterThreads(all, 'all', 'issue')).toEqual([readIssue])
    expect(filterThreads(all, 'unread', 'issue')).toEqual([])
    expect(filterThreads(all, 'cleared', 'all')).toEqual([])
  })
})

describe('the Cleared view’s words', () => {
  it('moves the triggers’ words from Settings word for word, and words clearing by hand', () => {
    expect(clearTriggers.map((t) => t.trigger)).toEqual([
      'merged',
      'pr_closed',
      'issue_closed',
      'reviewed',
      'dismissed',
      'unassigned',
    ])
    expect(triggerText('merged')).toBe('A pull request is merged')
    expect(triggerText('reviewed')).toBe('You approve or request changes on a pull request')
    expect(triggerText('by_hand')).toBe(byHandText)
    expect(triggerText('later' as Parameters<typeof triggerText>[0])).toBe('later')
  })

  it.each([
    ['2026-06-15T12:00:12Z', 'in 12s'],
    ['2026-06-15T12:00:00.4Z', 'in 1s'],
    ['2026-06-15T12:05:00Z', 'in 5m'],
    ['2026-06-15T12:00:00Z', 'any moment'],
    ['2026-06-15T11:59:00Z', 'any moment'],
    ['', ''],
  ])('says a clear sent after %j goes %j', (sendAfter, want) => {
    expect(clearsIn(FIXED_NOW, sendAfter)).toBe(want)
  })

  it.each([
    [settledClear(), 'Marked done'],
    [settledClear({ action: 'read' }), 'Marked read'],
    [settledClear({ state: 'kept' }), 'Kept'],
    [settledClear({ state: 'failed', error: 'GitHub refused it.' }), 'Couldn’t clear: GitHub refused it.'],
    [settledClear({ state: 'failed' }), 'Couldn’t clear'],
    [settledClear({ state: 'skipped', reason: 'new_activity' }), 'Skipped: there was new activity'],
    [settledClear({ state: 'skipped', reason: 'still_open' }), 'Skipped: it was reopened'],
    [settledClear({ state: 'skipped', reason: 'off' }), 'Skipped: clearing was turned off'],
    [settledClear({ state: 'skipped', reason: 'rule_off' }), 'Skipped: its rule is off'],
    [settledClear({ state: 'skipped', reason: 'already_read' }), 'Skipped: it was already read'],
    [settledClear({ state: 'skipped', reason: 'not_item' }), 'Skipped: it isn’t a pull request or an issue'],
    [settledClear({ state: 'skipped', reason: 'not_listed' }), 'Skipped: it left your GitHub inbox first'],
    [settledClear({ state: 'skipped', reason: 'kept' }), 'Skipped: you kept it before'],
    [settledClear({ state: 'skipped', reason: 'failed' }), 'Skipped: it failed before'],
    [settledClear({ state: 'skipped', reason: '' }), 'Skipped'],
    [settledClear({ state: 'queued' }), 'queued'],
  ])('words how %j settled', (clear, want) => {
    expect(settledText(clear)).toBe(want)
  })

  it('hides a skip the backend decided again', () => {
    expect(shownSettled(settledClear())).toBe(true)
    expect(shownSettled(settledClear({ state: 'skipped', reason: 'new_activity' }))).toBe(true)
    expect(shownSettled(settledClear({ state: 'skipped', reason: 'changed' }))).toBe(false)
    expect(shownSettled(settledClear({ state: 'skipped', reason: 'stale' }))).toBe(false)
  })

  it('says where a clear is, and opens its item, or else the GitHub inbox', () => {
    expect(clearWhere(queuedClear())).toBe('example-repo #7')
    expect(clearWhere(queuedClear({ number: 0 }))).toBe('example-repo')
    expect(clearURL(settledClear())).toBe('https://github.com/acme-org/example-repo/pull/7')
    expect(clearURL(queuedClear({ kind: 'issue', number: 3 }))).toBe(
      'https://github.com/acme-org/example-repo/issues/3',
    )
    expect(clearURL(settledClear({ owner: 'acme org' }))).toBe(inboxURL)
    expect(doneURL).toBe('https://github.com/notifications?query=is%3Adone')
  })
})
