import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { TriageTab } from '../lib/triage'
import type { InboxItem } from '../platform'
import { at } from '../test/clock'
import { item } from '../test/fixtures'
import { platform } from '../test/platform'
import { teamsText, type EditActions } from './ItemMenu'
import { ItemRow } from './ItemRow'
import type { TriageActions } from './TriageControls'

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
  }
}

function editActions(): EditActions {
  return {
    onCopyLink: vi.fn(),
    onUnassign: vi.fn(() => Promise.resolve()),
    onRemoveReview: vi.fn(() => Promise.resolve()),
  }
}

// renderRow renders one row with its triage buttons and its menu, and returns what they
// do.
function renderRow(it: InboxItem, tab: TriageTab = 'inbox') {
  const triage = triageActions()
  const edits = editActions()

  render(
    <ul>
      <ItemRow item={it} launch={null} tab={tab} triage={triage} edits={edits} />
    </ul>,
    { wrapper: at() },
  )

  return { triage, edits }
}

// openMenu opens the row's menu and returns the queries scoped to it.
async function openMenu() {
  await userEvent.click(screen.getByRole('button', { name: 'More actions' }))

  return within(screen.getByRole('menu', { name: 'More actions' }))
}

// openSubmenu opens a submenu of the open menu by its item, and returns its queries.
async function openSubmenu(label: string) {
  await userEvent.click(screen.getByRole('menuitem', { name: label }))

  return within(screen.getByRole('menu', { name: label }))
}

// names lists a menu's own items by name.
function names(menu: HTMLElement): string[] {
  return within(menu)
    .getAllByRole('menuitem')
    .filter((entry) => entry.closest('[role="menu"]') === menu)
    .map((entry) => entry.textContent)
}

describe('ItemMenu', () => {
  it('shows with the row’s quiet controls', () => {
    renderRow(item())

    const button = screen.getByRole('button', { name: 'More actions' })
    expect(button).toHaveAttribute('aria-haspopup', 'menu')
    expect(button.parentElement?.parentElement).toHaveClass('opacity-0', 'group-hover:opacity-100')
  })

  it('opens the item on GitHub, or copies its link', async () => {
    const it = item({ url: 'https://github.com/acme-org/example-repo/pull/7' })
    const { edits } = renderRow(it)

    await openMenu()
    expect(names(screen.getByRole('menu'))).toEqual(['Open on GitHub', 'Copy link', 'Inbox actions'])

    await userEvent.click(screen.getByRole('menuitem', { name: 'Open on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'More actions' })).toHaveFocus()

    await openMenu()
    await userEvent.click(screen.getByRole('menuitem', { name: 'Copy link' }))
    expect(edits.onCopyLink).toHaveBeenCalledWith(it)
  })

  it('gathers the inbox actions in a submenu', async () => {
    const it = item()
    const { triage } = renderRow(it)

    await openMenu()
    const actions = await openSubmenu('Inbox actions')

    expect(names(screen.getByRole('menu', { name: 'Inbox actions' }))).toEqual([
      'Refresh from GitHub',
      'Pin to top',
      'Snooze',
      'Dismiss',
    ])
    expect(actions.getByRole('menuitem', { name: 'Refresh from GitHub' })).toHaveFocus()

    await userEvent.click(actions.getByRole('menuitem', { name: 'Dismiss' }))

    expect(triage.onDismiss).toHaveBeenCalledWith(it)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    await openMenu()
    await openSubmenu('Inbox actions')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pin to top' }))

    expect(triage.onPin).toHaveBeenCalledWith(it)
  })

  it('snoozes for a length from a submenu of its own', async () => {
    const it = item()
    const { triage } = renderRow(it)

    await openMenu()
    await openSubmenu('Inbox actions')
    const lengths = await openSubmenu('Snooze')

    expect(lengths.getByRole('menuitem', { name: /^3 days,/ })).toHaveTextContent('Default')

    await userEvent.click(lengths.getByRole('menuitem', { name: /^1 week,/ }))

    expect(triage.onSnooze).toHaveBeenCalledWith(it, 7)
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('unpins a pinned item, and offers a set-aside item back', async () => {
    const it = item({ pinnedAt: '2026-06-15T10:00:00Z', snoozedUntil: '2026-06-20T10:00:00Z' })
    const { triage } = renderRow(it, 'snoozed')

    await openMenu()
    await openSubmenu('Inbox actions')
    expect(names(screen.getByRole('menu', { name: 'Inbox actions' }))).toEqual([
      'Unsnooze',
      'Unpin',
      'Refresh from GitHub',
    ])

    await userEvent.click(screen.getByRole('menuitem', { name: 'Unpin' }))
    expect(triage.onUnpin).toHaveBeenCalledWith(it)

    await openMenu()
    await openSubmenu('Inbox actions')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Unsnooze' }))
    expect(triage.onRestore).toHaveBeenCalledWith(it)
  })

  it('offers a dismissed item back with Restore', async () => {
    renderRow(item({ dismissedAt: '2026-06-15T10:00:00Z' }), 'dismissed')

    await openMenu()
    await openSubmenu('Inbox actions')

    expect(screen.getByRole('menuitem', { name: 'Restore' })).toHaveAttribute('title', 'Put it back in the list')
  })

  it('takes the user off an item assigned to them, holding the row while GitHub makes the change', async () => {
    let done: () => void = () => undefined
    const it = item({ assigned: true })
    const { edits, triage } = renderRow(it)
    vi.mocked(edits.onUnassign).mockReturnValue(
      new Promise((resolve) => {
        done = resolve
      }),
    )

    const menu = await openMenu()
    expect(menu.getByText('On GitHub')).toHaveAttribute('aria-hidden', 'true')

    const unassign = menu.getByRole('menuitem', { name: 'Unassign me' })
    expect(unassign).toHaveAttribute('title', 'Take yourself off its assignees on GitHub. Uses one REST request.')

    await userEvent.click(unassign)

    expect(edits.onUnassign).toHaveBeenCalledExactlyOnceWith(it)

    const button = screen.getByRole('button', { name: 'More actions' })
    expect(button).toHaveAttribute('title', 'Unassigning you on GitHub…')
    expect(button.parentElement?.parentElement).toHaveClass('opacity-100')
    expect(screen.getByRole('button', { name: 'Refresh from GitHub' })).toBeDisabled()

    // A second click while GitHub works sends nothing more.
    await openMenu()
    expect(screen.getByRole('menuitem', { name: 'Unassign me' })).toBeDisabled()
    await userEvent.keyboard('{Escape}')

    await act(async () => {
      done()
      await Promise.resolve()
    })

    expect(button).toHaveAttribute('title', 'More actions')
    expect(screen.getByRole('button', { name: 'Refresh from GitHub' })).toBeEnabled()
    expect(edits.onUnassign).toHaveBeenCalledTimes(1)
    expect(triage.onRefresh).not.toHaveBeenCalled()
  })

  it('takes the user off a pull request’s reviewers when it asks them by name', async () => {
    const it = item({ reviewRequest: 'you', reviewTeams: ['backend'] })
    const { edits } = renderRow(it)

    const menu = await openMenu()
    expect(names(screen.getByRole('menu'))).toEqual([
      'Open on GitHub',
      'Copy link',
      'Inbox actions',
      'Remove me as reviewer',
    ])

    await userEvent.click(menu.getByRole('menuitem', { name: 'Remove me as reviewer' }))

    expect(edits.onRemoveReview).toHaveBeenCalledWith(it)
  })

  it('says why a review asked of a team cannot be taken off one member', async () => {
    const { edits } = renderRow(item({ reviewRequest: 'team', reviewTeams: ['backend'], assigned: true }))

    const menu = await openMenu()
    const remove = menu.getByRole('menuitem', { name: 'Remove me as reviewer' })

    expect(remove).toBeDisabled()
    expect(remove).toHaveAttribute(
      'title',
      'GitHub asks the @backend team to review this, not you by name, and takes a team’s request off for every member or none. Dismiss it to hide it here.',
    )
    expect(menu.getAllByText('On GitHub')).toHaveLength(1)

    await userEvent.click(remove)
    expect(edits.onRemoveReview).not.toHaveBeenCalled()
  })

  it('refreshes from the menu as the row’s button does, and shows it on the button', async () => {
    let done: () => void = () => undefined
    const it = item()
    const { triage } = renderRow(it)
    vi.mocked(triage.onRefresh).mockReturnValue(
      new Promise((resolve) => {
        done = resolve
      }),
    )

    await openMenu()
    await openSubmenu('Inbox actions')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Refresh from GitHub' }))

    expect(triage.onRefresh).toHaveBeenCalledWith(it)

    const refresh = screen.getByRole('button', { name: 'Refresh from GitHub' })
    expect(refresh).toBeDisabled()
    expect(refresh.querySelector('svg')).toHaveClass('motion-safe:animate-spin')
    expect(screen.getByRole('button', { name: 'More actions' })).toHaveAttribute('title', 'Refreshing from GitHub…')

    await act(async () => {
      done()
      await Promise.resolve()
    })

    expect(refresh).toBeEnabled()
  })
})

describe('teamsText', () => {
  it('names the teams a pull request asks', () => {
    expect(teamsText([])).toBe('a team')
    expect(teamsText(['backend'])).toBe('the @backend team')
    expect(teamsText(['backend', 'infra'])).toBe('the @backend and @infra teams')
    expect(teamsText(['backend', 'infra', 'web'])).toBe('the @backend, @infra and @web teams')
  })
})

describe('ItemMenu review', () => {
  it('opens a pull request in the review sheet', async () => {
    const triage = triageActions()
    const edits = { ...editActions(), onReview: vi.fn() }
    const it = item()

    render(
      <ul>
        <ItemRow item={it} launch={null} tab="inbox" triage={triage} edits={edits} />
      </ul>,
      { wrapper: at() },
    )

    const menu = await openMenu()
    await userEvent.click(menu.getByRole('menuitem', { name: 'Review…' }))
    expect(edits.onReview).toHaveBeenCalledWith(it, 'inbox')
  })

  it('offers no review of an issue', async () => {
    const edits = { ...editActions(), onReview: vi.fn() }

    render(
      <ul>
        <ItemRow item={item({ kind: 'issue' })} launch={null} triage={triageActions()} edits={edits} />
      </ul>,
      { wrapper: at() },
    )

    const menu = await openMenu()
    expect(menu.queryByRole('menuitem', { name: 'Review…' })).not.toBeInTheDocument()
  })
})
