import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

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
  item,
  itemRef,
  leftAlone,
  profile,
  prompts,
  routine,
  settings as settingsOf,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import type { LaunchOptions } from '../ItemRow'
import { AutopilotRowsContext } from './AutopilotChip'
import AutopilotView from './AutopilotView'
import type { RowsKit } from './WatchingList'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

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
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(
      activityPage({
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
      }),
    )
    renderTab()

    const today = within(await screen.findByRole('list', { name: 'Today' }))

    await userEvent.click(today.getByRole('button', { name: 'Let it act again' }))
    expect(platform.letRoutineActAgain).toHaveBeenCalledExactlyOnceWith(3, 1)
    expect(await screen.findByText('Snooze passing bot PRs can act on it again.')).toBeInTheDocument()

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
    expect(platform.autopilotActivity).toHaveBeenCalledWith(0, 200)
  })

  it('reads earlier days on asking, and goes back to Activity from a routine', async () => {
    platform.listRoutines.mockResolvedValue(routines())
    platform.autopilotActivity.mockResolvedValue(activityPage({ more: true, next: 12 }))
    renderTab()

    await userEvent.click(await screen.findByRole('button', { name: 'Show earlier days' }))
    await waitFor(() => {
      expect(platform.autopilotActivity).toHaveBeenCalledWith(0, 200)
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
    expect(platform.openReview).toHaveBeenCalledWith(1)
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
