import { render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { NotificationsState } from '../lib/useNotifications'
import type { AppNotification, NotificationKind } from '../platform'
import { at } from '../test/clock'
import { notification } from '../test/fixtures'
import { mocks } from '../test/platform'
import { bellLabel, NotificationsMenu } from './NotificationsMenu'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

const onOpen = vi.fn()
const onSettings = vi.fn()
const onViewAll = vi.fn()

// state is the history the bell shows, with spies for what it does.
function state(overrides: Partial<NotificationsState> = {}): NotificationsState {
  return {
    log: { items: [], unread: 0 },
    error: '',
    markRead: vi.fn(),
    markUnread: vi.fn(),
    markAllRead: vi.fn(),
    markItemRead: vi.fn(),
    markError: '',
    clear: vi.fn(() => Promise.resolve()),
    ...overrides,
  }
}

// logOf is a log holding items, counting the unread.
function logOf(items: AppNotification[]): NotificationsState['log'] {
  return { items, unread: items.filter((n) => !n.read).length }
}

// entryName matches an entry's own button by its title, never its Done.
function entryName(title: string): RegExp {
  return new RegExp(`^(?!Done: ).*${title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`)
}

// open clicks the bell and returns the queries scoped to the quick view.
async function open() {
  await userEvent.click(screen.getByRole('button', { name: /unread notifications$/ }))

  return within(screen.getByRole('dialog', { name: 'Notifications' }))
}

describe('NotificationsMenu', () => {
  const review = notification({ id: 3 })
  const woke = notification({
    id: 2,
    kind: 'woke',
    title: 'Bump the parser',
    number: 8,
    detail: 'approved, 2 new commits',
    createdAt: '2026-06-15T09:00:00Z',
  })
  const read = notification({ id: 1, read: true, title: 'Fix the flaky test', number: 4 })

  it('names the bell apart from the Notifications tab', () => {
    expect(bellLabel(0)).toBe('No unread notifications')
    expect(bellLabel(1)).toBe('1 unread notifications')
    expect(bellLabel(1200)).toBe('1,200 unread notifications')
  })

  it('counts the unread on the bell', () => {
    render(
      <NotificationsMenu
        notifications={state({ log: logOf([review, read]) })}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    expect(screen.getByRole('button', { name: '1 unread notifications' })).toBeInTheDocument()
    expect(screen.getByTestId('notifications-unread')).toHaveTextContent('1')
  })

  it('caps the count at 99+', () => {
    render(
      <NotificationsMenu
        notifications={state({ log: { items: [review], unread: 140 } })}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    expect(screen.getByTestId('notifications-unread')).toHaveTextContent('99+')
  })

  it('shows no count when everything is read', () => {
    render(<NotificationsMenu notifications={state()} onOpen={onOpen} onSettings={onSettings} />)

    expect(screen.getByRole('button', { name: 'No unread notifications' })).toBeInTheDocument()
    expect(screen.queryByTestId('notifications-unread')).not.toBeInTheDocument()
  })

  it('marks nothing on opening, and lists only the unread, newest first', async () => {
    const notifications = state({ log: logOf([review, woke, read]) })
    render(<NotificationsMenu notifications={notifications} onOpen={onOpen} onSettings={onSettings} />)

    const panel = await open()

    expect(notifications.markRead).not.toHaveBeenCalled()
    expect(notifications.markAllRead).not.toHaveBeenCalled()

    expect(mocks().filter(([, fn]) => fn.mock.calls.length > 0)).toEqual([])

    const entries = panel.getAllByRole('button', {
      name: /^(?!Done: ).*(Add the widget cache|Bump the parser|Fix the flaky)/,
    })
    expect(entries).toHaveLength(2)
    expect(entries[0]).toHaveTextContent('Unread: Review requested')
    expect(entries[0]).toHaveTextContent('5m ago')
    expect(entries[0]).toHaveTextContent('example-repo #7')
    expect(entries[1]).toHaveTextContent('Back from snooze')
    expect(entries[1]).toHaveTextContent('example-repo #8 · approved, 2 new commits')
    expect(panel.queryByText('Fix the flaky test')).not.toBeInTheDocument()
    expect(panel.getAllByTestId('notification-unread')).toHaveLength(2)
  })

  it('shows a kind it does not know with the bell, titled plainly', async () => {
    // A kind another build stored, outside the ones this build knows.
    const mentions = notification({ id: 4, kind: 'mentions' as unknown as NotificationKind, title: 'Ping' })
    render(
      <NotificationsMenu notifications={state({ log: logOf([mentions]) })} onOpen={onOpen} onSettings={onSettings} />,
    )

    const panel = await open()

    expect(panel.getByRole('button', { name: entryName('Ping') })).toHaveTextContent('Notification')
  })

  it('opens an entry and closes', async () => {
    render(
      <NotificationsMenu notifications={state({ log: logOf([review]) })} onOpen={onOpen} onSettings={onSettings} />,
    )

    const panel = await open()
    await userEvent.click(panel.getByRole('button', { name: entryName('Add the widget cache') }))

    expect(onOpen).toHaveBeenCalledWith(review)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('marks one entry done with its ✓, which keeps the panel open', async () => {
    const notifications = state({ log: logOf([review, woke]) })
    render(<NotificationsMenu notifications={notifications} onOpen={onOpen} onSettings={onSettings} />)

    const panel = await open()
    const done = panel.getByRole('button', { name: 'Done: Add the widget cache' })

    expect(done).toHaveAttribute('title', 'Done · it stays in your history')

    await userEvent.click(done)

    expect(notifications.markRead).toHaveBeenCalledWith([3])
    expect(onOpen).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('marks them all done', async () => {
    const notifications = state({ log: logOf([review, woke]) })
    render(<NotificationsMenu notifications={notifications} onOpen={onOpen} onSettings={onSettings} />)

    const panel = await open()
    await userEvent.click(panel.getByRole('button', { name: 'Mark all done' }))

    expect(notifications.markAllRead).toHaveBeenCalledOnce()
  })

  it('lists at most twenty, and says how many more wait on the page', async () => {
    const many = Array.from({ length: 35 }, (_, i) => notification({ id: 100 - i, title: `Entry ${String(i)}` }))
    render(
      <NotificationsMenu
        notifications={state({ log: logOf(many) })}
        onOpen={onOpen}
        onSettings={onSettings}
        onViewAll={onViewAll}
      />,
    )

    const panel = await open()

    expect(panel.getAllByRole('button', { name: /^(?!Done: ).*Entry \d+/ })).toHaveLength(20)
    expect(panel.getByText(/15 more unread/)).toBeInTheDocument()

    const [more] = panel.getAllByRole('button', { name: 'View all' })
    await userEvent.click(more as HTMLElement)

    expect(onViewAll).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says how many more without a page to view them on', async () => {
    const many = Array.from({ length: 21 }, (_, i) => notification({ id: 100 - i, title: `Entry ${String(i)}` }))
    render(<NotificationsMenu notifications={state({ log: logOf(many) })} onOpen={onOpen} onSettings={onSettings} />)

    const panel = await open()

    expect(panel.getByText('1 more unread')).toBeInTheDocument()
    expect(panel.queryByRole('button', { name: 'View all' })).not.toBeInTheDocument()
  })

  it('opens the page from its footer', async () => {
    render(
      <NotificationsMenu
        notifications={state({ log: logOf([read]) })}
        onOpen={onOpen}
        onSettings={onSettings}
        onViewAll={onViewAll}
      />,
    )

    const panel = await open()
    await userEvent.click(panel.getByRole('button', { name: 'View all' }))

    expect(onViewAll).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows a failed run as a failure, and opens it', async () => {
    const failed = notification({
      id: 5,
      kind: 'run_failed',
      itemId: 0,
      itemKind: '',
      repo: 'acme-org/example-repo',
      number: 0,
      title: 'Checks failed: Checks · all files',
      url: '',
      detail: '4 passed, 1 failed: whitespace.',
      runId: 9,
    })
    const elsewhere = notification({
      id: 6,
      kind: 'run_failed',
      itemId: 0,
      itemKind: '',
      repo: '',
      number: 0,
      title: 'A script failed in ~/projects/notes: lint',
      url: '',
      detail: '',
      runId: 10,
    })
    render(
      <NotificationsMenu
        notifications={state({ log: logOf([failed, elsewhere]) })}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    const panel = await open()
    const entry = panel.getByRole('button', { name: entryName('Checks failed') })

    expect(entry).toHaveTextContent('Run failed')
    expect(entry).toHaveTextContent('example-repo · 4 passed, 1 failed: whitespace.')
    expect(entry.querySelector('.text-danger svg')).not.toBeNull()

    // A run in a folder that isn't a repository leaves no empty line under its title.
    const folder = panel.getByRole('button', { name: entryName('A script failed') })

    expect(folder).not.toHaveTextContent('·')
    expect(folder).not.toHaveAttribute('title')

    await userEvent.click(entry)

    expect(onOpen).toHaveBeenCalledWith(failed)
  })

  it('marks a release workflow that failed as a failure, and one that published plainly', async () => {
    const release = { itemId: 0, itemKind: '' as const, repo: 'acme-org/sdk', number: 0, detail: '' }
    const published = notification({
      ...release,
      id: 7,
      kind: 'release_published',
      title: 'acme-org/sdk v1.4.3 is published',
      url: 'https://github.com/acme-org/sdk/releases/tag/v1.4.3',
    })
    const workflowFailed = notification({
      ...release,
      id: 8,
      kind: 'release_failed',
      title: 'The release workflow failed for acme-org/sdk v1.4.4',
      url: 'https://github.com/acme-org/sdk/actions/runs/7',
    })
    render(
      <NotificationsMenu
        notifications={state({ log: logOf([published, workflowFailed]) })}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    const panel = await open()
    const failedEntry = panel.getByRole('button', { name: entryName('The release workflow failed for') })
    const publishedEntry = panel.getByRole('button', { name: entryName('is published') })

    expect(failedEntry).toHaveTextContent('Release workflow failed')
    expect(failedEntry.querySelector('.text-danger svg')).not.toBeNull()
    expect(publishedEntry).toHaveTextContent('Published')
    expect(publishedEntry.querySelector('.text-danger svg')).toBeNull()
  })

  it('says when there is nothing yet, offers no Clear, and opens the settings', async () => {
    render(<NotificationsMenu notifications={state()} onOpen={onOpen} onSettings={onSettings} />)

    const panel = await open()

    expect(panel.getByText('No notifications yet')).toBeInTheDocument()
    expect(panel.getByText(/releases, and failed runs show up here/)).toBeInTheDocument()
    expect(panel.queryByRole('button', { name: /Clear/ })).not.toBeInTheDocument()
    expect(panel.queryByRole('button', { name: 'Mark all done' })).not.toBeInTheDocument()

    await userEvent.click(panel.getByRole('button', { name: 'Notification settings' }))

    expect(onSettings).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says when everything is read, and where the past ones are', async () => {
    render(<NotificationsMenu notifications={state({ log: logOf([read]) })} onOpen={onOpen} onSettings={onSettings} />)

    const panel = await open()

    expect(panel.getByText('You’re all caught up')).toBeInTheDocument()
    expect(panel.getByText('Past notifications are on the Notifications page.')).toBeInTheDocument()
    expect(panel.queryByRole('button', { name: /Clear/ })).not.toBeInTheDocument()
    expect(panel.queryByRole('button', { name: 'Mark all done' })).not.toBeInTheDocument()
  })

  it('says when the entries could not be marked read', async () => {
    render(
      <NotificationsMenu
        notifications={state({ log: logOf([review]), markError: 'database is locked' })}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    const panel = await open()

    expect(panel.getByRole('alert')).toHaveTextContent('Could not mark them read: database is locked')
  })

  it('says when the history could not be loaded', async () => {
    render(
      <NotificationsMenu
        notifications={state({ error: 'database is locked' })}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    const panel = await open()

    expect(panel.getByRole('alert')).toHaveTextContent('Could not load the notifications: database is locked')
  })
})
