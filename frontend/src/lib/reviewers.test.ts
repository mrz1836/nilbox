import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { reviewerCandidates } from '../test/fixtures'
import {
  applyLabel,
  candidateHint,
  filtered,
  isTeamKey,
  learnedNote,
  matchRank,
  personFields,
  reviewersRequest,
  teamKey,
  teamName,
} from './reviewers'

const now = FIXED_NOW

// person is octo-member as the picker offers them, with overrides.
function person(overrides: Partial<ReturnType<typeof reviewerCandidates>['people'][number]> = {}) {
  const base = reviewerCandidates().people[1]

  if (base === undefined) {
    throw new Error('the fixture offers octo-member')
  }

  return { ...base, ...overrides }
}

describe('candidateHint', () => {
  it('says why GitHub suggests someone first', () => {
    expect(candidateHint(person({ suggestion: 'changed_files' }), now)).toBe('Changed these files')
    expect(candidateHint(person({ suggestion: 'commented' }), now)).toBe('Reviewed these files')
  })

  it('else says what they did here, and when', () => {
    expect(candidateHint(person(), now)).toBe('Reviewed 12 here · 3d ago')
    expect(candidateHint(person({ reviewed: 0 }), now)).toBe('Opened 4 here · 3d ago')
    expect(candidateHint(person({ reviewed: 0, authored: 0, committed: 1 }), now)).toBe(
      'Committed to 1 pull request here · 3d ago',
    )
    expect(candidateHint(person({ reviewed: 0, authored: 0, committed: 0, lastAt: '' }), now)).toBe('')
  })
})

describe('filtered', () => {
  const people = [
    { login: 'octo-zed', name: 'Ada Lovelace' },
    { login: 'ada-dev', name: 'Someone' },
    { login: 'octo-ada', name: '' },
  ]

  it('keeps those that match, those that start with it first, each kind in order', () => {
    expect(filtered(people, ' ADA ', personFields).map((p) => p.login)).toEqual(['octo-zed', 'ada-dev', 'octo-ada'])
    expect(filtered(people, 'dev', personFields).map((p) => p.login)).toEqual(['ada-dev'])
    expect(filtered(people, 'lace', personFields).map((p) => p.login)).toEqual(['octo-zed'])
    expect(filtered(people, '', personFields)).toEqual(people)
    expect(filtered(people, 'nobody', personFields)).toEqual([])
  })

  it('ranks a match at the start of a word as at the start', () => {
    expect(matchRank('love', ['Ada Lovelace'])).toBe(0)
    expect(matchRank('ace', ['Ada Lovelace'])).toBe(1)
    expect(matchRank('x', ['Ada'])).toBe(-1)
  })
})

describe('reviewersRequest', () => {
  it('sends people and teams apart', () => {
    expect(reviewersRequest(7, ['octo-a', teamKey('web')], ['octo-b', teamKey('ops')])).toEqual({
      itemId: 7,
      add: ['octo-a'],
      remove: ['octo-b'],
      addTeams: ['web'],
      removeTeams: ['ops'],
    })
    expect(isTeamKey('team:web')).toBe(true)
    expect(isTeamKey('octo-team')).toBe(false)
  })
})

describe('words', () => {
  it('names the button for what changed', () => {
    expect(applyLabel(['a', 'b'], ['c'])).toBe('Request 2 · Remove 1')
    expect(applyLabel([], ['c'])).toBe('Remove 1')
    expect(applyLabel([], [])).toBe('Apply')
  })

  it('names a team with its org', () => {
    expect(teamName('acme-org/api', 'web')).toBe('acme-org/web')
    expect(teamName('', 'web')).toBe('web')
  })

  it('says where the people come from', () => {
    expect(learnedNote('acme-org/api', 50, '2026-06-13T12:00:00Z', now)).toBe(
      'Contributors to acme-org/api · from 50 pull requests, 2d ago',
    )
    expect(learnedNote('acme-org/api', 0, '', now)).toBe(
      'Contributors to acme-org/api · from the pull requests you’ve viewed',
    )
  })
})
