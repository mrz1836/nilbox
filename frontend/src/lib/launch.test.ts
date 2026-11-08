import { describe, expect, it } from 'vitest'

import type { PromptUsage } from '../platform'
import {
  effortLabel,
  kindPlural,
  modelLabel,
  pinSummary,
  promptApplies,
  sourceLabel,
  usageCount,
  usagePlaces,
  usageSummary,
} from './launch'

const noUsage: PromptUsage = { defaults: [], orgs: [], repos: [], kinds: [] }

describe('labels', () => {
  it('names where a setting came from', () => {
    expect(sourceLabel('repo')).toBe('repo default')
    expect(sourceLabel('agent')).toBe('agent default')
    expect(sourceLabel('launch')).toBe('this launch')
  })

  it('names models and effort levels', () => {
    expect(modelLabel('opus')).toBe('Opus')
    expect(modelLabel('claude-opus-5-5')).toBe('claude-opus-5-5')
    expect(modelLabel('')).toBe('Agent default')
    expect(effortLabel('xhigh')).toBe('Extra high')
    expect(effortLabel('')).toBe('Agent default')
    expect(effortLabel('turbo')).toBe('turbo')
  })

  it('names kinds in the plural', () => {
    expect(kindPlural('pr')).toBe('pull requests')
    expect(kindPlural('issue')).toBe('issues')
  })
})

describe('promptApplies', () => {
  it('matches a prompt to the kinds it is for', () => {
    expect(promptApplies({ appliesTo: 'any' }, 'issue')).toBe(true)
    expect(promptApplies({ appliesTo: 'pr' }, 'pr')).toBe(true)
    expect(promptApplies({ appliesTo: 'pr' }, 'issue')).toBe(false)
  })
})

describe('usage', () => {
  it('summarizes where a prompt is a default', () => {
    expect(usageSummary(noUsage)).toBe('Not a default anywhere')
    expect(usageSummary({ ...noUsage, defaults: ['pr'], orgs: ['acme-org'], repos: ['a/b', 'a/c'] })).toBe(
      'App default for pull requests · 1 org · 2 repos',
    )
    expect(usageSummary({ ...noUsage, defaults: ['pr', 'issue'] })).toBe('App default for pull requests and issues')
  })

  it('counts and lists the places', () => {
    const usage: PromptUsage = { ...noUsage, defaults: ['issue'], orgs: ['acme-org'], repos: ['acme-org/api'] }

    expect(usageCount(usage)).toBe(3)
    expect(usagePlaces(usage)).toEqual(['App default for issues', 'acme-org (all repositories)', 'acme-org/api'])
  })
})

describe('pinSummary', () => {
  it('lists the settings a prompt sets', () => {
    expect(pinSummary({ model: '', effort: '', planMode: null, worktree: null, extraArgs: [] })).toEqual([])
    expect(
      pinSummary({
        model: 'sonnet',
        effort: 'low',
        planMode: true,
        worktree: false,
        extraArgs: ['--advisor', 'fable'],
      }),
    ).toEqual(['Sonnet', 'low effort', 'plan mode', 'no worktree', '2 flags'])
    expect(pinSummary({ model: '', effort: '', planMode: false, worktree: true, extraArgs: [] })).toEqual([
      'no plan mode',
      'worktree',
    ])
  })
})
