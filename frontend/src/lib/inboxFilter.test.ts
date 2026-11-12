import { describe, expect, it } from 'vitest'

import type { InboxItem } from '../platform'
import { FIXED_NOW } from '../test/clock'
import { details, item as fixture, list, project, snapshot } from '../test/fixtures'
import { filterInbox, inboxItems, inboxSpec, inboxSuggestions, knownValues, matchesItem } from './inboxFilter'
import { parseQuery } from './query'
import { triageInbox, type TriagedInbox } from './triage'

// item is the fixture's item with no branch and no teams, so only what a test gives it
// matches.
function item(overrides: Partial<InboxItem>): InboxItem {
  return fixture({ branch: '', reviewTeams: [], ...overrides })
}

// The inbox under test: pull requests and issues across two repositories of two orgs,
// with every kind of value the filter reads.
const cache = item({
  id: 1,
  number: 7,
  title: 'Add the widget cache',
  repo: 'acme-org/example-repo',
  author: 'octo-user',
  branch: 'widget-cache',
  reviewTeams: ['platform-team'],
  details: details({
    labels: [
      { name: 'enhancement', color: '' },
      { name: 'good first issue', color: '' },
    ],
    checks: { state: 'FAILURE', passed: 3, failed: 1, pending: 0, skipped: 0 },
    reviews: { decision: 'APPROVED', reviewers: 1, reviewed: 1, approvals: 1, changesRequested: 0 },
  }),
})

const draft = item({
  id: 2,
  number: 12,
  title: 'Café menu',
  repo: 'acme-org/example-repo',
  author: 'me-user',
  branch: 'menu',
  isDraft: true,
  pinnedAt: '2026-06-15T10:00:00Z',
  details: details({
    labels: [{ name: 'bug', color: '' }],
    mergeable: 'CONFLICTING',
    checks: { state: 'PENDING', passed: 0, failed: 0, pending: 2, skipped: 0 },
    reviews: { decision: 'CHANGES_REQUESTED', reviewers: 1, reviewed: 1, approvals: 0, changesRequested: 1 },
  }),
})

const required = item({
  id: 3,
  number: 123,
  title: 'Speed up 12 queries',
  repo: 'other-org/tools',
  author: 'octo-user',
  details: details({
    labels: [{ name: 'bugfix', color: '' }],
    checks: { state: 'SUCCESS', passed: 4, failed: 0, pending: 0, skipped: 0 },
    reviews: { decision: 'REVIEW_REQUIRED', reviewers: 2, reviewed: 0, approvals: 0, changesRequested: 0 },
  }),
})

const waiting = item({
  id: 4,
  number: 40,
  title: 'No checks yet',
  repo: 'other-org/tools',
  author: 'bot-user',
  details: details({
    labels: [],
    checks: { state: '', passed: 0, failed: 0, pending: 0, skipped: 0 },
    reviews: { decision: '', reviewers: 0, reviewed: 0, approvals: 0, changesRequested: 0 },
  }),
})

const issue = item({
  id: 5,
  kind: 'issue',
  number: 9,
  title: 'Roadmap issue',
  repo: 'acme-org/example-repo',
  author: 'octo-user',
  branch: '',
  details: details({
    labels: [{ name: 'bug', color: '' }],
    projects: [project({ title: 'Roadmap', status: ' In progress ' }), project({ title: 'Ops', status: '' })],
  }),
})

const bare = item({ id: 6, kind: 'issue', number: 10, title: 'Details to come', author: 'octo-user', details: null })

const snoozed = item({ id: 7, number: 70, title: 'Snoozed cache', snoozedUntil: '2026-07-01T00:00:00Z' })

const dismissed = item({ id: 8, number: 80, title: 'Dismissed cache', dismissedAt: '2026-06-14T00:00:00Z' })

const all: InboxItem[] = [cache, draft, required, waiting, issue, bare, snoozed, dismissed]

function triaged(): TriagedInbox {
  return triageInbox(
    snapshot({
      reviewRequested: list([cache, required, snoozed]),
      authored: list([draft, cache, dismissed]),
      assigned: list([issue, bare, waiting]),
    }),
    FIXED_NOW,
  )
}

const known = knownValues(all)

// ids lists the items a query matches in any list and tab, against the signed-in
// me-user.
function ids(src: string): number[] {
  const found = new Set<number>()

  for (const tabs of Object.values(filterInbox(triaged(), src, 'me-user')?.lists ?? {})) {
    for (const i of [...tabs.inbox, ...tabs.snoozed, ...tabs.dismissed, ...tabs.muted]) {
      found.add(i.id)
    }
  }

  return [...found].sort((a, b) => a - b)
}

describe('filterInbox', () => {
  it('filters nothing for a query that says nothing', () => {
    expect(filterInbox(triaged(), '', 'me-user')).toBeNull()
    expect(filterInbox(triaged(), 'repo: ', 'me-user')).toBeNull()
  })

  it.each<[string, number[]]>([
    // Words: the title, the repository, #number, author, branch, labels, projects, teams.
    ['cache', [1, 7, 8]],
    ['CACHE -snoozed', [1, 8]],
    ['cafe', [2]],
    ['example-repo widget', [1]],
    ['#7', [1]],
    ['#70', [7]],
    ['12', [2, 3]],
    ['menu', [2]],
    ['good first', [1]],
    ['"in progress"', [5]],
    ['ops', [5]],
    ['platform-team', [1]],
    ['enhancement', [1]],
    // is:
    ['is:pr', [1, 2, 3, 4, 7, 8]],
    ['is:issue', [5, 6]],
    ['is:draft', [2]],
    ['is:pinned', [2]],
    ['is:conflict', [2]],
    ['is:open', [1, 2, 3, 4, 5, 6, 7, 8]],
    ['is:pr is:draft', [2]],
    ['is:pr,issue -is:pr', [5, 6]],
    // Every item is open, so a part of a flag such as p matches them all.
    ['is:p', [1, 2, 3, 4, 5, 6, 7, 8]],
    // repo:, org:, author:
    ['repo:acme-org/example-repo', [1, 2, 5, 6, 7, 8]],
    ['repo:tools', [3, 4]],
    ['repo:tools repo:example', [1, 2, 3, 4, 5, 6, 7, 8]],
    ['org:other-org', [3, 4]],
    ['-org:acme-org', [3, 4]],
    ['author:@me', [2]],
    ['author:@me,bot is:pr', [2, 4]],
    ['author:octo', [1, 3, 5, 6, 7, 8]],
    // label:, status:, project:
    ['label:bug', [2, 5]],
    ['label:bu', [2, 3, 5]],
    ['label:bug label:enhancement', []],
    ['label:"good first issue"', [1]],
    ['-label:bug', [1, 3, 4, 6, 7, 8]],
    ['status:"in progress"', [5]],
    ['status:progress', [5]],
    ['project:roadmap', [5]],
    ['project:roadmap project:ops', [5]],
    // checks: and review:, as the cards show them.
    ['checks:failing', [1]],
    ['checks:pending', [2]],
    ['checks:passing', [3]],
    ['review:approved', [1]],
    ['review:changes_requested', [2]],
    ['review:required', [3]],
    // Items whose details are to come can't be excluded by them.
    ['-review:approved is:pr', [2, 3, 4, 7, 8]],
  ])('matches %j', (src, want) => {
    expect(ids(src)).toEqual(want)
  })

  it('matches an item pinned with its repository as pinned, and narrows the Muted tab', () => {
    const repoPinned = item({ id: 11, number: 110, title: 'Pinned with its repo cache', repoRule: 'pinned' })
    const muted = item({ id: 12, number: 120, title: 'Muted cache', repoRule: 'muted' })
    const mutedOther = item({ id: 13, number: 130, title: 'Muted other', repoRule: 'muted' })
    const inbox = triageInbox(snapshot({ reviewRequested: list([repoPinned, muted, mutedOther, cache]) }), FIXED_NOW)

    expect(filterInbox(inbox, 'is:pinned', 'me-user')?.lists.reviewRequested.inbox.map((i) => i.id)).toEqual([11])

    const filter = filterInbox(inbox, 'cache', 'me-user')
    expect(filter?.lists.reviewRequested.muted.map((i) => i.id)).toEqual([12])
    expect(filter?.matches).toBe(3)
    expect(inboxItems(inbox).map((i) => i.id)).toEqual([11, 1, 12, 13])
  })

  it('keeps each tab’s order, and counts every item once', () => {
    const filter = filterInbox(triaged(), 'cache', 'me-user')

    expect(filter).toMatchObject({ query: 'cache', matches: 3, marks: 'cache' })
    expect(filter?.lists.reviewRequested.inbox.map((i) => i.id)).toEqual([1])
    expect(filter?.lists.reviewRequested.snoozed.map((i) => i.id)).toEqual([7])
    expect(filter?.lists.authored.inbox.map((i) => i.id)).toEqual([1])
    expect(filter?.lists.authored.dismissed.map((i) => i.id)).toEqual([8])
    expect(filter?.lists.assigned).toEqual({ inbox: [], snoozed: [], dismissed: [], muted: [] })
  })

  it('reads @me as no one before the account is known', () => {
    expect(filterInbox(triaged(), 'author:@me', '')?.matches).toBe(0)
    expect(filterInbox(triaged(), 'author:@me cache', '')?.matches).toBe(0)
  })
})

describe('matchesItem', () => {
  const query = (src: string) => parseQuery(src, inboxSpec)

  it('matches no detail filter on an item whose details are still to come', () => {
    expect(matchesItem(bare, query('label:bug'), known)).toBe(false)
    expect(matchesItem(bare, query('-label:bug'), known)).toBe(true)
    expect(matchesItem(bare, query('checks:failing'), known)).toBe(false)
    expect(matchesItem(bare, query('is:issue details'), known)).toBe(true)
  })

  it('gives an issue no checks or reviews', () => {
    expect(matchesItem(issue, query('checks:failing'), known)).toBe(false)
    expect(matchesItem(issue, query('-review:approved'), known)).toBe(true)
  })
})

describe('inboxSuggestions', () => {
  const items = inboxItems(triaged())

  it('offers the facets on an empty query, each counted', () => {
    const got = inboxSuggestions(items, '', 0, 'me-user')

    expect(got.auto).toBe(true)
    expect(got.hint).toMatch(/^Matches titles/)
    expect(got.options.map((o) => `${o.label} ${o.count}`)).toEqual([
      'Pull requests 6',
      'Issues 2',
      'Drafts 1',
      'Pinned 1',
      'Repository 2 repos',
      'Label 4 labels',
      'Author 3 authors',
      'Project status 1 status',
      'Failing checks 1',
    ])
  })

  it('leaves out project status and failing checks while nothing has them', () => {
    const got = inboxSuggestions(items, 'is:pr -checks:failing ', 22, 'me-user')

    expect(got.options.map((o) => o.label)).not.toContain('Project status')
    expect(got.options.map((o) => o.label)).not.toContain('Failing checks')
  })

  it('offers a key’s values, counted within the rest of the query', () => {
    expect(inboxSuggestions(items, 'label:', 6, 'me-user')).toMatchObject({ label: 'Labels', pickFirst: true })
    expect(
      inboxSuggestions(items, 'repo:example label:b', 20, 'me-user').options.map((o) => [o.label, o.count]),
    ).toEqual([['bug', '2']])
    expect(inboxSuggestions(items, 'is:', 3, 'me-user').options.map((o) => o.label)).toEqual([
      'pr',
      'issue',
      'conflict',
      'draft',
      'pinned',
    ])
    expect(inboxSuggestions(items, 'author:@me is:', 14, 'me-user').options.map((o) => o.label)).toEqual([
      'conflict',
      'draft',
      'pinned',
      'pr',
    ])
    expect(inboxSuggestions(items, 'checks:', 7, 'me-user').options.map((o) => o.label)).toEqual([
      'failing',
      'passing',
      'pending',
    ])
  })
})
