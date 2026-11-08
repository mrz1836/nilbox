import { describe, expect, it } from 'vitest'

import type { WorktreePlan } from '../platform'
import { worktreeLine } from './LaunchPreviewSection'

// plan is the worktree a launch on pull request #7 plans, made from its head.
function plan(overrides: Partial<WorktreePlan> = {}): WorktreePlan {
  return {
    name: 'pr-7',
    path: '~/projects/.app/worktrees/example-repo/pr-7',
    branch: 'worktree-pr-7',
    action: 'create',
    base: 'the pull request’s head',
    dirty: 0,
    ...overrides,
  }
}

describe('worktreeLine', () => {
  it('says what a launch does to its worktree', () => {
    expect(worktreeLine(plan())).toBe(
      'Creates ~/projects/.app/worktrees/example-repo/pr-7 from the pull request’s head',
    )
    expect(worktreeLine(plan({ base: '' }))).toBe('Creates ~/projects/.app/worktrees/example-repo/pr-7')
    expect(worktreeLine(plan({ action: 'attach' }))).toBe(
      'Makes ~/projects/.app/worktrees/example-repo/pr-7 again on worktree-pr-7, keeping its commits',
    )
    expect(worktreeLine(plan({ action: 'adopt', path: '~/projects/example-repo/.claude/worktrees/pr-7' }))).toBe(
      'Takes over ~/projects/example-repo/.claude/worktrees/pr-7',
    )
    expect(worktreeLine(plan({ action: 'resume' }))).toBe('Resumes ~/projects/.app/worktrees/example-repo/pr-7')
  })

  it('counts the uncommitted files in a worktree it reuses', () => {
    expect(worktreeLine(plan({ action: 'resume', dirty: 1 }))).toBe(
      'Resumes ~/projects/.app/worktrees/example-repo/pr-7 · 1 uncommitted file',
    )
    expect(worktreeLine(plan({ action: 'adopt', dirty: 3 }))).toBe(
      'Takes over ~/projects/.app/worktrees/example-repo/pr-7 · 3 uncommitted files',
    )
  })
})
