import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Activity, useState } from 'react'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import type { AutopilotRequest } from '../../lib/autopilot'
import { useIssueDrafts } from '../../lib/issueDrafts'
import { useReviewDrafts } from '../../lib/reviewDrafts'
import { useAutopilot } from '../../lib/useAutopilot'
import { useInbox } from '../../lib/useInbox'
import type { Settings } from '../../platform'
import { at } from '../../test/clock'
import {
  activityGroup,
  activityPage,
  agents,
  autopilotEngine,
  autopilotMatch,
  dryRunMatch,
  issue,
  item,
  itemRef,
  leftAlone,
  profile,
  prompts,
  routine,
  routineFigures,
  settings as settingsOf,
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
  it('explains Autopilot in its two sentences while there is no routine, and nothing more', async () => {
    renderTab()

    expect(await screen.findByText(/Autopilot watches your pull requests and issues/)).toBeInTheDocument()
    expect(screen.getByText(/Its routines can tell you, tidy your inbox, or start a coding agent/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /routine/i })).not.toBeInTheDocument()
    expect(screen.queryByRole('switch')).not.toBeInTheDocument()
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
    platform.routineMatching.mockResolvedValue({ count: 3, pullRequests: 3, issues: 0, unknown: 0 })
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
    platform.routineMatching.mockResolvedValue({ count: 1, pullRequests: 0, issues: 1, unknown: 0 })
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

    platform.routineMatching.mockResolvedValue({ count: 0, pullRequests: 0, issues: 0, unknown: 0 })
    await userEvent.click(list.getByRole('switch', { name: 'Investigate new bugs' }))
    await waitFor(() => {
      expect(platform.setRoutineEnabled).toHaveBeenLastCalledWith(4, true, 'run')
    })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('shows a refusal in its own words', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.routineMatching.mockResolvedValue({ count: 2, pullRequests: 2, issues: 0, unknown: 0 })
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
    platform.routineMatching.mockResolvedValue({ count: 2, pullRequests: 2, issues: 0, unknown: 0 })
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

  it('takes a routine that is on out of Dry run by its mode, after asking', async () => {
    platform.listRoutines.mockResolvedValue([routine({ mode: 'dryRun' })])
    platform.routineMatching.mockResolvedValue({ count: 4, pullRequests: 4, issues: 0, unknown: 0 })
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
    platform.routineMatching.mockResolvedValue({ count: 3, pullRequests: 3, issues: 0, unknown: 1 })
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
    expect(await screen.findByText('Agent steps run in Dry run until this Mac can run agents.')).toBeInTheDocument()

    await user.type(screen.getByRole('textbox', { name: 'Name' }), '!')
    await user.click(screen.getByRole('button', { name: 'Discard' }))
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveValue('Snooze passing bot PRs')

    await user.click(screen.getByRole('button', { name: 'Edit in Prompts' }))
    expect(onEditPrompts).toHaveBeenCalledOnce()
  })
})
