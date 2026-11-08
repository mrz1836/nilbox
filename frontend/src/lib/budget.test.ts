import { describe, expect, it } from 'vitest'

import { FIXED_NOW } from '../test/clock'
import { budgetGauge, reading, unreadBudget, writes } from '../test/fixtures'
import {
  levelFor,
  newestRead,
  paceOf,
  readBudget,
  shareOf,
  summarize,
  summaryText,
  writesHeld,
  type BudgetLevel,
} from './budget'

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
    expect(readBudget(reading({ remaining: 1000 }), FIXED_NOW)).toEqual({
      known: true,
      limit: 5000,
      remaining: 1000,
      percent: 20,
      level: 'low',
      refilled: false,
    })
  })

  it('counts a budget whose reset has passed as full again', () => {
    const budget = readBudget(reading({ remaining: 3, resetAt: '2026-06-15T11:30:00Z' }), FIXED_NOW)

    expect(budget).toMatchObject({ remaining: 5000, percent: 100, level: 'healthy', refilled: true })
  })

  it('treats a reset at exactly now as passed', () => {
    expect(readBudget(reading({ remaining: 0, resetAt: FIXED_NOW.toISOString() }), FIXED_NOW).refilled).toBe(true)
  })

  it('keeps a reading with no usable reset time as read', () => {
    expect(readBudget(reading({ remaining: 0, resetAt: '' }), FIXED_NOW)).toMatchObject({
      level: 'critical',
      refilled: false,
    })
  })

  it('is unknown before the first reading', () => {
    expect(readBudget(unreadBudget, FIXED_NOW)).toMatchObject({ known: false, level: 'unknown', percent: 0 })
  })

  it('never goes below zero or divides by a zero limit', () => {
    expect(readBudget(reading({ remaining: -5 }), FIXED_NOW)).toMatchObject({
      remaining: 0,
      percent: 0,
      level: 'critical',
    })
    expect(readBudget(reading({ limit: 0, remaining: 0 }), FIXED_NOW)).toMatchObject({ percent: 0, refilled: false })
  })
})

describe('summarize', () => {
  it('follows the budget with the least left', () => {
    const summary = summarize(
      budgetGauge({
        graphql: reading(),
        core: reading({ remaining: 900 }),
        search: reading({ limit: 30, remaining: 12 }),
      }),
      FIXED_NOW,
    )

    expect(summary).toEqual({ level: 'low', percent: 18, lowest: 'REST' })
    expect(summaryText(summary)).toBe('API budget: Low, 18% left (REST)')
  })

  it('skips budgets that were never read', () => {
    const summary = summarize(budgetGauge({ graphql: reading({ remaining: 2600 }) }), FIXED_NOW)

    expect(summary).toEqual({ level: 'healthy', percent: 52, lowest: 'GraphQL' })
    expect(summaryText(summary)).toBe('API budget: Healthy, 52% left')
  })

  it('is unknown before the inbox loads or anything was read', () => {
    const unread = { level: 'unknown', percent: 0, lowest: '' }

    expect(summarize(null, FIXED_NOW)).toEqual(unread)
    expect(summarize(budgetGauge(), FIXED_NOW)).toEqual(unread)
    expect(summaryText(summarize(null, FIXED_NOW))).toBe('API budget: not read yet')
  })
})

describe('shareOf', () => {
  it('splits what a budget used between the app and everything else', () => {
    expect(shareOf(reading({ used: 107, spent: 45 }), FIXED_NOW)).toEqual({ app: 45, others: 62, complete: true })
    expect(shareOf(reading({ used: 10, spent: 12, complete: false }), FIXED_NOW)).toEqual({
      app: 12,
      others: 0,
      complete: false,
    })
  })

  it('has nothing to split for a budget unread, refilled since, or untouched', () => {
    expect(shareOf(unreadBudget, FIXED_NOW)).toBeNull()
    expect(shareOf(reading({ used: 30, spent: 3, resetAt: '2026-06-15T11:00:00Z' }), FIXED_NOW)).toBeNull()
    expect(shareOf(reading({ used: 0, spent: 0 }), FIXED_NOW)).toBeNull()
  })
})

describe('paceOf', () => {
  it('works out an hour of refreshes from the last one', () => {
    const gauge = budgetGauge({ graphql: reading(), refresh: { known: true, at: '', points: 5, items: 31 } })

    expect(paceOf(gauge)).toEqual({ points: 5, items: 31, perHour: 60, percent: 1.2 })
  })

  it('has no pace before a refresh, and no share of an unread budget', () => {
    expect(paceOf(budgetGauge())).toBeNull()
    expect(paceOf(budgetGauge({ refresh: { known: true, at: '', points: 2, items: 3 } }))?.percent).toBe(0)
  })
})

describe('newestRead', () => {
  it('is the newest reading of any budget', () => {
    const gauge = budgetGauge({
      graphql: reading({ takenAt: '2026-06-15T11:58:00Z' }),
      core: reading({ takenAt: '2026-06-15T11:59:30Z' }),
    })

    expect(newestRead(gauge)).toBe('2026-06-15T11:59:30Z')
  })

  it('is empty before any reading', () => {
    expect(newestRead(null)).toBe('')
    expect(newestRead(budgetGauge())).toBe('')
  })
})

describe('writesHeld', () => {
  it('holds while GitHub’s pause runs', () => {
    expect(writesHeld(writes(), FIXED_NOW)).toBe(false)
    expect(writesHeld(writes({ heldUntil: '2026-06-15T12:01:00Z' }), FIXED_NOW)).toBe(true)
    expect(writesHeld(writes({ heldUntil: '2026-06-15T11:59:00Z' }), FIXED_NOW)).toBe(false)
    expect(writesHeld(writes({ heldUntil: 'not a time' }), FIXED_NOW)).toBe(false)
  })
})
