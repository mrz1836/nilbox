import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PopoverPanel } from '../components/PopoverPanel'
import { usePopover } from './usePopover'

// Probe is a popover inside a sheet that can take focus itself, as the review sheet can,
// with a button outside both.
function Probe() {
  const popover = usePopover<HTMLButtonElement, HTMLDivElement>()
  const { toggle, triggerRef } = popover

  return (
    <>
      <section aria-label="Sheet" tabIndex={-1}>
        <div className="relative">
          <button ref={triggerRef} type="button" onClick={toggle}>
            Options
          </button>
          <PopoverPanel state={popover} align="right" role="dialog" aria-label="Options">
            <label>
              <input type="radio" name="prompt" /> Fix checks
            </label>
          </PopoverPanel>
        </div>
      </section>
      <button type="button">Elsewhere</button>
    </>
  )
}

// panel is the open popover, or null.
function panel() {
  return screen.queryByRole('dialog', { name: 'Options' })
}

describe('usePopover with PopoverPanel', () => {
  it('stays open when a click in it focuses no control, as the desktop WebKit does', async () => {
    render(<Probe />)
    await userEvent.click(screen.getByRole('button', { name: 'Options' }))

    // The desktop's WebKit gives a radio no focus on a click and focuses the nearest
    // focusable thing holding it. The panel takes focus itself, so that is the panel,
    // never the sheet around it, which would read as focus leaving.
    expect(panel()).toHaveAttribute('tabindex', '-1')

    act(() => {
      panel()?.focus()
    })
    expect(panel()).toBeInTheDocument()

    // Focus anywhere else still closes it.
    act(() => {
      screen.getByRole('button', { name: 'Elsewhere' }).focus()
    })
    expect(panel()).not.toBeInTheDocument()
  })
})
