import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { TriageTab } from '../lib/triage'
import type { AutopilotRoutine, InboxItem, RepoRules } from '../platform'
import { at } from '../test/clock'
import { autopilotMatch, details, explanation, item, routine, routineSpec } from '../test/fixtures'
import { platform } from '../test/platform'
import { AutopilotMenuContext, useRoutineMenu } from './autopilot/RoutineMenu'
import { teamsText, type EditActions } from './ItemMenu'
import { ItemRow } from './ItemRow'
import { defaultToastSeconds } from './Toast'
import type { TriageActions } from './TriageControls'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function triageActions(repoRules: RepoRules = { pinned: [], muted: [] }): TriageActions {
  return {
    snoozeDays: 3,
    onDismiss: vi.fn(),
    onSnooze: vi.fn(),
    onRestore: vi.fn(),
    onPin: vi.fn(),
    onUnpin: vi.fn(),
    onRefresh: vi.fn(() => Promise.resolve()),
    onChangeDefault: vi.fn(),
    repoRules,
    onRepoRule: vi.fn(),
    onRepoRuleSettings: vi.fn(),
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
function renderRow(it: InboxItem, tab: TriageTab = 'inbox', repoRules?: RepoRules) {
  const triage = triageActions(repoRules)
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
      'Pin or mute repository',
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
      'Pin or mute repository',
      'Refresh from GitHub',
    ])

    await userEvent.click(screen.getByRole('menuitem', { name: 'Unpin' }))
    expect(triage.onUnpin).toHaveBeenCalledWith(it)

    await openMenu()
    await openSubmenu('Inbox actions')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Unsnooze' }))
    expect(triage.onRestore).toHaveBeenCalledWith(it)
  })

  it('pins or mutes the item’s repository or its whole org', async () => {
    const { triage } = renderRow(item({ repo: 'acme-org/example-repo' }))

    await openMenu()
    await openSubmenu('Inbox actions')
    const rules = await openSubmenu('Pin or mute repository')

    expect(names(screen.getByRole('menu', { name: 'Pin or mute repository' }))).toEqual([
      'Pin acme-org/example-repo',
      'Mute acme-org/example-repo',
      'Pin everything in acme-org',
      'Mute everything in acme-org',
      'Pinned and muted…',
    ])

    await userEvent.click(rules.getByRole('menuitem', { name: 'Mute everything in acme-org' }))
    expect(triage.onRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org', 'muted')
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()

    await openMenu()
    await openSubmenu('Inbox actions')
    await openSubmenu('Pin or mute repository')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pinned and muted…' }))
    expect(triage.onRepoRuleSettings).toHaveBeenCalledOnce()
  })

  it('takes a rule off the target it is set on, and moves a target from one rule to the other', async () => {
    const { triage } = renderRow(item({ repo: 'acme-org/example-repo', repoRule: 'pinned' }), 'inbox', {
      pinned: ['Acme-Org/Example-Repo'],
      muted: ['acme-org'],
    })

    await openMenu()
    await openSubmenu('Inbox actions')
    await openSubmenu('Pin or mute repository')
    expect(names(screen.getByRole('menu', { name: 'Pin or mute repository' }))).toEqual([
      'Unpin acme-org/example-repo',
      'Mute acme-org/example-repo',
      'Pin everything in acme-org',
      'Unmute everything in acme-org',
      'Pinned and muted…',
    ])

    await userEvent.click(screen.getByRole('menuitem', { name: 'Unpin acme-org/example-repo' }))
    expect(triage.onRepoRule).toHaveBeenLastCalledWith('acme-org/example-repo', '')

    await openMenu()
    await openSubmenu('Inbox actions')
    await openSubmenu('Pin or mute repository')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pin everything in acme-org' }))
    expect(triage.onRepoRule).toHaveBeenLastCalledWith('acme-org', 'pinned')
  })

  it('unmutes a muted item’s repository or org from the Muted tab', async () => {
    const it = item({ repo: 'acme-org/example-repo', repoRule: 'muted' })
    const { triage } = renderRow(it, 'muted', { pinned: [], muted: ['acme-org'] })

    const unmute = screen.getByRole('button', { name: 'Unmute acme-org' })
    expect(unmute).toHaveTextContent('Unmute')
    await userEvent.click(unmute)
    expect(triage.onRepoRule).toHaveBeenCalledExactlyOnceWith('acme-org', '')

    await openMenu()
    await openSubmenu('Inbox actions')
    expect(names(screen.getByRole('menu', { name: 'Inbox actions' }))).toEqual([
      'Unmute acme-org',
      'Pin to top',
      'Refresh from GitHub',
    ])
    await userEvent.click(screen.getByRole('menuitem', { name: 'Unmute acme-org' }))
    expect(triage.onRepoRule).toHaveBeenLastCalledWith('acme-org', '')
  })

  it('marks an item pinned with its repository, which a row doesn’t unpin', async () => {
    const it = item({ repo: 'acme-org/example-repo', repoRule: 'pinned' })
    const { triage } = renderRow(it, 'inbox', { pinned: ['acme-org'], muted: [] })

    // The card marks it before its title; its triage offers no pin to take off.
    const mark = screen.getByRole('img', { name: 'Pinned with acme-org' })
    expect(mark).toHaveAttribute('title', expect.stringContaining('the card’s menu'))
    expect(screen.queryByRole('button', { name: 'Unpin' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Pin to top' })).not.toBeInTheDocument()

    await openMenu()
    await openSubmenu('Inbox actions')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Pin to top' }))
    expect(triage.onPin).toHaveBeenCalledWith(it)
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

    // The row fades under a note saying what GitHub is doing, since it will likely leave.
    const row = screen.getByRole('listitem')
    expect(row).toHaveAttribute('aria-busy', 'true')
    const note = within(row).getByRole('status')
    expect(note).toHaveTextContent('Unassigning you on GitHub…')
    expect(note.closest('[data-row-status]')?.parentElement).toBe(row)

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
    // An item GitHub still keeps in the inbox comes back as it was.
    expect(row).not.toHaveAttribute('aria-busy')
    expect(within(row).queryByRole('status')).not.toBeInTheDocument()
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

  it('fades the row while GitHub takes the user off its reviewers, and never for a refresh', async () => {
    let done: () => void = () => undefined
    const it = item({ reviewRequest: 'you' })
    const { edits, triage } = renderRow(it)
    const hold = () =>
      new Promise<void>((resolve) => {
        done = resolve
      })
    vi.mocked(edits.onRemoveReview).mockImplementation(hold)
    vi.mocked(triage.onRefresh).mockImplementation(hold)

    const row = screen.getByRole('listitem')

    await userEvent.click(screen.getByRole('button', { name: 'Refresh from GitHub' }))
    // A refresh leaves the item in its list, so the row stays as it is.
    expect(row).not.toHaveAttribute('aria-busy')
    expect(within(row).queryByRole('status')).not.toBeInTheDocument()

    await act(async () => {
      done()
      await Promise.resolve()
    })

    await userEvent.click((await openMenu()).getByRole('menuitem', { name: 'Remove me as reviewer' }))

    expect(row).toHaveAttribute('aria-busy', 'true')
    expect(within(row).getByRole('status')).toHaveTextContent('Removing you as a reviewer on GitHub…')

    await act(async () => {
      done()
      await Promise.resolve()
    })

    expect(row).not.toHaveAttribute('aria-busy')
  })

  it('offers no way off a pull request in Review requested only because it was pushed to since', async () => {
    renderRow(item({ reviewRequest: 'pushed', details: details({ pushedSinceReview: true }) }))

    const menu = await openMenu()

    expect(menu.queryByRole('menuitem', { name: 'Remove me as reviewer' })).not.toBeInTheDocument()
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

describe('ItemMenu open', () => {
  it('opens a pull request in the review sheet', async () => {
    const triage = triageActions()
    const edits = { ...editActions(), onOpen: vi.fn() }
    const it = item()

    render(
      <ul>
        <ItemRow item={it} launch={null} tab="inbox" triage={triage} edits={edits} />
      </ul>,
      { wrapper: at() },
    )

    const menu = await openMenu()
    await userEvent.click(menu.getByRole('menuitem', { name: 'Review…' }))
    expect(edits.onOpen).toHaveBeenCalledWith(it, 'inbox')
  })

  it('opens an issue in the issue sheet', async () => {
    const edits = { ...editActions(), onOpen: vi.fn() }
    const it = item({ kind: 'issue' })

    render(
      <ul>
        <ItemRow item={it} launch={null} tab="inbox" triage={triageActions()} edits={edits} />
      </ul>,
      { wrapper: at() },
    )

    const menu = await openMenu()
    expect(menu.queryByRole('menuitem', { name: 'Review…' })).not.toBeInTheDocument()
    await userEvent.click(menu.getByRole('menuitem', { name: 'View issue…' }))
    expect(edits.onOpen).toHaveBeenCalledWith(it, 'inbox')
  })

  it('offers neither without a sheet', async () => {
    render(
      <ul>
        <ItemRow item={item()} launch={null} triage={triageActions()} edits={editActions()} />
      </ul>,
      { wrapper: at() },
    )

    const menu = await openMenu()
    expect(menu.queryByRole('menuitem', { name: 'Review…' })).not.toBeInTheDocument()
    expect(menu.queryByRole('menuitem', { name: 'Autopilot' })).not.toBeInTheDocument()
  })
})

// menuRoutines are the routines the window lists: one on for pull requests, one off,
// one on for issues alone, and another account's.
function menuRoutines(): AutopilotRoutine[] {
  return [
    routine(),
    routine({ id: 4, name: 'Investigate slow reviews', enabled: false, enabledAt: null }),
    routine({
      id: 7,
      name: 'Triage new issues',
      spec: { ...routineSpec(), watch: { sources: [{ search: { kind: 'issue', where: ['@me'] } }] } },
    }),
    routine({ id: 8, name: 'Someone else’s', otherAccount: true, account: 'octo-other' }),
  ]
}

// WithRoutines provides the item menus' Autopilot as the window does, over routines.
function WithRoutines({ routines, children }: { routines: readonly AutopilotRoutine[]; children: ReactNode }) {
  const { menu, node } = useRoutineMenu(routines, defaultToastSeconds, () => undefined)

  return (
    <AutopilotMenuContext value={menu}>
      {children}
      {node}
    </AutopilotMenuContext>
  )
}

// renderWithRoutines renders one row whose menu offers Autopilot, once the catalog that
// says what each routine acts on is read.
async function renderWithRoutines(it: InboxItem = item(), routines = menuRoutines()) {
  render(
    <WithRoutines routines={routines}>
      <ul>
        <ItemRow item={it} launch={null} triage={triageActions()} edits={editActions()} />
      </ul>
    </WithRoutines>,
    { wrapper: at() },
  )

  await waitFor(() => {
    expect(platform.getAutopilotCatalog).toHaveBeenCalledOnce()
  })
  await act(async () => {
    await Promise.resolve()
  })
}

// openRoutines opens the row's Autopilot ▸ Run a routine ▸, and returns its menu.
async function openRoutines() {
  await openMenu()
  await openSubmenu('Autopilot')

  return openSubmenu('Run a routine')
}

describe('ItemMenu Autopilot', () => {
  it('runs a routine that is on and acts on the item’s kind, saying what became of it', async () => {
    await renderWithRoutines()

    await openMenu()
    expect(names(screen.getByRole('menu', { name: 'More actions' }))).toContain('Autopilot')
    await openSubmenu('Autopilot')
    expect(names(screen.getByRole('menu', { name: 'Autopilot' }))).toEqual([
      'Run a routine',
      'Make a routine from this…',
      'Why didn’t a routine run?',
    ])

    const run = await openSubmenu('Run a routine')

    expect(names(screen.getByRole('menu', { name: 'Run a routine' }))).toEqual(['Snooze passing bot PRs'])

    await userEvent.click(run.getByRole('menuitem', { name: 'Snooze passing bot PRs' }))

    expect(platform.runRoutineNow).toHaveBeenCalledWith(3, 1, false)
    expect(await screen.findByText('It starts as soon as a slot frees.')).toBeInTheDocument()
    expect(platform.explainRoutine).not.toHaveBeenCalled()
  })

  it('offers Run anyway on an item a routine doesn’t match, from the refusal alone, and runs it on asking', async () => {
    platform.runRoutineNow.mockResolvedValueOnce({ ran: false, notMatching: 'It doesn’t match it: checks pass.' })
    await renderWithRoutines()

    await userEvent.click((await openRoutines()).getByRole('menuitem', { name: 'Snooze passing bot PRs' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Run “Snooze passing bot PRs” anyway?' }))

    expect(dialog.getByText('It doesn’t match it: checks pass.')).toBeInTheDocument()
    expect(platform.explainRoutine).not.toHaveBeenCalled()

    await userEvent.click(dialog.getByRole('button', { name: 'Run anyway' }))

    expect(platform.runRoutineNow).toHaveBeenLastCalledWith(3, 1, true)
    expect(await screen.findByText('It starts as soon as a slot frees.')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    // Its toast closes on asking.
    await userEvent.click(
      within(screen.getByRole('group', { name: 'Notification' })).getByRole('button', { name: 'Close' }),
    )
    expect(screen.queryByText('It starts as soon as a slot frees.')).not.toBeInTheDocument()
  })

  it('runs nothing when Run anyway is cancelled', async () => {
    platform.runRoutineNow.mockResolvedValueOnce({ ran: false, notMatching: 'It doesn’t match it: checks pass.' })
    await renderWithRoutines()

    await userEvent.click((await openRoutines()).getByRole('menuitem', { name: 'Snooze passing bot PRs' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Run “Snooze passing bot PRs” anyway?' }))

    await userEvent.click(dialog.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.runRoutineNow).toHaveBeenCalledOnce()
  })

  it('tells any other refusal as it is, and offers nothing more', async () => {
    platform.runRoutineNow.mockRejectedValueOnce(new Error('The item isn’t stored on this Mac.'))
    await renderWithRoutines()

    await userEvent.click((await openRoutines()).getByRole('menuitem', { name: 'Snooze passing bot PRs' }))

    expect(await screen.findByText('Couldn’t run “Snooze passing bot PRs”')).toBeInTheDocument()
    expect(screen.getByText('· The item isn’t stored on this Mac.')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.explainRoutine).not.toHaveBeenCalled()
  })

  it('tells a Run anyway that is refused as it is', async () => {
    platform.runRoutineNow
      .mockResolvedValueOnce({ ran: false, notMatching: 'It doesn’t match it: checks pass.' })
      .mockRejectedValueOnce(new Error('It acts only on pull requests.'))
    await renderWithRoutines()

    await userEvent.click((await openRoutines()).getByRole('menuitem', { name: 'Snooze passing bot PRs' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Run “Snooze passing bot PRs” anyway?' }))

    await userEvent.click(dialog.getByRole('button', { name: 'Run anyway' }))

    expect(await screen.findByText('Couldn’t run “Snooze passing bot PRs”')).toBeInTheDocument()
    expect(screen.getByText('· It acts only on pull requests.')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('never asks Run anyway twice: one that still doesn’t match is told as it is', async () => {
    platform.runRoutineNow
      .mockResolvedValueOnce({ ran: false, notMatching: 'It doesn’t match it: checks pass.' })
      .mockResolvedValueOnce({ ran: false, notMatching: 'It doesn’t match it: it isn’t a bot’s.' })
    await renderWithRoutines()

    await userEvent.click((await openRoutines()).getByRole('menuitem', { name: 'Snooze passing bot PRs' }))
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: 'Run “Snooze passing bot PRs” anyway?' })).getByRole('button', {
        name: 'Run anyway',
      }),
    )

    expect(await screen.findByText('Couldn’t run “Snooze passing bot PRs”')).toBeInTheDocument()
    expect(screen.getByText('· It doesn’t match it: it isn’t a bot’s.')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('offers no routine to run on an item no routine that is on acts on', async () => {
    await renderWithRoutines(item({ kind: 'issue' }), [menuRoutines()[1] ?? routine()])

    await openMenu()
    await openSubmenu('Autopilot')

    expect(screen.getByRole('menuitem', { name: 'Run a routine' })).toBeDisabled()
  })

  it('says why a routine didn’t run, condition by condition, with what its history says', async () => {
    await renderWithRoutines()

    await openMenu()
    await openSubmenu('Autopilot')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Why didn’t a routine run?' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Why didn’t a routine run?' }))
    const pick = dialog.getByRole('combobox', { name: 'Routine' })

    expect(
      within(pick)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['Snooze passing bot PRs', 'Investigate slow reviews (off)'])
    expect(await dialog.findByText('It doesn’t match now.')).toBeInTheDocument()
    expect(platform.explainRoutine).toHaveBeenCalledWith(3, 1)

    const trace = dialog.getByRole('list', { name: 'Why, for example-repo #7' })

    expect(
      within(trace)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Doesn’t match:checks pass· test (go 1.26) failed', 'Matches:it is a bot’s· dependabot'])
    expect(dialog.getByRole('list', { name: 'What its history says' })).toHaveTextContent(
      'It hasn’t acted on this item yet.',
    )

    platform.explainRoutine.mockResolvedValueOnce(explanation({ routineId: 4, match: 'yes', trace: [], ledger: [] }))
    await userEvent.selectOptions(pick, '4')

    expect(platform.explainRoutine).toHaveBeenLastCalledWith(4, 1)
    expect(await dialog.findByText('It matches now.')).toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Done' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says why a routine didn’t run: what holds it back, why the item isn’t watched, and its whole ledger', async () => {
    platform.explainRoutine.mockResolvedValue(
      explanation({
        inWatch: false,
        routine: ['It’s switched off, so it runs on nothing.'],
        watch: ['Not in its search: its author is octo-other, not Dependabot.'],
        records: [
          autopilotMatch({ id: 31, words: 'It already ran on this commit.' }),
          autopilotMatch({ id: 30, dryRun: true, words: 'Would have run.' }),
        ],
      }),
    )
    await renderWithRoutines()

    await openMenu()
    await openSubmenu('Autopilot')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Why didn’t a routine run?' }))

    const dialog = within(await screen.findByRole('dialog', { name: 'Why didn’t a routine run?' }))

    expect(await dialog.findByRole('list', { name: 'What holds the routine back' })).toHaveTextContent(
      'It’s switched off, so it runs on nothing.',
    )
    expect(dialog.getByRole('list', { name: 'Why it isn’t in what it watches' })).toHaveTextContent(
      'its author is octo-other, not Dependabot.',
    )
    expect(dialog.queryByText('The item isn’t in what it watches.')).not.toBeInTheDocument()

    await userEvent.click(dialog.getByText('Its ledger for this item: 2 records'))

    const ledger = dialog.getByRole('list', { name: 'Its ledger for this item' })

    expect(
      within(ledger)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual([
      expect.stringContaining('It already ran on this commit.'),
      expect.stringContaining('Dry run · Would have run.'),
    ])
  })

  it('makes a routine from the item through the window’s own call', async () => {
    const made = vi.fn<(it: InboxItem) => void>()

    render(
      <MakeRoutines onMake={made}>
        <ul>
          <ItemRow item={item()} launch={null} triage={triageActions()} edits={editActions()} />
        </ul>
      </MakeRoutines>,
      { wrapper: at() },
    )

    await openMenu()
    await openSubmenu('Autopilot')
    await userEvent.click(screen.getByRole('menuitem', { name: 'Make a routine from this…' }))

    expect(made).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({ id: item().id }))
  })
})

// MakeRoutines provides the item menus' Autopilot with no routines, and onMake as the
// window's Make a routine from this….
function MakeRoutines({ onMake, children }: { onMake: (it: InboxItem) => void; children: ReactNode }) {
  const { menu, node } = useRoutineMenu([], defaultToastSeconds, onMake)

  return (
    <AutopilotMenuContext value={menu}>
      {children}
      {node}
    </AutopilotMenuContext>
  )
}
