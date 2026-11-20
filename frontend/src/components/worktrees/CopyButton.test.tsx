import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { copiedMs } from '../../lib/useCopy'
import { platform } from '../../test/platform'
import { CopyButton } from './CopyButton'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

describe('CopyButton', () => {
  it('copies its text, says so for a moment, then offers to copy again', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    render(<CopyButton text="~/projects/example-repo" what="path" label="Copy path" />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy path' }))

    expect(platform.copyText).toHaveBeenCalledExactlyOnceWith('~/projects/example-repo')
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Copied to the clipboard')

    act(() => {
      vi.advanceTimersByTime(copiedMs)
    })

    expect(screen.getByRole('button', { name: 'Copy path' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toBeEmptyDOMElement()
    vi.useRealTimers()
  })

  it('says a copy the clipboard refused, and logs it', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    platform.copyText.mockRejectedValueOnce(new Error('denied'))
    render(<CopyButton text="feat/x" what="branch name" label="Copy" />)

    await userEvent.click(screen.getByRole('button', { name: 'Copy' }))

    expect(await screen.findByRole('button', { name: 'Copy failed' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Couldn’t copy the branch name')
    expect(error).toHaveBeenCalledWith('could not copy the branch name', expect.any(Error))
  })

  it('shows its icon alone, named for a screen reader, with its tooltip', async () => {
    render(
      <CopyButton text="feat/x" what="branch name" ariaLabel="Copy the branch name" title="Copy the branch name" />,
    )

    const button = screen.getByRole('button', { name: 'Copy the branch name' })
    expect(button).toHaveAttribute('title', 'Copy the branch name')
    expect(button).toHaveTextContent(/^$/)
    expect(button.querySelector('svg')).not.toBeNull()

    await userEvent.click(button)
    expect(await screen.findByRole('status')).toHaveTextContent('Copied to the clipboard')
    expect(button).toHaveTextContent(/^$/)
  })

  it('takes its own mark, classes, and can wait', () => {
    render(
      <CopyButton
        text="x"
        what="list"
        label="Copy list"
        icon={<span data-testid="mark" />}
        className="custom"
        disabled
      />,
    )

    const button = screen.getByRole('button', { name: 'Copy list' })
    expect(button).toBeDisabled()
    expect(button).toHaveClass('custom')
    expect(screen.getByTestId('mark')).toBeInTheDocument()
  })
})
