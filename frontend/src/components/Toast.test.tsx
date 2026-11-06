import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { Toast, toastMs } from './Toast'

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('Toast', () => {
  it('keeps an empty live region mounted when there is nothing to say', () => {
    render(<Toast toast={null} onClose={vi.fn()} />)

    expect(screen.getByRole('status')).toBeEmptyDOMElement()
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite')
  })

  it('shows a message with its detail and an Undo, and closes itself', () => {
    const onClose = vi.fn()
    const undo = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Dismissed', detail: 'Add the widget cache', undo }} onClose={onClose} />)

    expect(screen.getByRole('status')).toHaveTextContent('Dismissed · Add the widget cache')

    fireEvent.click(screen.getByRole('button', { name: 'Undo' }))
    expect(undo).toHaveBeenCalledOnce()

    act(() => {
      vi.advanceTimersByTime(toastMs - 1)
    })
    expect(onClose).not.toHaveBeenCalled()

    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('shows an action button with its label', () => {
    const onClick = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Cleaned up', action: { label: 'View', onClick } }} onClose={vi.fn()} />)

    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'View' }))
    expect(onClick).toHaveBeenCalledOnce()
  })

  it('waits while the pointer rests on it', () => {
    const onClose = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Restored' }} onClose={onClose} />)

    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument()

    const card = screen.getByText('Restored').closest('div')
    if (card === null) {
      throw new Error('no toast card')
    }

    fireEvent.mouseEnter(card)
    act(() => {
      vi.advanceTimersByTime(toastMs * 3)
    })
    expect(onClose).not.toHaveBeenCalled()

    fireEvent.mouseLeave(card)
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

  it('gives a toast replaced under the pointer its own full time', () => {
    const onClose = vi.fn()
    const { rerender } = render(<Toast toast={{ id: 1, text: 'Dismissed' }} onClose={onClose} />)

    const card = screen.getByText('Dismissed').closest('div')
    if (card === null) {
      throw new Error('no toast card')
    }

    fireEvent.mouseEnter(card)
    rerender(<Toast toast={{ id: 2, text: 'Snoozed', tone: 'error' }} onClose={onClose} />)

    expect(screen.getByText('Snoozed')).toHaveClass('text-danger')

    act(() => {
      vi.advanceTimersByTime(toastMs)
    })
    expect(onClose).toHaveBeenCalledOnce()
  })

  it('closes on request', () => {
    const onClose = vi.fn()
    render(<Toast toast={{ id: 1, text: 'Dismissed', detail: '' }} onClose={onClose} />)

    expect(screen.getByRole('status')).toHaveTextContent(/^Dismissed$/)

    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledOnce()
  })
})
