import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { ConfirmDialog } from './ConfirmDialog'

// Harness opens the dialog from a button, the way the views do.
function Harness({ onConfirm }: { onConfirm: () => void }) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <button
        type="button"
        onClick={() => {
          setOpen(true)
        }}
      >
        Open
      </button>
      {open && (
        <ConfirmDialog
          title="Remove it?"
          confirmLabel="Remove"
          onConfirm={() => {
            onConfirm()
            setOpen(false)
          }}
          onCancel={() => {
            setOpen(false)
          }}
        >
          This cannot be undone.
        </ConfirmDialog>
      )}
    </>
  )
}

describe('ConfirmDialog', () => {
  it('is a labelled modal dialog that describes the action', () => {
    render(
      <ConfirmDialog title="Remove it?" confirmLabel="Remove" onConfirm={vi.fn()} onCancel={vi.fn()}>
        This cannot be undone.
      </ConfirmDialog>,
    )

    const dialog = screen.getByRole('dialog', { name: 'Remove it?' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAccessibleDescription('This cannot be undone.')
  })

  it('starts on Cancel and returns focus to the opener when it closes', async () => {
    render(<Harness onConfirm={vi.fn()} />)
    const opener = screen.getByRole('button', { name: 'Open' })

    await userEvent.click(opener)

    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(opener).toHaveFocus()
  })

  it('cancels on Escape without confirming', async () => {
    const onConfirm = vi.fn()
    render(<Harness onConfirm={onConfirm} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open' }))
    await userEvent.keyboard('{Escape}')

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('confirms through its confirm button', async () => {
    const onConfirm = vi.fn()
    render(<Harness onConfirm={onConfirm} />)

    await userEvent.click(screen.getByRole('button', { name: 'Open' }))
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))

    expect(onConfirm).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('keeps Tab inside the dialog', async () => {
    render(<Harness onConfirm={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: 'Open' }))

    const cancel = screen.getByRole('button', { name: 'Cancel' })
    const remove = screen.getByRole('button', { name: 'Remove' })

    await userEvent.tab()
    expect(remove).toHaveFocus()

    await userEvent.tab()
    expect(cancel).toHaveFocus()

    await userEvent.tab({ shift: true })
    expect(remove).toHaveFocus()

    await userEvent.tab({ shift: true })
    expect(cancel).toHaveFocus()
  })

  it('pulls focus back in when it has wandered outside', async () => {
    render(<Harness onConfirm={vi.fn()} />)
    const opener = screen.getByRole('button', { name: 'Open' })
    await userEvent.click(opener)

    opener.focus()
    await userEvent.tab()

    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()

    opener.focus()
    await userEvent.tab({ shift: true })

    expect(screen.getByRole('button', { name: 'Remove' })).toHaveFocus()
  })
})
