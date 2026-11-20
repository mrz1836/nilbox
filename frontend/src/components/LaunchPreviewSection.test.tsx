import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { WorktreePlan } from '../platform'
import { PlannedLaunch, worktreeLine, type Plan } from './LaunchPreviewSection'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

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

describe('PlannedLaunch', () => {
  // planned is a launch planned in ~/projects/example-repo, opening in terminal.
  function planned(terminal: string): Plan {
    return {
      prompt: 'Review the pull request',
      worktree: null,
      dir: '~/projects/example-repo',
      permission: 'plan mode',
      env: [],
      command: 'claude',
      terminal,
    }
  }

  it('names the terminal a launch opens in by its app, or as the plan names one it doesn’t know', () => {
    const { rerender } = render(<PlannedLaunch plan={planned('iterm2')} />)

    expect(screen.getByText(/plan mode · opens in iTerm2$/)).toBeInTheDocument()

    rerender(<PlannedLaunch plan={planned('ghostty')} />)
    expect(screen.getByText(/plan mode · opens in ghostty$/)).toBeInTheDocument()

    rerender(<PlannedLaunch plan={planned('')} />)
    expect(screen.queryByText(/opens in/)).not.toBeInTheDocument()
  })
})
