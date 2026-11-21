import { act, render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { CardPrefsContext, defaultCardPrefs } from '../../lib/cardPrefs'
import { HiddenLabelsContext, labelMatcher } from '../../lib/labels'
import type { LaunchOptions } from '../../lib/launch'
import type { TriageTab } from '../../lib/triage'
import type { InboxItem } from '../../platform'
import { deferred } from '../../test/async'
import { at } from '../../test/clock'
import {
  agentSession,
  agents,
  details,
  fileCheck,
  item,
  itemResolution,
  launchHold,
  launchResult,
  profile,
  project,
  prompts,
} from '../../test/fixtures'
import { platform } from '../../test/platform'
import { ItemRow, type ItemRowProps } from '../ItemRow'
import type { TriageActions } from '../TriageControls'
import { compactOutcomeClass } from './CardActions'

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

// compact renders ui with the cards compact, on a clock that stands at noon.
function compact(ui: ReactElement, hidden: readonly string[] = []) {
  const Clock = at()

  return renderPlain(
    <Clock>
      <HiddenLabelsContext value={labelMatcher(hidden)}>
        <CardPrefsContext value={{ ...defaultCardPrefs, density: 'compact' }}>{ui}</CardPrefsContext>
      </HiddenLabelsContext>
    </Clock>,
  )
}

// mountCompact renders an item's compact card in a list, its launch settings resolved, and
// returns the row.
function mountCompact(it: InboxItem, props: Partial<ItemRowProps> = {}): HTMLElement {
  compact(
    <ul>
      <ItemRow item={it} launch={launch} resolution={itemResolution()} {...props} />
    </ul>,
  )

  return screen.getByRole('listitem')
}

// asked is a pull request that asks for the user's review, its details read.
function asked(overrides: Partial<InboxItem> = {}): InboxItem {
  return item({ reviewRequest: 'you', details: details(), ...overrides })
}

// shown is what an element shows, leaving out what only a screen reader reads.
function shown(el: Element): string {
  const copy = el.cloneNode(true) as Element
  copy.querySelectorAll('.sr-only').forEach((sr) => {
    sr.remove()
  })

  return copy.textContent
}

// Columns are what a compact card shows in each of its columns, left to right.
interface Columns {
  edge: string
  kind: string
  repo: string
  title: string
  story: string
  risk: string
  track: string
  size: string
  age: string
  go: string
}

// columns reads a compact card's columns: its grid's cells, apart from the hover row.
function columns(row: HTMLElement): Columns {
  const cells = [...row.children].filter((el) => !el.hasAttribute('data-hover-row'))
  const [edge, kind, repo, title, story, risk, track, size, age, go] = cells

  if (go === undefined) {
    throw new Error(`a compact card has ten columns, not ${String(cells.length)}`)
  }

  const label = (el: Element | undefined) => el?.querySelector('[role="img"]')?.getAttribute('aria-label') ?? ''

  return {
    edge: edge?.getAttribute('data-edge') ?? '',
    kind: kind?.getAttribute('aria-label') ?? '',
    repo: shown(repo as Element),
    title: shown(title as Element),
    story: shown(story as Element),
    risk: label(risk),
    track: label(track),
    size: shown(size as Element),
    age: shown(age as Element),
    go: shown(go),
  }
}

// greenTrack is the track's sentence for the fixtures' details: green checks, an approval
// out of two reviewers, no conflicts.
const greenTrack = 'Checks: 12 passed, 3 skipped. Reviews: 1 of 2 reviewers reviewed, 1 approved. No conflicts.'

describe('CompactRow', () => {
  it.each<[string, InboxItem, Partial<ItemRowProps>, string, Columns]>([
    [
      'a review request',
      asked(),
      {},
      'review',
      {
        edge: 'yours',
        kind: 'Pull request',
        repo: 'acme-org/example-repo',
        title: 'Add the widget cache#7',
        story: 'Your review',
        risk: '',
        track: greenTrack,
        size: 'M',
        age: '3h',
        go: 'Review',
      },
    ],
    [
      'the user’s own pull request whose checks fail',
      item({ authored: true, details: details({ checks: { ...details().checks, passed: 10, failed: 2 } }) }),
      {},
      'checks_failing',
      {
        edge: 'blocked',
        kind: 'Pull request',
        repo: 'acme-org/example-repo',
        title: 'Add the widget cache#7',
        story: '2 of 12 checks failing',
        risk: '',
        track: 'Checks: 10 passed, 2 failing, 3 skipped. Reviews: 1 of 2 reviewers reviewed, 1 approved. No conflicts.',
        size: 'M',
        age: '3h',
        go: 'Open',
      },
    ],
    [
      'an agent at work',
      asked(),
      { sessions: [agentSession({ state: 'working', detail: '', stateAt: '2026-06-15T11:59:00Z' })] },
      'agent_working',
      {
        edge: 'agent',
        kind: 'Pull request',
        repo: 'acme-org/example-repo',
        title: 'Add the widget cache#7',
        story: 'Claude is on it · new worktree',
        risk: '',
        track: greenTrack,
        size: 'M',
        age: '3h',
        go: 'Review',
      },
    ],
    [
      'an agent waiting on the user, from an outside contributor',
      asked({ trusted: false, trust: 'outside', files: fileCheck() }),
      { sessions: [agentSession()] },
      'session_needs_you',
      {
        edge: 'urgent',
        kind: 'Pull request',
        repo: 'acme-org/example-repo',
        title: 'Add the widget cache#7',
        story: 'Claude needs approval · Permission prompt',
        risk: 'Outside · Agent config · Workflows',
        track: greenTrack,
        size: 'M',
        age: '3h',
        go: 'Show terminal',
      },
    ],
    [
      'an issue assigned to the user',
      item({
        kind: 'issue',
        assigned: true,
        details: details({
          labels: [],
          projects: [project({ fields: [{ name: 'Priority', value: 'P1', color: '' }] })],
          subIssues: { total: 5, completed: 3 },
        }),
      }),
      {},
      'issue',
      {
        edge: 'yours',
        kind: 'Issue',
        repo: 'acme-org/example-repo',
        title: 'Add the widget cache#7',
        story: 'In progress',
        risk: '',
        track: 'Roadmap: In progress · 3 of 5 sub-issues done (60%)',
        size: 'P1',
        age: '3h',
        go: 'Read',
      },
    ],
    [
      'a card whose details are still loading',
      item(),
      {},
      'loading',
      {
        edge: 'waiting',
        kind: 'Pull request',
        repo: 'acme-org/example-repo',
        title: 'Add the widget cache#7',
        story: 'Reading details…',
        risk: '',
        track: 'Checks, review, and merge: not read yet',
        size: '–',
        age: '3h',
        go: 'Open',
      },
    ],
  ])('shows each column for %s', (_, it, props, rule, want) => {
    const row = mountCompact(it, props)

    expect(row).toHaveAttribute('data-density', 'compact')
    expect(row).toHaveAttribute('data-rule', rule)
    expect(columns(row)).toEqual(want)
  })

  it('tints the story chip with its edge’s color, and leaves a card waiting on others untinted', () => {
    const story = (it: InboxItem, props: Partial<ItemRowProps> = {}) => {
      const { unmount } = compact(
        <ul>
          <ItemRow item={it} launch={null} {...props} />
        </ul>,
      )
      const chip = screen.getByRole('listitem').querySelector('[data-story-chip]')
      const classes = chip?.className ?? ''
      unmount()

      return classes
    }

    expect(story(asked())).toMatch(/\btext-accent\b.*\bbg-accent\/11\b/)
    expect(story(asked(), { sessions: [agentSession()] })).toMatch(/\btext-warning\b.*\bbg-warning\/13\b/)
    expect(story(item({ authored: true, details: details({ mergeable: 'CONFLICTING' }) }))).toMatch(
      /\btext-danger\b.*\bbg-danger\/11\b/,
    )
    expect(story(item({ authored: true, details: details() }))).toMatch(/\btext-success\b.*\bbg-success\/11\b/)
    expect(story(asked(), { sessions: [agentSession({ state: 'working' })] })).toMatch(/\btext-info\b.*\bbg-info\/12\b/)
    expect(story(item())).toMatch(/\btext-muted\b/)
    expect(story(item())).not.toMatch(/\bbg-/)
  })

  it('draws the track as three dots: filled, held, running, waiting, and dotted', () => {
    mountCompact(
      asked({
        details: details({
          checks: { ...details().checks, pending: 2, awaiting: 1, awaitingOnGitHub: 0 },
          reviews: { ...details().reviews, decision: 'REVIEW_REQUIRED', approvals: 0, reviewed: 0 },
          pushedSinceReview: true,
        }),
      }),
    )

    const dots = [...document.querySelectorAll('[data-step]')]
    expect(dots.map((dot) => dot.getAttribute('data-state'))).toEqual(['awaiting', 'rereview', 'mergeable'])
    const [held, rereview, mergeable] = dots
    expect(held).toHaveClass('size-2.25', 'border-warning', 'bg-warning/45')
    expect(rereview).toHaveClass('bg-accent')
    expect(mergeable).toHaveClass('bg-success')
  })

  it('pings a running check’s dot, and draws a draft’s review and merge dotted', () => {
    mountCompact(
      item({ authored: true, isDraft: true, details: details({ checks: { ...details().checks, pending: 2 } }) }),
    )

    const [running, review, merge] = [...document.querySelectorAll('[data-step]')]
    expect(running).toHaveAttribute('data-state', 'pending')
    expect(running?.firstElementChild).toHaveClass('motion-safe:animate-ping')
    expect(review).toHaveClass('border-dotted')
    expect(merge).toHaveClass('border-dotted')
  })

  it('names the go button for the primary in its tone, and acts on it', async () => {
    const onOpen = vi.fn<(item: InboxItem, tab: TriageTab) => void>()
    const it = asked()
    const row = mountCompact(it, { onOpen })

    const go = within(row).getByRole('button', { name: 'Review' })
    expect(go).toHaveAttribute('data-verb', 'review')
    expect(go).toHaveClass('text-accent', 'card-lit:bg-accent', 'card-lit:text-white')
    expect(go).not.toHaveClass('border')
    expect(go.querySelector('svg')).toBeNull()

    await userEvent.click(go)

    expect(onOpen).toHaveBeenCalledExactlyOnceWith(it, 'inbox')
  })

  it('holds a waiting agent’s go button still, and says under the card why it couldn’t show the terminal', async () => {
    const session = agentSession()
    const row = mountCompact(asked(), { sessions: [session] })

    const go = within(row).getByRole('button', { name: 'Show terminal' })
    expect(go).toHaveClass('text-warning', 'card-lit:bg-warning')
    expect(go).not.toHaveClass('motion-safe:animate-breathe')

    platform.focusSession.mockRejectedValueOnce(new Error('Couldn’t find the session’s tab.'))
    await userEvent.click(go)

    const alert = await within(row).findByRole('alert')
    expect(alert).toHaveTextContent('Couldn’t find the session’s tab.')
    expect(alert).toHaveClass(...compactOutcomeClass.split(' '))
  })

  it('keeps the agent’s slot mounted in the hover row, so a held launch still asks', async () => {
    const hold = launchHold()
    platform.launch.mockResolvedValueOnce(launchResult({ hold }))
    const edits = { onCopyLink: vi.fn(), onUnassign: vi.fn(), onRemoveReview: vi.fn() }
    const row = mountCompact(asked(), { triage: triageActions(), edits })

    const hover = row.querySelector('[data-hover-row]') as HTMLElement
    expect(hover).toHaveClass('opacity-0', 'card-lit:opacity-100')
    expect(hover).toHaveClass('absolute', 'col-[1/-2]', 'row-[1/2]', 'right-0')
    // The triage, the agent's slot, Show details, then the menu.
    expect(
      within(hover)
        .getAllByRole('button')
        .map((b) => b.getAttribute('aria-label') ?? b.textContent),
    ).toEqual([
      'Refresh from GitHub',
      'Snooze for 3 days',
      'Snooze for…',
      'Dismiss',
      'Pin to top',
      'Open in Claude',
      'Launch options: Review · default',
      'Show details',
      'More actions',
    ])

    await userEvent.click(within(hover).getByRole('button', { name: 'Open in Claude' }))
    await userEvent.click(
      within(await screen.findByRole('dialog', { name: 'Launch options' })).getByRole('button', {
        name: 'Open in Claude',
      }),
    )

    expect(
      await screen.findByRole('dialog', { name: 'This pull request changes agent configuration' }),
    ).toBeInTheDocument()
    expect(platform.launch).toHaveBeenCalledOnce()
  })

  it('has no agent slot until the launch options load, but its go button', () => {
    compact(
      <ul>
        <ItemRow item={asked()} launch={null} />
      </ul>,
    )

    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Review' })).toBeInTheDocument()
  })

  it('opens the peek from the card’s empty space and from Show details', async () => {
    const row = mountCompact(asked())
    const [stretched, chevron] = within(row).getAllByRole('button', { name: 'Show details' }) as [
      HTMLElement,
      HTMLElement,
    ]

    expect(stretched).toHaveClass('absolute', 'inset-0', 'z-0')
    expect(stretched).toHaveAttribute('tabIndex', '-1')

    await userEvent.click(stretched)

    const region = screen.getByRole('region', { name: 'Details' })
    expect(chevron).toHaveAttribute('aria-controls', region.id)
    expect(within(region).getByRole('button', { name: 'Claude…' })).toBeInTheDocument()

    await userEvent.click(chevron)

    expect(screen.queryByRole('region', { name: 'Details' })).not.toBeInTheDocument()
  })

  it('opens the peek with no launch options before they load', async () => {
    compact(
      <ul>
        <ItemRow item={asked()} launch={null} />
      </ul>,
    )

    await userEvent.click(screen.getAllByRole('button', { name: 'Show details' })[0] as HTMLElement)

    expect(screen.getByRole('region', { name: 'Details' })).toBeInTheDocument()
  })

  it('keeps every control and every titled element above the stretched toggle', async () => {
    const row = mountCompact(
      asked({
        trusted: false,
        trust: 'outside',
        files: fileCheck(),
        pinnedAt: '2026-06-15T11:00:00Z',
        unread: true,
      }),
      { triage: triageActions(), onOpen: vi.fn(), onFilterBy: vi.fn(), sessions: [agentSession({ state: 'working' })] },
    )
    expect(within(row).getByTitle('New since you last opened it')).toBeInTheDocument()
    const [stretched] = within(row).getAllByRole('button', { name: 'Show details' })
    await userEvent.click(stretched as HTMLElement)

    const controls = [...row.querySelectorAll('button, [title]')].filter((el) => el !== stretched)
    expect(controls.length).toBeGreaterThan(15)
    const below = controls.filter((el) => el.closest('.z-10, .z-20, .z-30') === null)
    expect(below.map((el) => el.getAttribute('aria-label') ?? el.getAttribute('title') ?? el.textContent)).toEqual([])
  })

  it('narrows with its list: the repository and size go below 56rem, the story and age below 48rem', () => {
    const row = mountCompact(asked())
    const { repo, size, story, age } = (() => {
      const cells = [...row.children].filter((el) => !el.hasAttribute('data-hover-row'))

      return { repo: cells[2], story: cells[4], size: cells[7], age: cells[8] }
    })()

    expect(row).toHaveClass(
      'grid-cols-[0.1875rem_1rem_9.375rem_minmax(0,1fr)_auto_1.125rem_3.125rem_1.625rem_2rem_7.25rem]',
      'grid-rows-[2.375rem]',
      '@max-4xl:grid-cols-[0.1875rem_1rem_minmax(0,1fr)_auto_1.125rem_3.125rem_2rem_7.25rem]',
      '@max-3xl:grid-cols-[0.1875rem_1rem_minmax(0,1fr)_1.125rem_3.125rem_7.25rem]',
    )
    expect(repo).toHaveClass('@max-4xl:hidden')
    expect(size).toHaveClass('@max-4xl:hidden')
    expect(story).toHaveClass('@max-3xl:hidden')
    expect(age).toHaveClass('@max-3xl:hidden')
  })

  it('puts the pin after the title, its number, and its label dots, and the unread dot last', () => {
    const row = mountCompact(asked({ pinnedAt: '2026-06-15T11:00:00Z', unread: true }), { triage: triageActions() })

    const title = within(row).getByRole('button', { name: 'Add the widget cache' }).parentElement as HTMLElement
    const parts = [...title.children].map((el) =>
      el.getAttribute('role') === 'img'
        ? (el.getAttribute('aria-label') ?? '')
        : el.tagName === 'BUTTON'
          ? (el.getAttribute('aria-label') ?? 'title')
          : (el.getAttribute('title') ?? el.textContent),
    )
    expect(parts).toEqual([
      'title',
      'Show details',
      '#7',
      'Labels: enhancement, needs-docs',
      'Pinned to the top of the list',
      'New since you last opened it',
    ])
    expect(title.lastElementChild).toHaveTextContent('Unread')
  })

  it('marks its title, as a comfortable card does, so the inspector reads it', () => {
    const row = mountCompact(asked())

    expect(row.querySelector('[data-card-title]')).toBe(
      within(row).getByRole('button', { name: 'Add the widget cache' }),
    )
  })

  it('shows no unread dot on a card read', () => {
    const read = mountCompact(asked())

    expect(within(read).queryByTitle('New since you last opened it')).not.toBeInTheDocument()
  })

  it('shows no unread dot while the cards show none', () => {
    renderPlain(
      <CardPrefsContext value={{ ...defaultCardPrefs, density: 'compact', unread: false }}>
        <ul>
          <ItemRow item={asked({ unread: true })} launch={null} />
        </ul>
      </CardPrefsContext>,
      { wrapper: at() },
    )

    expect(screen.getByRole('listitem')).toHaveAttribute('data-density', 'compact')
    expect(screen.queryByTitle('New since you last opened it')).not.toBeInTheDocument()
  })

  it('shows up to six label dots in their colors, the hidden left out, every name in the title', () => {
    const labels = ['bug', 'ci', 'docs', 'p1', 'size/XL', 'tests', 'ux', 'wip'].map((name, i) => ({
      name,
      color: i === 0 ? '' : '#a2eeef',
    }))
    compact(
      <ul>
        <ItemRow item={asked({ details: details({ labels }) })} launch={null} />
      </ul>,
      ['size/*'],
    )

    const dots = screen.getByRole('img', { name: 'Labels: bug, ci, docs, p1, tests, ux, wip' })
    expect(dots).toHaveAttribute('title', 'bug, ci, docs, p1, tests, ux, wip')
    const shownDots = dots.querySelectorAll('[data-label-dot]')
    expect(shownDots).toHaveLength(6)
    expect(shownDots[0]).toHaveClass('bg-muted')
    expect(shownDots[1]).toHaveStyle({ backgroundColor: '#a2eeef' })
  })

  it('shows no label dots when every label is hidden', () => {
    compact(
      <ul>
        <ItemRow item={asked({ details: details({ labels: [{ name: 'size/M', color: '' }] }) })} launch={null} />
      </ul>,
      ['size/*'],
    )

    expect(screen.queryByRole('img', { name: /^Labels:/ })).not.toBeInTheDocument()
  })

  it('filters by its repository, and opens its title in its sheet or, with ⌘, on GitHub', async () => {
    const onFilterBy = vi.fn()
    const onOpen = vi.fn()
    const triage = triageActions()
    const it = asked()
    mountCompact(it, { onFilterBy, onOpen, tab: 'inbox', triage })

    // The repository is text its column's ellipsis cuts, under the button (FilterText),
    // never inside it, where a name too long for the column would leave the cell empty.
    const repo = screen.getByRole('button', { name: 'Filter by repository acme-org/example-repo' })
    expect(repo).toBeEmptyDOMElement()
    expect(repo.closest('[data-repo]')).toHaveClass('min-w-0', 'truncate')
    expect(repo.closest('[data-repo]')).toHaveTextContent(/^acme-org\/example-repo$/)
    await userEvent.click(repo)
    expect(onFilterBy).toHaveBeenCalledExactlyOnceWith('repo', 'acme-org/example-repo')

    const title = screen.getByRole('button', { name: 'Add the widget cache' })
    expect(title).toHaveAttribute('title', 'Add the widget cache\nReview it here; ⌘-click opens GitHub')
    await userEvent.click(title)
    expect(onOpen).toHaveBeenCalledExactlyOnceWith(it, 'inbox')

    const user = userEvent.setup()
    expect(triage.onMarkRead, 'the sheet marks it').not.toHaveBeenCalled()
    await user.keyboard('{Meta>}')
    await user.click(title)
    await user.keyboard('{/Meta}')
    expect(platform.openExternal).toHaveBeenCalledWith(it.url)
    expect(triage.onMarkRead, 'opening it on GitHub sees it').toHaveBeenCalledExactlyOnceWith(it)
  })

  it('opens an issue’s title on GitHub without a sheet', async () => {
    mountCompact(item({ kind: 'issue', details: details() }))

    const title = screen.getByRole('button', { name: 'Add the widget cache' })
    expect(title).toHaveAttribute('title', 'Add the widget cache')
    await userEvent.click(title)

    expect(platform.openExternal).toHaveBeenCalledWith(item().url)
  })

  it('says when a snoozed item comes back, in the story’s place', () => {
    mountCompact(item({ snoozedUntil: '2026-06-18T09:00:00Z', details: details() }), {
      tab: 'snoozed',
      triage: triageActions(),
    })

    expect(screen.getByTitle(/^Snoozed until/)).toHaveTextContent(/^Back /)
    expect(screen.getByRole('button', { name: 'Unsnooze' })).toHaveClass('text-muted', 'card-lit:bg-raised')
  })

  it('says where a snoozed item stands after when it comes back, in the story chip', () => {
    const row = mountCompact(
      item({
        assigned: true,
        author: 'contributor-a',
        snoozedUntil: '2026-06-16T18:45:00Z',
        details: details({ viewerVerdict: 'changes', mergeable: 'CONFLICTING' }),
      }),
      { tab: 'snoozed', triage: triageActions() },
    )

    const chip = row.querySelector('[data-story-chip]')
    expect(chip).toHaveTextContent(/^Back tomorrow, .+·Waiting on the author$/)
    expect(chip?.firstElementChild).toHaveAttribute('title', expect.stringMatching(/^Snoozed until/))
    expect(chip?.querySelector('[data-story-state]')).toHaveClass('text-muted', 'truncate')
    expect(chip?.className).not.toMatch(/\bbg-/)
  })

  it('fades the card under a note while an edit will likely take it out of the list', async () => {
    const finish = deferred()
    const edits = {
      onCopyLink: vi.fn(),
      onUnassign: vi.fn(() => finish.promise),
      onRemoveReview: vi.fn(() => Promise.resolve()),
    }
    const row = mountCompact(item({ assigned: true, details: details() }), { triage: triageActions(), edits })

    await userEvent.click(within(row).getByRole('button', { name: 'More actions' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Unassign me' }))

    expect(row).toHaveAttribute('aria-busy', 'true')
    expect(within(row).getByRole('status')).toHaveTextContent('Unassigning you on GitHub…')
    expect(row.querySelector('[data-hover-row]')).toHaveClass('opacity-100')

    await act(async () => {
      finish.resolve()
      await Promise.resolve()
    })

    expect(row).not.toHaveAttribute('aria-busy')
  })
})
