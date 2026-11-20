import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { removeResult } from '../../test/fixtures'
import { BranchResults } from './BranchResults'
import { BulkResults } from './BulkResults'
import { ResultCard } from './ResultCard'

describe('ResultCard', () => {
  it('says on one line what a change did in full, and puts itself away from Dismiss', async () => {
    const onDismiss = vi.fn()
    render(
      <ResultCard
        summary={{ title: 'Cleaned up 2 finished worktrees', detail: '2 branches deleted.', tone: 'success' }}
        onDismiss={onDismiss}
      />,
    )

    const card = screen.getByRole('status', { name: 'Cleaned up 2 finished worktrees' })
    expect(card).toHaveClass('border-border')
    expect(card.querySelector('svg')).toHaveClass('text-success')
    expect(within(card).getByText('2 branches deleted.')).toHaveAttribute('title', '2 branches deleted.')
    expect(within(card).getByText('2 branches deleted.')).toHaveClass('truncate')

    await userEvent.click(within(card).getByRole('button', { name: 'Dismiss' }))
    expect(onDismiss).toHaveBeenCalledOnce()
  })

  it('marks a change that left something in amber, with its notes under the line', () => {
    render(
      <BulkResults
        bulk={{
          results: [
            removeResult(),
            removeResult({
              path: '/home/user/projects/example-repo/.claude/worktrees/pr-8',
              displayPath: '~/projects/example-repo/.claude/worktrees/pr-8',
              removed: false,
              deletedBranches: [],
              reason: 'it changed since the listing',
            }),
          ],
          noun: 'clean worktree',
          verb: 'Removed',
        }}
        onDismiss={vi.fn()}
      />,
    )

    const card = screen.getByRole('status', { name: 'Removed 1 of 2 clean worktrees' })
    expect(card).toHaveClass('border-warning/40')
    expect(card.querySelector('svg')).toHaveClass('text-warning')
    expect(within(card).getByText('1 branch deleted · 1 left in place, as noted below.')).toBeInTheDocument()
    const notes = within(card).getByRole('list', { name: 'Notes' })
    expect(notes).toHaveTextContent('~/projects/example-repo/.claude/worktrees/pr-8: it changed since the listing')
    // However many were left, the notes scroll past a few lines.
    expect(notes).toHaveClass('max-h-28', 'overflow-y-auto')
  })

  it('scrolls the branches a deletion kept past a few lines', () => {
    render(
      <BranchResults
        results={[
          {
            repoPath: '/home/user/projects/example-repo',
            displayRepo: '~/projects/example-repo',
            name: 'feature-x',
            head: 'fc1c3975011e66491785ace891c893e4fc716587',
            deleted: false,
            reason: 'the branch moved since it was checked',
          },
        ]}
        onDismiss={vi.fn()}
      />,
    )

    const kept = screen.getByRole('list', { name: 'Branches kept' })
    expect(kept).toHaveTextContent('~/projects/example-repo: feature-x kept: the branch moved since it was checked')
    expect(kept).toHaveClass('max-h-28', 'overflow-y-auto')
  })
})
