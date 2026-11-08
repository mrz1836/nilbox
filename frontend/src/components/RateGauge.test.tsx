import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { formatCount } from '../lib/format'
import type { RateGauge as Gauge } from '../platform'
import { FIXED_NOW } from '../test/clock'
import { rate, unknownRate } from '../test/fixtures'
import { RateGauge } from './RateGauge'

const unread: Gauge = { graphql: unknownRate, core: unknownRate, search: unknownRate }

function renderGauge(gauge: Gauge | null) {
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
    renderGauge({
      graphql: rate(),
      core: rate({ remaining: 4990, cost: 0, resetAt: '2026-06-15T13:00:00Z' }),
      search: rate({ limit: 30, remaining: 28, cost: 0, resetAt: '2026-06-15T12:01:00Z' }),
    })

    expect(screen.getByRole('heading', { name: 'API budget' })).toHaveAttribute('id', 'budget-title')
    expect(screen.getByText('Healthy')).toBeInTheDocument()
    expect(screen.getByText('Read 1m ago')).toBeInTheDocument()

    const meter = screen.getByRole('meter', { name: 'GraphQL budget remaining' })
    expect(meter).toHaveAttribute('aria-valuemin', '0')
    expect(meter).toHaveAttribute('aria-valuemax', '5000')
    expect(meter).toHaveAttribute('aria-valuenow', '4980')
    expect(meter).toHaveAttribute('aria-valuetext', `${formatCount(4980)} of ${formatCount(5000)} left`)

    expect(row('GraphQL').getByText(`of ${formatCount(5000)}`, { exact: false })).toBeInTheDocument()
    expect(row('GraphQL').getByText('Resets in 42m · last refresh cost 1 point')).toBeInTheDocument()
    expect(row('REST').getByText('Resets in 1h')).toBeInTheDocument()
    expect(row('Search').getByText('Resets in 1m')).toBeInTheDocument()
    expect(screen.getByText(/Shared with gh, your agents/)).toBeInTheDocument()
  })

  it('colors each meter by how much is left, and heads with the lowest', () => {
    renderGauge({
      graphql: rate({ remaining: 2000 }),
      core: rate({ remaining: 1000 }),
      search: rate({ limit: 30, remaining: 0, resetAt: '2026-06-15T12:00:30Z' }),
    })

    expect(fill('GraphQL')).toHaveClass('bg-warning')
    expect(fill('GraphQL')).toHaveStyle({ width: '40%' })
    expect(fill('REST')).toHaveClass('bg-caution')
    expect(fill('Search')).toHaveClass('bg-danger')
    expect(fill('Search')).toHaveStyle({ width: '0%' })
    expect(screen.getByText('Critical')).toHaveClass('text-danger')
    expect(row('Search').getByText('Resets within a minute')).toBeInTheDocument()
  })

  it('counts a budget that has reset since the refresh as full', () => {
    renderGauge({
      graphql: rate({ remaining: 12, resetAt: '2026-06-15T11:45:00Z', cost: 2 }),
      core: unknownRate,
      search: unknownRate,
    })

    expect(fill('GraphQL')).toHaveClass('bg-success')
    expect(fill('GraphQL')).toHaveStyle({ width: '100%' })
    expect(row('GraphQL').getByText('Refilled since the last refresh · last refresh cost 2 points')).toBeInTheDocument()
  })

  it('shows budgets that were never read as unknown', () => {
    renderGauge(unread)

    const meter = screen.getByRole('meter', { name: 'GraphQL budget remaining' })
    expect(meter).toHaveAttribute('aria-valuenow', '0')
    expect(meter).toHaveAttribute('aria-valuetext', 'not read yet')
    expect(screen.getAllByText('Not read yet')).toHaveLength(4)
    expect(row('REST').getByText('—')).toBeInTheDocument()
    expect(screen.getByText('Read on every inbox refresh')).toBeInTheDocument()
  })

  it('renders before the inbox has loaded', () => {
    renderGauge(null)

    expect(screen.getAllByRole('meter')).toHaveLength(3)
    expect(screen.getByText('Read on every inbox refresh')).toBeInTheDocument()
  })

  it('handles a zero limit without dividing by zero', () => {
    renderGauge({ graphql: rate({ limit: 0, remaining: 0 }), core: unknownRate, search: unknownRate })

    expect(fill('GraphQL')).toHaveStyle({ width: '0%' })
  })
})
