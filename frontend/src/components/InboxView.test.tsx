import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useCollapsedSections } from '../lib/useCollapsedSections'
import { useInbox } from '../lib/useInbox'
import { item, itemResolution, launchResult, list, profile, prompts, snapshot } from '../test/fixtures'
import { InboxView, type LaunchOptions, type Reveal } from './InboxView'

const platform = vi.hoisted(() => ({
  getInboxSnapshot: vi.fn(),
  refreshInbox: vi.fn(),
  onInboxUpdated: vi.fn(),
  openInBrowser: vi.fn(),
  launch: vi.fn(),
  dismissItem: vi.fn(),
  snoozeItem: vi.fn(),
  restoreItem: vi.fn(),
  pinItem: vi.fn(),
  unpinItem: vi.fn(),
  refreshItem: vi.fn(),
  getCollapsedSections: vi.fn(),
  setSectionCollapsed: vi.fn(),
}))

vi.mock('../platform', () => platform)

const now = new Date('2026-06-15T12:00:00Z')
const clock = () => now

const launchOptions: LaunchOptions = {
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  resolutions: new Map([[1, itemResolution()]]),
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
}

const stopListening = vi.fn()

const openSnoozeSettings = vi.fn()

interface HarnessProps {
  launch: LaunchOptions | null
  clock: () => Date
  snoozeDays?: number
  reveal?: Reveal | null
}

// Harness renders the view over the real inbox state and collapsed lists, the way App does.
function Harness({ launch, clock, snoozeDays = 3, reveal = null }: HarnessProps) {
  const inbox = useInbox(clock)
  const sections = useCollapsedSections()

  return (
    <InboxView
      inbox={inbox}
      launch={launch}
      snoozeDays={snoozeDays}
      onSnoozeSettings={openSnoozeSettings}
      collapsed={sections.collapsed}
      onCollapse={sections.setCollapsed}
      reveal={reveal}
    />
  )
}

// section waits for one inbox list and returns the queries scoped to it.
async function section(title: string) {
  return within(await screen.findByRole('region', { name: title }))
}

beforeEach(() => {
  platform.getInboxSnapshot.mockResolvedValue(snapshot())
  platform.onInboxUpdated.mockReturnValue(stopListening)
  platform.openInBrowser.mockResolvedValue(undefined)
  platform.launch.mockResolvedValue(launchResult())
  platform.getCollapsedSections.mockResolvedValue([])
  platform.setSectionCollapsed.mockResolvedValue(undefined)
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

    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    expect(review.getByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(review.getByText(/acme-org\/example-repo #7 · octo-user ·/)).toBeInTheDocument()
    expect(review.getByText('3h')).toHaveAttribute('dateTime', '2026-06-15T09:00:00Z')
    expect(review.getByRole('img', { name: 'Pull request' })).toBeInTheDocument()

    const authored = await section('Authored')
    expect(authored.getByRole('button', { name: 'Fix the parser' })).toBeInTheDocument()
    expect(authored.getByText(/acme-org\/other-repo #12/)).toBeInTheDocument()
    expect(authored.getByText('Draft')).toBeInTheDocument()

    const assigned = await section('Assigned')
    expect(assigned.getByRole('img', { name: 'Issue' })).toBeInTheDocument()
    expect(assigned.getByText(/dependabot\[bot\]/)).toBeInTheDocument()
    expect(assigned.getByText('outside contributor')).toBeInTheDocument()

    expect(screen.getByText('5m ago')).toBeInTheDocument()
    // The account and the API budget live in the toolbar now, not beside the lists.
    expect(screen.queryByText(/Signed in as/)).not.toBeInTheDocument()
    expect(screen.queryByRole('meter')).not.toBeInTheDocument()
  })

  it('shows N of M', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        reviewRequested: list([item({ id: 1 }), item({ id: 2, number: 8 })], 250),
        authored: list([item({ id: 3, number: 9 })]),
      }),
    )

    render(<Harness launch={launchOptions} clock={clock} />)

    expect((await section('Review requested')).getByText('2 of 250')).toBeInTheDocument()
    expect((await section('Authored')).getByText('1')).toBeInTheDocument()
    expect((await section('Assigned')).getByText('0')).toBeInTheDocument()
    expect((await section('Assigned')).getByText('Nothing here.')).toBeInTheDocument()
  })

  it('treats a list with no items as empty', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ authored: { items: null, shown: 0, total: 0 } }))

    render(<Harness launch={launchOptions} clock={clock} />)

    expect((await section('Authored')).getByText('Nothing here.')).toBeInTheDocument()
    expect(screen.getByText('Not refreshed yet')).toBeInTheDocument()
  })

  it('shows launch controls only once the launch options are loaded', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))

    const { rerender } = render(<Harness launch={null} clock={clock} />)

    expect(await screen.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()

    rerender(<Harness launch={launchOptions} clock={clock} />)

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()
  })

  it('opens an item in the browser', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
    render(<Harness launch={launchOptions} clock={clock} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

    expect(platform.openInBrowser).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
  })

  it('logs a browser that could not be opened', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.openInBrowser.mockRejectedValue(new Error('no browser'))
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
    render(<Harness launch={launchOptions} clock={clock} />)

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
    render(<Harness launch={launchOptions} clock={clock} />)

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
    render(<Harness launch={launchOptions} clock={clock} />)

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
    render(<Harness launch={launchOptions} clock={clock} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('run gh auth refresh -s repo,read:org')
    expect(screen.getByText('the token reports no scopes, so they could not be verified')).toBeInTheDocument()
  })

  it('shows an inbox that could not be loaded', async () => {
    platform.getInboxSnapshot.mockRejectedValue(new Error('open the database: locked'))
    render(<Harness launch={launchOptions} clock={clock} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('open the database: locked')
    expect(screen.queryByText('Loading the inbox…')).not.toBeInTheDocument()
  })

  it('reloads when the inbox updates and stops listening on unmount', async () => {
    platform.getInboxSnapshot
      .mockResolvedValueOnce(snapshot())
      .mockResolvedValueOnce(snapshot({ assigned: list([item({ title: 'Updated in the background' })]) }))

    const { unmount } = render(<Harness launch={launchOptions} clock={clock} />)

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
    render(<Harness launch={launchOptions} clock={ticking} />)

    expect(await screen.findByText('5m ago')).toBeInTheDocument()

    current = new Date(now.getTime() + 60_000)
    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    expect(screen.getByText('6m ago')).toBeInTheDocument()
  })
})

describe('InboxView collapsing', () => {
  const authored = snapshot({
    authored: list([
      item({ id: 1, title: 'Fix the parser' }),
      item({ id: 2, number: 8, title: 'Bump the parser', snoozedUntil: '2026-06-18T12:00:00Z' }),
    ]),
  })

  it('collapses a list from its header to just its tabs, and opens it again', async () => {
    platform.getInboxSnapshot.mockResolvedValue(authored)
    render(<Harness launch={launchOptions} clock={clock} />)

    const list = await section('Authored')
    expect(list.getByRole('button', { name: 'Fix the parser' })).toBeInTheDocument()

    await userEvent.click(list.getByRole('button', { name: 'Collapse Authored' }))

    const toggle = list.getByRole('button', { name: 'Expand Authored' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(list.queryByRole('tabpanel')).not.toBeInTheDocument()
    expect(list.queryByRole('button', { name: 'Fix the parser' })).not.toBeInTheDocument()
    // The tabs and their counts stay.
    expect(list.getByRole('tab', { name: 'Authored 1' })).toBeInTheDocument()
    expect(list.getByRole('tab', { name: 'Snoozed 1' })).toBeInTheDocument()
    // The other lists stay open.
    expect((await section('Assigned')).getByRole('button', { name: 'Collapse Assigned' })).toHaveAttribute(
      'aria-expanded',
      'true',
    )

    await userEvent.click(toggle)

    expect(list.getByRole('button', { name: 'Collapse Authored' })).toHaveAttribute('aria-expanded', 'true')
    expect(list.getByRole('button', { name: 'Fix the parser' })).toBeInTheDocument()
    expect(platform.setSectionCollapsed.mock.calls).toEqual([
      ['authored', true],
      ['authored', false],
    ])
  })

  it('opens with the lists collapsed the way they were left', async () => {
    platform.getInboxSnapshot.mockResolvedValue(authored)
    platform.getCollapsedSections.mockResolvedValue(['authored', 'assigned'])
    render(<Harness launch={launchOptions} clock={clock} />)

    const list = await section('Authored')
    expect(list.getByRole('button', { name: 'Expand Authored' })).toHaveAttribute('aria-expanded', 'false')
    expect(list.queryByRole('button', { name: 'Fix the parser' })).not.toBeInTheDocument()
    expect((await section('Assigned')).getByRole('button', { name: 'Expand Assigned' })).toBeInTheDocument()
    expect((await section('Review requested')).getByRole('tabpanel')).toBeVisible()
    expect(platform.setSectionCollapsed).not.toHaveBeenCalled()
  })

  it('waits for the collapsed lists before showing any, so none flashes open', async () => {
    let resolve: (sections: string[]) => void = () => undefined
    platform.getInboxSnapshot.mockResolvedValue(authored)
    platform.getCollapsedSections.mockReturnValue(
      new Promise<string[]>((r) => {
        resolve = r
      }),
    )
    render(<Harness launch={launchOptions} clock={clock} />)

    await waitFor(() => {
      expect(platform.getInboxSnapshot).toHaveBeenCalled()
    })
    expect(screen.getByText('Loading the inbox…')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Authored' })).not.toBeInTheDocument()

    act(() => {
      resolve(['authored'])
    })

    const list = await section('Authored')
    expect(list.queryByRole('button', { name: 'Fix the parser' })).not.toBeInTheDocument()
  })

  it('opens a collapsed list when one of its tabs is chosen', async () => {
    platform.getInboxSnapshot.mockResolvedValue(authored)
    platform.getCollapsedSections.mockResolvedValue(['authored'])
    render(<Harness launch={launchOptions} clock={clock} />)

    const list = await section('Authored')
    await userEvent.click(list.getByRole('tab', { name: 'Snoozed 1' }))

    expect(list.getByRole('button', { name: 'Collapse Authored' })).toHaveAttribute('aria-expanded', 'true')
    expect(list.getByRole('tabpanel')).toHaveAccessibleName('Snoozed 1')
    expect(list.getByRole('button', { name: 'Bump the parser' })).toBeInTheDocument()
    expect(platform.setSectionCollapsed).toHaveBeenCalledExactlyOnceWith('authored', false)

    // Choosing a tab of an open list leaves it as it is.
    await userEvent.click(list.getByRole('tab', { name: 'Authored 1' }))
    expect(platform.setSectionCollapsed).toHaveBeenCalledExactlyOnceWith('authored', false)
  })
})

describe('InboxView triage', () => {
  // A review request that is in the inbox, one snoozed until Thursday, and one dismissed.
  const fresh = item({ id: 1, title: 'Add the widget cache' })
  const snoozed = item({ id: 2, number: 8, title: 'Bump the parser', snoozedUntil: '2026-06-18T12:00:00Z' })
  const dismissed = item({ id: 3, number: 9, title: 'Rename the flag', dismissedAt: '2026-06-15T09:00:00Z' })

  it('shows each list as tabs, with what was set aside counted apart', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([fresh, snoozed, dismissed], 40), assigned: list([fresh]) }),
    )
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    expect(review.getByRole('tab', { name: 'Review requested 1 of 38' })).toHaveAttribute('aria-selected', 'true')
    expect(review.getByRole('tab', { name: 'Snoozed 1' })).toHaveAttribute('aria-selected', 'false')
    expect(review.getByRole('tab', { name: 'Dismissed 1' })).toBeInTheDocument()
    expect(review.getByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(review.queryByRole('button', { name: 'Bump the parser' })).not.toBeInTheDocument()
    expect(review.queryByRole('button', { name: 'Rename the flag' })).not.toBeInTheDocument()

    // Tabs with nothing in them show no count.
    const assigned = await section('Assigned')
    expect(assigned.getByRole('tab', { name: 'Snoozed' })).toBeInTheDocument()
    expect(assigned.getByRole('tab', { name: 'Dismissed' })).toBeInTheDocument()

    await userEvent.click(review.getByRole('tab', { name: 'Snoozed 1' }))
    expect(review.getByRole('tabpanel')).toHaveAccessibleName('Snoozed 1')
    expect(review.getByRole('button', { name: 'Bump the parser' })).toBeInTheDocument()
    expect(review.getByText(/Back Thu, 12:00\sPM/)).toHaveAttribute(
      'title',
      expect.stringMatching(/^Snoozed until Thursday, June 18, 2026/),
    )
    expect(review.getByRole('button', { name: 'Unsnooze' })).toBeInTheDocument()

    await userEvent.click(review.getByRole('tab', { name: 'Dismissed 1' }))
    expect(review.getByRole('button', { name: 'Rename the flag' })).toBeInTheDocument()
    expect(review.getByText('Dismissed 3h ago')).toBeInTheDocument()
    expect(review.getByRole('button', { name: 'Restore' })).toBeInTheDocument()
  })

  it('explains empty tabs', async () => {
    render(<Harness launch={launchOptions} clock={clock} />)
    const review = await section('Review requested')

    await userEvent.click(review.getByRole('tab', { name: 'Snoozed' }))
    expect(review.getByText(/Nothing snoozed\. .* comes back to Review requested on its own/)).toBeInTheDocument()

    await userEvent.click(review.getByRole('tab', { name: 'Dismissed' }))
    expect(review.getByText(/Nothing dismissed\. .* stays here until you restore it/)).toBeInTheDocument()
  })

  it('moves between tabs with the arrow keys', async () => {
    render(<Harness launch={launchOptions} clock={clock} />)
    const review = await section('Review requested')
    const inbox = review.getByRole('tab', { name: 'Review requested 0' })

    expect(inbox).toHaveAttribute('tabIndex', '0')
    expect(review.getByRole('tab', { name: 'Snoozed' })).toHaveAttribute('tabIndex', '-1')

    inbox.focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(review.getByRole('tab', { name: 'Snoozed' })).toHaveFocus()
    expect(review.getByRole('tab', { name: 'Snoozed' })).toHaveAttribute('aria-selected', 'true')

    await userEvent.keyboard('{End}')
    expect(review.getByRole('tab', { name: 'Dismissed' })).toHaveFocus()

    await userEvent.keyboard('{ArrowRight}')
    expect(inbox).toHaveFocus()

    await userEvent.keyboard('{ArrowLeft}')
    expect(review.getByRole('tab', { name: 'Dismissed' })).toHaveFocus()

    await userEvent.keyboard('{Home}')
    expect(inbox).toHaveFocus()

    // Other keys are left alone.
    await userEvent.keyboard('{ArrowDown}')
    expect(inbox).toHaveFocus()
  })

  it('dismisses an item, and undoes it from the toast', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.dismissItem.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, dismissedAt: now.toISOString() }]) }),
    )
    platform.restoreItem.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))

    expect(platform.dismissItem).toHaveBeenCalledWith(1)
    expect(await screen.findByRole('status')).toHaveTextContent('Dismissed · Add the widget cache')
    expect(review.queryByRole('button', { name: 'Add the widget cache' })).not.toBeInTheDocument()
    expect(review.getByRole('tab', { name: 'Dismissed 1' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Undo' }))

    expect(platform.restoreItem).toHaveBeenCalledWith(1)
    expect(await review.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })

  it('snoozes for the default length in one click', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.snoozeItem.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, snoozedUntil: '2026-06-22T12:00:00Z' }]) }),
    )
    render(<Harness launch={launchOptions} clock={clock} snoozeDays={7} />)

    const review = await section('Review requested')
    const button = review.getByRole('button', { name: 'Snooze for 1 week' })
    expect(button).toHaveTextContent('1w')
    expect(button).toHaveAttribute('title', expect.stringMatching(/^Snooze for 1 week: back Mon, Jun 22$/))

    await userEvent.click(button)

    expect(platform.snoozeItem).toHaveBeenCalledWith(1, 7)
    expect(await screen.findByRole('status')).toHaveTextContent('Snoozed until Mon, Jun 22 · Add the widget cache')
    expect(review.getByRole('tab', { name: 'Snoozed 1' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Undo' })).toBeInTheDocument()
  })

  it('snoozes for another length from the menu, or opens the default', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.snoozeItem.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, snoozedUntil: '2026-06-16T12:00:00Z' }]) }),
    )
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Snooze for…' }))

    const menu = screen.getByRole('menu', { name: 'Snooze for…' })
    const choices = within(menu).getAllByRole('menuitem')
    expect(choices.map((c) => c.textContent.replace(/\s/g, ' '))).toEqual([
      '1 day, Tomorrow, 12:00 PM',
      '2 days, Wed, 12:00 PM',
      '3 days, Thu, 12:00 PM, Default',
      '1 week, Mon, Jun 22',
      '2 weeks, Mon, Jun 29',
      '30 days, Wed, Jul 15',
      'Snooze settings…',
    ])
    expect(choices[0]).toHaveFocus()

    await userEvent.click(within(menu).getByRole('menuitem', { name: /^1 day/ }))

    expect(platform.snoozeItem).toHaveBeenCalledWith(1, 1)
    expect(await screen.findByRole('status')).toHaveTextContent(/Snoozed until tomorrow, 12:00\sPM/)

    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.restoreItem.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }))
    await userEvent.click(await review.findByRole('button', { name: 'Snooze for…' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Snooze settings…' }))

    expect(openSnoozeSettings).toHaveBeenCalledOnce()
  })

  it('unsnoozes and restores from their tabs', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([snoozed, dismissed]) }))
    platform.restoreItem.mockResolvedValue(snapshot({ reviewRequested: list([{ ...snoozed, snoozedUntil: '' }]) }))
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('tab', { name: 'Snoozed 1' }))
    await userEvent.click(review.getByRole('button', { name: 'Unsnooze' }))

    expect(platform.restoreItem).toHaveBeenCalledWith(2)
    expect(await screen.findByRole('status')).toHaveTextContent('Unsnoozed · Bump the parser')
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
    expect(review.getByText(/Nothing snoozed/)).toBeInTheDocument()

    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([dismissed]) }))
    platform.restoreItem.mockResolvedValue(snapshot({ reviewRequested: list([{ ...dismissed, dismissedAt: '' }]) }))
    act(() => {
      ;(platform.onInboxUpdated.mock.calls[0]?.[0] as () => void)()
    })
    await userEvent.click(await review.findByRole('tab', { name: 'Dismissed 1' }))
    await userEvent.click(review.getByRole('button', { name: 'Restore' }))

    expect(platform.restoreItem).toHaveBeenLastCalledWith(3)
    expect(await screen.findByRole('status')).toHaveTextContent('Restored · Rename the flag')
  })

  it('pins an item to the top, marks it, and unpins it from its pin', async () => {
    const other = item({ id: 4, number: 10, title: 'Tidy the docs' })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh, other]) }))
    platform.pinItem.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...other, pinnedAt: now.toISOString() }, fresh]) }),
    )
    platform.unpinItem.mockResolvedValue(snapshot({ reviewRequested: list([fresh, other]) }))
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    const titles = () => review.getAllByRole('listitem').map((row) => within(row).getAllByRole('button')[0]?.title)
    const pins = review.getAllByRole('button', { name: 'Pin to top' })
    expect(pins).toHaveLength(2)
    expect(pins[1]?.parentElement).toHaveClass('opacity-0')

    await userEvent.click(pins[1] as HTMLElement)

    expect(platform.pinItem).toHaveBeenCalledWith(4)
    const unpin = await review.findByRole('button', { name: 'Unpin' })
    expect(titles()).toEqual(['Tidy the docs', 'Add the widget cache'])
    // The pin of a pinned item shows without hovering, in the accent color.
    expect(unpin.parentElement).not.toHaveClass('opacity-0')
    expect(unpin).toHaveClass('text-accent')
    expect(unpin).not.toHaveClass('text-muted')
    expect(unpin).toHaveAttribute('title', 'Pinned to the top of the list: click to unpin')
    expect(screen.getByRole('status')).toBeEmptyDOMElement()

    await userEvent.click(unpin)

    expect(platform.unpinItem).toHaveBeenCalledWith(4)
    await waitFor(() => {
      expect(review.queryByRole('button', { name: 'Unpin' })).not.toBeInTheDocument()
    })
    expect(titles()).toEqual(['Add the widget cache', 'Tidy the docs'])
  })

  it('shows the pin of a pinned item set aside, and unpins it there', async () => {
    const pinned = { ...snoozed, pinnedAt: '2026-06-14T12:00:00Z' }
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([pinned, dismissed]) }))
    platform.unpinItem.mockResolvedValue(snapshot({ reviewRequested: list([snoozed, dismissed]) }))
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('tab', { name: 'Snoozed 1' }))
    expect(review.getByRole('button', { name: 'Unsnooze' })).toBeInTheDocument()

    await userEvent.click(review.getByRole('button', { name: 'Unpin' }))

    expect(platform.unpinItem).toHaveBeenCalledWith(2)
    await waitFor(() => {
      expect(review.queryByRole('button', { name: 'Unpin' })).not.toBeInTheDocument()
    })

    // An item set aside without a pin offers none there.
    await userEvent.click(review.getByRole('tab', { name: 'Dismissed 1' }))
    expect(review.getByRole('button', { name: 'Restore' })).toBeInTheDocument()
    expect(review.queryByRole('button', { name: 'Pin to top' })).not.toBeInTheDocument()
  })

  it('says why a pin failed', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        reviewRequested: list([fresh, { ...dismissed, id: 5, dismissedAt: '', pinnedAt: now.toISOString() }]),
      }),
    )
    platform.pinItem.mockRejectedValue(new Error('database is locked'))
    platform.unpinItem.mockRejectedValue(new Error('database is locked'))
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Pin to top' }))
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not pin “Add the widget cache” · database is locked',
    )

    await userEvent.click(review.getByRole('button', { name: 'Unpin' }))
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not unpin “Rename the flag” · database is locked',
    )
  })

  it('says why a triage action failed', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.dismissItem.mockRejectedValue(new Error('item not found; refresh the inbox and try again'))
    render(<Harness launch={launchOptions} clock={clock} />)

    await userEvent.click((await section('Review requested')).getByRole('button', { name: 'Dismiss' }))

    const status = await screen.findByRole('status')
    expect(status).toHaveTextContent(
      'Could not dismiss “Add the widget cache” · item not found; refresh the inbox and try again',
    )
    expect(within(status).getByText(/Could not dismiss/)).toHaveClass('text-danger')

    await userEvent.click(within(status).getByRole('button', { name: 'Close' }))
    expect(status).toBeEmptyDOMElement()
  })

  it('reports an undo that failed', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.snoozeItem.mockRejectedValue(new Error('database is locked'))
    platform.dismissItem.mockResolvedValue(snapshot({ reviewRequested: list([{ ...fresh, dismissedAt: '' }]) }))
    platform.restoreItem.mockRejectedValue(new Error('database is locked'))
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Snooze for 3 days' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Could not snooze')

    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Undo' }))

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not undo the change to “Add the widget cache” · database is locked',
    )
  })

  it('says a snooze without a stored end was snoozed', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.snoozeItem.mockResolvedValue(snapshot())
    render(<Harness launch={launchOptions} clock={clock} />)

    await userEvent.click((await section('Review requested')).getByRole('button', { name: 'Snooze for 3 days' }))

    expect(await screen.findByRole('status')).toHaveTextContent(/^Snoozed · Add the widget cache/)
  })

  it('refreshes one item from GitHub in place', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh, snoozed]) }))
    platform.refreshItem.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, title: 'Add the widget cache, take two' }, snoozed]) }),
    )
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    const button = review.getByRole('button', { name: 'Refresh from GitHub' })
    expect(button).toHaveAttribute('title', 'Refresh this item from GitHub, without refreshing the rest of the inbox')

    await userEvent.click(button)

    expect(platform.refreshItem).toHaveBeenCalledWith(1)
    expect(platform.refreshInbox).not.toHaveBeenCalled()
    expect(await review.findByRole('button', { name: 'Add the widget cache, take two' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })

  it('says why a refreshed item left the inbox', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.refreshItem.mockResolvedValue(snapshot())
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Refresh from GitHub' }))

    expect(await screen.findByRole('status')).toHaveTextContent('No longer open on GitHub · Add the widget cache')
    expect(review.queryByRole('button', { name: 'Add the widget cache' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
  })

  it('holds the row controls while a refresh runs, and says why it failed', async () => {
    let fail: (err: Error) => void = () => undefined
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.refreshItem.mockReturnValue(
      new Promise((_, reject) => {
        fail = reject
      }),
    )
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    const button = review.getByRole('button', { name: 'Refresh from GitHub' })
    const pin = review.getByRole('button', { name: 'Pin to top' })
    expect(button.parentElement).toHaveClass('opacity-0')
    expect(pin.parentElement).toHaveClass('opacity-0')

    await userEvent.click(button)

    expect(button).toBeDisabled()
    expect(button.parentElement).toHaveClass('opacity-100')
    expect(pin.parentElement).toHaveClass('opacity-100')
    expect(button.querySelector('svg')).toHaveClass('motion-safe:animate-spin')

    act(() => {
      fail(new Error('the GitHub GraphQL API returned errors: timeout'))
    })

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not refresh “Add the widget cache” · the GitHub GraphQL API returned errors: timeout',
    )
    expect(button).toBeEnabled()
    expect(button.parentElement).toHaveClass('opacity-0')
    expect(button.querySelector('svg')).not.toHaveClass('motion-safe:animate-spin')
  })

  it('brings an item back when its snooze ends, marked as back', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    let current = now
    const ticking = () => current

    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, snoozedUntil: '2026-06-15T12:00:30Z' }]) }),
    )
    render(<Harness launch={launchOptions} clock={ticking} />)

    const review = await section('Review requested')
    expect(review.getByRole('tab', { name: 'Snoozed 1' })).toBeInTheDocument()

    current = new Date(now.getTime() + 60_000)
    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    expect(review.getByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(review.getByText('Back from snooze')).toBeInTheDocument()
    expect(review.getByRole('tab', { name: 'Snoozed' })).toBeInTheDocument()
  })

  it('says what new activity brought an item back early', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({
        reviewRequested: list([
          {
            ...fresh,
            snoozedUntil: '2026-06-15T11:00:00Z',
            wokeBy: { commits: 2, forcePushed: false, review: 'approved', comments: 0 },
          },
        ]),
      }),
    )
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    const chip = review.getByText('Back early: approved, 2 new commits')

    expect(chip.parentElement).toHaveAttribute(
      'title',
      expect.stringMatching(
        /^Back early, Monday, June 15, 2026 at 11:00\sAM: approved, 2 new commits since you snoozed it$/,
      ),
    )
    expect(review.queryByText('Back from snooze')).not.toBeInTheDocument()
  })

  it('never lets an older answer replace a newer one', async () => {
    let finishDismiss: (value: unknown) => void = () => undefined
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.dismissItem.mockReturnValue(
      new Promise((resolve) => {
        finishDismiss = resolve
      }),
    )
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))

    // The shell's update after the dismissal is read first, and already has it.
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, title: 'Read later', dismissedAt: now.toISOString() }]) }),
    )
    act(() => {
      ;(platform.onInboxUpdated.mock.calls[0]?.[0] as () => void)()
    })
    expect(await review.findByRole('tab', { name: 'Dismissed 1' })).toBeInTheDocument()

    // The dismissal's own, older answer arrives last and is not shown.
    await act(async () => {
      finishDismiss(snapshot({ reviewRequested: list([fresh]) }))
      await Promise.resolve()
    })

    expect(review.getByRole('tab', { name: 'Dismissed 1' })).toBeInTheDocument()
    expect(review.queryByRole('button', { name: 'Add the widget cache' })).not.toBeInTheDocument()
    expect(await screen.findByRole('status')).toHaveTextContent('Dismissed · Add the widget cache')
  })
})

describe('InboxView at nil', () => {
  const refreshed = '2026-06-15T11:55:00Z'
  const last = item({ id: 1, title: 'Add the widget cache' })

  // party finds the party, when there is one.
  const party = () => screen.queryByRole('region', { name: 'inbox == nil' })

  it('throws a party over the lists once a refresh finds every list empty', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: refreshed }))
    render(<Harness launch={launchOptions} clock={clock} />)

    expect(await screen.findByRole('region', { name: 'inbox == nil' })).toBeInTheDocument()
    // Opening the page at nil sprinkles; the achievement is for emptying it.
    expect(screen.queryByText('Achievement unlocked')).not.toBeInTheDocument()
    // The lists stay, for what was snoozed or dismissed.
    expect(await section('Review requested')).toBeTruthy()
    expect(screen.getAllByText('Nothing here.')).toHaveLength(3)
  })

  it('throws no party before the first refresh, after an error, or while anything is left', async () => {
    platform.getInboxSnapshot.mockResolvedValueOnce(snapshot())
    const { unmount } = render(<Harness launch={launchOptions} clock={clock} />)
    expect(await screen.findAllByText('Nothing here.')).toHaveLength(3)
    expect(party()).not.toBeInTheDocument()
    unmount()

    platform.getInboxSnapshot.mockResolvedValueOnce(snapshot({ lastRefreshedAt: refreshed, lastError: 'rate limited' }))
    const view = render(<Harness launch={launchOptions} clock={clock} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('rate limited')
    expect(party()).not.toBeInTheDocument()
    view.unmount()

    platform.getInboxSnapshot.mockResolvedValueOnce(snapshot({ lastRefreshedAt: refreshed, assigned: list([last]) }))
    render(<Harness launch={launchOptions} clock={clock} />)
    expect(await screen.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(party()).not.toBeInTheDocument()
  })

  it('unlocks the achievement when the last item is dismissed, and leaves with an undo', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: refreshed, reviewRequested: list([last]) }))
    platform.dismissItem.mockResolvedValue(
      snapshot({ lastRefreshedAt: refreshed, reviewRequested: list([{ ...last, dismissedAt: now.toISOString() }]) }),
    )
    platform.restoreItem.mockResolvedValue(snapshot({ lastRefreshedAt: refreshed, reviewRequested: list([last]) }))
    render(<Harness launch={launchOptions} clock={clock} />)

    const review = await section('Review requested')
    expect(party()).not.toBeInTheDocument()

    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))

    expect(await screen.findByText('Achievement unlocked')).toBeInTheDocument()
    expect(party()).toHaveTextContent('Inbox zero is for amateurs. You hit nil.')
    expect(screen.getAllByTestId('confetti').length).toBeGreaterThan(0)
    expect(within(screen.getByRole('figure', { name: 'Terminal' })).getByText('dismissed')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Undo' }))

    expect(await review.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(party()).not.toBeInTheDocument()
  })
})

describe('InboxView reveal', () => {
  const fresh = item({ id: 1, title: 'Add the widget cache' })
  const snoozed = item({ id: 2, number: 8, title: 'Bump the parser', snoozedUntil: '2026-06-18T12:00:00Z' })
  const assigned = item({ id: 5, number: 21, title: 'Crash on start' })
  const scrolled = vi.fn()

  beforeEach(() => {
    // jsdom does not scroll; the row's call is what matters.
    Element.prototype.scrollIntoView = scrolled
  })

  afterEach(() => {
    delete (Element.prototype as Partial<Element>).scrollIntoView
  })

  // rowOf returns the row of the item titled title in a list.
  function rowOf(list: Awaited<ReturnType<typeof section>>, title: string): HTMLElement {
    const row = list.getByRole('button', { name: title }).closest('li')

    if (row === null) {
      throw new Error(`no row shows ${title}`)
    }

    return row
  }

  it('opens a collapsed list at the tab that holds the item, and highlights its row for a moment', async () => {
    platform.getCollapsedSections.mockResolvedValue(['reviewRequested'])
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh, snoozed]) }))
    // Fake time runs along with real time, so the queries' own waits still pass.
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'], shouldAdvanceTime: true })

    render(<Harness launch={launchOptions} clock={clock} reveal={{ id: 2, seq: 1 }} />)

    const review = await section('Review requested')
    await waitFor(() => {
      expect(review.getByRole('button', { name: 'Collapse Review requested' })).toHaveAttribute('aria-expanded', 'true')
    })
    expect(platform.setSectionCollapsed).toHaveBeenCalledWith('reviewRequested', false)
    expect(review.getByRole('tab', { name: 'Snoozed 1' })).toHaveAttribute('aria-selected', 'true')

    const row = rowOf(review, 'Bump the parser')
    expect(row).toHaveAttribute('data-highlighted', 'true')
    await waitFor(() => {
      expect(scrolled).toHaveBeenCalledWith(expect.objectContaining({ block: 'center' }))
    })

    act(() => {
      vi.advanceTimersByTime(2400)
    })

    expect(row).not.toHaveAttribute('data-highlighted')
  })

  it('shows an item in the first list that holds it, and leaves a list collapsed afterwards alone', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([fresh]), assigned: list([assigned, fresh]) }),
    )

    const { rerender } = render(<Harness launch={launchOptions} clock={clock} reveal={{ id: 1, seq: 1 }} />)

    const review = await section('Review requested')
    await waitFor(() => {
      expect(rowOf(review, 'Add the widget cache')).toHaveAttribute('data-highlighted', 'true')
    })
    expect(rowOf(await section('Assigned'), 'Add the widget cache')).not.toHaveAttribute('data-highlighted')

    // Collapsing the list after the reveal keeps it collapsed.
    await userEvent.click(review.getByRole('button', { name: 'Collapse Review requested' }))
    rerender(<Harness launch={launchOptions} clock={clock} reveal={{ id: 1, seq: 1 }} />)
    expect(review.getByRole('button', { name: 'Expand Review requested' })).toBeInTheDocument()

    // Asking again opens it again.
    rerender(<Harness launch={launchOptions} clock={clock} reveal={{ id: 1, seq: 2 }} />)
    await waitFor(() => {
      expect(review.getByRole('button', { name: 'Collapse Review requested' })).toBeInTheDocument()
    })
  })

  it('ignores an item that is in no list', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))

    render(<Harness launch={launchOptions} clock={clock} reveal={{ id: 99, seq: 1 }} />)

    const review = await section('Review requested')
    expect(rowOf(review, 'Add the widget cache')).not.toHaveAttribute('data-highlighted')
    expect(scrolled).not.toHaveBeenCalled()
  })
})
