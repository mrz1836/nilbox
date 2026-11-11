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

describe('usePopover', () => {
  it('stays open when a click in it leaves focus on what holds it, as the desktop WebKit does', async () => {
    render(<Probe />)
    await userEvent.click(screen.getByRole('button', { name: 'Options' }))

    // The desktop's WebKit focuses no radio on a click, but the nearest focusable thing
    // holding it: the panel itself, or else the sheet.
    act(() => {
      panel()?.focus()
    })
    expect(panel()).toBeInTheDocument()

    act(() => {
      screen.getByRole('region', { name: 'Sheet' }).focus()
    })
    expect(panel()).toBeInTheDocument()

    // Focus anywhere else still closes it.
    act(() => {
      screen.getByRole('button', { name: 'Elsewhere' }).focus()
    })
    expect(panel()).not.toBeInTheDocument()
  })

  it('takes focus itself, so a click in it keeps focus there', async () => {
    render(<Probe />)
    await userEvent.click(screen.getByRole('button', { name: 'Options' }))

    expect(panel()).toHaveAttribute('tabindex', '-1')
  })
})
