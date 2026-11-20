import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Activity, useState } from 'react'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { activityTarget, type AutopilotRequest } from '../../lib/autopilot'
import { useIssueDrafts } from '../../lib/issueDrafts'
import { useReviewDrafts } from '../../lib/reviewDrafts'
import { useAutopilot } from '../../lib/useAutopilot'
import { useInbox } from '../../lib/useInbox'
import type { AutopilotMatch, KitOutcome, Settings } from '../../platform'
import { deferred, nextTask } from '../../test/async'
import { at } from '../../test/clock'
import {
  activityGroup,
  activityPage,
  agentRun,
  agents,
  autopilotEngine,
  autopilotHold,
  autopilotMatch,
  claudeMissing,
  cloneTarget,
  describeCard,
  describeConversation,
  describeStatus,
  describeTurn,
  dryRunMatch,
  galleryTemplate,
  githubStep,
  issue,
  item,
  itemRef,
  kitChecked,
  kitCheckedRoutine,
  kitOutcome,
  leftAlone,
  profile,
  prompts,
  readiness,
  readyCheck,
  routine,
  routineFigures,
  routineReady,
  routineSpec,
  settings as settingsOf,
  snoozeTemplate,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import type { LaunchOptions } from '../ItemRow'
import { AutopilotRowsContext } from './AutopilotChip'
import AutopilotView from './AutopilotView'
import type { RowsKit } from './WatchingList'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// The sheets load on their own (lazily, as the inbox's do), and are imported once before
// the tests, so no test pays a sheet's cold, instrumented import inside the second a
// findBy waits: on a cold runner the first test to open one otherwise ran out of it.
beforeAll(async () => {
  await Promise.all([import('../review/ReviewSheet'), import('../issue/IssueSheet')])
})

const launch: LaunchOptions = {
  agents: agents(),
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
  onSettingsSaved: () => undefined,
}

const onShowItem = vi.fn<(itemId: number, url: string) => void>()
const onSettings = vi.fn()
const onEditPrompts = vi.fn()

interface TabProps {
  stored?: Settings
  request?: AutopilotRequest | null
  // listed are the items the inbox lists.
  listed?: readonly number[]
}

// Tab renders the Autopilot tab over the real Autopilot, inbox, and drafts, as App does,
// keeping the settings a setter stores.
function Tab({ stored = settingsOf(), request = null, listed = [] }: TabProps) {
  const autopilot = useAutopilot()
  const inbox = useInbox()
  const review = useReviewDrafts()
  const issue = useIssueDrafts()
  const [settings, setSettings] = useState(stored)
  const kit: RowsKit = {
    inbox,
    launch,
    sessions: new Map(),
    worktrees: new Map(),
    snoozeDays: 3,
    repoRules: settings.repoRules,
    drafts: { review, issue },
    reviewTab: 'files',
    onReviewTab: () => undefined,
    conversationOrder: 'oldest',
    onConversationOrder: () => undefined,
    onSnoozeSettings: () => undefined,
    onFilesSettings: () => undefined,
    onRepoRuleSettings: () => undefined,
    onManageReplies: () => undefined,
    onSettingsSaved: setSettings,
    onItemOpened: () => undefined,
  }

  return (
    <AutopilotRowsContext value={{ names: autopilot.names, chips: new Map() }}>
      <AutopilotView
        autopilot={autopilot}
        settings={settings}
        onSettingsSaved={setSettings}
        request={request}
        kit={kit}
        listed={(id) => listed.includes(id)}
        onShowItem={onShowItem}
        onSettings={onSettings}
        viewer="octo-user"
        prompts={launch.prompts}
        agents={launch.agents}
        profiles={launch.profiles}
        onEditPrompts={onEditPrompts}
      />
    </AutopilotRowsContext>
  )
}

function renderTab(props: TabProps = {}) {
  return render(<Tab {...props} />, { wrapper: at() })
}

// routines are the tab's routines: Snooze passing bot PRs, on; Investigate new bugs, off in
// Dry run; another account's; and one a newer build made.
function routines() {
  return [
    routine(),
    routine({ id: 4, name: 'Investigate new bugs', enabled: false, enabledAt: null, mode: 'dryRun' }),
    routine({ id: 5, name: 'Someone else’s', otherAccount: true, account: 'octo-other' }),
    routine({
      id: 6,
      name: 'From a newer build',
      enabled: false,
      broken: `Made by a newer ${brand.name}: update to edit it`,
      brokenReason: 'newer',
      sentence: '',
    }),
  ]
}

// sheetReads makes the issue sheet's reads, its opening and its refresh, both answer read.
function sheetReads(read: ReturnType<typeof issue>) {
  platform.openIssue.mockResolvedValue(read)
  platform.refreshIssue.mockResolvedValue(read)
}

// routineList returns the queries scoped to the list of routines, once it shows.
async function routineList() {
  return within(await screen.findByRole('list', { name: 'Routines' }))
}

describe('the Autopilot tab', () => {
  it('explains Autopilot in its two sentences while there is no routine, then offers templates and the ways to start', async () => {
    platform.listTemplates.mockResolvedValue([
      snoozeTemplate(),
      galleryTemplate({ suggested: '3 of your clones run go-fortress’s security checks.' }),
    ])
    renderTab()

    expect(await screen.findByText(/Autopilot watches your pull requests and issues/)).toBeInTheDocument()
    expect(screen.getByText(/Its routines can tell you, tidy your inbox, or start a coding agent/)).toBeInTheDocument()

    const suggested = within(await screen.findByRole('list', { name: 'Suggested templates' }))
    expect(suggested.getByRole('button', { name: /Fix Dependabot security failures/ })).toBeInTheDocument()
    expect(suggested.getByText('3 of your clones run go-fortress’s security checks.')).toBeInTheDocument()
    expect(
      within(screen.getByRole('list', { name: 'Templates' })).getByRole('button', { name: /Snooze passing bot PRs/ }),
    ).toBeInTheDocument()

    for (const name of ['Start from scratch', 'Start from an item', 'Every template', 'Import routines…']) {
      expect(screen.getByRole('button', { name })).toBeInTheDocument()
    }

    expect(screen.queryByRole('switch')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Start from an item' }))
    expect(screen.getByText(/choose Autopilot ▸ Make a routine from this…/)).toBeInTheDocument()
  })

  it('lists the routines with their status lines and switches, greying another account’s and saying why one is broken', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    const list = await routineList()

    expect(list.getByRole('switch', { name: 'Snooze passing bot PRs' })).toBeChecked()
    expect(list.getByRole('switch', { name: 'Investigate new bugs' })).not.toBeChecked()
    expect(list.getByText('Dry run · off')).toBeInTheDocument()
    expect(list.getByText('Belongs to @octo-other')).toBeInTheDocument()
    expect(list.queryByRole('switch', { name: 'Someone else’s' })).not.toBeInTheDocument()
    expect(list.getByText(`Made by a newer ${brand.name}: update to edit it`)).toBeInTheDocument()
    expect(list.queryByRole('switch', { name: 'From a newer build' })).not.toBeInTheDocument()
  })

  it('switches a routine off at once, and on only after asking about what it matches now', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 3, pullRequests: 3, issues: 0, unknown: 0, incomplete: false })
    renderTab()

    const list = await routineList()

    await userEvent.click(list.getByRole('switch', { name: 'Snooze passing bot PRs' }))
    expect(platform.routineMatching).not.toHaveBeenCalled()
    expect(platform.setRoutineEnabled).toHaveBeenLastCalledWith(3, false, 'run')

    await userEvent.click(list.getByRole('switch', { name: 'Investigate new bugs' }))

    const question = await screen.findByRole('dialog', { name: '3 pull requests match right now.' })

    expect(platform.routineMatching).toHaveBeenCalledWith(
      expect.objectContaining({ id: 4, name: 'Investigate new bugs' }),
    )
    expect(platform.setRoutineEnabled).toHaveBeenCalledTimes(1)
    expect(question).toHaveTextContent('in Dry run')

    await userEvent.click(within(question).getByRole('button', { name: 'Only what changes from now on' }))

    await waitFor(() => {
      expect(platform.setRoutineEnabled).toHaveBeenLastCalledWith(4, true, 'fromNow')
    })
    expect(screen.queryByRole('dialog', { name: '3 pull requests match right now.' })).not.toBeInTheDocument()
  })

  it('runs on what matches now when asked to, asks nothing when nothing does, and Escape changes nothing', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 1, pullRequests: 0, issues: 1, unknown: 0, incomplete: false })
    renderTab()

    const list = await routineList()

    await userEvent.click(list.getByRole('switch', { name: 'Investigate new bugs' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Run on them too' }))
    await waitFor(() => {
      expect(platform.setRoutineEnabled).toHaveBeenLastCalledWith(4, true, 'run')
    })

    platform.setRoutineEnabled.mockClear()
    await userEvent.click(list.getByRole('switch', { name: 'Investigate new bugs' }))
    await screen.findByRole('dialog', { name: '1 issue matches right now.' })
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.setRoutineEnabled).not.toHaveBeenCalled()

    platform.routineMatching.mockResolvedValue({ count: 0, pullRequests: 0, issues: 0, unknown: 0, incomplete: false })
    await userEvent.click(list.getByRole('switch', { name: 'Investigate new bugs' }))
    await waitFor(() => {
      expect(platform.setRoutineEnabled).toHaveBeenLastCalledWith(4, true, 'run')
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('asks, too, when what matches can’t be told yet or not everything could be read', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 0, pullRequests: 0, issues: 0, unknown: 2, incomplete: false })
    renderTab()

    const list = await routineList()

    await userEvent.click(list.getByRole('switch', { name: 'Investigate new bugs' }))

    const unknown = await screen.findByRole('dialog', { name: '2 items may match right now.' })

    expect(unknown).toHaveTextContent('2 items can’t be told yet')
    expect(platform.setRoutineEnabled).not.toHaveBeenCalled()
    await userEvent.keyboard('{Escape}')

    platform.routineMatching.mockResolvedValue({ count: 0, pullRequests: 0, issues: 0, unknown: 0, incomplete: true })
    await userEvent.click(list.getByRole('switch', { name: 'Investigate new bugs' }))

    const unread = await screen.findByRole('dialog', { name: 'Not everything it watches could be read right now.' })

    expect(unread).toHaveTextContent('it records what it would do, in Dry run: for whatever matches now too')
    expect(unread).toHaveTextContent('more may match than this says')
    expect(platform.setRoutineEnabled).not.toHaveBeenCalled()
    await userEvent.click(within(unread).getByRole('button', { name: 'Only what changes from now on' }))
    await waitFor(() => {
      expect(platform.setRoutineEnabled).toHaveBeenLastCalledWith(4, true, 'fromNow')
    })
  })

  it('shows a refusal in its own words', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 2, pullRequests: 2, issues: 0, unknown: 0, incomplete: false })
    platform.setRoutineEnabled.mockRejectedValue(
      new Error(`Autopilot is running in another copy of ${brand.name}; make this change there.`),
    )
    renderTab()

    await userEvent.click((await routineList()).getByRole('switch', { name: 'Investigate new bugs' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Only what changes from now on' }))

    expect(
      await screen.findByText(`Autopilot is running in another copy of ${brand.name}; make this change there.`),
    ).toBeInTheDocument()
  })

  it('duplicates, puts in Dry run, and deletes a routine from its ⋯ menu, asking before it deletes', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    const list = await routineList()

    await userEvent.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Duplicate' }))
    expect(platform.duplicateRoutine).toHaveBeenCalledExactlyOnceWith(3)
    expect(await screen.findByText('Made “Snooze passing bot PRs copy”, switched off.')).toBeInTheDocument()

    await userEvent.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Dry run' }))
    await waitFor(() => {
      expect(platform.setRoutineMode).toHaveBeenLastCalledWith(3, 'dryRun', 'run')
    })

    await userEvent.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Delete…' }))

    const asked = await screen.findByRole('dialog', { name: 'Delete “Snooze passing bot PRs”?' })

    expect(asked).toHaveTextContent('What it did stays in Activity, under its name.')
    expect(platform.deleteRoutine).not.toHaveBeenCalled()
    await userEvent.click(within(asked).getByRole('button', { name: 'Delete' }))
    expect(platform.deleteRoutine).toHaveBeenCalledExactlyOnceWith(3)
  })

  it('asks before an enabled routine leaves Dry run, and keeps another account’s mode where it is', async () => {
    platform.listRoutines.mockResolvedValue([routine({ mode: 'dryRun' }), routines()[2] ?? routine()])
    platform.routineMatching.mockResolvedValue({ count: 2, pullRequests: 2, issues: 0, unknown: 0, incomplete: false })
    renderTab()

    const list = await routineList()

    await userEvent.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Leave Dry run' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Run on them too' }))
    await waitFor(() => {
      expect(platform.setRoutineMode).toHaveBeenLastCalledWith(3, 'ask', 'run')
    })

    await userEvent.click(list.getByRole('button', { name: 'More for Someone else’s' }))
    expect(await screen.findByRole('menuitem', { name: 'Dry run' })).toBeDisabled()
  })
})

describe('Activity', () => {
  it('lists today’s lines: the local steps grouped by routine with Undo, and what a Dry run would have done', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    renderTab()

    const today = within(await screen.findByRole('list', { name: 'Today' }))
    const snoozed = today.getByText('Snoozed 1').closest('li')
    const dry = today.getByText('Would run').closest('li')

    expect(snoozed).toHaveTextContent('example-repo #7')
    expect(dry).toHaveTextContent('example-repo #9')

    await userEvent.click(within(snoozed as HTMLElement).getByRole('button', { name: 'Undo' }))

    expect(platform.undoAutopilot).toHaveBeenCalledExactlyOnceWith([51])
    expect(await screen.findByText('Put back 1 item.')).toBeInTheDocument()
  })

  it('offers Let it act again on what a routine leaves alone, and Try again on one that gave up', async () => {
    const page = activityPage({
      days: [
        {
          date: '2026-06-15',
          matches: [
            autopilotMatch({
              id: 30,
              state: 'skipped',
              reason: 'tries',
              words: 'It gave up: 2 tries used.',
              item: itemRef({ id: 2, number: 9 }),
              effects: [],
            }),
          ],
          groups: [],
        },
      ],
      leftAlone: [leftAlone()],
    })

    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(page)
    platform.autopilotActivityFrom.mockResolvedValue(page)
    renderTab()

    const today = within(await screen.findByRole('list', { name: 'Today' }))

    await userEvent.click(today.getByRole('button', { name: 'Let it act again' }))
    expect(platform.letRoutineActAgain).toHaveBeenCalledExactlyOnceWith(3, 1)
    expect(await screen.findByText('Snooze passing bot PRs can act on it again.')).toBeInTheDocument()
    // What a line's action changes may be further down than Activity reads again on its own,
    // so every page loaded is read again: here the first, all there is, read as it was.
    await waitFor(() => {
      expect(platform.autopilotActivity).toHaveBeenCalledTimes(2)
    })
    expect(platform.autopilotActivity).toHaveBeenLastCalledWith(0, 100)

    expect(today.getByText('Gave up').closest('li')).toHaveTextContent('It gave up: 2 tries used.')
    await userEvent.click(today.getByRole('button', { name: 'Try again' }))
    expect(platform.tryRoutineAgain).toHaveBeenCalledExactlyOnceWith(3, 2)
  })

  it('filters the lines by routine and by result', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    renderTab()

    await screen.findByText('Snoozed 1')

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Routine' }), 'Investigate new bugs')
    expect(screen.queryByText('Snoozed 1')).not.toBeInTheDocument()
    expect(screen.getByText('Would run')).toBeInTheDocument()

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Result' }), 'Snoozes, pins, and dismissals')
    expect(screen.getByText('Nothing here matches the filter.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Clear the filter' }))
    expect(screen.getByText('Snoozed 1')).toBeInTheDocument()
  })

  it('reads its days again whenever Autopilot changes', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    expect(await screen.findByText('Nothing yet. What your routines do shows here, newest first.')).toBeInTheDocument()

    platform.autopilotActivity.mockResolvedValue(activityPage())
    act(() => {
      emit('autopilot:updated')
    })

    expect(await screen.findByText('Snoozed 1')).toBeInTheDocument()
  })

  it('opens an item’s card in the inbox while it is listed, else its row in the routine’s Watching list', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    platform.routineWatching.mockResolvedValue([2])
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    renderTab({ listed: [1] })

    const snoozed = (await screen.findByText('Snoozed 1')).closest('li') as HTMLElement

    expect(within(snoozed).queryByRole('button', { name: /^Open/ })).not.toBeInTheDocument()

    const dry = screen.getByText('Would run').closest('li') as HTMLElement

    await userEvent.click(within(dry).getByRole('button', { name: 'Open example-repo #9' }))

    expect(onShowItem).not.toHaveBeenCalled()
    expect(platform.routineWatching).toHaveBeenCalledWith(4)

    const watching = within(await screen.findByRole('list', { name: 'Watching' }))
    const row = watching.getByRole('button', { name: 'The widget forgets its size' }).closest('li')

    expect(row).toHaveAttribute('data-highlighted', 'true')
  })

  it('opens an item no routine watches in its sheet, a deleted routine’s too, and GitHub only on a click', async () => {
    // The line is a deleted routine's: routine 9 is in no list.
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(
      activityPage({
        days: [{ date: '2026-06-15', matches: [dryRunMatch({ routineId: 9, routine: 'Gone' })], groups: [] }],
      }),
    )
    platform.itemsByID.mockImplementation((ids) =>
      Promise.resolve(
        (ids ?? []).includes(2)
          ? [item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' })]
          : [],
      ),
    )
    // The sheet takes its name from its own reads of the issue, once they are in: both say
    // what the card says, so the name never changes under the test.
    sheetReads(issue({ id: 2, number: 9, title: 'The widget forgets its size' }))
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Open example-repo #9' }))

    expect(await screen.findByRole('dialog', { name: 'The widget forgets its size' })).toBeInTheDocument()
    // The sheet reads the issue in an effect after it shows, which a busy runner may not
    // have run when the sheet is first found.
    await waitFor(() => {
      expect(platform.openIssue).toHaveBeenCalledWith(2)
    })
    expect(platform.openExternal).not.toHaveBeenCalled()

    // Gone from the store too, it opens on GitHub, once, for the click.
    await userEvent.keyboard('{Escape}')
    platform.itemsByID.mockResolvedValue([])
    await userEvent.click(screen.getByRole('button', { name: 'Open example-repo #9' }))
    await waitFor(() => {
      expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/issues/9')
    })

    // Visiting routines afterwards opens nothing.
    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await screen.findByText(/Nothing to watch right now/)
    await userEvent.click((await routineList()).getByRole('button', { name: /^Investigate new bugs/ }))
    await screen.findByText(/Nothing to watch right now/)
    expect(platform.openExternal).toHaveBeenCalledOnce()
  })

  it('marks a watched row once, in the routine that watches it, and not when the routine is visited again', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    platform.routineWatching.mockImplementation((rid) => Promise.resolve(rid === 3 ? [2] : []))
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    renderTab()

    // The Dry run line is routine 4's, which no longer watches the item; routine 3 does.
    await userEvent.click(await screen.findByRole('button', { name: 'Open example-repo #9' }))

    const pane = within(await screen.findByRole('region', { name: 'Snooze passing bot PRs' }))
    const row = (await pane.findByRole('button', { name: 'The widget forgets its size' })).closest('li')

    expect(row).toHaveAttribute('data-highlighted', 'true')

    await userEvent.click(screen.getByRole('button', { name: 'Activity' }))
    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))

    const again = (await screen.findByRole('button', { name: 'The widget forgets its size' })).closest('li')

    expect(again).not.toHaveAttribute('data-highlighted')
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('asks every routine at once, and shows the item in the first in order that watches it', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    // The Dry run line is routine 4's, which no longer watches the item; routines 3 and 5
    // both do, and 5 answers first.
    const earlier = deferred<number[]>()
    platform.routineWatching.mockImplementation((rid) =>
      rid === 3 ? earlier.promise : Promise.resolve(rid === 5 ? [2] : []),
    )
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Open example-repo #9' }))

    expect(platform.routineWatching.mock.calls.map(([rid]) => rid)).toEqual([4, 3, 5, 6])
    await nextTask()
    expect(screen.queryByRole('region', { name: 'Someone else’s' })).not.toBeInTheDocument()

    earlier.resolve([2])

    const pane = within(await screen.findByRole('region', { name: 'Snooze passing bot PRs' }))
    const row = (await pane.findByRole('button', { name: 'The widget forgets its size' })).closest('li')

    expect(row).toHaveAttribute('data-highlighted', 'true')
    expect(screen.queryByRole('region', { name: 'Someone else’s' })).not.toBeInTheDocument()
  })

  it('passes over a routine whose list couldn’t be read when it opens an item', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    // The line's own routine, 4, would come first, but its list can't be read.
    platform.routineWatching.mockImplementation((rid) =>
      rid === 4 ? Promise.reject(new Error('The database is locked.')) : Promise.resolve(rid === 3 ? [2] : []),
    )
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Open example-repo #9' }))

    const pane = within(await screen.findByRole('region', { name: 'Snooze passing bot PRs' }))
    const row = (await pane.findByRole('button', { name: 'The widget forgets its size' })).closest('li')

    expect(row).toHaveAttribute('data-highlighted', 'true')
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('lets a Watching ask go once another routine or Activity is chosen, so it never lands later', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    platform.routineWatching.mockImplementation((rid) => Promise.resolve(rid === 3 ? [2] : []))
    // The list's first read of its items is still on its way when Activity is chosen.
    platform.itemsByID.mockReturnValueOnce(new Promise(() => undefined))
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Open example-repo #9' }))
    expect(await screen.findByRole('region', { name: 'Snooze passing bot PRs' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Activity' }))
    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))

    const row = (await screen.findByRole('button', { name: 'The widget forgets its size' })).closest('li')

    expect(row).not.toHaveAttribute('data-highlighted')
  })

  it('keeps the sheet of an item no list holds as the item is, after its own triage', async () => {
    // The line is a deleted routine's: routine 9 is in no list.
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(
      activityPage({
        days: [{ date: '2026-06-15', matches: [dryRunMatch({ routineId: 9, routine: 'Gone' })], groups: [] }],
      }),
    )
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    // The sheet takes its name from its own reads of the issue, once they are in: both say
    // what the card says, so the name never changes under the test.
    sheetReads(issue({ id: 2, number: 9, title: 'The widget forgets its size' }))
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Open example-repo #9' }))

    const sheet = within(await screen.findByRole('dialog', { name: 'The widget forgets its size' }))

    platform.itemsByID.mockResolvedValue([
      item({
        id: 2,
        kind: 'issue',
        number: 9,
        title: 'The widget forgets its size',
        dismissedAt: '2026-06-15T12:00:00Z',
      }),
    ])
    // The item is read again once the backend says the stored inbox changed, never on the
    // window's own showing of the change, before it is stored or as it is.
    const reads = platform.itemsByID.mock.calls.length

    await userEvent.click(sheet.getByRole('button', { name: 'Dismiss' }))

    expect(platform.dismissItem).toHaveBeenCalledWith(2)
    expect(platform.itemsByID).toHaveBeenCalledTimes(reads)

    act(() => {
      emit('inbox:updated')
    })

    expect(await sheet.findByRole('button', { name: 'Restore' })).toBeInTheDocument()
    expect(sheet.queryByRole('button', { name: 'Dismiss' })).not.toBeInTheDocument()
    expect(platform.itemsByID).toHaveBeenCalledTimes(reads + 1)
  })

  it('keeps a Watching ask while the question about unsaved changes is open, and shows it on Discard', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    platform.routineWatching.mockImplementation((rid) => Promise.resolve(rid === 3 ? [2] : []))
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    renderTab()

    // The Dry run line is in Investigate new bugs' own Activity, whose draft has changes;
    // Snooze passing bot PRs watches its item.
    await user.click((await routineList()).getByRole('button', { name: /^Investigate new bugs/ }))
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), ' now')

    const pane = within(screen.getByRole('region', { name: 'Investigate new bugs' }))

    await user.click(await pane.findByRole('button', { name: 'Open example-repo #9' }))
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Discard your changes?' })).getByRole('button', {
        name: 'Discard changes',
      }),
    )

    const watching = within(await screen.findByRole('region', { name: 'Snooze passing bot PRs' }))
    const row = (await watching.findByRole('button', { name: 'The widget forgets its size' })).closest('li')

    await waitFor(() => {
      expect(row).toHaveAttribute('data-highlighted', 'true')
    })
  })

  it('lets a Watching ask go once the question about unsaved changes is answered Keep editing', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    platform.routineWatching.mockImplementation((rid) => Promise.resolve(rid === 3 ? [2] : []))
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    renderTab()

    await user.click((await routineList()).getByRole('button', { name: /^Investigate new bugs/ }))
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), ' now')

    const pane = within(screen.getByRole('region', { name: 'Investigate new bugs' }))

    await user.click(await pane.findByRole('button', { name: 'Open example-repo #9' }))
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Discard your changes?' })).getByRole('button', {
        name: 'Keep editing',
      }),
    )

    // Choosing the routine later, by hand, marks nothing.
    await user.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Discard your changes?' })).getByRole('button', {
        name: 'Discard changes',
      }),
    )

    const row = (await screen.findByRole('button', { name: 'The widget forgets its size' })).closest('li')

    expect(row).not.toHaveAttribute('data-highlighted')
  })

  it('says why Activity has no line for a match a request names', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(
      activityPage({
        days: [
          {
            date: '2026-06-15',
            matches: [autopilotMatch({ state: 'baseline', reason: 'from_now', effects: [], steps: [] })],
            groups: [],
          },
        ],
      }),
    )
    renderTab({
      request: {
        seq: 1,
        target: { routineId: 3, matchId: 12, runId: 0, createdAt: '2026-06-15T11:30:00Z', activity: false },
      },
    })

    expect(
      await screen.findByText('Activity has no line for that one. Already matched when you turned it on.'),
    ).toBeInTheDocument()
  })

  it('opens an agent’s run beside Activity when a request names it, as Agent finished does', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab({
      request: {
        seq: 1,
        target: { routineId: 3, matchId: 12, runId: 81, createdAt: '2026-06-15T11:30:00Z', activity: false },
      },
    })

    const view = await screen.findByRole('region', { name: 'Fix failing checks' })
    expect(platform.autopilotRunView).toHaveBeenCalledWith(81)
    expect(screen.getByRole('heading', { name: 'Activity' })).toBeInTheDocument()

    await userEvent.click(within(view).getByRole('button', { name: 'Close the run' }))
    expect(screen.queryByRole('region', { name: 'Fix failing checks' })).not.toBeInTheDocument()
  })

  it('opens a finished agent’s run from its line in Activity', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(
      activityPage({
        days: [
          {
            date: '2026-06-15',
            matches: [
              autopilotMatch({
                effects: [],
                runs: [agentRun({ status: 'ended', outcome: 'done', finishedAt: '2026-06-15T11:40:00Z' })],
              }),
            ],
            groups: [],
          },
        ],
      }),
    )
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Show the run' }))

    expect(await screen.findByRole('region', { name: 'Fix failing checks' })).toBeInTheDocument()
    expect(platform.autopilotRunView).toHaveBeenCalledWith(81)
  })

  it('opens a working agent’s run from Working’s Watch', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotWorking.mockResolvedValue({
      working: [
        {
          runId: 81,
          matchId: 12,
          routineId: 3,
          routine: 'Fix failing checks',
          item: itemRef(),
          agent: 'claude',
          where: 'background',
          startedAt: '2026-06-15T11:30:00Z',
          createdAt: '2026-06-15T11:30:00Z',
          status: 'running',
          progress: { step: '', todos: [], tokensIn: 0, tokensOut: 0, cacheRead: 0, events: 0, unknown: 0 },
        },
      ],
    })
    renderTab()

    const working = await screen.findByRole('region', { name: 'Working' })
    await userEvent.click(within(working).getByRole('button', { name: 'Watch' }))

    expect(await screen.findByRole('region', { name: 'Fix failing checks' })).toBeInTheDocument()
    expect(within(working).getByRole('button', { name: 'Watch' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('shows every line again once the filter is back to every routine', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())
    renderTab()

    await screen.findByText('Snoozed 1')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Routine' }), 'Investigate new bugs')
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Routine' }), 'Every routine')

    expect(screen.getByText('Snoozed 1')).toBeInTheDocument()
    expect(screen.queryByText('Nothing here matches the filter.')).not.toBeInTheDocument()
  })

  it('reads each routine’s line from its own figures, whatever Activity has loaded', async () => {
    platform.listRoutines.mockResolvedValue([
      routine({ figures: routineFigures({ today: 6 }) }),
      routine({
        id: 4,
        name: 'Investigate new bugs',
        mode: 'dryRun',
        figures: routineFigures({ lastAt: '2026-06-15T10:00:00Z', lastDryRun: true }),
      }),
    ])
    renderTab()

    const list = await routineList()

    expect(list.getByText('Autopilot · 6 today')).toBeInTheDocument()
    expect(list.getByText(/^Dry run · would have run/)).toHaveTextContent('Dry run · would have run 2h ago')
  })

  it('says, across the top, that Autopilot is paused, with Resume, and that another copy runs it', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.getAutopilotEngine.mockResolvedValue(autopilotEngine({ elsewhere: true, holds: false }))
    renderTab({ stored: settingsOf({ autopilot: { ...settingsOf().autopilot, paused: true } }) })

    expect(await screen.findByText('Autopilot is paused. Nothing new starts.')).toBeInTheDocument()
    expect(
      await screen.findByText(
        `Autopilot is running in another copy of ${brand.name}. This one shows them and starts nothing.`,
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Resume Autopilot' })).toHaveAttribute('aria-pressed', 'true')

    await userEvent.click(screen.getByRole('button', { name: 'Resume' }))

    expect(platform.setAutopilotPaused).toHaveBeenCalledExactlyOnceWith(false)
    await waitFor(() => {
      expect(screen.queryByText('Autopilot is paused. Nothing new starts.')).not.toBeInTheDocument()
    })
  })

  it('pauses Autopilot from its header', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Pause Autopilot' }))

    expect(platform.setAutopilotPaused).toHaveBeenCalledExactlyOnceWith(true)
    expect(await screen.findByText('Autopilot is paused. Nothing new starts.')).toBeInTheDocument()
  })

  it('shows the match a request names, reading earlier pages until one holds it', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockImplementation((before, limit) =>
      Promise.resolve(
        limit <= 100 && before === 0
          ? activityPage({ days: [{ date: '2026-06-15', matches: [dryRunMatch()], groups: [] }], more: true, next: 14 })
          : activityPage(),
      ),
    )
    renderTab({
      request: {
        seq: 1,
        target: { routineId: 3, matchId: 12, runId: 0, createdAt: '2026-06-15T11:30:00Z', activity: false },
      },
    })

    const line = (await screen.findByText('Snoozed 1')).closest('li')

    await waitFor(() => {
      expect(line).toHaveAttribute('data-highlighted', 'true')
    })
    expect(platform.autopilotActivity).toHaveBeenCalledWith(14, 100)
  })

  it('looks for the match a request names in Activity as it is now, once the tab shows again', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage())

    // Shown is the tab as App holds it: kept, hidden, while another tab shows.
    function Shown({ visible, request }: { visible: boolean; request: AutopilotRequest | null }) {
      return (
        <Activity mode={visible ? 'visible' : 'hidden'}>
          <Tab request={request} />
        </Activity>
      )
    }

    const { rerender } = render(<Shown visible request={null} />, { wrapper: at() })

    await screen.findByText('Snoozed 1')
    rerender(<Shown visible={false} request={null} />)

    // While the tab is away, the routine matches again, and its notification asks for that.
    const recorded = autopilotMatch({ id: 20, effects: [], createdAt: '2026-06-15T11:58:00Z' })
    let give: (page: ReturnType<typeof activityPage>) => void = () => undefined
    const held = new Promise<ReturnType<typeof activityPage>>((done) => {
      give = done
    })

    platform.autopilotActivity.mockReturnValueOnce(Object.assign(held, { cancel: vi.fn() }))
    rerender(
      <Shown
        visible
        request={{
          seq: 1,
          target: { routineId: 3, matchId: 20, runId: 0, createdAt: '2026-06-15T11:58:00Z', activity: false },
        }}
      />,
    )

    // The days held from before it hid don't have it, and the tab says nothing yet.
    expect(screen.queryByText('That is no longer in Autopilot’s history.')).not.toBeInTheDocument()

    await act(async () => {
      give(
        activityPage({
          days: [
            {
              date: '2026-06-15',
              matches: [recorded, dryRunMatch(), autopilotMatch()],
              groups: [activityGroup()],
            },
          ],
        }),
      )
      await held
    })

    await waitFor(() => {
      expect(document.querySelector('[data-line="match:20"]')).toHaveAttribute('data-highlighted', 'true')
    })
    expect(screen.queryByText('That is no longer in Autopilot’s history.')).not.toBeInTheDocument()
  })

  it('reads earlier days on asking, and goes back to Activity from a routine', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage({ more: true, next: 12 }))
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Show earlier days' }))
    await waitFor(() => {
      expect(platform.autopilotActivity).toHaveBeenCalledWith(12, 100)
    })

    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    expect(screen.getByRole('region', { name: 'Snooze passing bot PRs' })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Activity' }))
    expect(screen.getByRole('heading', { name: 'Activity', level: 2 })).toBeInTheDocument()
  })

  it('says why a routine couldn’t be copied or deleted, and deletes nothing on Cancel', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.duplicateRoutine.mockRejectedValue(new Error('The routine is gone.'))
    platform.deleteRoutine.mockRejectedValue(new Error('The routines couldn’t be saved.'))
    renderTab()

    const list = await routineList()

    await userEvent.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Duplicate' }))
    expect(await screen.findByText('The routine is gone.')).toBeInTheDocument()

    await userEvent.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Delete…' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.deleteRoutine).not.toHaveBeenCalled()

    await userEvent.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Delete…' }))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(await screen.findByText('The routines couldn’t be saved.')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(screen.queryByText('The routines couldn’t be saved.')).not.toBeInTheDocument()
  })

  it('opens Settings → Autopilot', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Autopilot settings' }))

    expect(onSettings).toHaveBeenCalledOnce()
  })
})

describe('a routine', () => {
  it('shows its sentence, its mode, and what it watches as the inbox’s own rows, menus and all', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockResolvedValue([item()])
    renderTab()

    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))

    const pane = within(screen.getByRole('region', { name: 'Snooze passing bot PRs' }))

    expect(pane.getByText(/When checks pass on pull requests in Review requested/)).toBeInTheDocument()
    expect(pane.getByRole('radio', { name: 'Autopilot' })).toBeChecked()
    expect(pane.getByRole('switch', { name: 'Run this routine' })).toBeChecked()

    const watching = within(await pane.findByRole('list', { name: 'Watching' }))

    expect(platform.itemsByID).toHaveBeenCalledWith([1])
    expect(watching.getByRole('button', { name: 'More actions' })).toBeInTheDocument()

    await userEvent.click(watching.getByRole('button', { name: 'Add the widget cache' }))

    expect(await screen.findByRole('dialog', { name: 'Add the widget cache' })).toBeInTheDocument()
    // The sheet reads the pull request in an effect after it shows, as the issue sheet does.
    await waitFor(() => {
      expect(platform.openReview).toHaveBeenCalledWith(1)
    })
  })

  it('shows a Dependabot pull request no inbox list holds in its Watching list, with its chip and menus', async () => {
    const bump = item({
      title: 'Bump golang.org/x/net from 0.30.0 to 0.31.0',
      author: 'dependabot',
      authorIsBot: true,
      authorAssociation: 'NONE',
      assigned: false,
      reviewRequest: 'none',
    })

    platform.listRoutines.mockResolvedValue(routines())
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockResolvedValue([bump])
    platform.latestAutopilotMatches.mockResolvedValue([autopilotMatch()])
    renderTab()

    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))

    const pane = within(screen.getByRole('region', { name: 'Snooze passing bot PRs' }))
    const watching = within(await pane.findByRole('list', { name: 'Watching' }))

    expect(
      await watching.findByRole('button', { name: /^Autopilot: Snoozed · Snooze passing bot PRs\./ }),
    ).toBeInTheDocument()
    expect(platform.latestAutopilotMatches).toHaveBeenCalledWith([1])
    expect(watching.getByRole('button', { name: 'More actions' })).toBeInTheDocument()

    await userEvent.click(watching.getByRole('button', { name: bump.title }))

    // The sheet opens on the row's item and takes its title from its own read of it.
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    await waitFor(() => {
      expect(platform.openReview).toHaveBeenCalledWith(1)
    })
  })

  it('takes a routine that is on out of Dry run by its mode, after asking', async () => {
    platform.listRoutines.mockResolvedValue([routine({ mode: 'dryRun' })])
    platform.routineMatching.mockResolvedValue({ count: 4, pullRequests: 4, issues: 0, unknown: 0, incomplete: false })
    renderTab()

    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await userEvent.click(screen.getByRole('radio', { name: 'Ask first' }))

    const question = await screen.findByRole('dialog', { name: '4 pull requests match right now.' })

    expect(question).toHaveTextContent('Once “Snooze passing bot PRs” is in Ask first')
    await userEvent.click(within(question).getByRole('button', { name: 'Only what changes from now on' }))
    await waitFor(() => {
      expect(platform.setRoutineMode).toHaveBeenLastCalledWith(3, 'ask', 'fromNow')
    })
  })

  it('says what it watches is empty, and its own Activity', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(
      activityPage({ days: [{ date: '2026-06-15', matches: [], groups: [activityGroup()] }] }),
    )
    renderTab()

    await userEvent.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))

    const pane = within(screen.getByRole('region', { name: 'Snooze passing bot PRs' }))

    expect(await pane.findByText(/Nothing to watch right now/)).toBeInTheDocument()
    expect(pane.getByText('Snoozed 1')).toBeInTheDocument()
  })
})

describe('the builder in the tab', () => {
  it('starts a routine from scratch on the empty tab, in the mode new routines start in, and makes it off', async () => {
    const user = userEvent.setup()
    renderTab()

    await user.click(await screen.findByRole('button', { name: 'Start from scratch' }))

    expect(screen.getByRole('heading', { name: 'New routine' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('New routine')
    expect(
      within(screen.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Ask first' }),
    ).toBeChecked()

    const create = screen.getByRole('button', { name: 'Create routine' })

    await waitFor(() => {
      expect(create).toBeEnabled()
    })
    await user.click(create)

    await waitFor(() => {
      expect(platform.saveRoutine).toHaveBeenCalledOnce()
    })
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({
      id: 0,
      name: 'New routine',
      mode: 'ask',
      spec: { version: 1 },
    })
    expect(await screen.findByText(/Made “New routine”, switched off/)).toBeInTheDocument()
    expect(await screen.findByRole('region', { name: 'New routine' })).toBeInTheDocument()
    expect(platform.routineMatching).not.toHaveBeenCalled()
  })

  it('says why a new routine couldn’t be made, and Cancel leaves it', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.saveRoutine.mockRejectedValue(new Error('Another routine has that name.'))
    renderTab()

    await (await routineList()).findByRole('button', { name: /^Snooze passing bot PRs/ })
    await user.click(screen.getByRole('button', { name: 'New routine' }))
    expect(screen.getByRole('button', { name: 'New routine' })).toHaveAttribute('aria-current', 'page')

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Create routine' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: 'Create routine' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Another routine has that name.')

    await user.type(screen.getByRole('textbox', { name: 'Name' }), ' 2')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(await screen.findByRole('heading', { name: 'Activity' })).toBeInTheDocument()
  })

  it('asks before leaving unsaved changes, keeps them on Keep editing, and keeps them across a visit elsewhere', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    const list = await routineList()

    await user.click(list.getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), ' now')

    await user.click(screen.getByRole('button', { name: 'Activity' }))
    const ask = await screen.findByRole('dialog', { name: 'Discard your changes?' })

    expect(ask).toHaveTextContent('“Snooze passing bot PRs” has changes that aren’t saved.')
    await user.click(within(ask).getByRole('button', { name: 'Keep editing' }))
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Snooze passing bot PRs now')

    // Choosing the same routine again keeps the draft.
    await user.click(list.getByRole('button', { name: /^Snooze passing bot PRs/ }))
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Snooze passing bot PRs now')

    await user.click(list.getByRole('button', { name: /^Investigate new bugs/ }))
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Discard your changes?' })).getByRole('button', {
        name: 'Discard changes',
      }),
    )
    expect(await screen.findByRole('region', { name: 'Investigate new bugs' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Investigate new bugs')

    // Another account's routine shows no builder.
    await user.click(list.getByRole('button', { name: /^Someone else’s/ }))
    expect(screen.queryByRole('textbox', { name: 'Name' })).not.toBeInTheDocument()
  })

  it('asks of a new routine with changes too', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await (await routineList()).findByRole('button', { name: /^Snooze passing bot PRs/ })
    await user.click(screen.getByRole('button', { name: 'New routine' }))
    await user.clear(screen.getByRole('textbox', { name: 'Name' }))
    await user.click(screen.getByRole('button', { name: 'Activity' }))

    const ask = await screen.findByRole('dialog', { name: 'Discard your changes?' })

    expect(ask).toHaveTextContent('“The new routine” hasn’t been created yet.')
    await user.click(within(ask).getByRole('button', { name: 'Discard changes' }))
    expect(await screen.findByRole('heading', { name: 'Activity' })).toBeInTheDocument()
  })

  it('saves a change to what an enabled routine watches only after asking about what it matches now', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 3, pullRequests: 3, issues: 0, unknown: 1, incomplete: false })
    renderTab()

    await user.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.selectOptions(await screen.findByRole('combobox', { name: 'Trigger' }), 'appears')

    const save = screen.getByRole('button', { name: 'Save' })

    await waitFor(() => {
      expect(save).toBeEnabled()
    })
    await user.click(save)

    const question = await screen.findByRole('dialog', { name: '3 pull requests match right now.' })

    expect(question).toHaveTextContent('Once the change to “Snooze passing bot PRs” is saved, it can act on them too')
    expect(question).toHaveTextContent('1 more item can’t be told yet')
    expect(platform.routineMatching.mock.lastCall?.[0]).toMatchObject({
      id: 3,
      spec: { trigger: { event: 'appears' } },
    })
    await user.click(within(question).getByRole('button', { name: 'Only what changes from now on' }))

    await waitFor(() => {
      expect(platform.saveRoutine).toHaveBeenCalledOnce()
    })
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({
      id: 3,
      current: 'fromNow',
      spec: { trigger: { event: 'appears' } },
    })
    expect(await screen.findByText('Saved “Snooze passing bot PRs”.')).toBeInTheDocument()
  })

  it('saves at once when nothing matches now, or when what it watches didn’t change', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await user.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.selectOptions(await screen.findByRole('combobox', { name: 'Trigger' }), 'appears')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(platform.saveRoutine).toHaveBeenCalledWith(expect.objectContaining({ id: 3, current: 'run' }))
    })
    expect(platform.routineMatching).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await user.type(screen.getByRole('spinbutton', { name: 'Runs a day' }), '4')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      expect(platform.saveRoutine).toHaveBeenCalledTimes(2)
    })
    expect(platform.routineMatching).toHaveBeenCalledOnce()
  })

  it('discards a routine’s changes, and says why its mode stays in Dry run', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue([
      routine({
        mode: 'dryRun',
        dryRunOnly: ['agent'],
        spec: { ...routine().spec, steps: [{ agent: { agent: 'inherit', prompt: { itemDefault: true } } }] },
      }),
    ])
    renderTab()

    await user.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    expect(
      await screen.findByText(`Agent steps run in Dry run until this version of ${brand.name} can run agents.`),
    ).toBeInTheDocument()

    await user.type(screen.getByRole('textbox', { name: 'Name' }), '!')
    await user.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Snooze passing bot PRs')

    await user.click(screen.getByRole('button', { name: 'Edit in Prompts' }))
    expect(onEditPrompts).toHaveBeenCalledOnce()
  })

  it('keeps a new routine’s draft when New routine is clicked again', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await (await routineList()).findByRole('button', { name: /^Snooze passing bot PRs/ })
    await user.click(screen.getByRole('button', { name: 'New routine' }))
    await user.type(screen.getByRole('textbox', { name: 'Name' }), ' for bugs')
    await user.click(screen.getByRole('button', { name: 'New routine' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('New routine for bugs')
  })

  it('shows each routine’s own test, never another’s', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    const list = await routineList()

    await user.click(list.getByRole('button', { name: /^Snooze passing bot PRs/ }))

    const test = await screen.findByRole('button', { name: /^Test/ })

    await waitFor(() => {
      expect(test).toBeEnabled()
    })
    await user.click(test)
    expect(await screen.findByText(/matches now$/)).toBeInTheDocument()

    await user.click(list.getByRole('button', { name: /^Investigate new bugs/ }))
    expect(await screen.findByRole('region', { name: 'Investigate new bugs' })).toBeInTheDocument()
    expect(await screen.findByText(/^Not tried yet/)).toBeInTheDocument()
    expect(screen.queryByText(/matches now$/)).not.toBeInTheDocument()
  })

  it('says why a saved routine’s change wasn’t saved in its builder, as a new one’s, not in a toast', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.saveRoutine.mockRejectedValue(new Error('Another routine has that name.'))
    renderTab()

    await user.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), ' now')

    const save = screen.getByRole('button', { name: 'Save' })

    await waitFor(() => {
      expect(save).toBeEnabled()
    })
    await user.click(save)

    const pane = screen.getByRole('region', { name: 'Snooze passing bot PRs' })

    expect(await within(pane).findByRole('alert')).toHaveTextContent('Another routine has that name.')
    expect(screen.getAllByText('Another routine has that name.')).toHaveLength(1)

    await user.type(screen.getByRole('textbox', { name: 'Name' }), '!')
    expect(within(pane).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('saves nothing more with ⌘S while the question a save asked is open', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 3, pullRequests: 3, issues: 0, unknown: 0, incomplete: false })
    renderTab()

    await user.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.selectOptions(await screen.findByRole('combobox', { name: 'Trigger' }), 'appears')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await screen.findByRole('dialog', { name: '3 pull requests match right now.' })

    fireEvent.keyDown(document.body, { key: 's', metaKey: true })
    expect(platform.routineMatching).toHaveBeenCalledOnce()
    expect(platform.saveRoutine).not.toHaveBeenCalled()
  })

  it('asks before a save that changes what the routine fires once per', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 2, pullRequests: 2, issues: 0, unknown: 0, incomplete: false })
    renderTab()

    await user.click((await routineList()).getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.selectOptions(await screen.findByRole('combobox', { name: 'Once per' }), 'day')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    })
    await user.click(screen.getByRole('button', { name: 'Save' }))

    expect(await screen.findByRole('dialog', { name: '2 pull requests match right now.' })).toBeInTheDocument()
    expect(platform.saveRoutine).not.toHaveBeenCalled()
  })
})

// open is routine 3's match id on item 1, still open in state, waiting on blockedBy.
function open(
  id: number,
  state: 'waiting' | 'blocked' | 'queued',
  blockedBy: AutopilotMatch['blockedBy'],
  words: string,
  more: Partial<AutopilotMatch> = {},
): AutopilotMatch {
  return autopilotMatch({
    id,
    state,
    blockedBy,
    reason: '',
    words,
    steps: [],
    effects: [],
    closedAt: null,
    createdAt: '2026-06-15T11:50:00Z',
    ...more,
  })
}

// needsYouSection returns the queries scoped to Activity's Needs you, once it shows.
async function needsYouSection() {
  return within(await screen.findByRole('region', { name: 'Needs you' }))
}

describe('Needs you', () => {
  it('asks for the OK of what waits, runs or skips it, and lists what is queued', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([
      open(21, 'waiting', 'ask', 'Waits for your OK, as Ask first does.'),
      open(22, 'queued', 'slot', 'Starts as soon as an agent slot frees.', { item: itemRef({ id: 2, number: 9 }) }),
    ])
    renderTab()

    const needs = await needsYouSection()
    const card = within(needs.getByRole('listitem', { name: 'Snooze passing bot PRs on example-repo #7' }))

    expect(card.getByText('Waits for your OK, as Ask first does.')).toBeInTheDocument()
    expect(
      within(screen.getByRole('region', { name: 'Queued' })).getByText(/Starts as soon as an agent slot frees/),
    ).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Run' }))
    expect(platform.approveMatches).toHaveBeenCalledWith([21])
    expect(await screen.findByText('Snooze passing bot PRs runs once it can.')).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Skip this commit' }))
    expect(platform.skipMatch).toHaveBeenCalledWith(21)
    await waitFor(() => {
      expect(platform.autopilotNeedsYou).toHaveBeenCalledTimes(3)
    })
  })

  it('shows a held start’s files as the hold dialog does, and Run confirms them at their commit', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([
      open(23, 'waiting', 'hold', 'Waits for you: the pull request changes agent configuration.', {
        hold: autopilotHold(),
      }),
    ])
    renderTab()

    const card = within((await needsYouSection()).getByRole('listitem', { name: /example-repo #7/ }))

    expect(card.getByText('This pull request changes agent configuration')).toBeInTheDocument()
    expect(card.getByRole('list', { name: 'Agent configuration it changes' })).toBeInTheDocument()
    expect(card.getByText(/Claude loads these files the moment it starts/)).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Run on a1b2c3d' }))
    expect(platform.approveMatches).toHaveBeenCalledWith([23])
  })

  it('says how many files a worktree has uncommitted, and runs there anyway on asking', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([
      open(24, 'waiting', 'dirty_worktree', 'Waits for you: its worktree has uncommitted changes.', {
        hold: autopilotHold({ reason: 'dirty', files: [], worktree: 'pr-88', dirty: 3 }),
      }),
    ])
    renderTab()

    const card = within((await needsYouSection()).getByRole('listitem', { name: /example-repo #7/ }))

    expect(card.getByText(/pr-88 has 3 uncommitted files/)).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Run in pr-88 anyway' }))
    expect(platform.approveMatches).toHaveBeenCalledWith([24])
  })

  it('clones a repository a match found no clone of, after asking, then runs it', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([
      open(25, 'waiting', 'no_clone', 'Waits for a clone of acme-org/example-repo on this Mac.'),
    ])
    platform.cloneTarget.mockResolvedValue(cloneTarget({ cloned: false }))
    renderTab()

    const card = within((await needsYouSection()).getByRole('listitem', { name: /example-repo #7/ }))

    expect(card.queryByRole('button', { name: 'Run' })).not.toBeInTheDocument()
    await userEvent.click(card.getByRole('button', { name: 'Clone and run' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Clone acme-org/example-repo?' }))

    expect(dialog.getByText(/acme-org\/example-repo ~\/projects\/example-repo/)).toBeInTheDocument()
    expect(platform.cloneRepo).not.toHaveBeenCalled()

    await userEvent.click(dialog.getByRole('button', { name: 'Clone and run' }))

    expect(platform.cloneRepo).toHaveBeenCalledWith(1)
    await waitFor(() => {
      expect(platform.approveMatches).toHaveBeenCalledWith([25])
    })
    expect(
      await screen.findByText('Cloned acme-org/example-repo. Snooze passing bot PRs runs once it can.'),
    ).toBeInTheDocument()
  })

  it('runs at once where the repository was cloned since, and says why it can’t clone onto something there', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([
      open(25, 'waiting', 'no_clone', 'Waits for a clone of acme-org/example-repo on this Mac.'),
    ])
    platform.cloneTarget
      .mockResolvedValueOnce(cloneTarget())
      .mockResolvedValueOnce(cloneTarget({ cloned: false, taken: true }))
    renderTab()

    const card = within((await needsYouSection()).getByRole('listitem', { name: /example-repo #7/ }))

    await userEvent.click(card.getByRole('button', { name: 'Clone and run' }))
    await waitFor(() => {
      expect(platform.approveMatches).toHaveBeenCalledWith([25])
    })
    expect(platform.cloneRepo).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Clone and run' }))
    expect(
      await screen.findByText(
        'Something is already at ~/projects/example-repo, so acme-org/example-repo can’t be cloned there.',
      ),
    ).toBeInTheDocument()
    expect(platform.cloneRepo).not.toHaveBeenCalled()
  })

  it('checks this Mac again for what can’t start yet, and skips or opens it', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([
      open(26, 'blocked', 'not_ready', 'Can’t start: Claude isn’t installed on this Mac.'),
    ])
    renderTab()

    const needs = await needsYouSection()
    const card = within(within(needs.getByRole('list', { name: 'Can’t start yet' })).getByRole('listitem'))

    expect(card.getByText('Can’t start: Claude isn’t installed on this Mac.')).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Check again' }))
    expect(platform.checkAutopilotReadiness).toHaveBeenCalledOnce()

    await userEvent.click(card.getByRole('button', { name: 'Skip this commit' }))
    expect(platform.skipMatch).toHaveBeenCalledWith(26)

    await userEvent.click(card.getByRole('button', { name: 'Open example-repo #7' }))
    await waitFor(() => {
      expect(platform.routineWatching).toHaveBeenCalled()
    })
  })

  it('says what a GitHub action would send, and Send anyway accepts the warnings it shows', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([
      open(27, 'waiting', 'warning', 'Updating a Dependabot branch stops Dependabot from rebasing it.', {
        steps: [githubStep({ actions: ['reply', 'updateBranch'], sent: 1 })],
        step: 0,
      }),
    ])
    renderTab()

    const card = within((await needsYouSection()).getByRole('listitem', { name: /example-repo #7/ }))

    expect(card.getByText('Updating a Dependabot branch stops Dependabot from rebasing it.')).toBeInTheDocument()
    expect(card.getByText('Update branch')).toBeInTheDocument()
    expect(card.queryByRole('button', { name: 'Run' })).not.toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Send anyway' }))
    expect(platform.approveMatches).toHaveBeenCalledWith([27])
    expect(await screen.findByText('Snooze passing bot PRs sends it once your limits let it.')).toBeInTheDocument()
  })

  it('asks to merge a fix whose checks passed, showing the verdict, and opens the sheet for a review GitHub wants', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    const steps = [
      githubStep({ kind: 'agent', label: 'Run a coding agent', status: 'done', outcome: 'pushed', actions: [] }),
      githubStep({
        kind: 'waitForChecks',
        label: 'Wait for the checks',
        status: 'done',
        outcome: 'passed',
        verdict: 'fixed',
        note: 'Fixed: checks pass on 4f2a9c1.',
        actions: [],
      }),
      githubStep({ kind: 'merge', label: 'Merge it', merge: 'Merges it (squash).', actions: [] }),
    ]
    platform.autopilotNeedsYou.mockResolvedValue([
      open(30, 'waiting', 'ask', 'Its checks passed: waiting for your OK to merge it.', { steps, step: 2 }),
      open(31, 'waiting', 'review_required', 'GitHub wants an approving review before it merges this.', {
        steps,
        step: 2,
        item: itemRef({ id: 2, number: 9 }),
      }),
    ])
    renderTab()

    const needs = await needsYouSection()
    const ask = within(needs.getByRole('listitem', { name: /example-repo #7/ }))

    expect(ask.getByText('Fixed: checks pass on 4f2a9c1.')).toBeInTheDocument()
    expect(ask.getByText('Merges it (squash).')).toBeInTheDocument()
    expect(ask.queryByRole('button', { name: 'Approve and merge…' })).not.toBeInTheDocument()

    await userEvent.click(ask.getByRole('button', { name: 'Merge' }))
    expect(platform.approveMatches).toHaveBeenCalledWith([30])

    const review = within(needs.getByRole('listitem', { name: /example-repo #9/ }))
    await userEvent.click(review.getByRole('button', { name: 'Approve and merge…' }))
    await waitFor(() => {
      expect(platform.routineWatching).toHaveBeenCalled()
    })
    expect(platform.approveMatches).toHaveBeenCalledTimes(1)
  })

  it('shows the agent’s review, and posts it, as a comment, or skips it for the sheet', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    const review = { verdict: 'approve' as const, body: 'Looks good to me.', comments: [] }
    platform.autopilotNeedsYou.mockResolvedValue([
      open(32, 'waiting', 'ask', 'The agent’s review is ready: waiting for your OK to post it.', {
        steps: [
          githubStep({
            kind: 'agent',
            label: 'Run a coding agent',
            status: 'done',
            outcome: 'approve',
            review,
            actions: [],
          }),
          githubStep({
            kind: 'postReview',
            label: 'Post the agent’s review',
            reviewAs: 'verdict',
            allowApprove: true,
            actions: [],
          }),
        ],
        step: 1,
      }),
    ])
    renderTab()

    const card = within((await needsYouSection()).getByRole('listitem', { name: /example-repo #7/ }))

    expect(card.getByText('Looks good to me.')).toBeInTheDocument()
    expect(card.getByText(/Approves/)).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Post approval' }))
    expect(platform.approveMatches).toHaveBeenCalledWith([32])

    await userEvent.click(card.getByRole('button', { name: 'Post as comment' }))
    expect(platform.postReviewAsComment).toHaveBeenCalledWith(32)

    await userEvent.click(card.getByRole('button', { name: 'Edit in the sheet' }))
    expect(platform.skipMatch).toHaveBeenCalledWith(32)
    await waitFor(() => {
      expect(platform.routineWatching).toHaveBeenCalled()
    })
  })

  it('gathers a routine’s backlog in one card, whose Run on them lets every one go', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    const rebase = { steps: [githubStep()], step: 0 }
    const backlog = [21, 22, 23, 24, 25, 26, 27].map((id) =>
      open(id, 'waiting', 'backlog', '7 matched at once: run on them from Activity.', {
        ...rebase,
        item: itemRef({ id, number: id }),
      }),
    )
    platform.autopilotNeedsYou.mockResolvedValue(backlog)
    platform.approveAutopilotBacklog.mockResolvedValue(backlog)
    renderTab()

    const card = within(
      (await needsYouSection()).getByRole('listitem', { name: 'Snooze passing bot PRs: 7 items matched at once' }),
    )

    expect(
      card.getByText(
        '7 items matched at once, more than Snooze passing bot PRs takes on by itself: Ask Dependabot to rebase, for each.',
      ),
    ).toBeInTheDocument()
    expect(card.getByText('and 2 more')).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Run on them' }))
    expect(platform.approveAutopilotBacklog).toHaveBeenCalledExactlyOnceWith(3)
    expect(await screen.findByText('Snooze passing bot PRs runs on 7 items, at Autopilot’s pace.')).toBeInTheDocument()

    await userEvent.click(card.getByRole('button', { name: 'Skip them' }))
    expect(platform.skipAutopilotBacklog).toHaveBeenCalledExactlyOnceWith(3, [21, 22, 23, 24, 25, 26, 27])
    expect(platform.skipMatch).not.toHaveBeenCalled()
    expect(
      await screen.findByText('Skipped them. Snooze passing bot PRs waits for the next time each fires.'),
    ).toBeInTheDocument()
  })

  it('says when Skip them found some of the backlog it showed moved on already', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    const backlog = [21, 22, 23].map((id) =>
      open(id, 'waiting', 'backlog', '3 matched at once: run on them from Activity.', {
        item: itemRef({ id, number: id }),
      }),
    )
    platform.autopilotNeedsYou.mockResolvedValue(backlog)
    platform.skipAutopilotBacklog.mockResolvedValue(backlog.slice(0, 2))
    renderTab()

    const card = within(
      (await needsYouSection()).getByRole('listitem', { name: 'Snooze passing bot PRs: 3 items matched at once' }),
    )

    await userEvent.click(card.getByRole('button', { name: 'Skip them' }))
    expect(platform.skipAutopilotBacklog).toHaveBeenCalledExactlyOnceWith(3, [21, 22, 23])
    expect(await screen.findByText('Skipped 2 items; 1 other had moved on already.')).toBeInTheDocument()
  })

  it('says across the top how many routines this Mac can’t run, and what one lacks in the list', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.getAutopilotReadiness.mockResolvedValue(
      readiness({
        checks: [readyCheck(), claudeMissing()],
        routines: [routineReady({ ready: false, problems: [claudeMissing()] })],
        notReady: 1,
      }),
    )
    renderTab()

    expect(await screen.findByText('This Mac can’t run 1 of your routines.')).toBeInTheDocument()
    expect((await routineList()).getByText('Not ready on this Mac: Claude · isn’t installed')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'See what’s missing' }))
    expect(onSettings).toHaveBeenCalledOnce()
  })

  it('marks the card of an open match a request names, not a line of the history', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotNeedsYou.mockResolvedValue([open(21, 'waiting', 'ask', 'Waits for your OK.')])
    renderTab({
      request: {
        seq: 1,
        target: { routineId: 3, matchId: 21, runId: 0, createdAt: '2026-06-15T11:50:00Z', activity: false },
      },
    })

    const card = (await needsYouSection()).getByRole('listitem', { name: /example-repo #7/ })

    await waitFor(() => {
      expect(card).toHaveAttribute('data-highlighted', 'true')
    })
    expect(screen.queryByText(/Activity has no line for that one/)).not.toBeInTheDocument()
    expect(screen.queryByText(/no longer in Autopilot’s history/)).not.toBeInTheDocument()
  })

  it('reads what waits again whenever Autopilot changes', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await routineList()
    expect(screen.queryByRole('region', { name: 'Needs you' })).not.toBeInTheDocument()

    platform.autopilotNeedsYou.mockResolvedValue([open(21, 'waiting', 'ask', 'Waits for your OK.')])
    act(() => {
      emit('autopilot:updated')
    })

    expect(await screen.findByRole('region', { name: 'Needs you' })).toBeInTheDocument()
  })
})

describe('Easy mode', () => {
  it('sets a template up through quick setup, asking only its blanks, in the mode new routines start in', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.listTemplates.mockResolvedValue([galleryTemplate({ needs: [], needsWords: '', suggests: 'auto' })])
    platform.saveRoutine.mockImplementation((d) =>
      Promise.resolve(
        routine({ id: 9, name: d.name, mode: d.mode ?? 'ask', enabled: false, templateKey: 'fix-dependabot-security' }),
      ),
    )
    renderTab({ stored: settingsOf({ autopilot: { ...settingsOf().autopilot, defaultMode: 'dryRun' } }) })

    await routineList()
    await user.click(screen.getByRole('button', { name: 'Templates' }))
    await user.click(await screen.findByRole('button', { name: /Fix Dependabot security failures/ }))

    const setup = within(await screen.findByRole('region', { name: 'Fix Dependabot security failures' }))

    expect(setup.getAllByRole('combobox')).toHaveLength(1)
    expect(setup.getByRole('combobox', { name: 'Where' })).toBeInTheDocument()
    expect(setup.getByText('Your account')).toBeInTheDocument()
    expect(
      within(setup.getByRole('radiogroup', { name: 'Mode' })).getByRole('radio', { name: 'Dry run' }),
    ).toBeChecked()
    expect(setup.getByText(/This template suggests Autopilot\. It starts in Dry run/)).toBeInTheDocument()
    expect(await setup.findByText('Ready to run here.')).toBeInTheDocument()

    const save = setup.getByRole('button', { name: 'Save' })

    await waitFor(() => {
      expect(save).toBeEnabled()
    })
    await user.click(save)

    await waitFor(() => {
      expect(platform.saveRoutine).toHaveBeenCalledOnce()
    })
    expect(platform.saveRoutine.mock.lastCall?.[0]).toMatchObject({
      id: 0,
      name: 'Fix Dependabot security failures',
      mode: 'dryRun',
      templateKey: 'fix-dependabot-security',
      spec: { watch: { sources: [{ search: { where: ['@me'] } }] } },
    })
    expect(await screen.findByText(/Made “Fix Dependabot security failures”, switched off/)).toBeInTheDocument()
  })

  it('says plainly what a template needs that this build can’t run yet, and opens it in the builder all the same', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await routineList()
    await user.click(screen.getByRole('button', { name: 'Templates' }))
    expect(await screen.findByText('Needs agents in the background')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Fix Dependabot security failures/ }))

    const setup = within(await screen.findByRole('region', { name: 'Fix Dependabot security failures' }))

    expect(setup.getByRole('alert')).toHaveTextContent('It needs agents in the background')
    expect(setup.getByRole('button', { name: 'Save' })).toBeDisabled()

    await user.click(setup.getByRole('button', { name: 'Customize' }))

    expect(screen.getByRole('heading', { name: 'New routine' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Fix Dependabot security failures')
  })

  it('offers a starter the library lost back, and reads the gallery again once it’s added', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.listTemplates.mockResolvedValue([
      galleryTemplate({
        prompts: [{ starter: 'fix-dependency-security', name: 'Fix dep security', inLibrary: false }],
      }),
    ])
    renderTab()

    await routineList()
    await user.click(screen.getByRole('button', { name: 'Templates' }))
    await user.click(await screen.findByRole('button', { name: /Fix Dependabot security failures/ }))
    await user.click(await screen.findByRole('button', { name: 'Add it again' }))

    expect(platform.addStarterPrompt).toHaveBeenCalledWith('fix-dependency-security')
    await waitFor(() => {
      expect(platform.listTemplates).toHaveBeenCalledTimes(2)
    })
  })

  it('opens the quick setup the first-run setup suggested', async () => {
    renderTab({ request: { seq: 1, target: activityTarget, template: 'snooze-passing-bot-prs' } })

    expect(await screen.findByRole('region', { name: 'Snooze passing bot PRs' })).toBeInTheDocument()
  })

  it('makes a routine from an item: the builder filled in, its suggestions a click away, the item at the top of Test', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab({ request: { seq: 1, target: activityTarget, fromItem: 7 } })

    expect(await screen.findByRole('heading', { name: 'New routine' })).toBeInTheDocument()
    expect(platform.routineFromItem).toHaveBeenCalledExactlyOnceWith(7)
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Dependabot’s pull requests in acme-org')

    const test = within(screen.getByRole('region', { name: 'Test' }))

    expect(test.getByRole('list', { name: 'The item it was made from' })).toHaveTextContent('acme-org/example-repo #7')
    expect(test.getByText('matches now, from what is stored')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /\+ .*minor/ }))

    expect(screen.queryByRole('button', { name: /\+ .*minor/ })).not.toBeInTheDocument()
  })

  it('offers to put a routine on Autopilot after clean approvals, and never changes its mode by itself', async () => {
    const user = userEvent.setup()
    const asking = routine({ mode: 'ask', figures: routineFigures({ graduate: true }) })
    platform.listRoutines.mockResolvedValue([asking])
    platform.setRoutineMode.mockResolvedValue(routine({ mode: 'auto' }))
    renderTab()

    const list = await routineList()

    expect(list.getByText('Put it on Autopilot?')).toBeInTheDocument()
    expect(platform.setRoutineMode).not.toHaveBeenCalled()

    await user.click(list.getByRole('button', { name: /^Snooze passing bot PRs/ }))
    await user.click(await screen.findByRole('button', { name: 'Not now' }))

    expect(platform.declineGraduate).toHaveBeenCalledExactlyOnceWith(3)
    expect(platform.setRoutineMode).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Put it on Autopilot' }))

    await waitFor(() => {
      expect(platform.setRoutineMode).toHaveBeenCalledWith(3, 'auto', expect.anything())
    })
  })

  it('imports routines after a preview that maps what this Mac lacks', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    platform.chooseFileToOpen.mockResolvedValue('/exports/routines.json')
    renderTab()

    await routineList()
    await user.click(screen.getByRole('button', { name: 'Import routines…' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Import routines?' }))

    expect(platform.previewRoutineImport).toHaveBeenCalledWith('/exports/routines.json')
    expect(dialog.getByText('Triage bugs')).toBeInTheDocument()
    expect(dialog.getByText(/added to your prompts/)).toBeInTheDocument()

    await user.selectOptions(dialog.getByRole('combobox', { name: /claude’s profile “work”/ }), 'personal')
    await user.click(dialog.getByRole('button', { name: 'Import 1 routine' }))

    await waitFor(() => {
      expect(platform.importRoutines).toHaveBeenCalledOnce()
    })
    expect(platform.importRoutines.mock.lastCall?.[0]).toEqual({
      path: '/exports/routines.json',
      sha256: 'a1b2c3',
      map: [{ kind: 'profile', agent: 'claude', name: 'work', starter: '', to: 'personal' }],
    })
    expect(await screen.findByText(/Imported 1 routine, switched off\. Added 1 prompt: Triage\./)).toBeInTheDocument()
  })

  it('exports a routine with the text of its prompts, where the user chose', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue([routine()])
    platform.chooseFileToSave.mockResolvedValue('/exports/snooze.json')
    renderTab()

    const list = await routineList()

    await user.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Export…' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Export “Snooze passing bot PRs”?' }))

    expect(dialog.getByRole('checkbox', { name: 'Include the text of the prompts they run' })).toBeChecked()
    await user.click(dialog.getByRole('button', { name: 'Choose where…' }))

    await waitFor(() => {
      expect(platform.exportRoutines).toHaveBeenCalledExactlyOnceWith('/exports/snooze.json', [3], true)
    })
    expect(await screen.findByText('Exported 1 routine.')).toBeInTheDocument()
  })
})

// nav returns the queries scoped to the tab's left side.
function nav() {
  return within(screen.getByRole('navigation', { name: 'Autopilot' }))
}

// sent are the messages Describe it sent as turns, in order.
function sent(): unknown[] {
  return platform.sendToDescribeIt.mock.calls.map(([message]) => message)
}

// chips are the words of what Describe it's composer holds for the next message, in
// order: none while it holds nothing.
function chips(): string[] {
  const list = screen.queryByRole('list', { name: 'Attached to your message' })

  return list === null
    ? []
    : within(list)
        .getAllByRole('listitem')
        .map((chip) => chip.textContent)
}

// named has this Mac name routines 3 and 4, and store item 7 as acme-org/example-repo#42,
// as the chips in Describe it's composer read them.
function named() {
  platform.routineNames.mockResolvedValue([
    { id: 3, name: 'Snooze passing bot PRs', deleted: false },
    { id: 4, name: 'Investigate new bugs', deleted: false },
  ])
  platform.itemsByID.mockResolvedValue([item({ id: 7, number: 42 })])
}

// carded has Describe it's conversation hold a turn with a card proposing a new routine,
// Pin failing PRs, which the user has dealt with as outcomes say.
function carded(outcomes: KitOutcome[] = []) {
  const name = 'Pin failing PRs'

  platform.describeItConversation.mockResolvedValue(
    describeConversation({
      id: '00000000-0000-4000-8000-000000000001',
      turns: [
        describeTurn({
          cards: [
            describeCard({
              proposal: { summary: 'Pins them.', routines: [{ name, mode: 'ask', spec: routineSpec() }] },
              checked: kitChecked({ routines: [kitCheckedRoutine({ name })] }),
              outcomes,
              settled: outcomes.length > 0,
            }),
          ],
        }),
      ],
    }),
  )
}

// askBox returns the queries scoped to the empty tab's box, once it shows.
async function askBox() {
  return within(await screen.findByRole('form', { name: 'Tell Autopilot what you want' }))
}

describe('the Autopilot tab’s Describe it', () => {
  it('sits above New routine in the nav, and opens with nothing asked of it', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await routineList()

    const places = nav()
      .getAllByRole('button')
      .map((b) => b.textContent)

    expect(places.indexOf('Describe it')).toBe(places.indexOf('New routine') - 1)
    expect(screen.queryByRole('region', { name: 'Describe it' })).not.toBeInTheDocument()

    await user.click(nav().getByRole('button', { name: 'Describe it' }))

    expect(screen.getByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    expect(nav().getByRole('button', { name: 'Describe it' })).toHaveAttribute('aria-current', 'page')
    expect(await screen.findByText(/Claude proposes the routines, prompts, and settings it needs/)).toBeInTheDocument()
    expect(sent()).toEqual([])
    expect(chips()).toEqual([])
  })

  it('counts the proposals from the user’s own Claude Code that wait, read again as they change', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    await routineList()
    await waitFor(() => {
      expect(platform.describeItStatus).toHaveBeenCalledOnce()
    })
    expect(nav().getByRole('button', { name: 'Describe it' })).not.toHaveTextContent(/\d/)

    platform.describeItStatus.mockResolvedValue(describeStatus({ pending: 2 }))
    act(() => {
      emit('describeit:proposals')
    })

    const counted = await nav().findByRole('button', {
      name: 'Describe it (2 proposals from your own Claude Code wait)',
    })

    expect(counted).toHaveTextContent('Describe it 2')
    expect(counted).toHaveAttribute('title', '2 proposals from your own Claude Code wait')

    platform.describeItStatus.mockResolvedValue(describeStatus({ pending: 0 }))
    act(() => {
      emit('describeit:proposals')
    })

    expect(await nav().findByRole('button', { name: 'Describe it' })).not.toHaveTextContent(/\d/)
  })

  it('asks what the user wants on the empty tab, under the intro and above the templates', async () => {
    renderTab()

    const box = await askBox()
    const form = screen.getByRole('form', { name: 'Tell Autopilot what you want' })
    const templates = await screen.findByRole('list', { name: 'Templates' })

    expect(screen.getByText(/Autopilot watches your pull requests/).compareDocumentPosition(form)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    )
    expect(form.compareDocumentPosition(templates)).toBe(Node.DOCUMENT_POSITION_FOLLOWING)
    expect(box.getByRole('textbox', { name: 'Tell Autopilot what you want' })).toBeEnabled()
    expect(box.getByRole('button', { name: 'Send' })).toBeDisabled()
    expect(box.getByText(/Claude sees what you type, how Autopilot works/)).toBeInTheDocument()
    expect(
      within(box.getByRole('list', { name: 'Examples' }))
        .getAllByRole('button')
        .map((b) => b.textContent),
    ).toEqual([
      'When Dependabot’s security scans fail, fix it in the background and tell me how it went',
      'Pin my pull requests when their checks fail',
      'Snooze passing bot PRs for a few days',
    ])
  })

  it('fills the box with an example, and sends it on asking, opening Describe it with the message', async () => {
    const user = userEvent.setup()
    renderTab()

    const box = await askBox()
    const text = box.getByRole('textbox', { name: 'Tell Autopilot what you want' })

    await user.click(box.getByRole('button', { name: 'Pin my pull requests when their checks fail' }))

    expect(text).toHaveValue('Pin my pull requests when their checks fail')
    expect(text).toHaveFocus()

    await user.type(text, ' twice')
    await user.click(box.getByRole('button', { name: 'Send' }))

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    await waitFor(() => {
      expect(sent()).toEqual([{ text: 'Pin my pull requests when their checks fail twice', attach: [] }])
    })
    expect(await screen.findByRole('button', { name: 'Stop' })).toBeInTheDocument()
    expect(nav().getByRole('button', { name: 'Describe it' })).toHaveAttribute('aria-current', 'page')
    expect(nav().getByText('No routines yet.')).toBeInTheDocument()
  })

  it('sends with ⌘↩, trimmed, and sends nothing blank', async () => {
    const user = userEvent.setup()
    renderTab()

    const box = await askBox()
    const text = box.getByRole('textbox', { name: 'Tell Autopilot what you want' })

    await user.type(text, '   ')
    await user.keyboard('{Meta>}{Enter}{/Meta}')

    expect(screen.queryByRole('region', { name: 'Describe it' })).not.toBeInTheDocument()

    await user.type(text, ' Snooze bot PRs  ')
    await user.keyboard('{Enter}')

    expect(screen.queryByRole('region', { name: 'Describe it' })).not.toBeInTheDocument()

    await user.keyboard('{Meta>}{Enter}{/Meta}')

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    await waitFor(() => {
      expect(sent()).toEqual([{ text: 'Snooze bot PRs', attach: [] }])
    })
  })

  it('opens Describe it from the empty tab without a message, its count showing while proposals wait', async () => {
    const user = userEvent.setup()
    platform.describeItStatus.mockResolvedValue(describeStatus({ pending: 1 }))
    renderTab()

    const box = await askBox()
    const open = await box.findByRole('button', {
      name: 'Open Describe it (1 proposal from your own Claude Code waits)',
    })

    await user.click(open)

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    expect(await screen.findByText(/Claude proposes the routines, prompts, and settings it needs/)).toBeInTheDocument()
    expect(sent()).toEqual([])
    expect(chips()).toEqual([])

    // Activity, with no routine yet, is the empty tab again.
    await user.click(nav().getByRole('button', { name: 'Activity' }))

    expect(await askBox()).toBeDefined()
    expect(screen.queryByRole('region', { name: 'Describe it' })).not.toBeInTheDocument()
  })

  it('holds the box back, with why and what to do, while Describe it can’t run a turn', async () => {
    platform.describeItStatus.mockResolvedValue(
      describeStatus({
        ready: false,
        problem: 'Claude Code isn’t signed in.',
        fix: 'Run claude auth login in a terminal.',
      }),
    )
    renderTab()

    const box = await askBox()

    expect(await box.findByText('Claude Code isn’t signed in.')).toBeInTheDocument()
    expect(box.getByText('Run claude auth login in a terminal.')).toBeInTheDocument()
    expect(box.getByRole('textbox', { name: 'Tell Autopilot what you want' })).toBeDisabled()
    expect(box.getByRole('textbox', { name: 'Tell Autopilot what you want' })).toHaveAccessibleDescription(
      'Claude Code isn’t signed in. Run claude auth login in a terminal.',
    )
    expect(box.getByRole('button', { name: 'Send' })).toBeDisabled()

    for (const example of within(box.getByRole('list', { name: 'Examples' })).getAllByRole('button')) {
      expect(example).toBeDisabled()
    }

    expect(box.queryByText(/Claude sees what you type/)).not.toBeInTheDocument()
    expect(box.getByRole('button', { name: 'Check again' })).toBeEnabled()
    // Describe it itself still opens, where its own box says the same.
    expect(box.getByRole('button', { name: 'Open Describe it' })).toBeEnabled()
  })

  it('reads Describe it’s status again on Check again, and takes a message once a turn can run', async () => {
    const user = userEvent.setup()
    platform.describeItStatus.mockResolvedValue(
      describeStatus({
        ready: false,
        problem: 'Profile default (Claude Code) isn’t signed in.',
        fix: 'Run `claude auth login`, then Check again.',
      }),
    )
    renderTab()

    const box = await askBox()
    const text = box.getByRole('textbox', { name: 'Tell Autopilot what you want' })

    expect(await box.findByText('Run `claude auth login`, then Check again.')).toBeInTheDocument()
    expect(text).toBeDisabled()

    // The user signs in, then asks again.
    platform.describeItStatus.mockResolvedValue(describeStatus())
    await user.click(box.getByRole('button', { name: 'Check again' }))

    await waitFor(() => {
      expect(text).toBeEnabled()
    })
    expect(platform.describeItStatus).toHaveBeenCalledTimes(2)
    expect(box.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument()
    expect(box.getByText(/Claude sees what you type/)).toBeInTheDocument()
  })

  it('lets the user write and send while Describe it’s status is still being read', async () => {
    const user = userEvent.setup()
    platform.describeItStatus.mockReturnValue(new Promise(() => undefined))
    renderTab()

    const box = await askBox()

    await user.type(box.getByRole('textbox', { name: 'Tell Autopilot what you want' }), 'Pin my PRs')
    await user.click(box.getByRole('button', { name: 'Send' }))

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    await waitFor(() => {
      expect(sent()).toEqual([{ text: 'Pin my PRs', attach: [] }])
    })
  })

  it('holds the box back, saying why, when Describe it’s status can’t be read', async () => {
    platform.describeItStatus.mockRejectedValue(new Error('Describe it isn’t ready in this build yet.'))
    renderTab()

    const box = await askBox()

    expect(await box.findByText('Describe it isn’t ready in this build yet.')).toBeInTheDocument()
    expect(box.getByRole('textbox', { name: 'Tell Autopilot what you want' })).toBeDisabled()
    expect(box.getByRole('button', { name: 'Send' })).toBeDisabled()
  })

  it('changes a routine with Claude from its ⋯ menu and its pane, with the routine attached', async () => {
    const user = userEvent.setup()
    named()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab()

    const list = await routineList()

    await user.click(list.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Change with Claude…' }))

    expect(screen.getByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    await waitFor(() => {
      expect(chips()).toEqual(['Snooze passing bot PRs'])
    })
    expect(sent()).toEqual([])

    await user.click(list.getByRole('button', { name: /^Investigate new bugs/ }))
    await user.click(await screen.findByRole('button', { name: 'Change with Claude…' }))

    await waitFor(() => {
      expect(chips()).toEqual(['Snooze passing bot PRs', 'Investigate new bugs'])
    })

    // Another account's routine can't change here, nor with Claude.
    await user.click(list.getByRole('button', { name: 'More for Someone else’s' }))
    expect(await screen.findByRole('menuitem', { name: 'Change with Claude…' })).toBeDisabled()
  })

  it('opens Describe it with the item a request attaches, once per request', async () => {
    named()
    const first: AutopilotRequest = { seq: 1, target: activityTarget, describe: { itemId: 7 } }
    const view = render(<Tab request={first} />, { wrapper: at() })

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    await waitFor(() => {
      expect(chips()).toEqual(['acme-org/example-repo#42'])
    })
    expect(platform.itemsByID).toHaveBeenCalledExactlyOnceWith([7])
    expect(platform.routineFromItem).not.toHaveBeenCalled()
    expect(sent()).toEqual([])

    view.rerender(<Tab request={{ ...first }} />)

    expect(platform.itemsByID).toHaveBeenCalledOnce()

    view.rerender(<Tab request={{ seq: 2, target: activityTarget, describe: { routineId: 3, itemId: 7 } }} />)

    await waitFor(() => {
      expect(chips()).toEqual(['acme-org/example-repo#42', 'Snooze passing bot PRs'])
    })
    expect(platform.itemsByID).toHaveBeenCalledTimes(2)
  })

  it('drops what it was opened with once left, and keeps a message being written', async () => {
    const user = userEvent.setup()
    named()
    platform.listRoutines.mockResolvedValue(routines())
    renderTab({ request: { seq: 1, target: activityTarget, describe: { itemId: 7 } } })

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    await waitFor(() => {
      expect(chips()).toEqual(['acme-org/example-repo#42'])
    })
    await user.type(screen.getByRole('textbox', { name: 'Tell Autopilot what you want' }), 'only on weekdays')

    await user.click(nav().getByRole('button', { name: 'Activity' }))

    expect(await screen.findByRole('heading', { name: 'Activity' })).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Describe it' })).not.toBeInTheDocument()

    await user.click(nav().getByRole('button', { name: 'Describe it' }))

    // What it was opened with acted once: the item is attached once, and not read again.
    expect(chips()).toEqual(['acme-org/example-repo#42'])
    expect(platform.itemsByID).toHaveBeenCalledOnce()
    expect(screen.getByRole('textbox', { name: 'Tell Autopilot what you want' })).toHaveValue('only on weekdays')
  })

  it('asks first while the builder has unsaved changes, and opens with what was asked once they go', async () => {
    const user = userEvent.setup()
    named()
    platform.listRoutines.mockResolvedValue(routines())
    const view = render(<Tab />, { wrapper: at() })

    await routineList()
    await user.click(nav().getByRole('button', { name: 'New routine' }))
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), ' for bugs')

    // Describe it from the nav: Keep editing stays.
    await user.click(nav().getByRole('button', { name: 'Describe it' }))
    await user.click(await screen.findByRole('button', { name: 'Keep editing' }))

    expect(screen.getByRole('heading', { name: 'New routine' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('New routine for bugs')

    // An item's request waits for the answer, and Discard changes opens it with the item.
    view.rerender(<Tab request={{ seq: 1, target: activityTarget, describe: { itemId: 7 } }} />)

    const dialog = within(await screen.findByRole('dialog', { name: 'Discard your changes?' }))

    expect(dialog.getByText('“New routine for bugs” hasn’t been created yet.')).toBeInTheDocument()
    expect(screen.queryByRole('region', { name: 'Describe it' })).not.toBeInTheDocument()

    await user.click(dialog.getByRole('button', { name: 'Discard changes' }))

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    await waitFor(() => {
      expect(chips()).toEqual(['acme-org/example-repo#42'])
    })
  })

  it('drops what a request asked once its question is answered Keep editing', async () => {
    const user = userEvent.setup()
    named()
    platform.listRoutines.mockResolvedValue(routines())
    const view = render(<Tab />, { wrapper: at() })

    await routineList()
    await user.click(nav().getByRole('button', { name: 'New routine' }))
    await user.type(await screen.findByRole('textbox', { name: 'Name' }), ' for bugs')
    // The name is left first, as a click elsewhere would leave it: a field still focused as
    // the request's question takes focus has user-event send its change event, in an act of
    // its own, while React runs the dialog's effects, which React reports as a component
    // suspended inside an act that wasn't awaited.
    await user.tab()

    view.rerender(<Tab request={{ seq: 1, target: activityTarget, describe: { itemId: 7 } }} />)
    await user.click(await screen.findByRole('button', { name: 'Keep editing' }))
    await user.click(nav().getByRole('button', { name: 'Describe it' }))
    await user.click(await screen.findByRole('button', { name: 'Discard changes' }))

    expect(await screen.findByRole('region', { name: 'Describe it' })).toBeInTheDocument()
    expect(await screen.findByText(/Claude proposes the routines, prompts, and settings it needs/)).toBeInTheDocument()
    expect(platform.itemsByID).not.toHaveBeenCalledWith([7])
    expect(chips()).toEqual([])
  })

  it('opens the builder on a card’s routine, and leaving it asks first', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue(routines())
    carded()
    renderTab()

    await routineList()
    await user.click(nav().getByRole('button', { name: 'Describe it' }))
    await user.click(await screen.findByRole('button', { name: 'Edit in builder' }))

    expect(await screen.findByRole('heading', { name: 'New routine' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Pin failing PRs')
    expect(screen.queryByRole('region', { name: 'Describe it' })).not.toBeInTheDocument()
    expect(platform.settleDescribeItCard).toHaveBeenCalledWith(
      expect.objectContaining({ part: 'routine', index: 0, kind: 'builder' }),
    )

    await user.click(nav().getByRole('button', { name: 'Describe it' }))

    expect(await screen.findByRole('dialog', { name: 'Discard your changes?' })).toBeInTheDocument()
  })

  it('shows the routine a card added, reading the routines again when the list doesn’t hold it yet', async () => {
    const user = userEvent.setup()
    platform.listRoutines.mockResolvedValue([routine({ id: 4, name: 'Investigate new bugs' })])
    carded([kitOutcome({ routineId: 3 })])
    renderTab()

    await routineList()
    await user.click(nav().getByRole('button', { name: 'Describe it' }))
    await waitFor(() => {
      expect(platform.listRoutines).toHaveBeenCalledOnce()
    })

    platform.listRoutines.mockResolvedValue(routines())
    await user.click(await screen.findByRole('button', { name: 'Show it' }))

    expect(await screen.findByRole('region', { name: 'Snooze passing bot PRs' })).toBeInTheDocument()
    expect(platform.listRoutines).toHaveBeenCalledTimes(2)

    // One the list holds shows at once, with nothing read again.
    await user.click(nav().getByRole('button', { name: 'Describe it' }))
    await user.click(screen.getByRole('button', { name: 'Show it' }))

    expect(screen.getByRole('region', { name: 'Snooze passing bot PRs' })).toBeInTheDocument()
    expect(platform.listRoutines).toHaveBeenCalledTimes(2)
  })
})
