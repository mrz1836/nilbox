import { afterEach, describe, expect, it, vi } from 'vitest'

import { holdPlace, scrollerOf } from './scroll'

// page builds a scroller holding a box, scrolled to scrollTop, with the box standing top
// pixels from the top of the window.
function page(scrollTop: number, top: number) {
  const scroller = document.createElement('main')
  scroller.style.overflowY = 'auto'
  const wrapper = document.createElement('div')
  const box = document.createElement('section')
  wrapper.append(box)
  scroller.append(wrapper)
  document.body.append(scroller)
  scroller.scrollTop = scrollTop
  vi.spyOn(box, 'getBoundingClientRect').mockReturnValue(DOMRect.fromRect({ x: 0, y: top, width: 100, height: 40 }))

  return { scroller, box }
}

afterEach(() => {
  document.body.replaceChildren()
})

describe('scrollerOf', () => {
  it('finds the nearest ancestor that scrolls', () => {
    const { scroller, box } = page(0, 0)

    expect(scrollerOf(box)).toBe(scroller)
  })

  it('falls back to the document’s scroller', () => {
    const box = document.createElement('div')
    document.body.append(box)

    expect(scrollerOf(box)).toBe(document.scrollingElement)
  })
})

describe('holdPlace', () => {
  it('scrolls by as far as the layout above moved the box', () => {
    // The box stood at 600 and the layout above it shrank, pulling it up to 100.
    const { scroller, box } = page(800, 100)

    holdPlace(box, 600)

    expect(scroller.scrollTop).toBe(300)
  })

  it('leaves the scroller alone when the box did not move', () => {
    const { scroller, box } = page(800, 600)

    holdPlace(box, 600)

    expect(scroller.scrollTop).toBe(800)
  })
})
