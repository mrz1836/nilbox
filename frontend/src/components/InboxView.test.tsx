import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { item, launchResult, list, profile, snapshot } from '../test/fixtures'
import { InboxView, type LaunchOptions } from './InboxView'

const platform = vi.hoisted(() => ({
  getInboxSnapshot: vi.fn(),
  refreshInbox: vi.fn(),
  onInboxUpdated: vi.fn(),
  openInBrowser: vi.fn(),
  launch: vi.fn(),
}))

vi.mock('../platform', () => platform)

const now = new Date('2026-06-15T12:00:00Z')
const clock = () => now

const launchOptions: LaunchOptions = { profiles: [profile()], defaultProfile: 'default', worktreeDefault: true }

const stopListening = vi.fn()

// section waits for one inbox list and returns the queries scoped to it.
async function section(title: string) {
  return within(await screen.findByRole('region', { name: title }))
}

beforeEach(() => {
  vi.clearAllMocks()
  platform.getInboxSnapshot.mockResolvedValue(snapshot())
  platform.onInboxUpdated.mockReturnValue(stopListening)
  platform.openInBrowser.mockResolvedValue(undefined)
  platform.launch.mockResolvedValue(launchResult())
})

afterEach(() => {
  vi.useRealTimers()
})

describe('InboxView', () => {
  it('renders the three lists from the snapshot', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        reviewRequested: list([item({ id: 1, title: 'Add the widget cache', updatedAt: '2026-06-15T09:00:00Z' })]),
        authored: list([
          item({ id: 2, number: 12, title: 'Fix the parser', repo: 'acme-org/other-repo', isDraft: true }),
        ]),
        assigned: list([
          item({
            id: 3,
            kind: 'issue',
            number: 31,
            title: 'Crash on start',
            author: 'dependabot[bot]',
            trusted: false,
          }),
        ]),
        lastRefreshedAt: '2026-06-15T11:55:00Z',
      }),
    )

    render(<InboxView launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    expect(review.getByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(review.getByText(/acme-org\/example-repo #7 · octo-user ·/)).toBeInTheDocument()
    expect(review.getByText('3h')).toHaveAttribute('dateTime', '2026-06-15T09:00:00Z')
    expect(review.getByText('PR')).toBeInTheDocument()

    const authored = await section('Authored')
    expect(authored.getByRole('button', { name: 'Fix the parser' })).toBeInTheDocument()
    expect(authored.getByText(/acme-org\/other-repo #12/)).toBeInTheDocument()
    expect(authored.getByText('draft')).toBeInTheDocument()

    const assigned = await section('Assigned')
    expect(assigned.getByText('Issue')).toBeInTheDocument()
    expect(assigned.getByText(/dependabot\[bot\]/)).toBeInTheDocument()
    expect(assigned.getByText('outside contributor')).toBeInTheDocument()

    expect(screen.getByText('5m ago')).toBeInTheDocument()
    expect(screen.getByText('Signed in as octo-user')).toBeInTheDocument()
    expect(screen.getByRole('meter', { name: 'GraphQL budget remaining' })).toBeInTheDocument()
  })

  it('shows N of M', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        reviewRequested: list([item({ id: 1 }), item({ id: 2, number: 8 })], 250),
        authored: list([item({ id: 3, number: 9 })]),
      }),
    )

    render(<InboxView launch={launchOptions} clock={clock} />)

    expect((await section('Review requested')).getByText('2 of 250')).toBeInTheDocument()
    expect((await section('Authored')).getByText('1')).toBeInTheDocument()
    expect((await section('Assigned')).getByText('0')).toBeInTheDocument()
    expect((await section('Assigned')).getByText('Nothing here.')).toBeInTheDocument()
  })

  it('treats a list with no items as empty', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ authored: { items: null, shown: 0, total: 0 } }))

    render(<InboxView launch={launchOptions} clock={clock} />)

    expect((await section('Authored')).getByText('Nothing here.')).toBeInTheDocument()
    expect(screen.getByText('Not refreshed yet')).toBeInTheDocument()
  })

  it('shows launch controls only once the launch options are loaded', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))

    const { rerender } = render(<InboxView launch={null} clock={clock} />)

    expect(await screen.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()

    rerender(<InboxView launch={launchOptions} clock={clock} />)

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()
  })

  it('opens an item in the browser', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
    render(<InboxView launch={launchOptions} clock={clock} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

    expect(platform.openInBrowser).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
  })

  it('logs a browser that could not be opened', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.openInBrowser.mockRejectedValue(new Error('no browser'))
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
    render(<InboxView launch={launchOptions} clock={clock} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

    await waitFor(() => {
      expect(logged).toHaveBeenCalled()
    })
    logged.mockRestore()
  })

  it('refreshes on click and shows the new snapshot', async () => {
    let finish: (value: unknown) => void = () => undefined
    platform.refreshInbox.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    render(<InboxView launch={launchOptions} clock={clock} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Refresh' }))
    expect(screen.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()

    await act(async () => {
      finish(snapshot({ authored: list([item({ title: 'Fresh from GitHub' })]), lastRefreshedAt: now.toISOString() }))
      await Promise.resolve()
    })

    expect(await screen.findByRole('button', { name: 'Fresh from GitHub' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Refresh' })).toBeEnabled()
    expect(screen.getByText('just now')).toBeInTheDocument()
  })

  it('shows an actionable refresh error verbatim', async () => {
    const message =
      'borrow the GitHub token: gh not found: install the GitHub CLI from https://cli.github.com, then run gh auth login'
    platform.refreshInbox.mockRejectedValue(new Error(message))
    render(<InboxView launch={launchOptions} clock={clock} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Refresh' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(message)
  })

  it('shows the stored refresh error and the scope warning', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        lastError: 'missing scopes read:org: run gh auth refresh -s repo,read:org',
        scopeWarning: 'the token reports no scopes, so they could not be verified',
      }),
    )
    render(<InboxView launch={launchOptions} clock={clock} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('run gh auth refresh -s repo,read:org')
    expect(screen.getByText('the token reports no scopes, so they could not be verified')).toBeInTheDocument()
  })

  it('shows an inbox that could not be loaded', async () => {
    platform.getInboxSnapshot.mockRejectedValue(new Error('open the database: locked'))
    render(<InboxView launch={launchOptions} clock={clock} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('open the database: locked')
    expect(screen.queryByText('Loading the inbox…')).not.toBeInTheDocument()
  })

  it('reloads when the inbox updates and stops listening on unmount', async () => {
    platform.getInboxSnapshot
      .mockResolvedValueOnce(snapshot())
      .mockResolvedValueOnce(snapshot({ assigned: list([item({ title: 'Updated in the background' })]) }))

    const { unmount } = render(<InboxView launch={launchOptions} clock={clock} />)

    expect(await screen.findAllByText('Nothing here.')).toHaveLength(3)
    expect(platform.onInboxUpdated).toHaveBeenCalledOnce()
    const listener = platform.onInboxUpdated.mock.calls[0]?.[0] as () => void

    act(() => {
      listener()
    })

    expect(await screen.findByRole('button', { name: 'Updated in the background' })).toBeInTheDocument()
    expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(2)

    unmount()
    expect(stopListening).toHaveBeenCalledOnce()
  })

  it('brings the ages up to date every minute', async () => {
    // Only the minute ticker is faked, so the loads and the queries' own waits still run.
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    let current = now
    const ticking = () => current

    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z' }))
    render(<InboxView launch={launchOptions} clock={ticking} />)

    expect(await screen.findByText('5m ago')).toBeInTheDocument()

    current = new Date(now.getTime() + 60_000)
    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    expect(screen.getByText('6m ago')).toBeInTheDocument()
  })
})
