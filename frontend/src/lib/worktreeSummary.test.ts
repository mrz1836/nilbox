import { describe, expect, it } from 'vitest'

import { removeResult, worktree } from '../test/fixtures'
import { branchSummary, bulkSummary, failedSummary, shortName, singleSummary } from './worktreeSummary'

describe('shortName', () => {
  it.each([
    ['~/projects/.nilbox/worktrees/example-repo/pr-7', 'example-repo/pr-7'],
    ['~/projects/example-repo/.claude/worktrees/issue-9', 'example-repo/issue-9'],
    ['~/projects/example-repo-scratch', 'projects/example-repo-scratch'],
    ['scratch', 'scratch'],
  ])('names %s as %s', (path, name) => {
    expect(shortName(path)).toBe(name)
  })
})

describe('singleSummary', () => {
  const wt = worktree({ displayPath: '~/projects/.nilbox/worktrees/example-repo/pr-7' })

  it('names every branch deleted with the worktree', () => {
    expect(
      singleSummary('Removed', wt, removeResult({ deletedBranches: ['worktree-pr-7', 'feature-x', 'fix-y'] })),
    ).toEqual({
      title: 'Removed example-repo/pr-7',
      detail: 'Its branches worktree-pr-7, feature-x, and fix-y were deleted.',
      tone: 'success',
    })
  })

  it('says why a branch ticked was kept', () => {
    expect(
      singleSummary('Cleaned up', wt, removeResult({ deletedBranches: [], reason: 'kept worktree-pr-7: it moved' })),
    ).toEqual({
      title: 'Cleaned up example-repo/pr-7',
      detail: 'No branch was deleted. Kept worktree-pr-7: it moved.',
      tone: 'warning',
    })
  })
})

describe('bulkSummary', () => {
  it('counts the worktrees taken and the branches deleted with them', () => {
    expect(
      bulkSummary(
        [removeResult(), removeResult({ path: '/elsewhere', deletedBranches: ['worktree-pr-8', 'feature-8'] })],
        'finished worktree',
        'Cleaned up',
      ),
    ).toEqual({ title: 'Cleaned up 2 finished worktrees', detail: '3 branches deleted.', tone: 'success' })
  })

  it('warns when a worktree taken kept a branch ticked', () => {
    expect(
      bulkSummary(
        [removeResult({ deletedBranches: [], reason: 'kept worktree-pr-7: it moved' })],
        'clean worktree',
        'Removed',
      ),
    ).toEqual({ title: 'Removed 1 clean worktree', detail: 'No branch was deleted.', tone: 'warning' })
  })
})

describe('branchSummary', () => {
  it('says nothing more when no branch was deleted or kept', () => {
    expect(branchSummary([])).toEqual({ title: 'Deleted 0 branches', detail: '', tone: 'success' })
  })
})

describe('failedSummary', () => {
  it('reports a failure with why', () => {
    expect(failedSummary('Couldn’t delete the branches', 'offline')).toEqual({
      title: 'Couldn’t delete the branches',
      detail: 'offline',
      tone: 'error',
    })
  })
})
