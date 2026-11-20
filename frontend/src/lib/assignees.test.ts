import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { assigneeCandidates } from '../test/fixtures'
import type { AssigneeCandidate } from '../platform'
import {
  assigneeGroups,
  assigneeHint,
  assigneesNote,
  assignLabel,
  mergeFound,
  quickPicks,
  searchesGitHub,
} from './assignees'

// person is someone the picker offers, from source, with overrides.
function person(login: string, source: AssigneeCandidate['source'], overrides: Partial<AssigneeCandidate> = {}) {
  return {
    login,
    name: '',
    mine: false,
    source,
    reviewed: 0,
    authored: 0,
    committed: 0,
    assignedAt: '',
    lastAt: '',
    where: '',
    ...overrides,
  } satisfies AssigneeCandidate
}

const repo = 'acme-org/example-repo'

describe('assignLabel', () => {
  it('counts what an edit assigns and unassigns', () => {
    expect(assignLabel([], [])).toBe('Apply')
    expect(assignLabel(['a', 'b'], [])).toBe('Assign 2')
    expect(assignLabel([], ['a'])).toBe('Unassign 1')
    expect(assignLabel(['a', 'b'], ['c'])).toBe('Assign 2 · Unassign 1')
  })
})

describe('assigneeHint', () => {
  it('says why each is offered, in a few words', () => {
    const hint = (c: AssigneeCandidate, noun: 'issue' | 'pull request' = 'pull request') =>
      assigneeHint(c, FIXED_NOW, noun)

    expect(hint(person('octo-user', 'you', { mine: true })), 'their row says (you)').toBe('')
    expect(hint(person('octo-other', 'author'))).toBe('Opened this pull request')
    expect(hint(person('octo-other', 'author'), 'issue')).toBe('Opened this issue')
    expect(hint(person('octo-c', 'participant'), 'issue')).toBe('Took part in this issue')
    expect(hint(person('octo-a', 'assigned_before', { assignedAt: '2026-06-12T12:00:00Z' }))).toBe(
      'Assigned here 3d ago',
    )
    expect(hint(person('octo-a', 'assigned_before'))).toBe('Assigned here before')
    expect(hint(person('octo-m', 'works_here', { reviewed: 12, lastAt: '2026-06-14T12:00:00Z' }))).toBe(
      'Reviewed 12 here · 1d ago',
    )
    expect(hint(person('octo-m', 'works_here', { authored: 3 }))).toBe('Opened 3 here')
    expect(hint(person('octo-m', 'works_here', { committed: 1 }))).toBe('Committed to 1 pull request here')
    expect(hint(person('octo-o', 'org', { where: 'acme-org/web', lastAt: '2026-06-13T12:00:00Z' }))).toBe(
      'Works on acme-org/web · 2d ago',
    )
    expect(hint(person('octo-l', 'listed'))).toBe('Can be assigned here')
    expect(hint(person('octo-f', 'found'))).toBe('Found on GitHub')
  })
})

describe('assigneeGroups', () => {
  it('sections the people in the app’s order, without whom it is assigned to', () => {
    const people = [
      ...assigneeCandidates().people,
      person('octo-listed', 'listed'),
      person('octo-found', 'found'),
      person('octo-before', 'assigned_before'),
    ]

    expect(
      assigneeGroups(people, [{ login: 'Octo-Other', mine: false }], repo, '').map((g) => [
        g.section,
        g.people.map((c) => c.login),
      ]),
    ).toEqual([
      ['Suggested', ['octo-user', 'octo-commenter', 'octo-before']],
      [`Works on ${repo}`, ['octo-member']],
      ['In acme-org', ['octo-org']],
      [`Can be assigned in ${repo}`, ['octo-listed']],
      ['Found on GitHub', ['octo-found']],
    ])
  })

  it('keeps those who match, the ones that start with it first, and drops a section left empty', () => {
    const people = [person('bob', 'listed', { name: 'Annie Bob' }), person('ann-lee', 'listed'), person('zed', 'org')]

    expect(assigneeGroups(people, [], repo, ' AN ').map((g) => [g.section, g.people.map((c) => c.login)])).toEqual([
      [`Can be assigned in ${repo}`, ['bob', 'ann-lee']],
    ])
  })
})

describe('mergeFound', () => {
  it('adds whom a search found, each once, keeping why the list offered them', () => {
    const people = [person('octo-a', 'works_here')]

    expect(mergeFound(people, undefined)).toEqual(people)
    expect(mergeFound(people, [person('Octo-A', 'found'), person('octo-b', 'found')])).toEqual([
      person('octo-a', 'works_here'),
      person('octo-b', 'found'),
    ])
  })
})

describe('searchesGitHub', () => {
  it('searches only words, and only while the app’s list is cut short', () => {
    expect(searchesGitHub(assigneeCandidates(), '  ')).toBe(false)
    expect(searchesGitHub(assigneeCandidates(), 'octo')).toBe(true)
    expect(searchesGitHub(undefined, 'octo'), 'before the first read').toBe(true)
    expect(searchesGitHub(assigneeCandidates({ complete: true }), 'octo'), 'the app holds everyone').toBe(false)
  })
})

describe('assigneesNote', () => {
  it('says where the people come from', () => {
    expect(assigneesNote(undefined, repo, FIXED_NOW)).toBe(
      `People from the issues and pull requests you’ve viewed in ${repo}`,
    )
    expect(assigneesNote(assigneeCandidates({ readAt: '2026-06-13T12:00:00Z', complete: true }), repo, FIXED_NOW)).toBe(
      `Everyone who can be assigned in ${repo} · read 2d ago`,
    )
    expect(assigneesNote(assigneeCandidates({ readAt: '2026-06-13T12:00:00Z', total: 340 }), repo, FIXED_NOW)).toBe(
      `Some of the 340 who can be assigned in ${repo} · type to search the rest`,
    )
  })
})

describe('quickPicks', () => {
  it('offers the user first, then the author, then the next, never whom it is assigned to', () => {
    expect(quickPicks(assigneeCandidates(), []).map((p) => [p.login, p.you])).toEqual([
      ['octo-user', true],
      ['octo-other', false],
      ['octo-commenter', false],
    ])
    expect(quickPicks(assigneeCandidates(), [{ login: 'octo-user', mine: true }]).map((p) => p.login)).toEqual([
      'octo-other',
      'octo-commenter',
      'octo-member',
    ])
    expect(quickPicks(undefined, [])).toEqual([])
  })
})
