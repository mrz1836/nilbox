import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useState } from 'react'

import { filterInbox } from '../lib/inboxFilter'
import { offersProjects } from '../lib/projects'
import { useCollapsedSections } from '../lib/useCollapsedSections'
import { useInbox } from '../lib/useInbox'
import { useInboxOrder } from '../lib/useInboxOrder'
import { useReviewDrafts } from '../lib/reviewDrafts'
import { ClockContext } from '../lib/useNow'
import { useTriage } from '../lib/useTriage'
import type { AfterMerge, InboxSnapshot, ListKey, RepoRules, ReviewResult, Settings } from '../platform'
import { clockAt, FIXED_NOW, fixedNow, fromNow } from '../test/clock'
import {
  afterMerge,
  agents,
  details,
  item,
  itemResolution,
  list,
  profile,
  project,
  prompts,
  review,
  reviewResult,
  settings,
  snapshot,
} from '../test/fixtures'
import { emit, listening, platform } from '../test/platform'
import { InboxView, type LaunchOptions, type Reveal } from './InboxView'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const launchOptions: LaunchOptions = {
  agents: agents(),
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
  onSettingsSaved: () => undefined,
}

const resolutions = new Map([[1, itemResolution()]])

const openSnoozeSettings = vi.fn()
const dismissProjects = vi.fn()
const filterBy = vi.fn<(key: string, value: string) => void>()
const clearFilter = vi.fn()
const manageReplies = vi.fn()
const release = vi.fn<(offer: AfterMerge) => void>()
const settingsSaved = vi.fn<(settings: Settings) => void>()
const repoRuleSettings = vi.fn()

interface HarnessProps {
  launch: LaunchOptions | null
  // clock reads the time the window's clock moves on to.
  clock: () => Date
  snoozeDays?: number
  reveal?: Reveal | null
  // query filters the inbox, as the toolbar's field does.
  query?: string
  // reviews turns the review sheet on, as App does, and releases the offer of a release
  // after a merge, on with it unless turned off.
  reviews?: boolean
  releases?: boolean
  // repoRules are the pinned and muted repositories the settings hold.
  repoRules?: RepoRules
}

// Harness renders the view over the real inbox state, triage, and collapsed lists, the
// way App does, on a clock that reads its time from clock.
function Harness({ clock, ...props }: HarnessProps) {
  const [shared] = useState(() => clockAt(clock))

  return (
    <ClockContext value={shared}>
      <Inbox {...props} />
    </ClockContext>
  )
}

function Inbox({
  launch,
  snoozeDays = 3,
  reveal = null,
  query = '',
  reviews = false,
  releases = true,
  repoRules = { pinned: [], muted: [] },
}: Omit<HarnessProps, 'clock'>) {
  const inbox = useInbox()
  const drafts = useReviewDrafts()
  const triaged = useTriage(inbox.snapshot)
  const filter = triaged === null ? null : filterInbox(triaged, query, inbox.snapshot?.account.login ?? '')
  const sections = useCollapsedSections()
  const inboxOrder = useInboxOrder()
  // tipDismissed stands in for the stored setting, as App keeps it.
  const [tipDismissed, setTipDismissed] = useState(false)

  return (
    <InboxView
      inbox={inbox}
      triaged={triaged}
      launch={launch}
      resolutions={resolutions}
      snoozeDays={snoozeDays}
      onSnoozeSettings={openSnoozeSettings}
      collapsed={sections.collapsed}
      onCollapse={sections.setCollapsed}
      reveal={reveal}
      offerProjects={offersProjects(inbox.snapshot, settings({ projectsTipDismissed: tipDismissed }))}
      onDismissProjects={() => {
        dismissProjects()
        setTipDismissed(true)
      }}
      inboxOrder={inboxOrder}
      filter={filter}
      onFilterBy={filterBy}
      onClearFilter={clearFilter}
      {...(reviews ? { drafts, onManageReplies: manageReplies } : {})}
      {...(reviews && releases ? { onRelease: release } : {})}
      repoRules={repoRules}
      onSettingsSaved={settingsSaved}
      onRepoRuleSettings={repoRuleSettings}
    />
  )
}

// section waits for one inbox list and returns the queries scoped to it.
async function section(title: string) {
  return within(await screen.findByRole('region', { name: title }))
}

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

    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    expect(review.getByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(review.getByRole('listitem')).toHaveTextContent(/acme-org\/example-repo #7 · octo-user ·/)
    expect(review.getByText('3h')).toHaveAttribute('dateTime', '2026-06-15T09:00:00Z')
    expect(review.getByRole('img', { name: 'Pull request' })).toBeInTheDocument()

    const authored = await section('Authored')
    expect(authored.getByRole('button', { name: 'Fix the parser' })).toBeInTheDocument()
    expect(authored.getByRole('listitem')).toHaveTextContent(/acme-org\/other-repo #12/)
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

    render(<Harness launch={launchOptions} clock={fixedNow} />)

    expect((await section('Review requested')).getByText('2 of 250')).toBeInTheDocument()
    expect((await section('Authored')).getByText('1')).toBeInTheDocument()
    expect((await section('Assigned')).getByText('0')).toBeInTheDocument()
    expect((await section('Assigned')).getByText('Nothing here.')).toBeInTheDocument()
  })

  it('says when the inbox has not been refreshed yet', async () => {
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    expect((await section('Authored')).getByText('Nothing here.')).toBeInTheDocument()
    expect(screen.getByText('Not refreshed yet')).toBeInTheDocument()
  })

  it('shows launch controls only once the launch options are loaded', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))

    const { rerender } = render(<Harness launch={null} clock={fixedNow} />)

    expect(await screen.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()

    rerender(<Harness launch={launchOptions} clock={fixedNow} />)

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()
  })

  it('opens an item in the browser', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([item()]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
  })

  it('refreshes on click and shows the new snapshot', async () => {
    let finish: (value: InboxSnapshot) => void = () => undefined
    platform.refreshInbox.mockReturnValue(
      new Promise((resolve) => {
        finish = resolve
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Refresh' }))
    expect(screen.getByRole('button', { name: 'Refreshing…' })).toBeDisabled()

    await act(async () => {
      finish(
        snapshot({
          authored: list([item({ title: 'Fresh from GitHub' })]),
          lastRefreshedAt: FIXED_NOW.toISOString(),
        }),
      )
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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('run gh auth refresh -s repo,read:org')
    expect(screen.getByText('the token reports no scopes, so they could not be verified')).toBeInTheDocument()
  })

  it('closes a refresh error, and shows it again when a refresh fails again', async () => {
    const message = 'read the item details: the GitHub API returned HTTP 502'
    platform.refreshInbox.mockRejectedValue(new Error(message))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Refresh' }))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent(message)

    await userEvent.click(within(alert).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
  })

  it('keeps a closed stored error closed while a refresh runs, and shows it again once it clears and returns', async () => {
    const message = 'read the item details: the GitHub API returned HTTP 502'
    let finish: (value: InboxSnapshot) => void = () => undefined
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z', lastError: message }),
    )
    platform.refreshInbox.mockReturnValue(
      new Promise<InboxSnapshot>((resolve) => {
        finish = resolve
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(within(await screen.findByRole('alert')).getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    // A refresh the user starts doesn't bring the closed error back while it runs.
    await userEvent.click(screen.getByRole('button', { name: 'Refresh' }))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    // It succeeds, the error clears, and a later background refresh fails the same way.
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z' }))
    act(() => {
      finish(snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z' }))
      emit('inbox:updated')
    })
    await waitFor(() => {
      expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(2)
    })

    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z', lastError: message }),
    )
    act(() => {
      emit('inbox:updated')
    })
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
  })

  it('offers no close on an inbox that could not be loaded, which has nothing else to show', async () => {
    platform.getInboxSnapshot.mockRejectedValue(new Error('open the database: locked'))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('open the database: locked')
    expect(within(alert).queryByRole('button', { name: 'Close' })).not.toBeInTheDocument()
  })

  it('shows an inbox that could not be loaded', async () => {
    platform.getInboxSnapshot.mockRejectedValue(new Error('open the database: locked'))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    expect(await screen.findByRole('alert')).toHaveTextContent('open the database: locked')
    expect(screen.queryByText('Loading the inbox…')).not.toBeInTheDocument()
  })

  it('reloads when the inbox updates and stops listening on unmount', async () => {
    platform.getInboxSnapshot
      .mockResolvedValueOnce(snapshot())
      .mockResolvedValueOnce(snapshot({ assigned: list([item({ title: 'Updated in the background' })]) }))

    const { unmount } = render(<Harness launch={launchOptions} clock={fixedNow} />)

    expect(await screen.findAllByText('Nothing here.')).toHaveLength(3)
    expect(platform.on).toHaveBeenCalledExactlyOnceWith('inbox:updated', expect.any(Function))

    act(() => {
      emit('inbox:updated')
    })

    expect(await screen.findByRole('button', { name: 'Updated in the background' })).toBeInTheDocument()
    expect(platform.getInboxSnapshot).toHaveBeenCalledTimes(2)

    expect(listening('inbox:updated')).toBe(1)
    unmount()
    expect(listening('inbox:updated')).toBe(0)
  })

  it('brings the ages up to date every minute', async () => {
    // Only the minute ticker is faked, so the loads and the queries' own waits still run.
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] })
    let current = FIXED_NOW
    const ticking = () => current

    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z' }))
    render(<Harness launch={launchOptions} clock={ticking} />)

    expect(await screen.findByText('5m ago')).toBeInTheDocument()

    // The clock starts its minute interval once something subscribes, in an effect that
    // can land after the text shows on a loaded machine; advancing before then would move
    // nothing. So the interval is waited for, and the tick's render after it.
    await vi.waitFor(() => {
      expect(vi.getTimerCount()).toBeGreaterThan(0)
    })

    current = fromNow(60_000)
    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    expect(await screen.findByText('6m ago')).toBeInTheDocument()
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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const list = await section('Authored')
    expect(list.getByRole('button', { name: 'Expand Authored' })).toHaveAttribute('aria-expanded', 'false')
    expect(list.queryByRole('button', { name: 'Fix the parser' })).not.toBeInTheDocument()
    expect((await section('Assigned')).getByRole('button', { name: 'Expand Assigned' })).toBeInTheDocument()
    expect((await section('Review requested')).getByRole('tabpanel')).toBeVisible()
    expect(platform.setSectionCollapsed).not.toHaveBeenCalled()
  })

  it('waits for the collapsed lists before showing any, so none flashes open', async () => {
    let resolve: (sections: ListKey[]) => void = () => undefined
    platform.getInboxSnapshot.mockResolvedValue(authored)
    platform.getCollapsedSections.mockReturnValue(
      new Promise<ListKey[]>((r) => {
        resolve = r
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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

  it('sets a muted repository’s items apart in the Muted tab, out of the count', async () => {
    const quiet = item({ id: 4, number: 11, title: 'Quiet change', repo: 'acme-org/quiet', repoRule: 'muted' })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh, quiet], 2) }))
    render(<Harness launch={launchOptions} clock={fixedNow} repoRules={{ pinned: [], muted: ['acme-org/quiet'] }} />)

    const review = await section('Review requested')
    expect(review.getByRole('tab', { name: 'Review requested 1' })).toHaveAttribute('aria-selected', 'true')
    expect(review.queryByRole('button', { name: 'Quiet change' })).not.toBeInTheDocument()

    await userEvent.click(review.getByRole('tab', { name: 'Muted 1' }))
    expect(review.getByRole('button', { name: 'Quiet change' })).toBeInTheDocument()

    platform.setRepoRule.mockResolvedValue(settings({ repoRules: { pinned: [], muted: [] } }))
    await userEvent.click(review.getByRole('button', { name: 'Unmute acme-org/quiet' }))

    expect(platform.setRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org/quiet', '')
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Unmuted acme-org/quiet · Its items are back in the lists.',
    )
    expect(settingsSaved).toHaveBeenCalledWith(settings({ repoRules: { pinned: [], muted: [] } }))

    // The inbox shows the change once the backend has built it again under the new rules.
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([fresh, { ...quiet, repoRule: '' }]) }),
    )
    act(() => {
      emit('inbox:updated')
    })
    expect(await review.findByRole('tab', { name: 'Review requested 2' })).toBeInTheDocument()
    expect(review.getByRole('tab', { name: 'Muted' })).toBeInTheDocument()
  })

  it('mutes a card’s org from its menu, and undoes it from the toast', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.setRepoRule.mockResolvedValue(settings({ repoRules: { pinned: [], muted: ['acme-org'] } }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Inbox actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pin or mute repository' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Mute everything in acme-org' }))

    expect(platform.setRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org', 'muted')
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Muted acme-org · Its items wait in each list’s Muted tab.',
    )

    platform.setRepoRule.mockResolvedValue(settings())
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }))

    expect(platform.setRepoRule).toHaveBeenLastCalledWith('acme-org', '')
    await waitFor(() => {
      expect(settingsSaved).toHaveBeenLastCalledWith(settings())
    })
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })

  it('says why a rule could not be changed', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.setRepoRule.mockRejectedValue(new Error('database is locked'))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Inbox actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pin or mute repository' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pin acme-org/example-repo' }))

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not change the rule for acme-org/example-repo · database is locked',
    )
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
  })

  it('opens the pinned and muted repositories in Settings from a card’s menu', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Inbox actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pin or mute repository' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pinned and muted…' }))

    expect(repoRuleSettings).toHaveBeenCalledOnce()
  })

  it('shows each list as tabs, with what was set aside counted apart', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([fresh, snoozed, dismissed], 40), assigned: list([fresh]) }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)
    const review = await section('Review requested')

    await userEvent.click(review.getByRole('tab', { name: 'Snoozed' }))
    expect(review.getByText(/Nothing snoozed\. .* comes back to Review requested on its own/)).toBeInTheDocument()

    await userEvent.click(review.getByRole('tab', { name: 'Dismissed' }))
    expect(review.getByText(/Nothing dismissed\. .* stays here until you restore it/)).toBeInTheDocument()
  })

  it('moves between tabs with the arrow keys', async () => {
    render(<Harness launch={launchOptions} clock={fixedNow} />)
    const review = await section('Review requested')
    const inbox = review.getByRole('tab', { name: 'Review requested 0' })

    expect(inbox).toHaveAttribute('tabIndex', '0')
    expect(review.getByRole('tab', { name: 'Snoozed' })).toHaveAttribute('tabIndex', '-1')

    inbox.focus()
    await userEvent.keyboard('{ArrowRight}')
    expect(review.getByRole('tab', { name: 'Snoozed' })).toHaveFocus()
    expect(review.getByRole('tab', { name: 'Snoozed' })).toHaveAttribute('aria-selected', 'true')

    await userEvent.keyboard('{End}')
    expect(review.getByRole('tab', { name: 'Muted' })).toHaveFocus()

    await userEvent.keyboard('{ArrowRight}')
    expect(inbox).toHaveFocus()

    await userEvent.keyboard('{ArrowLeft}')
    expect(review.getByRole('tab', { name: 'Muted' })).toHaveFocus()

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
      snapshot({ reviewRequested: list([{ ...fresh, dismissedAt: FIXED_NOW.toISOString() }]) }),
    )
    platform.restoreItem.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} snoozeDays={7} />)

    const review = await section('Review requested')
    const button = review.getByRole('button', { name: 'Snooze for 1 week' })
    expect(button).toHaveTextContent('1w')
    // The tooltip works out when the snooze would end once the pointer reaches it.
    expect(button).toHaveAttribute('title', 'Snooze for 1 week')
    await userEvent.hover(button)
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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
      emit('inbox:updated')
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
      snapshot({ reviewRequested: list([{ ...other, pinnedAt: FIXED_NOW.toISOString() }, fresh]) }),
    )
    platform.unpinItem.mockResolvedValue(snapshot({ reviewRequested: list([fresh, other]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    const titles = () => review.getAllByRole('listitem').map((row) => within(row).getAllByRole('button')[0]?.title)
    const pins = review.getAllByRole('button', { name: 'Pin to top' })
    expect(pins).toHaveLength(2)
    expect(pins[1]?.parentElement).toHaveClass('opacity-0')

    await userEvent.click(pins[1] as HTMLElement)

    expect(platform.pinItem).toHaveBeenCalledWith(4)
    const unpin = await review.findByRole('button', { name: 'Unpin' })
    // The pin shows before the stored order lands, so the order is waited for too.
    await waitFor(() => {
      expect(titles()).toEqual(['Tidy the docs', 'Add the widget cache'])
    })
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
    // The pin goes before the stored order lands, so the order is waited for too.
    await waitFor(() => {
      expect(titles()).toEqual(['Add the widget cache', 'Tidy the docs'])
    })
  })

  it('shows the pin of a pinned item set aside, and unpins it there', async () => {
    const pinned = { ...snoozed, pinnedAt: '2026-06-14T12:00:00Z' }
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([pinned, dismissed]) }))
    platform.unpinItem.mockResolvedValue(snapshot({ reviewRequested: list([snoozed, dismissed]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
        reviewRequested: list([fresh, { ...dismissed, id: 5, dismissedAt: '', pinnedAt: FIXED_NOW.toISOString() }]),
      }),
    )
    platform.pinItem.mockRejectedValue(new Error('database is locked'))
    platform.unpinItem.mockRejectedValue(new Error('database is locked'))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Pin to top' }))
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not pin “Add the widget cache” · database is locked',
    )

    // The refused pin goes, leaving the pin the item already had.
    await waitFor(() => {
      expect(review.getAllByRole('button', { name: 'Unpin' })).toHaveLength(1)
    })
    await userEvent.click(review.getByRole('button', { name: 'Unpin' }))
    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not unpin “Rename the flag” · database is locked',
    )
  })

  it('shows a pin at once, and puts the item back with the reason when it is refused', async () => {
    let refuse: (err: Error) => void = () => undefined
    const other = item({ id: 4, number: 10, title: 'Tidy the docs', updatedAt: '2026-06-15T08:00:00Z' })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh, other]) }))
    platform.pinItem.mockReturnValue(
      new Promise((_, reject) => {
        refuse = reject
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    const titles = () => review.getAllByRole('listitem').map((row) => within(row).getAllByRole('button')[0]?.title)
    await userEvent.click(review.getAllByRole('button', { name: 'Pin to top' })[1] as HTMLElement)

    // Pinned before the backend answers: at the top, with its pin filled.
    expect(review.getByRole('button', { name: 'Unpin' })).toBeInTheDocument()
    expect(titles()).toEqual(['Tidy the docs', 'Add the widget cache'])

    await act(async () => {
      refuse(new Error('database is locked'))
      await Promise.resolve()
    })

    expect(await screen.findByRole('status')).toHaveTextContent('Could not pin “Tidy the docs” · database is locked')
    await waitFor(() => {
      expect(review.queryByRole('button', { name: 'Unpin' })).not.toBeInTheDocument()
    })
    // The pin goes before the stored order lands, so the order is waited for too.
    await waitFor(() => {
      expect(titles()).toEqual(['Add the widget cache', 'Tidy the docs'])
    })
  })

  it('sets a dismissed item aside at once, before the backend answers', async () => {
    let answer: (value: InboxSnapshot) => void = () => undefined
    const dismissedNow = snapshot({ reviewRequested: list([{ ...fresh, dismissedAt: FIXED_NOW.toISOString() }]) })
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.dismissItem.mockReturnValue(
      new Promise((resolve) => {
        answer = resolve
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))

    expect(review.getByRole('tab', { name: 'Dismissed 1' })).toBeInTheDocument()
    expect(review.queryByRole('button', { name: 'Add the widget cache' })).not.toBeInTheDocument()

    await act(async () => {
      answer(dismissedNow)
      await Promise.resolve()
    })

    expect(await screen.findByRole('status')).toHaveTextContent('Dismissed · Add the widget cache')
  })

  it('says why a triage action failed', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.dismissItem.mockRejectedValue(new Error('item not found; refresh the inbox and try again'))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Snooze for 3 days' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Could not snooze')

    // The refused snooze puts the item back in the list.
    await userEvent.click(await review.findByRole('button', { name: 'Dismiss' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Undo' }))

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not undo the change to “Add the widget cache” · database is locked',
    )
  })

  it('says a snooze without a stored end was snoozed', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.snoozeItem.mockResolvedValue(snapshot())
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click((await section('Review requested')).getByRole('button', { name: 'Snooze for 3 days' }))

    expect(await screen.findByRole('status')).toHaveTextContent(/^Snoozed · Add the widget cache/)
  })

  it('refreshes one item from GitHub in place', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh, snoozed]) }))
    platform.refreshItem.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, title: 'Add the widget cache, take two' }, snoozed]) }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    let current = FIXED_NOW
    const ticking = () => current

    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, snoozedUntil: '2026-06-15T12:00:30Z' }]) }),
    )
    render(<Harness launch={launchOptions} clock={ticking} />)

    const review = await section('Review requested')
    expect(review.getByRole('tab', { name: 'Snoozed 1' })).toBeInTheDocument()

    current = fromNow(60_000)
    act(() => {
      vi.advanceTimersByTime(60_000)
    })

    // The tick re-renders the list; on a loaded machine that can land after the act, so
    // the item is waited for (only the interval is faked, so waiting uses real time).
    expect(await review.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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
    let finishDismiss: (value: InboxSnapshot) => void = () => undefined
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))
    platform.dismissItem.mockReturnValue(
      new Promise((resolve) => {
        finishDismiss = resolve
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await userEvent.click(review.getByRole('button', { name: 'Dismiss' }))

    // The shell's update after the dismissal is read first, and already has it.
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...fresh, title: 'Read later', dismissedAt: FIXED_NOW.toISOString() }]) }),
    )
    act(() => {
      emit('inbox:updated')
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
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    expect(await screen.findByRole('region', { name: 'inbox == nil' })).toBeInTheDocument()
    // Opening the page at nil sprinkles; the achievement is for emptying it.
    expect(screen.queryByText('Achievement unlocked')).not.toBeInTheDocument()
    // The lists stay, for what was snoozed or dismissed.
    expect(await section('Review requested')).toBeTruthy()
    expect(screen.getAllByText('Nothing here.')).toHaveLength(3)
  })

  it('throws no party before the first refresh, after an error, or while anything is left', async () => {
    platform.getInboxSnapshot.mockResolvedValueOnce(snapshot())
    const { unmount } = render(<Harness launch={launchOptions} clock={fixedNow} />)
    expect(await screen.findAllByText('Nothing here.')).toHaveLength(3)
    expect(party()).not.toBeInTheDocument()
    unmount()

    platform.getInboxSnapshot.mockResolvedValueOnce(snapshot({ lastRefreshedAt: refreshed, lastError: 'rate limited' }))
    const view = render(<Harness launch={launchOptions} clock={fixedNow} />)
    expect(await screen.findByRole('alert')).toHaveTextContent('rate limited')
    expect(party()).not.toBeInTheDocument()
    view.unmount()

    platform.getInboxSnapshot.mockResolvedValueOnce(snapshot({ lastRefreshedAt: refreshed, assigned: list([last]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)
    expect(await screen.findByRole('button', { name: 'Add the widget cache' })).toBeInTheDocument()
    expect(party()).not.toBeInTheDocument()
  })

  it('unlocks the achievement when the last item is dismissed, and leaves with an undo', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: refreshed, reviewRequested: list([last]) }))
    platform.dismissItem.mockResolvedValue(
      snapshot({
        lastRefreshedAt: refreshed,
        reviewRequested: list([{ ...last, dismissedAt: FIXED_NOW.toISOString() }]),
      }),
    )
    platform.restoreItem.mockResolvedValue(snapshot({ lastRefreshedAt: refreshed, reviewRequested: list([last]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

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

    render(<Harness launch={launchOptions} clock={fixedNow} reveal={{ id: 2, seq: 1 }} />)

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

    const { rerender } = render(<Harness launch={launchOptions} clock={fixedNow} reveal={{ id: 1, seq: 1 }} />)

    const review = await section('Review requested')
    await waitFor(() => {
      expect(rowOf(review, 'Add the widget cache')).toHaveAttribute('data-highlighted', 'true')
    })
    expect(rowOf(await section('Assigned'), 'Add the widget cache')).not.toHaveAttribute('data-highlighted')

    // Collapsing the list after the reveal keeps it collapsed.
    await userEvent.click(review.getByRole('button', { name: 'Collapse Review requested' }))
    rerender(<Harness launch={launchOptions} clock={fixedNow} reveal={{ id: 1, seq: 1 }} />)
    expect(review.getByRole('button', { name: 'Expand Review requested' })).toBeInTheDocument()

    // Asking again opens it again.
    rerender(<Harness launch={launchOptions} clock={fixedNow} reveal={{ id: 1, seq: 2 }} />)
    await waitFor(() => {
      expect(review.getByRole('button', { name: 'Collapse Review requested' })).toBeInTheDocument()
    })
  })

  it('ignores an item that is in no list', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([fresh]) }))

    render(<Harness launch={launchOptions} clock={fixedNow} reveal={{ id: 99, seq: 1 }} />)

    const review = await section('Review requested')
    expect(rowOf(review, 'Add the widget cache')).not.toHaveAttribute('data-highlighted')
    expect(scrolled).not.toHaveBeenCalled()
  })
})

describe('InboxView’s projects tip', () => {
  // needsScope is an inbox with an issue whose token can't read projects.
  const needsScope = () =>
    snapshot({
      assigned: list([item({ id: 2, kind: 'issue', title: 'Fix the login page', details: details() })]),
      projectAccess: 'needs_scope',
    })

  it('offers to show issue projects while the token cannot read them', async () => {
    platform.getInboxSnapshot.mockResolvedValue(needsScope())
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    expect(
      await screen.findByRole('region', { name: 'See where your issues stand in their projects' }),
    ).toBeInTheDocument()
    expect(screen.getByText('gh auth refresh -s read:project')).toBeInTheDocument()
  })

  it('offers nothing when the token reads projects', async () => {
    platform.getInboxSnapshot.mockResolvedValue({ ...needsScope(), projectAccess: 'on' })
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await section('Assigned')
    expect(screen.queryByRole('region', { name: /projects/ })).not.toBeInTheDocument()
  })

  it('checks again with a refresh, then says the projects are on and shows them', async () => {
    platform.getInboxSnapshot.mockResolvedValue(needsScope())
    platform.refreshInbox.mockResolvedValue(
      snapshot({
        assigned: list([
          item({ id: 2, kind: 'issue', title: 'Fix the login page', details: details({ projects: [project()] }) }),
        ]),
        projectAccess: 'on',
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Check again' }))

    expect(platform.refreshInbox).toHaveBeenCalled()
    expect(await screen.findByRole('region', { name: 'Projects are on.' })).toBeInTheDocument()
    expect(
      (await section('Assigned')).getByRole('button', { name: 'Project Roadmap: In progress. Open it on GitHub' }),
    ).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('region', { name: 'Projects are on.' })).not.toBeInTheDocument()
    expect(dismissProjects).not.toHaveBeenCalled()
  })

  it('puts the offer away for good', async () => {
    platform.getInboxSnapshot.mockResolvedValue(needsScope())
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Don’t show this again' }))

    expect(dismissProjects).toHaveBeenCalledOnce()
    expect(screen.queryByRole('region', { name: /projects/ })).not.toBeInTheDocument()
  })
})

describe('InboxView’s status sort', () => {
  // assigned is an inbox whose Assigned list holds issues in a project, newest first.
  const assigned = () =>
    snapshot({
      assigned: list([
        item({
          id: 1,
          kind: 'issue',
          title: 'Deferred work',
          details: details({ projects: [project({ status: 'Deferred', statusColor: '#e16f24' })] }),
        }),
        item({ id: 2, kind: 'issue', title: 'Work with no project', details: details() }),
        item({
          id: 3,
          kind: 'issue',
          title: 'Ready work',
          details: details({ projects: [project({ status: 'Ready', statusColor: '#218bff' })] }),
        }),
        item({ id: 4, kind: 'issue', title: 'Work under way', details: details({ projects: [project()] }) }),
      ]),
    })

  // titles lists the titles of a list's rows, top first.
  const titles = (rows: Awaited<ReturnType<typeof section>>) =>
    rows
      .getAllByRole('listitem')
      .flatMap((row) => (row.dataset.itemId === undefined ? [] : [row.querySelector('button')?.textContent]))

  it('sorts a list by project status, and back to newest first', async () => {
    platform.getInboxSnapshot.mockResolvedValue(assigned())
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const shown = await section('Assigned')
    expect(titles(shown)).toEqual(['Deferred work', 'Work with no project', 'Ready work', 'Work under way'])

    await userEvent.click(shown.getByRole('radio', { name: 'Status' }))

    expect(titles(shown)).toEqual(['Work under way', 'Ready work', 'Deferred work', 'Work with no project'])
    expect(platform.setStatusSort).toHaveBeenCalledWith('assigned', true)

    await userEvent.click(shown.getByRole('radio', { name: 'Recent' }))
    expect(titles(shown)).toEqual(['Deferred work', 'Work with no project', 'Ready work', 'Work under way'])
  })

  it('opens sorted by status as it was left, in the user’s order', async () => {
    platform.getInboxSnapshot.mockResolvedValue(assigned())
    platform.getInboxOrder.mockResolvedValue({
      statusSorted: ['assigned'],
      statusOrder: ['Deferred', 'In progress', 'Ready'],
    })
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const shown = await section('Assigned')
    expect(shown.getByRole('radio', { name: 'Status' })).toBeChecked()
    expect(titles(shown)).toEqual(['Deferred work', 'Work under way', 'Ready work', 'Work with no project'])
  })

  it('offers no sort for a list without project statuses', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ assigned: list([item({ id: 2, kind: 'issue', details: details() })]) }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const shown = await section('Assigned')
    expect(shown.queryByRole('radiogroup', { name: 'Sort Assigned by' })).not.toBeInTheDocument()
  })
})

describe('InboxView edits on GitHub', () => {
  const assignedIssue = item({ id: 5, kind: 'issue', number: 9, title: 'Fix the flaky test', assigned: true })
  const asked = item({ id: 6, number: 11, title: 'Tune the cache', reviewRequest: 'you', reviewTeams: ['backend'] })

  // choose opens the menu of the one row in a list and picks one of its items.
  async function choose(list: Awaited<ReturnType<typeof section>>, label: string) {
    await userEvent.click(list.getByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: label }))
  }

  it('unassigns the user on GitHub, and confirms once the item has left the list', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ assigned: list([assignedIssue]) }))
    platform.unassignItem.mockResolvedValue(snapshot())
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const assigned = await section('Assigned')
    await choose(assigned, 'Unassign me')

    expect(platform.unassignItem).toHaveBeenCalledWith(5)
    expect(await screen.findByRole('status')).toHaveTextContent('Unassigned you on GitHub · Fix the flaky test')
    expect(assigned.queryByRole('button', { name: 'Fix the flaky test' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
  })

  it('says why an edit failed, and leaves the item where it was', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ assigned: list([assignedIssue]) }))
    platform.unassignItem.mockRejectedValue(new Error('GitHub kept you assigned to acme-org/example-repo#9.'))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const assigned = await section('Assigned')
    await choose(assigned, 'Unassign me')

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Could not unassign you from “Fix the flaky test” · GitHub kept you assigned to acme-org/example-repo#9.',
    )
    expect(assigned.getByRole('button', { name: 'Fix the flaky test' })).toBeInTheDocument()
    expect(assigned.getByRole('button', { name: 'More actions' })).toHaveAttribute('title', 'More actions')
  })

  it('takes the user off a pull request’s reviewers', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([asked]) }))
    platform.removeReviewRequest.mockResolvedValue(snapshot())
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await choose(review, 'Remove me as reviewer')

    expect(platform.removeReviewRequest).toHaveBeenCalledWith(6)
    expect(await screen.findByRole('status')).toHaveTextContent('Removed you as a reviewer on GitHub · Tune the cache')
    expect(review.queryByRole('button', { name: 'Tune the cache' })).not.toBeInTheDocument()
  })

  it('says which team a pull request still asks once the user is off it', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([asked]) }))
    platform.removeReviewRequest.mockResolvedValue(
      snapshot({ reviewRequested: list([{ ...asked, reviewRequest: 'team' }]) }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await choose(review, 'Remove me as reviewer')

    expect(await screen.findByRole('status')).toHaveTextContent(
      'Removed you as a reviewer on GitHub · It still asks the @backend team',
    )
    expect(review.getByRole('button', { name: 'Tune the cache' })).toBeInTheDocument()

    await userEvent.click(review.getByRole('button', { name: 'More actions' }))
    expect(screen.getByRole('menuitem', { name: 'Remove me as reviewer' })).toBeDisabled()
  })

  it('copies an item’s link, or says why it could not', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ reviewRequested: list([asked]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    await choose(review, 'Copy link')

    expect(platform.copyText).toHaveBeenCalledWith(asked.url)
    expect(await screen.findByRole('status')).toHaveTextContent(`Link copied · ${asked.url}`)

    platform.copyText.mockRejectedValue(new Error('The clipboard is not available.'))
    await choose(review, 'Copy link')

    expect(await screen.findByText('Could not copy the link')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('The clipboard is not available.')
  })
})

describe('InboxView filtering', () => {
  const later = '2026-06-20T00:00:00Z'
  // entry is an item on no branch, so only the words a test gives it match.
  const entry = (overrides: Parameters<typeof item>[0]) => item({ branch: '', ...overrides })
  const inbox = () =>
    snapshot({
      reviewRequested: list([
        entry({ id: 1, title: 'Add the widget cache', details: details() }),
        entry({ id: 2, number: 12, title: 'Fix the parser' }),
        entry({ id: 3, number: 13, title: 'Snoozed widget', snoozedUntil: later }),
      ]),
      authored: list([entry({ id: 4, number: 14, title: 'Write the docs' })]),
      assigned: list([
        entry({ id: 5, kind: 'issue', number: 31, title: 'Crash on start' }),
        entry({ id: 6, kind: 'issue', number: 32, title: 'Crash later', snoozedUntil: later }),
      ]),
      lastRefreshedAt: '2026-06-15T11:55:00Z',
    })

  beforeEach(() => {
    platform.getInboxSnapshot.mockResolvedValue(inbox())
  })

  it('shows only the matching items, counted in each tab, with the matches marked', async () => {
    render(<Harness launch={launchOptions} clock={fixedNow} query="widget" />)

    const review = await section('Review requested')

    expect(review.getByRole('tab', { name: 'Review requested 1 of 2 matching' })).toBeInTheDocument()
    expect(review.getByRole('tab', { name: 'Snoozed 1 matching' })).toBeInTheDocument()
    expect(review.queryByRole('button', { name: 'Fix the parser' })).not.toBeInTheDocument()

    const title = review.getByRole('button', { name: 'Add the widget cache' })

    expect(title.querySelector('mark')).toHaveTextContent('widget')
    expect(review.getByText('1 of 2')).toHaveAttribute('data-filtered', 'true')
  })

  it('hides a list with no match, which comes back as it was left', async () => {
    const view = render(<Harness launch={launchOptions} clock={fixedNow} />)
    const assigned = await section('Assigned')

    await userEvent.click(assigned.getByRole('tab', { name: 'Snoozed 1' }))
    view.rerender(<Harness launch={launchOptions} clock={fixedNow} query="widget" />)

    expect(screen.queryByRole('region', { name: 'Assigned' })).not.toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Authored' })).not.toBeInTheDocument()

    view.rerender(<Harness launch={launchOptions} clock={fixedNow} />)

    expect((await section('Assigned')).getByRole('tab', { name: 'Snoozed 1' })).toHaveAttribute('aria-selected', 'true')
  })

  it('shows a collapsed list open while filtering, leaving it collapsed', async () => {
    const view = render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Collapse Authored' }))
    view.rerender(<Harness launch={launchOptions} clock={fixedNow} query="docs" />)

    const authored = await section('Authored')

    expect(authored.getByRole('button', { name: 'Write the docs' })).toBeInTheDocument()
    expect(authored.queryByRole('button', { name: /Expand|Collapse/ })).not.toBeInTheDocument()

    view.rerender(<Harness launch={launchOptions} clock={fixedNow} />)

    expect(await screen.findByRole('button', { name: 'Expand Authored' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Write the docs' })).not.toBeInTheDocument()
  })

  it('points a tab with no match to the tab that has one', async () => {
    render(<Harness launch={launchOptions} clock={fixedNow} query="later" />)

    const assigned = await section('Assigned')

    expect(assigned.getByText(/No matches here/)).toBeInTheDocument()
    expect(assigned.queryByText(/Nothing here/)).not.toBeInTheDocument()

    await userEvent.click(assigned.getByRole('button', { name: '1 in Snoozed' }))

    expect(assigned.getByRole('tab', { name: 'Snoozed 1 matching' })).toHaveAttribute('aria-selected', 'true')
    expect(assigned.getByRole('button', { name: 'Crash later' })).toBeInTheDocument()
    expect(assigned.queryByText(/No matches here/)).not.toBeInTheDocument()
  })

  it('points a set-aside tab with no match back to the list', async () => {
    render(<Harness launch={launchOptions} clock={fixedNow} query="start" />)

    const assigned = await section('Assigned')

    await userEvent.click(assigned.getByRole('tab', { name: 'Snoozed' }))
    await userEvent.click(assigned.getByRole('button', { name: '1 in the list' }))

    expect(assigned.getByRole('button', { name: 'Crash on start' })).toBeInTheDocument()
  })

  it('says at the top when nothing matches, and offers to clear the filter', async () => {
    render(<Harness launch={launchOptions} clock={fixedNow} query="  zzz " />)

    expect(await screen.findByText('No items match “zzz”')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Review requested' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Clear filter' }))

    expect(clearFilter).toHaveBeenCalledOnce()
  })

  it('says nothing matches in place of the party at nil', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ lastRefreshedAt: '2026-06-15T11:55:00Z' }))
    render(<Harness launch={launchOptions} clock={fixedNow} query="cache" />)

    expect(await screen.findByText('No items match “cache”')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'inbox == nil' })).not.toBeInTheDocument()
  })

  it('filters by a card’s repository, author, or label when clicked', async () => {
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    const review = await section('Review requested')
    const row = review.getByRole('button', { name: 'Add the widget cache' }).closest('li') as HTMLElement

    await userEvent.click(within(row).getByRole('button', { name: 'Filter by repository acme-org/example-repo' }))
    await userEvent.click(within(row).getByRole('button', { name: 'Filter by author octo-user' }))
    await userEvent.click(within(row).getByRole('button', { name: 'Filter by label enhancement' }))

    expect(filterBy.mock.calls).toEqual([
      ['repo', 'acme-org/example-repo'],
      ['author', 'octo-user'],
      ['label', 'enhancement'],
    ])
  })
})

describe('InboxView review sheet', () => {
  it('opens a pull request from its title, and reports what the action came to', async () => {
    const pr = item({ id: 1, title: 'Add the widget cache' })
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ assigned: list([pr, item({ id: 2, number: 8, title: 'Second' })]) }),
    )
    platform.actOnReview.mockResolvedValue(
      reviewResult({ outcome: 'approved_not_merged', message: 'Approved, but GitHub won’t merge yet' }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} reviews />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

    const sheet = await screen.findByRole('dialog', { name: 'Add the widget cache' })
    expect(within(sheet).getByText('1 of 2 in Assigned')).toBeInTheDocument()

    await userEvent.click(await within(sheet).findByRole('button', { name: 'More review actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Approve' }))
    await userEvent.click(within(sheet).getByRole('button', { name: 'Approve' }))

    expect(await screen.findByText('Approved, but GitHub won’t merge yet')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: 'Add the widget cache' })).not.toBeInTheDocument()
    // Only a merge asks whether to offer a release.
    expect(platform.offerReleaseAfterMerge).not.toHaveBeenCalled()
  })

  it('says a merge’s comment was posted when GitHub didn’t merge, as a warning', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ assigned: list([item()]) }))
    platform.actOnReview.mockResolvedValue(
      reviewResult({
        outcome: 'commented_not_merged',
        message: 'Commented on acme-org/example-repo#7, but it wasn’t merged: It has conflicts',
        reason: 'It has conflicts',
        steps: ['Commented'],
        commented: true,
      }),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} reviews />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

    const sheet = await screen.findByRole('dialog', { name: 'Add the widget cache' })

    await userEvent.click(await within(sheet).findByRole('button', { name: 'More review actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Squash-merge' }))
    await userEvent.click(await within(sheet).findByRole('button', { name: 'Squash-merge…' }))
    await userEvent.click(
      within(screen.getByRole('dialog', { name: 'Merge #7 into main?' })).getByRole('button', { name: 'Squash-merge' }),
    )

    const toast = await screen.findByRole('group', { name: 'Notification' })
    expect(
      within(toast).getByText('Commented on acme-org/example-repo#7, but it wasn’t merged: It has conflicts'),
    ).toBeInTheDocument()
    expect(toast.querySelector('.text-warning')).not.toBeNull()
    expect(platform.offerReleaseAfterMerge).not.toHaveBeenCalled()
  })

  describe('after a close', () => {
    // close closes the inbox's pull request from the review sheet, with result, and returns
    // the toast that reports it.
    async function close(result: ReviewResult) {
      platform.getInboxSnapshot.mockResolvedValue(snapshot({ assigned: list([item()]) }))
      platform.actOnReview.mockResolvedValue(result)
      render(<Harness launch={launchOptions} clock={fixedNow} reviews />)

      await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

      const sheet = await screen.findByRole('dialog', { name: 'Add the widget cache' })

      await userEvent.click(await within(sheet).findByRole('button', { name: 'More review actions' }))
      await userEvent.click(screen.getByRole('menuitem', { name: 'Close pull request' }))
      await userEvent.click(
        within(screen.getByRole('dialog', { name: 'Close #7 without merging it?' })).getByRole('button', {
          name: 'Close pull request',
        }),
      )

      return screen.findByRole('group', { name: 'Notification' })
    }

    it('says it closed, and offers no release', async () => {
      const toast = await close(
        reviewResult({
          outcome: 'closed',
          message: 'Closed acme-org/example-repo#7',
          steps: ['Closed'],
          leftInbox: true,
        }),
      )

      expect(within(toast).getByText('Closed acme-org/example-repo#7')).toBeInTheDocument()
      expect(toast.querySelector('.text-warning')).toBeNull()
      expect(screen.queryByRole('dialog', { name: 'Add the widget cache' })).not.toBeInTheDocument()
      expect(platform.offerReleaseAfterMerge).not.toHaveBeenCalled()
    })

    it('says its comment was posted when GitHub didn’t close it, as a warning', async () => {
      const toast = await close(
        reviewResult({
          outcome: 'commented_not_closed',
          message: 'Commented on acme-org/example-repo#7, but it wasn’t closed: GitHub refused the change',
          reason: 'GitHub refused the change',
          steps: ['Commented'],
          commented: true,
        }),
      )

      expect(
        within(toast).getByText(
          'Commented on acme-org/example-repo#7, but it wasn’t closed: GitHub refused the change',
        ),
      ).toBeInTheDocument()
      expect(toast.querySelector('.text-warning')).not.toBeNull()
    })
  })

  describe('after a merge', () => {
    // merge merges the inbox's pull request from the review sheet, as Approve does when
    // GitHub merges it too, and returns the toast that says so.
    async function merge(releases = true) {
      platform.getInboxSnapshot.mockResolvedValue(snapshot({ assigned: list([item()]) }))
      platform.actOnReview.mockResolvedValue(
        reviewResult({ outcome: 'merged', message: 'Merged acme-org/example-repo#7' }),
      )
      render(<Harness launch={launchOptions} clock={fixedNow} reviews releases={releases} />)

      await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))

      const sheet = await screen.findByRole('dialog', { name: 'Add the widget cache' })

      await userEvent.click(await within(sheet).findByRole('button', { name: 'More review actions' }))
      await userEvent.click(screen.getByRole('menuitem', { name: 'Approve' }))
      await userEvent.click(within(sheet).getByRole('button', { name: 'Approve' }))

      return screen.findByRole('group', { name: 'Notification' })
    }

    it('offers to release the repository on the toast that says it merged, and opens its sheet', async () => {
      platform.offerReleaseAfterMerge.mockResolvedValue(
        afterMerge({ repo: 'acme-org/example-repo', dir: '~/projects/example-repo', branch: 'main' }),
      )

      const toast = within(await merge())

      expect(toast.getByText('Merged acme-org/example-repo#7')).toBeInTheDocument()
      expect(platform.offerReleaseAfterMerge).toHaveBeenCalledExactlyOnceWith('acme-org', 'example-repo', 'main')

      await userEvent.click(await toast.findByRole('button', { name: 'Release acme-org/example-repo…' }))

      expect(release).toHaveBeenCalledExactlyOnceWith(
        afterMerge({ repo: 'acme-org/example-repo', dir: '~/projects/example-repo', branch: 'main' }),
      )
      expect(screen.queryByRole('group', { name: 'Notification' })).not.toBeInTheDocument()
    })

    it('offers nothing when Release can’t release the repository', async () => {
      const toast = within(await merge())

      await waitFor(() => {
        expect(platform.offerReleaseAfterMerge).toHaveBeenCalledOnce()
      })
      expect(toast.queryByRole('button', { name: /^Release/ })).not.toBeInTheDocument()
    })

    it('asks nothing where the window offers no release', async () => {
      platform.offerReleaseAfterMerge.mockResolvedValue(afterMerge())

      const toast = within(await merge(false))

      expect(platform.offerReleaseAfterMerge).not.toHaveBeenCalled()
      expect(toast.queryByRole('button', { name: /^Release/ })).not.toBeInTheDocument()
    })

    it('writes a failure to ask to the console, and offers nothing', async () => {
      const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)

      platform.offerReleaseAfterMerge.mockRejectedValue(new Error('database is locked'))

      const toast = within(await merge())

      await waitFor(() => {
        expect(logged).toHaveBeenCalledWith('could not ask whether the merge offers a release:', 'database is locked')
      })
      expect(toast.queryByRole('button', { name: /^Release/ })).not.toBeInTheDocument()
    })

    it('adds nothing to a toast already gone when the answer comes', async () => {
      let answer: (offer: AfterMerge) => void = () => undefined

      platform.offerReleaseAfterMerge.mockReturnValue(
        new Promise((resolve) => {
          answer = resolve
        }),
      )

      const toast = within(await merge())

      await userEvent.click(toast.getByRole('button', { name: 'Close' }))
      await act(async () => {
        answer(afterMerge())
        await Promise.resolve()
      })

      expect(screen.queryByRole('button', { name: /^Release/ })).not.toBeInTheDocument()
    })
  })

  it('opens one from the menu, steps through its list, and closes', async () => {
    platform.getInboxSnapshot.mockResolvedValue(
      snapshot({ reviewRequested: list([item({ id: 1 }), item({ id: 2, number: 8, title: 'Second' })]) }),
    )
    platform.openReview.mockImplementation((id) =>
      Promise.resolve(review({ id, title: id === 2 ? 'Second' : 'Add the widget cache' })),
    )
    platform.refreshReview.mockImplementation((id) =>
      Promise.resolve(review({ id, title: id === 2 ? 'Second' : 'Add the widget cache' })),
    )
    render(<Harness launch={launchOptions} clock={fixedNow} reviews />)

    const row = (await screen.findAllByRole('listitem')).find((li) => li.textContent.includes('Add the widget cache'))
    await userEvent.click(within(row as HTMLElement).getByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Review…' }))

    expect(await screen.findByText('1 of 2 in Review requested')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(await screen.findByRole('heading', { name: 'Second' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('opens GitHub when the sheet is off', async () => {
    platform.getInboxSnapshot.mockResolvedValue(snapshot({ assigned: list([item()]) }))
    render(<Harness launch={launchOptions} clock={fixedNow} />)

    await userEvent.click(await screen.findByRole('button', { name: 'Add the widget cache' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
