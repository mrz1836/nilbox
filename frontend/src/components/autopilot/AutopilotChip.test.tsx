import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { AutopilotMatch, AutopilotTarget, InboxItem } from '../../platform'
import { at } from '../../test/clock'
import { autopilotEffect, autopilotMatch, dryRunMatch, item } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ItemRow } from '../ItemRow'
import { AutopilotChip, AutopilotRowsContext } from './AutopilotChip'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const names = new Map([
  [3, 'Snooze passing bot PRs'],
  [4, 'Investigate new bugs'],
])

// withRows renders inside the rows' Autopilot, with the chips given, as the window does.
function withRows(chips: ReadonlyMap<number, AutopilotMatch>, onShow?: (target: AutopilotTarget) => void) {
  const Clock = at()

  return function Rows({ children }: { children: ReactNode }) {
    return (
      <Clock>
        <AutopilotRowsContext value={{ names, chips, onShow }}>{children}</AutopilotRowsContext>
      </Clock>
    )
  }
}

function renderRow(it: InboxItem, chips: ReadonlyMap<number, AutopilotMatch> = new Map()) {
  render(
    <ul>
      <ItemRow item={it} launch={null} />
    </ul>,
    { wrapper: withRows(chips) },
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
    expect(onShow).toHaveBeenCalledExactlyOnceWith({
      routineId: 3,
      matchId: 12,
      runId: 0,
      createdAt: '2026-06-15T11:30:00Z',
      activity: false,
    })
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
})
