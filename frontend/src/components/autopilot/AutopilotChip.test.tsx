import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotMatch, AutopilotTarget, AutopilotUpcoming, InboxItem } from '../../platform'
import { at } from '../../test/clock'
import {
  autopilotEffect,
  autopilotMatch,
  autopilotTarget,
  autopilotUpcoming,
  dryRunMatch,
  item,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { expectWraps } from '../../test/wrap'
import { ItemRow } from '../ItemRow'
import { AutopilotChip, AutopilotRowsContext } from './AutopilotChip'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const names = new Map([
  [3, 'Snooze passing bot PRs'],
  [4, 'Investigate new bugs'],
])

// withRows renders inside the rows' Autopilot, with the chips and the routines that will run
// given, as the window does.
function withRows(
  chips: ReadonlyMap<number, AutopilotMatch>,
  onShow?: (target: AutopilotTarget) => void,
  upcoming: ReadonlyMap<number, readonly AutopilotUpcoming[]> = new Map(),
) {
  const Clock = at()

  return function Rows({ children }: { children: ReactNode }) {
    return (
      <Clock>
        <AutopilotRowsContext value={{ names, chips, upcoming, onShow }}>{children}</AutopilotRowsContext>
      </Clock>
    )
  }
}

function renderRow(
  it: InboxItem,
  chips: ReadonlyMap<number, AutopilotMatch> = new Map(),
  upcoming: ReadonlyMap<number, readonly AutopilotUpcoming[]> = new Map(),
  onShow?: (target: AutopilotTarget) => void,
) {
  render(
    <ul>
      <ItemRow item={it} launch={null} />
    </ul>,
    { wrapper: withRows(chips, onShow, upcoming) },
  )
}

describe('AutopilotChip', () => {
  it('shows nothing on an item no routine touched, nor outside the window’s Autopilot', () => {
    renderRow(item())

    expect(screen.queryByRole('button', { name: /^Autopilot:/ })).not.toBeInTheDocument()

    render(<AutopilotChip item={item()} />)
    expect(screen.queryByRole('button', { name: /^Autopilot:/ })).not.toBeInTheDocument()
  })

  it('says what the newest match did, on the row, after the session pill', () => {
    renderRow(item(), new Map([[1, autopilotMatch()]]))

    const chip = screen.getByRole('button', { name: /^Autopilot: Snoozed · Snooze passing bot PRs/ })

    expect(chip).toHaveTextContent('Snoozed')
    expect(chip).toHaveAttribute('title', 'Snoozed · Snooze passing bot PRs')
  })

  it('says what a Dry run would have done', () => {
    renderRow(item({ id: 2 }), new Map([[2, dryRunMatch()]]))

    expect(screen.getByRole('button', { name: /^Autopilot: Would run/ })).toHaveTextContent('Would run')
  })

  it('opens the item’s history, with each local step’s change and its Undo, and a way to Activity', async () => {
    const onShow = vi.fn<(target: AutopilotTarget) => void>()
    platform.autopilotItemHistory.mockResolvedValue({
      itemId: 1,
      matches: [
        autopilotMatch(),
        autopilotMatch({
          id: 9,
          createdAt: '2026-06-14T09:00:00Z',
          effects: [autopilotEffect({ id: 40, undoneAt: '2026-06-14T10:00:00Z', canUndo: false })],
        }),
      ],
      open: 0,
    })
    render(<AutopilotChip item={item()} />, { wrapper: withRows(new Map([[1, autopilotMatch()]]), onShow) })

    await userEvent.click(screen.getByRole('button', { name: /^Autopilot:/ }))

    const panel = within(screen.getByRole('dialog', { name: 'Autopilot history' }))
    const entries = await panel.findAllByRole('listitem')

    expect(platform.autopilotItemHistory).toHaveBeenCalledWith(1)
    expect(entries).toHaveLength(2)
    expect(entries[0]).toHaveTextContent('Snoozed · Still there')
    expect(entries[1]).toHaveTextContent('Snoozed · Undone')
    expect(within(entries[1] as HTMLElement).queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()

    await userEvent.click(within(entries[0] as HTMLElement).getByRole('button', { name: 'Undo' }))
    expect(platform.undoAutopilot).toHaveBeenCalledExactlyOnceWith([51])
    expect(await panel.findByText('Put back 1 item.')).toBeInTheDocument()

    await userEvent.click(within(entries[0] as HTMLElement).getByRole('button', { name: 'Show in Activity' }))
    expect(onShow).toHaveBeenCalledExactlyOnceWith(
      autopilotTarget({ routineId: 3, matchId: 12, createdAt: '2026-06-15T11:30:00Z' }),
    )
  })

  it('wraps its sentences, opened from a card’s line that does not wrap', async () => {
    const words =
      'It snoozed the pull request until its checks pass again, since the routine waits for a green run before it asks anyone to review.'
    const match = autopilotMatch({ words })
    platform.autopilotItemHistory.mockResolvedValue({ itemId: 1, matches: [match], open: 0 })
    platform.undoAutopilot.mockRejectedValue(
      new Error('Autopilot hasn’t started yet, so the change can’t be taken back; try again in a moment, once it has.'),
    )
    renderRow(item(), new Map([[1, match]]))

    await userEvent.click(screen.getByRole('button', { name: /^Autopilot:/ }))

    const dialog = screen.getByRole('dialog', { name: 'Autopilot history' })
    const panel = within(dialog)
    const entry = within(await panel.findByRole('listitem'))
    const undo = entry.getByRole('button', { name: 'Undo' })

    await userEvent.click(undo)
    const alert = await panel.findByRole('alert')

    expect(alert).toHaveTextContent(/so the change can’t be taken back/)
    expect(dialog.parentElement?.closest('.whitespace-nowrap')).not.toBeNull()
    expect(dialog).toHaveClass('whitespace-normal', 'break-words', 'w-[24rem]', 'max-w-[calc(100vw-2rem)]')
    for (const element of [
      panel.getByText('What the routines did with it'),
      panel.getByText(words),
      entry.getByText('Snoozed · Still there'),
      alert,
    ]) {
      expectWraps(element, dialog)
    }
    expect(undo).toHaveClass('shrink-0')
  })
})

describe('a routine’s marks on a row', () => {
  it('says which routine snoozed or dismissed an item, in its tab', () => {
    render(
      <ul>
        <ItemRow item={item({ snoozedUntil: '2026-06-18T12:00:00Z', setAsideBy: 3 })} launch={null} tab="snoozed" />
        <ItemRow
          item={item({ id: 2, dismissedAt: '2026-06-15T11:00:00Z', setAsideBy: 4 })}
          launch={null}
          tab="dismissed"
        />
      </ul>,
      { wrapper: withRows(new Map()) },
    )

    expect(screen.getByTitle(/^Snoozed by the routine Snooze passing bot PRs until/)).toHaveTextContent(
      /Snoozed by the routine Snooze passing bot PRs · back/,
    )
    expect(screen.getByTitle(/^Dismissed by the routine Investigate new bugs/)).toHaveTextContent(
      /Dismissed by the routine Investigate new bugs · 1h ago/,
    )
  })

  it('says which routine pinned an item, while it is pinned by one', () => {
    renderRow(item({ pinnedAt: '2026-06-15T11:00:00Z', pinnedBy: 3 }))

    expect(screen.getByTitle('Pinned by the routine Snooze passing bot PRs')).toBeInTheDocument()
  })

  it('says nothing of a routine where the user set the item aside', () => {
    render(
      <ul>
        <ItemRow item={item({ snoozedUntil: '2026-06-18T12:00:00Z' })} launch={null} tab="snoozed" />
      </ul>,
      { wrapper: withRows(new Map()) },
    )

    expect(screen.queryByText(/by the routine/)).not.toBeInTheDocument()
    expect(screen.getByTitle(/^Snoozed until/)).toBeInTheDocument()
  })

  describe('what runs next', () => {
    const next = 'Autopilot: Merge syncs runs when its checks pass. Show what runs next'

    it('says, dashed, which routine will run and when, before it does', () => {
      renderRow(item(), new Map(), new Map([[1, [autopilotUpcoming()]]]))

      const chip = screen.getByRole('button', { name: next })

      expect(chip).toHaveTextContent('Runs when its checks pass')
      expect(chip).toHaveAttribute('title', 'Merge syncs runs when its checks pass')
      expect(chip).toHaveAttribute('data-upcoming')
      expect(chip).toHaveClass('outline-dashed', 'text-accent')
    })

    it('counts down to a moment the clock decides, says when it is due, and how many more will run', () => {
      const settling = autopilotUpcoming({
        when: 'once its checks settle',
        at: '2026-06-15T12:02:00Z',
        mode: 'ask',
        asksFirst: true,
      })
      const due = autopilotUpcoming({ itemId: 2, at: '2026-06-15T11:59:00Z' })

      renderRow(item(), new Map(), new Map([[1, [settling, autopilotUpcoming({ routineId: 8, routine: 'Tell me' })]]]))
      renderRow(item({ id: 2, number: 13, title: 'Another' }), new Map(), new Map([[2, [due]]]))

      const chip = screen.getByRole('button', { name: /^Autopilot: Merge syncs asks you first in 2m · 1 more/ })

      expect(chip).toHaveTextContent('Runs in 2m')
      expect(screen.getByRole('button', { name: /^Autopilot: Merge syncs runs any moment/ })).toHaveTextContent(
        'Runs any moment',
      )
    })

    it('says what goes on now before what runs next, and what runs next before what was', () => {
      const queued = autopilotMatch({ state: 'queued', reason: '', blockedBy: 'slot', closedAt: null })

      renderRow(item(), new Map([[1, queued]]), new Map([[1, [autopilotUpcoming()]]]))
      renderRow(
        item({ id: 2, number: 13, title: 'Another' }),
        new Map([[2, autopilotMatch({ item: { ...autopilotMatch().item, id: 2 } })]]),
        new Map([[2, [autopilotUpcoming({ itemId: 2 })]]]),
      )

      expect(screen.getAllByRole('button', { name: /^Autopilot:/ }).map((b) => b.getAttribute('aria-label'))).toEqual([
        expect.stringMatching(/^Autopilot: Queued/),
        expect.stringMatching(/^Autopilot: Merge syncs runs when its checks pass/),
      ])
    })

    it('opens Up next, how and when each runs and what it will do, above the item’s history', async () => {
      const onShow = vi.fn<(target: AutopilotTarget) => void>()
      platform.autopilotItemHistory.mockResolvedValue({ itemId: 1, matches: [autopilotMatch()], open: 0 })
      renderRow(item(), new Map([[1, autopilotMatch()]]), new Map([[1, [autopilotUpcoming()]]]), onShow)

      await userEvent.click(screen.getByRole('button', { name: next }))

      const panel = within(screen.getByRole('dialog', { name: 'Autopilot' }))
      const upNext = within(panel.getByRole('region', { name: 'Up next' }))

      expect(upNext.getByText('Merge syncs')).toBeInTheDocument()
      expect(upNext.getByText('Runs by itself when its checks pass')).toBeInTheDocument()
      expect(upNext.getByText('Then: Comment one of my replies, then Merge it')).toBeInTheDocument()
      expect(await panel.findByText('What the routines did with it')).toBeInTheDocument()

      await userEvent.click(upNext.getByRole('button', { name: 'Open routine' }))

      expect(onShow).toHaveBeenCalledExactlyOnceWith({
        routineId: 7,
        matchId: 0,
        runId: 0,
        createdAt: '',
        activity: false,
      })
    })

    it('leaves the item alone on a click, keeping the line with Undo, which lets it act again', async () => {
      renderRow(item(), new Map(), new Map([[1, [autopilotUpcoming()]]]))

      await userEvent.click(screen.getByRole('button', { name: next }))

      const panel = within(screen.getByRole('dialog', { name: 'Autopilot' }))

      expect(panel.queryByText('What the routines did with it')).not.toBeInTheDocument()

      await userEvent.click(panel.getByRole('button', { name: 'Leave this one alone' }))

      expect(platform.leaveItemAlone).toHaveBeenCalledExactlyOnceWith(7, 1)
      expect(await panel.findByRole('status')).toHaveTextContent('It leaves this one alone now')

      await userEvent.click(panel.getByRole('button', { name: 'Undo' }))

      expect(platform.letRoutineActAgain).toHaveBeenCalledExactlyOnceWith(7, 1)
      expect(await panel.findByText('Runs by itself when its checks pass')).toBeInTheDocument()
    })

    it('says why it couldn’t leave the item alone', async () => {
      platform.leaveItemAlone.mockRejectedValue(new Error('Autopilot is off. Turn it on in Settings → Labs.'))
      renderRow(item(), new Map(), new Map([[1, [autopilotUpcoming()]]]))

      await userEvent.click(screen.getByRole('button', { name: next }))
      await userEvent.click(screen.getByRole('button', { name: 'Leave this one alone' }))

      expect(await screen.findByText(/Autopilot is off/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Leave this one alone' })).toBeInTheDocument()
    })
  })
})
