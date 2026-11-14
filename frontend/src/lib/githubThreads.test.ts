import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { githubThread, githubThreads, item, itemThreads, threadsStatus } from '../test/fixtures'
import {
  askAgainText,
  badgeText,
  buttonLabel,
  checkedText,
  countsText,
  groupWhere,
  itemURL,
  noTags,
  reasonsText,
  reasonText,
  refusal,
  tagText,
  tagTitle,
  threadGroups,
  threadItem,
  tagOf,
  threadTags,
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
  it('caps the badge at 99+', () => {
    expect(badgeText(7)).toBe('7')
    expect(badgeText(99)).toBe('99')
    expect(badgeText(100)).toBe('99+')
  })

  it('says what the GitHub inbox holds, and how much of it is unread', () => {
    expect(countsText(threadsStatus({ count: 38, unread: 5 }))).toBe('38 in your GitHub inbox · 5 unread')
    expect(countsText(threadsStatus({ count: 38 }))).toBe('38 in your GitHub inbox')
    expect(countsText(threadsStatus({ count: 1234, unread: 1000 }))).toBe('1,234 in your GitHub inbox · 1,000 unread')
    expect(countsText(threadsStatus())).toBe('Nothing in your GitHub inbox')
    expect(countsText(threadsStatus({ lastPassAt: '' }))).toBe('Not read from GitHub yet')
  })

  it('names the button with what its badge says', () => {
    expect(buttonLabel(threadsStatus())).toBe('GitHub notifications')
    expect(buttonLabel(threadsStatus({ count: 38 }))).toBe('GitHub notifications, 38 in your inbox')
    expect(buttonLabel(threadsStatus({ count: 38, unread: 5 }))).toBe(
      'GitHub notifications, 38 in your inbox, 5 unread',
    )
    expect(buttonLabel(threadsStatus({ access: 'refused', count: 38 }))).toBe('GitHub notifications, can’t be read')
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

    expect(groups.map((g) => [g.key, g.count, g.reasons, g.updatedAt])).toEqual([
      ['acme-org/example-repo#7', 3, ['mention', 'review_requested'], '2026-06-15T11:58:00Z'],
      ['acme-org/example-repo#9', 1, ['review_requested'], '2026-06-15T11:55:00Z'],
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

describe('threadTags', () => {
  it('maps each item with unread notifications, leaving out those with none unread', () => {
    const tags = threadTags(
      githubThreads({
        byItem: [itemThreads(), itemThreads({ number: 8, unread: 0, total: 2 }), itemThreads({ number: 9, unread: 2 })],
      }),
    )

    expect([...tags.keys()]).toEqual(['acme-org/example-repo#7', 'acme-org/example-repo#9'])
    expect(tagOf(tags, item({ repo: 'Acme-Org/Example-Repo', number: 9 }))?.unread).toBe(2)
    expect(tagOf(tags, item({ number: 8 }))).toBeUndefined()
  })

  it('keeps each entry that shows the same, and the whole map when nothing changed', () => {
    const was = threadTags(githubThreads({ byItem: [itemThreads(), itemThreads({ number: 9, unread: 2 })] }))
    const same = threadTags(githubThreads({ byItem: [itemThreads(), itemThreads({ number: 9, unread: 2 })] }), was)
    const changed = threadTags(githubThreads({ byItem: [itemThreads(), itemThreads({ number: 9, unread: 3 })] }), was)

    expect(same).toBe(was)
    expect(changed).not.toBe(was)
    expect(changed.get('acme-org/example-repo#7')).toBe(was.get('acme-org/example-repo#7'))
    expect(changed.get('acme-org/example-repo#9')).not.toBe(was.get('acme-org/example-repo#9'))
    expect(changed.get('acme-org/example-repo#9')?.unread).toBe(3)
  })

  it('holds only what a card shows, so another read notification changes nothing', () => {
    const was = threadTags(githubThreads({ byItem: [itemThreads({ unread: 2, total: 2 })] }))
    const more = threadTags(githubThreads({ byItem: [itemThreads({ unread: 2, total: 5 })] }), was)

    expect(was.get('acme-org/example-repo#7')).toEqual({ unread: 2, reasons: ['review_requested'] })
    expect(more).toBe(was)
  })

  it('takes in a change of reasons, and lets an item that left go', () => {
    const was = threadTags(githubThreads({ byItem: [itemThreads(), itemThreads({ number: 9 })] }))
    const reasons = threadTags(githubThreads({ byItem: [itemThreads({ reasons: ['mention'] })] }), was)

    expect(reasons.size).toBe(1)
    expect(reasons.get('acme-org/example-repo#7')).not.toBe(was.get('acme-org/example-repo#7'))
    expect(reasons.get('acme-org/example-repo#7')?.reasons).toEqual(['mention'])
  })

  it('holds nothing before the mirror loads, or while the app does not read them', () => {
    const was = threadTags(githubThreads({ byItem: [itemThreads()] }))

    expect(threadTags(null)).toBe(noTags)
    expect(threadTags(githubThreads({ status: { reading: false }, byItem: [itemThreads()] }))).toBe(noTags)
    expect(threadTags(githubThreads({ status: { reading: false } }), was)).toBe(noTags)
    expect(threadTags(githubThreads())).toBe(noTags)
    expect(threadTags(githubThreads(), was)).toBe(noTags)
  })

  it('keeps the newest entry of an item GitHub names in two cases', () => {
    const tags = threadTags(
      githubThreads({ byItem: [itemThreads({ unread: 2 }), itemThreads({ owner: 'Acme-Org', unread: 5 })] }),
    )

    expect(tags.size).toBe(1)
    expect(tagOf(tags, item())?.unread).toBe(2)
  })
})

describe('the card’s tag', () => {
  it('counts the unread, in the singular for one', () => {
    expect(tagText(1)).toBe('1 unread on GitHub')
    expect(tagText(2)).toBe('2 unread on GitHub')
  })

  it('gives in its tooltip why GitHub notified you about the item, read notifications included', () => {
    expect(tagTitle({ unread: 2, reasons: ['mention', 'review_requested'] })).toBe(
      '2 unread on GitHub · GitHub notified you: Mentioned, Review requested',
    )
    expect(tagTitle({ unread: 1, reasons: [] })).toBe('1 unread on GitHub')
  })
})
