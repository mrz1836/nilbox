import { render, screen } from '@testing-library/react'
import { userEvent } from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ProjectsTip, type ProjectsTipProps } from './ProjectsTip'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

function renderTip(props: Partial<ProjectsTipProps> = {}) {
  const handlers = { onCheck: vi.fn(), onClose: vi.fn() }
  const view = render(<ProjectsTip on={false} refreshing={false} {...handlers} {...props} />)

  return { ...view, ...handlers }
}

describe('ProjectsTip', () => {
  it('offers to show issue projects, with the command that grants the access', () => {
    renderTip()

    expect(screen.getByRole('region', { name: 'See where your issues stand in their projects' })).toBeInTheDocument()
    expect(screen.getByText('gh auth refresh -s read:project')).toBeInTheDocument()
    expect(screen.getByText(/needs read-only access to your projects/)).toBeInTheDocument()
  })

  it('checks again, says it is checking, and says when there is still no access', async () => {
    const { onCheck, rerender } = renderTip()

    await userEvent.click(screen.getByRole('button', { name: 'Check again' }))
    expect(onCheck).toHaveBeenCalledOnce()

    rerender(<ProjectsTip on={false} refreshing onCheck={onCheck} onClose={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Checking…' })).toBeDisabled()
    expect(screen.queryByText(/Still no access/)).not.toBeInTheDocument()

    rerender(<ProjectsTip on={false} refreshing={false} onCheck={onCheck} onClose={vi.fn()} />)
    expect(screen.getByText(/Still no access to your projects/).closest('[role="status"]')).toBeInTheDocument()
  })

  it('says nothing about a refresh the user did not ask for', () => {
    const { rerender, onCheck, onClose } = renderTip({ refreshing: true })

    expect(screen.getByRole('button', { name: 'Check again' })).toBeDisabled()

    rerender(<ProjectsTip on={false} refreshing={false} onCheck={onCheck} onClose={onClose} />)
    expect(screen.queryByText(/Still no access/)).not.toBeInTheDocument()
  })

  it('puts the offer away for good', async () => {
    const { onClose } = renderTip()

    const close = screen.getByRole('button', { name: 'Don’t show this again' })
    expect(close).toHaveAttribute('title', expect.stringContaining('Settings → Account'))

    await userEvent.click(close)
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('says the projects are on once a refresh finds access', async () => {
    const { onClose } = renderTip({ on: true })

    expect(screen.getByRole('region', { name: 'Projects are on.' })).toBeInTheDocument()
    expect(screen.queryByText('gh auth refresh -s read:project')).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledOnce()
  })
})
