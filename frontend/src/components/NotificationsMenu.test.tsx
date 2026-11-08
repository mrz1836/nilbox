import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { NotificationsState } from '../lib/useNotifications'
import type { NotificationKind } from '../platform'
import { notification } from '../test/fixtures'
import { NotificationsMenu } from './NotificationsMenu'

const now = new Date('2026-06-15T12:00:00Z')

const onOpen = vi.fn()
const onSettings = vi.fn()

// state is the history the bell shows, with spies for what it does.
function state(overrides: Partial<NotificationsState> = {}): NotificationsState {
  return {
    log: { items: [], unread: 0 },
    error: '',
    markRead: vi.fn(),
    clear: vi.fn(() => Promise.resolve()),
    ...overrides,
  }
}

// open clicks the bell and returns the queries scoped to the history.
async function open() {
  await userEvent.click(screen.getByRole('button', { name: /^Notifications/ }))

  return within(screen.getByRole('dialog', { name: 'Notifications' }))
}

describe('NotificationsMenu', () => {
  const review = notification({ id: 3 })
  const woke = notification({
    id: 2,
    kind: 'woke',
    read: true,
    title: 'Bump the parser',
    number: 8,
    detail: 'approved, 2 new commits',
    createdAt: '2026-06-15T09:00:00Z',
  })

  it('counts the unread on the bell', () => {
    render(
      <NotificationsMenu
        notifications={state({ log: { items: [review, woke], unread: 1 } })}
        now={now}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    expect(screen.getByRole('button', { name: 'Notifications, 1 unread' })).toBeInTheDocument()
    expect(screen.getByTestId('notifications-unread')).toHaveTextContent('1')
  })

  it('caps the count at 99+', () => {
    render(
      <NotificationsMenu
        notifications={state({ log: { items: [review], unread: 140 } })}
        now={now}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    expect(screen.getByTestId('notifications-unread')).toHaveTextContent('99+')
  })

  it('shows a kind it does not know with the bell, titled plainly', async () => {
    // A kind another build stored, outside the ones this build knows.
    const mentions = notification({ id: 4, kind: 'mentions' as unknown as NotificationKind, title: 'Ping' })
    render(
      <NotificationsMenu
        notifications={state({ log: { items: [mentions], unread: 1 } })}
        now={now}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    const history = await open()

    expect(history.getByRole('button', { name: /Notification.*Ping/ })).toBeInTheDocument()
  })

  it('shows no count when everything is read', () => {
    render(<NotificationsMenu notifications={state()} now={now} onOpen={onOpen} onSettings={onSettings} />)

    expect(screen.getByRole('button', { name: 'Notifications' })).toBeInTheDocument()
    expect(screen.queryByTestId('notifications-unread')).not.toBeInTheDocument()
  })

  it('lists the history newest first, marks it read, and keeps the new entries marked while open', async () => {
    const notifications = state({ log: { items: [review, woke], unread: 1 } })
    const { rerender } = render(
      <NotificationsMenu notifications={notifications} now={now} onOpen={onOpen} onSettings={onSettings} />,
    )

    const panel = await open()

    expect(notifications.markRead).toHaveBeenCalledOnce()

    const entries = panel.getAllByRole('button', { name: /Add the widget cache|Bump the parser/ })
    expect(entries).toHaveLength(2)
    expect(entries[0]).toHaveTextContent('Unread: Review requested')
    expect(entries[0]).toHaveTextContent('5m ago')
    expect(entries[0]).toHaveTextContent('example-repo #7')
    expect(entries[1]).toHaveTextContent('Back from snooze')
    expect(entries[1]).toHaveTextContent('example-repo #8 · approved, 2 new commits')
    expect(entries[1]).not.toHaveTextContent('Unread')

    // Read now, the first entry stays marked while the history is open.
    rerender(
      <NotificationsMenu
        notifications={{ ...notifications, log: { items: [{ ...review, read: true }, woke], unread: 0 } }}
        now={now}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )
    expect(panel.getAllByTestId('notification-unread')).toHaveLength(1)
  })

  it('does not mark an all-read history again', async () => {
    const notifications = state({ log: { items: [woke], unread: 0 } })
    render(<NotificationsMenu notifications={notifications} now={now} onOpen={onOpen} onSettings={onSettings} />)

    await open()

    expect(notifications.markRead).not.toHaveBeenCalled()
  })

  it('opens an entry’s item and closes', async () => {
    render(
      <NotificationsMenu
        notifications={state({ log: { items: [review], unread: 0 } })}
        now={now}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    const panel = await open()
    await userEvent.click(panel.getByRole('button', { name: /Add the widget cache/ }))

    expect(onOpen).toHaveBeenCalledWith(review)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says when there is nothing yet, and opens the settings', async () => {
    render(<NotificationsMenu notifications={state()} now={now} onOpen={onOpen} onSettings={onSettings} />)

    const panel = await open()

    expect(panel.getByText('No notifications yet')).toBeInTheDocument()
    expect(panel.queryByRole('button', { name: 'Clear' })).not.toBeInTheDocument()

    await userEvent.click(panel.getByRole('button', { name: 'Notification settings' }))

    expect(onSettings).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('clears the history, and says when it could not', async () => {
    const notifications = state({ log: { items: [review], unread: 0 } })
    render(<NotificationsMenu notifications={notifications} now={now} onOpen={onOpen} onSettings={onSettings} />)

    let panel = await open()
    await userEvent.click(panel.getByRole('button', { name: 'Clear' }))
    expect(notifications.clear).toHaveBeenCalledOnce()

    vi.mocked(notifications.clear).mockRejectedValue(new Error('database is locked'))
    await userEvent.click(panel.getByRole('button', { name: 'Clear' }))
    expect(await panel.findByRole('alert')).toHaveTextContent('Could not clear: database is locked')

    // Opening the history again starts without the old error.
    await userEvent.click(screen.getByRole('button', { name: /^Notifications/ }))
    panel = await open()
    expect(panel.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says when the history could not be loaded', async () => {
    render(
      <NotificationsMenu
        notifications={state({ error: 'database is locked' })}
        now={now}
        onOpen={onOpen}
        onSettings={onSettings}
      />,
    )

    const panel = await open()

    expect(panel.getByRole('alert')).toHaveTextContent('Could not load the notifications: database is locked')
  })
})
