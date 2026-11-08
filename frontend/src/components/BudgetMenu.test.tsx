import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { RateGauge as Gauge } from '../platform'
import { at } from '../test/clock'
import { rate, unknownRate } from '../test/fixtures'
import { BudgetMenu } from './BudgetMenu'

const now = new Date('2026-06-15T12:00:00Z')

const healthy: Gauge = { graphql: rate(), core: rate({ remaining: 4990 }), search: rate({ limit: 30, remaining: 30 }) }

function renderMenu(gauge: Gauge | null) {
  return render(
    <>
      <BudgetMenu gauge={gauge} />
      <button type="button">Elsewhere</button>
    </>,
    { wrapper: at(now) },
  )
}

// needle returns the dial's needle end point, or null when it has none.
function needle(button: HTMLElement): [number, number] | null {
  const line = button.querySelector('[data-part="needle"]')

  return line === null ? null : [Number(line.getAttribute('x2')), Number(line.getAttribute('y2'))]
}

describe('BudgetMenu', () => {
  it('signals a healthy budget in green with the needle near full', () => {
    renderMenu(healthy)

    const gauge = screen.getByRole('button', { name: 'API budget: Healthy, 99% left' })
    expect(gauge).toHaveClass('text-success')
    expect(gauge).toHaveAttribute('title', 'API budget: Healthy, 99% left')
    expect(gauge).toHaveAttribute('aria-haspopup', 'dialog')
    expect(gauge).toHaveAttribute('aria-expanded', 'false')

    const [x, y] = needle(gauge) ?? [0, 0]
    expect(x).toBeCloseTo(17, 0)
    expect(y).toBeCloseTo(15, 0)
  })

  it.each([
    ['moderate', 2000, 'text-warning', 'API budget: Moderate, 40% left (REST)'],
    ['low', 1000, 'text-caution', 'API budget: Low, 20% left (REST)'],
    ['critical', 0, 'text-danger', 'API budget: Critical, 0% left (REST)'],
  ])('signals a %s budget', (_level, remaining, color, label) => {
    renderMenu({ ...healthy, core: rate({ remaining }) })

    expect(screen.getByRole('button', { name: label })).toHaveClass(color)
  })

  it('points the needle at empty and drops the fill when nothing is left', () => {
    renderMenu({ ...healthy, graphql: rate({ remaining: 0 }) })

    const gauge = screen.getByRole('button', { name: /Critical/ })
    expect(needle(gauge)).toEqual([7, 15])
    expect(gauge.querySelector('[data-part="fill"]')).toBeNull()
  })

  it('shows an empty dial before anything was read', () => {
    renderMenu(null)

    const gauge = screen.getByRole('button', { name: 'API budget: not read yet' })
    expect(gauge).toHaveClass('text-muted')
    expect(needle(gauge)).toBeNull()
  })

  it('opens the details, and closes on Escape with focus back on the gauge', async () => {
    renderMenu({ ...healthy, core: unknownRate })
    const gauge = screen.getByRole('button', { name: /API budget/ })

    await userEvent.click(gauge)

    const details = screen.getByRole('dialog', { name: 'API budget' })
    expect(gauge).toHaveAttribute('aria-expanded', 'true')
    expect(gauge).toHaveAttribute('aria-controls', details.id)
    expect(details).toHaveFocus()
    expect(screen.getAllByRole('meter')).toHaveLength(3)

    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(gauge).toHaveFocus()
  })

  it('closes on a click outside or on the gauge again, but not on a click inside', async () => {
    renderMenu(healthy)
    const gauge = screen.getByRole('button', { name: /API budget/ })

    await userEvent.click(gauge)
    await userEvent.click(screen.getByText('GraphQL'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Elsewhere' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await userEvent.click(gauge)
    await userEvent.click(gauge)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('closes when focus moves elsewhere', async () => {
    renderMenu(healthy)

    await userEvent.click(screen.getByRole('button', { name: /API budget/ }))
    screen.getByRole('button', { name: 'Elsewhere' }).focus()

    expect(await screen.findByRole('button', { name: /API budget/ })).toHaveAttribute('aria-expanded', 'false')
  })
})
