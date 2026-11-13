import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { Sheet } from './Sheet'

// Body is a sheet's content: its title and a step arrow, disabled at the list's end.
function Body({ end = false }: { end?: boolean }) {
  return (
    <>
      <h2 id="title">Parser drops the trailing newline</h2>
      <button type="button" data-step="next" disabled={end}>
        Next
      </button>
    </>
  )
}

describe('Sheet', () => {
  it('starts focus on the sheet itself', () => {
    render(
      <Sheet labelledBy="title" onClose={vi.fn()}>
        <Body />
      </Sheet>,
    )

    expect(screen.getByRole('dialog', { name: 'Parser drops the trailing newline' })).toHaveAttribute('open')
    expect(document.activeElement).toBe(screen.getByRole('dialog').firstElementChild)
  })

  it('starts focus on the control it is told to, while it can take focus', () => {
    const { unmount } = render(
      <Sheet labelledBy="title" onClose={vi.fn()} initialFocus='[data-step="next"]:not(:disabled)'>
        <Body />
      </Sheet>,
    )

    expect(screen.getByRole('button', { name: 'Next' })).toHaveFocus()
    unmount()

    const { unmount: unmountEnd } = render(
      <Sheet labelledBy="title" onClose={vi.fn()} initialFocus='[data-step="next"]:not(:disabled)'>
        <Body end />
      </Sheet>,
    )
    expect(document.activeElement).toBe(screen.getByRole('dialog').firstElementChild)
    unmountEnd()

    // A disabled control it picks can't take focus, which stays on the sheet.
    render(
      <Sheet labelledBy="title" onClose={vi.fn()} initialFocus='[data-step="next"]'>
        <Body end />
      </Sheet>,
    )
    expect(document.activeElement).toBe(screen.getByRole('dialog').firstElementChild)
  })
})
