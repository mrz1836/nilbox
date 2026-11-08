import { act, fireEvent, render, screen } from '@testing-library/react'
import { userEvent } from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { platform } from '../test/platform'
import { CommandLine, copiedMs } from './CommandLine'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const command = 'gh auth refresh -s read:project'

describe('CommandLine', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows the command to run', () => {
    render(<CommandLine command={command} />)

    expect(screen.getByText(command)).toHaveClass('select-all')
    expect(screen.getByRole('button', { name: 'Copy the command' })).toHaveTextContent('Copy')
  })

  it('copies the command, and says so for a moment', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    render(<CommandLine command={command} />)

    fireEvent.click(screen.getByRole('button', { name: 'Copy the command' }))

    // The copy resolves on a microtask, which act waits for.
    await act(async () => {
      await Promise.resolve()
    })

    expect(platform.copyText).toHaveBeenCalledWith(command)
    expect(screen.getByRole('button', { name: 'Copy the command' })).toHaveTextContent('Copied')
    expect(screen.getByRole('status')).toHaveTextContent('Copied to the clipboard')

    act(() => {
      vi.advanceTimersByTime(copiedMs)
    })

    expect(screen.getByRole('button', { name: 'Copy the command' })).toHaveTextContent(/^Copy$/)
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
  })

  it('says when the clipboard refuses the command', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.copyText.mockRejectedValueOnce(new Error('no clipboard'))
    render(<CommandLine command={command} />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy the command' }))

    expect(screen.getByRole('button', { name: 'Copy the command' })).toHaveTextContent('Copy failed')
    expect(screen.getByRole('status')).toHaveTextContent('Select the command and copy it by hand')
  })
})
