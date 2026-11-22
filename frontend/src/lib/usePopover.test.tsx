import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { PopoverPanel } from '../components/PopoverPanel'
import { shiftInto, usePopover } from './usePopover'

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

// box stands el at left..right, as the browser lays it out, 10px tall.
function box(el: Element, left: number, right: number) {
  el.getBoundingClientRect = () => DOMRect.fromRect({ x: left, y: 0, width: right - left, height: 10 })
}

describe('shiftInto', () => {
  // inArea is a panel inside an area that scrolls, so clips it, 256px wide at the
  // window's left edge, as the Autopilot tab's list of routines is.
  function inArea() {
    const area = document.createElement('div')
    const panel = document.createElement('div')

    area.style.overflowY = 'auto'
    area.style.overflowX = 'auto'
    area.append(panel)
    document.body.append(area)
    box(area, 0, 256)

    return panel
  }

  it('moves a panel that runs past an edge of what clips it back inside, a gap from the edge', () => {
    const panel = inArea()

    box(panel, -20, 180)
    expect(shiftInto(panel)).toBe(32)

    box(panel, 100, 300)
    expect(shiftInto(panel)).toBe(-56)

    box(panel, 20, 220)
    expect(shiftInto(panel), 'it fits where it opens').toBe(0)
  })

  it('shows the start of a panel wider than the room, and leaves one not laid out alone', () => {
    const panel = inArea()

    box(panel, -50, 400)
    expect(shiftInto(panel)).toBe(62)

    box(panel, 0, 0)
    expect(shiftInto(panel)).toBe(0)
  })

  it('keeps a panel in the window when nothing around it clips', () => {
    const panel = document.createElement('div')
    document.body.append(panel)

    box(panel, window.innerWidth - 100, window.innerWidth + 50)
    expect(shiftInto(panel)).toBe(-62)
  })
})

describe('a panel in an area that clips it', () => {
  it('opens moved in from the edge it would run past, lined up with its button otherwise', async () => {
    render(
      <nav aria-label="Routines" style={{ overflowY: 'auto', overflowX: 'auto' }}>
        <Probe />
      </nav>,
    )

    box(screen.getByRole('navigation', { name: 'Routines' }), 0, 256)
    // The panel lines up with its button's right edge, so it opens 20px past the left one;
    // it moves as its right edge says, and nothing else here is laid out.
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const shift = this.style.right === '' ? 0 : -Number.parseFloat(this.style.right)

      return this.getAttribute('aria-label') === 'Options' && this.getAttribute('role') === 'dialog'
        ? DOMRect.fromRect({ x: -20 + shift, y: 0, width: 200, height: 10 })
        : DOMRect.fromRect()
    })

    await userEvent.click(screen.getByRole('button', { name: 'Options' }))

    expect(panel()).toHaveStyle({ right: '-32px' })
  })
})
