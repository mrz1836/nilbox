// API budget levels for the toolbar gauge and its details. GitHub's budgets refill in
// full at their reset time, so a reading whose reset has passed counts as full again: a
// budget that ran low an hour ago does not keep the gauge red.
//
// Every GitHub response the app gets says what is left of its budget, so the readings are
// as of the app's latest call; the gauge's panel also reads them from GitHub while open.
// Each reading says what the app itself spent since the budget last refilled, which tells
// the app's share from everything else signed in as the same account.

import type { BudgetGauge, BudgetReading, BudgetWrites } from '../platform'

// BudgetLevel grades the share of a budget left: healthy (green), moderate (yellow), low
// (orange), and critical (red). Unknown means it was never read.
export type BudgetLevel = 'unknown' | 'healthy' | 'moderate' | 'low' | 'critical'

// The lower bound of each level, in percent left. At or below lowAbove is critical.
const healthyAbove = 50
const moderateAbove = 25
const lowAbove = 10

export type BudgetKey = 'graphql' | 'core' | 'search'

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
function hasReset(rate: BudgetReading, now: Date): boolean {
  const reset = Date.parse(rate.resetAt)

  return !Number.isNaN(reset) && reset <= now.getTime()
}

// readBudget grades one reading as of now.
export function readBudget(rate: BudgetReading, now: Date): Budget {
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
export function summarize(gauge: BudgetGauge | null, now: Date): BudgetSummary {
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

// Share is who used a budget since it last refilled: the app, and everything else signed
// in as the same account (gh, coding agents).
export interface Share {
  app: number
  others: number
  // complete is false when the app opened after the budget last refilled, so others may
  // hold some of what the app spent before.
  complete: boolean
}

// shareOf splits what a budget used between the app and everything else, or returns null
// when there is nothing to split: the budget is unread, refilled since, or untouched.
export function shareOf(rate: BudgetReading, now: Date): Share | null {
  if (!rate.known || hasReset(rate, now) || (rate.used === 0 && rate.spent === 0)) {
    return null
  }

  return { app: rate.spent, others: Math.max(0, rate.used - rate.spent), complete: rate.complete }
}

// Pace is what the inbox's own refreshes spend of the GraphQL budget: what the last cost,
// for how many items, and what that comes to in an hour of refreshes.
export interface Pace {
  points: number
  items: number
  perHour: number
  // percent is perHour as a share of the GraphQL budget, 0 when its size is unknown.
  percent: number
}

// paceOf works out the refreshes' pace from the last refresh, or returns null before one.
// Each refresh reads every tracked item, so the pace follows the number of items.
export function paceOf(gauge: BudgetGauge): Pace | null {
  const { refresh, refreshesPerHour, graphql } = gauge

  if (!refresh.known) {
    return null
  }

  const perHour = refresh.points * refreshesPerHour

  return {
    points: refresh.points,
    items: refresh.items,
    perHour,
    percent: graphql.limit > 0 ? (perHour / graphql.limit) * 100 : 0,
  }
}

// newestRead is when the newest reading was taken, as RFC 3339, or '' before any.
export function newestRead(gauge: BudgetGauge | null): string {
  if (gauge === null) {
    return ''
  }

  return budgets
    .map(({ key }) => gauge[key])
    .filter((r) => r.known)
    .map((r) => r.takenAt)
    .reduce((newest, at) => (Date.parse(at) > Date.parse(newest || '0') ? at : newest), '')
}

// writesHeld reports whether GitHub's request to pause the app's changes still runs at
// now.
export function writesHeld(writes: BudgetWrites, now: Date): boolean {
  const until = Date.parse(writes.heldUntil)

  return !Number.isNaN(until) && until > now.getTime()
}
