import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { at } from '../test/clock'
import { account, budgetGauge, notification, reading } from '../test/fixtures'
import { AppHeader, type AppHeaderProps } from './AppHeader'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function renderHeader(overrides: Partial<AppHeaderProps> = {}) {
  const props: AppHeaderProps = {
    view: 'inbox',
    onViewChange: vi.fn(),
    account: account(),
    gauge: budgetGauge({ graphql: reading() }),
    onOpenSettings: vi.fn(),
    ...overrides,
  }

  render(<AppHeader {...props} />, { wrapper: at() })

  return props
}

describe('AppHeader', () => {
  it('shows no notification bell without a history', () => {
    renderHeader()
    expect(screen.queryByRole('button', { name: /^Notifications/ })).not.toBeInTheDocument()
  })

  it('opens a notification and the notification settings from the bell', async () => {
    const entry = notification()
    const { onOpenNotification, onNotificationSettings } = renderHeader({
      notifications: {
        log: { items: [entry], unread: 1 },
        error: '',
        markRead: vi.fn(),
        markError: '',
        clear: vi.fn(),
      },
      onOpenNotification: vi.fn(),
      onNotificationSettings: vi.fn(),
    })

    await userEvent.click(screen.getByRole('button', { name: 'Notifications, 1 unread' }))
    await userEvent.click(screen.getByRole('button', { name: /Add the widget cache/ }))
    expect(onOpenNotification).toHaveBeenCalledWith(entry)

    await userEvent.click(screen.getByRole('button', { name: /^Notifications/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Notification settings' }))
    expect(onNotificationSettings).toHaveBeenCalledOnce()
  })

  it('opens the general settings from the bell when given no notification settings', async () => {
    const { onOpenSettings } = renderHeader({
      notifications: { log: { items: [], unread: 0 }, error: '', markRead: vi.fn(), markError: '', clear: vi.fn() },
    })

    await userEvent.click(screen.getByRole('button', { name: 'Notifications' }))
    await userEvent.click(screen.getByRole('button', { name: 'Notification settings' }))
    expect(onOpenSettings).toHaveBeenCalledOnce()
  })

  it('is one toolbar: the product name, the views, the budget gauge, and the account', () => {
    renderHeader()

    expect(screen.getByRole('heading', { level: 1, name: brand.name })).toBeInTheDocument()
    expect(screen.queryByText(brand.tagline)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /^API budget: Healthy/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Account: octo-user' })).toBeInTheDocument()
  })

  it('leads with the logo: the mark beside the wordmark, named by the product name alone', () => {
    renderHeader()

    const heading = screen.getByRole('heading', { level: 1 })

    expect(heading).toHaveAccessibleName(brand.name)
    expect(heading.querySelector('svg[aria-hidden="true"] path')).toHaveAttribute('d', brand.mark.path)
    expect(screen.getByText(brand.name)).toHaveClass('lowercase')
  })

  it('drags the window from the bar but not from its controls', () => {
    renderHeader()

    expect(screen.getByRole('banner')).toHaveClass('[--wails-draggable:drag]')
    expect(screen.getByRole('navigation', { name: 'Views' })).toHaveClass('[--wails-draggable:no-drag]')
    expect(
      screen.getByRole('button', { name: /^Account/ }).closest('.\\[--wails-draggable\\:no-drag\\]'),
    ).not.toBeNull()
  })

  it('counts the items in the inbox and the linked worktrees, but not the prompts', () => {
    renderHeader({ inboxCount: 1234, worktreeCount: 5 })

    const inbox = screen.getByRole('button', { name: 'Inbox' })
    expect(inbox).toHaveTextContent('Inbox1,234')
    expect(inbox).toHaveAccessibleDescription('1,234 items in the inbox')

    const worktrees = screen.getByRole('button', { name: 'Worktrees' })
    expect(worktrees).toHaveTextContent('Worktrees5')
    expect(worktrees).toHaveAccessibleDescription('5 linked worktrees')
    expect(screen.queryByTestId('worktree-attention')).not.toBeInTheDocument()

    const prompts = screen.getByRole('button', { name: 'Prompts' })
    expect(prompts).toHaveTextContent(/^Prompts$/)
    expect(prompts).not.toHaveAttribute('title')
  })

  it('marks the worktree count when some are done on GitHub but still on disk', () => {
    renderHeader({ inboxCount: 1, worktreeCount: 4, worktreeAttention: 2 })

    const worktrees = screen.getByRole('button', { name: 'Worktrees' })
    expect(worktrees).toHaveTextContent('Worktrees4')
    expect(worktrees).toHaveAccessibleDescription('4 linked worktrees, 2 done on GitHub but still on disk')
    expect(screen.getByTestId('worktree-attention')).toHaveAttribute('aria-hidden', 'true')
    expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAccessibleDescription('1 item in the inbox')
  })

  it('hides the Scripts tab while no integration is on (D2)', () => {
    renderHeader({ scripts: null })

    expect(screen.queryByRole('button', { name: 'Scripts' })).not.toBeInTheDocument()
  })

  it('shows the Scripts tab after Prompts while an integration is on', () => {
    renderHeader({ scripts: { running: 0, failed: false, blocked: false } })

    const views = within(screen.getByRole('navigation', { name: 'Views' })).getAllByRole('button')
    const scripts = screen.getByRole('button', { name: 'Scripts' })

    expect(views.map((b) => b.getAttribute('aria-label'))).toEqual(['Inbox', 'Worktrees', 'Prompts', 'Scripts'])
    expect(scripts).toHaveTextContent(/^Scripts$/)
    expect(scripts).not.toHaveAttribute('title')
  })

  it('badges Scripts with the runs going, a red dot after a failed run, and an amber one for a tool that can’t be used', () => {
    renderHeader({ view: 'scripts', scripts: { running: 2, failed: true, blocked: true } })

    const scripts = screen.getByRole('button', { name: 'Scripts' })

    expect(scripts).toHaveAttribute('aria-pressed', 'true')
    expect(scripts).toHaveTextContent('Scripts2')
    expect(scripts).toHaveAccessibleDescription('2 scripts running, a run failed, a tool that’s on can’t be used')
    expect(screen.getByTestId('scripts-failed')).toHaveClass('bg-danger')
    expect(screen.getByTestId('scripts-attention')).toHaveClass('bg-warning')
  })

  it('shows a dot alone on Scripts while nothing runs', () => {
    renderHeader({ scripts: { running: 0, failed: true, blocked: false } })

    expect(screen.getByTestId('scripts-count')).toHaveTextContent(/^$/)
    expect(screen.getByRole('button', { name: 'Scripts' })).toHaveAccessibleDescription('A run failed')
    expect(screen.queryByTestId('scripts-attention')).not.toBeInTheDocument()
  })

  it('shows no count while there is nothing to count', () => {
    renderHeader()

    expect(screen.queryByTestId('inbox-count')).not.toBeInTheDocument()
    expect(screen.queryByTestId('worktrees-count')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Inbox' })).not.toHaveAttribute('title')
    expect(screen.getByRole('button', { name: 'Worktrees' })).not.toHaveAttribute('title')
  })

  it('marks the active view and switches views on click', async () => {
    const { onViewChange } = renderHeader({ view: 'inbox' })

    expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Worktrees' })).toHaveAttribute('aria-pressed', 'false')

    await userEvent.click(screen.getByRole('button', { name: 'Worktrees' }))

    expect(onViewChange).toHaveBeenCalledWith('worktrees')
  })

  it('marks no tab on the settings page', () => {
    renderHeader({ view: 'settings' })

    expect(screen.getByRole('button', { name: 'Inbox' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'Worktrees' })).toHaveAttribute('aria-pressed', 'false')
  })

  it('opens the settings from the account menu', async () => {
    const { onOpenSettings } = renderHeader()

    await userEvent.click(screen.getByRole('button', { name: 'Account: octo-user' }))
    await userEvent.click(screen.getByRole('menuitem', { name: /Settings/ }))

    expect(onOpenSettings).toHaveBeenCalledOnce()
  })

  it('renders before the inbox has loaded', () => {
    renderHeader({ account: null, gauge: null })

    expect(screen.getByRole('button', { name: 'API budget: not read yet' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Account' })).toBeInTheDocument()
  })
})
