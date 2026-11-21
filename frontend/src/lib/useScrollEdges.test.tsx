import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ScrollFades } from '../components/ScrollFades'
import { followResizes } from '../test/stubs'
import { useScrollEdges } from './useScrollEdges'

// Scroller is a list in a scroller with its fades, as the settings page lays them out.
function Scroller() {
  const [scroller, edges] = useScrollEdges()

  return (
    <div className="relative">
      <div data-testid="scroller" ref={scroller}>
        <div>A long list</div>
      </div>
      <ScrollFades {...edges} />
    </div>
  )
}

// layout gives the scroller the sizes and the place jsdom, which lays nothing out, lacks.
function layout(el: HTMLElement, at: { scrollTop: number; scrollHeight: number; clientHeight: number }) {
  for (const [key, value] of Object.entries(at)) {
    Object.defineProperty(el, key, { configurable: true, value })
  }
}

// shown lists the fades that show.
function shown(): (string | null)[] {
  return [...document.querySelectorAll('[data-scroll-fade][data-shown]')].map((el) =>
    el.getAttribute('data-scroll-fade'),
  )
}

describe('useScrollEdges', () => {
  it('fades no end of a scroller whose content fits', () => {
    render(<Scroller />)

    expect(shown()).toEqual([])
  })

  it('fades each end with more beyond it as the scroller scrolls', () => {
    render(<Scroller />)
    const scroller = screen.getByTestId('scroller')

    layout(scroller, { scrollTop: 0, scrollHeight: 900, clientHeight: 300 })
    fireEvent.scroll(scroller)
    expect(shown()).toEqual(['below'])

    layout(scroller, { scrollTop: 300, scrollHeight: 900, clientHeight: 300 })
    fireEvent.scroll(scroller)
    expect(shown()).toEqual(['above', 'below'])

    // Rounding can leave a scroller a fraction of a pixel short of its end.
    layout(scroller, { scrollTop: 599.5, scrollHeight: 900, clientHeight: 300 })
    fireEvent.scroll(scroller)
    expect(shown()).toEqual(['above'])
  })

  it('measures again when the scroller or its content changes size', () => {
    const { resize } = followResizes()
    render(<Scroller />)
    const scroller = screen.getByTestId('scroller')

    layout(scroller, { scrollTop: 0, scrollHeight: 900, clientHeight: 300 })
    resize()
    expect(shown()).toEqual(['below'])

    // A taller window shows the whole list.
    layout(scroller, { scrollTop: 0, scrollHeight: 900, clientHeight: 900 })
    resize()
    expect(shown()).toEqual([])
  })

  it('stops following a scroller that goes away', () => {
    const { unmount } = render(<Scroller />)
    const scroller = screen.getByTestId('scroller')
    const removed = vi.spyOn(scroller, 'removeEventListener')

    unmount()

    expect(removed).toHaveBeenCalledWith('scroll', expect.any(Function))
  })
})
