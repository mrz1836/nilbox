import { render as renderPlain, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { InboxItem } from '../platform'
import { at } from '../test/clock'
import { details, fileCheck, item, itemResolution, profile, prompts } from '../test/fixtures'
import { platform } from '../test/platform'
import { ItemRow, type LaunchOptions } from './ItemRow'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const launch: LaunchOptions = {
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
    expect(screen.getByText(/acme-org\/example-repo #7 · octo-user ·/)).toBeInTheDocument()
    expect(screen.getByText('3h')).toHaveAttribute('dateTime', '2026-06-15T09:00:00Z')
    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeInTheDocument()

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
      'Launches in plan mode unless you lift the restriction',
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

    expect(screen.getByRole('button', { name: 'Open in Claude' })).toBeDisabled()
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
