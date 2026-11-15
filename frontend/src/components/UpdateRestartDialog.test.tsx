import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { brand } from '../generated/brand'
import type { RestartQuestion } from '../lib/update'
import { runGoing } from '../test/fixtures'
import { UpdateRestartDialog } from './UpdateRestartDialog'

// asking renders the dialog asking question, with its answers as mocks.
function asking(question: RestartQuestion) {
  const onConfirm = vi.fn()
  const onCancel = vi.fn()

  render(<UpdateRestartDialog flow={{ step: 'asking', question }} onConfirm={onConfirm} onCancel={onCancel} />)

  return { onConfirm, onCancel, dialog: screen.getByRole('dialog', { name: 'Restart to update?' }) }
}

describe('UpdateRestartDialog', () => {
  it('asks nothing while the restart asks nothing', () => {
    for (const step of ['idle', 'preparing', 'downloading', 'restarting'] as const) {
      const { unmount } = render(<UpdateRestartDialog flow={{ step }} onConfirm={vi.fn()} onCancel={vi.fn()} />)

      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
      unmount()
    }
  })

  it('asks about the text not sent, starting on Not now', () => {
    const { dialog } = asking({ runs: [], sheets: 2 })

    expect(dialog).toHaveTextContent('Restarting loses the text you haven’t sent in 2 sheets.')
    expect(within(dialog).getByRole('button', { name: 'Not now' })).toHaveFocus()
  })

  it('asks about a run going, and what stopping a release can leave', () => {
    const { dialog } = asking({ runs: [runGoing()], sheets: 0 })

    expect(within(dialog).getByText('Restarting stops Release v1.2.0 in acme-org/sdk.')).toBeInTheDocument()
    expect(
      within(dialog).getByText('Stopping it now can leave a tag on this Mac but not on GitHub.'),
    ).toBeInTheDocument()
    expect(within(dialog).queryByRole('list')).not.toBeInTheDocument()
  })

  it('asks about both in one question, listing several runs', () => {
    const pull = runGoing({ id: 8, kind: 'gitPull', title: 'Pull main', repo: 'acme-org/api' })
    const { dialog } = asking({ runs: [runGoing(), pull], sheets: 1 })
    const items = within(dialog).getAllByRole('listitem')

    expect(dialog).toHaveTextContent('Restarting stops 2 runs and loses the text you haven’t sent in a sheet.')
    expect(items.map((li) => li.textContent)).toEqual(['Release v1.2.0 in acme-org/sdk', 'Pull main in acme-org/api'])
  })

  it('asks about the runs as a whole when they couldn’t be listed', () => {
    const { dialog } = asking({ runs: null, sheets: 0 })

    expect(dialog).toHaveTextContent(`Restarting stops the runs going in ${brand.name}.`)
  })

  it('restarts on Restart to update, and leaves everything on Not now', async () => {
    const { onConfirm, onCancel } = asking({ runs: [runGoing()], sheets: 1 })

    await userEvent.click(screen.getByRole('button', { name: 'Restart to update' }))
    expect(onConfirm).toHaveBeenCalledOnce()

    await userEvent.click(screen.getByRole('button', { name: 'Not now' }))
    expect(onCancel).toHaveBeenCalledOnce()
  })
})
