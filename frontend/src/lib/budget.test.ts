import { describe, expect, it } from 'vitest'

import { rate, unknownRate } from '../test/fixtures'
import { levelFor, readBudget, summarize, summaryText, type BudgetLevel } from './budget'

const now = new Date('2026-06-15T12:00:00Z')

describe('levelFor', () => {
  it.each<[number, BudgetLevel]>([
    [100, 'healthy'],
    [50.1, 'healthy'],
    [50, 'moderate'],
    [25.1, 'moderate'],
    [25, 'low'],
    [10.1, 'low'],
    [10, 'critical'],
    [0, 'critical'],
  ])('grades %d%% left as %s', (percent, level) => {
    expect(levelFor(percent)).toBe(level)
  })
})

describe('readBudget', () => {
  it('reads what is left as a share of the limit', () => {
    expect(readBudget(rate({ remaining: 1000 }), now)).toEqual({
      known: true,
      limit: 5000,
      remaining: 1000,
      percent: 20,
      level: 'low',
      refilled: false,
    })
  })

  it('counts a budget whose reset has passed as full again', () => {
    const budget = readBudget(rate({ remaining: 3, resetAt: '2026-06-15T11:30:00Z' }), now)

    expect(budget).toMatchObject({ remaining: 5000, percent: 100, level: 'healthy', refilled: true })
  })

  it('treats a reset at exactly now as passed', () => {
    expect(readBudget(rate({ remaining: 0, resetAt: now.toISOString() }), now).refilled).toBe(true)
  })

  it('keeps a reading with no usable reset time as read', () => {
    expect(readBudget(rate({ remaining: 0, resetAt: '' }), now)).toMatchObject({ level: 'critical', refilled: false })
  })

  it('is unknown before the first reading', () => {
    expect(readBudget(unknownRate, now)).toMatchObject({ known: false, level: 'unknown', percent: 0 })
  })

  it('never goes below zero or divides by a zero limit', () => {
    expect(readBudget(rate({ remaining: -5 }), now)).toMatchObject({ remaining: 0, percent: 0, level: 'critical' })
    expect(readBudget(rate({ limit: 0, remaining: 0 }), now)).toMatchObject({ percent: 0, refilled: false })
  })
})

describe('summarize', () => {
  it('follows the budget with the least left', () => {
    const summary = summarize(
      { graphql: rate(), core: rate({ remaining: 900 }), search: rate({ limit: 30, remaining: 12 }) },
      now,
    )

    expect(summary).toEqual({ level: 'low', percent: 18, lowest: 'REST' })
    expect(summaryText(summary)).toBe('API budget: Low, 18% left (REST)')
  })

  it('skips budgets that were never read', () => {
    const summary = summarize({ graphql: rate({ remaining: 2600 }), core: unknownRate, search: unknownRate }, now)

    expect(summary).toEqual({ level: 'healthy', percent: 52, lowest: 'GraphQL' })
    expect(summaryText(summary)).toBe('API budget: Healthy, 52% left')
  })

  it('is unknown before the inbox loads or anything was read', () => {
    const unread = { level: 'unknown', percent: 0, lowest: '' }

    expect(summarize(null, now)).toEqual(unread)
    expect(summarize({ graphql: unknownRate, core: unknownRate, search: unknownRate }, now)).toEqual(unread)
    expect(summaryText(summarize(null, now))).toBe('API budget: not read yet')
  })
})
