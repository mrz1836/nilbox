import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { account, rate, unknownRate } from '../test/fixtures'
import { AppHeader, type AppHeaderProps } from './AppHeader'

vi.mock('../platform', () => ({ openInBrowser: vi.fn() }))

function renderHeader(overrides: Partial<AppHeaderProps> = {}) {
  const props: AppHeaderProps = {
    view: 'inbox',
    onViewChange: vi.fn(),
    account: account(),
    gauge: { graphql: rate(), core: unknownRate, search: unknownRate },
    now: new Date('2026-06-15T12:00:00Z'),
    onOpenSettings: vi.fn(),
    ...overrides,
  }

  render(<AppHeader {...props} />)

  return props
}

describe('AppHeader', () => {
  it('is one toolbar: the product name, the views, the budget gauge, and the account', () => {
    renderHeader()

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()
    expect(screen.queryByText(brand.tagline)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^API budget: Healthy/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Account: octo-user' })).toBeInTheDocument()
  })

  it('drags the window from the bar but not from its controls', () => {
    renderHeader()

    expect(screen.getByRole('banner')).toHaveClass('[--wails-draggable:drag]')
    expect(screen.getByRole('navigation', { name: 'Views' })).toHaveClass('[--wails-draggable:no-drag]')
    expect(
      screen.getByRole('button', { name: /^Account/ }).closest('.\\[--wails-draggable\\:no-drag\\]'),
    ).not.toBeNull()
  })

  it('marks the active view and switches views on click', async () => {
    const props = renderHeader({ view: 'inbox' })

    expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Worktrees' })).toHaveAttribute('aria-pressed', 'false')

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(props.onViewChange).toHaveBeenCalledWith('worktrees')
  })

  it('marks no tab on the settings page', () => {
    renderHeader({ view: 'settings' })

    expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Worktrees' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('opens the settings from the account menu', async () => {
    const props = renderHeader()

    await userEvent.click(screen.getByRole('button', { name: 'Account: octo-user' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Settings/ }))

    expect(props.onOpenSettings).toHaveBeenCalledOnce()
  })

  it('renders before the inbox has loaded', () => {
    renderHeader({ account: null, gauge: null })

    expect(screen.getByRole('button', { name: 'API budget: not read yet' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Account' })).toBeInTheDocument()
  })
})
