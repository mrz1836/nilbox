import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { at } from '../test/clock'
import { account, budgetGauge, githubThread, githubThreads, notification, reading, settings } from '../test/fixtures'
import type { GitHubThreadsState } from '../lib/useGitHubThreads'
import type { NotificationsState } from '../lib/useNotifications'
import type { AppNotification, GitHubThreadsSnapshot } from '../platform'
import { AppHeader, type AppHeaderProps } from './AppHeader'

// threadsState is the GitHub notifications the button shows, with spies for what it does.
function threadsState(snapshot: GitHubThreadsSnapshot, checkNow = vi.fn()): GitHubThreadsState {
  const answer = () => Promise.resolve({ ok: true as const, data: githubThreads() })

  return {
    snapshot,
    error: '',
    reload: vi.fn(),
    checkNow: checkNow.mockImplementation(answer),
    checking: false,
    checkError: '',
    markDone: vi.fn(answer),
    markRead: vi.fn(answer),
    finishing: new Set(),
    reading: new Set(),
    markError: '',
    dismissMarkError: vi.fn(),
    list: null,
    listError: '',
    keep: vi.fn(answer),
  }
}

// historyOf is the notification history the bell shows, holding items, with spies for
// what it does.
function historyOf(items: AppNotification[] = []): NotificationsState {
  return {
    log: { items, unread: items.filter((n) => !n.read).length },
    error: '',
    markRead: vi.fn(),
    markUnread: vi.fn(),
    markAllRead: vi.fn(),
    markItemRead: vi.fn(),
    markError: '',
    clear: vi.fn(),
  }
}

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
    expect(screen.queryByRole('button', { name: /unread notifications$/ })).not.toBeInTheDocument()
  })

  it('opens a notification and the notification settings from the bell', async () => {
    const entry = notification()
    const { onOpenNotification, onNotificationSettings } = renderHeader({
      notifications: historyOf([entry]),
      onOpenNotification: vi.fn(),
      onNotificationSettings: vi.fn(),
    })

    await userEvent.click(screen.getByRole('button', { name: '1 unread notifications' }))
    await userEvent.click(screen.getByRole('button', { name: /^(?!Done: ).*Add the widget cache/ }))
    expect(onOpenNotification).toHaveBeenCalledWith(entry)

    await userEvent.click(screen.getByRole('button', { name: /unread notifications$/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Notification settings' }))
    expect(onNotificationSettings).toHaveBeenCalledOnce()
  })

  it('opens the Notifications page on each quick view’s tab from its View all', async () => {
    const { onViewAll } = renderHeader({
      notifications: historyOf([notification()]),
      githubThreads: threadsState(githubThreads({ status: { count: 1, unread: 1 }, unread: [githubThread()] })),
      settings: settings(),
      onViewAll: vi.fn(),
    })

    await userEvent.click(screen.getByRole('button', { name: '1 unread notifications' }))
    await userEvent.click(screen.getByRole('button', { name: 'View all' }))
    expect(onViewAll).toHaveBeenLastCalledWith('app')

    await userEvent.click(screen.getByRole('button', { name: /^GitHub notifications/ }))
    await userEvent.click(screen.getByRole('button', { name: 'View all' }))
    expect(onViewAll).toHaveBeenLastCalledWith('github')
  })

  it('offers no View all with nowhere to view them', async () => {
    renderHeader({ notifications: historyOf([notification({ read: true })]) })

    await userEvent.click(screen.getByRole('button', { name: 'No unread notifications' }))
    expect(screen.queryByRole('button', { name: 'View all' })).not.toBeInTheDocument()
  })

  it('counts the unread notifications, the app’s own and GitHub’s, on the Notifications tab', () => {
    renderHeader({
      notifications: historyOf([notification({ id: 1 }), notification({ id: 2 }), notification({ id: 3, read: true })]),
      githubThreads: threadsState(githubThreads({ status: { count: 30, unread: 12 } })),
      settings: settings(),
    })

    const tab = screen.getByRole('button', { name: 'Notifications' })

    expect(tab).toHaveTextContent('Notifications14')
    expect(tab).toHaveAccessibleDescription(`2 unread in ${brand.name} · 12 unread on GitHub`)
    expect(screen.getByTestId('notifications-count')).toHaveTextContent('14')
  })

  it('counts GitHub’s unread only while the app reads them', () => {
    const { rerender } = render(
      <AppHeader
        view="inbox"
        onViewChange={vi.fn()}
        account={account()}
        gauge={null}
        onOpenSettings={vi.fn()}
        githubThreads={threadsState(githubThreads({ status: { count: 30, unread: 12 } }))}
        settings={settings({ githubNotifications: { ...settings().githubNotifications, read: false } })}
      />,
      { wrapper: at() },
    )

    expect(screen.queryByTestId('notifications-count')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Notifications' })).not.toHaveAttribute('title')

    rerender(
      <AppHeader
        view="inbox"
        onViewChange={vi.fn()}
        account={account()}
        gauge={null}
        onOpenSettings={vi.fn()}
        notifications={historyOf([notification()])}
        githubThreads={threadsState(githubThreads({ status: { reading: false, count: 30, unread: 12 } }))}
        settings={null}
      />,
    )

    expect(screen.getByTestId('notifications-count')).toHaveTextContent('1')
    expect(screen.getByRole('button', { name: 'Notifications' })).toHaveAccessibleDescription(
      `1 unread in ${brand.name}`,
    )

    rerender(
      <AppHeader
        view="inbox"
        onViewChange={vi.fn()}
        account={account()}
        gauge={null}
        onOpenSettings={vi.fn()}
        githubThreads={threadsState(githubThreads({ status: { count: 30, unread: 12 } }))}
        settings={null}
      />,
    )

    expect(screen.getByRole('button', { name: 'Notifications' })).toHaveAccessibleDescription('12 unread on GitHub')
  })

  it('shows no GitHub notifications button without them', () => {
    renderHeader()
    expect(screen.queryByRole('button', { name: /^GitHub notifications/ })).not.toBeInTheDocument()
  })

  it('puts the GitHub notifications button between the bell and the budget gauge, and opens from it', async () => {
    const threads = threadsState(githubThreads({ status: { count: 3, unread: 1 }, unread: [githubThread()] }))
    const { onGitHubThreadsSettings } = renderHeader({
      notifications: historyOf(),
      githubThreads: threads,
      settings: settings({ githubNotifications: { ...settings().githubNotifications, auto: true } }),
      onGitHubThreadsSettings: vi.fn(),
    })

    const names = within(screen.getByRole('banner'))
      .getAllByRole('button')
      .map((b) => b.getAttribute('aria-label') ?? '')
    const bell = names.indexOf('No unread notifications')
    const github = names.indexOf('GitHub notifications, 1 unread, 3 in your inbox')

    expect(bell).toBeGreaterThan(-1)
    expect(github).toBe(bell + 1)
    expect(names[github + 1]).toMatch(/budget/i)

    await userEvent.click(screen.getByRole('button', { name: /^GitHub notifications/ }))
    await userEvent.click(screen.getByRole('button', { name: /^Review requested.*Add the widget cache/ }))
    expect(threads.markRead).toHaveBeenCalledExactlyOnceWith([githubThread().id])

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    expect(onGitHubThreadsSettings).toHaveBeenCalledOnce()
  })

  it('opens the general settings from the GitHub notifications button when given nothing else to open', async () => {
    const checkNow = vi.fn()
    const { onOpenSettings } = renderHeader({
      githubThreads: threadsState(
        githubThreads({ status: { count: 1, unread: 1 }, unread: [githubThread()] }),
        checkNow,
      ),
      settings: settings(),
    })

    // Turning clearing on still checks GitHub, with no one to hand the settings to.
    await userEvent.click(screen.getByRole('button', { name: /^GitHub notifications/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Turn on automatic clearing' }))
    await vi.waitFor(() => {
      expect(checkNow).toHaveBeenCalledOnce()
    })

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
    expect(onOpenSettings).toHaveBeenCalledOnce()
  })

  it('opens the general settings from the bell when given no notification settings', async () => {
    const { onOpenSettings } = renderHeader({ notifications: historyOf() })

    await userEvent.click(screen.getByRole('button', { name: 'No unread notifications' }))
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

  it('shows the Scripts tab after Prompts while an integration is on, Notifications staying beside the Inbox', () => {
    renderHeader({ scripts: { running: 0, failed: false, blocked: false } })

    const views = within(screen.getByRole('navigation', { name: 'Views' })).getAllByRole('button')
    const scripts = screen.getByRole('button', { name: 'Scripts' })

    expect(views.map((b) => b.getAttribute('aria-label'))).toEqual([
      'Inbox',
      'Notifications',
      'Worktrees',
      'Prompts',
      'Scripts',
    ])
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
    expect(screen.queryByTestId('notifications-count')).not.toBeInTheDocument()
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
