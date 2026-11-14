import { render as renderPlain, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { noTags } from '../lib/githubThreads'
import type { GitHubThreadsState } from '../lib/useGitHubThreads'
import type { GitHubThreadsSnapshot, Settings, ThreadRules } from '../platform'
import { at } from '../test/clock'
import { githubThread, githubThreads, settings } from '../test/fixtures'
import { platform } from '../test/platform'
import { GitHubThreadsMenu } from './GitHubThreadsMenu'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

// state is the notifications the button shows, with spies for what it does.
function state(
  snapshot: GitHubThreadsSnapshot | null,
  overrides: Partial<GitHubThreadsState> = {},
): GitHubThreadsState {
  return {
    snapshot,
    byItem: noTags,
    error: '',
    reload: vi.fn(),
    checkNow: vi.fn(() => Promise.resolve({ ok: true as const, data: githubThreads() })),
    checking: false,
    checkError: '',
    ...overrides,
  }
}

const onOpen = vi.fn()
const onSettings = vi.fn()
const onSettingsSaved = vi.fn()

// rulesOf is the fixture's settings with the GitHub notification rules changed.
function rulesOf(change: Partial<ThreadRules>): Settings {
  return settings({ githubNotifications: { ...settings().githubNotifications, ...change } })
}

// menu is the button over threads, with automatic clearing as auto says (null: the
// settings haven't loaded), and reading as read says.
function menu(threads: GitHubThreadsState, auto: boolean | null = true, read = true) {
  return (
    <GitHubThreadsMenu
      threads={threads}
      settings={auto === null ? null : rulesOf({ auto, read })}
      onOpen={onOpen}
      onSettings={onSettings}
      onSettingsSaved={onSettingsSaved}
    />
  )
}

// renderMenu renders the menu.
function renderMenu(threads: GitHubThreadsState, auto: boolean | null = true, read = true) {
  return render(menu(threads, auto, read))
}

// button is the toolbar's GitHub notifications button.
function button() {
  return screen.getByRole('button', { name: /^GitHub notifications/ })
}

// open clicks the button and returns the queries scoped to its panel.
async function open() {
  await userEvent.click(button())

  return within(screen.getByRole('dialog', { name: 'GitHub notifications' }))
}

// calledPlatform lists the platform calls made so far.
function calledPlatform(): string[] {
  return Object.entries(platform)
    .filter(([, fn]) => fn.mock.calls.length > 0)
    .map(([name]) => name)
}

describe('GitHubThreadsMenu', () => {
  it('counts the GitHub inbox on its badge, accented while any are unread', () => {
    renderMenu(state(githubThreads({ status: { count: 38, unread: 5 } })))

    expect(button()).toHaveAccessibleName('GitHub notifications, 38 in your inbox, 5 unread')

    const badge = screen.getByTestId('github-threads-count')
    expect(badge).toHaveTextContent('38')
    expect(badge).toHaveClass('bg-accent')
  })

  it('leaves the count unaccented once none are unread', () => {
    renderMenu(state(githubThreads({ status: { count: 38 } })))

    const badge = screen.getByTestId('github-threads-count')
    expect(badge).toHaveTextContent('38')
    expect(badge).not.toHaveClass('bg-accent')
    expect(button()).toHaveAccessibleName('GitHub notifications, 38 in your inbox')
  })

  it('caps the count at 99+', () => {
    renderMenu(state(githubThreads({ status: { count: 1000, unread: 140 } })))

    expect(screen.getByTestId('github-threads-count')).toHaveTextContent('99+')
  })

  it('hides the count at none', () => {
    renderMenu(state(githubThreads()))

    expect(button()).toHaveAccessibleName('GitHub notifications')
    expect(screen.queryByTestId('github-threads-count')).not.toBeInTheDocument()
  })

  it('goes as soon as the settings say reading is off, before the mirror does', () => {
    renderMenu(state(githubThreads({ status: { count: 3 } })), true, false)

    expect(screen.queryByRole('button', { name: /^GitHub notifications/ })).not.toBeInTheDocument()
  })

  it('closes its panel as it goes, so it doesn’t open again when it comes back', async () => {
    const threads = state(githubThreads({ status: { count: 3 } }))
    const { rerender } = renderMenu(threads)

    await open()
    rerender(menu(threads, true, false))
    expect(screen.queryByRole('button', { name: /^GitHub notifications/ })).not.toBeInTheDocument()

    rerender(menu(threads))
    expect(button()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // The mirror saying reading is off hides it the same way.
    await open()
    rerender(menu(state(githubThreads({ status: { reading: false } }))))
    rerender(menu(threads))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('isn’t there while the app doesn’t read them, or before the mirror loads', () => {
    const { unmount } = renderMenu(state(githubThreads({ status: { reading: false } })))

    expect(screen.queryByRole('button', { name: /^GitHub notifications/ })).not.toBeInTheDocument()
    unmount()

    renderMenu(state(null))
    expect(screen.queryByRole('button', { name: /^GitHub notifications/ })).not.toBeInTheDocument()
  })

  it('marks the button with a warning dot while GitHub refuses the token, says why, and checks again', async () => {
    const threads = state(
      githubThreads({
        status: {
          access: 'refused',
          count: 12,
          unread: 3,
          lastError: 'Your GitHub token can’t read notifications.',
          askAgainAt: '2026-06-15T17:00:00Z',
        },
      }),
    )
    renderMenu(threads, false)

    expect(screen.getByTestId('github-threads-refused')).toHaveClass('bg-warning')
    expect(screen.queryByTestId('github-threads-count')).not.toBeInTheDocument()
    expect(button()).toHaveAccessibleName('GitHub notifications, can’t be read')

    const panel = await open()

    expect(panel.getByText('Your GitHub token can’t read notifications.')).toBeInTheDocument()
    expect(panel.getByText('Asks GitHub again in 5h.')).toBeInTheDocument()
    // Clearing can't help while nothing can be read.
    expect(panel.queryByRole('button', { name: 'Turn on automatic clearing' })).not.toBeInTheDocument()

    await userEvent.click(panel.getByRole('button', { name: 'Check again' }))
    expect(threads.checkNow).toHaveBeenCalledOnce()
  })

  it('reads the mirror again as it opens, and sends GitHub nothing and marks nothing read', async () => {
    const threads = state(
      githubThreads({ status: { count: 38, unread: 2 }, unread: [githubThread(), githubThread({ id: 2, number: 9 })] }),
    )
    renderMenu(threads, false)

    const panel = await open()

    expect(panel.getByRole('heading', { name: 'GitHub notifications' })).toBeInTheDocument()
    expect(panel.getByText('38 in your GitHub inbox · 2 unread')).toBeInTheDocument()
    expect(threads.reload).toHaveBeenCalledOnce()
    expect(threads.checkNow).not.toHaveBeenCalled()
    expect(calledPlatform()).toEqual([])
    // The badge still counts what it counted: nothing was marked read.
    expect(screen.getByTestId('github-threads-count')).toHaveTextContent('38')
  })

  it('lists the newest unread grouped by item, and opens the item a line is about', async () => {
    renderMenu(
      state(
        githubThreads({
          status: { count: 5, unread: 3 },
          unread: [
            githubThread({ id: 1, reason: 'mention', updatedAt: '2026-06-15T11:58:00Z' }),
            githubThread({ id: 2, kind: 'issue', number: 9, subjectType: 'Issue', title: 'Fix the parser' }),
            githubThread({ id: 3, reason: 'review_requested' }),
          ],
        }),
      ),
    )

    const panel = await open()
    const lines = within(panel.getByRole('list', { name: 'Unread on GitHub' })).getAllByRole('listitem')

    expect(lines).toHaveLength(2)
    expect(lines[0]).toHaveTextContent('Mentioned, Review requested · 2 unread')
    expect(lines[0]).toHaveTextContent('2m ago')
    expect(lines[0]).toHaveTextContent('Add the widget cache')
    expect(lines[0]).toHaveTextContent('example-repo #7')
    expect(lines[1]).toHaveTextContent('Review requested')
    expect(lines[1]).toHaveTextContent('example-repo #9')

    const parser = panel.getByRole('button', { name: /Fix the parser/ })
    expect(parser).toHaveAttribute('title', 'acme-org/example-repo #9')

    await userEvent.click(parser)

    expect(onOpen).toHaveBeenCalledExactlyOnceWith({
      owner: 'acme-org',
      name: 'example-repo',
      kind: 'issue',
      number: 9,
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows a notification about no item as plain text, and every title as text', async () => {
    renderMenu(
      state(
        githubThreads({
          status: { count: 2, unread: 2 },
          unread: [
            githubThread({
              id: 5,
              kind: '',
              number: 0,
              subjectType: 'Release',
              title: 'v1.4.3',
              reason: 'subscribed',
            }),
            githubThread({ id: 6, title: '<img src=x onerror=alert(1)> Add the cache' }),
          ],
        }),
      ),
    )

    const panel = await open()
    const release = panel.getByText('v1.4.3')

    expect(release.closest('button')).toBeNull()
    expect(release.closest('li')).toHaveTextContent('acme-org/example-repo · Release')
    expect(release.closest('li')).toHaveTextContent('Watching')
    expect(panel.getByRole('button', { name: /<img src=x onerror=alert\(1\)> Add the cache/ })).toBeInTheDocument()
    expect(document.querySelector('img')).toBeNull()
  })

  it('says when nothing is unread', async () => {
    renderMenu(state(githubThreads({ status: { count: 4 } })))

    const panel = await open()

    expect(panel.getByText('Nothing unread')).toBeInTheDocument()
    expect(panel.queryByRole('list')).not.toBeInTheDocument()
  })

  it('turns automatic clearing on, then checks GitHub', async () => {
    const threads = state(githubThreads({ status: { count: 4 } }))
    renderMenu(threads, false)

    const panel = await open()
    await userEvent.click(panel.getByRole('button', { name: 'Turn on automatic clearing' }))

    await waitFor(() => {
      expect(threads.checkNow).toHaveBeenCalledOnce()
    })
    expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledExactlyOnceWith('auto', true)
    expect(onSettingsSaved).toHaveBeenCalledExactlyOnceWith(rulesOf({ auto: true }))
    expect(platform.setGitHubNotificationSwitch.mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(threads.checkNow).mock.invocationCallOrder[0] ?? 0,
    )
  })

  it('says why automatic clearing could not be turned on, and checks nothing', async () => {
    platform.setGitHubNotificationSwitch.mockRejectedValueOnce(new Error('database is locked'))
    const threads = state(githubThreads())
    renderMenu(threads, false)

    const panel = await open()
    await userEvent.click(panel.getByRole('button', { name: 'Turn on automatic clearing' }))

    expect(await panel.findByRole('alert')).toHaveTextContent(
      'Could not turn on automatic clearing: database is locked',
    )
    expect(threads.checkNow).not.toHaveBeenCalled()
    expect(onSettingsSaved).not.toHaveBeenCalled()
  })

  it.each([
    ['on', true],
    ['loading', null],
  ] as const)('offers no automatic clearing while it is %s', async (_, auto) => {
    renderMenu(state(githubThreads()), auto)

    const panel = await open()

    expect(panel.queryByRole('button', { name: 'Turn on automatic clearing' })).not.toBeInTheDocument()
  })

  it('opens the GitHub inbox and the settings from its footer', async () => {
    renderMenu(state(githubThreads()))

    await userEvent.click((await open()).getByRole('button', { name: 'Open on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/notifications')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await userEvent.click((await open()).getByRole('button', { name: 'Settings' }))
    expect(onSettings).toHaveBeenCalledOnce()
  })

  it('shows a check under way, and says why the last one failed', async () => {
    const threads = state(
      githubThreads({ status: { access: 'refused', lastError: 'Your GitHub token can’t read notifications.' } }),
      { checking: true, checkError: 'database is locked' },
    )
    renderMenu(threads)

    const panel = await open()

    expect(panel.getByRole('button', { name: 'Checking…' })).toBeDisabled()
    expect(panel.getByRole('alert')).toHaveTextContent('Could not check GitHub: database is locked')

    // Clicking the button again closes the panel, reading nothing more.
    await userEvent.click(button())
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(threads.reload).toHaveBeenCalledOnce()
  })

  it('says why the last check failed, and checks again', async () => {
    const threads = state(githubThreads({ status: { count: 4, lastError: 'GitHub didn’t answer in time.' } }))
    renderMenu(threads)

    const panel = await open()

    expect(panel.getByText('GitHub didn’t answer in time.')).toBeInTheDocument()
    await userEvent.click(panel.getByRole('button', { name: 'Check again' }))
    expect(threads.checkNow).toHaveBeenCalledOnce()
  })
})
