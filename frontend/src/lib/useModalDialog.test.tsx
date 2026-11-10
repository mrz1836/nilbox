import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { isTopmost, nextFocus, useModalDialog } from './useModalDialog'

// Modal is a dialog run by the hook, with two buttons and, when nested is set, a second
// dialog opened from inside it.
function Modal({ onCancel, nested = false }: { onCancel: () => void; nested?: boolean }) {
  const ref = useRef<HTMLDialogElement>(null)
  const firstRef = useRef<HTMLButtonElement>(null)
  const cancel = useModalDialog(ref, onCancel, () => firstRef.current)
  const [inner, setInner] = useState(nested)

  return (
    <dialog ref={ref} aria-label="Outer" onCancel={cancel}>
      <button ref={firstRef} type="button">
        First
      </button>
      <button type="button">Last</button>
      {inner && (
        <Inner
          onCancel={() => {
            setInner(false)
          }}
        />
      )}
    </dialog>
  )
}

function Inner({ onCancel }: { onCancel: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  const cancel = useModalDialog(ref, onCancel)

  return (
    <dialog ref={ref} aria-label="Inner" onCancel={cancel}>
      <button type="button">Inside</button>
    </dialog>
  )
}

describe('useModalDialog', () => {
  it('opens the dialog, focuses where asked, and cancels on Escape', async () => {
    const onCancel = vi.fn()
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()

    const { unmount } = render(<Modal onCancel={onCancel} />)

    expect(screen.getByRole('dialog', { name: 'Outer' })).toHaveAttribute('open')
    expect(screen.getByRole('button', { name: 'First' })).toHaveFocus()

    await userEvent.keyboard('{Escape}')
    expect(onCancel).toHaveBeenCalledOnce()

    unmount()
    expect(opener).toHaveFocus()
    opener.remove()
  })

  it('keeps Tab inside the dialog', async () => {
    render(<Modal onCancel={vi.fn()} />)

    await userEvent.tab({ shift: true })
    expect(screen.getByRole('button', { name: 'Last' })).toHaveFocus()

    await userEvent.tab()
    expect(screen.getByRole('button', { name: 'First' })).toHaveFocus()
  })

  it('hands the keys to the dialog opened over it', async () => {
    const onCancel = vi.fn()
    render(<Modal onCancel={onCancel} nested />)

    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Inner' })).not.toBeInTheDocument()
    expect(onCancel).not.toHaveBeenCalled()

    await userEvent.keyboard('{Escape}')
    expect(onCancel).toHaveBeenCalledOnce()
  })

  it('cancels a close request the system sends', () => {
    const onCancel = vi.fn()
    render(<Modal onCancel={onCancel} />)

    const event = new Event('cancel', { cancelable: true })
    screen.getByRole('dialog', { name: 'Outer' }).dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(onCancel).toHaveBeenCalledOnce()
  })
})

describe('nextFocus', () => {
  it('wraps around, and moves in from outside', () => {
    const dialog = document.createElement('dialog')
    const a = document.createElement('button')
    const b = document.createElement('button')
    dialog.append(a, b)
    dialog.setAttribute('open', '')
    document.body.append(dialog)

    expect(nextFocus(dialog, false)).toBe(a)
    expect(nextFocus(dialog, true)).toBe(b)

    a.focus()
    expect(nextFocus(dialog, true)).toBe(b)
    expect(nextFocus(dialog, false)).toBeNull()

    b.focus()
    expect(nextFocus(dialog, false)).toBe(a)

    expect(nextFocus(document.createElement('dialog'), false)).toBeNull()
    dialog.remove()
  })

  it('knows the topmost open dialog', () => {
    const a = document.createElement('dialog')
    const b = document.createElement('dialog')
    a.setAttribute('open', '')
    b.setAttribute('open', '')
    document.body.append(a, b)

    expect(isTopmost(b)).toBe(true)
    expect(isTopmost(a)).toBe(false)

    a.remove()
    b.remove()
  })
})
