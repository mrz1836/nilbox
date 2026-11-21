import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createRef } from 'react'
import { describe, expect, it, vi } from 'vitest'

import type { WorktreeCleanup } from '../../platform'
import { FIXED_NOW } from '../../test/clock'
import { linkedItem, managed, worktreeCleanup } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { CleanupHistory, type CleanupHistoryProps } from './CleanupHistory'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const head = 'fc1c3975011e66491785ace891c893e4fc716587'

// issue is example-repo's issue #9, closed.
const issue = linkedItem({
  kind: 'issue',
  number: 9,
  title: 'Crash on start',
  url: 'https://github.com/acme-org/example-repo/issues/9',
  state: 'closed',
  closedAt: '',
})

// renderHistory draws cleanups, as of the tests' clock, with Clear a mock.
function renderHistory(cleanups: readonly WorktreeCleanup[], overrides: Partial<CleanupHistoryProps> = {}) {
  const onClear = vi.fn()
  const sectionRef = createRef<HTMLElement>()

  render(
    <CleanupHistory
      cleanups={cleanups}
      now={FIXED_NOW}
      disabled={false}
      error=""
      onClear={onClear}
      sectionRef={sectionRef}
      {...overrides}
    />,
  )

  return { onClear, sectionRef, history: within(screen.getByRole('region', { name: 'Recently cleaned up' })) }
}

// lines are the history's lines.
function lines(): HTMLElement[] {
  return within(screen.getByRole('list', { name: 'Recently cleaned up worktrees' })).getAllByRole('listitem')
}

describe('CleanupHistory', () => {
  it('heads the list with how many there are, and that everything in them was on GitHub', () => {
    const { history, sectionRef } = renderHistory([worktreeCleanup(), worktreeCleanup({ id: 2 })])

    const heading = history.getByRole('heading')
    expect(heading).toHaveTextContent('Recently cleaned up · 2')
    expect(heading).toHaveTextContent('everything in them was on GitHub')
    expect(sectionRef.current).toBe(screen.getByRole('region', { name: 'Recently cleaned up' }))
  })

  it('says on one line where each was, what for, who cleaned it up, its branch, its commit, and when', () => {
    renderHistory([worktreeCleanup()])

    const line = within(lines()[0] as HTMLElement)
    expect(line.getByTitle('Pull request, merged')).toBeInTheDocument()
    expect(line.getByTitle('~/projects/example-repo/.claude/worktrees/pr-7')).toHaveTextContent('example-repo / pr-7')
    expect(line.getByText('#7').parentElement).toHaveTextContent('#7 Add the widget cache')
    expect(line.getByText('automatic · after merge')).toBeInTheDocument()
    expect(line.getByText('worktree-pr-7').parentElement).toHaveTextContent('deleted worktree-pr-7')
    expect(line.getByText('fc1c397')).toHaveAttribute('title', `Was at ${head}, which is on GitHub`)

    const when = line.getByText('2h ago')
    expect(when).toHaveAttribute('dateTime', '2026-06-15T10:00:00Z')
    expect(when.getAttribute('title')).toMatch(/2026/)
  })

  it.each<[string, Partial<WorktreeCleanup>, string, string]>([
    ['one cleaned up by hand', { automatic: false, item: issue }, 'by you', 'Issue, closed'],
    ['an issue cleaned up automatically', { item: issue }, 'automatic · after close', 'Issue, closed'],
    ['one for nothing', { item: null }, 'automatic · after it finished', 'Cleaned up'],
    [
      'a pull request closed without merging',
      { item: linkedItem({ state: 'closed', closedAt: '2026-06-14T12:00:00Z' }) },
      'automatic · after merge',
      'Pull request, closed without merging 1d ago',
    ],
  ])('says who cleaned up %s, and marks what it was for', (_label, overrides, how, mark) => {
    renderHistory([worktreeCleanup(overrides)])

    const line = within(lines()[0] as HTMLElement)
    expect(line.getByText(how)).toBeInTheDocument()
    expect(line.getByTitle(mark).querySelector('svg')).not.toBeNull()
  })

  it('says a branch was kept, and shows no commit it doesn’t know', () => {
    renderHistory([worktreeCleanup({ deletedBranch: '', head: '' })])

    const line = lines()[0] as HTMLElement
    expect(line).toHaveTextContent('its branch was kept')
    expect(within(line).queryByTitle(/^Was at/)).not.toBeInTheDocument()
  })

  it('copies the command that brings a worktree back on the branch that went with it', async () => {
    const path = managed('pr-201')
    renderHistory([
      worktreeCleanup({
        worktree: 'pr-201',
        displayPath: path,
        deletedBranch: 'worktree-pr-201',
        item: linkedItem({ number: 201 }),
      }),
    ])

    const command = `git -C ~/projects/example-repo worktree add -b worktree-pr-201 ${path} ${head}`
    const copy = within(lines()[0] as HTMLElement).getByRole('button', {
      name: 'Copy the command that restores pr-201',
    })
    expect(copy).toHaveAttribute(
      'title',
      `Copy restore command: ${command}. If git has pruned it since, git fetch origin pull/201/head brings it back first.`,
    )
    await userEvent.click(copy)

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith(command)
  })

  it('brings back a worktree whose branch was kept at its commit, detached', () => {
    renderHistory([worktreeCleanup({ deletedBranch: '', item: issue })])

    expect(
      within(lines()[0] as HTMLElement).getByRole('button', { name: 'Copy the command that restores pr-7' }),
    ).toHaveAttribute(
      'title',
      `Copy restore command: git -C ~/projects/example-repo worktree add ~/projects/example-repo/.claude/worktrees/pr-7 ${head}.`,
    )
  })

  it('offers no command for a worktree whose clone its path doesn’t say', () => {
    renderHistory([worktreeCleanup({ displayPath: '~/projects/example-repo-hotfix' })])

    expect(
      within(lines()[0] as HTMLElement).queryByRole('button', { name: /^Copy the command/ }),
    ).not.toBeInTheDocument()
  })

  it('opens what it was for on GitHub', async () => {
    renderHistory([worktreeCleanup({ item: issue })])

    const open = within(lines()[0] as HTMLElement).getByRole('button', { name: 'Open Issue #9 on GitHub' })
    expect(open).toHaveAttribute('title', 'Open Issue #9 on GitHub')
    await userEvent.click(open)

    expect(platform.openExternal).toHaveBeenCalledExactlyOnceWith('https://github.com/acme-org/example-repo/issues/9')
  })

  it('offers nothing to open for a worktree that was for nothing', () => {
    renderHistory([worktreeCleanup({ item: null })])

    expect(within(lines()[0] as HTMLElement).queryByRole('button', { name: /^Open/ })).not.toBeInTheDocument()
  })

  it('folds and unfolds from its heading', async () => {
    const { history } = renderHistory([worktreeCleanup()])

    const heading = history.getByRole('button', { name: /^Recently cleaned up/ })
    expect(heading).toHaveAttribute('aria-expanded', 'true')
    await userEvent.click(heading)

    expect(heading).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('list')).not.toBeInTheDocument()

    await userEvent.click(heading)
    expect(lines()).toHaveLength(1)
  })

  it('shows the newest five, and the rest on request', async () => {
    const { history } = renderHistory(
      Array.from({ length: 7 }, (_, i) => worktreeCleanup({ id: i + 1, worktree: `pr-${String(i + 1)}` })),
    )

    expect(lines()).toHaveLength(5)

    const more = history.getByRole('button', { name: 'Show 2 more' })
    expect(more).toHaveAttribute('aria-expanded', 'false')
    await userEvent.click(more)
    expect(lines()).toHaveLength(7)

    await userEvent.click(history.getByRole('button', { name: 'Show fewer' }))
    expect(lines()).toHaveLength(5)
  })

  it('offers no more with five or fewer', () => {
    const { history } = renderHistory([worktreeCleanup()])

    expect(history.queryByRole('button', { name: /more$/ })).not.toBeInTheDocument()
  })

  it('clears the list from Clear, which removes nothing else', async () => {
    const { history, onClear } = renderHistory([worktreeCleanup()])

    const clear = history.getByRole('button', { name: 'Clear' })
    expect(clear).toHaveAttribute('title', 'Forgets this list; removes nothing else')
    await userEvent.click(clear)

    expect(onClear).toHaveBeenCalledOnce()
  })

  it('holds Clear while a change runs, and says it clears', () => {
    renderHistory([worktreeCleanup()], { disabled: true })
    expect(screen.getByRole('button', { name: 'Clear' })).toBeDisabled()
  })

  it('says it is clearing', () => {
    renderHistory([worktreeCleanup()], { clearing: true })
    expect(screen.getByRole('button', { name: 'Clearing…' })).toBeDisabled()
  })

  it('says why it couldn’t clear', () => {
    renderHistory([worktreeCleanup()], { error: 'database is locked' })
    expect(screen.getByRole('alert')).toHaveTextContent('database is locked')
  })
})
