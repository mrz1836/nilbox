import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { agentGroup, agents, launchHold, runningHold } from '../test/fixtures'
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

  it('names the folder of an agent working in the clone, with nothing to offer but Close', () => {
    const base = runningHold()
    const hold = {
      ...base,
      worktree: '',
      dir: '~/projects/example-repo',
      running: base.running.map((r) => ({ ...r, background: true })),
    }

    render(<LaunchHoldDialog hold={hold} agentName="Claude" agents={agents()} onConfirm={vi.fn()} onCancel={vi.fn()} />)

    const dialog = within(screen.getByRole('dialog', { name: 'Autopilot’s agent is working here' }))
    expect(dialog.getByText(/is working in ~\/projects\/example-repo in the background/)).toBeInTheDocument()
    expect(dialog.queryByRole('button', { name: 'Watch it' })).not.toBeInTheDocument()
    expect(dialog.queryByRole('button', { name: 'Take over' })).not.toBeInTheDocument()
  })

  it('offers Watch it and Take over for an agent working in the background, never Start another', async () => {
    const base = runningHold()
    const hold = {
      ...base,
      running: base.running.map((r) => ({
        ...r,
        background: true,
        canFocus: false,
        doing: 'is working in the background',
      })),
    }
    const sessionId = hold.running[0]?.sessionId
    const onWatch = vi.fn()
    const onTakeOver = vi.fn()
    const onConfirm = vi.fn()
    const onCancel = vi.fn()

    render(
      <LaunchHoldDialog
        hold={hold}
        agentName="Claude"
        agents={agents()}
        onConfirm={onConfirm}
        onCancel={onCancel}
        onWatch={onWatch}
        onTakeOver={onTakeOver}
      />,
    )

    const dialog = within(screen.getByRole('dialog', { name: 'Autopilot’s agent is working here' }))
    expect(dialog.getByText(/Claude is working in pr-7 in the background, for Autopilot/)).toBeInTheDocument()
    expect(dialog.queryByRole('button', { name: 'Start another' })).not.toBeInTheDocument()

    await userEvent.click(dialog.getByRole('button', { name: 'Watch it' }))
    expect(onWatch).toHaveBeenCalledExactlyOnceWith(sessionId)

    await userEvent.click(dialog.getByRole('button', { name: 'Take over' }))
    expect(onTakeOver).toHaveBeenCalledExactlyOnceWith(sessionId)

    await userEvent.click(dialog.getByRole('button', { name: 'Close' }))
    expect(onCancel).toHaveBeenCalledOnce()
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
