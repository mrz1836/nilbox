import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { agentGroup, launchHold } from '../test/fixtures'
import { LaunchHoldDialog } from './LaunchHoldDialog'

const platform = vi.hoisted(() => ({ openExternal: vi.fn() }))

vi.mock('../platform', () => platform)

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
})
