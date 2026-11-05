// API budget levels for the toolbar gauge and its details. GitHub's budgets refill in
// full at their reset time, so a reading whose reset has passed counts as full again: a
// budget that ran low an hour ago does not keep the gauge red.

import type { Rate, RateGauge } from '../platform'

// BudgetLevel grades the share of a budget left: healthy (green), moderate (yellow), low
// (orange), and critical (red). Unknown means it was never read.
export type BudgetLevel = 'unknown' | 'healthy' | 'moderate' | 'low' | 'critical'

// The lower bound of each level, in percent left. At or below lowAbove is critical.
const healthyAbove = 50
const moderateAbove = 25
const lowAbove = 10

export type BudgetKey = keyof RateGauge

// budgets lists the budgets in display order. The inbox spends GraphQL; gh and the
// agents spend REST and search, which share the same account.
export const budgets: readonly { key: BudgetKey; label: string }[] = [
  { key: 'graphql', label: 'GraphQL' },
  { key: 'core', label: 'REST' },
  { key: 'search', label: 'Search' },
]

// levelLabels name each level for people.
export const levelLabels: Readonly<Record<BudgetLevel, string>> = {
  unknown: 'Not read yet',
  healthy: 'Healthy',
  moderate: 'Moderate',
  low: 'Low',
  critical: 'Critical',
}

// levelText and levelFill are each level's text and fill color. They are spelled out in
// full so Tailwind finds them.
export const levelText: Readonly<Record<BudgetLevel, string>> = {
  unknown: 'text-muted',
  healthy: 'text-success',
  moderate: 'text-warning',
  low: 'text-caution',
  critical: 'text-danger',
}

export const levelFill: Readonly<Record<BudgetLevel, string>> = {
  unknown: 'bg-muted',
  healthy: 'bg-success',
  moderate: 'bg-warning',
  low: 'bg-caution',
  critical: 'bg-danger',
}

// Budget is one budget as of now.
export interface Budget {
  known: boolean
  limit: number
  // remaining is what is left now: the reading, or the full limit once it has reset.
  remaining: number
  // percent is remaining as a share of the limit, 0–100.
  percent: number
  level: BudgetLevel
  // refilled is true when the budget has reset since it was read.
  refilled: boolean
}

// levelFor grades a percentage left.
export function levelFor(percent: number): BudgetLevel {
  if (percent > healthyAbove) {
    return 'healthy'
  }

  if (percent > moderateAbove) {
    return 'moderate'
  }

  if (percent > lowAbove) {
    return 'low'
  }

  return 'critical'
}

// hasReset reports whether a reading's reset time is at or before now.
function hasReset(rate: Rate, now: Date): boolean {
  const reset = Date.parse(rate.resetAt)

  return !Number.isNaN(reset) && reset <= now.getTime()
}

// readBudget grades one reading as of now.
export function readBudget(rate: Rate, now: Date): Budget {
  if (!rate.known) {
    return { known: false, limit: 0, remaining: 0, percent: 0, level: 'unknown', refilled: false }
  }

  const refilled = rate.limit > 0 && hasReset(rate, now)
  const remaining = refilled ? rate.limit : Math.max(0, rate.remaining)
  const percent = rate.limit > 0 ? Math.min(100, (remaining / rate.limit) * 100) : 0

  return { known: true, limit: rate.limit, remaining, percent, level: levelFor(percent), refilled }
}

// BudgetSummary is the gauge's headline: the budget with the least left.
export interface BudgetSummary {
  level: BudgetLevel
  percent: number
  // lowest is the label of the budget with the least left ("" when none was read).
  lowest: string
}

// summarize finds the budget with the smallest share left, which sets the gauge.
export function summarize(gauge: RateGauge | null, now: Date): BudgetSummary {
  let summary: BudgetSummary = { level: 'unknown', percent: 0, lowest: '' }

  if (gauge === null) {
    return summary
  }

  for (const { key, label } of budgets) {
    const budget = readBudget(gauge[key], now)

    if (budget.known && (summary.lowest === '' || budget.percent < summary.percent)) {
      summary = { level: budget.level, percent: budget.percent, lowest: label }
    }
  }

  return summary
}

// summaryText reads the summary for the gauge's label and tooltip, e.g.
// "API budget: Healthy, 99% left".
export function summaryText(summary: BudgetSummary): string {
  if (summary.level === 'unknown') {
    return `API budget: ${levelLabels.unknown.toLowerCase()}`
  }

  const left = `${String(Math.floor(summary.percent))}% left`
  const which = summary.level === 'healthy' ? '' : ` (${summary.lowest})`

  return `API budget: ${levelLabels[summary.level]}, ${left}${which}`
}
