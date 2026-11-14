import { render as renderPlain, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { Settings, ThreadRules } from '../../platform'
import { at } from '../../test/clock'
import { githubThreads, settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { useGitHubThreads } from '../../lib/useGitHubThreads'
import { GitHubNotificationsSection } from './GitHubNotificationsSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// rulesOf is the fixture's settings with the GitHub notification rules changed.
function rulesOf(change: Partial<ThreadRules>): Settings {
  return settings({ githubNotifications: { ...settings().githubNotifications, ...change } })
}

interface StoredProps {
  initial: Settings
  onSaved: (settings: Settings) => void
}

// Stored keeps the settings each change stored, and the mirror of the notifications, as
// the window does, so the section shows them once saved.
function Stored({ initial, onSaved }: StoredProps) {
  const [current, setCurrent] = useState(initial)
  const threads = useGitHubThreads()

  return (
    <GitHubNotificationsSection
      settings={current}
      onSaved={(next) => {
        onSaved(next)
        setCurrent(next)
      }}
      threads={threads}
    />
  )
}

// renderSection renders the section on a clock that stands at the tests' noon.
function renderSection(initial: Settings = settings()) {
  const onSaved = vi.fn()

  renderPlain(<Stored initial={initial} onSaved={onSaved} />, { wrapper: at() })

  return { onSaved }
}

// toggle is a switch, by its label.
function toggle(name: string) {
  return screen.getByRole('switch', { name })
}

describe('GitHubNotificationsSection', () => {
  it('shows each rule as stored, every moment on by default', () => {
    renderSection()

    expect(toggle('Read my GitHub notifications')).toBeChecked()
    expect(toggle('Clear them automatically')).not.toBeChecked()
    expect(toggle('Clear them automatically')).toBeEnabled()

    for (const name of [
      'A pull request is merged',
      'A pull request is closed without merging',
      'An issue is closed',
      `You submit a review in ${brand.name}`,
      `You dismiss an item in ${brand.name}`,
      'You unassign yourself or remove yourself as reviewer',
      'Include repositories you only watch',
      'Count bot activity after it closes as seen',
    ]) {
      expect(toggle(name)).toBeChecked()
    }

    expect(screen.getByRole('radio', { name: 'Mark done' })).toBeChecked()
    expect(screen.getByRole('radio', { name: 'Mark read' })).not.toBeChecked()
    expect(screen.getByRole('radiogroup', { name: 'What clearing does' })).toHaveAccessibleDescription(
      /Mark read leaves it there: the count on the toolbar’s GitHub button stays, and only its accent goes/,
    )
  })

  it('holds “Clear them automatically” while reading is off, since clearing needs reading', () => {
    renderSection(rulesOf({ read: false }))

    expect(toggle('Clear them automatically')).toBeDisabled()
    expect(toggle('Clear them automatically')).toHaveAccessibleDescription(/Needs reading them/)
    // Nothing is read, so nothing is said of how reading goes.
    expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument()
  })

  it('turns reading on and reads them from GitHub at once', async () => {
    const { onSaved } = renderSection(rulesOf({ read: false }))

    await userEvent.click(toggle('Read my GitHub notifications'))

    await waitFor(() => {
      expect(platform.checkGitHubThreadsNow).toHaveBeenCalledOnce()
    })
    expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledExactlyOnceWith('read', true)
    expect(onSaved).toHaveBeenCalledExactlyOnceWith(rulesOf({ read: true }))
    expect(platform.setGitHubNotificationSwitch.mock.invocationCallOrder[0]).toBeLessThan(
      platform.checkGitHubThreadsNow.mock.invocationCallOrder[0] ?? 0,
    )
  })

  it('turns clearing on and reads them from GitHub at once', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(toggle('Clear them automatically'))

    await waitFor(() => {
      expect(platform.checkGitHubThreadsNow).toHaveBeenCalledOnce()
    })
    expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledExactlyOnceWith('auto', true)
    expect(onSaved).toHaveBeenCalledExactlyOnceWith(rulesOf({ auto: true }))
  })

  it('turns reading off and reads the mirror again, asking GitHub nothing', async () => {
    const { onSaved } = renderSection()

    await waitFor(() => {
      expect(platform.githubThreadsSnapshot).toHaveBeenCalledOnce()
    })
    await userEvent.click(toggle('Read my GitHub notifications'))

    await waitFor(() => {
      expect(platform.githubThreadsSnapshot).toHaveBeenCalledTimes(2)
    })
    expect(onSaved).toHaveBeenCalledExactlyOnceWith(rulesOf({ read: false }))
    expect(platform.checkGitHubThreadsNow).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument()
  })

  it('says nothing of how reading goes without the window’s mirror', () => {
    renderPlain(<GitHubNotificationsSection settings={settings()} onSaved={vi.fn()} threads={null} />, {
      wrapper: at(),
    })

    expect(toggle('Read my GitHub notifications')).toBeChecked()
    expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument()
    expect(platform.githubThreadsSnapshot).not.toHaveBeenCalled()
  })

  it('says a notification waits while a person’s activity since is unread', () => {
    renderSection()

    expect(
      screen.getByText(
        /unless a person’s activity since is still unread on GitHub\. Read it there, and it clears on the next check\./,
      ),
    ).toBeInTheDocument()
  })

  it.each([
    ['Include repositories you only watch', 'watching', false],
    ['Count bot activity after it closes as seen', 'botActivity', false],
  ] as const)('saves %j, and asks GitHub nothing for it', async (name, key, on) => {
    const { onSaved } = renderSection()

    await userEvent.click(toggle(name))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(platform.setGitHubNotificationSwitch).toHaveBeenCalledExactlyOnceWith(key, on)
    expect(platform.checkGitHubThreadsNow).not.toHaveBeenCalled()
  })

  it('asks GitHub nothing for a failed change, and says why', async () => {
    platform.setGitHubNotificationSwitch.mockRejectedValueOnce(new Error('database is locked'))
    renderSection()

    await userEvent.click(toggle('Clear them automatically'))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    expect(toggle('Clear them automatically')).not.toBeChecked()
    expect(platform.checkGitHubThreadsNow).not.toHaveBeenCalled()
  })

  it.each([
    ['A pull request is merged', 'merged'],
    ['A pull request is closed without merging', 'pr_closed'],
    ['An issue is closed', 'issue_closed'],
    [`You submit a review in ${brand.name}`, 'reviewed'],
    [`You dismiss an item in ${brand.name}`, 'dismissed'],
    ['You unassign yourself or remove yourself as reviewer', 'unassigned'],
  ] as const)('turns off and back on the moment %j', async (name, trigger) => {
    const { onSaved } = renderSection()

    await userEvent.click(toggle(name))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledOnce()
    })
    expect(platform.setGitHubNotificationTrigger).toHaveBeenCalledExactlyOnceWith(trigger, false)
    expect(toggle(name)).not.toBeChecked()

    await userEvent.click(toggle(name))

    await waitFor(() => {
      expect(platform.setGitHubNotificationTrigger).toHaveBeenLastCalledWith(trigger, true)
    })
  })

  it('shows a moment turned off as off', () => {
    renderSection(rulesOf({ triggersOff: ['reviewed'] }))

    expect(toggle(`You submit a review in ${brand.name}`)).not.toBeChecked()
    expect(toggle('A pull request is merged')).toBeChecked()
  })

  it('chooses what clearing does', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(screen.getByRole('radio', { name: 'Mark read' }))

    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledExactlyOnceWith(rulesOf({ action: 'read' }))
    })
    expect(platform.setGitHubNotificationAction).toHaveBeenCalledExactlyOnceWith('read')
    expect(screen.getByRole('radio', { name: 'Mark read' })).toBeChecked()
  })

  it('says what the GitHub inbox holds and when it was checked, and checks again', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(
      githubThreads({ status: { count: 38, unread: 5, lastPassAt: '2026-06-15T11:58:00Z' } }),
    )
    platform.checkGitHubThreadsNow.mockResolvedValue(
      githubThreads({ status: { count: 40, lastPassAt: '2026-06-15T12:00:00Z' } }),
    )
    renderSection()

    expect(await screen.findByText('38 in your GitHub inbox · checked 2m ago')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))

    expect(await screen.findByText('40 in your GitHub inbox · checked just now')).toBeInTheDocument()
    expect(platform.checkGitHubThreadsNow).toHaveBeenCalledOnce()
  })

  it('says GitHub refuses the token, and when it asks again', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(
      githubThreads({
        status: {
          access: 'refused',
          lastError: 'Your GitHub token can’t read notifications.',
          askAgainAt: '2026-06-15T18:00:00Z',
        },
      }),
    )
    renderSection()

    expect(await screen.findByText('Your GitHub token can’t read notifications.')).toHaveClass('text-warning')
    expect(screen.getByText('Asks GitHub again in 6h.')).toBeInTheDocument()
    expect(screen.queryByText(/in your GitHub inbox/)).not.toBeInTheDocument()
  })

  it('says why the last check failed', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(
      githubThreads({ status: { count: 3, lastError: 'GitHub didn’t answer in time.' } }),
    )
    renderSection()

    expect(await screen.findByText('GitHub didn’t answer in time.')).toBeInTheDocument()
  })

  it('says why Check again failed', async () => {
    platform.checkGitHubThreadsNow.mockRejectedValueOnce(new Error('database is locked'))
    renderSection()

    await userEvent.click(await screen.findByRole('button', { name: 'Check again' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('Could not check GitHub: database is locked')
  })

  it('notes that the app reads the newest 1,000 when GitHub holds more', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { count: 1000, truncated: true } }))
    renderSection()

    expect(
      await screen.findByText(
        `GitHub holds more than 1,000 notifications for you. ${brand.name} reads the newest 1,000, and counts only those.`,
      ),
    ).toBeInTheDocument()
  })

  it('notes nothing of the newest 1,000 while GitHub holds fewer', async () => {
    platform.githubThreadsSnapshot.mockResolvedValue(githubThreads({ status: { count: 12 } }))
    renderSection()

    await screen.findByText(/^12 in your GitHub inbox/)
    expect(screen.queryByText(/newest 1,000/)).not.toBeInTheDocument()
  })
})
