import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { BranchRules, Settings } from '../../platform'
import { settings } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { WorktreesSection } from './WorktreesSection'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// rules are the app's default branch rules, with any changes.
function rules(overrides: Partial<BranchRules> = {}): BranchRules {
  return { own: true, pullRequest: true, merged: true, protected: ['main', 'master'], ...overrides }
}

function renderSection(current: Settings = settings()) {
  const onSaved = vi.fn()

  render(<WorktreesSection settings={current} onSaved={onSaved} />)

  return { onSaved }
}

function protectedList() {
  return within(screen.getByRole('list', { name: 'Protected branches' }))
}

beforeEach(() => {
  // Each change comes back with the settings as stored.
  platform.setBranchRules.mockImplementation((branches) =>
    Promise.resolve(settings({ branches: { ...branches, protected: branches.protected ?? [] } })),
  )
})

describe('WorktreesSection', () => {
  it('shows each branch rule and the protected branches, marking the defaults', () => {
    renderSection(settings({ branches: rules({ pullRequest: false, protected: ['main', 'master', 'release/*'] }) }))

    expect(screen.getByRole('switch', { name: 'Clean up after merging' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'Delete the worktree’s own branch' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'Delete the pull request’s branch' })).not.toBeChecked()
    expect(screen.getByRole('switch', { name: 'Tick merged branches' })).toHaveAccessibleDescription(
      /In Clean up merged branches…, on the Worktrees page/,
    )
    expect(
      protectedList()
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['maindefault', 'masterdefault', 'release/*'])
    expect(screen.getByRole('button', { name: 'Restore the defaults' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Branch to protect' })).toHaveAccessibleDescription(
      /release\/\* protects release\/1\.2 but not release\/1\.2\/fix/,
    )
  })

  it('offers no restore while the protected branches are the defaults', () => {
    renderSection()

    expect(screen.queryByRole('button', { name: 'Restore the defaults' })).not.toBeInTheDocument()
  })

  it('stores every rule with each switch, and shows a refusal under them', async () => {
    const { onSaved } = renderSection()

    await userEvent.click(screen.getByRole('switch', { name: 'Tick merged branches' }))

    expect(platform.setBranchRules).toHaveBeenCalledWith(rules({ merged: false }))
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings({ branches: rules({ merged: false }) }))
    })

    platform.setBranchRules.mockRejectedValueOnce(new Error('database is locked'))
    await userEvent.click(screen.getByRole('switch', { name: 'Delete the worktree’s own branch' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    expect(screen.getByRole('switch', { name: 'Delete the worktree’s own branch' })).toHaveAccessibleDescription(
      /made for it\. .* database is locked$/,
    )
    expect(screen.getByRole('switch', { name: 'Delete the worktree’s own branch' })).toBeChecked()
  })

  it('protects another branch pattern', async () => {
    const { onSaved } = renderSection()

    await userEvent.type(screen.getByRole('textbox', { name: 'Branch to protect' }), ' release/* ')
    await userEvent.click(screen.getByRole('button', { name: 'Protect branch' }))

    expect(platform.setBranchRules).toHaveBeenCalledWith(rules({ protected: ['main', 'master', 'release/*'] }))
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(
        settings({ branches: rules({ protected: ['main', 'master', 'release/*'] }) }),
      )
    })
    expect(screen.getByRole('textbox', { name: 'Branch to protect' })).toHaveValue('')
  })

  it('says when a branch is already protected, or the pattern is refused', async () => {
    platform.setBranchRules.mockRejectedValue(new Error('not a branch pattern: "two words" holds a space'))
    renderSection()

    const input = screen.getByRole('textbox', { name: 'Branch to protect' })

    await userEvent.type(input, 'main')
    await userEvent.click(screen.getByRole('button', { name: 'Protect branch' }))

    expect(screen.getByRole('alert')).toHaveTextContent('main is already protected.')
    expect(platform.setBranchRules).not.toHaveBeenCalled()

    await userEvent.clear(input)
    await userEvent.type(input, 'two words')
    await userEvent.click(screen.getByRole('button', { name: 'Protect branch' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('not a branch pattern')
    expect(input).toHaveAttribute('aria-invalid', 'true')

    // Typing again clears the refusal.
    await userEvent.type(input, '!')
    expect(input).toHaveAttribute('aria-invalid', 'false')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('stops protecting a branch', async () => {
    const { onSaved } = renderSection(settings({ branches: rules({ protected: ['main', 'master', 'develop'] }) }))

    await userEvent.click(protectedList().getByRole('button', { name: 'Stop protecting develop' }))

    expect(platform.setBranchRules).toHaveBeenCalledWith(rules())
    await waitFor(() => {
      expect(onSaved).toHaveBeenCalledWith(settings())
    })
  })

  it('restores the default protected branches', async () => {
    renderSection(settings({ branches: rules({ merged: false, protected: ['develop'] }) }))

    await userEvent.click(screen.getByRole('button', { name: 'Restore the defaults' }))

    expect(platform.setBranchRules).toHaveBeenCalledWith(rules({ merged: false }))
  })

  it('explains an empty list', () => {
    renderSection(settings({ branches: rules({ protected: [] }) }))

    expect(screen.getByText(/No branch is protected but each repository’s default branch/)).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Protected branches' })).not.toBeInTheDocument()
  })
})
