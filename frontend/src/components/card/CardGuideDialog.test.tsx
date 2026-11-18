import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { at } from '../../test/clock'
import { CardGuideDialog } from './CardGuideDialog'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// showDialog opens the dialog, with focus on a button outside it, as the inbox's opens it.
function showDialog() {
  const onClose = vi.fn()

  render(<CardGuideDialog onClose={onClose} />, { wrapper: at() })

  return onClose
}

describe('CardGuideDialog', () => {
  it('shows the full guide, titled, with focus on Close', () => {
    showDialog()

    const dialog = screen.getByRole('dialog', { name: 'How to read a card' })

    expect(dialog).toHaveAttribute('open')
    expect(within(dialog).getByRole('button', { name: 'Close' })).toHaveFocus()
    expect(dialog.querySelector('[inert] li[data-item-id]')).not.toBeNull()
    expect(dialog.querySelectorAll('[data-marker]')).toHaveLength(8)
    expect(within(dialog).getByRole('list', { name: 'The parts of a card' })).toBeInTheDocument()
    expect(within(dialog).getByRole('list', { name: 'Whose move' })).toBeInTheDocument()
  })

  it('closes with Close', async () => {
    const onClose = showDialog()

    await userEvent.click(screen.getByRole('button', { name: 'Close' }))

    expect(onClose).toHaveBeenCalledOnce()
  })

  it('closes with Escape', async () => {
    const onClose = showDialog()

    await userEvent.keyboard('{Escape}')

    expect(onClose).toHaveBeenCalledOnce()
  })

  it('keeps Tab on Close, past the sample card’s controls, which take no focus', async () => {
    showDialog()

    const close = screen.getByRole('button', { name: 'Close' })

    await userEvent.tab()
    expect(close).toHaveFocus()

    await userEvent.tab({ shift: true })
    expect(close).toHaveFocus()
  })
})
