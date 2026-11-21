import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { noChoices, type Choices } from '../lib/launchChoices'
import type { WorktreePlan } from '../platform'
import { launchPreview, repoLaunchPreview } from '../test/fixtures'
import { LaunchPreviewSection, PlannedLaunch, worktreeLine, type Plan } from './LaunchPreviewSection'

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

describe('LaunchPreviewSection', () => {
  // planner plans a launch through the subject's own preview, its prompt naming the
  // profile it was planned with.
  function planner() {
    return vi.fn((c: Choices) =>
      Promise.resolve(launchPreview({ prompt: `Planned as ${c.profile === '' ? 'default' : c.profile}\n` })),
    )
  }

  // planned lets the preview's wait for the choices to rest run out, and its plan land.
  async function planned() {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(120)
    })
  }

  it('plans a repository’s launch on request through its subject, its button named as the launch names it', async () => {
    const preview = vi.fn(() => Promise.resolve(repoLaunchPreview()))
    const choices = { ...noChoices, promptId: 21 }
    render(
      <LaunchPreviewSection
        subject={{ key: '["acme-org/api-server","~/projects/api-server"]', preview }}
        choices={choices}
        label="Preview the launch"
      />,
    )

    expect(preview).not.toHaveBeenCalled()

    await userEvent.click(screen.getByRole('button', { name: 'Preview the launch' }))

    expect(await screen.findByText(/Upgrade the Go and JavaScript dependencies/)).toBeInTheDocument()
    expect(screen.getByText(/^Creates .*deps-20260615 from origin\/master$/)).toBeInTheDocument()
    expect(preview).toHaveBeenCalledExactlyOnceWith(choices)
  })

  it('plans again for other choices or another subject, never for the same choices made again', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const preview = planner()
    const section = (key: string, choices: Choices) => (
      <LaunchPreviewSection subject={{ key, preview }} choices={choices} />
    )
    const { rerender } = render(section('pr-7', noChoices))

    fireEvent.click(screen.getByRole('button', { name: 'Preview' }))
    await planned()
    expect(screen.getByText('Planned as default')).toBeInTheDocument()

    rerender(section('pr-7', { ...noChoices }))
    await planned()
    expect(preview).toHaveBeenCalledOnce()

    rerender(section('pr-7', { ...noChoices, profile: 'work' }))
    await planned()
    expect(screen.getByText('Planned as work')).toBeInTheDocument()

    rerender(section('pr-8', { ...noChoices, profile: 'work' }))
    await planned()
    expect(preview).toHaveBeenCalledTimes(3)
  })
})
