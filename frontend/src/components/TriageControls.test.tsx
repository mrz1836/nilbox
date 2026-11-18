import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { RowWork } from '../lib/rowWork'
import type { TriageTab } from '../lib/triage'
import type { InboxItem } from '../platform'
import { at } from '../test/clock'
import { item } from '../test/fixtures'
import { SheetTriage, TriageControls, type TriageActions } from './TriageControls'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function triageActions(): TriageActions {
  return {
    snoozeDays: 3,
    onDismiss: vi.fn(),
    onSnooze: vi.fn(),
    onRestore: vi.fn(),
    onPin: vi.fn(),
    onUnpin: vi.fn(),
    onRefresh: vi.fn(() => Promise.resolve()),
    onChangeDefault: vi.fn(),
    repoRules: { pinned: [], muted: [] },
    onRepoRule: vi.fn(),
    onRepoRuleSettings: vi.fn(),
    onMarkRead: vi.fn(),
    onMarkUnread: vi.fn(),
  }
}

const pinned = item({ pinnedAt: '2026-06-15T10:00:00Z' })

// cardTriage renders a card's triage for an item in a tab, the row doing work.
function cardTriage(it: InboxItem, tab: TriageTab, work: RowWork = null) {
  const actions = triageActions()
  render(<TriageControls item={it} tab={tab} actions={actions} work={work} start={vi.fn()} mode="card" />, {
    wrapper: at(),
  })

  return actions
}

// hidden reports a control shown only while the pointer or focus is on the card.
function hidden(el: HTMLElement): boolean {
  return el.closest('.opacity-0.group-hover\\:opacity-100') !== null
}

describe('TriageControls, card mode', () => {
  it('shows Refresh, Snooze, Dismiss, and Pin only while the pointer or focus is on the card', () => {
    cardTriage(item(), 'inbox')

    for (const name of ['Refresh from GitHub', 'Snooze for 3 days', 'Dismiss', 'Pin to top']) {
      expect(hidden(screen.getByRole('button', { name }))).toBe(true)
    }
  })

  it('shows a pinned item’s Unpin only on hover too, since the card marks a pinned item itself', async () => {
    const actions = cardTriage(pinned, 'inbox')

    const unpin = screen.getByRole('button', { name: 'Unpin' })
    expect(hidden(unpin)).toBe(true)

    await userEvent.click(unpin)
    expect(actions.onUnpin).toHaveBeenCalledExactlyOnceWith(pinned)
  })

  it('keeps every button shown while the card works with GitHub', () => {
    cardTriage(item(), 'inbox', 'refresh')

    expect(hidden(screen.getByRole('button', { name: 'Dismiss' }))).toBe(false)
    expect(hidden(screen.getByRole('button', { name: 'Pin to top' }))).toBe(false)
  })

  it.each(['snoozed', 'dismissed', 'muted'] as const)(
    'offers only Pin or Unpin in the %s tab, the card’s primary bringing it back',
    async (tab) => {
      const actions = cardTriage(item(), tab)

      const pin = screen.getByRole('button', { name: 'Pin to top' })
      expect(screen.getAllByRole('button')).toEqual([pin])
      expect(hidden(pin)).toBe(true)

      await userEvent.click(pin)
      expect(actions.onPin).toHaveBeenCalledExactlyOnceWith(item())
    },
  )

  it('offers Unpin in a set-aside tab for a pinned item', () => {
    cardTriage(pinned, 'snoozed')

    expect(screen.getAllByRole('button').map((b) => b.getAttribute('aria-label'))).toEqual(['Unpin'])
  })

  it.each(['inbox', 'snoozed'] as const)(
    'offers no pin in the %s tab for an item pinned only with its repository, which the card marks',
    (tab) => {
      cardTriage(item({ repoRule: 'pinned' }), tab)

      expect(screen.queryByRole('button', { name: /^(Pin to top|Unpin)$/ })).not.toBeInTheDocument()
      expect(screen.queryByRole('img', { name: /^Pinned with/ })).not.toBeInTheDocument()
    },
  )
})

describe('TriageControls, row mode', () => {
  it('keeps a pinned item’s Unpin always shown, and brings a set-aside item back', () => {
    const actions = triageActions()
    const { rerender } = render(
      <TriageControls item={pinned} tab="inbox" actions={actions} work={null} start={vi.fn()} />,
      { wrapper: at() },
    )

    expect(hidden(screen.getByRole('button', { name: 'Unpin' }))).toBe(false)

    rerender(<TriageControls item={item()} tab="snoozed" actions={actions} work={null} start={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Unsnooze' })).toBeInTheDocument()
  })
})

describe('SheetTriage', () => {
  it('shows Snooze, Dismiss, and Pin at once in the inbox tab', () => {
    render(<SheetTriage item={item()} tab="inbox" actions={triageActions()} />, { wrapper: at() })

    for (const name of ['Snooze for 3 days', 'Dismiss', 'Pin to top']) {
      expect(hidden(screen.getByRole('button', { name }))).toBe(false)
    }
    expect(screen.queryByRole('button', { name: 'Refresh from GitHub' })).not.toBeInTheDocument()
  })

  it.each([
    ['snoozed', 'Unsnooze'],
    ['dismissed', 'Restore'],
  ] as const)('brings an item back from the %s tab', async (tab, name) => {
    const actions = triageActions()
    render(<SheetTriage item={item()} tab={tab} actions={actions} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name }))

    expect(actions.onRestore).toHaveBeenCalledExactlyOnceWith(item())
  })

  it('unmutes what mutes the item from the Muted tab', async () => {
    const actions = triageActions()
    render(<SheetTriage item={item()} tab="muted" actions={actions} />, { wrapper: at() })

    await userEvent.click(screen.getByRole('button', { name: 'Unmute acme-org/example-repo' }))

    expect(actions.onRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org/example-repo', '')
  })
})
