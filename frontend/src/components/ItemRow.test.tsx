import { act, render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement, ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { HiddenLabelsContext, labelMatcher } from '../lib/labels'
import type { TriageTab } from '../lib/triage'
import type { InboxItem, RepoRules, Worktree } from '../platform'
import { at } from '../test/clock'
import {
  agentSession,
  agents,
  autopilotMatch,
  details,
  fileCheck,
  item,
  itemResolution,
  profile,
  project,
  prompts,
  worktree,
} from '../test/fixtures'
import { platform } from '../test/platform'
import { AutopilotRowsContext, type AutopilotRows } from './autopilot/AutopilotChip'
import type { EditActions } from './ItemMenu'
import { CardChips, ItemRow, TrustTag, WorktreeTag, type ItemRowProps, type LaunchOptions } from './ItemRow'
import type { TriageActions } from './TriageControls'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const launch: LaunchOptions = {
  agents: agents(),
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
  onSettingsSaved: () => undefined,
}

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

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

interface Setup extends Partial<ItemRowProps> {
  // rows are the rows' Autopilot, if Autopilot is on.
  rows?: AutopilotRows
}

// mountCard renders an item's card in a list, with its launch settings resolved for item
// 1, and returns the row.
function mountCard(it: InboxItem, { rows, ...props }: Setup = {}): HTMLElement {
  const card = (
    <ul>
      <ItemRow item={it} launch={launch} resolution={it.id === 1 ? itemResolution() : null} {...props} />
    </ul>
  )
  const wrapped: ReactNode =
    rows === undefined ? card : <AutopilotRowsContext value={rows}>{card}</AutopilotRowsContext>

  render(<>{wrapped}</>)

  return screen.getByRole('listitem')
}

// asked is a pull request that asks for the user's review, its details read.
function asked(overrides: Partial<InboxItem> = {}): InboxItem {
  return item({ reviewRequest: 'you', details: details(), ...overrides })
}

// toggles are the card's two Show details buttons: the stretched toggle over the whole
// card, and the ⌄ among its triage.
function toggles(row: HTMLElement): { stretched: HTMLElement; chevron: HTMLElement } {
  const [stretched, chevron] = within(row).getAllByRole('button', { name: 'Show details' })

  if (stretched === undefined || chevron === undefined) {
    throw new Error('the card has no Show details buttons')
  }

  return { stretched, chevron }
}

// body is the card's body: its two lines.
function body(row: HTMLElement): HTMLElement {
  const title = within(row).getAllByRole('button')[0]
  const line = title?.parentElement?.parentElement

  if (line === null || line === undefined) {
    throw new Error('the card has no body')
  }

  return line
}

describe('ItemRow', () => {
  it('names its item and its rule for the inbox to find, and marks it when highlighted', () => {
    const { rerender } = render(
      <ul>
        <ItemRow item={item({ id: 42 })} launch={launch} />
      </ul>,
    )

    const row = screen.getByRole('listitem')
    expect(row).toHaveAttribute('data-item-id', '42')
    expect(row).toHaveAttribute('data-rule', 'loading')
    expect(row).not.toHaveAttribute('data-highlighted')

    rerender(
      <ul>
        <ItemRow item={item({ id: 42 })} launch={launch} highlighted />
      </ul>,
    )
    expect(row).toHaveAttribute('data-highlighted', 'true')
  })

  it('shows a review request on two lines: the title, then its story and byline', () => {
    const row = mountCard(asked())

    expect(row).toHaveAttribute('data-rule', 'review')
    expect(row.querySelector('[data-edge]')).toHaveClass('bg-accent')
    expect(body(row).children).toHaveLength(2)

    const [first, second] = body(row).children
    expect(within(first as HTMLElement).getByRole('button', { name: 'Add the widget cache' })).toHaveClass(
      'font-semibold',
    )
    expect(second).toHaveTextContent(/^Your review·acme-org\/example-repo #7 · octo-user · 3h$/)
    expect(screen.getByText('3h')).toHaveAttribute('dateTime', '2026-06-15T09:00:00Z')
    // Without a filter to add to, the repository, author, and labels are plain text.
    expect(screen.queryByRole('button', { name: /^Filter by/ })).not.toBeInTheDocument()

    // The author's initials, with the kind at the corner.
    expect(screen.getByText('OU')).toHaveAttribute('data-avatar', 'initials')
    expect(screen.getByRole('img', { name: 'Pull request' })).toHaveClass('text-success')

    // The state column, then the human's move beside the agent's.
    expect(screen.getByText(/^Checks: 12 passed, 3 skipped\. Reviews:/)).toHaveClass('sr-only')
    expect(screen.getByText('Size M: 120 lines added and 30 removed across 6 files in 3 commits')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Review' })).toHaveAttribute('data-verb', 'review')
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()
  })

  it('makes the title the row’s first button, which opens GitHub without a sheet', async () => {
    const row = mountCard(asked())

    const first = within(row).getAllByRole('button')[0]
    expect(first).toHaveAccessibleName('Add the widget cache')
    expect(first).toHaveAttribute('title', 'Add the widget cache')

    await userEvent.click(screen.getByRole('button', { name: 'Add the widget cache' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
  })

  it('prefixes a draft’s title and mutes its kind', () => {
    const row = mountCard(item({ isDraft: true, authored: true, details: details() }))

    expect(within(body(row).firstElementChild as HTMLElement).getByText('Draft')).toHaveClass('text-muted')
    expect(screen.getByRole('img', { name: 'Draft pull request' })).toHaveClass('text-muted')
    expect(screen.getByRole('listitem')).toHaveAttribute('data-rule', 'my_draft')
  })

  it('shows an issue’s state in the state column, with Read as its move', () => {
    mountCard(
      item({
        kind: 'issue',
        assigned: true,
        details: details({ projects: [project()], subIssues: { total: 8, completed: 6 } }),
      }),
    )

    expect(screen.getByRole('listitem')).toHaveAttribute('data-rule', 'issue')
    expect(screen.getByRole('img', { name: 'Issue' })).toBeInTheDocument()
    expect(screen.getByText('Sub-issues: 6 of 8 sub-issues done (75%)')).toBeInTheDocument()
    expect(screen.queryByText(/^Checks:/)).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Read' })).toBeInTheDocument()
  })

  it('names the agent’s session or the resolved launch in its story', () => {
    mountCard(asked(), { sessions: [agentSession({ agent: 'codex' })] })

    expect(screen.getByRole('listitem')).toHaveAttribute('data-rule', 'session_needs_you')
    expect(screen.getByText('Codex needs approval')).toBeInTheDocument()
    expect(screen.getByRole('listitem').querySelector('[data-edge]')).toHaveClass('bg-warning')
  })
})

describe('ItemRow labels', () => {
  it('shows two labels after the title, colored, and folds the rest into a count', () => {
    const labels = ['one', 'two', 'three', 'four'].map((name) => ({ name, color: '#d73a4a' }))
    mountCard(asked({ details: details({ labels: [...labels, { name: 'plain', color: '' }] }) }))

    expect(screen.getByText('one')).toBeInTheDocument()
    expect(screen.getByText('two').previousElementSibling).toHaveStyle({ backgroundColor: '#d73a4a' })
    expect(screen.queryByText('three')).not.toBeInTheDocument()
    expect(screen.getByTitle('three, four, plain')).toHaveTextContent('+3')
    expect(screen.getByText('more: three, four, plain')).toHaveClass('sr-only')
    // A narrow list leaves the labels out before the title gives way.
    expect(screen.getByText('Labels:').parentElement).toHaveClass('@max-4xl:hidden', 'shrink-[10]')
  })

  it('leaves out the labels the user hid before taking the first two', () => {
    const labels = ['size/XL', 'one', 'wontfix', 'two', 'three'].map((name) => ({ name, color: '#d73a4a' }))
    render(
      <HiddenLabelsContext value={labelMatcher(['size/*', 'wontfix'])}>
        <ul>
          <ItemRow item={asked({ details: details({ labels }) })} launch={null} />
        </ul>
      </HiddenLabelsContext>,
    )

    expect(screen.queryByText('size/XL')).not.toBeInTheDocument()
    expect(screen.getByText('two'), 'a hidden label takes no place').toBeInTheDocument()
    expect(screen.getByTitle('three')).toHaveTextContent('+1')
  })

  it('shows no labels when every one is hidden, or there are none', () => {
    render(
      <HiddenLabelsContext value={labelMatcher(['size/*'])}>
        <ul>
          <ItemRow item={asked({ details: details({ labels: [{ name: 'size/M', color: '' }] }) })} launch={null} />
          <ItemRow item={asked({ id: 2, details: details({ labels: [] }) })} launch={null} />
        </ul>
      </HiddenLabelsContext>,
    )

    expect(screen.queryByText('Labels:')).not.toBeInTheDocument()
  })

  it('filters by a label, the repository, and the author when clicked', async () => {
    const onFilterBy = vi.fn()
    mountCard(asked(), { onFilterBy })

    await userEvent.click(screen.getByRole('button', { name: 'Filter by label enhancement' }))
    await userEvent.click(screen.getByRole('button', { name: 'Filter by repository acme-org/example-repo' }))
    await userEvent.click(screen.getByRole('button', { name: 'Filter by author octo-user' }))

    expect(onFilterBy.mock.calls).toEqual([
      ['label', 'enhancement'],
      ['repo', 'acme-org/example-repo'],
      ['author', 'octo-user'],
    ])
  })
})

describe('ItemRow byline', () => {
  it('says “you” for the user’s own item, and why it is theirs', () => {
    mountCard(item({ authored: true, assigned: true, details: details() }))

    const you = screen.getByTitle('octo-user (you)')
    expect(you).toHaveTextContent('you')
    expect(body(screen.getByRole('listitem')).lastElementChild).toHaveTextContent(
      /acme-org\/example-repo #7 · you · assigned · 3h/,
    )
  })

  it('names the teams a review is asked of', () => {
    mountCard(asked({ reviewRequest: 'team', reviewTeams: ['platform', 'security'] }))

    expect(body(screen.getByRole('listitem')).lastElementChild).toHaveTextContent(
      /octo-user · via @platform, @security · 3h/,
    )
  })

  it('says a team before GitHub names it', () => {
    mountCard(asked({ reviewRequest: 'team', reviewTeams: [] }))

    expect(body(screen.getByRole('listitem')).lastElementChild).toHaveTextContent(/octo-user · via a team · 3h/)
  })

  it('dims the org, which a narrow list leaves out', () => {
    mountCard(asked())

    expect(screen.getByText('acme-org/')).toHaveClass('opacity-60', '@max-xl:hidden')
  })
})

describe('ItemRow chips', () => {
  it('shows one risk chip for an outside contributor’s sensitive files, which opens them', async () => {
    const onFilesSettings = vi.fn()
    mountCard(asked({ trusted: false, trust: 'outside', files: fileCheck() }), { onFilesSettings })

    const chip = screen.getByRole('button', { name: 'Outside · Agent config · Workflows' })
    await userEvent.click(chip)
    await userEvent.click(screen.getByRole('button', { name: 'What counts as sensitive' }))

    expect(onFilesSettings).toHaveBeenCalledOnce()
  })

  it('gives a trusted bot its square and no chip', () => {
    mountCard(asked({ author: 'dependabot', trust: 'bot', trusted: true, authorIsBot: true }))

    expect(screen.getByRole('listitem').querySelector('[data-avatar="bot"]')).toHaveClass('rounded-lg')
    expect(screen.queryByText(/Outside|Untrusted/)).not.toBeInTheDocument()
  })

  it('marks a worktree with changes or in use, and nothing else', () => {
    const shown = (wt: Worktree) => {
      const { unmount } = render(
        <ul>
          <ItemRow item={asked()} launch={null} worktree={wt} />
        </ul>,
      )
      const chip = screen.queryByTitle('~/projects/example-repo/.claude/worktrees/pr-7')
      const words = chip?.textContent ?? null
      unmount()

      return words
    }

    expect(shown(worktree({ dirty: true, dirtyCount: 2 }))).toBe('2 changes')
    expect(shown(worktree({ inUse: [{ pid: 4242, command: 'claude', agent: '', by: '' }] }))).toBe('in use')
    expect(shown(worktree({ agent: 'codex', lastLaunchedAt: '2026-06-15T09:00:00Z' }))).toBeNull()
  })

  it('shows a routine’s chip, except when the story already is the routine’s', () => {
    const rows: AutopilotRows = { names: new Map(), chips: new Map([[1, autopilotMatch()]]) }
    const { unmount } = render(
      <AutopilotRowsContext value={rows}>
        <ul>
          <ItemRow item={asked()} launch={null} />
        </ul>
      </AutopilotRowsContext>,
    )

    expect(screen.getByRole('button', { name: /^Autopilot: Snoozed/ })).toBeInTheDocument()
    unmount()

    render(
      <AutopilotRowsContext value={rows}>
        <ul>
          <ItemRow item={asked()} launch={null} autopilot="needs:Heal bot PRs" />
          <ItemRow item={asked({ id: 2 })} launch={null} autopilot="working:Fix checks" />
        </ul>
      </AutopilotRowsContext>,
    )

    expect(screen.getByText('Heal bot PRs wants your OK')).toBeInTheDocument()
    expect(screen.getByText('Fix checks is working')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Autopilot:/ })).not.toBeInTheDocument()
  })

  it('folds the chips past the second into a count that names them', () => {
    render(
      <CardChips
        chips={[
          { key: 'a', words: 'First', node: <span>First</span> },
          { key: 'b', words: 'Second', node: <span>Second</span> },
          { key: 'c', words: 'Third', node: <span>Third</span> },
        ]}
      />,
    )

    expect(screen.getByText('Second')).toBeInTheDocument()
    expect(screen.queryByText('Third')).not.toBeInTheDocument()
    expect(screen.getByTitle('Third')).toHaveTextContent('+1')
  })
})

describe('ItemRow pin', () => {
  it('marks a pinned card before its title', () => {
    mountCard(asked({ pinnedAt: '2026-06-15T11:00:00Z' }), { triage: triageActions() })

    expect(screen.getByRole('img', { name: 'Pinned to the top of the list' })).toBeInTheDocument()
  })

  it('names the rule that pins it, which the card doesn’t unpin', () => {
    mountCard(asked({ repoRule: 'pinned' }), { triage: triageActions({ pinned: ['acme-org'], muted: [] }) })

    const mark = screen.getByRole('img', { name: 'Pinned with acme-org' })
    expect(mark).toHaveAttribute('title', expect.stringContaining('Settings → Repositories'))
    expect(screen.queryByRole('button', { name: 'Unpin' })).not.toBeInTheDocument()
  })

  it('names its repository while the rules are unknown, and the routine that pinned it', () => {
    const rows: AutopilotRows = { names: new Map([[4, 'Pin outside PRs']]), chips: new Map() }
    render(
      <AutopilotRowsContext value={rows}>
        <ul>
          <ItemRow item={asked({ repoRule: 'pinned' })} launch={null} />
          <ItemRow item={asked({ id: 2, pinnedAt: '2026-06-15T11:00:00Z', pinnedBy: 4 })} launch={null} />
        </ul>
      </AutopilotRowsContext>,
    )

    expect(screen.getByRole('img', { name: 'Pinned with acme-org/example-repo' })).toBeInTheDocument()
    expect(screen.getByRole('img', { name: 'Pinned by the routine Pin outside PRs' })).toBeInTheDocument()
  })
})

describe('ItemRow tabs', () => {
  // inTab renders an item's card in a tab, with the row's triage.
  function inTab(it: InboxItem, tab: TriageTab) {
    const triage = triageActions({ pinned: [], muted: ['acme-org'] })
    mountCard(it, { tab, triage })

    return triage
  }

  it('says when a snoozed item comes back, and brings it back', async () => {
    const it = item({ snoozedUntil: '2026-06-18T09:00:00Z', details: details() })
    const triage = inTab(it, 'snoozed')

    expect(screen.getByTitle(/^Snoozed until/)).toHaveTextContent(/^Back /)
    const unsnooze = screen.getByRole('button', { name: 'Unsnooze' })
    expect(unsnooze).toHaveClass('border-border', 'text-text')

    await userEvent.click(unsnooze)
    expect(triage.onRestore).toHaveBeenCalledWith(it)
  })

  it('says when a dismissed item was dismissed, and restores it', async () => {
    const it = item({ dismissedAt: '2026-06-15T10:00:00Z' })
    const triage = inTab(it, 'dismissed')

    expect(screen.getByTitle(/^Dismissed /)).toHaveTextContent('Dismissed 2h ago')

    await userEvent.click(screen.getByRole('button', { name: 'Restore' }))
    expect(triage.onRestore).toHaveBeenCalledWith(it)
  })

  it('says what mutes a muted item, and unmutes it', async () => {
    const triage = inTab(item({ repoRule: 'muted', details: details() }), 'muted')

    expect(screen.getByText('Muted with acme-org')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Unmute acme-org' }))
    expect(triage.onRepoRule).toHaveBeenCalledWith('acme-org', '')
  })

  it('names the repository that mutes it while the row has no triage', () => {
    mountCard(item({ repoRule: 'muted' }), { tab: 'muted' })

    expect(screen.getByText('Muted with acme-org/example-repo')).toBeInTheDocument()
  })

  it('says an item is back from a snooze for a day, then no longer', () => {
    mountCard(item({ id: 2, snoozedUntil: '2026-06-15T11:00:00Z', details: details() }))
    expect(screen.getByText(/· back from snooze$/)).toBeInTheDocument()
    expect(screen.getByRole('listitem').querySelector('[data-icon="clock"]')).not.toBeNull()
  })

  it('stops saying so a day after it came back', () => {
    mountCard(item({ id: 2, snoozedUntil: '2026-06-13T12:00:00Z', details: details() }))
    expect(screen.queryByText(/back from snooze/)).not.toBeInTheDocument()
  })
})

describe('ItemRow agent', () => {
  it('has no agent slot until the launch options load, but its own move', () => {
    render(
      <ul>
        <ItemRow item={asked()} launch={null} />
      </ul>,
    )

    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Review' })).toBeInTheDocument()
  })

  it('names the item’s resolved prompt and profile on its launch options', () => {
    mountCard(asked())

    expect(screen.getByRole('button', { name: 'Launch options: Review · default' })).toBeInTheDocument()
  })

  it('waits for an item’s launch settings to resolve', () => {
    mountCard(asked({ id: 99 }))

    // Until the settings resolve, the agent the button starts is not known.
    expect(screen.getByRole('button', { name: 'Open' })).toBeDisabled()
  })

  it('keeps the human’s move, quiet, beside a live session’s pill', () => {
    const working = agentSession({ state: 'working', detail: '', stateAt: '2026-06-15T11:59:00Z' })
    const row = mountCard(asked(), { sessions: [working] })

    expect(row).toHaveAttribute('data-rule', 'agent_working')
    expect(row.querySelector('[data-edge]')).toHaveClass('bg-info')
    expect(screen.getByText('Claude is on it')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Review' })).toHaveClass('border-border', 'text-text')

    const pill = screen.getByRole('button', { name: /^Claude session: working/ })
    expect(pill).toHaveAttribute('data-moving', 'true')
    expect(pill).toHaveTextContent(/^Claude working/)
    expect(screen.getByRole('button', { name: 'Show in terminal' })).toBeEnabled()
  })

  it('breathes its Show terminal while an agent waits on the user', async () => {
    mountCard(asked(), { sessions: [agentSession()] })

    const terminal = screen.getByRole('button', { name: 'Show terminal' })
    expect(terminal).toHaveAttribute('data-moving')
    expect(terminal).toHaveClass('motion-safe:animate-breathe')

    await userEvent.click(terminal)
    expect(platform.focusSession).toHaveBeenCalledOnce()
  })

  it('offers what happened after a session failed today, and the item’s move after a day', () => {
    const failed = (at: string) => agentSession({ state: 'failed', stateAt: at, detail: '' })
    render(
      <ul>
        <ItemRow item={asked()} launch={launch} sessions={[failed('2026-06-15T11:00:00Z')]} />
        <ItemRow item={asked({ id: 2 })} launch={launch} sessions={[failed('2026-06-13T11:00:00Z')]} />
      </ul>,
    )

    const [today, older] = screen.getAllByRole('listitem')
    expect(today).toHaveAttribute('data-rule', 'session_failed')
    expect(within(today as HTMLElement).getByRole('button', { name: 'What happened' })).toBeInTheDocument()
    expect(older).toHaveAttribute('data-rule', 'review')
  })

  it('opens what an ended session found in the item’s sheet', async () => {
    const onOpen = vi.fn()
    const it = asked()
    mountCard(it, {
      sessions: [agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:00Z', canFocus: false })],
      onOpen,
      tab: 'inbox',
    })

    await userEvent.click(screen.getByRole('button', { name: 'Findings · 1m' }))

    expect(onOpen).toHaveBeenCalledExactlyOnceWith(it, 'inbox')
  })
})

describe('ItemRow toggle', () => {
  it('keeps every control and every titled element above the stretched toggle', () => {
    const working = agentSession({ state: 'working', detail: '' })
    const row = mountCard(
      asked({
        trusted: false,
        trust: 'outside',
        files: fileCheck(),
        pinnedAt: '2026-06-15T11:00:00Z',
        isDraft: false,
      }),
      {
        triage: triageActions(),
        edits: editActions(),
        onOpen: vi.fn(),
        onFilterBy: vi.fn(),
        sessions: [working],
        worktree: worktree({ dirty: true, dirtyCount: 1 }),
      },
    )
    const { stretched } = toggles(row)

    const controls = [...row.querySelectorAll('button, [title]')].filter((el) => el !== stretched)
    expect(controls.length).toBeGreaterThan(15)
    const below = controls.filter((el) => el.closest('.z-10, .z-20, .z-30') === null)
    expect(below.map((el) => el.getAttribute('aria-label') ?? el.getAttribute('title') ?? el.textContent)).toEqual([])
  })
})

describe('ItemRow triage', () => {
  it('hides the triage until the pointer or focus is on the card, over the state column', () => {
    const row = mountCard(asked(), { triage: triageActions(), edits: editActions() })

    const triage = row.querySelector('[data-triage]')
    expect(triage).toHaveClass('opacity-0', 'group-hover:opacity-100', 'group-focus-within:opacity-100')
    expect(triage).toHaveClass('col-[4/5]', 'row-[1/2]', '@max-xl:col-[3/4]')
    expect(within(triage as HTMLElement).getByRole('button', { name: 'More actions' })).toBeInTheDocument()
    expect(row.querySelector('[data-state-column]')).toHaveClass('group-hover:opacity-0', '@max-xl:hidden')
    expect(row.querySelector('[data-state-column]')).not.toHaveClass('opacity-0')
  })

  it('keeps the triage shown, and the state column faded, while the card works with GitHub', async () => {
    let done: () => void = () => undefined
    const triage = triageActions()
    triage.onRefresh = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          done = resolve
        }),
    )
    const row = mountCard(asked(), { triage })

    await userEvent.click(screen.getByRole('button', { name: 'Refresh from GitHub' }))

    expect(row.querySelector('[data-triage]')).toHaveClass('opacity-100')
    expect(row.querySelector('[data-state-column]')).toHaveClass('opacity-0')

    await act(async () => {
      done()
      await Promise.resolve()
    })

    expect(row.querySelector('[data-triage]')).toHaveClass('opacity-0')
  })

  it('fades the card under a note while an edit will likely take it out of the list', async () => {
    let done: () => void = () => undefined
    const edits = editActions()
    edits.onUnassign = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          done = resolve
        }),
    )
    const row = mountCard(item({ assigned: true, details: details() }), { triage: triageActions(), edits })

    await userEvent.click(screen.getByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Unassign me' }))

    expect(row).toHaveAttribute('aria-busy', 'true')
    const note = within(row).getByRole('status')
    expect(note).toHaveTextContent('Unassigning you on GitHub…')
    expect(note.closest('[data-row-status]')).toHaveClass('z-30')

    await act(async () => {
      done()
      await Promise.resolve()
    })

    expect(row).not.toHaveAttribute('aria-busy')
  })
})

describe('ItemRow open', () => {
  it('opens a pull request in the review sheet from its title, and GitHub otherwise', async () => {
    const onOpen = vi.fn()
    const user = userEvent.setup()
    const pull = item()
    render(
      <ul>
        <ItemRow item={pull} launch={null} tab="dismissed" onOpen={onOpen} />
      </ul>,
    )

    const title = screen.getByRole('button', { name: 'Add the widget cache' })
    expect(title).toHaveAttribute('title', 'Add the widget cache\nReview it here; ⌘-click opens GitHub')

    await user.click(title)
    expect(onOpen).toHaveBeenCalledExactlyOnceWith(pull, 'dismissed')
    expect(platform.openExternal).not.toHaveBeenCalled()

    await user.keyboard('{Meta>}')
    await user.click(screen.getByRole('button', { name: 'Add the widget cache' }))
    await user.keyboard('{/Meta}')
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')

    await user.click(screen.getByRole('button', { name: 'Open on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledTimes(2)
  })

  it('opens an issue in the issue sheet from its title', async () => {
    const onOpen = vi.fn()
    const crash = item({
      kind: 'issue',
      title: 'Crash on start',
      url: 'https://github.com/acme-org/example-repo/issues/8',
    })
    render(
      <ul>
        <ItemRow item={crash} launch={null} onOpen={onOpen} />
      </ul>,
    )

    const title = screen.getByRole('button', { name: 'Crash on start' })
    expect(title).toHaveAttribute('title', 'Crash on start\nRead it here; ⌘-click opens GitHub')

    await userEvent.click(title)
    expect(onOpen).toHaveBeenCalledExactlyOnceWith(crash, 'inbox')

    await userEvent.click(screen.getByRole('button', { name: 'Open on GitHub' }))
    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/issues/8')
  })

  it('opens a card on GitHub when no sheet does', async () => {
    render(
      <ul>
        <ItemRow item={item({ kind: 'issue', title: 'Crash on start' })} launch={null} />
      </ul>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Crash on start' }))
    expect(platform.openExternal).toHaveBeenCalledOnce()
    expect(screen.queryByRole('button', { name: 'Open on GitHub' })).not.toBeInTheDocument()
  })
})

describe('TrustTag', () => {
  it('marks a trusted bot quietly, an untrusted bot or outside contributor as a warning, and no one else', () => {
    const tag = (it: InboxItem) => {
      const { container, unmount } = render(<TrustTag item={it} />)
      const shown = container.firstElementChild
      const words = shown === null ? null : [shown.textContent, shown.className.includes('text-warning')]
      unmount()

      return words
    }

    expect(tag(item({ trust: 'bot', trusted: true, authorIsBot: true }))).toEqual(['bot', false])
    expect(tag(item({ trust: 'outside', trusted: false, authorIsBot: true }))).toEqual(['untrusted bot', true])
    expect(tag(item({ trust: 'outside', trusted: false }))).toEqual(['outside contributor', true])
    expect(tag(item())).toBeNull()
  })
})

describe('WorktreeTag', () => {
  // tag renders a worktree's tag and returns its words.
  function tag(overrides: Partial<Worktree>): string | null {
    const { container, unmount } = render(<WorktreeTag wt={worktree(overrides)} agents={agents()} />)
    const words = container.textContent
    unmount()

    return words
  }

  it('says its changes, then the last session launched in it, or that one is running in it', () => {
    expect(tag({ dirtyCount: 2, agent: 'codex', lastLaunchedAt: '2026-06-15T09:00:00Z' })).toBe(
      'worktree · 2 changes · Codex 3h ago',
    )
    expect(tag({ inUse: [{ pid: 1, command: 'claude', agent: '', by: '' }], agent: 'claude' })).toBe(
      'worktree · in use',
    )
    expect(tag({})).toBe('worktree')
  })
})
