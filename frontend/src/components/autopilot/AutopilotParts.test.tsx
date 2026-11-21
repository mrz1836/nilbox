import { act, render, renderHook, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import { maxIDs, type ActivityLine } from '../../lib/autopilot'
import { useIssueDrafts } from '../../lib/issueDrafts'
import type { LaunchOptions } from '../../lib/launch'
import { useReviewDrafts } from '../../lib/reviewDrafts'
import { useAutopilot, useAutopilotChips } from '../../lib/useAutopilot'
import { useAutopilotActivity } from '../../lib/useAutopilotActivity'
import { useInbox } from '../../lib/useInbox'
import { deferred } from '../../test/async'
import { at, clockAt, FIXED_NOW, withClock } from '../../test/clock'
import {
  activityGroup,
  activityPage,
  agents,
  autopilotMatch,
  details,
  issue,
  item,
  itemRef,
  profile,
  prompts,
  review as pullReview,
  routine,
  settings,
} from '../../test/fixtures'
import { emit, platform, send } from '../../test/platform'
import { ActivityList } from './ActivityList'
import { AutopilotRowsContext } from './AutopilotChip'
import { RoutinePane } from './RoutinePane'
import { WatchingList, type RowsKit } from './WatchingList'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// The sheets load on their own, and are imported once before the tests, so no test pays a
// sheet's cold, instrumented import inside the second a findBy waits.
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
            actions: [],
            sent: 0,
            verdict: '',
            review: null,
            postAs: '',
            reviewAs: '',
            allowApprove: false,
            merge: '',
            autoMerge: false,
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

// Answer is a page the backend gives as a bound call does: one that can be cancelled.
type Answer = Promise<ReturnType<typeof activityPage>> & { cancel: ReturnType<typeof vi.fn> }

// answer is a page, given at once.
function answer(page: ReturnType<typeof activityPage>): Answer {
  return Object.assign(Promise.resolve(page), { cancel: vi.fn() })
}

// heldAnswer is a page the test gives when it lets go.
function heldAnswer() {
  const d = deferred<ReturnType<typeof activityPage>>()

  return { call: Object.assign(d.promise, { cancel: vi.fn() }), give: d.resolve }
}

// fakeLedger is a ledger of count matches, IDs count down to 1, which Activity reads as the
// backend does: those before a match (all for 0), or from a match on and before another
// (up to the newest for 0), newest first, at most limit, with more and the last one's ID
// when there are. grow records n newer matches; clear clears the history, and records n
// matches after, whose IDs start again from 1, as SQLite hands them out again, at a later
// time.
function fakeLedger(count: number) {
  let top = count
  let cleared = false

  const pageOf = (ids: number[], limit: number) => {
    const page = ids.slice(0, limit)
    const more = ids.length > limit
    const createdAt = cleared ? '2026-06-15T11:59:00Z' : '2026-06-15T11:00:00Z'

    return activityPage({
      days: [
        {
          date: '2026-06-15',
          matches: page.map((id) => autopilotMatch({ id, createdAt, effects: [] })),
          groups: [],
        },
      ],
      more,
      next: more ? (page.at(-1) ?? 0) : 0,
      leftAlone: [],
    })
  }
  const all = () => Array.from({ length: top }, (_, i) => top - i)

  const page = (before: number, limit: number) =>
    pageOf(before === 0 ? all() : all().filter((id) => id < before), limit)
  const pageFrom = (first: number, before: number, limit: number) =>
    pageOf(
      all().filter((id) => id >= first && (before === 0 || id < before)),
      limit,
    )

  return {
    page,
    pageFrom,
    read: (before: number, limit: number) => answer(page(before, limit)),
    from: (first: number, before: number, limit: number) => answer(pageFrom(first, before, limit)),
    grow: (n: number) => {
      top += n
    },
    clear: (n: number) => {
      cleared = true
      top = n
    },
  }
}

// loadedIds are the matches the days hold, newest first.
function loadedIds(days: readonly { matches: readonly { id: number }[] }[]): number[] {
  return days.flatMap((d) => d.matches.map((m) => m.id))
}

// countdown is from down to to, both included.
function countdown(from: number, to: number): number[] {
  return Array.from({ length: from - to + 1 }, (_, i) => from - i)
}

// scrolled renders the hook over ledger and loads pages more past the first.
async function scrolled(ledger: ReturnType<typeof fakeLedger>, pages: number) {
  platform.autopilotActivity.mockImplementation(ledger.read)
  platform.autopilotActivityFrom.mockImplementation(ledger.from)

  const view = renderHook(() => useAutopilotActivity(), { wrapper: at() })

  await waitFor(() => {
    expect(view.result.current.loaded).toBe(true)
  })

  for (let i = 0; i < pages; i++) {
    await act(async () => {
      await view.result.current.loadMore()
    })
  }

  return view
}

// settled waits for the reads under way to land, the calls asked for so far answered.
async function settled() {
  await act(async () => {
    await Promise.resolve()
  })
}

describe('useAutopilotActivity', () => {
  it('reads the newest again as Autopilot changes, keeping every page past them in its place', async () => {
    const ledger = fakeLedger(700)
    const { result } = await scrolled(ledger, 5)

    expect(platform.autopilotActivity).toHaveBeenNthCalledWith(1, 0, 100)
    expect(loadedIds(result.current.days)).toEqual(countdown(700, 101))

    // 150 newer matches: only the first page is read again, from where it began, so it
    // meets the pages past it with nothing between.
    ledger.grow(150)
    send('autopilot:updated')

    await waitFor(() => {
      expect(loadedIds(result.current.days)[0]).toBe(850)
    })

    expect(platform.autopilotActivityFrom).toHaveBeenCalledExactlyOnceWith(601, 0, 500)
    expect(loadedIds(result.current.days)).toEqual(countdown(850, 101))
    expect(result.current.more).toBe(true)
    expect(platform.autopilotActivity).toHaveBeenCalledTimes(6)
  })

  it('keeps every page after a read of every page, as Autopilot changes again', async () => {
    const ledger = fakeLedger(300)
    const { result } = await scrolled(ledger, 1)

    // A line's own action reads every page again, each by its own bounds.
    act(() => {
      result.current.reload()
    })
    await waitFor(() => {
      expect(platform.autopilotActivityFrom).toHaveBeenCalledTimes(2)
    })
    expect(platform.autopilotActivityFrom).toHaveBeenNthCalledWith(1, 201, 0, 500)
    expect(platform.autopilotActivityFrom).toHaveBeenNthCalledWith(2, 101, 201, 500)

    // The engine's next cycle reads the first page again, by its bounds, never as a fresh
    // first page, which would drop the page past it.
    ledger.grow(5)
    send('autopilot:updated')

    await waitFor(() => {
      expect(loadedIds(result.current.days)).toEqual(countdown(305, 101))
    })
    expect(platform.autopilotActivityFrom).toHaveBeenLastCalledWith(201, 0, 500)
    expect(platform.autopilotActivity).toHaveBeenCalledTimes(2)
    expect(result.current.more).toBe(true)
  })

  it('reads every page again however many are loaded, past one read’s limit', async () => {
    const ledger = fakeLedger(700)
    const { result } = await scrolled(ledger, 5)

    act(() => {
      result.current.reload()
    })

    await waitFor(() => {
      expect(platform.autopilotActivityFrom).toHaveBeenCalledTimes(6)
    })
    await settled()

    expect(platform.autopilotActivityFrom.mock.calls).toEqual([
      [601, 0, 500],
      [501, 601, 500],
      [401, 501, 500],
      [301, 401, 500],
      [201, 301, 500],
      [101, 201, 500],
    ])
    expect(loadedIds(result.current.days)).toEqual(countdown(700, 101))
    expect(result.current.more).toBe(true)
  })

  it('reads the newest again in slices once more was recorded than one read holds, keeping every page', async () => {
    const ledger = fakeLedger(300)
    const { result } = await scrolled(ledger, 2)

    expect(result.current.more).toBe(false)

    // 600 newer: the newest page becomes the first again, and the rest is read in slices
    // down to where the first page began, so nothing loaded goes.
    ledger.grow(600)
    send('autopilot:updated')

    await waitFor(() => {
      expect(loadedIds(result.current.days)).toEqual(countdown(900, 1))
    })
    expect(platform.autopilotActivityFrom.mock.calls).toEqual([
      [201, 0, 500],
      [801, 0, 500],
      [201, 801, 500],
      [201, 301, 500],
    ])
    expect(result.current.more).toBe(false)

    // The next change reads that page alone again, a page and what came since.
    ledger.grow(1)
    send('autopilot:updated')

    await waitFor(() => {
      expect(loadedIds(result.current.days)).toEqual(countdown(901, 1))
    })
    expect(platform.autopilotActivityFrom).toHaveBeenCalledTimes(5)
    expect(platform.autopilotActivityFrom).toHaveBeenLastCalledWith(801, 0, 500)
  })

  it('starts from scratch once the history was cleared, as a change finds the first page’s first match gone', async () => {
    const ledger = fakeLedger(300)
    const { result } = await scrolled(ledger, 1)

    // Cleared, the history starts again from ID 1, below every page loaded.
    ledger.clear(20)
    send('autopilot:updated')

    await waitFor(() => {
      expect(loadedIds(result.current.days)).toEqual(countdown(20, 1))
    })
    expect(platform.autopilotActivityFrom).toHaveBeenCalledExactlyOnceWith(201, 0, 500)
    expect(platform.autopilotActivity).toHaveBeenLastCalledWith(0, 100)
    expect(result.current.more).toBe(false)
  })

  it('starts from scratch once the match the first page begins at was recorded again since, as the tab shows', async () => {
    const ledger = fakeLedger(300)
    const { result, rerender } = await scrolled(ledger, 1)

    // Cleared while the tab was away, the history holds a match with that ID again, made
    // later: what is loaded is no longer what the IDs hold.
    ledger.clear(260)
    act(() => {
      result.current.reload()
    })

    await waitFor(() => {
      expect(loadedIds(result.current.days)).toEqual(countdown(260, 161))
    })
    rerender()
    expect(platform.autopilotActivityFrom).toHaveBeenCalledExactlyOnceWith(201, 0, 500)
    expect(platform.autopilotActivity).toHaveBeenLastCalledWith(0, 100)
    expect(result.current.more).toBe(true)
    expect(result.current.current).toBe(true)
  })

  it('runs one read at a time, with one more waiting, however often Autopilot changes', async () => {
    const ledger = fakeLedger(300)
    const { result } = await scrolled(ledger, 1)
    const held = heldAnswer()

    platform.autopilotActivityFrom.mockReturnValueOnce(held.call)

    act(() => {
      for (let i = 0; i < 6; i++) {
        emit('autopilot:updated')
      }
    })

    expect(platform.autopilotActivityFrom).toHaveBeenCalledOnce()

    await act(async () => {
      held.give(ledger.pageFrom(201, 0, 500))
      await held.call
    })

    await waitFor(() => {
      expect(platform.autopilotActivityFrom).toHaveBeenCalledTimes(2)
    })
    expect(loadedIds(result.current.days)).toEqual(countdown(300, 101))
  })

  it('drops a read of the newest under way, and one waiting, for a read of every page, and stops every read as it hides', async () => {
    const ledger = fakeLedger(300)
    const { result, unmount } = await scrolled(ledger, 1)
    const held = heldAnswer()

    platform.autopilotActivityFrom.mockReturnValueOnce(held.call)
    act(() => {
      emit('autopilot:updated')
      emit('autopilot:updated')
    })

    act(() => {
      result.current.reload()
    })

    expect(held.call.cancel).toHaveBeenCalledOnce()

    // The read of every page reads both pages, and the read of the newest that waited
    // doesn't follow it.
    await waitFor(() => {
      expect(platform.autopilotActivityFrom).toHaveBeenLastCalledWith(101, 201, 500)
    })
    await settled()
    await settled()
    expect(platform.autopilotActivityFrom).toHaveBeenCalledTimes(3)

    // Asked again while it reads, it waits its turn, and hiding stops it.
    const first = heldAnswer()

    platform.autopilotActivityFrom.mockReturnValueOnce(first.call)
    act(() => {
      result.current.reload()
      result.current.reload()
    })
    expect(platform.autopilotActivityFrom).toHaveBeenCalledTimes(4)

    unmount()

    expect(first.call.cancel).toHaveBeenCalledOnce()
    expect(platform.autopilotActivityFrom).toHaveBeenCalledTimes(4)
  })

  it('never loses a page read while the newest are read again', async () => {
    const ledger = fakeLedger(300)
    const { result } = await scrolled(ledger, 0)
    const held = heldAnswer()

    // With nothing past the first page, a read again is the first page's own, a page long.
    platform.autopilotActivity.mockReturnValueOnce(held.call)
    send('autopilot:updated')

    expect(platform.autopilotActivity).toHaveBeenLastCalledWith(0, 100)

    platform.autopilotActivity.mockImplementation(ledger.read)
    await act(async () => {
      await result.current.loadMore()
    })

    expect(loadedIds(result.current.days)).toEqual(countdown(300, 101))

    // The first page's read again lands after: the page read meanwhile stays, and the
    // newest are read again from where they begin, which meets it.
    ledger.grow(10)
    await act(async () => {
      held.give(ledger.page(0, 100))
      await held.call
    })

    await waitFor(() => {
      expect(loadedIds(result.current.days)).toEqual(countdown(310, 101))
    })
    expect(platform.autopilotActivityFrom).toHaveBeenCalledExactlyOnceWith(201, 0, 500)
  })

  it('says the days are current only once the read as the tab shows has landed', async () => {
    const ledger = fakeLedger(150)
    const held = heldAnswer()

    platform.autopilotActivity.mockReturnValueOnce(held.call)
    platform.autopilotActivityFrom.mockImplementation(ledger.from)

    const { result } = renderHook(() => useAutopilotActivity(), { wrapper: at() })

    expect(result.current.current).toBe(false)

    await act(async () => {
      held.give(ledger.page(0, 100))
      await held.call
    })

    expect(result.current.loaded).toBe(true)
    expect(result.current.current).toBe(true)
  })

  it('says why an earlier page, or a read again, couldn’t be read', async () => {
    const ledger = fakeLedger(250)
    const { result } = await scrolled(ledger, 0)

    expect(result.current.more).toBe(true)

    platform.autopilotActivity.mockRejectedValueOnce(new Error('Autopilot’s history couldn’t be read.'))
    await act(async () => {
      await result.current.loadMore()
    })

    expect(result.current.error).toBe('Autopilot’s history couldn’t be read.')

    await act(async () => {
      await result.current.loadMore()
    })

    expect(result.current.error).toBe('')

    platform.autopilotActivityFrom.mockRejectedValue(new Error('The database is locked.'))
    act(() => {
      result.current.reload()
    })
    await waitFor(() => {
      expect(result.current.error).toBe('The database is locked.')
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

  it('reads the routines again as a new day begins, so today’s counts turn over though nothing changed', async () => {
    let now = FIXED_NOW
    const clock = clockAt(() => now)

    platform.listRoutines.mockResolvedValue([routine()])

    const { result } = renderHook(() => useAutopilot(), { wrapper: withClock(clock) })

    await waitFor(() => {
      expect(result.current.loaded).toBe(true)
    })
    expect(platform.listRoutines).toHaveBeenCalledOnce()

    // A minute on, the same day, reads nothing.
    now = new Date(FIXED_NOW.getTime() + 60_000)
    act(() => {
      clock.read()
    })
    expect(platform.listRoutines).toHaveBeenCalledOnce()

    now = new Date(FIXED_NOW.getTime() + 24 * 60 * 60_000)
    act(() => {
      clock.read()
    })

    await waitFor(() => {
      expect(platform.listRoutines).toHaveBeenCalledTimes(2)
    })
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

    send('autopilot:updated')
    await waitFor(() => {
      expect(platform.latestAutopilotMatches).toHaveBeenCalledTimes(2)
    })

    rerender({ ids: '' })
    expect(result.current.size).toBe(0)
  })
})

describe('RoutinePane', () => {
  const actions = {
    onSwitch: vi.fn(),
    onDryRun: vi.fn(),
    onDescribe: vi.fn(),
    onDuplicate: vi.fn(),
    onExport: vi.fn(),
    onDelete: vi.fn(),
  }

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

  it('changes the routine with Claude from its header and its menu, with the routine attached', async () => {
    renderPane()

    const change = screen.getByRole('button', { name: 'Change with Claude…' })

    expect(change).toHaveAttribute('title', expect.stringContaining('review the change it proposes'))
    await userEvent.click(change)
    expect(actions.onDescribe).toHaveBeenCalledExactlyOnceWith(routine())

    await userEvent.click(screen.getByRole('button', { name: 'More for Snooze passing bot PRs' }))

    const item = await screen.findByRole('menuitem', { name: 'Change with Claude…' })

    expect(item).toBeEnabled()
    await userEvent.click(item)
    expect(actions.onDescribe).toHaveBeenCalledTimes(2)
  })

  it('offers no change with Claude for a routine that can’t change here', async () => {
    const theirs = routine({ otherAccount: true, account: 'octo-other' })

    renderPane(theirs)

    expect(screen.queryByRole('button', { name: 'Change with Claude…' })).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'More for Snooze passing bot PRs' }))

    const item = await screen.findByRole('menuitem', { name: 'Change with Claude…' })

    expect(item).toBeDisabled()
    expect(item).toHaveAttribute('title', 'This routine can’t change here')
    expect(actions.onDescribe).not.toHaveBeenCalled()
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
    // Both the sheet's reads, its opening and its refresh, say what the card says, so the
    // sheet's name never changes under the test.
    const read = issue({ id: 2, number: 9, title: 'The widget forgets its size' })

    platform.openIssue.mockResolvedValue(read)
    platform.refreshIssue.mockResolvedValue(read)
    render(<Watching />, { wrapper: rows })

    await userEvent.click(await screen.findByRole('button', { name: 'The widget forgets its size' }))

    expect(await screen.findByRole('dialog', { name: 'The widget forgets its size' })).toBeInTheDocument()
    // The sheet reads the issue, and the host hears it opened, in effects after the sheet
    // shows, which a busy runner may not have run when the sheet is first found.
    await waitFor(() => {
      expect(platform.openIssue).toHaveBeenCalledWith(2)
    })
    await waitFor(() => {
      expect(onItemOpened).toHaveBeenCalledWith(2)
    })
  })

  it('marks a watched item read as its sheet opens, steps away, and closes, and reads the rows again after each mark', async () => {
    // What the backend stores, which the marks change.
    let watched = [
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size', unread: true }),
      item({ id: 4, kind: 'issue', number: 10, title: 'The widget forgets its place', unread: true }),
    ]
    const titleOf = (id: number) => watched.find((w) => w.id === id)?.title ?? ''

    platform.routineWatching.mockResolvedValue([2, 4])
    platform.itemsByID.mockImplementation(() => Promise.resolve(watched))
    platform.markItemsSeen.mockImplementation((ids) => {
      watched = watched.map((w) => (ids?.includes(w.id) === true ? { ...w, unread: false } : w))

      return Promise.resolve()
    })
    platform.openIssue.mockImplementation((id) => Promise.resolve(issue({ id, title: titleOf(id) })))
    platform.refreshIssue.mockImplementation((id) => Promise.resolve(issue({ id, title: titleOf(id) })))
    render(<Watching />, { wrapper: rows })

    expect(await screen.findAllByTitle('New since you last opened it')).toHaveLength(2)

    await userEvent.click(screen.getByRole('button', { name: 'The widget forgets its size' }))
    expect(await screen.findByRole('dialog', { name: 'The widget forgets its size' })).toBeInTheDocument()
    await waitFor(() => {
      expect(platform.markItemsSeen).toHaveBeenCalledExactlyOnceWith([2])
    })

    await userEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(await screen.findByRole('dialog', { name: 'The widget forgets its place' })).toBeInTheDocument()
    await waitFor(() => {
      expect(platform.markItemsSeen, 'the one left and the one opened').toHaveBeenLastCalledWith([2, 4])
    })

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => {
      expect(platform.markItemsSeen, 'the one it closed on').toHaveBeenLastCalledWith([4])
    })
    await waitFor(() => {
      expect(screen.queryByTitle('New since you last opened it'), 'the rows read again').not.toBeInTheDocument()
    })
    expect(platform.markItemsSeen).toHaveBeenCalledTimes(3)
  })

  it('steps past watched items a read finds merged or closed, and closes the sheet opened on one', async () => {
    // A pull request merged and an issue closed since, side by side, which the list, not
    // read again here, keeps holding.
    const watched = [
      item({ id: 2, kind: 'issue', number: 9, title: 'The widget forgets its size' }),
      item({
        id: 4,
        number: 10,
        title: 'The widget forgets its place',
        url: 'https://github.com/acme-org/example-repo/pull/10',
      }),
      item({ id: 6, kind: 'issue', number: 11, title: 'The widget forgets its color' }),
    ]
    const named = (id: number) => {
      const w = watched.find((c) => c.id === id) ?? item()

      return { id, number: w.number, title: w.title, url: w.url }
    }

    platform.routineWatching.mockResolvedValue([2, 4, 6])
    platform.itemsByID.mockResolvedValue(watched)
    platform.openIssue.mockImplementation((id) => Promise.resolve(issue(named(id))))
    platform.refreshIssue.mockImplementation((id) =>
      Promise.resolve(issue({ ...named(id), state: id === 6 ? 'CLOSED' : 'OPEN' })),
    )
    platform.openReview.mockImplementation((id) => Promise.resolve(pullReview(named(id))))
    platform.refreshReview.mockImplementation((id) =>
      Promise.resolve(pullReview({ ...named(id), state: 'MERGED', offers: [], primary: '' })),
    )
    render(<Watching />, { wrapper: rows })

    await userEvent.click(await screen.findByRole('button', { name: 'The widget forgets its size' }))
    expect(await screen.findByRole('dialog', { name: 'The widget forgets its size' })).toBeInTheDocument()

    // On to #10, merged, past it to #11, closed and the last, and back to #9, never to #10.
    // Each sheet's read is waited for in turn: the whole walk opens and reads two sheets,
    // which can take longer than one wait on a loaded machine.
    await userEvent.click(screen.getByRole('button', { name: 'Next' }))
    await waitFor(() => {
      expect(platform.refreshReview).toHaveBeenCalledExactlyOnceWith(4)
    })
    await waitFor(() => {
      expect(platform.refreshIssue).toHaveBeenCalledWith(6)
    })
    await waitFor(() => {
      expect(screen.getByRole('group', { name: 'Notification' })).toHaveTextContent(
        'acme-org/example-repo#11 was closed · The widget forgets its color',
      )
    })
    expect(await screen.findByRole('dialog', { name: 'The widget forgets its size' })).toBeInTheDocument()
    expect(platform.refreshReview).toHaveBeenCalledExactlyOnceWith(4)

    await userEvent.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    await userEvent.click(screen.getByRole('button', { name: 'The widget forgets its place' }))
    await waitFor(() => {
      expect(platform.refreshReview).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(screen.getByRole('group', { name: 'Notification' })).toHaveTextContent(
      'acme-org/example-repo#10 was merged · The widget forgets its place',
    )
  })

  it('dismisses a watched row through the inbox’s own triage, and reads the rows again as the inbox changes', async () => {
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockResolvedValue([item()])
    render(<Watching />, { wrapper: rows })

    await userEvent.click(await screen.findByRole('button', { name: 'Dismiss' }))

    expect(platform.dismissItem).toHaveBeenCalledWith(1)
    expect(await screen.findByText('Dismissed')).toBeInTheDocument()

    platform.itemsByID.mockResolvedValue([item({ dismissedAt: '2026-06-15T12:00:00Z' })])
    send('inbox:updated')

    expect(await screen.findByRole('button', { name: 'Restore' })).toBeInTheDocument()
  })

  it('shows each watched item as the inbox’s card, with where its routine stands', async () => {
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockResolvedValue([item({ reviewRequest: 'you', details: details() })])
    platform.latestAutopilotMatches.mockResolvedValue([autopilotMatch({ state: 'waiting', reason: '' })])
    render(<Watching />, { wrapper: rows })

    const list = await screen.findByRole('list', { name: 'Watching' })
    expect(list).toHaveClass('@container')

    const row = within(list).getByRole('listitem')
    await waitFor(() => {
      expect(row).toHaveAttribute('data-rule', 'autopilot_needs_you')
    })
    expect(within(row).getByText('Snooze passing bot PRs wants your OK')).toBeInTheDocument()
    expect(within(row).getByRole('button', { name: 'Decide' })).toBeInTheDocument()
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

    // An ask for an item it doesn't hold, once it has read what it watches, is handed back
    // at once, marking nothing, so it can't land later unasked.
    rerender(<Watching />)
    send('autopilot:updated')
    rerender(<Watching highlight={{ id: 9, seq: 2 }} />)

    await waitFor(() => {
      expect(onHighlighted).toHaveBeenCalledTimes(2)
    })
    expect(platform.routineWatching.mock.calls.length).toBeGreaterThan(1)
    expect(platform.openExternal).not.toHaveBeenCalled()
  })

  it('scrolls the row Activity asked for into view, and no other', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    platform.routineWatching.mockResolvedValue([1, 2])
    platform.itemsByID.mockResolvedValue([item({ id: 2, number: 8, title: 'Fix the flaky test' }), item()])
    render(<Watching highlight={{ id: 1, seq: 1 }} />, { wrapper: rows })

    const row = (await screen.findByRole('button', { name: 'Add the widget cache' })).closest('[data-item-id]')

    await waitFor(() => {
      expect(onHighlighted).toHaveBeenCalledOnce()
    })
    expect(row).toHaveAttribute('data-item-id', '1')
    expect(scroll.mock.contexts).toEqual([row])
    expect(scroll).toHaveBeenCalledWith({ block: 'center' })
  })

  it('hands back an ask when what it watches couldn’t be read', async () => {
    platform.routineWatching.mockResolvedValue([1])
    platform.itemsByID.mockRejectedValue(new Error('database is locked'))
    render(<Watching highlight={{ id: 1, seq: 1 }} />, { wrapper: rows })

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    await waitFor(() => {
      expect(onHighlighted).toHaveBeenCalledOnce()
    })
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
