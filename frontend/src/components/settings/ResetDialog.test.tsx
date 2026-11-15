import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../../generated/brand'
import { platform } from '../../test/platform'
import { ResetDialog } from './ResetDialog'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

// dialog is the reset dialog.
function dialog() {
  return within(screen.getByRole('dialog', { name: `Reset ${brand.name}?` }))
}

describe('ResetDialog', () => {
  it('says what goes and what stays, and resets only once reset is typed', async () => {
    render(<ResetDialog onCancel={vi.fn()} />)

    const confirm = dialog().getByRole('button', { name: 'Reset and restart' })
    expect(confirm).toBeDisabled()
    expect(dialog().getByText(/Your prompts, launch defaults, and your own scripts/)).toBeInTheDocument()
    expect(dialog().getByText(/Triage, the cached inbox/)).toBeInTheDocument()

    await userEvent.type(dialog().getByRole('textbox', { name: 'Type reset to confirm' }), 'Reset')
    expect(confirm).toBeDisabled()

    await userEvent.clear(dialog().getByRole('textbox'))
    await userEvent.type(dialog().getByRole('textbox'), 'reset')
    expect(confirm).toBeEnabled()

    await userEvent.click(confirm)

    expect(platform.resetApp).toHaveBeenCalledWith('reset')
    expect(await dialog().findByRole('status')).toHaveTextContent(
      'Saved a backup to ~/data/backups/example before reset 20261115T120000Z.json. Restarting…',
    )
    expect(dialog().getByRole('button', { name: 'Restarting…' })).toBeDisabled()
    expect(dialog().getByRole('textbox')).toBeDisabled()
  })

  it('says it is saving while the backup is written', async () => {
    platform.resetApp.mockReturnValue(new Promise(() => undefined))
    render(<ResetDialog onCancel={vi.fn()} />)

    await userEvent.type(dialog().getByRole('textbox'), 'reset')
    await userEvent.click(dialog().getByRole('button', { name: 'Reset and restart' }))

    expect(await dialog().findByRole('status')).toHaveTextContent('Saving a backup of your setup…')
  })

  it('says why the reset was refused, and lets the user try again', async () => {
    platform.resetApp.mockRejectedValue(new Error('Runs are going in the app.'))
    render(<ResetDialog onCancel={vi.fn()} />)

    await userEvent.type(dialog().getByRole('textbox'), 'reset')
    await userEvent.click(dialog().getByRole('button', { name: 'Reset and restart' }))

    expect(await dialog().findByRole('alert')).toHaveTextContent('Runs are going in the app.')
    expect(dialog().getByRole('button', { name: 'Reset and restart' })).toBeEnabled()
  })

  it('cancels', async () => {
    const onCancel = vi.fn()
    render(<ResetDialog onCancel={onCancel} />)

    await userEvent.click(dialog().getByRole('button', { name: 'Cancel' }))

    expect(onCancel).toHaveBeenCalledOnce()
    expect(platform.resetApp).not.toHaveBeenCalled()
  })
})
