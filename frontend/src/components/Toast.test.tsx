import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { Toast, toastMs } from './Toast'

// The toast's timer runs on fake timers. Testing Library waits for a timer of its own
// after each user-event action, which it moves on itself only under Jest's fake timers, so
// it is shown Vitest's as Jest's.
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('jest', {
    advanceTimersByTime: (ms: number) => {
      vi.advanceTimersByTime(ms)
    },
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

// user acts as a person would, its pauses moving the fake clock on.
function user() {
  return userEvent.setup({
    advanceTimers: (ms) => {
      vi.advanceTimersByTime(ms)
    },
  })
}

// cardOf returns the toast's card, the element the pointer rests on.
function cardOf(text: string): HTMLElement {
  const card = screen.getByText(text).closest('div')
  if (card === null) {
    throw new Error('no toast card')
  }

  return card
}

describe('Toast', () => {
  it('keeps an empty live region mounted when there is nothing to say', () => {
    render(<Toast toast={null} onClose={vi.fn()} />)

    expect(screen.getByRole('status')).toBeEmptyDOMElement()
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite')
  })

  it('shows a message with its detail and an Undo, and closes itself', async () => {
    const person = user()
    const onClose = vi.fn()
    const undo = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Dismissed', detail: 'Add the widget cache', undo }} onClose={onClose} />)

    expect(screen.getByRole('status')).toHaveTextContent('Dismissed · Add the widget cache')

    await person.click(screen.getByRole('button', { name: 'Undo' }))
    expect(undo).toHaveBeenCalledOnce()

    // The click left the pointer and focus on the toast, which holds it; clicking
    // elsewhere lets it run out.
    await person.click(document.body)

    act(() => {
      vi.advanceTimersByTime(toastMs - 1)
    })
    expect(onClose).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('shows an action button with its label', async () => {
    const person = user()
    const onClick = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Cleaned up', action: { label: 'View', onClick } }} onClose={vi.fn()} />)

    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()

    await person.click(screen.getByRole('button', { name: 'View' }))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it('waits while the pointer rests on it', async () => {
    const person = user()
    const onClose = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Restored' }} onClose={onClose} />)

    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()

    const card = cardOf('Restored')

    await person.hover(card)
    act(() => {
      vi.advanceTimersByTime(toastMs * 3)
    })
    expect(onClose).not.toHaveBeenCalled()

    await person.unhover(card)
    act(() => {
      vi.advanceTimersByTime(toastMs)
    })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('waits while focus is in it, so Undo can be reached with the keyboard', () => {
    const onClose = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Dismissed', undo: vi.fn() }} onClose={onClose} />)

    const undo = screen.getByRole('button', { name: 'Undo' })
    act(() => {
      undo.focus()
    })
    act(() => {
      screen.getByRole('button', { name: 'Close' }).focus()
    })
    act(() => {
      vi.advanceTimersByTime(toastMs * 3)
    })
    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('group', { name: 'Notification' })).toBeInTheDocument()

    act(() => {
      screen.getByRole('button', { name: 'Close' }).blur()
    })
    act(() => {
      vi.advanceTimersByTime(toastMs)
    })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('gives a toast replaced under the pointer its own full time', async () => {
    const person = user()
    const onClose = vi.fn()
    const { rerender } = render(<Toast toast={{ id: 1, text: 'Dismissed' }} onClose={onClose} />)

    await person.hover(cardOf('Dismissed'))
    rerender(<Toast toast={{ id: 2, text: 'Snoozed', tone: 'error' }} onClose={onClose} />)

    expect(screen.getByText('Snoozed')).toHaveClass('text-danger')

    act(() => {
      vi.advanceTimersByTime(toastMs)
    })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('closes on request', async () => {
    const person = user()
    const onClose = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Dismissed', detail: '' }} onClose={onClose} />)

    expect(screen.getByRole('status')).toHaveTextContent(/^Dismissed$/)

    await person.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledOnce()
  })
})
