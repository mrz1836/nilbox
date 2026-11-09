import { render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { itemWorktrees, worktreeOf } from '../lib/itemWorktrees'
import type { InboxItem, Worktree } from '../platform'
import { at } from '../test/clock'
import {
  agentSession,
  agents,
  details,
  fileCheck,
  item,
  itemResolution,
  linkedItem,
  mainWorktree,
  profile,
  project,
  prompts,
  worktree,
  worktreeRepo,
  worktreesSnapshot,
} from '../test/fixtures'
import { platform } from '../test/platform'
import { ItemRow, type LaunchOptions } from './ItemRow'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const launch: LaunchOptions = {
  agents: agents(),
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
}

// render renders on a clock that stands at now.
function render(ui: ReactElement) {
  return renderPlain(ui, { wrapper: at() })
}

function renderRow(it: InboxItem, options: LaunchOptions | null = launch) {
  render(
    <ul>
      <ItemRow item={it} launch={options} resolution={it.id === 1 ? itemResolution() : null} />
    </ul>,
  )
}

// signals returns the queries scoped to the card's line of details.
function signals() {
  return within(screen.getByRole('list', { name: 'Details' }))
}

// slot returns the signal whose full sentence is text.
function slot(text: string): HTMLElement {
  return screen.getByTitle(text)
}

describe('ItemRow', () => {
  it('names its item for the inbox to find, and marks it when highlighted', () => {
    const { rerender } = render(
      <ul>
        <ItemRow item={item({ id: 42 })} launch={launch} />
      </ul>,
    )

    const row = screen.getByRole('listitem')
    expect(row).toHaveAttribute('data-item-id', '42')
    expect(row).not.toHaveAttribute('data-highlighted')

    rerender(
      <ul>
        <ItemRow item={item({ id: 42 })} launch={launch} highlighted />
      </ul>,
    )
    expect(row).toHaveAttribute('data-highlighted', 'true')
  })

  it('shows a pull request on three lines', () => {
    renderRow(item({ title: 'Add the widget cache', details: details() }))

    expect(screen.getByRole('img', { name: 'Pull request' })).toHaveClass('text-success')
    expect(screen.getByRole('button', { name: 'Add the widget cache' })).toHaveAttribute(
      'title',
      'Add the widget cache',
    )
    expect(screen.getByRole('button', { name: 'Add the widget cache' }).closest('li')).toHaveTextContent(
      /acme-org\/example-repo #7 · octo-user ·/,
    )
    expect(screen.getByText('3h')).toHaveAttribute('dateTime', '2026-06-15T09:00:00Z')
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()
    // Without a filter to add to, the repository, author, and labels are plain text.
    expect(screen.queryByRole('button', { name: /^Filter by/ })).not.toBeInTheDocument()

    const checks = slot('Checks: 12 passed, 3 skipped')
    expect(checks).toHaveClass('text-success')
    expect(checks).toHaveTextContent('12/12')

    expect(slot('Reviews: 1 of 2 reviewers reviewed, 1 approved')).toHaveClass('text-success')
    expect(slot('Reviews: 1 of 2 reviewers reviewed, 1 approved')).toHaveTextContent('1/2')
    expect(slot('4 comments')).toHaveTextContent('4')

    const size = slot('Size L: 120 lines added and 30 removed across 6 files in 3 commits')
    expect(within(size).getByText('L')).toBeInTheDocument()
    expect(within(size).getByText('+120')).toHaveClass('text-success')
    expect(within(size).getByText('−30')).toHaveClass('text-danger')
    expect(size).toHaveTextContent('6 files')
    expect(size).toHaveTextContent('3 commits')

    expect(signals().getByText('enhancement')).toBeInTheDocument()
    expect(signals().getByText('needs-docs')).toBeInTheDocument()
  })

  it('colors each label with its GitHub color, or a neutral dot', () => {
    renderRow(item({ details: details() }))

    const colored = signals().getByText('enhancement').previousElementSibling
    expect(colored).toHaveStyle({ backgroundColor: '#a2eeef' })

    const plain = signals().getByText('needs-docs').previousElementSibling
    expect(plain).toHaveClass('bg-muted')
  })

  it('folds labels past the third into a count', () => {
    const labels = ['one', 'two', 'three', 'four', 'five'].map((name) => ({ name, color: '#d73a4a' }))
    renderRow(item({ details: details({ labels }) }))

    expect(signals().getByText('three')).toBeInTheDocument()
    expect(signals().queryByText('four')).not.toBeInTheDocument()
    expect(signals().getByTitle('four, five')).toHaveTextContent('+2')
    expect(signals().getByText('more: four, five')).toHaveClass('sr-only')
  })

  it('marks failing checks, requested changes, and conflicts in red', () => {
    renderRow(
      item({
        details: details({
          mergeable: 'CONFLICTING',
          checks: { state: 'FAILURE', passed: 18, failed: 2, pending: 0, skipped: 0 },
          reviews: { decision: 'CHANGES_REQUESTED', reviewers: 1, reviewed: 1, approvals: 0, changesRequested: 1 },
        }),
      }),
    )

    expect(slot('Checks: 18 passed, 2 failing')).toHaveClass('text-danger')
    expect(slot('Checks: 18 passed, 2 failing')).toHaveTextContent('18/20')
    expect(slot('Reviews: 1 of 1 reviewer reviewed, 1 requested changes')).toHaveClass('text-danger')
    expect(screen.getByText('Conflicts')).toHaveClass('text-danger')
  })

  it('marks running checks in yellow and a pull request without checks or reviewers as such', () => {
    renderRow(
      item({
        details: details({
          checks: { state: 'PENDING', passed: 3, failed: 0, pending: 2, skipped: 0 },
          reviews: { decision: '', reviewers: 0, reviewed: 0, approvals: 0, changesRequested: 0 },
        }),
      }),
    )

    expect(slot('Checks: 3 passed, 2 running')).toHaveClass('text-warning')
    expect(slot('Reviews: no reviewers yet')).toHaveTextContent('—')
  })

  it('tags a draft and mutes its icon', () => {
    renderRow(item({ isDraft: true, details: details() }))

    expect(screen.getByRole('img', { name: 'Draft pull request' })).toHaveClass('text-muted')
    expect(screen.getByText('Draft')).toBeInTheDocument()
    expect(screen.queryByText('Conflicts')).not.toBeInTheDocument()
  })

  it('shows an issue with its comments and labels only', () => {
    renderRow(
      item({
        kind: 'issue',
        details: details({ comments: 1, labels: [{ name: 'bug', color: '#d73a4a' }] }),
      }),
    )

    expect(screen.getByRole('img', { name: 'Issue' })).toBeInTheDocument()
    expect(slot('1 comment')).toHaveTextContent('1')
    expect(signals().getByText('bug')).toBeInTheDocument()
    expect(screen.queryByTitle(/^Checks/)).not.toBeInTheDocument()
    expect(screen.queryByTitle(/^Reviews/)).not.toBeInTheDocument()
    expect(screen.queryByTitle(/^Size/)).not.toBeInTheDocument()
  })

  it('shows the project an issue is in, with its status there in the status’s color', () => {
    renderRow(item({ kind: 'issue', details: details({ comments: 2, projects: [project()] }) }))

    const link = signals().getByRole('button', { name: 'Project Roadmap: In progress. Open it on GitHub' })
    expect(link).toHaveAttribute('title', 'Roadmap: In progress\nOpen the project on GitHub')
    expect(within(link).getByText('Roadmap')).toBeInTheDocument()

    const status = within(link).getByText('In progress').parentElement
    expect(status).toHaveStyle({ backgroundColor: '#bf870029' })
    expect(status?.firstElementChild).toHaveStyle({ backgroundColor: '#bf8700' })
    expect(signals().getByText('Projects:')).toHaveClass('sr-only')
  })

  it('opens an issue’s project on GitHub', async () => {
    renderRow(item({ kind: 'issue', details: details({ projects: [project()] }) }))

    await userEvent.click(signals().getByRole('button', { name: /^Project Roadmap/ }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/orgs/acme-org/projects/1')
  })

  it('names a project alone when the issue has no status there, and a status without a color neutrally', () => {
    renderRow(
      item({
        kind: 'issue',
        details: details({
          projects: [project({ status: 'Todo', statusColor: '' }), project({ title: 'Sprint', status: '' })],
        }),
      }),
    )

    const status = signals().getByText('Todo').parentElement
    expect(status).toHaveClass('bg-raised')
    expect(status?.firstElementChild).toHaveClass('bg-muted')
    expect(signals().getByTitle('Sprint')).toHaveTextContent('+1')
    expect(signals().getByText('more: Sprint')).toHaveClass('sr-only')
  })

  it('folds an issue’s projects past the first into a count, with their statuses in its tooltip', () => {
    renderRow(
      item({
        kind: 'issue',
        details: details({
          projects: [
            project(),
            project({ title: 'Sprint', status: 'Todo' }),
            project({ title: 'Triage', status: 'New' }),
          ],
        }),
      }),
    )

    expect(signals().getByRole('button', { name: /^Project Roadmap/ })).toBeInTheDocument()
    expect(signals().queryByRole('button', { name: /^Project Sprint/ })).not.toBeInTheDocument()
    const rest = signals().getByTitle(/^Sprint: Todo\s+Triage: New$/)
    expect(rest).toHaveAttribute('title', 'Sprint: Todo\nTriage: New')
    expect(rest).toHaveTextContent('+2')
  })

  it('shows an issue’s priority and size beside its status, and every field in the tooltip', () => {
    const fields = [
      { name: 'Phase', value: 'Beta', color: '' },
      { name: 'Size', value: 'XL', color: '' },
      { name: 'Priority', value: 'High', color: '#fa4549' },
    ]
    renderRow(item({ kind: 'issue', details: details({ projects: [project({ fields })] }) }))

    const link = signals().getByRole('button', {
      name: 'Project Roadmap: In progress, Priority High, Size XL. Open it on GitHub',
    })
    expect(link).toHaveAttribute(
      'title',
      'Roadmap: In progress\nPhase: Beta\nSize: XL\nPriority: High\nOpen the project on GitHub',
    )
    expect(within(link).getByText('High').parentElement).toHaveStyle({ borderColor: '#fa4549b3' })
    expect(within(link).getByText('XL').parentElement).toHaveClass('border-border')
    expect(within(link).queryByText('Beta')).not.toBeInTheDocument()
  })

  it('shows how far along an issue’s sub-issues are', () => {
    renderRow(item({ kind: 'issue', details: details({ subIssues: { total: 8, completed: 6 } }) }))

    const progress = slot('Sub-issues: 6 of 8 sub-issues done (75%)')
    expect(progress).toHaveTextContent('6/8')
    expect(progress).not.toHaveClass('text-success')
    expect(progress.querySelector('.bg-success')).toHaveStyle({ width: '75%' })
  })

  it('marks an issue whose sub-issues are all done', () => {
    renderRow(item({ kind: 'issue', details: details({ subIssues: { total: 2, completed: 2 } }) }))

    expect(slot('Sub-issues: 2 of 2 sub-issues done (100%)')).toHaveClass('text-success')
  })

  it('shows no sub-issue progress for an issue without sub-issues', () => {
    renderRow(item({ kind: 'issue', details: details() }))

    expect(screen.queryByTitle(/^Sub-issues/)).not.toBeInTheDocument()
  })

  it('shows no project on a pull request', () => {
    renderRow(item({ details: details({ projects: [project()] }) }))

    expect(signals().queryByRole('button', { name: /^Project/ })).not.toBeInTheDocument()
  })

  it('gives each value the same width on every card, whatever it says, so the columns line up', () => {
    const small = details({ comments: 8, additions: 689, deletions: 65, changedFiles: 20, commits: 3 })
    const large = details({
      comments: 0,
      additions: 12_799,
      deletions: 25_322,
      changedFiles: 358,
      commits: 63,
      checks: { state: '', passed: 0, failed: 0, pending: 0, skipped: 0 },
      reviews: { decision: '', reviewers: 0, reviewed: 0, approvals: 0, changesRequested: 0 },
    })
    render(
      <ul>
        <ItemRow item={item({ id: 1, details: small })} launch={null} />
        <ItemRow item={item({ id: 2, details: large })} launch={null} />
      </ul>,
    )

    // values reads the classes of every value on a card's line of details, labels aside.
    const values = (list: HTMLElement) =>
      [...list.querySelectorAll('li[title] > [aria-hidden] > span')].map((value) => value.className)
    const [first = [], second] = screen.getAllByRole('list', { name: 'Details' }).map(values)

    expect(slot('No checks on the latest commit')).toHaveTextContent('No checks')
    expect(first).toHaveLength(8)
    expect(second).toEqual(first)
    for (const value of first) {
      expect(value).toMatch(/\bmin-w-\[[\d.]+ch\]/)
    }
  })

  it('handles details with no labels at all', () => {
    renderRow(item({ details: details({ labels: [] }) }))

    expect(signals().getAllByRole('listitem')).toHaveLength(4)
  })

  it('says when the details have not been read yet', () => {
    renderRow(item())

    expect(screen.getByText('Details arrive with the next refresh.')).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Details' })).not.toBeInTheDocument()
  })

  it('flags an outside contributor', () => {
    renderRow(item({ trust: 'outside', trusted: false }))

    expect(screen.getByText('outside contributor')).toHaveAttribute(
      'title',
      'Launches in plan or read-only mode unless you lift the restriction',
    )
  })

  it('marks a trusted bot quietly, and an untrusted one as a warning', () => {
    renderRow(item({ author: 'dependabot', trust: 'bot', trusted: true, authorIsBot: true }))

    expect(screen.getByText('bot')).toHaveClass('text-muted')
    expect(screen.getByText('bot')).toHaveAttribute('title', expect.stringContaining('A trusted bot'))
    expect(screen.queryByText('outside contributor')).not.toBeInTheDocument()
  })

  it('flags a bot that is not trusted', () => {
    renderRow(item({ author: 'random-helper', trust: 'outside', trusted: false, authorIsBot: true }))

    expect(screen.getByText('untrusted bot')).toHaveClass('text-warning')
    expect(screen.getByText('untrusted bot')).toHaveAttribute('title', expect.stringContaining('Settings → Trust'))
    expect(screen.queryByText('outside contributor')).not.toBeInTheDocument()
  })

  it('tags nothing for the repository’s own people', () => {
    renderRow(item())

    expect(screen.queryByText('bot')).not.toBeInTheDocument()
    expect(screen.queryByText('outside contributor')).not.toBeInTheDocument()
  })

  it('has no launch button until the launch options load', () => {
    renderRow(item(), null)

    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
  })

  it('names the item’s resolved prompt and profile on its launch button', () => {
    renderRow(item())

    expect(screen.getByRole('button', { name: 'Launch options: Review · default' })).toBeInTheDocument()
  })

  it('waits for an item’s launch settings to resolve', () => {
    renderRow(item({ id: 99 }))

    // Until the settings resolve, the agent the button starts is not known.
    expect(screen.getByRole('button', { name: 'Open' })).toBeDisabled()
  })

  it('opens the item in the browser', async () => {
    renderRow(item())

    await userEvent.click(screen.getByRole('button', { name: 'Add the widget cache' }))

    expect(platform.openExternal).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
  })

  it('points out the sensitive files of a checked pull request beside who opened it', async () => {
    const onFilesSettings = vi.fn()
    render(
      <ul>
        <ItemRow
          item={item({ trusted: false, trust: 'outside', details: details(), files: fileCheck() })}
          launch={launch}
          resolution={itemResolution()}
          onFilesSettings={onFilesSettings}
        />
      </ul>,
    )

    const chip = screen.getByRole('button', { name: /^Changes sensitive files/ })
    expect(chip).toHaveTextContent('Agent config · Workflows')
    expect(chip.closest('div')).toHaveTextContent('outside contributor')

    await userEvent.click(chip)
    await userEvent.click(screen.getByRole('button', { name: 'What counts as sensitive' }))
    expect(onFilesSettings).toHaveBeenCalledOnce()
  })

  it('marks an item back from a snooze for a day, then no longer', () => {
    renderRow(item({ id: 2, snoozedUntil: '2026-06-15T11:00:00Z' }))
    expect(screen.getByText('Back from snooze')).toBeInTheDocument()
  })

  it('stops marking an item back from a snooze a day after it came back', () => {
    renderRow(item({ id: 2, snoozedUntil: '2026-06-13T12:00:00Z' }))
    expect(screen.queryByText('Back from snooze')).not.toBeInTheDocument()
  })

  it('shows no file chip for an item whose files are not checked', () => {
    renderRow(item({ details: details(), files: null }))

    expect(screen.queryByRole('button', { name: /sensitive/i })).not.toBeInTheDocument()
  })
})

describe('ItemRow worktree tag', () => {
  // renderWith renders an item's row with its worktree, as the Worktrees view last listed
  // it, if the listing links one to the item.
  function renderWith(it: InboxItem, overrides: Partial<Worktree> = {}) {
    const listed = worktree({ item: linkedItem({ state: 'open' }), ...overrides })
    const worktrees = itemWorktrees(
      worktreesSnapshot({ repos: [worktreeRepo({ worktrees: [mainWorktree(), listed] })] }),
    )

    render(
      <ul>
        <ItemRow item={it} launch={launch} worktree={worktreeOf(worktrees, it)} />
      </ul>,
    )
  }

  // tag is the worktree tag, titled with the worktree's path.
  function tag(): HTMLElement {
    return screen.getByTitle('~/projects/example-repo/.claude/worktrees/pr-7')
  }

  it('marks an item with a worktree, its changes, and the last session launched in it', () => {
    renderWith(item(), { dirty: true, dirtyCount: 2, agent: 'codex', lastLaunchedAt: '2026-06-15T09:00:00Z' })

    expect(tag()).toHaveTextContent('worktree · 2 changes · Codex 3h ago')
  })

  it('offers its running session’s terminal instead of a second launch', () => {
    render(
      <ul>
        <ItemRow item={item()} launch={launch} resolution={itemResolution()} sessions={[agentSession()]} />
      </ul>,
    )

    expect(screen.getByRole('button', { name: 'Show in terminal' })).toBeEnabled()
    expect(screen.queryByRole('button', { name: 'Open in Claude' })).not.toBeInTheDocument()
  })

  it('launches again once its session ended', () => {
    render(
      <ul>
        <ItemRow
          item={item()}
          launch={launch}
          resolution={itemResolution()}
          sessions={[agentSession({ state: 'ended', endedAt: '2026-06-15T11:59:00Z', canFocus: false })]}
        />
      </ul>,
    )

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Show in terminal' })).not.toBeInTheDocument()
  })

  it('says a session is running in it, rather than when one was launched', () => {
    renderWith(item(), {
      inUse: [{ pid: 4242, command: 'claude', agent: '' }],
      agent: 'claude',
      lastLaunchedAt: '2026-06-15T09:00:00Z',
    })

    expect(tag()).toHaveTextContent(/^worktree · in use$/)
  })

  it('marks a worktree no session was launched in as just a worktree', () => {
    renderWith(item())

    expect(tag()).toHaveTextContent(/^worktree$/)
  })

  it('marks nothing for an item with no worktree', () => {
    renderWith(item({ number: 8 }))

    expect(screen.queryByText(/^worktree/)).not.toBeInTheDocument()
  })
})
