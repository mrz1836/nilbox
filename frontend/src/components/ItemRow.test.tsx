import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { InboxItem } from '../platform'
import { details, item, itemResolution, profile, prompts } from '../test/fixtures'
import { ItemRow, type LaunchOptions } from './ItemRow'

const platform = vi.hoisted(() => ({ openInBrowser: vi.fn(), launch: vi.fn() }))

vi.mock('../platform', () => platform)

const now = new Date('2026-06-15T12:00:00Z')
const launch: LaunchOptions = {
  profiles: [profile()],
  prompts: prompts(),
  launchDefaults: [],
  resolutions: new Map([[1, itemResolution()]]),
  onDefaultsSaved: () => Promise.resolve(),
  onManagePrompts: () => undefined,
}

function renderRow(it: InboxItem, options: LaunchOptions | null = launch) {
  render(
    <ul>
      <ItemRow item={it} now={now} launch={options} />
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

beforeEach(() => {
  platform.openInBrowser.mockReset().mockResolvedValue(undefined)
})

describe('ItemRow', () => {
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

  it('handles details with no labels at all', () => {
    renderRow(item({ details: details({ labels: null }) }))

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

    expect(platform.openInBrowser).toHaveBeenCalledWith('https://github.com/acme-org/example-repo/pull/7')
  })
})
