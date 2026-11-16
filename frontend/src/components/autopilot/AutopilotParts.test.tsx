import { act, render, renderHook, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { ActivityLine } from '../../lib/autopilot'
import { useIssueDrafts } from '../../lib/issueDrafts'
import { useReviewDrafts } from '../../lib/reviewDrafts'
import { useAutopilotChips } from '../../lib/useAutopilot'
import { useAutopilotActivity } from '../../lib/useAutopilotActivity'
import { useInbox } from '../../lib/useInbox'
import { at } from '../../test/clock'
import {
  activityGroup,
  activityPage,
  agents,
  autopilotMatch,
  issue,
  item,
  itemRef,
  profile,
  prompts,
  routine,
  settings,
} from '../../test/fixtures'
import { emit, platform } from '../../test/platform'
import type { LaunchOptions } from '../ItemRow'
import { ActivityList } from './ActivityList'
import { AutopilotRowsContext } from './AutopilotChip'
import { RoutinePane } from './RoutinePane'
import { WatchingList, type RowsKit } from './WatchingList'

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

// line is an Activity line of a match.
function matchLine(overrides: Parameters<typeof autopilotMatch>[0]): ActivityLine {
  const match = autopilotMatch({ effects: [], ...overrides })

  return { kind: 'match', key: `match:${String(match.id)}`, at: match.createdAt, match }
}

describe('ActivityList', () => {
  const onOpen = vi.fn()
  const onToast = vi.fn()

  function renderList(lines: ActivityLine[]) {
    render(<ActivityList lines={lines} onOpen={onOpen} onToast={onToast} empty={<p>Nothing</p>} />, {
      wrapper: at(),
    })
  }

  it('says what each match did in a word or two', () => {
    renderList([
      matchLine({ id: 1, state: 'skipped', reason: 'outside_contributor', words: 'An outside contributor’s.' }),
      matchLine({ id: 2, state: 'dismissed', reason: 'you_skipped', words: 'You skipped it.' }),
      matchLine({ id: 3, reason: 'failed', words: 'It ran, and a step failed.' }),
      matchLine({
        id: 4,
        words: '',
        steps: [
          {
            kind: 'tellMe',
            label: 'Tell me',
            status: 'done',
            outcome: '',
            note: '',
            runId: 0,
            startedAt: null,
            endedAt: null,
          },
        ],
      }),
      matchLine({ id: 5, steps: [], words: 'It ran.' }),
    ])

    for (const headline of ['Skipped', 'You skipped it', 'A step failed', 'Told you', 'Ran']) {
      expect(screen.getByTitle(headline)).toBeInTheDocument()
    }
  })

  it('marks each kind of grouped line, and says when the user took some of it back', () => {
    renderList([
      {
        kind: 'group',
        key: 'a',
        at: '2026-06-15T11:00:00Z',
        group: activityGroup({ key: 'a', action: 'pin', words: 'Pinned 2', undo: [], count: 2, reversed: 2 }),
      },
      {
        kind: 'group',
        key: 'b',
        at: '2026-06-15T10:00:00Z',
        group: activityGroup({ key: 'b', action: 'dismiss', words: 'Dismissed 2', undo: [], count: 2, undone: 1 }),
      },
      {
        kind: 'group',
        key: 'c',
        at: '2026-06-15T09:00:00Z',
        group: activityGroup({ key: 'c', action: 'restore', words: 'Restored 1', undo: [], count: 1 }),
      },
    ])

    expect(screen.getByText('Pinned 2').closest('li')).toHaveTextContent('Taken back')
    expect(screen.getByText('Dismissed 2').closest('li')).toHaveTextContent('Partly taken back')
    expect(screen.getByText('Restored 1').closest('li')).not.toHaveTextContent('taken back')
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()
  })

  it('names the items of a line that stands for many, and says why an action failed', async () => {
    platform.undoAutopilot.mockRejectedValue(new Error('Autopilot hasn’t started yet; try again in a moment.'))
    const items = [1, 2, 3, 4, 5].map((n) => itemRef({ id: n, number: n }))

    renderList([{ kind: 'group', key: 'g', at: '2026-06-15T11:00:00Z', group: activityGroup({ items, count: 5 }) }])

    expect(screen.getByText('example-repo #1, example-repo #2, example-repo #3, and 2 more')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Undo' }))

    await waitFor(() => {
      expect(onToast).toHaveBeenCalledWith('Autopilot hasn’t started yet; try again in a moment.', 'error')
    })
  })

  it('opens a line’s item, and offers none for an item gone', async () => {
    renderList([
      matchLine({ id: 6, item: itemRef({ id: 0, url: '' }) }),
      matchLine({ id: 7, item: itemRef({ id: 2, number: 3 }) }),
    ])

    expect(screen.getAllByRole('button', { name: /^Open/ })).toHaveLength(1)

    await userEvent.click(screen.getByRole('button', { name: 'Open example-repo #3' }))

    expect(onOpen).toHaveBeenCalledExactlyOnceWith(itemRef({ id: 2, number: 3 }), 3)
  })
})

describe('useAutopilotActivity', () => {
  it('reads past the first read’s cap a page at a time, and joins what two pages split', async () => {
    platform.autopilotActivity.mockImplementation((before) =>
      Promise.resolve(
        before === 0
          ? activityPage({
              more: true,
              next: 12,
              days: [{ date: '2026-06-15', matches: [autopilotMatch()], groups: [activityGroup()] }],
            })
          : activityPage({
              more: false,
              next: 0,
              days: [
                {
                  date: '2026-06-15',
                  matches: [autopilotMatch({ id: 11 })],
                  groups: [activityGroup({ undo: [50], matches: [11] })],
                },
              ],
            }),
      ),
    )

    const { result } = renderHook(() => useAutopilotActivity(), { wrapper: at() })

    await waitFor(() => {
      expect(result.current.loaded).toBe(true)
    })

    // The first read grows to the backend's cap before pages are read on.
    for (const limit of [200, 300, 400, 500]) {
      await act(async () => {
        await result.current.loadMore()
      })
      await waitFor(() => {
        expect(platform.autopilotActivity).toHaveBeenCalledWith(0, limit)
      })
    }

    await act(async () => {
      await result.current.loadMore()
    })

    expect(platform.autopilotActivity).toHaveBeenLastCalledWith(12, 100)
    expect(result.current.more).toBe(false)
    expect(result.current.days[0]?.groups[0]).toMatchObject({ words: 'Snoozed 2', undo: [51, 50] })

    platform.autopilotActivity.mockRejectedValue(new Error('Autopilot’s history couldn’t be read.'))
    act(() => {
      result.current.reload()
    })

    await waitFor(() => {
      expect(result.current.error).toBe('Autopilot’s history couldn’t be read.')
    })
  })

  it('says why an earlier page couldn’t be read', async () => {
    platform.autopilotActivity.mockResolvedValue(activityPage({ more: true, next: 12 }))

    const { result } = renderHook(() => useAutopilotActivity(), { wrapper: at() })

    await waitFor(() => {
      expect(result.current.loaded).toBe(true)
    })

    for (let i = 0; i < 4; i++) {
      await act(async () => {
        await result.current.loadMore()
      })
    }

    platform.autopilotActivity.mockRejectedValue(new Error('Autopilot’s history couldn’t be read.'))
    await act(async () => {
      await result.current.loadMore()
    })

    expect(result.current.error).toBe('Autopilot’s history couldn’t be read.')
  })
})

describe('useAutopilotChips', () => {
  it('reads the items’ newest matches again whenever Autopilot changes, and nothing for no item', async () => {
    platform.latestAutopilotMatches.mockResolvedValue([autopilotMatch()])

    const { result, rerender } = renderHook(({ ids }) => useAutopilotChips(ids), {
      wrapper: at(),
      initialProps: { ids: '1,2' },
    })

    await waitFor(() => {
      expect(result.current.get(1)?.id).toBe(12)
    })
    expect(platform.latestAutopilotMatches).toHaveBeenCalledWith([1, 2])

    act(() => {
      emit('autopilot:updated')
    })
    await waitFor(() => {
      expect(platform.latestAutopilotMatches).toHaveBeenCalledTimes(2)
    })

    rerender({ ids: '' })
    expect(result.current.size).toBe(0)
  })
})

describe('RoutinePane', () => {
  const actions = { onSwitch: vi.fn(), onDryRun: vi.fn(), onDuplicate: vi.fn(), onDelete: vi.fn() }

  function renderPane(r = routine()) {
    render(
      <RoutinePane
        routine={r}
        status={{ text: 'Autopilot · 6 today', tone: 'muted' }}
        busy={false}
        actions={actions}
        onMode={vi.fn()}
        watching={<p>Watched</p>}
        activity={<p>Done</p>}
      />,
      { wrapper: at() },
    )
  }

  it('switches the routine and opens its menu from its header', async () => {
    renderPane()

    await userEvent.click(screen.getByRole('switch', { name: 'Run this routine' }))
    expect(actions.onSwitch).toHaveBeenCalledWith(routine(), false)

    await userEvent.click(screen.getByRole('button', { name: 'More for Snooze passing bot PRs' }))
    await userEvent.click(await screen.findByRole('menuitem', { name: 'Delete…' }))
    expect(actions.onDelete).toHaveBeenCalledWith(routine())
  })

  it('keeps a routine with steps this version can’t run yet in Dry run, and lists what is wrong with one', () => {
    renderPane(
      routine({
        mode: 'dryRun',
        dryRunOnly: ['agent'],
        problems: [{ part: 'steps', field: '[0]', message: 'Its prompt is gone.' }],
      }),
    )

    for (const radio of screen.getAllByRole('radio')) {
      expect(radio).toBeDisabled()
    }

    expect(
      screen.getByText('It has steps this version can only try in Dry run, so it stays there.'),
    ).toBeInTheDocument()
    expect(
      within(screen.getByRole('list', { name: 'What is wrong with it' })).getByText('Its prompt is gone.'),
    ).toBeInTheDocument()
  })
})

// kitOf is a Watching list's kit over the real inbox and drafts.
function useKit(): RowsKit {
  const inbox = useInbox()
  const review = useReviewDrafts()
  const issueDrafts = useIssueDrafts()

  return {
    inbox,
    launch,
    sessions: new Map(),
    worktrees: new Map(),
    snoozeDays: 3,
    repoRules: settings().repoRules,
    drafts: { review, issue: issueDrafts },
    reviewTab: 'files',
    onReviewTab: () => undefined,
    conversationOrder: 'oldest',
    onConversationOrder: () => undefined,
    onSnoozeSettings: () => undefined,
    onFilesSettings: () => undefined,
    onRepoRuleSettings: () => undefined,
    onManageReplies: () => undefined,
    onSettingsSaved: () => undefined,
    onItemOpened,
  }
}

const onItemOpened = vi.fn<(itemId: number) => void>()

function Watching({ highlight = null }: { highlight?: { id: number; url: string; seq: number } | null }) {
  return <WatchingList routineId={3} kit={useKit()} highlight={highlight} />
}

function rows({ children }: { children: ReactNode }) {
  const Clock = at()

  return (
    <Clock>
      <AutopilotRowsContext value={{ names: new Map([[3, 'Snooze passing bot PRs']]), chips: new Map() }}>
        {children}
      </AutopilotRowsContext>
    </Clock>
  )
}

describe('WatchingList', () => {
  it('opens a watched issue in the issue sheet, and hears each opening', async () => {
    platform.routineWatching.mockResolvedValue([2])
    platform.itemsByID.mockResolvedValue([
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
    ])
    platform.openIssue.mockResolvedValue(issue({ id: 2, title: 'The widget forgets its size' }))
    render(<Watching />, { wrapper: rows })

    await userEvent.click(await screen.findByRole('button', { name: 'The widget forgets its size' }))

    expect(await screen.findByRole('dialog', { name: 'The widget forgets its size' })).toBeInTheDocument()
    expect(platform.openIssue).toHaveBeenCalledWith(2)
    expect(onItemOpened).toHaveBeenCalledWith(2)
  })

  it('dismisses a watched row through the inbox’s own triage, and reads the rows again as the inbox changes', async () => {
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockResolvedValue([item()])
    render(<Watching />, { wrapper: rows })

    await userEvent.click(await screen.findByRole('button', { name: 'Dismiss' }))

    expect(platform.dismissItem).toHaveBeenCalledWith(1)
    expect(await screen.findByText('Dismissed')).toBeInTheDocument()

    platform.itemsByID.mockResolvedValue([item({ dismissedAt: '2026-06-15T12:00:00Z' })])
    act(() => {
      emit('inbox:updated')
    })

    expect(await screen.findByRole('button', { name: 'Restore' })).toBeInTheDocument()
  })

  it('opens on GitHub an item Activity asked for that it no longer watches', async () => {
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockResolvedValue([item()])
    render(<Watching highlight={{ id: 9, url: 'https://github.com/acme-org/example-repo/issues/9', seq: 1 }} />, {
      wrapper: rows,
    })

    await waitFor(() => {
      expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/9')
    })
  })

  it('says why what it watches couldn’t be read', async () => {
    platform.routineWatching.mockRejectedValue(new Error('The routine is gone.'))
    render(<Watching />, { wrapper: rows })

    expect(await screen.findByRole('alert')).toHaveTextContent('The routine is gone.')
  })
})
