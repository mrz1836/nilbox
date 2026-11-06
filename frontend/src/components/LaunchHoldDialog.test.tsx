import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { agentGroup, launchHold } from '../test/fixtures'
import { LaunchHoldDialog } from './LaunchHoldDialog'

const platform = vi.hoisted(() => ({ openInBrowser: vi.fn() }))

vi.mock('../platform', () => platform)

beforeEach(() => {
  vi.clearAllMocks()
})

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

  it('lists a file it cannot link without a link, and says so when a link fails to open', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.openInBrowser.mockRejectedValue(new Error('no browser'))

    const files = [
      ...(agentGroup().files ?? []),
      { path: 'CLAUDE.md', previousPath: '', change: 'modified', pattern: 'CLAUDE.md', url: '' },
    ]
    render(<LaunchHoldDialog hold={launchHold({ files })} agentName="Claude" onConfirm={vi.fn()} onCancel={vi.fn()} />)

    const list = within(screen.getByRole('list', { name: 'Agent configuration it changes' }))
    expect(list.getByRole('button', { name: 'CLAUDE.md' })).toBeDisabled()

    await userEvent.click(list.getByRole('button', { name: /\.claude\/settings\.json/ }))

    await waitFor(() => {
      expect(logged).toHaveBeenCalledWith('could not open the file in the browser', expect.any(Error))
    })
    logged.mockRestore()
  })
})
