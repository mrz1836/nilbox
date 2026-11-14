import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { brand } from '../generated/brand'
import { formatCount } from '../lib/format'
import type { BudgetGauge } from '../platform'
import { FIXED_NOW } from '../test/clock'
import { budgetGauge, reading, unreadBudget, writes } from '../test/fixtures'
import { RateGauge } from './RateGauge'

function renderGauge(gauge: BudgetGauge | null) {
  return render(<RateGauge gauge={gauge} now={FIXED_NOW} titleId="budget-title" />)
}

// row returns the queries scoped to one budget's list item.
function row(label: string) {
  const item = screen.getByText(label, { selector: 'span' }).closest('li')

  if (item === null) {
    throw new Error(`no row for ${label}`)
  }

  return within(item)
}

// fill returns a meter's colored bar.
function fill(label: string): HTMLElement {
  const bar = screen.getByRole('meter', { name: `${label} budget remaining` }).firstElementChild

  if (!(bar instanceof HTMLElement)) {
    throw new Error(`no bar in the ${label} meter`)
  }

  return bar
}

describe('RateGauge', () => {
  it('details every budget on one line each', () => {
    renderGauge(
      budgetGauge({
        graphql: reading(),
        core: reading({ remaining: 4990, used: 10, resetAt: '2026-06-15T13:00:00Z' }),
        search: reading({ limit: 30, remaining: 28, used: 2, resetAt: '2026-06-15T12:01:00Z' }),
      }),
    )

    expect(screen.getByRole('heading', { name: 'API budget' })).toHaveAttribute('id', 'budget-title')
    expect(screen.getByText('Healthy')).toBeInTheDocument()
    expect(screen.getByText('Read 1m ago')).toBeInTheDocument()

    const meter = screen.getByRole('meter', { name: 'GraphQL budget remaining' })
    expect(meter).toHaveAttribute('aria-valuemin', '0')
    expect(meter).toHaveAttribute('aria-valuemax', '5000')
    expect(meter).toHaveAttribute('aria-valuenow', '4980')
    expect(meter).toHaveAttribute('aria-valuetext', `${formatCount(4980)} of ${formatCount(5000)} left`)

    expect(row('GraphQL').getByText(`of ${formatCount(5000)}`, { exact: false })).toBeInTheDocument()
    expect(row('GraphQL').getByText('Resets in 42m')).toBeInTheDocument()
    expect(row('REST').getByText('Resets in 1h')).toBeInTheDocument()
    expect(row('Search').getByText('Resets in 1m')).toBeInTheDocument()
    expect(screen.getByText(/Shared with gh, your agents/)).toBeInTheDocument()
  })

  it('says it is live while the newest reading is fresh', () => {
    renderGauge(budgetGauge({ graphql: reading(), core: reading({ takenAt: FIXED_NOW.toISOString() }) }))

    expect(screen.getByText('Live · read just now')).toBeInTheDocument()
  })

  it('splits what each budget used between the app and everything else', () => {
    renderGauge(
      budgetGauge({
        graphql: reading({ used: 107, remaining: 4893, spent: 45 }),
        core: reading({ used: 23, remaining: 4977, spent: 3, complete: false }),
        search: reading({ limit: 30, remaining: 30, used: 0 }),
      }),
    )

    const graphql = row('GraphQL').getByText(`Used: ${brand.name} 45 · everything else 62`)
    expect(graphql).toHaveAttribute('title', expect.stringContaining('since it last refilled'))
    expect(row('REST').getByText(`Used: ${brand.name} 3 since it opened`)).toBeInTheDocument()
    expect(row('Search').queryByText(/^Used:/)).not.toBeInTheDocument()
  })

  it('colors each meter by how much is left, and heads with the lowest', () => {
    renderGauge(
      budgetGauge({
        graphql: reading({ remaining: 2000 }),
        core: reading({ remaining: 1000 }),
        search: reading({ limit: 30, remaining: 0, resetAt: '2026-06-15T12:00:30Z' }),
      }),
    )

    expect(fill('GraphQL')).toHaveClass('bg-warning')
    expect(fill('GraphQL')).toHaveStyle({ width: '40%' })
    expect(fill('REST')).toHaveClass('bg-caution')
    expect(fill('Search')).toHaveClass('bg-danger')
    expect(fill('Search')).toHaveStyle({ width: '0%' })
    expect(screen.getByText('Critical')).toHaveClass('text-danger')
    expect(row('Search').getByText('Resets within a minute')).toBeInTheDocument()
  })

  it('counts a budget that has refilled since it was read as full, with nothing used', () => {
    renderGauge(
      budgetGauge({ graphql: reading({ remaining: 12, used: 4988, spent: 40, resetAt: '2026-06-15T11:45:00Z' }) }),
    )

    expect(fill('GraphQL')).toHaveClass('bg-success')
    expect(fill('GraphQL')).toHaveStyle({ width: '100%' })
    expect(row('GraphQL').getByText('Refilled since it was read')).toBeInTheDocument()
    expect(row('GraphQL').queryByText(/^Used:/)).not.toBeInTheDocument()
  })

  it('works out what the inbox refreshes cost from the items they track', () => {
    renderGauge(
      budgetGauge({
        graphql: reading(),
        refresh: { known: true, at: '2026-06-15T11:59:00Z', points: 5, items: 31 },
      }),
    )

    expect(screen.getByText('5 points')).toBeInTheDocument()
    expect(screen.getByText('· 31 items', { exact: false })).toBeInTheDocument()
    expect(
      screen.getByText('12 refreshes an hour: about 60 GraphQL points (1% of the budget). More items cost more.'),
    ).toBeInTheDocument()
  })

  it('reads a small pace as under one percent', () => {
    renderGauge(budgetGauge({ graphql: reading(), refresh: { known: true, at: '', points: 1, items: 1 } }))

    expect(screen.getByText(/about 12 GraphQL points \(<1% of the budget\)/)).toBeInTheDocument()
  })

  it('says the refresh cost shows after the first refresh', () => {
    renderGauge(budgetGauge())

    expect(screen.getByText('Its cost shows after the first refresh.')).toBeInTheDocument()
  })

  it('shows budgets that were never read as unknown', () => {
    renderGauge(budgetGauge())

    const meter = screen.getByRole('meter', { name: 'REST budget remaining' })
    expect(meter).toHaveAttribute('aria-valuenow', '0')
    expect(meter).toHaveAttribute('aria-valuetext', 'not read yet')
    expect(screen.getAllByText('Not read yet')).toHaveLength(4)
    expect(row('REST').getByText('—')).toBeInTheDocument()
    expect(screen.getByText('Read with every GitHub request')).toBeInTheDocument()
  })

  it('renders before the budgets have loaded', () => {
    renderGauge(null)

    expect(screen.getAllByRole('meter')).toHaveLength(3)
    expect(screen.getByText('Read with every GitHub request')).toBeInTheDocument()
    expect(screen.queryByText('Inbox refresh')).not.toBeInTheDocument()
  })

  it('handles a zero limit without dividing by zero', () => {
    renderGauge(
      budgetGauge({
        graphql: reading({ limit: 0, remaining: 0 }),
        core: unreadBudget,
        refresh: { known: true, at: '', points: 3, items: 4 },
      }),
    )

    expect(fill('GraphQL')).toHaveStyle({ width: '0%' })
    expect(screen.getByText(/about 36 GraphQL points \(0% of the budget\)/)).toBeInTheDocument()
  })

  it('counts the changes the app made on GitHub, and how it paces them', () => {
    renderGauge(budgetGauge({ writes: writes({ lastHour: 3, reserve: 50 }) }))

    expect(screen.getByText('Changes on GitHub')).toBeInTheDocument()
    expect(screen.getByText('in the last hour').parentElement).toHaveTextContent('3 in the last hour')
    expect(
      screen.getByText(
        `Each spends 1 REST point. ${brand.name} makes at most 30 a minute and 200 an hour, and none while REST is down to its last 50.`,
      ),
    ).toBeInTheDocument()
    expect(screen.queryByText(/GitHub asked for a pause/)).not.toBeInTheDocument()
  })

  it('says when GitHub asked the app to pause its changes', () => {
    renderGauge(budgetGauge({ writes: writes({ heldUntil: '2026-06-15T12:05:00Z' }) }))

    expect(screen.getByText('GitHub asked for a pause: changes resume in 5m')).toHaveClass('text-warning')
  })

  it('counts the notifications the app cleared on GitHub, apart from the changes, and how it paces them', () => {
    renderGauge(
      budgetGauge({ chores: writes({ lastHour: 12, perHour: 300, perMinute: 50, reserve: 50 }), writes: writes() }),
    )

    expect(screen.getByText('Housekeeping on GitHub')).toBeInTheDocument()

    const count = screen.getByTitle('12 in the last hour, of 300')
    expect(count).toHaveTextContent(/^12 of 300$/)
    expect(
      screen.getByText(
        `Notifications marked done or read for you in the last hour. Each spends 1 REST point. ${brand.name} clears at most 50 a minute and 300 an hour, apart from the changes above, and none while REST is down to its last 50.`,
      ),
    ).toBeInTheDocument()
    // The edits count apart.
    expect(screen.getByText('in the last hour').parentElement).toHaveTextContent('0 in the last hour')
    expect(screen.queryByText(/housekeeping resumes/)).not.toBeInTheDocument()
  })

  it('says when GitHub asked the app to pause its housekeeping', () => {
    renderGauge(budgetGauge({ chores: writes({ perHour: 300, heldUntil: '2026-06-15T12:05:00Z' }) }))

    expect(screen.getByText('GitHub asked for a pause: housekeeping resumes in 5m')).toHaveClass('text-warning')
  })

  it('leaves the housekeeping out before the budgets have loaded', () => {
    renderGauge(null)

    expect(screen.queryByText('Housekeeping on GitHub')).not.toBeInTheDocument()
  })
})
