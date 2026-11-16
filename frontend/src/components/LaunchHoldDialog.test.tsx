import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { agentGroup, launchHold } from '../test/fixtures'
import { platform } from '../test/platform'
import { LaunchHoldDialog } from './LaunchHoldDialog'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

describe('LaunchHoldDialog', () => {
  it('confirms or cancels', async () => {
    const onConfirm = vi.fn()
    const onCancel = vi.fn()
    render(<LaunchHoldDialog hold={launchHold()} agentName="Claude" onConfirm={onConfirm} onCancel={onCancel} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open anyway' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(onConfirm).toHaveBeenCalledOnce()
    expect(onCancel).toHaveBeenCalledOnce()
  })

  it('opens each file’s diff, and lists a file it cannot link without a link', async () => {
    const files = [
      ...agentGroup().files,
      { path: 'CLAUDE.md', previousPath: '', change: 'modified', pattern: 'CLAUDE.md', url: '' },
    ]
    render(<LaunchHoldDialog hold={launchHold({ files })} agentName="Claude" onConfirm={vi.fn()} onCancel={vi.fn()} />)

    const list = within(screen.getByRole('list', { name: 'Agent configuration it changes' }))
    expect(list.getByRole('button', { name: 'CLAUDE.md' })).toBeDisabled()

    await userEvent.click(list.getByRole('button', { name: /\.claude\/settings\.json/ }))
    expect(platform.openExternal).toHaveBeenCalledWith(
      'https://github.com/acme-org/example-repo/pull/7/files#diff-aaaa',
    )
  })

  it('explains a pull request whose files couldn’t all be checked', () => {
    render(
      <LaunchHoldDialog
        hold={launchHold({ reason: 'unchecked', files: [], checked: 3, total: 6 })}
        agentName="Claude"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    const dialog = within(screen.getByRole('dialog', { name: 'Its files couldn’t all be checked' }))
    expect(dialog.getByText(/GitHub listed only 3 of its 6 changed files/)).toBeInTheDocument()
    expect(dialog.getByText(/open the session only if they’re safe/)).toBeInTheDocument()
  })

  it('explains a worktree with uncommitted changes', () => {
    render(
      <LaunchHoldDialog
        hold={launchHold({ reason: 'dirty', files: [], worktree: 'pr-88', dirty: 3 })}
        agentName="Codex"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    const dialog = within(screen.getByRole('dialog', { name: 'Its worktree has uncommitted changes' }))
    expect(dialog.getByText(/pr-88 has 3 uncommitted files/)).toBeInTheDocument()
    expect(dialog.getByText(/Codex would work on top of them/)).toBeInTheDocument()
  })
})
