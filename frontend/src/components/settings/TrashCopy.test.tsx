import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { Result } from '../../lib/call'
import { toolCopy } from '../../test/fixtures'
import { platform } from '../../test/platform'
import { TrashCopy } from './TrashCopy'

vi.mock('../../platform', () => import('../../test/platform').then((fake) => fake.platform))

const path = '/Users/example/.local/bin/magex'
const refused = 'Finder couldn’t move ~/.local/bin/magex to the Trash: the disk is locked'

// Trash is how the card moves a copy to the Trash.
type Trash = (path: string) => Promise<Result<unknown>>

// trashing is a Trash that ends as outcome.
function trashing(outcome: Result<unknown>) {
  return vi.fn<Trash>(() => Promise.resolve(outcome))
}

// renderTrash renders the button for magex's copy in ~/.local/bin, moving it with trash.
function renderTrash(trash = trashing({ ok: true, data: undefined })) {
  render(<TrashCopy toolId="mage-x" copy={toolCopy()} label="Move to the Trash" disabled={false} trash={trash} />)

  return { trash }
}

// confirmation is the dialog that asks before the copy is moved.
function confirmation() {
  return screen.getByRole('dialog', { name: 'Move ~/.local/bin/magex to the Trash?' })
}

describe('TrashCopy', () => {
  it('asks first, and moves nothing when cancelled', async () => {
    const { trash } = renderTrash()

    await userEvent.click(screen.getByRole('button', { name: 'Move to the Trash' }))

    expect(confirmation()).toHaveAccessibleDescription('Finder’s Put Back can restore it.')

    await userEvent.click(within(confirmation()).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trash).not.toHaveBeenCalled()
  })

  it('moves the copy once the user confirms', async () => {
    const { trash } = renderTrash()

    await userEvent.click(screen.getByRole('button', { name: 'Move to the Trash' }))
    await userEvent.click(within(confirmation()).getByRole('button', { name: 'Move to the Trash' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(trash).toHaveBeenCalledExactlyOnceWith(path)
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Reveal in Finder' })).not.toBeInTheDocument()
  })

  it('says why Finder couldn’t, and offers to show the copy in Finder', async () => {
    const trash = trashing({ ok: false, error: refused })
    renderTrash(trash)

    await userEvent.click(screen.getByRole('button', { name: 'Move to the Trash' }))
    await userEvent.click(within(confirmation()).getByRole('button', { name: 'Move to the Trash' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(refused)

    await userEvent.click(screen.getByRole('button', { name: 'Reveal in Finder' }))

    expect(platform.revealToolCopy).toHaveBeenCalledExactlyOnceWith('mage-x', path)
    expect(screen.getAllByRole('alert')).toHaveLength(1)

    // Trying again clears what the last try said.
    trash.mockResolvedValue({ ok: true, data: undefined })
    await userEvent.click(screen.getByRole('button', { name: 'Move to the Trash' }))
    await userEvent.click(within(confirmation()).getByRole('button', { name: 'Move to the Trash' }))

    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Reveal in Finder' })).not.toBeInTheDocument()
  })

  it('says why Finder couldn’t show the copy either', async () => {
    platform.revealToolCopy.mockRejectedValue(new Error('Finder couldn’t show ~/.local/bin/magex: no such file'))
    renderTrash(trashing({ ok: false, error: refused }))

    await userEvent.click(screen.getByRole('button', { name: 'Move to the Trash' }))
    await userEvent.click(within(confirmation()).getByRole('button', { name: 'Move to the Trash' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Reveal in Finder' }))

    const alerts = await screen.findAllByRole('alert')
    expect(alerts.map((a) => a.textContent)).toEqual([refused, 'Finder couldn’t show ~/.local/bin/magex: no such file'])
  })

  it('holds the button while another change runs', () => {
    render(
      <TrashCopy
        toolId="mage-x"
        copy={toolCopy()}
        label="Move it to the Trash"
        disabled
        trash={() => Promise.resolve({ ok: true, data: undefined })}
      />,
    )

    expect(screen.getByRole('button', { name: 'Move it to the Trash' })).toBeDisabled()
  })
})
