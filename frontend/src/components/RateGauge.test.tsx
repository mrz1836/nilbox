import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { formatCount } from '../lib/format'
import type { Rate, RateGauge as Gauge } from '../platform'
import { RateGauge } from './RateGauge'

const now = new Date('2026-06-15T12:00:00Z')

function rate(overrides: Partial<Rate> = {}): Rate {
  return {
    known: true,
    limit: 5000,
    remaining: 4980,
    used: 20,
    cost: 1,
    resetAt: '2026-06-15T12:42:00Z',
    takenAt: '2026-06-15T11:59:00Z',
    ...overrides,
  }
}

const unknown: Rate = { known: false, limit: 0, remaining: 0, used: 0, cost: 0, resetAt: '', takenAt: '' }

// term returns the description next to a label in the gauge's list.
function term(label: string): HTMLElement {
  const dt = screen.getByText(label, { selector: 'dt' })
  const dd = dt.nextElementSibling

  if (!(dd instanceof HTMLElement)) {
    throw new Error(`no description for ${label}`)
  }

  return dd
}

describe('RateGauge', () => {
  it('renders the rate gauge', () => {
    const gauge: Gauge = {
      graphql: rate(),
      core: rate({ remaining: 4990, cost: 0, resetAt: '2026-06-15T13:00:00Z' }),
      search: rate({ limit: 30, remaining: 28, cost: 0, resetAt: '2026-06-15T12:01:00Z' }),
    }

    render(<RateGauge gauge={gauge} now={now} />)

    const meter = screen.getByRole('meter', { name: 'GraphQL budget remaining' })
    expect(meter).toHaveAttribute('aria-valuemin', '0')
    expect(meter).toHaveAttribute('aria-valuemax', '5000')
    expect(meter).toHaveAttribute('aria-valuenow', '4980')
    expect(meter).toHaveAttribute('aria-valuetext', `${formatCount(4980)} of ${formatCount(5000)} left`)

    expect(term('GraphQL')).toHaveTextContent(`${formatCount(4980)} of ${formatCount(5000)} left · resets in 42m`)
    expect(term('Last query cost')).toHaveTextContent('1')
    expect(term('Core')).toHaveTextContent(`${formatCount(4990)} of ${formatCount(5000)} left · resets in 1h`)
    expect(term('Search')).toHaveTextContent('28 of 30 left · resets in 1m')
  })

  it('shows budgets that were never read as unknown', () => {
    render(<RateGauge gauge={{ graphql: unknown, core: unknown, search: unknown }} now={now} />)

    const meter = screen.getByRole('meter', { name: 'GraphQL budget remaining' })
    expect(meter).toHaveAttribute('aria-valuenow', '0')
    expect(meter).toHaveAttribute('aria-valuetext', 'not read yet')
    expect(term('Last query cost')).toHaveTextContent('—')
    expect(within(term('Core')).queryByText(/resets/)).not.toBeInTheDocument()
  })

  it('turns the meter red when the budget runs low', () => {
    const { container } = render(
      <RateGauge gauge={{ graphql: rate({ remaining: 100 }), core: unknown, search: unknown }} now={now} />,
    )

    expect(container.querySelector('.bg-danger')).toHaveStyle({ width: '2%' })
  })

  it('handles a zero limit without dividing by zero', () => {
    const { container } = render(
      <RateGauge gauge={{ graphql: rate({ limit: 0, remaining: 0 }), core: unknown, search: unknown }} now={now} />,
    )

    expect(container.querySelector('[role="meter"] > div')).toHaveStyle({ width: '0%' })
  })
})
