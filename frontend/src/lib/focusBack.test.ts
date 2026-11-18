import { fireEvent } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { focusBack } from './focusBack'

// button adds a button to the page.
function button(name: string): HTMLButtonElement {
  const el = document.createElement('button')
  el.textContent = name
  document.body.append(el)

  return el
}

afterEach(() => {
  document.body.replaceChildren()
})

describe('focusBack', () => {
  it('gives focus back as the browser does after a key, ring and all', () => {
    const trigger = button('Options')
    const focus = vi.spyOn(trigger, 'focus')
    fireEvent.keyDown(document.body, { key: 'Escape' })

    focusBack(trigger)

    expect(focus).toHaveBeenCalledExactlyOnceWith()
    expect(trigger).toHaveFocus()
  })

  it('gives focus back without a ring after a pointer, letting go of what held it first', () => {
    const trigger = button('Options')
    const item = button('Copy link')
    item.focus()
    const blur = vi.spyOn(item, 'blur')
    const focus = vi.spyOn(trigger, 'focus')
    fireEvent.pointerDown(item)

    focusBack(trigger)

    expect(blur).toHaveBeenCalledOnce()
    expect(focus).toHaveBeenCalledExactlyOnceWith({ focusVisible: false })
    expect(trigger).toHaveFocus()
  })

  it('lets go of the button itself when a closing dialog already gave it focus back', () => {
    const trigger = button('Options')
    trigger.focus()
    const blur = vi.spyOn(trigger, 'blur')
    fireEvent.pointerDown(document.body)

    focusBack(trigger)

    expect(blur).toHaveBeenCalledOnce()
    expect(trigger).toHaveFocus()
  })

  it('moves focus on from a drawing that holds it', () => {
    const trigger = button('Options')
    const drawing = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    drawing.setAttribute('tabindex', '0')
    document.body.append(drawing)
    drawing.focus()
    fireEvent.pointerDown(drawing)
    expect(document.activeElement).toBe(drawing)

    focusBack(trigger)

    expect(trigger).toHaveFocus()
  })
})
