import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import { offerOf } from '../lib/update'
import type { MoveDialogState } from '../lib/useMove'
import { useMoveToApplications } from '../lib/useMove'
import type { MoveResult, UpdateLocationKind, UpdateStatus } from '../platform'
import { moveResult, updateLocation, updateStatus } from '../test/fixtures'
import { platform } from '../test/platform'
import { moveOf } from '../test/query'
import { MoveDialog } from './MoveDialog'

vi.mock('../platform', () => import('../test/platform').then((fake) => fake.platform))

const asks = `${brand.name} can only update itself from your Applications folder. It will reopen from there.`
const stays = 'The copy you opened stays where it is, and you can delete it.'

// shown renders the dialog as a move at dialog leaves it, for an app running from kind, and
// returns the move it was given, with its moves as mocks.
function shown(dialog: Partial<MoveDialogState> = {}, kind: UpdateLocationKind = 'elsewhere') {
  const status = updateStatus({ location: updateLocation(kind) })
  const move = moveOf(status, { dialog: { variant: 'move', work: null, leaving: false, error: '', ...dialog } })

  render(<MoveDialog move={move} />)

  return move
}

// Live is the dialog over the real move, for the status given, offered as the update's
// offer (offerOf) says.
function Live({ status }: { status: UpdateStatus }) {
  return <MoveDialog move={useMoveToApplications(offerOf(status, { step: 'idle' }), status.location)} />
}

describe('MoveDialog', () => {
  it('asks to move the app into Applications: Move to Applications · Not now, starting on Not now', async () => {
    const move = shown()
    const dialog = screen.getByRole('dialog', { name: 'Move to Applications?' })

    expect(dialog).toHaveAccessibleDescription(expect.stringContaining(asks))
    expect(dialog).toHaveTextContent(stays)
    expect(screen.getByRole('button', { name: 'Not now' })).toHaveFocus()

    await userEvent.click(screen.getByRole('button', { name: 'Move to Applications' }))
    expect(move.confirm).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Not now' }))
    expect(move.dismiss).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Show in Finder' }))
    expect(move.reveal).toHaveBeenCalledOnce()
  })

  it('says the copy the user opened stays, from a disk image and Downloads with Show in Finder, translocated without', () => {
    const cases: [UpdateLocationKind, boolean][] = [
      ['diskImage', true],
      ['elsewhere', true],
      ['translocated', false],
    ]

    for (const [kind, finder] of cases) {
      const { unmount } = render(
        <MoveDialog
          move={moveOf(updateStatus({ location: updateLocation(kind) }), {
            dialog: { variant: 'move', work: null, leaving: false, error: '' },
          })}
        />,
      )

      expect(screen.getByRole('dialog')).toHaveTextContent(stays)
      expect({ kind, finder: screen.queryByRole('button', { name: 'Show in Finder' }) !== null }).toEqual({
        kind,
        finder,
      })
      unmount()
    }
  })

  it('offers to open a copy already in Applications: Open it · Not now', async () => {
    const move = shown({ variant: 'exists' })
    const dialog = screen.getByRole('dialog', { name: `Open ${brand.name} from Applications?` })

    expect(dialog).toHaveTextContent(`${brand.name} is already in Applications.`)
    expect(dialog).not.toHaveTextContent(asks)

    await userEvent.click(screen.getByRole('button', { name: 'Open it' }))
    expect(move.confirm).toHaveBeenCalledOnce()
  })

  it('says what it is doing while it works, and lets no button or Escape interrupt it', async () => {
    const steps: [Partial<MoveDialogState>, string][] = [
      [{ work: 'moving' }, 'Moving to Applications…'],
      [{ work: 'moving', leaving: true }, 'Reopening from Applications…'],
      [{ variant: 'exists', work: 'opening' }, 'Opening it…'],
      [{ variant: 'exists', work: 'opening', leaving: true }, 'Opening it…'],
    ]

    for (const [dialog, words] of steps) {
      const status = updateStatus({ location: updateLocation() })
      const move = moveOf(status, { dialog: { variant: 'move', work: null, leaving: false, error: '', ...dialog } })
      const { unmount } = render(<MoveDialog move={move} />)

      const working = screen.getByRole('button', { name: words })
      expect(working).toBeDisabled()
      expect(working).toHaveAttribute('aria-busy', 'true')
      expect(screen.getByRole('button', { name: 'Not now' })).toBeDisabled()
      expect(within(screen.getByRole('dialog')).getByRole('status')).toHaveTextContent(words)

      await userEvent.keyboard('{Escape}')
      expect(move.dismiss).not.toHaveBeenCalled()
      unmount()
    }
  })

  it('shows a failure’s words', () => {
    shown({ error: '/Applications already has a link named Example.app, so nothing was moved.' })

    expect(screen.getByRole('alert')).toHaveTextContent(
      '/Applications already has a link named Example.app, so nothing was moved.',
    )
    expect(screen.getByRole('button', { name: 'Move to Applications' })).toBeEnabled()
  })

  it('closes on Escape while it waits for the user', async () => {
    const move = shown()

    await userEvent.keyboard('{Escape}')
    expect(move.dismiss).toHaveBeenCalledOnce()
  })

  it('shows nothing while closed, or before the status loads', () => {
    const { container } = render(<MoveDialog move={moveOf(updateStatus({ location: updateLocation() }))} />)
    expect(container).toBeEmptyDOMElement()

    render(<MoveDialog move={moveOf(null, { dialog: { variant: 'move', work: null, leaving: false, error: '' } })} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('moves, working, then offers the copy in Applications, and opens it, over the real move', async () => {
    const gate: { answer?: (result: MoveResult) => void } = {}
    platform.moveToApplications.mockReturnValue(
      new Promise((resolve) => {
        gate.answer = resolve
      }),
    )
    render(<Live status={updateStatus({ flavor: 'rehearsal', location: updateLocation('diskImage') })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Move to Applications' }))

    expect(screen.getByRole('button', { name: 'Moving to Applications…' })).toBeDisabled()
    await userEvent.dblClick(screen.getByRole('button', { name: 'Moving to Applications…' }))
    expect(platform.moveToApplications).toHaveBeenCalledOnce()

    gate.answer?.(moveResult({ outcome: 'exists' }))

    await userEvent.click(await screen.findByRole('button', { name: 'Open it' }))
    expect(platform.openInApplications).toHaveBeenCalledOnce()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Opening it…' })).toBeDisabled()
    })
  })

  it('goes away for this launch on Not now, over the real move', async () => {
    render(<Live status={updateStatus({ location: updateLocation('translocated') })} />)

    await userEvent.click(screen.getByRole('button', { name: 'Not now' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(platform.moveToApplications).not.toHaveBeenCalled()
  })
})
