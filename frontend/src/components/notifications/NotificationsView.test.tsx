import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { startLists, type NotificationLists } from '../../lib/notifications'
import type { GitHubThreadsState } from '../../lib/useGitHubThreads'
import type { NotificationsState } from '../../lib/useNotifications'
import type { AppNotification, GitHubThread, GitHubThreadsSnapshot, NotificationsTab, Settings } from '../../platform'
import { deferred } from '../../test/async'
import { at } from '../../test/clock'
import {
  autopilotAlert,
  githubThread,
  githubThreadList,
  githubThreads,
  item,
  list,
  notification,
  queuedClear,
  settings,
  settledClear,
  snapshot,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import NotificationsView from './NotificationsView'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// history is the app's own notifications, with spies for what the page does to them.
function history(items: AppNotification[] = [], overrides: Partial<NotificationsState> = {}): NotificationsState {
  return {
    log: { items, unread: items.filter((n) => !n.read).length },
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

// mirror is the user's GitHub notifications, listed as threads say, with spies.
function mirror(
  threads: GitHubThread[] | null = [],
  snap: GitHubThreadsSnapshot = githubThreads({ status: { count: threads?.length ?? 0 } }),
  overrides: Partial<GitHubThreadsState> = {},
): GitHubThreadsState {
  const answer = () => Promise.resolve({ ok: true as const, data: githubThreads() })

  return {
    snapshot: snap,
    error: '',
    reload: vi.fn(),
    checkNow: vi.fn(answer),
    checking: false,
    checkError: '',
    markDone: vi.fn(answer),
    markRead: vi.fn(answer),
    finishing: new Set(),
    reading: new Set(),
    markError: '',
    dismissMarkError: vi.fn(),
    list: threads === null ? null : githubThreadList({ threads, status: snap.status }),
    listError: '',
    keep: vi.fn(answer),
    ...overrides,
  }
}

interface PageOptions {
  tab?: NotificationsTab
  lists?: NotificationLists
  query?: string
  notifications?: NotificationsState
  threads?: GitHubThreadsState
  stored?: Settings | null
  inbox?: Parameters<typeof NotificationsView>[0]['snapshot']
}

const onOpen = vi.fn()
const onShowItem = vi.fn()
const onClearQuery = vi.fn()
const onNotificationSettings = vi.fn()
const onGitHubSettings = vi.fn()
const onSettingsSaved = vi.fn()
const onTab = vi.fn()

// Page holds the page's tab and lists, as the window does.
function Page({
  tab: firstTab = 'app',
  lists: firstLists = startLists,
  query = '',
  notifications = history(),
  threads = mirror(),
  stored = settings(),
  inbox = snapshot(),
}: PageOptions) {
  const [tab, setTab] = useState(firstTab)
  const [lists, setLists] = useState(firstLists)

  return (
    <NotificationsView
      tab={tab}
      onTab={(next) => {
        onTab(next)
        setTab(next)
      }}
      lists={lists}
      onLists={setLists}
      query={query}
      onClearQuery={onClearQuery}
      notifications={notifications}
      githubThreads={threads}
      settings={stored}
      onSettingsSaved={onSettingsSaved}
      snapshot={inbox}
      onShowItem={onShowItem}
      onOpen={onOpen}
      onNotificationSettings={onNotificationSettings}
      onGitHubSettings={onGitHubSettings}
    />
  )
}

function renderPage(options: PageOptions = {}) {
  return render(<Page {...options} />, { wrapper: at() })
}

// own matches a row's own button by its title, never one of its actions.
function own(title: string): RegExp {
  return new RegExp(`^(?!Done: |Mark unread: |Mark read: |Show in inbox: |Marking ).*${title}`)
}

describe('NotificationsView', () => {
  const review = notification({ id: 5 })
  const woke = notification({
    id: 4,
    kind: 'woke',
    read: true,
    title: 'Bump the parser',
    number: 8,
    createdAt: '2026-06-14T09:00:00Z',
  })
  const released = notification({
    id: 3,
    kind: 'release_done',
    itemId: 0,
    itemKind: '',
    repo: 'acme-org/sdk',
    number: 0,
    title: 'v1.4.3 is on GitHub',
    createdAt: '2026-06-10T09:00:00Z',
  })
  const failed = notification({
    id: 2,
    kind: 'run_failed',
    itemId: 0,
    itemKind: '',
    number: 0,
    title: 'Checks failed: lint',
    read: true,
    runId: 9,
    createdAt: '2025-12-30T09:00:00Z',
  })
  const entries = [review, woke, released, failed]

  describe('tabs', () => {
    it('counts each tab’s unread, and switches between them', async () => {
      renderPage({
        notifications: history(entries),
        threads: mirror([githubThread()], githubThreads({ status: { count: 9, unread: 4 } })),
      })

      const tabs = within(screen.getByRole('tablist', { name: 'Notifications' }))

      expect(tabs.getByRole('tab', { name: `${brand.name} 2` })).toHaveAttribute('aria-selected', 'true')
      expect(tabs.getByRole('tab', { name: 'GitHub 4' })).toHaveAttribute('aria-selected', 'false')

      await userEvent.click(tabs.getByRole('tab', { name: 'GitHub 4' }))

      expect(onTab).toHaveBeenCalledExactlyOnceWith('github')
      expect(tabs.getByRole('tab', { name: 'GitHub 4' })).toHaveAttribute('aria-selected', 'true')
      expect(screen.getByRole('radiogroup', { name: 'Show' })).toHaveTextContent('Cleared')
    })

    it('counts nothing on GitHub while the app doesn’t read it', () => {
      renderPage({
        tab: 'github',
        threads: mirror([], githubThreads({ status: { reading: false, unread: 4 } })),
        stored: settings({ githubNotifications: { ...settings().githubNotifications, read: false } }),
      })

      expect(screen.getByRole('tab', { name: 'GitHub' })).toBeInTheDocument()
    })
  })

  describe(`the ${brand.name} tab`, () => {
    it('lists the whole history under a heading for each day', () => {
      renderPage({ notifications: history(entries) })

      expect(screen.getByRole('list', { name: 'Today' })).toHaveTextContent('Add the widget cache')
      expect(screen.getByRole('list', { name: 'Yesterday' })).toHaveTextContent('Bump the parser')
      expect(screen.getByRole('list', { name: 'Wednesday' })).toHaveTextContent('v1.4.3 is on GitHub')
      expect(screen.getByRole('list', { name: 'Dec 30, 2025' })).toHaveTextContent('Checks failed: lint')
      expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
        'Today',
        'Yesterday',
        'Wednesday',
        'Dec 30, 2025',
      ])
    })

    it('styles the unread apart from the read', () => {
      renderPage({ notifications: history(entries) })

      const unread = screen.getByRole('button', { name: own('Add the widget cache') })
      const read = screen.getByRole('button', { name: own('Bump the parser') })

      expect(unread).toHaveTextContent('Unread: Review requested')
      expect(within(unread).getByText('Add the widget cache')).toHaveClass('font-semibold')
      expect(within(unread).getByTestId('notification-unread')).toBeInTheDocument()
      expect(read).not.toHaveTextContent('Unread')
      expect(within(read).getByText('Bump the parser')).toHaveClass('font-normal')
    })

    it('opens an entry, marks one done, and puts a read one back on the bell', async () => {
      const notifications = history(entries)
      renderPage({ notifications })

      await userEvent.click(screen.getByRole('button', { name: own('Add the widget cache') }))
      expect(onOpen).toHaveBeenCalledExactlyOnceWith(review)

      await userEvent.click(screen.getByRole('button', { name: 'Done: Add the widget cache' }))
      expect(notifications.markRead).toHaveBeenCalledExactlyOnceWith([5])

      expect(screen.queryByRole('button', { name: 'Done: Bump the parser' })).not.toBeInTheDocument()

      const unread = screen.getByRole('button', { name: 'Mark unread: Bump the parser' })

      expect(unread).toHaveAttribute('title', 'Mark unread · it goes back on the bell')
      await userEvent.click(unread)
      expect(notifications.markUnread).toHaveBeenCalledExactlyOnceWith([4])
    })

    it('narrows by Show and by Type', async () => {
      renderPage({ notifications: history(entries) })

      await userEvent.click(screen.getByRole('radio', { name: 'Unread' }))
      expect(screen.queryByText('Bump the parser')).not.toBeInTheDocument()
      expect(screen.getByText('Add the widget cache')).toBeInTheDocument()
      expect(screen.getByText('v1.4.3 is on GitHub')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('radio', { name: 'Releases' }))
      expect(screen.queryByText('Add the widget cache')).not.toBeInTheDocument()
      expect(screen.getByText('v1.4.3 is on GitHub')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('radio', { name: 'Runs' }))
      expect(screen.getByText('No failed runs.')).toBeInTheDocument()

      await userEvent.click(
        within(screen.getByRole('radiogroup', { name: 'Type' })).getByRole('radio', { name: 'All' }),
      )
      expect(screen.queryByText('Checks failed: lint')).not.toBeInTheDocument()

      await userEvent.click(
        within(screen.getByRole('radiogroup', { name: 'Show' })).getByRole('radio', { name: 'All' }),
      )
      expect(screen.getByText('Checks failed: lint')).toBeInTheDocument()
    })

    it('offers the Autopilot type, which lists Autopilot’s entries alone', async () => {
      renderPage({ notifications: history([...entries, autopilotAlert({ id: 9, itemId: 0 })]) })

      await userEvent.click(screen.getByRole('radio', { name: 'Autopilot' }))
      expect(screen.getByText('Bump golang.org/x/net')).toBeInTheDocument()
      expect(screen.queryByText('Add the widget cache')).not.toBeInTheDocument()
    })

    it('heads an Autopilot alert by its routine, and marks it with Autopilot’s bolt', () => {
      renderPage({ notifications: history([autopilotAlert({ id: 9 })]) })

      expect(screen.getByText('Tell me about security failures')).toBeInTheDocument()
      expect(screen.queryByText('Autopilot alert')).not.toBeInTheDocument()
      expect(screen.getByText('Bump golang.org/x/net')).toBeInTheDocument()
    })

    it('says when everything is read, and when there is nothing yet', () => {
      const { unmount } = renderPage({
        notifications: history([woke]),
        lists: { ...startLists, app: { show: 'unread', type: 'all' } },
      })

      expect(screen.getByText('You’re all caught up')).toBeInTheDocument()
      unmount()

      renderPage()

      expect(screen.getByText('No notifications yet')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Clear history' })).toBeDisabled()
      expect(screen.queryByRole('button', { name: 'Mark all done' })).not.toBeInTheDocument()
    })

    it('filters by the toolbar’s field, and offers to clear a filter that matches nothing', async () => {
      const { unmount } = renderPage({ notifications: history(entries), query: 'type:release' })

      expect(screen.getByText('v1.4.3 is on GitHub')).toBeInTheDocument()
      expect(screen.queryByText('Add the widget cache')).not.toBeInTheDocument()
      unmount()

      renderPage({ notifications: history(entries), query: 'nothing-like-it' })

      expect(screen.getByText('No notifications match')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Clear the filter' }))
      expect(onClearQuery).toHaveBeenCalledOnce()
    })

    it('marks them all done', async () => {
      const notifications = history(entries)
      renderPage({ notifications })

      await userEvent.click(screen.getByRole('button', { name: 'Mark all done' }))

      expect(notifications.markAllRead).toHaveBeenCalledOnce()
    })

    it('clears the history only once asked, and says when it could not', async () => {
      const notifications = history(entries)
      renderPage({ notifications })

      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))

      const dialog = screen.getByRole('dialog', { name: 'Clear the notification history?' })

      expect(dialog).toHaveTextContent('This forgets every notification. Banners and settings stay.')
      await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))
      expect(notifications.clear).not.toHaveBeenCalled()

      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))
      await userEvent.click(
        within(screen.getByRole('dialog', { name: 'Clear the notification history?' })).getByRole('button', {
          name: 'Clear history',
        }),
      )
      expect(notifications.clear).toHaveBeenCalledOnce()

      vi.mocked(notifications.clear).mockRejectedValueOnce(new Error('database is locked'))
      await userEvent.click(screen.getByRole('button', { name: 'Clear history' }))
      await userEvent.click(
        within(screen.getByRole('dialog', { name: 'Clear the notification history?' })).getByRole('button', {
          name: 'Clear history',
        }),
      )
      expect(await screen.findByText('Could not clear the history: database is locked')).toBeInTheDocument()
    })

    it('says what went wrong, and opens the notification settings', async () => {
      renderPage({ notifications: history(entries, { error: 'database is locked', markError: 'busy' }) })

      expect(screen.getByText('Could not load the notifications: database is locked')).toBeInTheDocument()
      expect(screen.getByText('Could not mark them: busy')).toBeInTheDocument()
      expect(screen.getByText(/Keeps your latest 1,000 notifications/)).toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Notification settings' }))
      expect(onNotificationSettings).toHaveBeenCalledOnce()
    })
  })

  describe('the GitHub tab', () => {
    const widget = githubThread({ id: 1, updatedAt: '2026-06-15T11:00:00Z' })
    const widgetRead = githubThread({ id: 2, reason: 'mention', unread: false, updatedAt: '2026-06-15T10:00:00Z' })
    const merged = githubThread({
      id: 3,
      number: 9,
      title: 'Fix the parser',
      reason: 'author',
      unread: false,
      state: 'MERGED',
      updatedAt: '2026-06-14T10:00:00Z',
    })
    const closed = githubThread({
      id: 4,
      number: 11,
      kind: 'issue',
      subjectType: 'Issue',
      title: 'Crash on start',
      reason: 'assign',
      state: 'CLOSED',
      updatedAt: '2026-06-14T09:00:00Z',
    })
    const release = githubThread({
      id: 5,
      kind: '',
      number: 0,
      subjectType: 'Release',
      title: 'v1.4.3',
      reason: 'subscribed',
      state: '',
      updatedAt: '2026-06-01T09:00:00Z',
    })
    const all = [widget, widgetRead, merged, closed, release]
    const inbox = snapshot({ reviewRequested: list([item()]) })

    it('says what the GitHub inbox holds and when it was checked, and checks again', async () => {
      const threads = mirror(all, githubThreads({ status: { count: 5, unread: 3 } }))
      renderPage({ tab: 'github', threads })

      expect(screen.getByText('3 unread · 5 in your GitHub inbox · checked 1m ago')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Check GitHub now' }))
      expect(threads.checkNow).toHaveBeenCalledOnce()

      await userEvent.click(screen.getByRole('button', { name: 'Open on GitHub' }))
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/notifications')

      await userEvent.click(screen.getByRole('button', { name: 'Settings' }))
      expect(onGitHubSettings).toHaveBeenCalledOnce()
    })

    it('lists every notification by item and by day, read and unread, with its item’s state', () => {
      renderPage({ tab: 'github', threads: mirror(all), inbox })

      expect(screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual([
        'Today',
        'Yesterday',
        'Jun 1',
      ])

      const line = screen.getByRole('button', { name: own('Add the widget cache') })

      expect(line).toHaveTextContent('Review requested, Mentioned')
      expect(within(line).getByText('Add the widget cache')).toHaveClass('font-semibold')
      expect(within(line).getByTestId('thread-unread')).toBeInTheDocument()

      const parser = screen.getByRole('button', { name: own('Fix the parser') })

      expect(parser).toHaveTextContent('Merged')
      expect(within(parser).getByText('Fix the parser')).toHaveClass('font-normal')
      expect(within(parser).queryByTestId('thread-unread')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: own('Crash on start') })).toHaveTextContent('Closed')
      expect(screen.getByRole('button', { name: own('v1.4.3') })).not.toHaveTextContent(/Merged|Closed/)
    })

    it('opens a line on GitHub and marks its unread read, and marks one read or done', async () => {
      const threads = mirror(all)
      renderPage({ tab: 'github', threads, inbox })

      await userEvent.click(screen.getByRole('button', { name: own('Add the widget cache') }))
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/7')
      expect(threads.markRead).toHaveBeenCalledExactlyOnceWith([1])

      await userEvent.click(screen.getByRole('button', { name: own('Fix the parser') }))
      expect(threads.markRead).toHaveBeenCalledOnce()

      await userEvent.click(screen.getByRole('button', { name: 'Mark read: Crash on start' }))
      expect(threads.markRead).toHaveBeenLastCalledWith([4])
      expect(screen.queryByRole('button', { name: 'Mark read: Fix the parser' })).not.toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Done: Add the widget cache' }))
      expect(threads.markDone).toHaveBeenCalledExactlyOnceWith([1, 2])
    })

    it('shows an item the inbox lists there, and offers it for no other', async () => {
      renderPage({ tab: 'github', threads: mirror(all), inbox })

      await userEvent.click(screen.getByRole('button', { name: 'Show in inbox: Add the widget cache' }))

      expect(onShowItem).toHaveBeenCalledExactlyOnceWith(1)
      expect(screen.queryByRole('button', { name: 'Show in inbox: Fix the parser' })).not.toBeInTheDocument()
    })

    it('dims a line on its way and turns the action it waits on', () => {
      renderPage({
        tab: 'github',
        threads: mirror(all, undefined, { finishing: new Set([1]), reading: new Set([4]) }),
        inbox,
      })

      const going = screen.getByRole('button', { name: 'Marking done: Add the widget cache' })

      expect(going).toBeDisabled()
      expect(going.closest('li')).toHaveAttribute('aria-busy', 'true')
      expect(screen.getByRole('button', { name: 'Marking read: Crash on start' })).toBeDisabled()
    })

    it('narrows by Show and by Type, and hides Type on Cleared', async () => {
      renderPage({ tab: 'github', threads: mirror(all) })

      await userEvent.click(screen.getByRole('radio', { name: 'Issues' }))
      expect(screen.getByText('Crash on start')).toBeInTheDocument()
      expect(screen.queryByText('Fix the parser')).not.toBeInTheDocument()

      await userEvent.click(screen.getByRole('radio', { name: 'Unread' }))
      await userEvent.click(screen.getByRole('radio', { name: 'Releases' }))
      expect(screen.getByText('v1.4.3')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('radio', { name: 'Actions' }))
      expect(screen.getByText('No notifications about workflow runs.')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('radio', { name: 'Cleared' }))
      expect(screen.queryByRole('radiogroup', { name: 'Type' })).not.toBeInTheDocument()
      expect(screen.getByText('Nothing cleared lately')).toBeInTheDocument()
    })

    it('says when nothing is unread, filters by the toolbar’s field, and offers to clear it', async () => {
      const { unmount } = renderPage({
        tab: 'github',
        threads: mirror([merged]),
        lists: { ...startLists, github: { show: 'unread', type: 'all' } },
      })

      expect(screen.getByText('You’re all caught up')).toBeInTheDocument()
      unmount()

      renderPage({ tab: 'github', threads: mirror(all), query: 'is:merged' })
      expect(screen.getByText('Fix the parser')).toBeInTheDocument()
      expect(screen.queryByText('Crash on start')).not.toBeInTheDocument()

      renderPage({ tab: 'github', threads: mirror(all), query: 'reason:invitation' })
      expect(screen.getByText('No notifications match')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Clear the filter' }))
      expect(onClearQuery).toHaveBeenCalledOnce()
    })

    it('shows the listing under way, and why it could not be listed', () => {
      const { unmount } = renderPage({ tab: 'github', threads: mirror(null) })

      expect(screen.getByRole('status')).toHaveTextContent('Listing your GitHub notifications…')
      unmount()

      renderPage({ tab: 'github', threads: mirror(null, undefined, { listError: 'database is locked' }) })
      expect(screen.queryByRole('status')).not.toBeInTheDocument()
      expect(screen.getByText('Could not list your GitHub notifications: database is locked')).toBeInTheDocument()
    })

    it('shows a check under way', () => {
      renderPage({
        tab: 'github',
        threads: mirror([], githubThreads({ status: { lastPassAt: '' } }), { checking: true }),
      })

      expect(screen.getByRole('button', { name: 'Checking GitHub' })).toBeDisabled()
      expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()
    })

    it('waits for the mirror before it says anything of it', () => {
      renderPage({ tab: 'github', threads: mirror(null, undefined, { snapshot: null }), inbox: null })

      expect(screen.getByRole('status')).toHaveTextContent('Listing your GitHub notifications…')
      expect(screen.getByRole('tab', { name: 'GitHub' })).toBeInTheDocument()
    })

    it('keeps the Cleared view empty before the mirror loads', () => {
      renderPage({
        tab: 'github',
        lists: { ...startLists, github: { show: 'cleared', type: 'all' } },
        threads: mirror([], undefined, { snapshot: null }),
      })

      expect(screen.getByText('Nothing cleared lately')).toBeInTheDocument()
    })

    it('offers a check while GitHub hasn’t been read yet', async () => {
      const threads = mirror([], githubThreads({ status: { lastPassAt: '' } }))
      renderPage({ tab: 'github', threads })

      expect(screen.getByText('Your GitHub notifications haven’t been read yet')).toBeInTheDocument()
      await userEvent.click(screen.getByRole('button', { name: 'Check now' }))
      expect(threads.checkNow).toHaveBeenCalledOnce()
    })

    it('says why GitHub refuses them, why the last check failed, and that it holds more than the app reads', async () => {
      const { unmount } = renderPage({
        tab: 'github',
        threads: mirror([], githubThreads({ status: { access: 'refused', askAgainAt: '2026-06-15T17:00:00Z' } })),
      })

      expect(screen.getByText('Your GitHub token can’t read notifications.')).toBeInTheDocument()
      expect(screen.getByText('Asks GitHub again in 5h.')).toBeInTheDocument()
      unmount()

      const threads = mirror(all, githubThreads({ status: { lastError: 'GitHub didn’t answer.', truncated: true } }), {
        checkError: 'database is locked',
        markError: 'GitHub asked for a pause before the next change.',
      })
      renderPage({ tab: 'github', threads })

      expect(screen.getByText('GitHub didn’t answer.')).toBeInTheDocument()
      expect(screen.getByText(/GitHub holds more than 1,000 notifications for you/)).toBeInTheDocument()
      expect(screen.getByText('Could not check GitHub: database is locked')).toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
      expect(threads.dismissMarkError).toHaveBeenCalledOnce()
    })

    it('turns reading on, then checks GitHub', async () => {
      const off = settings({ githubNotifications: { ...settings().githubNotifications, read: false } })
      const threads = mirror([], githubThreads({ status: { reading: false } }))
      renderPage({ tab: 'github', threads, stored: off })

      expect(screen.getByText(`${brand.name} isn’t reading your GitHub notifications`)).toBeInTheDocument()
      expect(screen.queryByRole('radiogroup', { name: 'Show' })).not.toBeInTheDocument()

      await userEvent.click(screen.getByRole('button', { name: 'Turn on' }))

      await waitFor(() => {
        expect(threads.checkNow).toHaveBeenCalledOnce()
      })
      expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledExactlyOnceWith('read', true)
      expect(onSettingsSaved).toHaveBeenCalled()
    })

    it('says why reading could not be turned on', async () => {
      platform.setGitHubNotificationSwitch.mockRejectedValueOnce(new Error('database is locked'))
      const off = settings({ githubNotifications: { ...settings().githubNotifications, read: false } })
      const threads = mirror([], githubThreads({ status: { reading: false } }))
      renderPage({ tab: 'github', threads, stored: off })

      await userEvent.click(screen.getByRole('button', { name: 'Turn on' }))

      expect(await screen.findByRole('alert')).toHaveTextContent('Could not turn it on: database is locked')
      expect(threads.checkNow).not.toHaveBeenCalled()
    })

    it('says reading is off before the settings load', () => {
      renderPage({ tab: 'github', threads: mirror([], githubThreads({ status: { reading: false } })), stored: null })

      expect(screen.getByText(`${brand.name} isn’t reading your GitHub notifications`)).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Turn on' })).not.toBeInTheDocument()
    })
  })

  describe('the Cleared view', () => {
    const cleared = { ...startLists, github: { show: 'cleared' as const, type: 'all' as const } }

    it('counts down each clear about to go, and takes one back with Undo', async () => {
      const threads = mirror(
        [],
        githubThreads({
          queued: [
            queuedClear(),
            queuedClear({
              id: 2,
              number: 9,
              title: 'Fix the parser',
              trigger: 'by_hand',
              sendAfter: '2026-06-15T12:05:00Z',
            }),
          ],
        }),
      )
      renderPage({ tab: 'github', lists: cleared, threads })

      const soon = within(screen.getByRole('list', { name: 'Clearing soon' }))
      const rows = soon.getAllByRole('listitem')

      expect(rows[0]).toHaveTextContent('Add the widget cache')
      expect(rows[0]).toHaveTextContent(`example-repo #7 · You dismiss an item in ${brand.name}`)
      expect(rows[0]).toHaveTextContent('in 15s')
      expect(rows[1]).toHaveTextContent('example-repo #9 · You cleared it')
      expect(rows[1]).toHaveTextContent('in 5m')

      await userEvent.click(soon.getByRole('button', { name: 'Undo: Add the widget cache' }))

      expect(threads.keep).toHaveBeenCalledExactlyOnceWith([1])
    })

    it('shows an Undo under way, and why it failed', async () => {
      const answer = deferred<Awaited<ReturnType<GitHubThreadsState['keep']>>>()
      const threads = mirror([], githubThreads({ queued: [queuedClear()] }), {
        keep: vi.fn(() => answer.promise),
      })
      renderPage({ tab: 'github', lists: cleared, threads })

      await userEvent.click(screen.getByRole('button', { name: 'Undo: Add the widget cache' }))

      expect(screen.getByRole('button', { name: 'Undo: Add the widget cache' })).toHaveTextContent('Keeping…')

      await act(async () => {
        answer.resolve({ ok: false, error: 'database is locked' })
        await Promise.resolve()
      })

      expect(await screen.findByText('Could not undo: database is locked')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Undo: Add the widget cache' })).toHaveTextContent('Undo')
    })

    it('moves the countdown on every second while a clear waits', async () => {
      vi.useFakeTimers()

      try {
        let now = new Date('2026-06-15T12:00:00Z')
        const { clockAt, withClock } = await import('../../test/clock')
        const clock = clockAt(() => now)

        render(<Page tab="github" lists={cleared} threads={mirror([], githubThreads({ queued: [queuedClear()] }))} />, {
          wrapper: withClock(clock),
        })

        expect(screen.getByText('in 15s')).toBeInTheDocument()

        now = new Date('2026-06-15T12:00:05Z')
        act(() => {
          vi.advanceTimersByTime(1000)
        })

        expect(screen.getByText('in 10s')).toBeInTheDocument()

        now = new Date('2026-06-15T12:00:20Z')
        act(() => {
          vi.advanceTimersByTime(1000)
        })

        expect(screen.getByText('any moment')).toBeInTheDocument()
      } finally {
        vi.useRealTimers()
      }
    })

    it('says how each recent clear settled, hiding the skips decided again, and opens one on GitHub', async () => {
      const threads = mirror(
        [],
        githubThreads({
          recent: [
            settledClear({ id: 1 }),
            settledClear({ id: 2, action: 'read', title: 'Read one' }),
            settledClear({ id: 3, state: 'kept', title: 'Kept one' }),
            settledClear({ id: 4, state: 'failed', error: 'GitHub refused it.', title: 'Failed one' }),
            settledClear({ id: 5, state: 'skipped', reason: 'new_activity', title: 'Busy one' }),
            settledClear({ id: 6, state: 'skipped', reason: 'changed', title: 'Changed one' }),
            settledClear({ id: 7, state: 'skipped', reason: 'stale', title: 'Stale one' }),
          ],
        }),
      )
      renderPage({ tab: 'github', lists: cleared, threads })

      const recent = within(screen.getByRole('list', { name: 'Recently cleared' }))
      const rows = recent.getAllByRole('button')

      expect(rows.map((r) => r.textContent)).toEqual([
        expect.stringContaining('Marked done'),
        expect.stringContaining('Marked read'),
        expect.stringContaining('Kept'),
        expect.stringContaining('Couldn’t clear: GitHub refused it.'),
        expect.stringContaining('Skipped: there was new activity'),
      ])
      expect(rows[0]).toHaveTextContent('example-repo #7 · A pull request is merged')
      expect(screen.queryByText('Changed one')).not.toBeInTheDocument()
      expect(screen.queryByText('Stale one')).not.toBeInTheDocument()
      expect(screen.queryByRole('list', { name: 'Clearing soon' })).not.toBeInTheDocument()

      await userEvent.click(rows[0] as HTMLElement)
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/pull/7')

      await userEvent.click(screen.getByRole('button', { name: 'GitHub’s Done view' }))
      expect(platform.openExternal).toHaveBeenLastCalledWith('https://github.com/notifications?query=is%3Adone')
    })

    it('offers to clear on its own while that is off', async () => {
      const off = settings({ githubNotifications: { ...settings().githubNotifications, auto: false } })
      const threads = mirror([])
      renderPage({ tab: 'github', lists: cleared, threads, stored: off })

      await userEvent.click(screen.getByRole('button', { name: 'Turn on automatic clearing' }))

      await waitFor(() => {
        expect(threads.checkNow).toHaveBeenCalledOnce()
      })
      expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledExactlyOnceWith('auto', true)
    })

    it('offers nothing while clearing is on, or while GitHub refuses the token', () => {
      const on = settings({ githubNotifications: { ...settings().githubNotifications, auto: true } })
      const { unmount } = renderPage({ tab: 'github', lists: cleared, stored: on })

      expect(screen.queryByRole('button', { name: 'Turn on automatic clearing' })).not.toBeInTheDocument()
      unmount()

      renderPage({
        tab: 'github',
        lists: cleared,
        threads: mirror([], githubThreads({ status: { access: 'refused' } })),
      })
      expect(screen.queryByRole('button', { name: 'Turn on automatic clearing' })).not.toBeInTheDocument()
    })
  })
})
