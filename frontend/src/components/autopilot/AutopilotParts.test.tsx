import { act, render, renderHook, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { maxIDs, type ActivityLine } from '../../lib/autopilot'
import { useIssueDrafts } from '../../lib/issueDrafts'
import { useReviewDrafts } from '../../lib/reviewDrafts'
import { useAutopilot, useAutopilotChips } from '../../lib/useAutopilot'
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

// fakeLedger is a ledger of count matches, IDs count down to 1, which Activity reads as the
// backend does: those before a match (all for 0), newest first, at most limit, with more
// and the last one's ID when there are. grow records n newer matches.
function fakeLedger(count: number) {
  let top = count

  const read = (before: number, limit: number) => {
    const ids = Array.from({ length: top }, (_, i) => top - i).filter((id) => before === 0 || id < before)
    const page = ids.slice(0, limit)
    const more = ids.length > limit

    return Promise.resolve(
      activityPage({
        days: [{ date: '2026-06-15', matches: page.map((id) => autopilotMatch({ id, effects: [] })), groups: [] }],
        more,
        next: more ? (page.at(-1) ?? 0) : 0,
        leftAlone: [],
      }),
    )
  }

  return {
    read,
    grow: (n: number) => {
      top += n
    },
  }
}

// loadedIds are the matches the days hold, newest first.
function loadedIds(days: readonly { matches: readonly { id: number }[] }[]): number[] {
  return days.flatMap((d) => d.matches.map((m) => m.id))
}

describe('useAutopilotActivity', () => {
  it('reads a page more on asking, and every match down to the oldest loaded again, so nothing drops out', async () => {
    const ledger = fakeLedger(700)
    platform.autopilotActivity.mockImplementation(ledger.read)

    const { result } = renderHook(() => useAutopilotActivity(), { wrapper: at() })

    await waitFor(() => {
      expect(result.current.loaded).toBe(true)
    })
    expect(platform.autopilotActivity).toHaveBeenLastCalledWith(0, 100)

    for (let i = 0; i < 5; i++) {
      await act(async () => {
        await result.current.loadMore()
      })
    }

    expect(loadedIds(result.current.days)).toEqual(Array.from({ length: 600 }, (_, i) => 700 - i))

    // 150 newer matches push the old first page's tail on: a read again still holds every
    // match from the newest down to the oldest loaded.
    ledger.grow(150)
    act(() => {
      emit('autopilot:updated')
    })

    await waitFor(() => {
      expect(loadedIds(result.current.days)[0]).toBe(850)
    })

    const ids = loadedIds(result.current.days)

    expect(ids.slice(0, 750)).toEqual(Array.from({ length: 750 }, (_, i) => 850 - i))
    expect(result.current.more).toBe(false)
  })

  it('says why an earlier page, or a read again, couldn’t be read', async () => {
    const ledger = fakeLedger(150)
    platform.autopilotActivity.mockImplementation(ledger.read)

    const { result } = renderHook(() => useAutopilotActivity(), { wrapper: at() })

    await waitFor(() => {
      expect(result.current.more).toBe(true)
    })

    platform.autopilotActivity.mockRejectedValue(new Error('Autopilot’s history couldn’t be read.'))
    await act(async () => {
      await result.current.loadMore()
    })

    expect(result.current.error).toBe('Autopilot’s history couldn’t be read.')

    act(() => {
      result.current.reload()
    })
    await waitFor(() => {
      expect(result.current.error).toBe('Autopilot’s history couldn’t be read.')
    })
  })
})

describe('useAutopilot', () => {
  it('names a deleted routine, so the marks it left still say whose they are', async () => {
    platform.listRoutines.mockResolvedValue([routine()])
    platform.routineNames.mockResolvedValue([
      { id: 3, name: 'Snooze passing bot PRs', deleted: false },
      { id: 9, name: 'An old routine', deleted: true },
    ])

    const { result } = renderHook(() => useAutopilot(), { wrapper: at() })

    await waitFor(() => {
      expect(result.current.names.get(9)).toBe('An old routine')
    })
    expect(result.current.names.get(3)).toBe('Snooze passing bot PRs')
    expect(result.current.routines).toHaveLength(1)
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

const onHighlighted = vi.fn()

function Watching({ highlight = null }: { highlight?: { id: number; seq: number } | null }) {
  return <WatchingList routineId={3} kit={useKit()} highlight={highlight} onHighlighted={onHighlighted} />
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

  it('marks the row Activity asked for once, hands the ask back, and never opens GitHub itself', async () => {
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockResolvedValue([item()])
    const { rerender } = render(<Watching highlight={{ id: 1, seq: 1 }} />, { wrapper: rows })

    const row = (await screen.findByRole('button', { name: 'Add the widget cache' })).closest('li')

    await waitFor(() => {
      expect(onHighlighted).toHaveBeenCalledOnce()
    })
    expect(row).toHaveAttribute('data-highlighted', 'true')

    rerender(<Watching />)
    act(() => {
      emit('autopilot:updated')
    })
    rerender(<Watching highlight={{ id: 9, seq: 2 }} />)

    await waitFor(() => {
      expect(platform.routineWatching.mock.calls.length).toBeGreaterThan(1)
    })
    expect(onHighlighted).toHaveBeenCalledOnce()
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('reads every item it watches, past what one read takes', async () => {
    const ids = Array.from({ length: maxIDs + 2 }, (_, i) => i + 1)
    platform.routineWatching.mockResolvedValue(ids)
    // Each read stores only its chunk's last item, so the list stays small enough to render
    // quickly while still showing the second chunk was read.
    platform.itemsByID.mockImplementation((asked) =>
      Promise.resolve((asked ?? []).slice(-1).map((id) => item({ id, number: id, title: `Item ${String(id)}` }))),
    )
    render(<Watching />, { wrapper: rows })

    expect(await screen.findByRole('button', { name: `Item ${String(maxIDs + 2)}` })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: `Item ${String(maxIDs)}` })).toBeInTheDocument()
    expect(platform.itemsByID.mock.calls.map(([asked]) => asked?.length)).toEqual([maxIDs, 2])
    expect(platform.latestAutopilotMatches.mock.calls.map(([asked]) => asked?.length)).toEqual([maxIDs, 2])
  })

  it('says why what it watches couldn’t be read', async () => {
    platform.routineWatching.mockRejectedValue(new Error('The routine is gone.'))
    render(<Watching />, { wrapper: rows })

    expect(await screen.findByRole('alert')).toHaveTextContent('The routine is gone.')
  })
})
