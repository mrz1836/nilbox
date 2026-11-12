import { describe, expect, it } from 'vitest'

import { orgOf, ownRule, parseTarget, ruleFor, ruleRows, targetKey, withRepoRule } from './repoRules'

describe('ruleFor', () => {
  const rules = { pinned: ['acme-org/example-repo', 'solo'], muted: ['Acme-Org', 'solo/quiet'] }

  it.each([
    ['acme-org/example-repo', 'pinned', 'acme-org/example-repo'],
    ['ACME-ORG/Example-Repo', 'pinned', 'acme-org/example-repo'],
    ['acme-org/other', 'muted', 'Acme-Org'],
    ['solo/quiet', 'muted', 'solo/quiet'],
    ['solo/loud', 'pinned', 'solo'],
    ['someone/else', '', ''],
  ])('finds the rule of %s, a repository’s own before its org’s', (repo, rule, target) => {
    expect(ruleFor(rules, repo)).toEqual({ rule, target })
  })

  it('reads a target’s own rule alone', () => {
    expect(ownRule(rules, 'acme-org')).toBe('muted')
    expect(ownRule(rules, 'acme-org/other')).toBe('')
    expect(ownRule(rules, 'SOLO')).toBe('pinned')
  })
})

describe('withRepoRule', () => {
  it('keeps a target in one list at most, sorted as the backend stores it', () => {
    const empty = { pinned: [], muted: [] }

    const muted = withRepoRule(empty, 'zeta', 'muted')
    const both = withRepoRule(withRepoRule(muted, 'alpha/x', 'muted'), 'Zeta', 'pinned')

    expect(both).toEqual({ pinned: ['Zeta'], muted: ['alpha/x'] })
    expect(withRepoRule(both, 'zeta', '')).toEqual({ pinned: [], muted: ['alpha/x'] })
    expect(withRepoRule({ pinned: [], muted: ['zeta/b', 'zeta-x'] }, 'zeta', 'muted').muted).toEqual([
      'zeta-x',
      'zeta',
      'zeta/b',
    ])
  })
})

describe('ruleRows', () => {
  it('lists each org first, then its repositories', () => {
    const rows = ruleRows({ pinned: ['acme-org/web', 'beta'], muted: ['acme-org', 'beta/x', 'zeta/y'] })

    expect(rows.map((r) => `${r.target} ${r.rule}`)).toEqual([
      'acme-org muted',
      'acme-org/web pinned',
      'beta pinned',
      'beta/x muted',
      'zeta/y muted',
    ])
    expect(rows[4]?.org).toBe('zeta')
  })
})

describe('parseTarget', () => {
  it.each([
    ['acme-org', 'acme-org'],
    [' Acme-Org/ ', 'Acme-Org'],
    ['acme-org/example.repo', 'acme-org/example.repo'],
    ['https://github.com/acme-org/example-repo/', 'acme-org/example-repo'],
    ['github.com/acme-org', null],
    ['', null],
    ['-acme', null],
    ['acme/..', null],
    ['acme/b/c', null],
    ['acme org', null],
    ['acme/-x', null],
  ])('reads %j as %j', (text, want) => {
    expect(parseTarget(text)).toBe(want)
  })
})

describe('names', () => {
  it('folds a target and finds a repository’s org', () => {
    expect(targetKey('Acme-Org/Web')).toBe('acme-org/web')
    expect(orgOf('acme-org/web')).toBe('acme-org')
  })
})
